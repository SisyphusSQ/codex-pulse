import CodexPulseCoreClient
import CodexPulseProtocolGenerated

public protocol AppCoreServing: Sendable {
    func handshake(
        clientName: String,
        clientVersion: String,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HandshakeResponse

    func bootstrap(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_BootstrapResponse

    func usageCost(
        _ request: Codexpulse_Core_V1_UsageCostRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_UsageCostResponse

    func quotaCurrent(
        _ request: Codexpulse_Core_V1_QuotaCurrentRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_QuotaCurrentResponse

    func requestQuotaRefresh(
        _ request: Codexpulse_Core_V1_QuotaRefreshRequest
    ) async throws -> Codexpulse_Core_V1_QuotaRefreshReceipt

    func runRuntimeAction(
        _ request: Codexpulse_Core_V1_RuntimeActionRequest
    ) async throws -> Codexpulse_Core_V1_RuntimeActionReceipt

    func listSessions(
        _ request: Codexpulse_Core_V1_ListSessionsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SessionListResponse

    func sessionDetail(
        _ request: Codexpulse_Core_V1_SessionDetailRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SessionDetailResponse

    func listProjects(
        _ request: Codexpulse_Core_V1_ListProjectsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_ProjectListResponse

    func projectDetail(
        _ request: Codexpulse_Core_V1_ProjectDetailRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_ProjectDetailResponse

    func listSources(
        _ request: Codexpulse_Core_V1_ListSourcesRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SourceListResponse

    func source(
        _ request: Codexpulse_Core_V1_SourceRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SourceDetailResponse

    func listJobs(
        _ request: Codexpulse_Core_V1_ListJobsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_JobListResponse

    func job(
        _ request: Codexpulse_Core_V1_JobRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_JobDetailResponse

    func listHealth(
        _ request: Codexpulse_Core_V1_ListHealthRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HealthListResponse

    func health(
        _ request: Codexpulse_Core_V1_HealthRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HealthDetailResponse

    func healthProjection(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HealthProjectionResponse

    func dataHealth(
        _ request: Codexpulse_Core_V1_DataHealthRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_DataHealthResponse

    func settings(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SettingsResponse

    func updateSettings(
        _ request: Codexpulse_Core_V1_UpdateSettingsRequest
    ) async throws -> Codexpulse_Core_V1_SettingsUpdateReceipt

    func migrationRecoveryRetry() async throws -> Codexpulse_Core_V1_MigrationRecoveryReceipt

    func notifyLifecycle(
        _ event: LifecycleEvent
    ) async throws -> Codexpulse_Core_V1_LifecycleNotificationReceipt

    func consumeInvalidations(
        domains: [String],
        afterSequence: UInt64,
        onReady: @Sendable @escaping () async -> Void,
        onEvent: @Sendable @escaping (Codexpulse_Core_V1_QueryInvalidationEvent) async throws -> Void
    ) async throws

    func shutdown(reason: String) async throws
    func closeTransport() async
}

public extension AppCoreServing {
    func requestQuotaRefresh(
        _ request: Codexpulse_Core_V1_QuotaRefreshRequest
    ) async throws -> Codexpulse_Core_V1_QuotaRefreshReceipt {
        throw AppRuntimeError.unavailable
    }

    func runRuntimeAction(
        _ request: Codexpulse_Core_V1_RuntimeActionRequest
    ) async throws -> Codexpulse_Core_V1_RuntimeActionReceipt {
        throw AppRuntimeError.unavailable
    }

    func sessionDetail(
        _ request: Codexpulse_Core_V1_SessionDetailRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SessionDetailResponse {
        throw AppRuntimeError.unavailable
    }

    func listProjects(
        _ request: Codexpulse_Core_V1_ListProjectsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_ProjectListResponse {
        throw AppRuntimeError.unavailable
    }

    func projectDetail(
        _ request: Codexpulse_Core_V1_ProjectDetailRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_ProjectDetailResponse {
        throw AppRuntimeError.unavailable
    }

    func listSources(
        _ request: Codexpulse_Core_V1_ListSourcesRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SourceListResponse {
        throw AppRuntimeError.unavailable
    }

    func source(
        _ request: Codexpulse_Core_V1_SourceRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SourceDetailResponse {
        throw AppRuntimeError.unavailable
    }

    func listJobs(
        _ request: Codexpulse_Core_V1_ListJobsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_JobListResponse {
        throw AppRuntimeError.unavailable
    }

    func job(
        _ request: Codexpulse_Core_V1_JobRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_JobDetailResponse {
        throw AppRuntimeError.unavailable
    }

    func listHealth(
        _ request: Codexpulse_Core_V1_ListHealthRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HealthListResponse {
        throw AppRuntimeError.unavailable
    }

    func health(
        _ request: Codexpulse_Core_V1_HealthRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HealthDetailResponse {
        throw AppRuntimeError.unavailable
    }

    func dataHealth(
        _ request: Codexpulse_Core_V1_DataHealthRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_DataHealthResponse {
        throw AppRuntimeError.unavailable
    }

    func settings(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SettingsResponse {
        throw AppRuntimeError.unavailable
    }

    func updateSettings(
        _ request: Codexpulse_Core_V1_UpdateSettingsRequest
    ) async throws -> Codexpulse_Core_V1_SettingsUpdateReceipt {
        throw AppRuntimeError.unavailable
    }
}

extension CoreClient: AppCoreServing {}

public protocol HelperSupervising: Sendable {
    func start() async throws -> RunningHelper
    func waitForExit(timeout: Duration) async throws -> Int32
    func stop(mode: HelperStopMode) async
}

extension HelperSupervisor: HelperSupervising {}
