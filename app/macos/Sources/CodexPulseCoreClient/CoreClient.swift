import CodexPulseProtocolGenerated
import GRPCCore
import GRPCNIOTransportHTTP2
import GRPCProtobuf

public enum CoreClientError: Error, Equatable, Sendable {
    case incompatibleContract(expected: String, actual: String)
    case incompatibleTransport(expected: String, actual: String)
    case shutdownRejected
}

public enum LifecycleEvent: String, Sendable {
    case systemWillSleep = "system_will_sleep"
    case systemDidWake = "system_did_wake"
    case applicationDidBecomeActive = "application_did_become_active"
}

public actor CoreClient {
    private let grpcClient: GRPCClient<HTTP2ClientTransport.Posix>
    private let service: Codexpulse_Core_V1_CoreService.Client<HTTP2ClientTransport.Posix>
    private let metadata: Metadata
    private var connectionTask: Task<Void, any Error>?

    public init(socketPath: String, bearerToken: String) throws {
        let transport = try HTTP2ClientTransport.Posix(
            target: .unixDomainSocket(path: socketPath),
            transportSecurity: .plaintext
        )
        let grpcClient = GRPCClient(transport: transport)
        self.grpcClient = grpcClient
        self.service = Codexpulse_Core_V1_CoreService.Client(wrapping: grpcClient)
        self.metadata = ["authorization": "Bearer \(bearerToken)"]
        self.connectionTask = Task { try await grpcClient.runConnections() }
    }

    public func handshake(
        clientName: String = "codex-pulse-macos",
        clientVersion: String,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_HandshakeResponse {
        var requestBuilder = Codexpulse_Core_V1_HandshakeRequest()
        requestBuilder.clientName = clientName
        requestBuilder.clientVersion = clientVersion
        requestBuilder.contractVersion = CodexPulseTransportContract.version
        let request = requestBuilder
        let service = service
        let metadata = metadata
        let response = try await retryPolicy.execute {
            try await service.handshake(request, metadata: metadata)
        }
        guard response.contractVersion == CodexPulseTransportContract.version else {
            throw CoreClientError.incompatibleContract(
                expected: CodexPulseTransportContract.version,
                actual: response.contractVersion
            )
        }
        guard response.transport == CodexPulseTransportContract.transport else {
            throw CoreClientError.incompatibleTransport(
                expected: CodexPulseTransportContract.transport,
                actual: response.transport
            )
        }
        return response
    }

    public func bootstrap(
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_BootstrapResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.bootstrap(Codexpulse_Core_V1_BootstrapRequest(), metadata: metadata)
        }
    }

    public func notifyLifecycle(
        _ event: LifecycleEvent
    ) async throws -> Codexpulse_Core_V1_LifecycleNotificationReceipt {
        var request = Codexpulse_Core_V1_LifecycleNotificationRequest()
        request.event = event.rawValue
        return try await service.notifyLifecycle(request, metadata: metadata)
    }

    public func migrationRecoveryState(
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_MigrationRecoverySnapshot {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.migrationRecoveryState(
                Codexpulse_Core_V1_MigrationRecoveryStateRequest(),
                metadata: metadata
            )
        }
    }

    public func migrationRecoveryRetry() async throws -> Codexpulse_Core_V1_MigrationRecoveryReceipt {
        try await service.migrationRecoveryRetry(
            Codexpulse_Core_V1_MigrationRecoveryRetryRequest(),
            metadata: metadata
        )
    }

    public func consumeInvalidations(
        domains: [String],
        afterSequence: UInt64 = 0,
        onReady: @Sendable @escaping () async -> Void = {},
        onEvent: @Sendable @escaping (Codexpulse_Core_V1_QueryInvalidationEvent) async throws -> Void
    ) async throws {
        var request = Codexpulse_Core_V1_SubscribeInvalidationsRequest()
        request.domains = domains
        request.afterSequence = afterSequence
        try await service.subscribeInvalidations(request, metadata: metadata) { response in
            await onReady()
            for try await event in response.messages {
                try Task.checkCancellation()
                try await onEvent(event)
            }
        }
    }

    public func shutdown(reason: String = "client_exit") async throws {
        var request = Codexpulse_Core_V1_ShutdownRequest()
        request.reason = reason
        let response = try await service.shutdown(request, metadata: metadata)
        guard response.accepted else { throw CoreClientError.shutdownRejected }
        grpcClient.beginGracefulShutdown()
        if let connectionTask {
            _ = try await connectionTask.value
            self.connectionTask = nil
        }
    }

    public func closeTransport() async {
        grpcClient.beginGracefulShutdown()
        if let connectionTask {
            _ = try? await connectionTask.value
            self.connectionTask = nil
        }
    }
}
