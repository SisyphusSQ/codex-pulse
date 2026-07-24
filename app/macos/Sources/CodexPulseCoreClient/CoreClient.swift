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
            transportSecurity: .plaintext,
            serviceConfig: CodexPulseTransportContract.clientServiceConfig
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

    public func usageCost(
        _ request: Codexpulse_Core_V1_UsageCostRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_UsageCostResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.usageCost(request, metadata: metadata)
        }
    }

    public func quotaCurrent(
        _ request: Codexpulse_Core_V1_QuotaCurrentRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_QuotaCurrentResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.quotaCurrent(request, metadata: metadata)
        }
    }

    public func requestQuotaRefresh(
        _ request: Codexpulse_Core_V1_QuotaRefreshRequest
    ) async throws -> Codexpulse_Core_V1_QuotaRefreshReceipt {
        try await service.requestQuotaRefresh(request, metadata: metadata)
    }

    public func runRuntimeAction(
        _ request: Codexpulse_Core_V1_RuntimeActionRequest
    ) async throws -> Codexpulse_Core_V1_RuntimeActionReceipt {
        try await service.runRuntimeAction(request, metadata: metadata)
    }

    public func listSessions(
        _ request: Codexpulse_Core_V1_ListSessionsRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_SessionListResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.listSessions(request, metadata: metadata)
        }
    }

    public func sessionDetail(
        _ request: Codexpulse_Core_V1_SessionDetailRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_SessionDetailResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.sessionDetail(request, metadata: metadata)
        }
    }

    public func listProjects(
        _ request: Codexpulse_Core_V1_ListProjectsRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_ProjectListResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.listProjects(request, metadata: metadata)
        }
    }

    public func projectDetail(
        _ request: Codexpulse_Core_V1_ProjectDetailRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_ProjectDetailResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.projectDetail(request, metadata: metadata)
        }
    }

    public func listSources(
        _ request: Codexpulse_Core_V1_ListSourcesRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_SourceListResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.listSources(request, metadata: metadata)
        }
    }

    public func source(
        _ request: Codexpulse_Core_V1_SourceRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_SourceDetailResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.source(request, metadata: metadata)
        }
    }

    public func listJobs(
        _ request: Codexpulse_Core_V1_ListJobsRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_JobListResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.listJobs(request, metadata: metadata)
        }
    }

    public func job(
        _ request: Codexpulse_Core_V1_JobRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_JobDetailResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.job(request, metadata: metadata)
        }
    }

    public func listHealth(
        _ request: Codexpulse_Core_V1_ListHealthRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_HealthListResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.listHealth(request, metadata: metadata)
        }
    }

    public func health(
        _ request: Codexpulse_Core_V1_HealthRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_HealthDetailResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.health(request, metadata: metadata)
        }
    }

    public func healthProjection(
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_HealthProjectionResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.healthProjection(
                Codexpulse_Core_V1_HealthProjectionRequest(),
                metadata: metadata
            )
        }
    }

    public func dataHealth(
        _ request: Codexpulse_Core_V1_DataHealthRequest,
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_DataHealthResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.dataHealth(request, metadata: metadata)
        }
    }

    public func settings(
        retryPolicy: ReadRetryPolicy = .transportDefault
    ) async throws -> Codexpulse_Core_V1_SettingsResponse {
        let service = service
        let metadata = metadata
        return try await retryPolicy.execute {
            try await service.settings(Codexpulse_Core_V1_SettingsRequest(), metadata: metadata)
        }
    }

    public func updateSettings(
        _ request: Codexpulse_Core_V1_UpdateSettingsRequest
    ) async throws -> Codexpulse_Core_V1_SettingsUpdateReceipt {
        try await service.updateSettings(request, metadata: metadata)
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
        var options = CallOptions.defaults
        options.timeout = .seconds(5)
        let response = try await service.shutdown(request, metadata: metadata, options: options)
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
