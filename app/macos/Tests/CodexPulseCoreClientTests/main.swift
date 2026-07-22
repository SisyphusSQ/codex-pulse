import CodexPulseProtocolGenerated
import Darwin
import Foundation
import GRPCCore
import GRPCProtobuf
import SwiftProtobuf
@testable import CodexPulseCoreClient

private enum TestFailure: Error, CustomStringConvertible {
    case mismatch(String)

    var description: String {
        switch self {
        case .mismatch(let message): return message
        }
    }
}

private func expect<Value: Equatable>(
    _ actual: Value,
    _ expected: Value,
    _ context: String
) throws {
    guard actual == expected else {
        throw TestFailure.mismatch("\(context): got \(actual), want \(expected)")
    }
}

private func waitUntil(
    _ context: String,
    timeout: Duration = .seconds(3),
    condition: @escaping @Sendable () async -> Bool
) async throws {
    let clock = ContinuousClock()
    let deadline = clock.now.advanced(by: timeout)
    while clock.now < deadline {
        if await condition() { return }
        try await Task.sleep(for: .milliseconds(10))
    }
    throw TestFailure.mismatch("timed out: \(context)")
}

private actor StreamRaceHarness {
    private var calls = 0
    private var waiters: [Int: CheckedContinuation<Void, Never>] = [:]
    private var released: Set<Int> = []

    func consume(
        onReady: @Sendable @escaping () async -> Void
    ) async throws {
        calls += 1
        let call = calls
        if call == 2 { await onReady() }
        await withCheckedContinuation { continuation in
            if released.remove(call) != nil {
                continuation.resume()
            } else {
                waiters[call] = continuation
            }
        }
        if call == 1 { await onReady() }
    }

    func callCount() -> Int { calls }

    func release(_ call: Int) {
        if let continuation = waiters.removeValue(forKey: call) {
            continuation.resume()
        } else {
            released.insert(call)
        }
    }
}

private actor ImmediateEOFHarness {
    private var calls = 0

    func consume(onReady: @Sendable @escaping () async -> Void) async {
        calls += 1
        await onReady()
    }

    func callCount() -> Int { calls }
}

private actor ReadyFlag {
    private var ready = false

    func markReady() { ready = true }
    func isReady() -> Bool { ready }
}

private actor RetryHarness {
    private var attempts = 0

    func unavailableThenSucceed(failures: Int) throws -> Int {
        attempts += 1
        if attempts <= failures {
            throw RPCError(code: .unavailable, message: "test unavailable")
        }
        return attempts
    }

    func failWithoutRetry() throws -> Int {
        attempts += 1
        throw RPCError(code: .invalidArgument, message: "test invalid")
    }

    func count() -> Int { attempts }
}

private actor LifecycleRaceHarness {
    private var streamCalls = 0
    private var lifecycleCalls = 0
    private var lifecycleContinuation: CheckedContinuation<Void, Never>?
    private var lifecycleReleased = false

    func consume(onReady: @Sendable @escaping () async -> Void) async throws {
        streamCalls += 1
        await onReady()
        try await Task.sleep(for: .seconds(60))
    }

    func notify() async {
        lifecycleCalls += 1
        await withCheckedContinuation { continuation in
            if lifecycleReleased {
                continuation.resume()
            } else {
                lifecycleContinuation = continuation
            }
        }
    }

    func releaseLifecycle() {
        lifecycleReleased = true
        lifecycleContinuation?.resume()
        lifecycleContinuation = nil
    }

    func streamCallCount() -> Int { streamCalls }
    func lifecycleCallCount() -> Int { lifecycleCalls }
}

private func testReadRetryPolicy() async throws {
    let retrying = RetryHarness()
    let attempts = try await ReadRetryPolicy(maximumAttempts: 3, backoff: .zero).execute {
        try await retrying.unavailableThenSucceed(failures: 2)
    }
    try expect(attempts, 3, "bounded read retry succeeds within budget")

    let nonRetrying = RetryHarness()
    do {
        _ = try await ReadRetryPolicy(maximumAttempts: 3, backoff: .zero).execute {
            try await nonRetrying.failWithoutRetry()
        }
        throw TestFailure.mismatch("non-retryable read error was replayed")
    } catch let error as RPCError {
        try expect(error.code, .invalidArgument, "non-retryable read error code")
    }
    try expect(await nonRetrying.count(), 1, "non-retryable read attempt count")
}

private func testLifecycleTransitionReentrancy() async throws {
    let harness = LifecycleRaceHarness()
    let controller = InvalidationStreamController(
        domains: ["quota"],
        consumeInvalidations: { _, _, onReady, _ in
            try await harness.consume(onReady: onReady)
        },
        notifyLifecycle: { _ in await harness.notify() },
        onEvent: { _ in }
    )
    await controller.start()
    try await controller.waitUntilReady()
    try await controller.prepareForSleep(sendLifecycle: false)

    let firstResume = Task { try await controller.resumeAfterWake(sendLifecycle: true) }
    try await waitUntil("wake lifecycle call") { await harness.lifecycleCallCount() == 1 }
    do {
        try await controller.resumeAfterWake(sendLifecycle: false)
        throw TestFailure.mismatch("concurrent wake transition was accepted")
    } catch InvalidationStreamError.invalidTransition {
        // Expected.
    }
    await controller.stop()
    await harness.releaseLifecycle()
    do {
        try await firstResume.value
        throw TestFailure.mismatch("stopped wake transition revived the stream")
    } catch InvalidationStreamError.invalidTransition {
        // Expected.
    }
    try expect(await harness.streamCallCount(), 1, "resume-vs-stop stream launch count")
}

private func testStreamGenerationIsolation() async throws {
    let harness = StreamRaceHarness()
    let controller = InvalidationStreamController(
        domains: ["quota"],
        consumeInvalidations: { _, _, onReady, _ in
            try await harness.consume(onReady: onReady)
        },
        onEvent: { _ in }
    )
    await controller.start()
    try await waitUntil("first stream launch") { await harness.callCount() == 1 }
    try await controller.prepareForSleep(sendLifecycle: false)
    try await controller.resumeAfterWake(sendLifecycle: false)
    try await waitUntil("replacement stream launch") { await harness.callCount() == 2 }
    try await controller.waitUntilReady()
    do {
        try await controller.resumeAfterWake(sendLifecycle: false)
        throw TestFailure.mismatch("duplicate wake transition was accepted")
    } catch InvalidationStreamError.invalidTransition {
        // Expected.
    }

    await harness.release(1)
    try await Task.sleep(for: .milliseconds(150))
    let (state, _) = await controller.snapshot()
    try expect(state, .ready, "stale stream callback must not replace current state")
    try expect(await harness.callCount(), 2, "stale stream must not launch a third stream")

    await controller.stop()
    await harness.release(2)
}

private func testReconnectBudget() async throws {
    let harness = ImmediateEOFHarness()
    let terminalFailure = ReadyFlag()
    let controller = InvalidationStreamController(
        domains: ["quota"],
        maximumReconnectAttempts: 2,
        consumeInvalidations: { _, _, onReady, _ in
            await harness.consume(onReady: onReady)
        },
        onTerminalFailure: { await terminalFailure.markReady() },
        onEvent: { _ in }
    )
    await controller.start()
    try await waitUntil("bounded reconnect failure") {
        let (state, _) = await controller.snapshot()
        return state == .failed
    }
    try expect(await harness.callCount(), 3, "initial stream plus bounded reconnect attempts")
    try expect(await terminalFailure.isReady(), true, "terminal reconnect failure must notify owner")
}

private func makeProbeScript(
    at path: String,
    startupDelay: Double = 0,
    socketPermissionDelay: Double = 0
) throws {
    let script = """
    #!/usr/bin/python3
    import os, socket, sys, time
    values = sys.argv[1:]
    def value(flag):
        return values[values.index(flag) + 1]
    sentinel = int(os.environ.get("CODEX_PULSE_SENTINEL_FD", "-1"))
    if sentinel >= 0:
        try:
            os.fstat(sentinel)
            sys.exit(71)
        except OSError:
            pass
    auth_fd = int(value("--auth-fd"))
    token = os.read(auth_fd, 256)
    if not token.endswith(b"\\n"):
        sys.exit(72)
    time.sleep(\(startupDelay))
    socket_path = value("--socket")
    server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    server.bind(socket_path)
    time.sleep(\(socketPermissionDelay))
    os.chmod(socket_path, 0o600)
    server.listen(1)
    while os.read(auth_fd, 1):
        pass
    server.close()
    os.unlink(socket_path)
    """
    try script.write(toFile: path, atomically: true, encoding: .utf8)
    guard Darwin.chmod(path, 0o700) == 0 else {
        throw TestFailure.mismatch("failed to make probe executable")
    }
}

private func testSupervisorSecurityAndCancellation() async throws {
    let root = "/private/tmp/cp-test-\(UUID().uuidString.prefix(12))"
    try FileManager.default.createDirectory(
        atPath: root,
        withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700]
    )
    defer { try? FileManager.default.removeItem(atPath: root) }

    let unsafeDirectory = root + "/unsafe"
    try FileManager.default.createDirectory(
        atPath: unsafeDirectory,
        withIntermediateDirectories: false
    )
    guard Darwin.chmod(unsafeDirectory, 0o755) == 0 else {
        throw TestFailure.mismatch("failed to set unsafe fixture permissions")
    }
    do {
        try HelperSupervisor.validatePrivateDirectory(unsafeDirectory)
        throw TestFailure.mismatch("unsafe runtime directory was accepted")
    } catch HelperSupervisorError.runtimeDirectory("unsafe_directory") {
        // Expected.
    }

    let symlinkTarget = root + "/symlink-target"
    try FileManager.default.createDirectory(
        atPath: symlinkTarget,
        withIntermediateDirectories: false,
        attributes: [.posixPermissions: 0o700]
    )
    let symlinkRuntime = root + "/symlink-runtime"
    guard Darwin.symlink(symlinkTarget, symlinkRuntime) == 0 else {
        throw TestFailure.mismatch("failed to create runtime symlink fixture")
    }

    let probePath = root + "/probe.py"
    try makeProbeScript(at: probePath)
    let symlinkSupervisor = HelperSupervisor(configuration: .init(
        executablePath: probePath,
        runtimeDirectory: symlinkRuntime
    ))
    do {
        _ = try await symlinkSupervisor.start()
        throw TestFailure.mismatch("symlink runtime directory was accepted")
    } catch HelperSupervisorError.runtimeDirectory("unsafe_directory") {
        // Expected.
    }
    let sentinelSource = Darwin.open("/dev/null", O_RDONLY)
    guard sentinelSource >= 0 else {
        throw TestFailure.mismatch("failed to open sentinel descriptor")
    }
    let sentinel = Darwin.fcntl(sentinelSource, F_DUPFD, 100)
    Darwin.close(sentinelSource)
    guard sentinel >= 100 else {
        throw TestFailure.mismatch("failed to move sentinel descriptor out of auth-fd range")
    }
    defer { Darwin.close(sentinel) }
    guard Darwin.fcntl(sentinel, F_SETFD, 0) != -1 else {
        throw TestFailure.mismatch("failed to clear sentinel CLOEXEC")
    }
    setenv("CODEX_PULSE_SENTINEL_FD", String(sentinel), 1)
    defer { unsetenv("CODEX_PULSE_SENTINEL_FD") }

    let supervisor = HelperSupervisor(configuration: .init(
        executablePath: probePath,
        runtimeDirectory: root + "/runtime"
    ))
    let helper = try await supervisor.start()
    guard try HelperSupervisor.validatedSocketIdentity(helper.socketPath) != nil else {
        throw TestFailure.mismatch("validated probe socket was not observed")
    }
    await supervisor.stop(mode: .kill)

    let permissionProbePath = root + "/permission-probe.py"
    try makeProbeScript(at: permissionProbePath, socketPermissionDelay: 0.15)
    let permissionSupervisor = HelperSupervisor(configuration: .init(
        executablePath: permissionProbePath,
        runtimeDirectory: root + "/permission-runtime"
    ))
    let permissionHelper = try await permissionSupervisor.start()
    guard try HelperSupervisor.validatedSocketIdentity(permissionHelper.socketPath) != nil else {
        throw TestFailure.mismatch("supervisor accepted a socket before the 0600 readback")
    }
    await permissionSupervisor.stop(mode: .kill)

    let delayedProbePath = root + "/delayed-probe.py"
    try makeProbeScript(at: delayedProbePath, startupDelay: 5)
    let delayedSupervisor = HelperSupervisor(configuration: .init(
        executablePath: delayedProbePath,
        runtimeDirectory: root + "/delayed-runtime"
    ))
    let startTask = Task { try await delayedSupervisor.start() }
    try await Task.sleep(for: .milliseconds(100))
    await delayedSupervisor.stop(mode: .kill)
    do {
        _ = try await startTask.value
        throw TestFailure.mismatch("cancelled helper launch returned a running helper")
    } catch HelperSupervisorError.launchCancelled {
        // Expected.
    }
}

private func testCrossLanguageCancellation() async throws {
    guard let probePath = ProcessInfo.processInfo.environment["CODEX_PULSE_CANCEL_PROBE"],
          FileManager.default.isExecutableFile(atPath: probePath)
    else { throw TestFailure.mismatch("CODEX_PULSE_CANCEL_PROBE is not executable") }

    let root = "/private/tmp/cp-cancel-\(UUID().uuidString.prefix(12))"
    defer { try? FileManager.default.removeItem(atPath: root) }
    let supervisor = HelperSupervisor(configuration: .init(
        executablePath: probePath,
        runtimeDirectory: root
    ))
    var client: CoreClient?
    do {
        let helper = try await supervisor.start()
        let connectedClient = try CoreClient(
            socketPath: helper.socketPath,
            bearerToken: helper.bearerToken
        )
        client = connectedClient
        let ready = ReadyFlag()
        let stream = Task {
            try await connectedClient.consumeInvalidations(
                domains: ["quota"],
                onReady: { await ready.markReady() },
                onEvent: { _ in }
            )
        }
        try await waitUntil("grpc-go stream ready") { await ready.isReady() }
        stream.cancel()
        do {
            try await stream.value
        } catch {
            guard stream.isCancelled else { throw error }
        }
        try await waitUntil("grpc-go context cancellation marker") {
            FileManager.default.fileExists(atPath: helper.preferencesPath)
        }
        let marker = try String(contentsOfFile: helper.preferencesPath, encoding: .utf8)
        try expect(marker, "cancelled\n", "Go stream context cancellation marker")
        await connectedClient.closeTransport()
        client = nil
        await supervisor.stop(mode: .kill)
    } catch {
        if let client { await client.closeTransport() }
        await supervisor.stop(mode: .kill)
        throw error
    }
}

@main
struct ContractTestMain {
    static func main() async throws {
        try expect(CodexPulseTransportContract.version, "core-rpc-v1", "contract version")
        try expect(CodexPulseTransportContract.transport, "grpc+unix", "transport")
        try expect(LifecycleEvent.systemWillSleep.rawValue, "system_will_sleep", "sleep event")
        try expect(LifecycleEvent.systemDidWake.rawValue, "system_did_wake", "wake event")

        var zero = Codexpulse_Core_V1_NumericValue()
        zero.value = 0
        zero.unit = "count"
        try expect(NumericState(zero), .known(value: 0, unit: "count"), "explicit zero")

        var unknown = Codexpulse_Core_V1_NumericValue()
        unknown.unknownReason = "not_observed"
        unknown.unit = "tokens"
        try expect(
            NumericState(unknown),
            .unknown(reason: "not_observed", unit: "tokens"),
            "unknown numeric value"
        )

        var absent = Codexpulse_Core_V1_NumericValue()
        absent.unit = "usd_micros"
        try expect(NumericState(absent), .absent(unit: "usd_micros"), "absent numeric value")

        try expect(ResponseDisposition(status: "complete"), .complete, "complete disposition")
        try expect(ResponseDisposition(status: "partial"), .partial, "partial disposition")
        try expect(ResponseDisposition(status: "unavailable"), .unavailable, "unavailable disposition")
        try expect(ResponseDisposition(status: "future"), .unsupported("future"), "future disposition")

        var normal = Codexpulse_Core_V1_BootstrapResponse()
        normal.mode = "normal"
        try expect(BootstrapState(normal), .normal, "normal bootstrap")
        var recoverySnapshot = Codexpulse_Core_V1_MigrationRecoverySnapshot()
        recoverySnapshot.version = "migration-recovery-v1"
        recoverySnapshot.phase = "failed"
        recoverySnapshot.stage = "migrate"
        var recovery = Codexpulse_Core_V1_BootstrapResponse()
        recovery.mode = "recovery"
        recovery.recovery = recoverySnapshot
        try expect(BootstrapState(recovery), .recovery(recoverySnapshot), "recovery bootstrap")
        var incompleteRecovery = Codexpulse_Core_V1_BootstrapResponse()
        incompleteRecovery.mode = "recovery"
        try expect(BootstrapState(incompleteRecovery), .unsupported("recovery"), "incomplete recovery")

        var retryReceipt = Codexpulse_Core_V1_MigrationRecoveryReceipt()
        retryReceipt.phase = "failed"
        try expect(
            RecoveryTransition(retryReceipt),
            .remainInRecovery(phase: "failed"),
            "recovery remains active"
        )
        retryReceipt.restartRequired = true
        try expect(RecoveryTransition(retryReceipt), .restartRequired, "restart required")

        var detail = Codexpulse_Core_V1_ErrorDetail()
        detail.code = "invalid_argument"
        detail.messageKey = "core.error.invalid_argument"
        detail.field = "event"
        let any = try Google_Protobuf_Any(message: detail)
        let status = GoogleRPCStatus(code: .invalidArgument, message: "redacted", details: .any(any))
        guard let packedDetail = status.details.first?.any else {
            throw TestFailure.mismatch("custom error detail wrapper was not retained")
        }
        let unpacked = try Codexpulse_Core_V1_ErrorDetail(unpackingAny: packedDetail)
        try expect(CoreErrorDetail(unpacked), CoreErrorDetail(detail), "direct custom error detail")
        try expect(
            CoreErrorDetail.decode(fromGoogleStatus: status),
            CoreErrorDetail(detail),
            "custom error detail"
        )

        try await testStreamGenerationIsolation()
        try await testReconnectBudget()
        try await testReadRetryPolicy()
        try await testLifecycleTransitionReentrancy()
        try await testSupervisorSecurityAndCancellation()
        try await testCrossLanguageCancellation()

        print("CodexPulseCoreClient deterministic tests passed")
    }
}
