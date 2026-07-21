import CodexPulseCoreClient
import Darwin
import Foundation

private enum SpikeError: Error, CustomStringConvertible {
    case usage
    case invalidBootstrapMode(String)
    case helperExit(Int32)
    case isolatedDatabaseMissing

    var description: String {
        switch self {
        case .usage:
            return "usage: codex-pulse-transport-spike --helper <path> [--runtime-root <path>]"
        case .invalidBootstrapMode(let mode):
            return "unexpected bootstrap mode: \(mode)"
        case .helperExit(let status):
            return "helper exited with status \(status)"
        case .isolatedDatabaseMissing:
            return "helper did not create the isolated database"
        }
    }
}

private struct Arguments {
    let helperPath: String
    let runtimeRoot: String
    let removeRuntimeRoot: Bool

    init(_ values: [String]) throws {
        var helperPath: String?
        var runtimeRoot: String?
        var index = 0
        while index < values.count {
            guard index + 1 < values.count else { throw SpikeError.usage }
            switch values[index] {
            case "--helper": helperPath = values[index + 1]
            case "--runtime-root": runtimeRoot = values[index + 1]
            default: throw SpikeError.usage
            }
            index += 2
        }
        guard let helperPath else { throw SpikeError.usage }
        self.helperPath = URL(fileURLWithPath: helperPath).standardizedFileURL.path
        if let runtimeRoot {
            self.runtimeRoot = URL(fileURLWithPath: runtimeRoot).standardizedFileURL.path
            self.removeRuntimeRoot = false
        } else {
            self.runtimeRoot = "/private/tmp/cp-\(UUID().uuidString.prefix(12))"
            self.removeRuntimeRoot = true
        }
    }
}

private func milliseconds(
    from start: ContinuousClock.Instant,
    to end: ContinuousClock.Instant
) -> Double {
    let components = start.duration(to: end).components
    return Double(components.seconds) * 1_000 + Double(components.attoseconds) / 1e15
}

private func percentile(_ values: [Double], _ percentile: Double) -> Double {
    let sorted = values.sorted()
    guard !sorted.isEmpty else { return 0 }
    let index = Int((Double(sorted.count - 1) * percentile).rounded(.up))
    return sorted[min(index, sorted.count - 1)]
}

private func residentBytes(processID: Int32) throws -> Int64 {
    let process = Process()
    let output = Pipe()
    process.executableURL = URL(fileURLWithPath: "/bin/ps")
    process.arguments = ["-o", "rss=", "-p", String(processID)]
    process.standardOutput = output
    process.standardError = FileHandle.nullDevice
    try process.run()
    process.waitUntilExit()
    guard process.terminationStatus == 0,
          let data = try output.fileHandleForReading.readToEnd(),
          let text = String(data: data, encoding: .utf8),
          let kilobytes = Int64(text.trimmingCharacters(in: .whitespacesAndNewlines))
    else { return 0 }
    return kilobytes * 1_024
}

private func fileSize(_ path: String) -> Int64 {
    let attributes = try? FileManager.default.attributesOfItem(atPath: path)
    return (attributes?[.size] as? NSNumber)?.int64Value ?? 0
}

private func setSQLiteUserVersion(databasePath: String, version: Int32) throws {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/sqlite3")
    process.arguments = [databasePath, "PRAGMA user_version = \(version);"]
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.nullDevice
    try process.run()
    process.waitUntilExit()
    guard process.terminationStatus == 0 else {
        throw SpikeError.invalidBootstrapMode("sqlite_seed_failed")
    }
}

@main
struct TransportSpikeMain {
    static func main() async throws {
        let arguments = try Arguments(Array(CommandLine.arguments.dropFirst()))
        let supervisor = HelperSupervisor(configuration: .init(
            executablePath: arguments.helperPath,
            runtimeDirectory: arguments.runtimeRoot
        ))
        var client: CoreClient?
        do {
            let clock = ContinuousClock()
            let coldStart = clock.now
            print("transport spike stage: cold_start")
            let helper = try await supervisor.start()
            let connectedClient = try CoreClient(
                socketPath: helper.socketPath,
                bearerToken: helper.bearerToken
            )
            client = connectedClient

            let handshake = try await connectedClient.handshake(clientVersion: "transport-spike")
            let coldHandshakeMilliseconds = milliseconds(from: coldStart, to: clock.now)
            let bootstrap = try await connectedClient.bootstrap()
            guard bootstrap.mode == "normal" || bootstrap.mode == "recovery" else {
                throw SpikeError.invalidBootstrapMode(bootstrap.mode)
            }

            let streamTask = Task {
                try await connectedClient.consumeInvalidations(domains: ["quota"]) { _ in }
            }
            try await Task.sleep(for: .milliseconds(100))
            streamTask.cancel()
            do {
                try await streamTask.value
            } catch {
                guard streamTask.isCancelled else { throw error }
            }

            let controller = InvalidationStreamController(
                client: connectedClient,
                domains: ["quota"],
                onEvent: { _ in }
            )
            await controller.start()
            try await controller.waitUntilReady()
            try await controller.prepareForSleep(sendLifecycle: false)
            try await controller.resumeAfterWake(sendLifecycle: false)
            try await controller.waitUntilReady()
            let (_, streamMetrics) = await controller.snapshot()
            await controller.stop()

            var unaryMilliseconds: [Double] = []
            for _ in 0..<20 {
                let started = clock.now
                _ = try await connectedClient.bootstrap()
                unaryMilliseconds.append(milliseconds(from: started, to: clock.now))
            }
            let totalResidentBytes = try residentBytes(processID: getpid()) +
                residentBytes(processID: helper.processID)
            let swiftBinaryBytes = fileSize(CommandLine.arguments[0])
            let helperBinaryBytes = fileSize(arguments.helperPath)

            let abnormalRecoveryStarted = clock.now
            print("transport spike stage: abnormal_restart")
            await supervisor.stop(mode: .kill)
            await connectedClient.closeTransport()
            let restartedHelper = try await supervisor.start()
            let restartedClient = try CoreClient(
                socketPath: restartedHelper.socketPath,
                bearerToken: restartedHelper.bearerToken
            )
            client = restartedClient
            _ = try await restartedClient.handshake(clientVersion: "transport-spike-restart")
            let restartedBootstrap = try await restartedClient.bootstrap()
            guard restartedBootstrap.mode == "normal" || restartedBootstrap.mode == "recovery" else {
                throw SpikeError.invalidBootstrapMode(restartedBootstrap.mode)
            }
            let abnormalRecoveryMilliseconds = milliseconds(from: abnormalRecoveryStarted, to: clock.now)

            try await restartedClient.shutdown()
            let exitStatus = try await supervisor.waitForExit()
            guard exitStatus == 0 else { throw SpikeError.helperExit(exitStatus) }

            try setSQLiteUserVersion(databasePath: helper.databasePath, version: 99)
            print("transport spike stage: migration_recovery")
            let recoveryHelper = try await supervisor.start()
            let recoveryClient = try CoreClient(
                socketPath: recoveryHelper.socketPath,
                bearerToken: recoveryHelper.bearerToken
            )
            client = recoveryClient
            _ = try await recoveryClient.handshake(clientVersion: "transport-spike-recovery")
            let recoveryBootstrap = try await recoveryClient.bootstrap()
            guard case .recovery(let recoverySnapshot) = BootstrapState(recoveryBootstrap) else {
                throw SpikeError.invalidBootstrapMode(recoveryBootstrap.mode)
            }
            try setSQLiteUserVersion(
                databasePath: helper.databasePath,
                version: recoverySnapshot.targetVersion
            )
            let recoveryReceipt = try await recoveryClient.migrationRecoveryRetry()
            guard RecoveryTransition(recoveryReceipt) == .restartRequired else {
                throw SpikeError.invalidBootstrapMode(recoveryReceipt.phase)
            }
            try await recoveryClient.shutdown(reason: "client_restart")
            let recoveryExitStatus = try await supervisor.waitForExit()
            guard recoveryExitStatus == 0 else { throw SpikeError.helperExit(recoveryExitStatus) }
            guard FileManager.default.fileExists(atPath: helper.databasePath),
                  helper.databasePath.hasPrefix(arguments.runtimeRoot + "/")
            else { throw SpikeError.isolatedDatabaseMissing }

            if arguments.removeRuntimeRoot {
                try FileManager.default.removeItem(atPath: arguments.runtimeRoot)
            }
            let coldHandshakeText = String(format: "%.2f", coldHandshakeMilliseconds)
            let unaryP50Text = String(format: "%.2f", percentile(unaryMilliseconds, 0.50))
            let unaryP95Text = String(format: "%.2f", percentile(unaryMilliseconds, 0.95))
            let streamReconnectText = String(
                format: "%.2f",
                streamMetrics.lastReconnectMilliseconds ?? 0
            )
            let abnormalRecoveryText = String(format: "%.2f", abnormalRecoveryMilliseconds)
            print(
                "transport spike passed: helper=\(handshake.helperVersion) " +
                "contract=\(handshake.contractVersion) mode=\(bootstrap.mode) " +
                "cancellation=passed recovery_restart=passed " +
                "cold_handshake_ms=\(coldHandshakeText) unary_p50_ms=\(unaryP50Text) " +
                "unary_p95_ms=\(unaryP95Text) stream_reconnect_ms=\(streamReconnectText) " +
                "abnormal_recovery_ms=\(abnormalRecoveryText) " +
                "idle_total_rss_bytes=\(totalResidentBytes) swift_binary_bytes=\(swiftBinaryBytes) " +
                "helper_binary_bytes=\(helperBinaryBytes)"
            )
        } catch {
            if let client { await client.closeTransport() }
            await supervisor.terminate()
            if arguments.removeRuntimeRoot {
                try? FileManager.default.removeItem(atPath: arguments.runtimeRoot)
            }
            throw error
        }
    }
}
