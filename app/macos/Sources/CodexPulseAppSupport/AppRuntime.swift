import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

private enum ShutdownRequestResult: Equatable, Sendable {
    case accepted
    case failed
    case timedOut
}

private enum InvalidationStreamStartOutcome: Equatable, Sendable {
    case ready
    case sleeping
}

private enum InitialOverviewRefreshState: Equatable, Sendable {
    case idle
    case pending
    case inFlight
    case completed
}

private struct OverviewCacheKey: Hashable, Sendable {
	let provider: AgentProvider
	let range: DateRangePreset
}

private enum OverviewSectionResult<Value: Sendable>: Sendable {
    case value(Value)
    case failure(AppNotice)

    var notice: AppNotice? {
        if case .failure(let notice) = self { return notice }
        return nil
    }
}

private func captureOverviewSection<Value: Sendable>(
    _ operation: @Sendable () async throws -> Value
) async -> OverviewSectionResult<Value> {
    do {
        return .value(try await operation())
    } catch {
        return .failure(AppNotice.from(error))
    }
}

private func unavailableMeta() -> Codexpulse_Core_V1_ResponseMeta {
    var meta = Codexpulse_Core_V1_ResponseMeta()
    meta.version = "overview-v1"
    meta.status = "unavailable"
    return meta
}

private func unavailableQuota(
	at date: Date,
	provider: AgentProvider
) -> Codexpulse_Core_V1_QuotaCurrentResponse {
    var response = Codexpulse_Core_V1_QuotaCurrentResponse()
    response.meta = unavailableMeta()
    response.current.evaluatedAtMs = Int64(date.timeIntervalSince1970 * 1_000)
	response.providerContext = providerContext(for: provider)
    return response
}

private func unavailableQuotaPace(
    at date: Date,
	provider: AgentProvider
) -> Codexpulse_Core_V1_QuotaPaceResponse {
    var response = Codexpulse_Core_V1_QuotaPaceResponse()
    response.meta = unavailableMeta()
    response.pace.evaluatedAtMs = Int64(date.timeIntervalSince1970 * 1_000)
	response.providerContext = providerContext(for: provider)
    return response
}

private func providerContext(
    for provider: AgentProvider
) -> Codexpulse_Core_V1_ProviderContext {
    var context = Codexpulse_Core_V1_ProviderContext()
    context.effectiveProvider = provider.rawValue
    return context
}

private func unavailableUsage(
    provider: AgentProvider
) -> Codexpulse_Core_V1_UsageCostResponse {
    var response = Codexpulse_Core_V1_UsageCostResponse()
    response.meta = unavailableMeta()
    response.providerContext = providerContext(for: provider)
    return response
}

private func unavailableUsage(
    for request: Codexpulse_Core_V1_UsageCostRequest,
    provider: AgentProvider
) -> Codexpulse_Core_V1_UsageCostResponse {
    var response = unavailableUsage(provider: provider)
    if request.hasExactRange {
        response.range = request.exactRange
        response.reportingTimeZone = request.exactRange.timeZone
    }
    return response
}

private func unavailableInvocationUsage(
    for request: Codexpulse_Core_V1_InvocationUsageRequest,
    provider: AgentProvider
) -> Codexpulse_Core_V1_InvocationUsageResponse {
    var response = Codexpulse_Core_V1_InvocationUsageResponse()
    response.meta = unavailableMeta()
    response.providerContext = providerContext(for: provider)
    response.range = request.range
    response.granularity = request.granularity
    response.sourceClass = request.sourceClass
    return response
}

private func unavailableSessions(
    provider: AgentProvider
) -> Codexpulse_Core_V1_SessionListResponse {
    var response = Codexpulse_Core_V1_SessionListResponse()
    response.meta = unavailableMeta()
    response.providerContext = providerContext(for: provider)
    return response
}

private func unavailableProjects(
    provider: AgentProvider
) -> Codexpulse_Core_V1_ProjectListResponse {
    var response = Codexpulse_Core_V1_ProjectListResponse()
    response.meta = unavailableMeta()
    response.providerContext = providerContext(for: provider)
    return response
}

private func unavailableHealth() -> Codexpulse_Core_V1_HealthProjectionResponse {
    var response = Codexpulse_Core_V1_HealthProjectionResponse()
    response.failure = "overview_section_unavailable"
    return response
}

private final class OneShot<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var result: Value?
    private var continuation: CheckedContinuation<Value, Never>?

    func wait() async -> Value {
        await withCheckedContinuation { continuation in
            lock.lock()
            if let result {
                lock.unlock()
                continuation.resume(returning: result)
            } else {
                self.continuation = continuation
                lock.unlock()
            }
        }
    }

    func resolve(_ value: Value) {
        lock.lock()
        guard result == nil else {
            lock.unlock()
            return
        }
        result = value
        let continuation = continuation
        self.continuation = nil
        lock.unlock()
        continuation?.resume(returning: value)
    }
}

public enum AppRuntimeError: Error, Equatable, Sendable {
    case alreadyStarted
    case unavailable
    case invalidBootstrap
    case weeklyQuotaRangeUnavailable
	case providerMismatch
}

public enum ShutdownOutcome: Equatable, Sendable {
    case clean
    case forced
    case uncertain
}

public enum AppShutdownReason: Equatable, Sendable {
    case applicationExit
    case updateInstallation

    fileprivate var coreValue: String {
        switch self {
        case .applicationExit: "client_exit"
        case .updateInstallation: "client_restart"
        }
    }
}

public enum AppUpdateInstallPreparation: Equatable, Sendable {
    case ready
    case blocked(ShutdownOutcome)

    public init(shutdownOutcome: ShutdownOutcome) {
        switch shutdownOutcome {
        case .clean: self = .ready
        case .forced, .uncertain: self = .blocked(shutdownOutcome)
        }
    }
}

public actor AppRuntime {
    public typealias StateSink = @Sendable (CoreConnectionState) async -> Void
    public typealias InvalidationSink = @Sendable (_ domain: String) async -> Void
    public typealias ClientFactory = @Sendable (RunningHelper) throws -> any AppCoreServing
    public typealias ProcessMonitorFactory = @Sendable (
        _ processID: Int32,
        _ onExit: @escaping @Sendable () -> Void
    ) -> any HelperProcessMonitoring

    private let supervisor: any HelperSupervising
    private let clientFactory: ClientFactory
    private let clientVersion: String
    private let sendLifecycleToHelper: Bool
    private let shutdownRequestTimeout: Duration
    private let processMonitorFactory: ProcessMonitorFactory?
    private var stateSink: StateSink = { _ in }
    private var invalidationSink: InvalidationSink = { _ in }
    private var client: (any AppCoreServing)?
    private var streamController: InvalidationStreamController?
    private var helperProcessMonitor: (any HelperProcessMonitoring)?
    private var refreshTask: Task<OverviewResponses, any Error>?
    private var providerRefreshTask: Task<Codexpulse_Core_V1_ProviderRefreshReceipt, any Error>?
    private var accountRefreshTask: Task<Void, Never>?
    private var lastResponses: OverviewResponses?
	private var overviewCache: [OverviewCacheKey: OverviewResponses] = [:]
	private var overviewRange: DateRangePreset = .quotaWeek
	private var selectedProvider: AgentProvider = .codex
    private var runtimeGeneration: UInt64 = 0
    private var refreshGeneration: UInt64 = 0
    private var refreshAdmissionGeneration: UInt64?
    private var accountRefreshGeneration: UInt64 = 0
    private var startInFlight = false
    private var shuttingDown = false
    private var applicationIsActive = true
    private var systemIsSleeping = false
    private var sleepTransitionInFlight = false
    private var wakeAfterSleepPending = false
    private var sleepLifecycleDelivered = false
    private var readyForOverview = false
    private var activeRefreshPending = false
    private var invalidationRefreshPending = false
    private var initialOverviewRefreshState: InitialOverviewRefreshState = .idle
    private var streamHasReachedReady = false
    private var suppressNextStreamReadyRefresh = false

    public init(configuration: AppLaunchConfiguration) {
        self.supervisor = HelperSupervisor(configuration: .init(
            executablePath: configuration.helperExecutablePath,
            runtimeDirectory: configuration.runtimeDirectory
        ))
        self.clientVersion = configuration.clientVersion
        self.sendLifecycleToHelper = configuration.sendLifecycleToHelper
        self.shutdownRequestTimeout = .seconds(5)
        self.processMonitorFactory = { processID, onExit in
            DispatchHelperProcessMonitor(processID: processID, onExit: onExit)
        }
        self.clientFactory = { helper in
            try CoreClient(socketPath: helper.socketPath, bearerToken: helper.bearerToken)
        }
    }

    public init(
        supervisor: any HelperSupervising,
        clientVersion: String = "test",
        sendLifecycleToHelper: Bool = true,
        shutdownRequestTimeout: Duration = .seconds(5),
        processMonitorFactory: ProcessMonitorFactory? = nil,
        clientFactory: @escaping ClientFactory
    ) {
        self.supervisor = supervisor
        self.clientVersion = clientVersion
        self.sendLifecycleToHelper = sendLifecycleToHelper
        self.shutdownRequestTimeout = shutdownRequestTimeout
        self.processMonitorFactory = processMonitorFactory
        self.clientFactory = clientFactory
    }

    public func setStateSink(_ sink: @escaping StateSink) {
        stateSink = sink
    }

    public func setInvalidationSink(_ sink: @escaping InvalidationSink) {
        invalidationSink = sink
    }

    public func start() async {
        guard client == nil, !startInFlight, !shuttingDown else {
            await emit(.unavailable(AppNotice(
                code: "already_started",
                messageKey: "app.error.already_started",
                retryable: false
            )))
            return
        }
        startInFlight = true
        defer { startInFlight = false }
        runtimeGeneration &+= 1
        let generation = runtimeGeneration
        readyForOverview = false
        refreshAdmissionGeneration = nil
        invalidationRefreshPending = false
        initialOverviewRefreshState = .idle
        sleepTransitionInFlight = false
        wakeAfterSleepPending = false
        await emit(.starting)
        do {
            let helper = try await supervisor.start()
            try Task.checkCancellation()
            guard generation == runtimeGeneration else { throw CancellationError() }
            installHelperProcessMonitor(processID: helper.processID, generation: generation)
            await emit(.handshaking)
            let connectedClient = try clientFactory(helper)
            client = connectedClient
            _ = try await connectedClient.handshake(
                clientName: "codex-pulse-macos",
                clientVersion: clientVersion,
                retryPolicy: .transportDefault
            )
            guard generation == runtimeGeneration else { throw CancellationError() }
            let bootstrap = try await connectedClient.bootstrap(retryPolicy: .transportDefault)
            guard generation == runtimeGeneration else { throw CancellationError() }
            switch BootstrapState(bootstrap) {
            case .normal:
                readyForOverview = true
                requestInitialOverviewRefresh()
                if systemIsSleeping {
                    await suspendWithoutStream(client: connectedClient, generation: generation)
                    return
                }
                let streamOutcome = try await startInvalidationStream(
                    client: connectedClient,
                    generation: generation
                )
                guard streamOutcome == .ready, !systemIsSleeping else { return }
                guard generation == runtimeGeneration, client != nil else { return }
                await drainInitialOverviewRefresh(showLoading: true)
                guard generation == runtimeGeneration, client != nil else { return }
                if systemIsSleeping {
                    await suspendWithoutStream(client: connectedClient, generation: generation)
                    return
                }
                await deliverPendingActive(client: connectedClient, generation: generation)
            case .recovery(let snapshot):
                readyForOverview = false
                lastResponses = nil
				overviewCache.removeAll()
                await emit(.recovery(snapshot))
            case .unsupported:
                throw AppRuntimeError.invalidBootstrap
            }
        } catch is CancellationError {
            guard generation == runtimeGeneration else { return }
            await closeFailedStartup()
            await emit(.cancelled)
        } catch {
            guard generation == runtimeGeneration else { return }
            await closeFailedStartup()
            await emit(.unavailable(AppNotice.from(error)))
        }
    }

    public func refresh() async {
        await refresh(showLoading: false)
    }

	public func selectProvider(_ provider: AgentProvider) async {
		guard provider != selectedProvider else { return }
		selectedProvider = provider
		overviewRange = provider.defaultOverviewRange
		refreshGeneration &+= 1
		refreshAdmissionGeneration = nil
		invalidationRefreshPending = false
		refreshTask?.cancel()
		refreshTask = nil
		cancelAccountRefresh()
		lastResponses = overviewCache[OverviewCacheKey(provider: provider, range: overviewRange)]
		if readyForOverview {
			if let lastResponses {
				await publishOverview(lastResponses)
			}
			await refresh(showLoading: lastResponses == nil)
		}
	}

    public func refresh(range: DateRangePreset) async {
        guard range != .all else { return }
        if range != overviewRange {
            overviewRange = range
            refreshGeneration &+= 1
            refreshAdmissionGeneration = nil
            invalidationRefreshPending = false
            restorePendingInitialOverviewAfterCancellation()
            refreshTask?.cancel()
            refreshTask = nil
            cancelAccountRefresh()
        }
        await refresh(showLoading: false)
    }

    public func usageCost(
        _ request: Codexpulse_Core_V1_UsageCostRequest
    ) async throws -> Codexpulse_Core_V1_UsageCostResponse {
        try await performRead { try await $0.usageCost(request, retryPolicy: .transportDefault) }
    }

    public func dashboardSummary(
        _ request: Codexpulse_Core_V1_DashboardSummaryRequest
    ) async throws -> Codexpulse_Core_V1_DashboardSummaryResponse {
        try await performRead { try await $0.dashboardSummary(request, retryPolicy: .transportDefault) }
    }

    public func invocationUsage(
        _ request: Codexpulse_Core_V1_InvocationUsageRequest
    ) async throws -> Codexpulse_Core_V1_InvocationUsageResponse {
        try await performRead { try await $0.invocationUsage(request, retryPolicy: .transportDefault) }
    }

	public func statusOverview(
		provider: AgentProvider
	) async throws -> OverviewResponses {
		try await performRead { client in
			let now = Date()
			let requests = OverviewRequestSet.make(provider: provider, now: now)
			let quotaResult = await captureOverviewSection {
				try await client.quotaCurrent(requests.quota, retryPolicy: .transportDefault)
			}
			let quota: Codexpulse_Core_V1_QuotaCurrentResponse
			switch quotaResult {
			case .value(let response): quota = response
			case .failure: quota = unavailableQuota(at: now, provider: provider)
			}
			let evaluatedAt = quota.current.evaluatedAtMs > 0
				? Date(timeIntervalSince1970: Double(quota.current.evaluatedAtMs) / 1_000)
				: now
			let preset = provider.officialPeriodPreset(
				windowMinutes: quota.current.windows.first(where: \.hasWindowMinutes)?.windowMinutes
			)
			let range = OverviewRequestSet.resolveRange(
				preset,
				quota: quota,
				now: evaluatedAt
			)
			let content = OverviewRequestSet.content(range: range, provider: provider)
			let todayRange = OverviewRequestSet.resolveRange(.today, quota: quota, now: evaluatedAt)
			let todayContent = OverviewRequestSet.content(range: todayRange, provider: provider)
			let projectRequest = provider == .cursor
				? nil
				: OverviewRequestSet.periodProjectRanking(range: range, provider: provider)

			async let quotaPaceResult = captureOverviewSection {
				try await client.quotaPace(requests.quotaPace, retryPolicy: .transportDefault)
			}
			async let usageResult = captureOverviewSection {
				try await client.usageCost(content.usage, retryPolicy: .transportDefault)
			}
			async let todayUsageResult = captureOverviewSection {
				guard provider == .cursor else {
					return unavailableUsage(for: todayContent.usage, provider: provider)
				}
				return try await client.usageCost(
					todayContent.usage,
					retryPolicy: .transportDefault
				)
			}
			async let projectsResult = captureOverviewSection {
				guard let projectRequest else {
					return unavailableProjects(provider: provider)
				}
				return try await client.listProjects(
					projectRequest,
					retryPolicy: .transportDefault
				)
			}
			let results = await (
				quotaPaceResult,
				usageResult,
				todayUsageResult,
				projectsResult
			)
			let usage: Codexpulse_Core_V1_UsageCostResponse
			switch results.1 {
			case .value(let response): usage = response
			case .failure: usage = unavailableUsage(for: content.usage, provider: provider)
			}
			let invocation = unavailableInvocationUsage(
				for: content.invocationUsage,
				provider: provider
			)
			let todayUsage: Codexpulse_Core_V1_UsageCostResponse
			switch results.2 {
			case .value(let response): todayUsage = response
			case .failure: todayUsage = unavailableUsage(for: todayContent.usage, provider: provider)
			}
			let todayInvocation = unavailableInvocationUsage(
				for: todayContent.invocationUsage,
				provider: provider
			)
			let projects: Codexpulse_Core_V1_ProjectListResponse
			switch results.3 {
			case .value(let response): projects = response
			case .failure: projects = unavailableProjects(provider: provider)
			}
			let quotaPace: Codexpulse_Core_V1_QuotaPaceResponse
			switch results.0 {
			case .value(let response): quotaPace = response
			case .failure: quotaPace = unavailableQuotaPace(at: evaluatedAt, provider: provider)
			}
			guard usage.providerContext.effectiveProvider == provider.rawValue,
				quota.providerContext.effectiveProvider == provider.rawValue,
				quotaPace.providerContext.effectiveProvider == provider.rawValue,
				invocation.providerContext.effectiveProvider == provider.rawValue,
				todayUsage.providerContext.effectiveProvider == provider.rawValue,
				todayInvocation.providerContext.effectiveProvider == provider.rawValue,
				projects.providerContext.effectiveProvider == provider.rawValue
			else { throw AppRuntimeError.providerMismatch }

			return OverviewResponses(
				provider: provider,
				usage: usage,
				quota: quota,
				quotaPace: quotaPace,
				account: nil,
				sessions: unavailableSessions(provider: provider),
				projects: projects,
				health: unavailableHealth(),
				rangeResolution: range,
				todayUsage: todayUsage,
				weeklyUsage: usage,
				invocationUsage: invocation,
				todayInvocationUsage: todayInvocation,
				weeklyProjects: projects,
				weeklyProjectRange: range,
				additionalNotices: [
					quotaResult.notice,
					results.0.notice,
					results.1.notice,
					results.2.notice,
					results.3.notice,
				].compactMap { $0 }
			)
		}
	}

	public func accountSnapshot(provider: AgentProvider) async throws -> Codexpulse_Core_V1_AccountSnapshotResponse {
		var request = Codexpulse_Core_V1_AccountSnapshotRequest()
		request.provider.provider = provider.rawValue
		let preparedRequest = request
		return try await performRead { try await $0.accountSnapshot(preparedRequest, retryPolicy: .none) }
	}

    public func pricingCatalogCurrent(
		_ request: Codexpulse_Core_V1_PricingCatalogCurrentRequest
    ) async throws -> Codexpulse_Core_V1_PricingCatalogCurrentResponse {
        try await performRead {
			try await $0.pricingCatalogCurrent(request, retryPolicy: .transportDefault)
        }
    }

    public func quotaCurrent(
        _ request: Codexpulse_Core_V1_QuotaCurrentRequest
    ) async throws -> Codexpulse_Core_V1_QuotaCurrentResponse {
        try await performRead { try await $0.quotaCurrent(request, retryPolicy: .transportDefault) }
    }

    public func apiSubscriptionsCurrent(
        now: Date = Date()
    ) async throws -> Codexpulse_Core_V1_APISubscriptionsCurrentResponse {
        var request = Codexpulse_Core_V1_APISubscriptionsCurrentRequest()
        request.evaluatedAtMs = Int64(now.timeIntervalSince1970 * 1_000)
        let preparedRequest = request
        return try await performRead {
            try await $0.apiSubscriptionsCurrent(preparedRequest, retryPolicy: .transportDefault)
        }
    }

    public func apiCredentialStatus() async throws -> APISubscriptionCredentialStatus {
        let response = try await performRead {
            try await $0.apiCredentialStatus(
                Codexpulse_Core_V1_APICredentialStatusRequest(),
                retryPolicy: .transportDefault
            )
        }
        return APISubscriptionCredentialStatus(response)
    }

    public func updateAPICredential(
        service: APISubscriptionCredentialService,
        key: String?
    ) async throws -> APISubscriptionCredentialStatus {
        var request = Codexpulse_Core_V1_UpdateAPICredentialRequest()
        request.service = service.rawValue
        if let key {
            request.secret = Data(key.trimmingCharacters(in: .whitespacesAndNewlines).utf8)
        } else {
            request.delete = true
        }
        let preparedRequest = request
        let response = try await performMutation {
            try await $0.updateAPICredential(preparedRequest)
        }
        await restart()
        return APISubscriptionCredentialStatus(response)
    }

    public func quotaPace(
        _ request: Codexpulse_Core_V1_QuotaPaceRequest
    ) async throws -> Codexpulse_Core_V1_QuotaPaceResponse {
        try await performRead { try await $0.quotaPace(request, retryPolicy: .transportDefault) }
    }

    public func requestQuotaRefresh(
        source: String,
		provider: AgentProvider
    ) async throws -> Codexpulse_Core_V1_QuotaRefreshReceipt {
        guard source == "quota" || source == "reset_credits" else {
            throw AppRuntimeError.unavailable
        }
		guard source == "quota" || provider.supportsResetCredits else {
			throw AppRuntimeError.unavailable
		}
        var request = Codexpulse_Core_V1_QuotaRefreshRequest()
        request.source = source
		request.provider = provider.scope
        let preparedRequest = request
        return try await performMutation { try await $0.requestQuotaRefresh(preparedRequest) }
    }

    public func requestProviderRefresh(
        trigger: String
    ) async throws -> Codexpulse_Core_V1_ProviderRefreshReceipt {
        if let existing = providerRefreshTask {
            return try await existing.value
        }
        let task = Task<Codexpulse_Core_V1_ProviderRefreshReceipt, any Error> {
            var request = Codexpulse_Core_V1_ProviderRefreshRequest()
            request.trigger = trigger
            let preparedRequest = request
            return try await self.performMutation { try await $0.requestProviderRefresh(preparedRequest) }
        }
        providerRefreshTask = task
        defer { providerRefreshTask = nil }
        return try await task.value
    }

    public func runRuntimeAction(
        _ action: RuntimeControlAction
    ) async throws -> Codexpulse_Core_V1_RuntimeActionReceipt {
        var request = Codexpulse_Core_V1_RuntimeActionRequest()
        request.action = action.rawValue
        let preparedRequest = request
        return try await performMutation { try await $0.runRuntimeAction(preparedRequest) }
    }

    public func listSessions(
        _ request: Codexpulse_Core_V1_ListSessionsRequest
    ) async throws -> Codexpulse_Core_V1_SessionListResponse {
        try await performRead { try await $0.listSessions(request, retryPolicy: .transportDefault) }
    }

    public func sessionDetail(
        _ request: Codexpulse_Core_V1_SessionDetailRequest
    ) async throws -> Codexpulse_Core_V1_SessionDetailResponse {
        try await performRead { try await $0.sessionDetail(request, retryPolicy: .transportDefault) }
    }

    public func listProjects(
        _ request: Codexpulse_Core_V1_ListProjectsRequest
    ) async throws -> Codexpulse_Core_V1_ProjectListResponse {
        try await performRead { try await $0.listProjects(request, retryPolicy: .transportDefault) }
    }

    public func projectDetail(
        _ request: Codexpulse_Core_V1_ProjectDetailRequest
    ) async throws -> Codexpulse_Core_V1_ProjectDetailResponse {
        try await performRead { try await $0.projectDetail(request, retryPolicy: .transportDefault) }
    }

    public func listSources(
        _ request: Codexpulse_Core_V1_ListSourcesRequest
    ) async throws -> Codexpulse_Core_V1_SourceListResponse {
        try await performRead { try await $0.listSources(request, retryPolicy: .transportDefault) }
    }

    public func source(
        key: String
    ) async throws -> Codexpulse_Core_V1_SourceDetailResponse {
        guard !key.isEmpty else { throw AppRuntimeError.unavailable }
        var request = Codexpulse_Core_V1_SourceRequest()
        request.sourceKey = key
        let preparedRequest = request
        return try await performRead { try await $0.source(preparedRequest, retryPolicy: .transportDefault) }
    }

    public func listJobs(
        _ request: Codexpulse_Core_V1_ListJobsRequest
    ) async throws -> Codexpulse_Core_V1_JobListResponse {
        try await performRead { try await $0.listJobs(request, retryPolicy: .transportDefault) }
    }

    public func job(
        id: String
    ) async throws -> Codexpulse_Core_V1_JobDetailResponse {
        guard !id.isEmpty else { throw AppRuntimeError.unavailable }
        var request = Codexpulse_Core_V1_JobRequest()
        request.jobID = id
        let preparedRequest = request
        return try await performRead { try await $0.job(preparedRequest, retryPolicy: .transportDefault) }
    }

    public func listHealth(
        _ request: Codexpulse_Core_V1_ListHealthRequest
    ) async throws -> Codexpulse_Core_V1_HealthListResponse {
        try await performRead { try await $0.listHealth(request, retryPolicy: .transportDefault) }
    }

    public func health(
        eventID: String
    ) async throws -> Codexpulse_Core_V1_HealthDetailResponse {
        guard !eventID.isEmpty else { throw AppRuntimeError.unavailable }
        var request = Codexpulse_Core_V1_HealthRequest()
        request.eventID = eventID
        let preparedRequest = request
        return try await performRead { try await $0.health(preparedRequest, retryPolicy: .transportDefault) }
    }

    public func healthProjection() async throws -> Codexpulse_Core_V1_HealthProjectionResponse {
        try await performRead { try await $0.healthProjection(retryPolicy: .transportDefault) }
    }

    public func dataHealth(
        _ request: Codexpulse_Core_V1_DataHealthRequest
    ) async throws -> Codexpulse_Core_V1_DataHealthResponse {
        try await performRead { try await $0.dataHealth(request, retryPolicy: .transportDefault) }
    }

    public func settings() async throws -> Codexpulse_Core_V1_SettingsResponse {
        try await performRead { try await $0.settings(retryPolicy: .transportDefault) }
    }

    public func updateSettings(
        _ request: Codexpulse_Core_V1_UpdateSettingsRequest
    ) async throws -> Codexpulse_Core_V1_SettingsUpdateReceipt {
        try await performMutation { try await $0.updateSettings(request) }
    }

    public func primaryPagesSmoke(
        now: Date = Date(),
        calendar: Calendar = .current
    ) async throws -> PrimaryPagesSmokeSummary {
        var step = "usage"
        do {
            var unavailableSteps: [String] = []
            var usage: Codexpulse_Core_V1_UsageCostResponse?
            do {
                usage = try await usageCost(FeatureRequestFactory.usage(range: .sevenDays, now: now, calendar: calendar))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "dashboard_summary"
            var dashboard: Codexpulse_Core_V1_DashboardSummaryResponse?
            do {
                dashboard = try await dashboardSummary(
                    FeatureRequestFactory.dashboardSummary(range: .sevenDays, now: now, calendar: calendar)
                )
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "invocation_usage"
            var invocation: Codexpulse_Core_V1_InvocationUsageResponse?
            do {
                invocation = try await invocationUsage(FeatureRequestFactory.invocationUsage(
                    range: .sevenDays,
                    sourceClass: "all",
                    now: now,
                    calendar: calendar
                ))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "quota"
            var quota: Codexpulse_Core_V1_QuotaCurrentResponse?
            do {
                quota = try await quotaCurrent(FeatureRequestFactory.quota(now: now))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "quota_pace"
            var paceResponse: Codexpulse_Core_V1_QuotaPaceResponse?
            do {
                paceResponse = try await quotaPace(FeatureRequestFactory.quotaPace(now: now))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "api_subscriptions"
            var apiSubscriptions = "unavailable"
            do {
                let response = try await apiSubscriptionsCurrent(now: now)
                apiSubscriptions = "deepseek_\(response.deepSeek.status.state)"
                    + "+opencode_go_\(response.openCodeGo.status.state)"
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "sessions"
            var sessions: Codexpulse_Core_V1_SessionListResponse?
            do {
                sessions = try await listSessions(FeatureRequestFactory.sessions(options: .init(), limit: 20, now: now, calendar: calendar))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "projects"
            var projects: Codexpulse_Core_V1_ProjectListResponse?
            do {
                projects = try await listProjects(FeatureRequestFactory.projects(options: .init(), limit: 20, now: now, calendar: calendar))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "sources"
            var sources: Codexpulse_Core_V1_SourceListResponse?
            do {
                sources = try await listSources(FeatureRequestFactory.sources(options: .init(), limit: 20))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "jobs"
            var jobs: Codexpulse_Core_V1_JobListResponse?
            do {
                jobs = try await listJobs(FeatureRequestFactory.jobs(options: .init(), limit: 20))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "health_events"
            var healthEvents: Codexpulse_Core_V1_HealthListResponse?
            do {
                healthEvents = try await listHealth(FeatureRequestFactory.health(options: .init(), limit: 20))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "health_projection"
            do {
                _ = try await healthProjection()
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "data_health"
            do {
                _ = try await dataHealth(FeatureRequestFactory.dataHealth(now: now))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }

            var detailsRead = 0
            var projectDetailCostKnown = false
            var projectDetailModels = 0
            if let item = sessions?.items.first {
                step = "session_detail"
                do {
                    _ = try await sessionDetail(FeatureRequestFactory.sessionDetail(sessionID: item.sessionID))
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }
            if let item = projects?.items.first {
                step = "project_detail"
                do {
                    let detail = try await projectDetail(FeatureRequestFactory.projectDetail(
                        dimensionKey: item.dimensionKey,
                        range: .thirtyDays,
                        now: now,
                        calendar: calendar
                    ))
                    projectDetailCostKnown = detail.item.totals.estimatedUsdMicros.hasValue
                    projectDetailModels = detail.models.count
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }
            if let item = sources?.items.first {
                step = "source_detail"
                do {
                    _ = try await source(key: item.sourceKey)
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }
            if let item = jobs?.items.first {
                step = "job_detail"
                do {
                    _ = try await job(id: item.jobID)
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }
            if let item = healthEvents?.items.first {
                step = "health_detail"
                do {
                    _ = try await health(eventID: item.eventID)
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }

            step = "settings"
            return PrimaryPagesSmokeSummary(
                sessions: sessions?.items.count ?? 0,
                projects: projects?.items.count ?? 0,
                sources: sources?.items.count ?? 0,
                jobs: jobs?.items.count ?? 0,
                healthEvents: healthEvents?.items.count ?? 0,
                usageTrend: usage?.trend.count ?? 0,
                usageModels: usage?.models.count ?? 0,
                usageModelTrend: usage?.models.reduce(0) { $0 + $1.trend.count } ?? 0,
                usageModelReconciled: usage.map {
                    UsageModelTrendResolver.buckets($0).count(where: \.breakdownAvailable)
                } ?? 0,
                usageCostKnown: usage?.totals.estimatedUsdMicros.hasValue == true,
                dashboardProviders: dashboard?.providers.count ?? 0,
                dashboardKnownProviders: dashboard?.coverage.knownProviderCount ?? 0,
                dashboardTotalTokens: dashboard?.totals.totalTokens.hasValue == true
                    ? dashboard?.totals.totalTokens.value
                    : nil,
                invocationToolCalls: invocation?.totals.toolCallCount.value ?? 0,
                invocationSkillActivity: invocation?.totals.skillActivityCount.value ?? 0,
                quotaWindows: quota?.current.windows.count ?? 0,
                quotaPaceWindows: paceResponse?.pace.windows.count ?? 0,
                apiSubscriptions: apiSubscriptions,
                projectDetailCostKnown: projectDetailCostKnown,
                projectDetailModels: projectDetailModels,
                detailsRead: detailsRead,
                settingsMutation: try await settingsMutationSmoke(),
                unavailableSteps: unavailableSteps
            )
        } catch let error as PrimaryPagesSmokeError {
            throw error
        } catch {
            throw PrimaryPagesSmokeError(step: step)
        }
    }

    private func acceptedSmokeFailure(step: String, error: any Error) throws -> String {
        let notice = AppNotice.from(error)
        guard ["not_found", "partial", "unavailable", "deadline_exceeded"].contains(notice.code) else {
            throw PrimaryPagesSmokeError(step: step)
        }
        return "\(step)_\(notice.code)"
    }

    private func settingsMutationSmoke() async throws -> String {
        var step = "settings_read"
        do {
            let original = try await settings()
            let originalDraft = SettingsDraft(original)
            var changedDraft = originalDraft
            let editable = original.editableFields.filter(\.editable)
            var hasChange = false

            if let field = editable.first(where: { $0.key == "ui.launchBehavior" }),
               let alternate = field.options.first(where: { $0 != originalDraft.launchBehavior }) {
                changedDraft.launchBehavior = alternate
                hasChange = true
            } else if let field = editable.first(where: { $0.key == "ui.overviewRange" }),
                      let alternate = field.options.first(where: { $0 != originalDraft.overviewRange }) {
                changedDraft.overviewRange = alternate
                hasChange = true
            } else if editable.contains(where: { $0.key == "online.quotaEnabled" }) {
                changedDraft.quotaEnabled.toggle()
                hasChange = true
            }

            guard hasChange else { return "readback_only" }
            step = "settings_apply"
            let appliedReceipt = try await updateSettings(changedDraft.makeRequest(authoritative: original))
            step = "settings_apply_readback"
            var authoritative = try await settings()
            guard authoritative.snapshot.revision == appliedReceipt.revision,
                  SettingsDraft(authoritative) == changedDraft
            else {
                try? await restoreSettings(originalDraft, authoritative: authoritative)
                throw AppRuntimeError.invalidBootstrap
            }

            step = "settings_conflict"
            var conflictObserved = false
            do {
                _ = try await updateSettings(originalDraft.makeRequest(authoritative: original))
                authoritative = try await settings()
            } catch {
                conflictObserved = true
                step = "settings_conflict_readback"
                authoritative = try await settings()
            }

            step = "settings_restore"
            try await restoreSettings(originalDraft, authoritative: authoritative)
            step = "settings_restore_readback"
            let restored = try await settings()
            guard SettingsDraft(restored) == originalDraft, conflictObserved else {
                throw AppRuntimeError.invalidBootstrap
            }
            return "receipt+readback+conflict+restored"
        } catch let error as PrimaryPagesSmokeError {
            throw error
        } catch {
            throw PrimaryPagesSmokeError(step: step)
        }
    }

    private func restoreSettings(
        _ original: SettingsDraft,
        authoritative: Codexpulse_Core_V1_SettingsResponse
    ) async throws {
        let receipt = try await updateSettings(original.makeRequest(authoritative: authoritative))
        let readback = try await settings()
        guard readback.snapshot.revision == receipt.revision, SettingsDraft(readback) == original else {
            throw AppRuntimeError.invalidBootstrap
        }
    }

    public func cancelRefresh() async {
        refreshGeneration &+= 1
        refreshAdmissionGeneration = nil
        invalidationRefreshPending = false
        restorePendingInitialOverviewAfterCancellation()
        refreshTask?.cancel()
        refreshTask = nil
        cancelAccountRefresh()
        await emit(.cancelled)
    }

    private func performRead<Value: Sendable>(
        _ operation: @Sendable (any AppCoreServing) async throws -> Value
    ) async throws -> Value {
        guard readyForOverview, !shuttingDown, let client else {
            throw AppRuntimeError.unavailable
        }
        let generation = runtimeGeneration
        try Task.checkCancellation()
        let value = try await operation(client)
        try Task.checkCancellation()
        guard generation == runtimeGeneration, readyForOverview, !shuttingDown else {
            throw CancellationError()
        }
        return value
    }

    private func performMutation<Value: Sendable>(
        _ operation: @Sendable (any AppCoreServing) async throws -> Value
    ) async throws -> Value {
        guard readyForOverview, !shuttingDown, let client else {
            throw AppRuntimeError.unavailable
        }
        let generation = runtimeGeneration
        try Task.checkCancellation()
        let value = try await operation(client)
        try Task.checkCancellation()
        guard generation == runtimeGeneration, readyForOverview, !shuttingDown else {
            throw CancellationError()
        }
        return value
    }

    public func applicationWillResignActive() {
        applicationIsActive = false
        activeRefreshPending = false
    }

    public func applicationDidBecomeActive() async {
        applicationIsActive = true
        guard !shuttingDown, !systemIsSleeping else {
            activeRefreshPending = true
            return
        }
        guard readyForOverview, let client else {
            activeRefreshPending = true
            return
        }
        let generation = runtimeGeneration
        await notifyActiveAndRefresh(client: client, generation: generation)
    }

    public func prepareForSleep() async {
        systemIsSleeping = true
        sleepTransitionInFlight = true
        refreshGeneration &+= 1
        refreshAdmissionGeneration = nil
        invalidationRefreshPending = false
        restorePendingInitialOverviewAfterCancellation()
        refreshTask?.cancel()
        refreshTask = nil
        cancelAccountRefresh()
        let generation = runtimeGeneration
        guard let streamController else {
            if readyForOverview, let client {
                await suspendWithoutStream(client: client, generation: generation)
            }
            await finishSleepTransition(generation: generation)
            return
        }
        do {
            try await streamController.prepareForSleep(sendLifecycle: sendLifecycleToHelper)
            guard generation == runtimeGeneration, !shuttingDown else { return }
            sleepLifecycleDelivered = sendLifecycleToHelper
            await finishSleepTransition(generation: generation)
        } catch {
            guard generation == runtimeGeneration, !shuttingDown else { return }
            sleepTransitionInFlight = false
            if wakeAfterSleepPending {
                wakeAfterSleepPending = false
                systemIsSleeping = false
            }
            await emitRefreshFailure(error)
        }
    }

    public func resumeAfterWake() async {
        guard !shuttingDown, systemIsSleeping else { return }
        if sleepTransitionInFlight {
            wakeAfterSleepPending = true
            return
        }
        await resumeAfterWakeNow()
    }

    private func resumeAfterWakeNow() async {
        systemIsSleeping = false
        let generation = runtimeGeneration
        guard let streamController else {
            guard readyForOverview, let client else {
                sleepLifecycleDelivered = false
                return
            }
            do {
                if sendLifecycleToHelper, sleepLifecycleDelivered {
                    _ = try await client.notifyLifecycle(.systemDidWake)
                    guard generation == runtimeGeneration, !shuttingDown else { return }
                }
                sleepLifecycleDelivered = false
                let streamOutcome = try await startInvalidationStream(
                    client: client,
                    generation: generation
                )
                guard streamOutcome == .ready, !systemIsSleeping else { return }
                guard generation == runtimeGeneration, !shuttingDown else { return }
                await refreshAfterStreamReady(showLoading: lastResponses == nil)
                guard generation == runtimeGeneration, !shuttingDown else { return }
                await deliverPendingActive(client: client, generation: generation)
            } catch {
                guard generation == runtimeGeneration, !shuttingDown else { return }
                await emitRefreshFailure(error)
            }
            return
        }
        do {
            suppressNextStreamReadyRefresh = true
            try await streamController.resumeAfterWake(sendLifecycle: sendLifecycleToHelper)
            guard generation == runtimeGeneration, !shuttingDown else { return }
            try await streamController.waitUntilReady()
            guard generation == runtimeGeneration, !shuttingDown else { return }
            sleepLifecycleDelivered = false
            await refreshAfterStreamReady(showLoading: lastResponses == nil)
            guard generation == runtimeGeneration, !shuttingDown, let client else { return }
            await deliverPendingActive(client: client, generation: generation)
        } catch {
            guard generation == runtimeGeneration, !shuttingDown else { return }
            suppressNextStreamReadyRefresh = false
            await emitRefreshFailure(error)
        }
    }

    public func retryRecovery() async {
        guard !shuttingDown, let client else {
            await emit(.unavailable(AppNotice(
                code: "core_unavailable",
                messageKey: "app.error.core_unavailable",
                retryable: true
            )))
            return
        }
        let generation = runtimeGeneration
        do {
            let receipt = try await client.migrationRecoveryRetry()
            guard generation == runtimeGeneration, !shuttingDown else { return }
            if RecoveryTransition(receipt) == .restartRequired {
                await emit(.restartRequired)
                return
            }
            let bootstrap = try await client.bootstrap(retryPolicy: .transportDefault)
            guard generation == runtimeGeneration, !shuttingDown else { return }
            switch BootstrapState(bootstrap) {
            case .recovery(let snapshot):
                readyForOverview = false
                await emit(.recovery(snapshot))
            case .normal:
                readyForOverview = true
                requestInitialOverviewRefresh()
                if systemIsSleeping {
                    await suspendWithoutStream(client: client, generation: generation)
                    return
                }
                let streamOutcome = try await startInvalidationStream(
                    client: client,
                    generation: generation
                )
                guard streamOutcome == .ready, !systemIsSleeping else { return }
                guard generation == runtimeGeneration, !shuttingDown else { return }
                await drainInitialOverviewRefresh(showLoading: true)
                guard generation == runtimeGeneration, !shuttingDown else { return }
                if systemIsSleeping {
                    await suspendWithoutStream(client: client, generation: generation)
                    return
                }
                await deliverPendingActive(client: client, generation: generation)
            case .unsupported: throw AppRuntimeError.invalidBootstrap
            }
        } catch {
            guard generation == runtimeGeneration, !shuttingDown else { return }
            await emitRefreshFailure(error)
        }
    }

    public func restart() async {
        guard !startInFlight, !shuttingDown else { return }
        runtimeGeneration &+= 1
        _ = await stopCurrentCore(reason: "client_restart")
        await start()
    }

    public func shutdown(
        reason: AppShutdownReason = .applicationExit
    ) async -> ShutdownOutcome {
        guard !shuttingDown else { return .uncertain }
        shuttingDown = true
        runtimeGeneration &+= 1
        readyForOverview = false
        activeRefreshPending = false
        sleepTransitionInFlight = false
        wakeAfterSleepPending = false
        sleepLifecycleDelivered = false
        refreshGeneration &+= 1
        refreshAdmissionGeneration = nil
        invalidationRefreshPending = false
        refreshTask?.cancel()
        refreshTask = nil
        cancelAccountRefresh()
        await emit(.shuttingDown)
        let outcome = await stopCurrentCore(reason: reason.coreValue)
        shuttingDown = false
        await emit(.stopped)
        return outcome
    }

    private func refresh(showLoading: Bool) async {
        guard readyForOverview, !systemIsSleeping, !shuttingDown, let client else { return }
        if let refreshTask {
            _ = try? await refreshTask.value
            return
        }
        guard refreshAdmissionGeneration == nil else { return }
        cancelAccountRefresh()
        refreshGeneration &+= 1
        let generation = refreshGeneration
        let admittedRuntimeGeneration = runtimeGeneration
        refreshAdmissionGeneration = generation
        if showLoading || lastResponses == nil { await emit(.loadingOverview) }
        guard refreshAdmissionGeneration == generation,
              generation == refreshGeneration,
              admittedRuntimeGeneration == runtimeGeneration,
              readyForOverview,
              !systemIsSleeping,
              !shuttingDown,
              self.client != nil
        else {
            if refreshAdmissionGeneration == generation {
                refreshAdmissionGeneration = nil
            }
            return
        }
		let provider = selectedProvider
		let requests = OverviewRequestSet.make(provider: provider)
        let requestedRange = overviewRange
        let previousAccount = lastResponses?.account
        if initialOverviewRefreshState == .pending {
            initialOverviewRefreshState = .inFlight
        }
		let task = Task<OverviewResponses, any Error> {
			let quotaResult = await captureOverviewSection {
				return try await client.quotaCurrent(requests.quota, retryPolicy: .transportDefault)
			}
            let quotaResponse: Codexpulse_Core_V1_QuotaCurrentResponse
            switch quotaResult {
            case .value(let response): quotaResponse = response
            case .failure: quotaResponse = unavailableQuota(at: Date(), provider: provider)
            }
            let quotaNow = quotaResponse.current.evaluatedAtMs > 0
                ? Date(timeIntervalSince1970: TimeInterval(quotaResponse.current.evaluatedAtMs) / 1_000)
                : Date()
			let rangeNow = [.quotaWeek, .quotaMonth].contains(requestedRange) ? quotaNow : Date()
            let range = OverviewRequestSet.resolveRange(
                requestedRange, quota: quotaResponse, now: rangeNow)
            let weeklyProjectRange = OverviewRequestSet.resolveRange(
                .quotaWeek, quota: quotaResponse, now: quotaNow)
			let content = OverviewRequestSet.content(range: range, provider: provider)
            let sharesWeeklyUsage = !weeklyProjectRange.fellBackFromQuotaWeek
                && range.startAtMS == weeklyProjectRange.startAtMS
                && range.endAtMS == weeklyProjectRange.endAtMS
                && range.timeZone == weeklyProjectRange.timeZone
                && range.granularity == weeklyProjectRange.granularity
			let weeklyUsageRequest = sharesWeeklyUsage || provider == .cursor
				? nil
				: OverviewRequestSet.weeklyUsageRequest(quota: quotaResponse, provider: provider)
			let weeklyProjectRequest = OverviewRequestSet.weeklyProjectRanking(
				range: weeklyProjectRange, provider: provider)
			let tokenActivityRequest = OverviewRequestSet.tokenActivityRequest(provider: provider)
			let todayRange = OverviewRequestSet.resolveRange(.today, quota: quotaResponse, now: rangeNow)
			let todayContent = OverviewRequestSet.content(range: todayRange, provider: provider)
			async let quotaPaceResult = captureOverviewSection {
				return try await client.quotaPace(
                    requests.quotaPace, retryPolicy: .transportDefault)
            }
            async let usageResult = captureOverviewSection {
                try await client.usageCost(content.usage, retryPolicy: .transportDefault)
            }
            async let sessionResult = captureOverviewSection {
                try await client.listSessions(content.sessions, retryPolicy: .transportDefault)
            }
            async let projectResult = captureOverviewSection {
                try await client.listProjects(content.projects, retryPolicy: .transportDefault)
            }
            async let weeklyProjectResult = captureOverviewSection {
                guard let weeklyProjectRequest else {
                    return unavailableProjects(provider: provider)
                }
                return try await client.listProjects(
                    weeklyProjectRequest, retryPolicy: .transportDefault)
            }
            async let weeklyUsageResult = captureOverviewSection {
                guard let weeklyUsageRequest else {
                    return unavailableUsage(provider: provider)
                }
                return try await client.usageCost(
                    weeklyUsageRequest, retryPolicy: .transportDefault)
            }
            async let healthResult = captureOverviewSection {
                try await client.healthProjection(retryPolicy: .transportDefault)
            }
            async let tokenActivityResult = captureOverviewSection {
                try await client.usageCost(
                    tokenActivityRequest, retryPolicy: .transportDefault)
            }
            async let invocationUsageResult = captureOverviewSection {
				guard provider.supportsInvocationStatistics else {
					return unavailableInvocationUsage(
						for: content.invocationUsage,
						provider: provider
					)
				}
				return try await client.invocationUsage(
                    content.invocationUsage, retryPolicy: .transportDefault)
            }
			async let todayUsageResult = captureOverviewSection {
				guard provider == .cursor else {
					return unavailableUsage(for: todayContent.usage, provider: provider)
				}
				return try await client.usageCost(
					todayContent.usage, retryPolicy: .transportDefault)
			}
			async let todayInvocationResult = captureOverviewSection {
				guard provider == .cursor, provider.supportsInvocationStatistics else {
					return unavailableInvocationUsage(
						for: todayContent.invocationUsage, provider: provider)
				}
				return try await client.invocationUsage(
					todayContent.invocationUsage, retryPolicy: .transportDefault)
			}
            let sectionResults = await (
                quotaPaceResult,
                usageResult, sessionResult, projectResult, weeklyProjectResult,
                weeklyUsageResult, healthResult, tokenActivityResult, invocationUsageResult)
			let cursorTodayResults = await (todayUsageResult, todayInvocationResult)
			let mandatoryNotices = [
				provider == .codex ? quotaResult.notice : nil,
                sectionResults.1.notice,
                sectionResults.2.notice,
                sectionResults.3.notice,
                sectionResults.6.notice,
            ].compactMap { $0 }
            guard mandatoryNotices.count < 5 else { throw AppRuntimeError.unavailable }
            let notices = mandatoryNotices

            let usageResponse: Codexpulse_Core_V1_UsageCostResponse
            switch sectionResults.1 {
            case .value(let response): usageResponse = response
            case .failure: usageResponse = unavailableUsage(provider: provider)
            }
            let sessionResponse: Codexpulse_Core_V1_SessionListResponse
            switch sectionResults.2 {
            case .value(let response): sessionResponse = response
            case .failure: sessionResponse = unavailableSessions(provider: provider)
            }
            let projectResponse: Codexpulse_Core_V1_ProjectListResponse
            switch sectionResults.3 {
            case .value(let response): projectResponse = response
            case .failure: projectResponse = unavailableProjects(provider: provider)
            }
            let weeklyProjectResponse: Codexpulse_Core_V1_ProjectListResponse
            switch sectionResults.4 {
            case .value(let response): weeklyProjectResponse = response
            case .failure: weeklyProjectResponse = unavailableProjects(provider: provider)
            }
            let weeklyUsageResponse: Codexpulse_Core_V1_UsageCostResponse
			if sharesWeeklyUsage || provider == .cursor {
                weeklyUsageResponse = usageResponse
            } else {
                switch sectionResults.5 {
                case .value(let response): weeklyUsageResponse = response
                case .failure: weeklyUsageResponse = unavailableUsage(provider: provider)
                }
            }
            let healthResponse: Codexpulse_Core_V1_HealthProjectionResponse
            switch sectionResults.6 {
            case .value(let response): healthResponse = response
            case .failure: healthResponse = unavailableHealth()
            }
            let tokenActivityResponse: Codexpulse_Core_V1_UsageCostResponse
            switch sectionResults.7 {
            case .value(let response): tokenActivityResponse = response
            case .failure:
                tokenActivityResponse = unavailableUsage(
                    for: tokenActivityRequest,
                    provider: provider
                )
            }
            let quotaPaceResponse: Codexpulse_Core_V1_QuotaPaceResponse
            switch sectionResults.0 {
            case .value(let response): quotaPaceResponse = response
            case .failure: quotaPaceResponse = unavailableQuotaPace(at: quotaNow, provider: provider)
            }
            let invocationUsageResponse: Codexpulse_Core_V1_InvocationUsageResponse
            switch sectionResults.8 {
            case .value(let response): invocationUsageResponse = response
            case .failure:
                invocationUsageResponse = unavailableInvocationUsage(
                    for: content.invocationUsage,
                    provider: provider
                )
            }
			let todayUsageResponse: Codexpulse_Core_V1_UsageCostResponse
			switch cursorTodayResults.0 {
			case .value(let response): todayUsageResponse = response
			case .failure:
				todayUsageResponse = unavailableUsage(
					for: todayContent.usage,
					provider: provider
				)
			}
			let todayInvocationResponse: Codexpulse_Core_V1_InvocationUsageResponse
			switch cursorTodayResults.1 {
			case .value(let response): todayInvocationResponse = response
			case .failure:
				todayInvocationResponse = unavailableInvocationUsage(
					for: todayContent.invocationUsage,
					provider: provider
				)
			}
			return OverviewResponses(
				provider: provider,
                usage: usageResponse,
                quota: quotaResponse,
                quotaPace: quotaPaceResponse,
                account: previousAccount,
                sessions: sessionResponse,
                projects: projectResponse,
                health: healthResponse,
                rangeResolution: range,
				todayUsage: todayUsageResponse,
                weeklyUsage: weeklyUsageResponse,
                tokenActivityUsage: tokenActivityResponse,
                invocationUsage: invocationUsageResponse,
				todayInvocationUsage: todayInvocationResponse,
                weeklyProjects: weeklyProjectResponse,
                weeklyProjectRange: weeklyProjectRange,
                additionalNotices: notices
            )
        }
        refreshTask = task
        refreshAdmissionGeneration = nil
        do {
            let responses = try await task.value
			guard generation == refreshGeneration, refreshTask != nil, !shuttingDown,
				responses.provider == selectedProvider,
				responses.usage.providerContext.effectiveProvider == selectedProvider.rawValue,
				responses.sessions.providerContext.effectiveProvider == selectedProvider.rawValue,
				responses.projects.providerContext.effectiveProvider == selectedProvider.rawValue,
				responses.invocationUsage.providerContext.effectiveProvider == selectedProvider.rawValue,
				responses.todayUsage.providerContext.effectiveProvider == selectedProvider.rawValue,
				responses.todayInvocationUsage.providerContext.effectiveProvider == selectedProvider.rawValue,
				responses.quota.providerContext.effectiveProvider == selectedProvider.rawValue,
				responses.quotaPace.providerContext.effectiveProvider == selectedProvider.rawValue
			else { throw AppRuntimeError.providerMismatch }
            refreshTask = nil
            lastResponses = responses
			overviewCache[OverviewCacheKey(provider: provider, range: requestedRange)] = responses
            await publishOverview(responses)
            completeInitialOverviewRefreshIfNeeded()
            await drainPendingInvalidationRefresh()
            guard generation == refreshGeneration, !shuttingDown else { return }
            // Account data is optional and has its own bounded RPC/task lifecycle,
            // so it cannot delay the primary Overview publication above.
			startAccountRefresh(
				client: client,
				provider: selectedProvider,
				runtimeGeneration: runtimeGeneration,
				overviewGeneration: generation
			)
        } catch is CancellationError {
            guard generation == refreshGeneration else { return }
            refreshTask = nil
            restorePendingInitialOverviewAfterCancellation()
            await emit(.cancelled)
            await drainPendingInvalidationRefresh()
        } catch {
            guard generation == refreshGeneration else { return }
            refreshTask = nil
            await emitRefreshFailure(error)
            completeInitialOverviewRefreshIfNeeded()
            await drainPendingInvalidationRefresh()
        }
    }

    private func startAccountRefresh(
        client: any AppCoreServing,
		provider: AgentProvider,
        runtimeGeneration: UInt64,
        overviewGeneration: UInt64
    ) {
        guard !shuttingDown, readyForOverview,
              runtimeGeneration == self.runtimeGeneration,
              overviewGeneration == refreshGeneration
        else { return }
        cancelAccountRefresh()
        let generation = accountRefreshGeneration
        accountRefreshTask = Task { [weak self] in
            do {
				var request = Codexpulse_Core_V1_AccountSnapshotRequest()
				request.provider.provider = provider.rawValue
				let response = try await client.accountSnapshot(request, retryPolicy: .none)
                try Task.checkCancellation()
                await self?.finishAccountRefresh(
                    response,
                    generation: generation,
                    runtimeGeneration: runtimeGeneration,
                    overviewGeneration: overviewGeneration
                )
            } catch {
                await self?.finishAccountRefresh(
                    nil,
                    generation: generation,
                    runtimeGeneration: runtimeGeneration,
                    overviewGeneration: overviewGeneration
                )
            }
        }
    }

    private func finishAccountRefresh(
        _ response: Codexpulse_Core_V1_AccountSnapshotResponse?,
        generation: UInt64,
        runtimeGeneration: UInt64,
        overviewGeneration: UInt64
    ) async {
        guard generation == accountRefreshGeneration,
              runtimeGeneration == self.runtimeGeneration,
              overviewGeneration == refreshGeneration,
              !shuttingDown,
              readyForOverview
        else { return }
        accountRefreshTask = nil
        guard let response, let responses = lastResponses else { return }
        let updated = responses.replacingAccount(response)
        lastResponses = updated
		let requestedRange = updated.rangeResolution?.requestedPreset ?? overviewRange
		overviewCache[OverviewCacheKey(provider: updated.provider, range: requestedRange)] = updated
        await publishOverview(updated)
    }

    private func cancelAccountRefresh() {
        accountRefreshGeneration &+= 1
        accountRefreshTask?.cancel()
        accountRefreshTask = nil
    }

    private func publishOverview(_ responses: OverviewResponses) async {
        let presentation = OverviewPresentation(responses)
        if presentation.isPartial {
            await emit(.partial(responses, presentation.notices))
        } else {
            await emit(.normal(responses))
        }
    }

    private func startInvalidationStream(
        client: any AppCoreServing,
        generation: UInt64
    ) async throws -> InvalidationStreamStartOutcome {
        guard streamController == nil, generation == runtimeGeneration else { return .ready }
        let runtime = self
        streamHasReachedReady = false
        suppressNextStreamReadyRefresh = false
        let controller = InvalidationStreamController(
            domains: ["index", "quota", "health", "settings"],
            consumeInvalidations: { domains, afterSequence, onReady, onEvent in
                try await client.consumeInvalidations(
                    domains: domains,
                    afterSequence: afterSequence,
                    onReady: onReady,
                    onEvent: onEvent
                )
            },
            notifyLifecycle: { event in
                _ = try await client.notifyLifecycle(event)
            },
            onReady: {
                await runtime.handleInvalidationStreamReady(generation: generation)
            },
            onTerminalFailure: {
                await runtime.handleRuntimeFailure(
                    generation: generation,
                    streamIsAlreadyTerminal: true,
                    code: "invalidation_stream_failed"
                )
            },
            onEvent: { event in await runtime.handleInvalidation(domain: event.domain) }
        )
        streamController = controller
        await controller.start()
        do {
            try await controller.waitUntilReady()
            return .ready
        } catch InvalidationStreamError.suspendedForSleep {
            guard generation == runtimeGeneration,
                  systemIsSleeping,
                  streamController != nil
            else {
                if generation == runtimeGeneration, streamController != nil {
                    streamController = nil
                    streamHasReachedReady = false
                    suppressNextStreamReadyRefresh = false
                    await controller.stop()
                }
                throw InvalidationStreamError.suspendedForSleep
            }
            return .sleeping
        } catch {
            if generation == runtimeGeneration, streamController != nil {
                streamController = nil
                streamHasReachedReady = false
                suppressNextStreamReadyRefresh = false
                await controller.stop()
            }
            throw error
        }
    }

    private func handleInvalidationStreamReady(generation: UInt64) async {
        guard generation == runtimeGeneration, streamController != nil, !shuttingDown else { return }
        let isReconnect = streamHasReachedReady
        streamHasReachedReady = true
        if suppressNextStreamReadyRefresh {
            suppressNextStreamReadyRefresh = false
            return
        }
        guard isReconnect, readyForOverview, !systemIsSleeping, client != nil else { return }
        switch initialOverviewRefreshState {
        case .idle, .pending:
            requestInitialOverviewRefresh()
            return
        case .inFlight:
            invalidationRefreshPending = true
            return
        case .completed:
            break
        }
        if refreshTask != nil {
            invalidationRefreshPending = true
            return
        }
        await refresh(showLoading: false)
    }

    private func handleInvalidation(domain: String) async {
        await invalidationSink(domain)
        guard !systemIsSleeping else { return }
        guard domain != "settings" else { return }
        if refreshTask != nil {
            invalidationRefreshPending = true
            return
        }
        if domain == "health", lastResponses != nil {
            await refreshOverviewHealth()
            return
        }
        await refresh(showLoading: false)
    }

    // Health/job notifications do not change usage, project rankings or the annual activity.
    private func refreshOverviewHealth() async {
        guard readyForOverview, !shuttingDown, let client else { return }
        let admittedRuntime = runtimeGeneration
        let admittedRefresh = refreshGeneration
        let result = await captureOverviewSection {
            try await client.healthProjection(retryPolicy: .transportDefault)
        }
        guard admittedRuntime == runtimeGeneration,
              admittedRefresh == refreshGeneration,
              !systemIsSleeping, !shuttingDown,
              let responses = lastResponses
        else { return }
        let health: Codexpulse_Core_V1_HealthProjectionResponse
        switch result {
        case .value(let response): health = response
        case .failure: health = unavailableHealth()
        }
        guard health != responses.health else { return }
        let updated = responses.replacingHealth(health)
        lastResponses = updated
        let range = updated.rangeResolution?.requestedPreset ?? overviewRange
        overviewCache[OverviewCacheKey(provider: updated.provider, range: range)] = updated
        await publishOverview(updated)
    }

    private func drainPendingInvalidationRefresh() async {
        guard invalidationRefreshPending else { return }
        invalidationRefreshPending = false
        guard readyForOverview, !systemIsSleeping, !shuttingDown, client != nil else { return }
        await refresh(showLoading: false)
    }

    private func requestInitialOverviewRefresh() {
        if initialOverviewRefreshState == .idle {
            initialOverviewRefreshState = .pending
        }
    }

    private func refreshAfterStreamReady(showLoading: Bool) async {
        if initialOverviewRefreshState == .completed {
            await refresh(showLoading: showLoading)
            return
        }
        await drainInitialOverviewRefresh(showLoading: showLoading)
    }

    private func drainInitialOverviewRefresh(showLoading: Bool) async {
        requestInitialOverviewRefresh()
        guard initialOverviewRefreshState == .pending else { return }
        await refresh(showLoading: showLoading)
    }

    private func completeInitialOverviewRefreshIfNeeded() {
        guard initialOverviewRefreshState == .inFlight else { return }
        initialOverviewRefreshState = .completed
    }

    private func restorePendingInitialOverviewAfterCancellation() {
        guard initialOverviewRefreshState == .inFlight else { return }
        initialOverviewRefreshState = .pending
    }

    private func finishSleepTransition(generation: UInt64) async {
        guard generation == runtimeGeneration, !shuttingDown else { return }
        sleepTransitionInFlight = false
        guard wakeAfterSleepPending else { return }
        wakeAfterSleepPending = false
        await resumeAfterWakeNow()
    }

    private func notifyActiveAndRefresh(
        client: any AppCoreServing,
        generation: UInt64
    ) async {
        if sendLifecycleToHelper {
            do {
                _ = try await client.notifyLifecycle(.applicationDidBecomeActive)
                guard generation == runtimeGeneration, !shuttingDown, readyForOverview else { return }
            } catch {
                guard generation == runtimeGeneration, !shuttingDown else { return }
                await emitRefreshFailure(error)
                return
            }
        }
        await invalidationSink("lifecycle")
        if lastResponses == nil {
            await refresh(showLoading: true)
        }
    }

    private func deliverPendingActive(
        client: any AppCoreServing,
        generation: UInt64
    ) async {
        guard activeRefreshPending, applicationIsActive, !systemIsSleeping,
              generation == runtimeGeneration, readyForOverview
        else { return }
        activeRefreshPending = false
        await notifyActiveAndRefresh(client: client, generation: generation)
    }

    private func suspendWithoutStream(
        client: any AppCoreServing,
        generation: UInt64
    ) async {
        guard systemIsSleeping, generation == runtimeGeneration, !shuttingDown else { return }
        do {
            if sendLifecycleToHelper, !sleepLifecycleDelivered {
                _ = try await client.notifyLifecycle(.systemWillSleep)
                guard generation == runtimeGeneration, systemIsSleeping, !shuttingDown else { return }
                sleepLifecycleDelivered = true
            }
            await emit(.cancelled)
        } catch {
            guard generation == runtimeGeneration, !shuttingDown else { return }
            await emitRefreshFailure(error)
        }
    }

    private func installHelperProcessMonitor(processID: Int32, generation: UInt64) {
        guard let processMonitorFactory else { return }
        let runtime = self
        helperProcessMonitor?.cancel()
        helperProcessMonitor = processMonitorFactory(processID) {
            Task {
                await runtime.handleRuntimeFailure(
                    generation: generation,
                    streamIsAlreadyTerminal: false,
                    code: "helper_exited"
                )
            }
        }
    }

    private func handleRuntimeFailure(
        generation: UInt64,
        streamIsAlreadyTerminal: Bool,
        code: String
    ) async {
        guard generation == runtimeGeneration, !shuttingDown else { return }
        runtimeGeneration &+= 1
        let failureGeneration = runtimeGeneration
        readyForOverview = false
        activeRefreshPending = applicationIsActive
        sleepTransitionInFlight = false
        wakeAfterSleepPending = false
        sleepLifecycleDelivered = false
        streamHasReachedReady = false
        suppressNextStreamReadyRefresh = false
        refreshGeneration &+= 1
        refreshAdmissionGeneration = nil
        invalidationRefreshPending = false
        initialOverviewRefreshState = .idle
        refreshTask?.cancel()
        refreshTask = nil
        cancelAccountRefresh()
        helperProcessMonitor?.cancel()
        helperProcessMonitor = nil
        if let streamController {
            self.streamController = nil
            if !streamIsAlreadyTerminal { await streamController.stop() }
        }
        if let client {
            self.client = nil
            Task { await client.closeTransport() }
        }
        await supervisor.stop(mode: .terminate)
        guard failureGeneration == runtimeGeneration, !shuttingDown else { return }
        await emitRefreshFailure(AppNotice(
            code: code,
            messageKey: "app.error.core_unavailable",
            retryable: true
        ))
    }

    private func emitRefreshFailure(_ error: any Error) async {
        await emitRefreshFailure(AppNotice.from(error))
    }

    private func emitRefreshFailure(_ notice: AppNotice) async {
        if let lastResponses {
            await emit(.stale(lastResponses, notice))
        } else if notice.code == "cancelled" {
            await emit(.cancelled)
        } else {
            await emit(.unavailable(notice))
        }
    }

    private func stopCurrentCore(reason: String) async -> ShutdownOutcome {
        readyForOverview = false
        sleepTransitionInFlight = false
        wakeAfterSleepPending = false
        sleepLifecycleDelivered = false
        streamHasReachedReady = false
        suppressNextStreamReadyRefresh = false
        helperProcessMonitor?.cancel()
        helperProcessMonitor = nil
        refreshGeneration &+= 1
        refreshAdmissionGeneration = nil
        invalidationRefreshPending = false
        initialOverviewRefreshState = .idle
        refreshTask?.cancel()
        refreshTask = nil
        cancelAccountRefresh()
        if let streamController {
            await streamController.stop()
            self.streamController = nil
        }
        lastResponses = nil
		overviewCache.removeAll()

        guard let client else {
            await supervisor.stop(mode: .terminate)
            return .clean
        }
        self.client = nil
        let shutdownResult = await boundedShutdownRequest(client: client, reason: reason)
        if shutdownResult == .accepted {
            do {
                let status = try await supervisor.waitForExit(timeout: .seconds(10))
                return status == 0 ? .clean : .uncertain
            } catch {
                // Fall through to the forced bounded shutdown path.
            }
        }
        Task { await client.closeTransport() }
        await supervisor.stop(mode: .terminate)
        do {
            _ = try await supervisor.waitForExit(timeout: .seconds(1))
            return .forced
        } catch {
            return .uncertain
        }
    }

    private func boundedShutdownRequest(
        client: any AppCoreServing,
        reason: String
    ) async -> ShutdownRequestResult {
        let completion = OneShot<ShutdownRequestResult>()
        let requestTask = Task {
            do {
                try await client.shutdown(reason: reason)
                completion.resolve(.accepted)
            } catch {
                completion.resolve(.failed)
            }
        }
        let timeout = shutdownRequestTimeout
        let timeoutComponents = timeout.components
        let timeoutSeconds = max(
            0,
            Double(timeoutComponents.seconds)
                + Double(timeoutComponents.attoseconds) / 1e18
        )
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeoutSeconds) {
            completion.resolve(.timedOut)
        }
        let result = await completion.wait()
        requestTask.cancel()
        if result != .timedOut {
            _ = await requestTask.result
        }
        return result
    }

    private func closeFailedStartup() async {
        runtimeGeneration &+= 1
        readyForOverview = false
        sleepTransitionInFlight = false
        wakeAfterSleepPending = false
        sleepLifecycleDelivered = false
        streamHasReachedReady = false
        suppressNextStreamReadyRefresh = false
        helperProcessMonitor?.cancel()
        helperProcessMonitor = nil
        if let client {
            self.client = nil
            Task { await client.closeTransport() }
        }
        if let streamController {
            await streamController.stop()
            self.streamController = nil
        }
        refreshGeneration &+= 1
        refreshAdmissionGeneration = nil
        invalidationRefreshPending = false
        initialOverviewRefreshState = .idle
        refreshTask?.cancel()
        refreshTask = nil
        cancelAccountRefresh()
        await supervisor.stop(mode: .terminate)
    }

    private func emit(_ state: CoreConnectionState) async {
        await stateSink(state)
    }
}
