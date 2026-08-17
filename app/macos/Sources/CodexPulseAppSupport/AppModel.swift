import CodexPulseProtocolGenerated
import Combine
import Foundation

public enum AppFeature: String, CaseIterable, Hashable, Identifiable, Sendable {
    case overview
    case sessions
    case projects
    case invocationUsage
    case quotaUsage
    case localStatus
    case sourcesJobs
    case settings

    public var id: String { rawValue }

    public func title(localization: AppLocalization) -> String {
        let key: String = switch self {
        case .overview: "feature.overview"
        case .sessions: "feature.sessions"
        case .projects: "feature.projects"
        case .quotaUsage: "feature.quotaUsage"
        case .invocationUsage: "feature.invocationUsage"
        case .localStatus: "feature.localStatus"
        case .sourcesJobs: "feature.sourcesJobs"
        case .settings: "feature.settings"
        }
        return localization.text(key)
    }

    public var symbol: String {
        switch self {
        case .overview: "gauge.with.dots.needle.67percent"
        case .sessions: "text.bubble"
        case .projects: "folder"
        case .quotaUsage: "chart.xyaxis.line"
        case .invocationUsage: "wrench.and.screwdriver"
        case .localStatus: "heart.text.square"
        case .sourcesJobs: "externaldrive.connected.to.line.below"
        case .settings: "gearshape"
        }
    }
}

private enum FeatureTaskKey: Hashable {
    case usage, statusOverview, statusAccount, invocationUsage, pricingCatalog, quota, quotaPace, quotaRefresh, resetCreditsRefresh
    case runtimeAction
    case sessions, sessionDetail
    case projects, projectDetail
    case sources, sourceDetail
    case jobs, jobDetail
    case healthProjection, dataHealth, healthList, healthDetail
    case settings, settingsSave

    var isRead: Bool {
        switch self {
        case .quotaRefresh, .resetCreditsRefresh, .runtimeAction, .settingsSave:
            false
        default:
            true
        }
    }
}

@MainActor
public final class AppModel: ObservableObject {
    @Published public private(set) var state: AppViewState = .idle
    @Published public private(set) var lastShutdownOutcome: ShutdownOutcome?
    @Published public private(set) var isOverviewRefreshing = false
    @Published public private(set) var isRefreshingAll = false
    @Published public var selectedFeature: AppFeature = .overview
	@Published public private(set) var selectedProvider: AgentProvider = .codex
	@Published public private(set) var statusProvider: AgentProvider = .codex
    @Published public private(set) var renderedFeatures: Set<AppFeature> = []

    @Published public var sessionOptions = SessionQueryOptions()
    @Published public var projectOptions = ProjectQueryOptions()
    @Published public var sourceOptions = RuntimeQueryOptions()
    @Published public var jobOptions = RuntimeQueryOptions()
    @Published public var healthOptions = RuntimeQueryOptions(firstField: "active", firstValues: ["true"])
    @Published public var usageRange: DateRangePreset = .sevenDays
    @Published public private(set) var invocationRange: DateRangePreset = .sevenDays
    @Published public private(set) var invocationSourceClass = "all"
    @Published public private(set) var invocationRangeFellBackFromQuotaWeek = false
    @Published public private(set) var overviewRange: DateRangePreset = .quotaWeek

    @Published public private(set) var usageState: FeatureLoadState<Codexpulse_Core_V1_UsageCostResponse> = .idle
	@Published public private(set) var statusUsageState: FeatureLoadState<Codexpulse_Core_V1_UsageCostResponse> = .idle
	@Published public private(set) var statusInvocationState: FeatureLoadState<Codexpulse_Core_V1_InvocationUsageResponse> = .idle
	@Published public private(set) var statusOverviewState: FeatureLoadState<OverviewPresentation> = .idle
    @Published public private(set) var invocationUsageState:
        FeatureLoadState<Codexpulse_Core_V1_InvocationUsageResponse> = .idle
    @Published public private(set) var pricingCatalogState:
        FeatureLoadState<Codexpulse_Core_V1_PricingCatalogCurrentResponse> = .idle
    @Published public private(set) var quotaState: FeatureLoadState<Codexpulse_Core_V1_QuotaCurrentResponse> = .idle
    @Published public private(set) var quotaPaceState:
        FeatureLoadState<Codexpulse_Core_V1_QuotaPaceResponse> = .idle
    @Published public private(set) var quotaRefreshState: ActionState = .idle
    @Published public private(set) var resetCreditsRefreshState: ActionState = .idle
    @Published public private(set) var runtimeActionState: ActionState = .idle
    @Published public private(set) var sessionsState: FeatureLoadState<Codexpulse_Core_V1_SessionListResponse> = .idle
    @Published public private(set) var sessionDetailState: FeatureLoadState<Codexpulse_Core_V1_SessionDetailResponse> = .idle
    @Published public private(set) var projectsState: FeatureLoadState<Codexpulse_Core_V1_ProjectListResponse> = .idle
    @Published public private(set) var projectDetailState: FeatureLoadState<Codexpulse_Core_V1_ProjectDetailResponse> = .idle
    @Published public private(set) var sourcesState: FeatureLoadState<Codexpulse_Core_V1_SourceListResponse> = .idle
    @Published public private(set) var sourceDetailState: FeatureLoadState<Codexpulse_Core_V1_SourceDetailResponse> = .idle
    @Published public private(set) var jobsState: FeatureLoadState<Codexpulse_Core_V1_JobListResponse> = .idle
    @Published public private(set) var jobDetailState: FeatureLoadState<Codexpulse_Core_V1_JobDetailResponse> = .idle
    @Published public private(set) var healthProjectionState: FeatureLoadState<Codexpulse_Core_V1_HealthProjectionResponse> = .idle
    @Published public private(set) var dataHealthState: FeatureLoadState<Codexpulse_Core_V1_DataHealthResponse> = .idle
    @Published public private(set) var healthState: FeatureLoadState<Codexpulse_Core_V1_HealthListResponse> = .idle
    @Published public private(set) var healthDetailState: FeatureLoadState<Codexpulse_Core_V1_HealthDetailResponse> = .idle
    @Published public private(set) var settingsState: FeatureLoadState<Codexpulse_Core_V1_SettingsResponse> = .idle
    @Published public var settingsDraft: SettingsDraft?
    @Published public private(set) var settingsSaveState: SettingsSaveState = .idle
    @Published public private(set) var updatePolicy: AppUpdatePolicy = .disabled
    @Published public private(set) var localization: AppLocalization = .system

    @Published public private(set) var selectedSessionID: String?
    @Published public private(set) var selectedProjectKey: String?
    @Published public private(set) var selectedSourceKey: String?
    @Published public private(set) var selectedJobID: String?
    @Published public private(set) var selectedHealthEventID: String?

    private let runtime: AppRuntime
    private let observesUpdatePolicy: Bool
	private let providerDefaults: UserDefaults
	private let persistsProviderSelection: Bool
	private static let selectedProviderKey = "CodexPulse.selectedProvider"
	private static let statusProviderKey = "CodexPulse.statusProvider"
    private var startTask: Task<Void, Never>?
    private var updatePolicyTask: Task<Void, Never>?
    private var overviewRefreshTask: Task<Void, Never>?
    private var overviewRefreshGeneration: UInt64 = 0
    private var featureTasks: [FeatureTaskKey: Task<Void, Never>] = [:]
    private var featureGenerations: [FeatureTaskKey: UInt64] = [:]
    private var consumedCursors: [FeatureTaskKey: Set<String>] = [:]
    private var refreshAllPendingTasks: Set<FeatureTaskKey> = []
    private var refreshAllWaitsForOverview = false
    private var latestRuntimeState: CoreConnectionState = .idle

    public init(configuration: AppLaunchConfiguration) {
        runtime = AppRuntime(configuration: configuration)
        observesUpdatePolicy = !configuration.smokeMode
		providerDefaults = .standard
		persistsProviderSelection = !configuration.smokeMode
		selectedProvider = configuration.smokeMode
			? .codex
			: AgentProvider(rawValue: providerDefaults.string(forKey: Self.selectedProviderKey) ?? "") ?? .codex
		statusProvider = configuration.smokeMode
			? .codex
			: AgentProvider(rawValue: providerDefaults.string(forKey: Self.statusProviderKey) ?? "") ?? .codex
		overviewRange = selectedProvider == .cursor ? .quotaMonth : .quotaWeek
    }

    public init(
        runtime: AppRuntime,
        observesUpdatePolicy: Bool = false,
		providerDefaults: UserDefaults = .standard,
		persistsProviderSelection: Bool = true
    ) {
        self.runtime = runtime
        self.observesUpdatePolicy = observesUpdatePolicy
		self.providerDefaults = providerDefaults
		self.persistsProviderSelection = persistsProviderSelection
		selectedProvider = persistsProviderSelection
			? AgentProvider(rawValue: providerDefaults.string(forKey: Self.selectedProviderKey) ?? "") ?? .codex
			: .codex
		statusProvider = persistsProviderSelection
			? AgentProvider(rawValue: providerDefaults.string(forKey: Self.statusProviderKey) ?? "") ?? .codex
			: .codex
		overviewRange = selectedProvider == .cursor ? .quotaMonth : .quotaWeek
    }

    public var statusItemTitle: String {
		if statusProvider == .cursor {
			return "月剩 -- · 已用 --"
		}
        let currentLocalization = localization
		switch statusPresentation {
        case .some(let overview):
            let values = overview.quotaWindows.prefix(2).map { window in
                let percent = window.remainingPercent.map { currentLocalization.percent($0) } ?? "--"
                return "\(window.title) \(percent)"
            }
            return values.isEmpty ? "Codex Pulse --" : values.joined(separator: " · ")
        case .none:
            switch state {
            case .loading: return "Codex Pulse …"
            case .recovery, .restartRequired:
                return "Codex Pulse \(currentLocalization.textValue("恢复"))"
            case .unavailable:
                return "Codex Pulse \(currentLocalization.textValue("离线"))"
            default: return "Codex Pulse --"
            }
        }
    }

	public var statusPresentation: OverviewPresentation? {
		statusOverviewState.value
	}

    public var presentation: OverviewPresentation? {
        switch state {
        case .overview(let value), .partial(let value), .stale(let value, _): value
        default: nil
        }
    }

    public var requiresCoreRestart: Bool {
        switch state {
        case .stale(_, let notice):
            notice.code == "helper_exited" || notice.code == "invalidation_stream_failed"
        case .restartRequired: true
        case .unavailable(let notice): notice.retryable
        default: false
        }
    }

    public var canRefreshOrRestart: Bool {
        guard !isOverviewRefreshing else { return false }
        return switch state {
        case .unavailable(let notice): notice.retryable
        case .cancelled, .shuttingDown, .stopped: false
        default: true
        }
    }

    public func isRefreshing(_ feature: AppFeature) -> Bool {
        switch feature {
        case .overview:
            isOverviewRefreshing
        case .sessions:
            sessionsState.isLoading || sessionDetailState.isLoading
        case .projects:
            projectsState.isLoading || projectDetailState.isLoading
        case .quotaUsage:
            quotaState.isLoading || quotaPaceState.isLoading ||
                usageState.isLoading || pricingCatalogState.isLoading
        case .invocationUsage:
            invocationUsageState.isLoading
        case .localStatus:
            healthProjectionState.isLoading || dataHealthState.isLoading ||
                healthState.isLoading || healthDetailState.isLoading
        case .sourcesJobs:
            sourcesState.isLoading || sourceDetailState.isLoading ||
                jobsState.isLoading || jobDetailState.isLoading
        case .settings:
            settingsState.isLoading
        }
    }

    public func start() {
        guard startTask == nil else { return }
        let runtime = runtime
        startTask = Task { [weak self] in
            guard let self else { return }
            await runtime.setStateSink { [weak self] runtimeState in
                await self?.receive(runtimeState)
            }
            await runtime.setInvalidationSink { [weak self] domain in
                await self?.receiveInvalidation(domain: domain)
            }
			await runtime.selectProvider(self.selectedProvider)
            await runtime.start()
            if self.observesUpdatePolicy {
                self.loadSettings()
            }
            self.startTask = nil
        }
    }

	public func selectProvider(_ provider: AgentProvider) {
		guard provider != selectedProvider else { return }
		selectedProvider = provider
		if persistsProviderSelection {
			providerDefaults.set(provider.rawValue, forKey: Self.selectedProviderKey)
		}
		if provider == .cursor, sessionOptions.sortField == "estimatedCost" {
			sessionOptions.sortField = "lastActivityAt"
		}
		if provider == .cursor, projectOptions.sortField == "estimatedCost" {
			projectOptions.sortField = "lastActivityAt"
		}
		overviewRange = provider == .cursor ? .quotaMonth : .quotaWeek
		overviewRefreshGeneration &+= 1
		overviewRefreshTask?.cancel()
		overviewRefreshTask = nil
		cancelPageFeatureTasks()
		resetProviderFeatureState()
		state = .loading(localization.textValue("正在切换客户端…"))
		Task { [weak self, runtime] in
			await runtime.selectProvider(provider)
			guard let self, self.selectedProvider == provider else { return }
		}
	}

	public func selectStatusProvider(_ provider: AgentProvider) {
		guard provider != statusProvider else { return }
		invalidateTasks([.statusOverview, .statusAccount])
		statusProvider = provider
		if persistsProviderSelection {
			providerDefaults.set(provider.rawValue, forKey: Self.statusProviderKey)
		}
		statusOverviewState = .idle
		statusUsageState = .idle
		statusInvocationState = .idle
		loadStatusOverview()
	}

	public func refreshStatusProvider() {
		loadStatusOverview()
	}

	public func openMainWindowProvider(_ provider: AgentProvider) {
		selectProvider(provider)
		selectedFeature = .overview
	}

    public func applyLocalePreference(_ rawValue: String) {
        let preference = AppLanguagePreference(rawValue: rawValue)
        let next = AppLocalization(preference: preference)
        guard next != localization || preference.rawValue != localization.preference.rawValue else { return }
        AppLocalizationRegistry.shared.update(next)
        localization = next
        state = AppViewState(latestRuntimeState, localization: next)
    }

    public func refresh() {
        guard canRefreshOrRestart else { return }
        isOverviewRefreshing = true
        overviewRefreshGeneration &+= 1
        let generation = overviewRefreshGeneration
        let runtime = runtime
        overviewRefreshTask = Task { [weak self] in
            await runtime.refresh()
            guard let self, generation == self.overviewRefreshGeneration else { return }
            self.finishOverviewRefresh()
        }
    }

    public func selectOverviewRange(_ range: DateRangePreset) {
        guard range != .all, range != overviewRange, canRefreshOrRestart else { return }
        overviewRange = range
        isOverviewRefreshing = true
        overviewRefreshGeneration &+= 1
        let generation = overviewRefreshGeneration
        let runtime = runtime
        overviewRefreshTask?.cancel()
        overviewRefreshTask = Task { [weak self] in
            await runtime.refresh(range: range)
            guard let self, generation == self.overviewRefreshGeneration else { return }
            self.finishOverviewRefresh()
        }
    }

    public func refresh(_ feature: AppFeature) {
        guard canRefreshOrRestart else { return }
        if requiresCoreRestart {
            restartCore()
            return
        }
        switch feature {
        case .overview: refreshOrRestart()
        case .sessions: loadSessions(reset: true)
        case .projects: loadProjects(reset: true)
        case .quotaUsage: loadQuotaAndUsage()
        case .invocationUsage: loadInvocationUsage()
        case .localStatus: loadLocalStatus()
        case .sourcesJobs: loadSourcesAndJobs(reset: true)
        case .settings: loadSettings()
        }
        reloadSelectedDetails(for: feature, onlyIfNeeded: false)
    }

    public func load(_ feature: AppFeature) {
        selectedFeature = feature
        guard canRefreshOrRestart else { return }
        switch feature {
        case .overview: break
        case .sessions:
            if sessionsState.shouldReloadOnNavigation { loadSessions(reset: true) }
        case .projects:
            if projectsState.shouldReloadOnNavigation { loadProjects(reset: true) }
        case .quotaUsage:
            if quotaState.shouldReloadOnNavigation || quotaPaceState.shouldReloadOnNavigation ||
                usageState.shouldReloadOnNavigation ||
                pricingCatalogState.shouldReloadOnNavigation
            {
                loadQuotaAndUsage()
            }
        case .invocationUsage:
            if invocationUsageState.shouldReloadOnNavigation { loadInvocationUsage() }
        case .localStatus:
            if dataHealthState.shouldReloadOnNavigation || healthState.shouldReloadOnNavigation { loadLocalStatus() }
        case .sourcesJobs:
            if sourcesState.shouldReloadOnNavigation || jobsState.shouldReloadOnNavigation {
                loadSourcesAndJobs(reset: true)
            }
        case .settings:
            if settingsState.shouldReloadOnNavigation { loadSettings() }
        }
        reloadSelectedDetails(for: feature, onlyIfNeeded: true)
    }

    public func navigate(to feature: AppFeature) {
        selectedFeature = feature
        load(feature)
    }

    public func navigateToInvocationUsageFromOverview() {
        let contextChanged = invocationRange != overviewRange || invocationSourceClass != "all"
        invocationRange = overviewRange
        invocationSourceClass = "all"
        selectedFeature = .invocationUsage
        guard canRefreshOrRestart else { return }
        if contextChanged || invocationUsageState.shouldReloadOnNavigation {
            loadInvocationUsage()
        }
    }

    public func markFeatureRendered(_ feature: AppFeature) {
        renderedFeatures.insert(feature)
    }

    public func refreshAllFeatures() {
        guard canRefreshOrRestart, !isRefreshingAll else { return }
        if requiresCoreRestart {
            restartCore()
            return
        }
        isRefreshingAll = true
        refreshAllPendingTasks.removeAll()
        refreshAllWaitsForOverview = true
        loadSessions(reset: true)
        loadProjects(reset: true)
        loadQuotaAndUsage()
        loadInvocationUsage()
        loadLocalStatus()
        loadSourcesAndJobs(reset: true)
        loadSettings()
        for feature in [AppFeature.sessions, .projects, .localStatus, .sourcesJobs] {
            reloadSelectedDetails(for: feature, onlyIfNeeded: false)
        }
        refresh()
        finishRefreshAllIfPossible()
    }

    private func reloadSelectedDetails(for feature: AppFeature, onlyIfNeeded: Bool) {
        switch feature {
        case .sessions:
            if let selectedSessionID,
               !onlyIfNeeded || sessionDetailState.shouldReloadOnNavigation
            {
                loadSessionDetail(sessionID: selectedSessionID, reset: true)
            }
        case .projects:
            if let selectedProjectKey,
               !onlyIfNeeded || projectDetailState.shouldReloadOnNavigation
            {
                loadProjectDetail(dimensionKey: selectedProjectKey, reset: true)
            }
        case .localStatus:
            if let selectedHealthEventID,
               !onlyIfNeeded || healthDetailState.shouldReloadOnNavigation
            {
                loadHealthDetail(eventID: selectedHealthEventID)
            }
        case .sourcesJobs:
            if let selectedSourceKey,
               !onlyIfNeeded || sourceDetailState.shouldReloadOnNavigation
            {
                loadSourceDetail(sourceKey: selectedSourceKey)
            }
            if let selectedJobID,
               !onlyIfNeeded || jobDetailState.shouldReloadOnNavigation
            {
                loadJobDetail(jobID: selectedJobID)
            }
        case .overview, .quotaUsage, .invocationUsage, .settings:
            break
        }
    }

    public func refreshOrRestart() {
        guard canRefreshOrRestart else { return }
        requiresCoreRestart ? restartCore() : refresh()
    }

    public func retryRecovery() {
        Task { await runtime.retryRecovery() }
    }

    public func restartCore() {
        guard canRefreshOrRestart else { return }
        cancelUpdatePolicyObservation()
        cancelAllFeatureTasks()
        resetFeatureState()
        Task { await runtime.restart() }
    }

    public func applicationDidBecomeActive() {
        Task { await runtime.applicationDidBecomeActive() }
    }

    public func applicationWillResignActive() {
        Task { await runtime.applicationWillResignActive() }
    }

    public func prepareForSleep() {
        cancelUpdatePolicyObservation()
        cancelFeatureReadTasks()
        markFeatureStatesStale(AppNotice(
            code: "system_sleeping",
            messageKey: "app.notice.system_sleeping",
            retryable: true
        ))
        Task { await runtime.prepareForSleep() }
    }

    public func resumeAfterWake() {
        Task { [weak self] in
            guard let self else { return }
            await runtime.resumeAfterWake()
            startUpdatePolicyObservationIfNeeded()
        }
    }

    public func shutdown() async -> ShutdownOutcome {
        await shutdown(reason: .applicationExit)
    }

    public func prepareForUpdateInstallation() async -> AppUpdateInstallPreparation {
        AppUpdateInstallPreparation(
            shutdownOutcome: await shutdown(reason: .updateInstallation)
        )
    }

    private func shutdown(reason: AppShutdownReason) async -> ShutdownOutcome {
        startTask?.cancel()
        startTask = nil
        overviewRefreshTask?.cancel()
        overviewRefreshTask = nil
        overviewRefreshGeneration &+= 1
        isOverviewRefreshing = false
        cancelUpdatePolicyObservation()
        updatePolicy = .disabled
        cancelAllFeatureTasks()
        let outcome = await runtime.shutdown(reason: reason)
        lastShutdownOutcome = outcome
        return outcome
    }

    public func runPrimaryPagesSmoke() async throws -> PrimaryPagesSmokeSummary {
        try await runtime.primaryPagesSmoke()
    }

    public func sessionFiltersChanged() {
        selectedSessionID = nil
        sessionDetailState = .idle
        loadSessions(reset: true)
    }

    public func loadSessions(reset: Bool) {
        let previous = sessionsState.value
        let cursor = reset ? nil : previous?.meta.page.nextCursor
        guard reset || (previous.map { pageHasMore($0.meta) } == true) else { return }
        guard beginPage(.sessions, cursor: cursor, reset: reset) else {
            if let previous { sessionsState = stoppedPagination(previous) }
            return
        }
        sessionsState = .loading(previous: previous)
		let provider = selectedProvider
		let request = FeatureRequestFactory.sessions(options: sessionOptions, provider: provider, cursor: cursor)
        launch(.sessions, operation: { [runtime] in try await runtime.listSessions(request) }) { [weak self] response in
			guard let self, response.providerContext.effectiveProvider == provider.rawValue else { return }
            completePage(.sessions, cursor: cursor)
            let merged = FeatureResponseMerge.sessions(previous, response, append: !reset)
            sessionsState = loadState(value: merged, meta: merged.meta, isEmpty: merged.items.isEmpty)
        } failure: { [weak self] error in
            self?.sessionsState = failedLoadState(previous: previous, error: error)
        }
    }

    public func selectSession(_ sessionID: String?) {
        selectedSessionID = sessionID
        sessionDetailState = .idle
        guard let sessionID else { return }
        loadSessionDetail(sessionID: sessionID, reset: true)
    }

    public func loadMoreSessionTurns() {
        guard let selectedSessionID else { return }
        loadSessionDetail(sessionID: selectedSessionID, reset: false)
    }

    private func loadSessionDetail(sessionID: String, reset: Bool) {
        let previous = sessionDetailState.value
        let cursor = reset ? nil : previous?.turnPage.nextCursor
        guard reset || (previous?.turnPage.hasMore_p == true && previous?.turnPage.hasNextCursor == true) else { return }
        guard beginPage(.sessionDetail, cursor: cursor, reset: reset) else {
            if let previous { sessionDetailState = stoppedPagination(previous) }
            return
        }
        sessionDetailState = .loading(previous: previous)
		let provider = selectedProvider
		let request = FeatureRequestFactory.sessionDetail(sessionID: sessionID, provider: provider, turnCursor: cursor)
        launch(.sessionDetail, operation: { [runtime] in try await runtime.sessionDetail(request) }) { [weak self] response in
			guard let self, selectedSessionID == sessionID,
				response.providerContext.effectiveProvider == provider.rawValue else { return }
            completePage(.sessionDetail, cursor: cursor)
            let merged = FeatureResponseMerge.sessionDetail(previous, response, append: !reset)
            sessionDetailState = loadState(value: merged, meta: merged.meta, isEmpty: false)
        } failure: { [weak self] error in
            guard let self else { return }
            let notice = AppNotice.from(error)
            if notice.code == "not_found" { selectedSessionID = nil }
            sessionDetailState = failedLoadState(previous: previous, error: error)
        }
    }

    public func projectFiltersChanged() {
        selectedProjectKey = nil
        projectDetailState = .idle
        loadProjects(reset: true)
    }

    public func loadProjects(reset: Bool) {
        let previous = projectsState.value
        let cursor = reset ? nil : previous?.meta.page.nextCursor
        guard reset || (previous.map { pageHasMore($0.meta) } == true) else { return }
        guard beginPage(.projects, cursor: cursor, reset: reset) else {
            if let previous { projectsState = stoppedPagination(previous) }
            return
        }
        projectsState = .loading(previous: previous)
		let provider = selectedProvider
		let request = FeatureRequestFactory.projects(options: projectOptions, provider: provider, cursor: cursor)
        launch(.projects, operation: { [runtime] in try await runtime.listProjects(request) }) { [weak self] response in
			guard let self, response.providerContext.effectiveProvider == provider.rawValue else { return }
            completePage(.projects, cursor: cursor)
            let merged = FeatureResponseMerge.projects(previous, response, append: !reset)
            projectsState = loadState(value: merged, meta: merged.meta, isEmpty: merged.items.isEmpty)
        } failure: { [weak self] error in
            self?.projectsState = failedLoadState(previous: previous, error: error)
        }
    }

    public func selectProject(_ dimensionKey: String?) {
        selectedProjectKey = dimensionKey
        projectDetailState = .idle
        guard let dimensionKey else { return }
        loadProjectDetail(dimensionKey: dimensionKey, reset: true)
    }

    public func loadMoreProjectDetail() {
        guard let selectedProjectKey else { return }
        loadProjectDetail(dimensionKey: selectedProjectKey, reset: false)
    }

    private func loadProjectDetail(dimensionKey: String, reset: Bool) {
        let previous = projectDetailState.value
        let loadSessions = reset || previous.map { $0.sessionPage.hasMore_p && $0.sessionPage.hasNextCursor } == true
        let loadModels = reset || previous.map { $0.modelPage.hasMore_p && $0.modelPage.hasNextCursor } == true
        let sessionCursor = reset ? nil : previous.flatMap {
            $0.sessionPage.hasNextCursor ? $0.sessionPage.nextCursor : nil
        }
        let modelCursor = reset ? nil : previous.flatMap {
            $0.modelPage.hasNextCursor ? $0.modelPage.nextCursor : nil
        }
        let cursorKeys = [
            loadSessions ? sessionCursor.map { "session:\($0)" } : nil,
            loadModels ? modelCursor.map { "model:\($0)" } : nil,
        ].compactMap { $0 }
        let hasMore = loadSessions || loadModels
        guard reset || hasMore else { return }
        if reset {
            consumedCursors[.projectDetail] = []
        } else {
            guard !cursorKeys.isEmpty,
                  cursorKeys.allSatisfy({ cursorIsAvailable(.projectDetail, cursor: $0) })
            else {
                if let previous { projectDetailState = stoppedPagination(previous) }
                return
            }
        }
        projectDetailState = .loading(previous: previous)
        let request = FeatureRequestFactory.projectDetail(
            dimensionKey: dimensionKey,
            range: projectOptions.range,
			provider: selectedProvider,
            exactRange: projectOptions.exactRange,
            sessionCursor: sessionCursor,
            modelCursor: modelCursor
        )
		let provider = selectedProvider
		launch(.projectDetail, operation: { [runtime] in try await runtime.projectDetail(request) }) { [weak self] response in
			guard let self, selectedProjectKey == dimensionKey,
				response.providerContext.effectiveProvider == provider.rawValue else { return }
            cursorKeys.forEach { completePage(.projectDetail, cursor: $0) }
            let merged = FeatureResponseMerge.projectDetail(
                previous,
                response,
                append: !reset,
                appendSessions: loadSessions,
                appendModels: loadModels
            )
            projectDetailState = loadState(value: merged, meta: merged.meta, isEmpty: false)
        } failure: { [weak self] error in
            guard let self else { return }
            let notice = AppNotice.from(error)
            if notice.code == "not_found" { selectedProjectKey = nil }
            projectDetailState = failedLoadState(previous: previous, error: error)
        }
    }

	public func loadQuotaAndUsage() {
		let now = Date()
		loadUsage()
		loadPricingCatalog()
        loadQuota(now: now)
        loadQuotaPace(now: now)
    }

    public func loadUsage() {
        let previous = usageState.value
        usageState = .loading(previous: previous)
		let provider = selectedProvider
		let request = FeatureRequestFactory.usage(range: usageRange, provider: provider)
        launch(.usage, operation: { [runtime] in try await runtime.usageCost(request) }) { [weak self] response in
			guard response.providerContext.effectiveProvider == provider.rawValue else { return }
			self?.usageState = loadState(value: response, meta: response.meta, isEmpty: false)
        } failure: { [weak self] error in
            self?.usageState = failedLoadState(previous: previous, error: error)
        }
    }

    public func selectInvocationRange(_ range: DateRangePreset) {
        guard [.quotaWeek, .quotaMonth, .today, .sevenDays, .thirtyDays].contains(range),
              range != invocationRange
        else { return }
        invocationRange = range
        loadInvocationUsage()
    }

    public func selectInvocationSourceClass(_ sourceClass: String) {
        let normalized = ["structured", "detected"].contains(sourceClass) ? sourceClass : "all"
        guard normalized != invocationSourceClass else { return }
        invocationSourceClass = normalized
        loadInvocationUsage()
    }

    public func loadInvocationUsage() {
        let previous = invocationUsageState.value
        invocationUsageState = .loading(previous: previous)
        let quotaCycleRange = [.quotaWeek, .quotaMonth].contains(invocationRange)
            ? availableInvocationQuotaCycleRange : nil
        invocationRangeFellBackFromQuotaWeek = invocationRange == .quotaWeek && quotaCycleRange == nil
        let request = FeatureRequestFactory.invocationUsage(
            range: invocationRange,
            sourceClass: invocationSourceClass,
			provider: selectedProvider,
            quotaCycleRange: quotaCycleRange
        )
		let provider = selectedProvider
		launch(
            .invocationUsage,
            operation: { [runtime] in try await runtime.invocationUsage(request) }
        ) { [weak self] response in
			guard response.providerContext.effectiveProvider == provider.rawValue else { return }
            self?.invocationUsageState = loadState(
                value: response,
                meta: response.meta,
                isEmpty: response.tools.isEmpty && response.skills.isEmpty
            )
        } failure: { [weak self] error in
            self?.invocationUsageState = failedLoadState(previous: previous, error: error)
        }
    }

    private var availableInvocationQuotaCycleRange: Codexpulse_Core_V1_UTCTimeRange? {
        guard let presentation else { return nil }
        if presentation.requestedRange == invocationRange,
           presentation.effectiveRange == invocationRange,
           !presentation.fellBackFromQuotaWeek
        {
            return presentation.contentRange
        }
        guard invocationRange == .quotaWeek else { return nil }
        guard presentation.weeklyUsageAvailable else { return nil }
        return presentation.weeklyUsageRange
    }

    public func loadPricingCatalog() {
        let previous = pricingCatalogState.value
        pricingCatalogState = .loading(previous: previous)
		let provider = selectedProvider
		let request = FeatureRequestFactory.pricingCatalog(provider: provider)
        launch(
            .pricingCatalog,
			operation: { [runtime] in try await runtime.pricingCatalogCurrent(request) }
        ) { [weak self] response in
			guard response.providerContext.effectiveProvider == provider.rawValue else { return }
            self?.pricingCatalogState = loadState(
                value: response,
                meta: response.meta,
                isEmpty: response.items.isEmpty
            )
        } failure: { [weak self] error in
            self?.pricingCatalogState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadQuota(now: Date = Date()) {
        let previous = quotaState.value
        quotaState = .loading(previous: previous)
		let provider = selectedProvider
		let request = FeatureRequestFactory.quota(provider: provider, now: now)
        launch(.quota, operation: { [runtime] in try await runtime.quotaCurrent(request) }) { [weak self] response in
			guard response.providerContext.effectiveProvider == provider.rawValue else { return }
            self?.quotaState = loadState(
                value: response,
                meta: response.meta,
                isEmpty: response.current.windows.isEmpty && response.current.sources.isEmpty
            )
        } failure: { [weak self] error in
            self?.quotaState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadQuotaPace(now: Date = Date()) {
        let previous = quotaPaceState.value
        quotaPaceState = .loading(previous: previous)
		let provider = selectedProvider
		let request = FeatureRequestFactory.quotaPace(provider: provider, now: now)
        launch(.quotaPace, operation: { [runtime] in try await runtime.quotaPace(request) }) { [weak self] response in
			guard response.providerContext.effectiveProvider == provider.rawValue else { return }
            self?.quotaPaceState = loadState(
                value: response,
                meta: response.meta,
                isEmpty: response.pace.windows.isEmpty
            )
        } failure: { [weak self] error in
            self?.quotaPaceState = failedLoadState(previous: previous, error: error)
        }
    }

    public func requestQuotaRefresh(source: String) {
        guard let taskKey = refreshTaskKey(source: source) else { return }
        guard canRefreshOrRestart else { return }
        if isRefreshRunning(source: source) { return }
        setRefreshState(.running, source: source)
        launch(taskKey, operation: { [runtime] in
            try await runtime.requestQuotaRefresh(source: source)
        }) { [weak self] receipt in
            guard let self else { return }
            setRefreshState(.succeeded(receipt.reason), source: source)
            let now = Date()
            loadQuota(now: now)
            loadQuotaPace(now: now)
        } failure: { [weak self] error in
            self?.setRefreshState(.unavailable(AppNotice.from(error)), source: source)
        }
    }

    private func refreshTaskKey(source: String) -> FeatureTaskKey? {
        switch source {
        case "quota": .quotaRefresh
        case "reset_credits": .resetCreditsRefresh
        default: nil
        }
    }

    private func isRefreshRunning(source: String) -> Bool {
        switch source {
        case "quota":
            if case .running = quotaRefreshState { return true }
        case "reset_credits":
            if case .running = resetCreditsRefreshState { return true }
        default:
            break
        }
        return false
    }

    private func setRefreshState(_ state: ActionState, source: String) {
        switch source {
        case "quota": quotaRefreshState = state
        case "reset_credits": resetCreditsRefreshState = state
        default: break
        }
    }

    public func runRuntimeAction(_ action: RuntimeControlAction) {
        guard canRefreshOrRestart else { return }
        if case .running = runtimeActionState { return }
        runtimeActionState = .running
        launch(.runtimeAction, operation: { [runtime] in
            try await runtime.runRuntimeAction(action)
        }) { [weak self] receipt in
            guard let self else { return }
            let result = receipt.transition.isEmpty ? receipt.sourceState : receipt.transition
            runtimeActionState = .succeeded(result.isEmpty ? receipt.action : result)
            refresh()
            loadLocalStatus()
            loadSourcesAndJobs(reset: true)
        } failure: { [weak self] error in
            self?.runtimeActionState = .unavailable(AppNotice.from(error))
        }
    }

    public func loadSourcesAndJobs(reset: Bool) {
        loadSources(reset: reset)
        loadJobs(reset: reset)
    }

    public func sourceFiltersChanged() {
        selectedSourceKey = nil
        sourceDetailState = .idle
        loadSources(reset: true)
    }

    public func loadSources(reset: Bool) {
        let previous = sourcesState.value
        let cursor = reset ? nil : previous?.meta.page.nextCursor
        guard reset || (previous.map { pageHasMore($0.meta) } == true) else { return }
        guard beginPage(.sources, cursor: cursor, reset: reset) else {
            if let previous { sourcesState = stoppedPagination(previous) }
            return
        }
        sourcesState = .loading(previous: previous)
        let request = FeatureRequestFactory.sources(options: sourceOptions, cursor: cursor)
        launch(.sources, operation: { [runtime] in try await runtime.listSources(request) }) { [weak self] response in
            guard let self else { return }
            completePage(.sources, cursor: cursor)
            let merged = FeatureResponseMerge.sources(previous, response, append: !reset)
            sourcesState = loadState(value: merged, meta: merged.meta, isEmpty: merged.items.isEmpty)
        } failure: { [weak self] error in
            self?.sourcesState = failedLoadState(previous: previous, error: error)
        }
    }

    public func selectSource(_ sourceKey: String?) {
        selectedSourceKey = sourceKey
        sourceDetailState = .idle
        guard let sourceKey else { return }
        loadSourceDetail(sourceKey: sourceKey)
    }

    private func loadSourceDetail(sourceKey: String) {
        let previous = sourceDetailState.value
        sourceDetailState = .loading(previous: previous)
        launch(.sourceDetail, operation: { [runtime] in try await runtime.source(key: sourceKey) }) { [weak self] response in
            guard let self, selectedSourceKey == sourceKey else { return }
            sourceDetailState = loadState(value: response, meta: response.meta, isEmpty: false)
        } failure: { [weak self] error in
            guard let self else { return }
            if AppNotice.from(error).code == "not_found" { selectedSourceKey = nil }
            sourceDetailState = failedLoadState(previous: previous, error: error)
        }
    }

    public func jobFiltersChanged() {
        selectedJobID = nil
        jobDetailState = .idle
        loadJobs(reset: true)
    }

    public func loadJobs(reset: Bool) {
        let previous = jobsState.value
        let cursor = reset ? nil : previous?.meta.page.nextCursor
        guard reset || (previous.map { pageHasMore($0.meta) } == true) else { return }
        guard beginPage(.jobs, cursor: cursor, reset: reset) else {
            if let previous { jobsState = stoppedPagination(previous) }
            return
        }
        jobsState = .loading(previous: previous)
        let request = FeatureRequestFactory.jobs(options: jobOptions, cursor: cursor)
        launch(.jobs, operation: { [runtime] in try await runtime.listJobs(request) }) { [weak self] response in
            guard let self else { return }
            completePage(.jobs, cursor: cursor)
            let merged = FeatureResponseMerge.jobs(previous, response, append: !reset)
            jobsState = loadState(value: merged, meta: merged.meta, isEmpty: merged.items.isEmpty)
        } failure: { [weak self] error in
            self?.jobsState = failedLoadState(previous: previous, error: error)
        }
    }

    public func selectJob(_ jobID: String?) {
        selectedJobID = jobID
        jobDetailState = .idle
        guard let jobID else { return }
        loadJobDetail(jobID: jobID)
    }

    private func loadJobDetail(jobID: String) {
        let previous = jobDetailState.value
        jobDetailState = .loading(previous: previous)
        launch(.jobDetail, operation: { [runtime] in try await runtime.job(id: jobID) }) { [weak self] response in
            guard let self, selectedJobID == jobID else { return }
            jobDetailState = loadState(value: response, meta: response.meta, isEmpty: false)
        } failure: { [weak self] error in
            guard let self else { return }
            if AppNotice.from(error).code == "not_found" { selectedJobID = nil }
            jobDetailState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadLocalStatus() {
        loadHealthProjection()
        loadDataHealth()
        loadHealth(reset: true)
    }

    public func loadHealthProjection() {
        let previous = healthProjectionState.value
        healthProjectionState = .loading(previous: previous)
        launch(.healthProjection, operation: { [runtime] in try await runtime.healthProjection() }) { [weak self] response in
            self?.healthProjectionState = response.hasValue_p
                ? .ready(response)
                : (response.failure.isEmpty ? .empty : .partial(response, notices: [AppNotice(
                    code: response.failure,
                    messageKey: "health.projection.partial",
                    retryable: true
                )]))
        } failure: { [weak self] error in
            self?.healthProjectionState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadDataHealth() {
        let previous = dataHealthState.value
        dataHealthState = .loading(previous: previous)
        let request = FeatureRequestFactory.dataHealth()
        launch(.dataHealth, operation: { [runtime] in try await runtime.dataHealth(request) }) { [weak self] response in
            self?.dataHealthState = loadState(value: response, meta: response.meta, isEmpty: response.runtime.isEmpty)
        } failure: { [weak self] error in
            self?.dataHealthState = failedLoadState(previous: previous, error: error)
        }
    }

    public func healthFiltersChanged() {
        selectedHealthEventID = nil
        healthDetailState = .idle
        loadHealth(reset: true)
    }

    public func loadHealth(reset: Bool) {
        let previous = healthState.value
        let cursor = reset ? nil : previous?.meta.page.nextCursor
        guard reset || (previous.map { pageHasMore($0.meta) } == true) else { return }
        guard beginPage(.healthList, cursor: cursor, reset: reset) else {
            if let previous { healthState = stoppedPagination(previous) }
            return
        }
        healthState = .loading(previous: previous)
        let request = FeatureRequestFactory.health(options: healthOptions, cursor: cursor)
        launch(.healthList, operation: { [runtime] in try await runtime.listHealth(request) }) { [weak self] response in
            guard let self else { return }
            completePage(.healthList, cursor: cursor)
            let merged = FeatureResponseMerge.health(previous, response, append: !reset)
            healthState = loadState(value: merged, meta: merged.meta, isEmpty: merged.items.isEmpty)
        } failure: { [weak self] error in
            self?.healthState = failedLoadState(previous: previous, error: error)
        }
    }

    public func selectHealthEvent(_ eventID: String?) {
        selectedHealthEventID = eventID
        healthDetailState = .idle
        guard let eventID else { return }
        loadHealthDetail(eventID: eventID)
    }

    private func loadHealthDetail(eventID: String) {
        let previous = healthDetailState.value
        healthDetailState = .loading(previous: previous)
        launch(.healthDetail, operation: { [runtime] in try await runtime.health(eventID: eventID) }) { [weak self] response in
            guard let self, selectedHealthEventID == eventID else { return }
            healthDetailState = loadState(value: response, meta: response.meta, isEmpty: false)
        } failure: { [weak self] error in
            guard let self else { return }
            if AppNotice.from(error).code == "not_found" { selectedHealthEventID = nil }
            healthDetailState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadSettings() {
        if case .saving = settingsSaveState { return }
        let previous = settingsState.value
        let draftAtStart = settingsDraft
        let hadUnsavedChanges = draftAtStart != nil && previous.map(SettingsDraft.init) != draftAtStart
        settingsState = .loading(previous: previous)
        launch(.settings, operation: { [runtime] in try await runtime.settings() }) { [weak self] response in
            guard let self else { return }
            settingsState = loadState(value: response, meta: response.meta, isEmpty: false)
            let editedDuringLoad = settingsDraft != draftAtStart
            let preservedDraft = editedDuringLoad ? settingsDraft : (hadUnsavedChanges ? draftAtStart : nil)
            let effectiveDraft = preservedDraft ?? SettingsDraft(response)
            applyLocalePreference(effectiveDraft.locale)
            if let preservedDraft {
                settingsDraft = preservedDraft
                if previous?.snapshot.revision != response.snapshot.revision {
                    settingsSaveState = .conflict
                } else {
                    settingsSaveState = .idle
                }
            } else {
                settingsDraft = effectiveDraft
                settingsSaveState = .idle
            }
        } failure: { [weak self] error in
            self?.settingsState = failedLoadState(previous: previous, error: error)
        }
    }

    public func saveSettings() {
        if case .saving = settingsSaveState { return }
        guard canRefreshOrRestart, !requiresCoreRestart else { return }
        guard let authoritative = settingsState.value, let draft = settingsDraft else { return }
        let expectedRevision = authoritative.snapshot.revision
        let request = draft.makeRequest(authoritative: authoritative)
        settingsSaveState = .saving
        let generation = beginTask(.settingsSave)
        let runtime = runtime
        featureTasks[.settingsSave] = Task { [weak self] in
            guard let self else { return }
            do {
                let receipt = try await runtime.updateSettings(request)
                let readback = try await runtime.settings()
                try Task.checkCancellation()
                guard isCurrent(.settingsSave, generation: generation) else { return }
                finishTask(.settingsSave)
                let pendingDraft = settingsDraft.flatMap { $0 == draft ? nil : $0 }
                guard readback.snapshot.revision == receipt.revision else {
                    settingsState = loadState(value: readback, meta: readback.meta, isEmpty: false)
                    settingsDraft = pendingDraft ?? draft
                    settingsSaveState = .conflict
                    return
                }
                settingsState = loadState(value: readback, meta: readback.meta, isEmpty: false)
                switch receipt.result {
                case "applied":
                    let effectiveDraft = pendingDraft ?? SettingsDraft(readback)
                    settingsDraft = effectiveDraft
                    applyLocalePreference(effectiveDraft.locale)
                    settingsSaveState = pendingDraft == nil ? .applied(revision: receipt.revision) : .idle
                    restartUpdatePolicyObservation()
                case "applied_reconcile_required":
                    let effectiveDraft = pendingDraft ?? SettingsDraft(readback)
                    settingsDraft = effectiveDraft
                    applyLocalePreference(effectiveDraft.locale)
                    settingsSaveState = .reconcileRequired(revision: receipt.revision)
                    restartUpdatePolicyObservation()
                default:
                    settingsDraft = pendingDraft ?? draft
                    settingsSaveState = .unavailable(AppNotice(
                        code: "contract_unavailable",
                        messageKey: "app.error.settings_receipt_result",
                        retryable: false
                    ))
                }
            } catch {
                guard isCurrent(.settingsSave, generation: generation) else { return }
                let readback = try? await runtime.settings()
                guard isCurrent(.settingsSave, generation: generation) else { return }
                finishTask(.settingsSave)
                if let readback, readback.snapshot.revision != expectedRevision {
                    settingsState = loadState(value: readback, meta: readback.meta, isEmpty: false)
                    let pendingDraft = settingsDraft.flatMap { $0 == draft ? nil : $0 }
                    settingsDraft = pendingDraft ?? draft
                    applyLocalePreference((pendingDraft ?? SettingsDraft(readback)).locale)
                    settingsSaveState = .conflict
                } else {
                    settingsSaveState = .unavailable(AppNotice.from(error))
                }
            }
        }
    }

    private func launch<Value: Sendable>(
        _ key: FeatureTaskKey,
        operation: @escaping @Sendable () async throws -> Value,
        success: @escaping @MainActor (Value) -> Void,
        failure: @escaping @MainActor (any Error) -> Void
    ) {
        let generation = beginTask(key)
        featureTasks[key] = Task { [weak self] in
            guard let self else { return }
            do {
                let value = try await operation()
                try Task.checkCancellation()
                guard isCurrent(key, generation: generation) else { return }
                finishTask(key)
                success(value)
            } catch {
                guard isCurrent(key, generation: generation) else { return }
                finishTask(key)
                failure(error)
            }
        }
    }

    private func beginTask(_ key: FeatureTaskKey) -> UInt64 {
        featureTasks[key]?.cancel()
        let generation = (featureGenerations[key] ?? 0) &+ 1
        featureGenerations[key] = generation
        if isRefreshingAll, key.isRead {
            refreshAllPendingTasks.insert(key)
        }
        return generation
    }

    private func isCurrent(_ key: FeatureTaskKey, generation: UInt64) -> Bool {
        featureGenerations[key] == generation
    }

    private func finishTask(_ key: FeatureTaskKey) {
        featureTasks[key] = nil
        refreshAllPendingTasks.remove(key)
        finishRefreshAllIfPossible()
    }

    private func cancelAllFeatureTasks() {
        for key in featureTasks.keys {
            featureTasks[key]?.cancel()
            featureGenerations[key, default: 0] &+= 1
        }
        featureTasks.removeAll()
        cancelRefreshAll()
    }

	private func cancelPageFeatureTasks() {
		let statusKeys: Set<FeatureTaskKey> = [.statusOverview, .statusAccount]
		let keys = featureTasks.keys.filter { !statusKeys.contains($0) }
		for key in keys {
			featureTasks[key]?.cancel()
			featureTasks[key] = nil
			featureGenerations[key, default: 0] &+= 1
			refreshAllPendingTasks.remove(key)
		}
		cancelRefreshAll()
	}

    private func cancelFeatureReadTasks() {
        let mutationKeys: Set<FeatureTaskKey> = [
            .quotaRefresh, .resetCreditsRefresh, .runtimeAction, .settingsSave,
        ]
        let keys = featureTasks.keys.filter { !mutationKeys.contains($0) }
        for key in keys {
            featureTasks[key]?.cancel()
            featureTasks[key] = nil
            featureGenerations[key, default: 0] &+= 1
        }
        cancelRefreshAll()
    }

    private func receive(_ runtimeState: CoreConnectionState) {
        latestRuntimeState = runtimeState
        switch runtimeState {
        case .normal, .partial, .stale, .unavailable, .cancelled:
            finishOverviewRefresh()
        case .idle, .starting, .handshaking, .loadingOverview, .recovery, .restartRequired, .shuttingDown, .stopped:
            break
        }
        state = AppViewState(runtimeState, localization: localization)
        switch runtimeState {
        case .normal, .partial:
            startUpdatePolicyObservationIfNeeded()
				if statusOverviewState.shouldReloadOnNavigation {
					loadStatusOverview()
				}
            if selectedFeature != .overview { load(selectedFeature) }
        case .stale(_, let notice), .unavailable(let notice):
            cancelUpdatePolicyObservation()
            cancelAllFeatureTasks()
            markMutationsUncertain(notice)
            markFeatureStatesStale(notice)
        case .recovery, .restartRequired, .shuttingDown, .stopped:
            cancelUpdatePolicyObservation()
            cancelAllFeatureTasks()
            markMutationsUncertain(AppNotice(
                code: "mutation_result_unknown",
                messageKey: "app.error.mutation_result_unknown",
                retryable: true
            ))
        case .idle, .starting, .handshaking, .loadingOverview, .cancelled:
            break
        }
    }

    private func startUpdatePolicyObservationIfNeeded() {
        guard updatePolicyTask == nil, observesUpdatePolicy else { return }
        let runtime = runtime
        updatePolicyTask = Task { [weak self] in
            guard let self else { return }
            while !Task.isCancelled {
                var retryDelay: Int64 = 3_600
                do {
                    let settings = try await runtime.settings()
                    try Task.checkCancellation()
                    guard let channel = AppUpdateChannel(
                        rawValue: settings.snapshot.updates.channel
                    ) else {
                        updatePolicy = .disabled
                        try await Task.sleep(nanoseconds: 3_600 * 1_000_000_000)
                        continue
                    }
                    let policy = AppUpdatePolicy(
                        automaticallyChecks: settings.snapshot.updates.autoCheckEnabled,
                        automaticallyDownloads: settings.snapshot.updates.autoDownloadEnabled,
                        channel: channel,
                        checkIntervalSeconds: settings.snapshot.updates.checkIntervalSeconds
                    )
                    updatePolicy = policy
                    retryDelay = Int64(policy.checkIntervalSeconds)
                    guard policy.automaticallyChecks else {
                        updatePolicyTask = nil
                        return
                    }
                } catch is CancellationError {
                    return
                } catch {
                    if Task.isCancelled { return }
                }
                do {
                    try await Task.sleep(
                        nanoseconds: UInt64(max(retryDelay, 0)) * 1_000_000_000
                    )
                } catch {
                    return
                }
            }
        }
    }

    private func restartUpdatePolicyObservation() {
        cancelUpdatePolicyObservation()
        startUpdatePolicyObservationIfNeeded()
    }

    private func cancelUpdatePolicyObservation() {
        updatePolicyTask?.cancel()
        updatePolicyTask = nil
    }

    private func finishOverviewRefresh() {
        isOverviewRefreshing = false
        overviewRefreshTask = nil
        refreshAllWaitsForOverview = false
        finishRefreshAllIfPossible()
    }

    private func finishRefreshAllIfPossible() {
        guard isRefreshingAll,
              refreshAllPendingTasks.isEmpty,
              !refreshAllWaitsForOverview
        else {
            return
        }
        isRefreshingAll = false
    }

    private func cancelRefreshAll() {
        isRefreshingAll = false
        refreshAllPendingTasks.removeAll()
        refreshAllWaitsForOverview = false
    }

    private func markMutationsUncertain(_ notice: AppNotice) {
        if case .running = quotaRefreshState { quotaRefreshState = .unavailable(notice) }
        if case .running = resetCreditsRefreshState { resetCreditsRefreshState = .unavailable(notice) }
        if case .running = runtimeActionState { runtimeActionState = .unavailable(notice) }
        if case .saving = settingsSaveState { settingsSaveState = .unavailable(notice) }
    }

    private func receiveInvalidation(domain: String) {
        let notice = AppNotice(
            code: "content_invalidated",
            messageKey: "app.notice.content_invalidated.\(domain)",
            retryable: true
        )
        let affected: Set<AppFeature>
		let refreshesStatus: Bool
        switch domain {
        case "index":
            invalidateTasks([
                .usage, .invocationUsage, .sessions, .sessionDetail, .projects, .projectDetail,
            ])
            usageState = stale(usageState, notice)
            invocationUsageState = stale(invocationUsageState, notice)
            sessionsState = stale(sessionsState, notice)
            sessionDetailState = stale(sessionDetailState, notice)
            projectsState = stale(projectsState, notice)
            projectDetailState = stale(projectDetailState, notice)
            affected = [.sessions, .projects, .quotaUsage, .invocationUsage]
			refreshesStatus = true
        case "quota":
            invalidateTasks([.quota, .quotaPace])
            quotaState = stale(quotaState, notice)
            quotaPaceState = stale(quotaPaceState, notice)
            affected = [.quotaUsage]
			refreshesStatus = true
        case "health":
            invalidateTasks([.healthProjection, .dataHealth, .healthList, .healthDetail, .sources, .sourceDetail, .jobs, .jobDetail])
            healthProjectionState = stale(healthProjectionState, notice)
            dataHealthState = stale(dataHealthState, notice)
            healthState = stale(healthState, notice)
            healthDetailState = stale(healthDetailState, notice)
            sourcesState = stale(sourcesState, notice)
            sourceDetailState = stale(sourceDetailState, notice)
            jobsState = stale(jobsState, notice)
            jobDetailState = stale(jobDetailState, notice)
            affected = [.localStatus, .sourcesJobs]
			refreshesStatus = false
        case "settings":
            invalidateTasks([.settings])
            settingsState = stale(settingsState, notice)
            affected = [.settings]
			refreshesStatus = false
        case "lifecycle":
            cancelFeatureReadTasks()
            markFeatureStatesStale(notice)
            affected = Set(AppFeature.allCases.filter { $0 != .overview })
			refreshesStatus = false
        default:
            return
        }
		if affected.contains(selectedFeature), !requiresCoreRestart {
			refresh(selectedFeature)
		}
		if refreshesStatus, !requiresCoreRestart {
			if !statusOverviewState.isLoading {
				invalidateTasks([.statusOverview, .statusAccount])
				statusOverviewState = stale(statusOverviewState, notice)
				loadStatusOverview()
			}
		}
    }

    private func invalidateTasks(_ keys: Set<FeatureTaskKey>) {
        for key in keys {
            featureTasks[key]?.cancel()
            featureTasks[key] = nil
            featureGenerations[key, default: 0] &+= 1
            refreshAllPendingTasks.remove(key)
        }
        finishRefreshAllIfPossible()
    }

    private func resetFeatureState() {
		statusOverviewState = .idle
		statusUsageState = .idle
		statusInvocationState = .idle
        usageState = .idle
        invocationUsageState = .idle
        pricingCatalogState = .idle
        quotaState = .idle
        quotaPaceState = .idle
        quotaRefreshState = .idle
        resetCreditsRefreshState = .idle
        runtimeActionState = .idle
        sessionsState = .idle
        sessionDetailState = .idle
        projectsState = .idle
        projectDetailState = .idle
        sourcesState = .idle
        sourceDetailState = .idle
        jobsState = .idle
        jobDetailState = .idle
        healthProjectionState = .idle
        dataHealthState = .idle
        healthState = .idle
        healthDetailState = .idle
        settingsState = .idle
        settingsSaveState = .idle
        selectedSessionID = nil
        selectedProjectKey = nil
        selectedSourceKey = nil
        selectedJobID = nil
        selectedHealthEventID = nil
        consumedCursors.removeAll()
    }

	private func resetProviderFeatureState() {
		usageState = .idle
		invocationUsageState = .idle
		pricingCatalogState = .idle
		quotaState = .idle
		quotaPaceState = .idle
		sessionsState = .idle
		sessionDetailState = .idle
		projectsState = .idle
		projectDetailState = .idle
		selectedSessionID = nil
		selectedProjectKey = nil
		consumedCursors.removeAll()
	}

	private func loadStatusOverview() {
		let provider = statusProvider
		let previous = statusOverviewState.value
		invalidateTasks([.statusAccount])
		statusOverviewState = .loading(previous: previous)
		if provider == .cursor {
			statusUsageState = .loading(previous: statusUsageState.value)
			statusInvocationState = .loading(previous: statusInvocationState.value)
		}
		launch(
			.statusOverview,
			operation: { [runtime] in try await runtime.statusOverview(provider: provider) }
		) { [weak self] responses in
			guard let self,
				self.statusProvider == provider,
				responses.provider == provider
			else { return }
			let presentation = OverviewPresentation(responses)
			statusOverviewState = presentation.isPartial
				? .partial(presentation, notices: presentation.notices)
				: .ready(presentation)
			loadStatusAccount(for: responses, provider: provider)
			if provider == .cursor {
				statusUsageState = loadState(
					value: responses.todayUsage,
					meta: responses.todayUsage.meta,
					isEmpty: false
				)
				statusInvocationState = loadState(
					value: responses.todayInvocationUsage,
					meta: responses.todayInvocationUsage.meta,
					isEmpty: false
				)
			} else {
				statusUsageState = .idle
				statusInvocationState = .idle
			}
		} failure: { [weak self] error in
			guard let self, statusProvider == provider else { return }
			statusOverviewState = failedLoadState(previous: previous, error: error)
			if provider == .cursor {
				statusUsageState = failedLoadState(previous: statusUsageState.value, error: error)
				statusInvocationState = failedLoadState(
					previous: statusInvocationState.value,
					error: error
				)
			}
		}
	}

	private func loadStatusAccount(for responses: OverviewResponses, provider: AgentProvider) {
		launch(
			.statusAccount,
			operation: { [runtime] in try await runtime.accountSnapshot(provider: provider) }
		) { [weak self] account in
			guard let self, statusProvider == provider else { return }
			let presentation = OverviewPresentation(responses.replacingAccount(account))
			statusOverviewState = presentation.isPartial
				? .partial(presentation, notices: presentation.notices)
				: .ready(presentation)
		} failure: { _ in
			// Account data is optional; retain the already-published status overview.
		}
	}

    private func beginPage(_ key: FeatureTaskKey, cursor: String?, reset: Bool) -> Bool {
        if reset {
            consumedCursors[key] = []
            return true
        }
        guard let cursor, !cursor.isEmpty else { return false }
        return cursorIsAvailable(key, cursor: cursor)
    }

    private func cursorIsAvailable(_ key: FeatureTaskKey, cursor: String) -> Bool {
        !consumedCursors[key, default: []].contains(cursor)
    }

    private func completePage(_ key: FeatureTaskKey, cursor: String?) {
        guard let cursor, !cursor.isEmpty else { return }
        consumedCursors[key, default: []].insert(cursor)
    }

    private var paginationNotice: AppNotice {
        AppNotice(
            code: "pagination_cursor_repeated",
            messageKey: "app.notice.pagination_cursor_repeated",
            retryable: false
        )
    }

    private func stoppedPagination(
        _ response: Codexpulse_Core_V1_SessionListResponse
    ) -> FeatureLoadState<Codexpulse_Core_V1_SessionListResponse> {
        var response = response
        response.meta.page.hasMore_p = false
        response.meta.page.clearNextCursor()
        return .partial(response, notices: [paginationNotice])
    }

    private func stoppedPagination(
        _ response: Codexpulse_Core_V1_SessionDetailResponse
    ) -> FeatureLoadState<Codexpulse_Core_V1_SessionDetailResponse> {
        var response = response
        response.turnPage.hasMore_p = false
        response.turnPage.clearNextCursor()
        return .partial(response, notices: [paginationNotice])
    }

    private func stoppedPagination(
        _ response: Codexpulse_Core_V1_ProjectListResponse
    ) -> FeatureLoadState<Codexpulse_Core_V1_ProjectListResponse> {
        var response = response
        response.meta.page.hasMore_p = false
        response.meta.page.clearNextCursor()
        return .partial(response, notices: [paginationNotice])
    }

    private func stoppedPagination(
        _ response: Codexpulse_Core_V1_ProjectDetailResponse
    ) -> FeatureLoadState<Codexpulse_Core_V1_ProjectDetailResponse> {
        var response = response
        response.sessionPage.hasMore_p = false
        response.sessionPage.clearNextCursor()
        response.modelPage.hasMore_p = false
        response.modelPage.clearNextCursor()
        return .partial(response, notices: [paginationNotice])
    }

    private func stoppedPagination(
        _ response: Codexpulse_Core_V1_SourceListResponse
    ) -> FeatureLoadState<Codexpulse_Core_V1_SourceListResponse> {
        var response = response
        response.meta.page.hasMore_p = false
        response.meta.page.clearNextCursor()
        return .partial(response, notices: [paginationNotice])
    }

    private func stoppedPagination(
        _ response: Codexpulse_Core_V1_JobListResponse
    ) -> FeatureLoadState<Codexpulse_Core_V1_JobListResponse> {
        var response = response
        response.meta.page.hasMore_p = false
        response.meta.page.clearNextCursor()
        return .partial(response, notices: [paginationNotice])
    }

    private func stoppedPagination(
        _ response: Codexpulse_Core_V1_HealthListResponse
    ) -> FeatureLoadState<Codexpulse_Core_V1_HealthListResponse> {
        var response = response
        response.meta.page.hasMore_p = false
        response.meta.page.clearNextCursor()
        return .partial(response, notices: [paginationNotice])
    }

    private func markFeatureStatesStale(_ notice: AppNotice) {
		statusOverviewState = stale(statusOverviewState, notice)
        usageState = stale(usageState, notice)
        invocationUsageState = stale(invocationUsageState, notice)
        pricingCatalogState = stale(pricingCatalogState, notice)
        quotaState = stale(quotaState, notice)
        quotaPaceState = stale(quotaPaceState, notice)
        sessionsState = stale(sessionsState, notice)
        sessionDetailState = stale(sessionDetailState, notice)
        projectsState = stale(projectsState, notice)
        projectDetailState = stale(projectDetailState, notice)
        sourcesState = stale(sourcesState, notice)
        sourceDetailState = stale(sourceDetailState, notice)
        jobsState = stale(jobsState, notice)
        jobDetailState = stale(jobDetailState, notice)
        healthProjectionState = stale(healthProjectionState, notice)
        dataHealthState = stale(dataHealthState, notice)
        healthState = stale(healthState, notice)
        healthDetailState = stale(healthDetailState, notice)
        settingsState = stale(settingsState, notice)
    }

    private func stale<Value: Sendable>(
        _ state: FeatureLoadState<Value>,
        _ notice: AppNotice
    ) -> FeatureLoadState<Value> {
        if case .idle = state { return .idle }
        if let value = state.value { return .stale(value, notice: notice) }
        return .unavailable(notice)
    }
}
