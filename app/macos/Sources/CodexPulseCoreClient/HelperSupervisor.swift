import Darwin
import Foundation

public enum HelperSupervisorError: Error, Equatable, Sendable {
    case alreadyRunning
    case invalidExecutable
    case runtimeDirectory(String)
    case pipe(Int32)
    case spawn(Int32)
    case tokenWrite(Int32)
    case launchCancelled
    case socketTimeout
    case exitTimeout
    case helperExited(Int32)
    case wait(Int32)
}

public struct RunningHelper: Sendable {
    public let processID: Int32
    public let socketPath: String
    public let databasePath: String
    public let preferencesPath: String
    public let bearerToken: String
}

public enum HelperStopMode: Sendable {
    case terminate
    case kill
}

public actor HelperSupervisor {
    public struct Configuration: Sendable {
        public let executablePath: String
        public let runtimeDirectory: String
        public let startupTimeout: Duration

        public init(
            executablePath: String,
            runtimeDirectory: String,
            startupTimeout: Duration = .seconds(10)
        ) {
            self.executablePath = executablePath
            self.runtimeDirectory = runtimeDirectory
            self.startupTimeout = startupTimeout
        }
    }

    private let configuration: Configuration
    private var processID: pid_t?
    private var authWriteDescriptor: Int32?
    private var launchGeneration: UInt64 = 0
    private var lastExit: (processID: pid_t, status: Int32)?

    public init(configuration: Configuration) {
        self.configuration = configuration
    }

    deinit {
        if let descriptor = authWriteDescriptor {
            Darwin.close(descriptor)
        }
        if let processID {
            Darwin.kill(processID, SIGTERM)
        }
    }

    public func start() async throws -> RunningHelper {
        guard processID == nil else { throw HelperSupervisorError.alreadyRunning }
        guard FileManager.default.isExecutableFile(atPath: configuration.executablePath) else {
            throw HelperSupervisorError.invalidExecutable
        }

        launchGeneration &+= 1
        let generation = launchGeneration
        let paths = try prepareRuntimePaths()
        let previousSocketIdentity = try Self.validatedSocketIdentity(paths.socket)
        let token = Self.makeBearerToken()
        var descriptors: [Int32] = [0, 0]
        guard Darwin.pipe(&descriptors) == 0 else { throw HelperSupervisorError.pipe(errno) }
        let readDescriptor = descriptors[0]
        let writeDescriptor = descriptors[1]
        do {
            try Self.setCloseOnExec(readDescriptor)
            try Self.setCloseOnExec(writeDescriptor)
            try Self.setNoSigPipe(writeDescriptor)
        } catch {
            Darwin.close(readDescriptor)
            Darwin.close(writeDescriptor)
            throw error
        }
        var parentOwnsReadDescriptor = true
        var parentOwnsWriteDescriptor = true
        var spawnedProcessID: pid_t?

        do {
            let childPID = try Self.spawn(
                executablePath: configuration.executablePath,
                readDescriptor: readDescriptor,
                writeDescriptor: writeDescriptor,
                socketPath: paths.socket,
                databasePath: paths.database,
                preferencesPath: paths.preferences
            )
            spawnedProcessID = childPID
            Darwin.close(readDescriptor)
            parentOwnsReadDescriptor = false
            processID = childPID
            authWriteDescriptor = writeDescriptor
            parentOwnsWriteDescriptor = false
            try writeToken(token, to: writeDescriptor)
            try await waitForSocket(
                paths.socket,
                replacing: previousSocketIdentity,
                processID: childPID,
                generation: generation,
                timeout: configuration.startupTimeout
            )
            guard self.processID == childPID, launchGeneration == generation else {
                throw HelperSupervisorError.launchCancelled
            }
            return RunningHelper(
                processID: childPID,
                socketPath: paths.socket,
                databasePath: paths.database,
                preferencesPath: paths.preferences,
                bearerToken: token
            )
        } catch {
            if parentOwnsReadDescriptor { Darwin.close(readDescriptor) }
            if parentOwnsWriteDescriptor {
                Darwin.close(writeDescriptor)
            } else if authWriteDescriptor == writeDescriptor {
                closeAuthPipe()
            }
            if let childPID = spawnedProcessID,
               self.processID == childPID,
               launchGeneration == generation {
                Darwin.kill(childPID, SIGKILL)
                var status: Int32 = 0
                _ = Darwin.waitpid(childPID, &status, 0)
                lastExit = (childPID, Self.exitStatus(status))
                self.processID = nil
            }
            throw error
        }
    }

    public func waitForExit(timeout: Duration = .seconds(10)) async throws -> Int32 {
        guard let processID else { return lastExit?.status ?? 0 }
        let clock = ContinuousClock()
        let deadline = clock.now.advanced(by: timeout)
        while clock.now < deadline {
            if self.processID != processID {
                return lastExit?.processID == processID ? lastExit!.status : 0
            }
            var status: Int32 = 0
            let result = Darwin.waitpid(processID, &status, WNOHANG)
            if result == processID {
                let exitStatus = Self.exitStatus(status)
                self.processID = nil
                closeAuthPipe()
                lastExit = (processID, exitStatus)
                return exitStatus
            }
            if result == -1 {
                if errno == ECHILD, self.processID != processID {
                    return lastExit?.processID == processID ? lastExit!.status : 0
                }
                throw HelperSupervisorError.wait(errno)
            }
            try await Task.sleep(for: .milliseconds(20))
        }
        throw HelperSupervisorError.exitTimeout
    }

    public func terminate() async {
        await stop(mode: .terminate)
    }

    public func stop(mode: HelperStopMode) async {
        guard let processID else { return }
        launchGeneration &+= 1
        switch mode {
        case .terminate:
            closeAuthPipe()
            Darwin.kill(processID, SIGTERM)
        case .kill:
            Darwin.kill(processID, SIGKILL)
            closeAuthPipe()
        }
        _ = try? await waitForExit(timeout: .seconds(5))
    }

    private func prepareRuntimePaths() throws -> (socket: String, database: String, preferences: String) {
        let root = URL(fileURLWithPath: configuration.runtimeDirectory, isDirectory: true)
        let data = root.appendingPathComponent("data", isDirectory: true)
        do {
            try Self.ensurePrivateDirectory(root.path, withIntermediateDirectories: true)
            try Self.ensurePrivateDirectory(data.path, withIntermediateDirectories: false)
        } catch let error as HelperSupervisorError {
            throw error
        } catch {
            throw HelperSupervisorError.runtimeDirectory("create")
        }
        let socketPath = root.appendingPathComponent("core.sock").path
        guard socketPath.utf8.count <= 103 else {
            throw HelperSupervisorError.runtimeDirectory("socket_path_too_long")
        }
        if try Self.validatedSocketIdentity(socketPath) != nil {
            do {
                try FileManager.default.removeItem(atPath: socketPath)
            } catch {
                throw HelperSupervisorError.runtimeDirectory("remove_residual_socket")
            }
        }
        return (
            socketPath,
            data.appendingPathComponent("codex-pulse.db").path,
            root.appendingPathComponent("preferences.json").path
        )
    }

    private nonisolated static func spawn(
        executablePath: String,
        readDescriptor: Int32,
        writeDescriptor: Int32,
        socketPath: String,
        databasePath: String,
        preferencesPath: String
    ) throws -> pid_t {
        let inheritedDescriptor: Int32 = 3
        var actions: posix_spawn_file_actions_t?
        let actionsResult = posix_spawn_file_actions_init(&actions)
        guard actionsResult == 0 else {
            throw HelperSupervisorError.spawn(actionsResult)
        }
        defer { posix_spawn_file_actions_destroy(&actions) }
        if readDescriptor == inheritedDescriptor {
            try Self.requireSpawnAction(
                posix_spawn_file_actions_addinherit_np(&actions, inheritedDescriptor)
            )
        } else {
            try Self.requireSpawnAction(
                posix_spawn_file_actions_adddup2(&actions, readDescriptor, inheritedDescriptor)
            )
            try Self.requireSpawnAction(
                posix_spawn_file_actions_addclose(&actions, readDescriptor)
            )
        }
        try Self.requireSpawnAction(
            posix_spawn_file_actions_addclose(&actions, writeDescriptor)
        )

        var attributes: posix_spawnattr_t?
        let attributesResult = posix_spawnattr_init(&attributes)
        guard attributesResult == 0 else {
            throw HelperSupervisorError.spawn(attributesResult)
        }
        defer { posix_spawnattr_destroy(&attributes) }
        try Self.requireSpawnAction(
            posix_spawnattr_setflags(&attributes, Int16(POSIX_SPAWN_CLOEXEC_DEFAULT))
        )

        let arguments = [
            executablePath,
            "--socket", socketPath,
            "--auth-fd", String(inheritedDescriptor),
            "--database-path", databasePath,
            "--preferences-path", preferencesPath,
        ]
        let environment = ProcessInfo.processInfo.environment.map { "\($0.key)=\($0.value)" }
        var childPID: pid_t = 0
        let result = Self.withCStringArray(arguments) { argumentVector in
            Self.withCStringArray(environment) { environmentVector in
                posix_spawn(
                    &childPID,
                    executablePath,
                    &actions,
                    &attributes,
                    argumentVector,
                    environmentVector
                )
            }
        }
        guard result == 0 else { throw HelperSupervisorError.spawn(result) }
        return childPID
    }

    private nonisolated static func requireSpawnAction(_ result: Int32) throws {
        guard result == 0 else { throw HelperSupervisorError.spawn(result) }
    }

    private nonisolated static func setCloseOnExec(_ descriptor: Int32) throws {
        guard Darwin.fcntl(descriptor, F_SETFD, FD_CLOEXEC) != -1 else {
            throw HelperSupervisorError.pipe(errno)
        }
    }

    private nonisolated static func setNoSigPipe(_ descriptor: Int32) throws {
        guard Darwin.fcntl(descriptor, F_SETNOSIGPIPE, 1) != -1 else {
            throw HelperSupervisorError.pipe(errno)
        }
    }

    private func writeToken(_ token: String, to descriptor: Int32) throws {
        let bytes = Array((token + "\n").utf8)
        var written = 0
        while written < bytes.count {
            let count = bytes.withUnsafeBytes { buffer in
                Darwin.write(descriptor, buffer.baseAddress!.advanced(by: written), bytes.count - written)
            }
            if count < 0 {
                if errno == EINTR { continue }
                throw HelperSupervisorError.tokenWrite(errno)
            }
            if count == 0 { throw HelperSupervisorError.tokenWrite(EIO) }
            written += count
        }
    }

    private func waitForSocket(
        _ path: String,
        replacing previousIdentity: UInt64?,
        processID expectedProcessID: pid_t,
        generation expectedGeneration: UInt64,
        timeout: Duration
    ) async throws {
        let clock = ContinuousClock()
        let deadline = clock.now.advanced(by: timeout)
        while clock.now < deadline {
            guard launchGeneration == expectedGeneration, processID == expectedProcessID else {
                throw HelperSupervisorError.launchCancelled
            }
            if let identity = try Self.validatedSocketIdentity(path), identity != previousIdentity {
                return
            }
            var status: Int32 = 0
            let result = Darwin.waitpid(expectedProcessID, &status, WNOHANG)
            if result == expectedProcessID {
                let exitStatus = Self.exitStatus(status)
                if processID == expectedProcessID {
                    self.processID = nil
                    closeAuthPipe()
                    lastExit = (expectedProcessID, exitStatus)
                }
                throw HelperSupervisorError.helperExited(exitStatus)
            }
            if result == -1 { throw HelperSupervisorError.wait(errno) }
            try await Task.sleep(for: .milliseconds(20))
        }
        throw HelperSupervisorError.socketTimeout
    }

    private func closeAuthPipe() {
        if let authWriteDescriptor {
            Darwin.close(authWriteDescriptor)
            self.authWriteDescriptor = nil
        }
    }

    private static func makeBearerToken() -> String {
        let alphabet = Array("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_")
        var generator = SystemRandomNumberGenerator()
        return String((0..<48).map { _ in alphabet.randomElement(using: &generator)! })
    }

    private nonisolated static func ensurePrivateDirectory(
        _ path: String,
        withIntermediateDirectories: Bool
    ) throws {
        var metadata = stat()
        if Darwin.lstat(path, &metadata) == 0 {
            guard metadata.st_mode & S_IFMT == S_IFDIR, metadata.st_uid == getuid() else {
                throw HelperSupervisorError.runtimeDirectory("unsafe_directory")
            }
        } else if errno == ENOENT {
            do {
                try FileManager.default.createDirectory(
                    atPath: path,
                    withIntermediateDirectories: withIntermediateDirectories,
                    attributes: [.posixPermissions: 0o700]
                )
            } catch {
                throw HelperSupervisorError.runtimeDirectory("create")
            }
        } else {
            throw HelperSupervisorError.runtimeDirectory("inspect_directory")
        }
        guard Darwin.chmod(path, 0o700) == 0 else {
            throw HelperSupervisorError.runtimeDirectory("permission")
        }
        try validatePrivateDirectory(path)
    }

    nonisolated static func validatePrivateDirectory(_ path: String) throws {
        var metadata = stat()
        guard Darwin.lstat(path, &metadata) == 0 else {
            throw HelperSupervisorError.runtimeDirectory("inspect_directory")
        }
        guard metadata.st_mode & S_IFMT == S_IFDIR,
              metadata.st_uid == getuid(),
              metadata.st_mode & 0o777 == 0o700
        else { throw HelperSupervisorError.runtimeDirectory("unsafe_directory") }
    }

    nonisolated static func validatedSocketIdentity(_ path: String) throws -> UInt64? {
        var metadata = stat()
        guard Darwin.lstat(path, &metadata) == 0 else {
            if errno == ENOENT { return nil }
            throw HelperSupervisorError.runtimeDirectory("inspect_socket")
        }
        guard metadata.st_mode & S_IFMT == S_IFSOCK,
              metadata.st_uid == getuid(),
              metadata.st_mode & 0o777 == 0o600
        else { throw HelperSupervisorError.runtimeDirectory("unsafe_socket") }
        return UInt64(metadata.st_ino)
    }

    private static func exitStatus(_ status: Int32) -> Int32 {
        let signal = status & 0x7f
        if signal == 0 { return (status >> 8) & 0xff }
        if signal != 0x7f { return 128 + signal }
        return status
    }

    private static func withCStringArray<Result>(
        _ strings: [String],
        body: (UnsafeMutablePointer<UnsafeMutablePointer<CChar>?>) -> Result
    ) -> Result {
        var pointers = strings.map { strdup($0) }
        pointers.append(nil)
        defer { pointers.dropLast().forEach { free($0) } }
        return pointers.withUnsafeMutableBufferPointer { buffer in
            body(buffer.baseAddress!)
        }
    }
}
