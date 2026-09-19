import CodexPulseProtocolGenerated
import Combine
import Foundation

public enum AppFeature: String, CaseIterable, Hashable, Identifiable, Sendable {
    case overview
    case sessions
    case projects
    case invocationUsage
    case quotaUsage
    case apiSubscriptions
    case localStatus
    case sourcesJobs
    case settings
    case dashboardSummary

    public var id: String { rawValue }

    public func title(localization: AppLocalization) -> String {
        let key: String = switch self {
        case .overview: "feature.overview"
        case .sessions: "feature.sessions"
        case .projects: "feature.projects"
        case .quotaUsage: "feature.quotaUsage"
        case .invocationUsage: "feature.invocationUsage"
        case .apiSubscriptions: "feature.apiSubscriptions"
        case .localStatus: "feature.localStatus"
        case .sourcesJobs: "feature.sourcesJobs"
        case .settings: "feature.settings"
        case .dashboardSummary: "feature.dashboardSummary"
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
        case .apiSubscriptions: "creditcard.and.123"
        case .localStatus: "heart.text.square"
        case .sourcesJobs: "externaldrive.connected.to.line.below"
        case .settings: "gearshape"
        case .dashboardSummary: "square.grid.2x2"
        }
    }

	public static func usageFeatures(for provider: AgentProvider?) -> [AppFeature] {
        guard let provider else { return [] }
		let features: [AppFeature] = [.overview, .sessions, .projects, .invocationUsage, .quotaUsage]
		guard !provider.supportsInvocationStatistics else { return features }
		return features.filter { $0 != .invocationUsage }
	}

    public var requiresEnabledProvider: Bool {
        switch self {
        case .overview, .sessions, .projects, .quotaUsage, .invocationUsage:
            true
        case .apiSubscriptions, .localStatus, .sourcesJobs, .settings, .dashboardSummary:
            false
        }
    }
}

private enum FeatureTaskKey: Hashable {
    case usage, statusOverview, statusAccount, codexCardAccount, invocationUsage, pricingCatalog, quota, quotaAccount, quotaPace, dashboardSummary
    case quotaRefresh(AgentProvider), resetCreditsRefresh(AgentProvider)
    case apiSubscriptions, apiCredentialStatus, apiCredentialSave
    case runtimeAction
    case sessions, sessionDetail
    case projects, projectDetail
    case sources, sourceDetail
    case jobs, jobDetail
    case healthProjection, dataHealth, healthList, healthDetail
    case settings, settingsSave
    case codexSubscriptionList, codexSubscriptionMutate

    var isRead: Bool {
        switch self {
        case .quotaRefresh, .resetCreditsRefresh, .runtimeAction, .settingsSave, .apiCredentialSave,
            .codexSubscriptionMutate:
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
    @Published public private(set) var isGlobalRefreshing = false
    @Published public private(set) var globalRefreshPresentation: ProviderRefreshPresentation?
    @Published public var selectedFeature: AppFeature = .dashboardSummary
	@Published public private(set) var selectedProvider: AgentProvider?
	@Published public private(set) var statusProvider: AgentProvider?
    @Published public private(set) var providerCatalog: ProviderCatalog = .empty
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
    @Published public private(set) var dashboardRange: DateRangePreset = .today
    @Published public private(set) var dashboardTrendMode: DashboardTrendMode = .total

    @Published public private(set) var dashboardSummaryState:
        FeatureLoadState<Codexpulse_Core_V1_DashboardSummaryResponse> = .idle
    @Published public private(set) var usageState: FeatureLoadState<Codexpulse_Core_V1_UsageCostResponse> = .idle
	@Published public private(set) var statusUsageState: FeatureLoadState<Codexpulse_Core_V1_UsageCostResponse> = .idle
	@Published public private(set) var statusInvocationState: FeatureLoadState<Codexpulse_Core_V1_InvocationUsageResponse> = .idle
	@Published public private(set) var statusOverviewState: FeatureLoadState<OverviewPresentation> = .idle
    @Published public private(set) var invocationUsageState:
        FeatureLoadState<Codexpulse_Core_V1_InvocationUsageResponse> = .idle
    @Published public private(set) var pricingCatalogState:
        FeatureLoadState<Codexpulse_Core_V1_PricingCatalogCurrentResponse> = .idle
    @Published public private(set) var quotaState: FeatureLoadState<Codexpulse_Core_V1_QuotaCurrentResponse> = .idle
    @Published public private(set) var quotaAccountState:
        FeatureLoadState<Codexpulse_Core_V1_AccountSnapshotResponse> = .idle
    @Published private var codexCardAccountSnapshot: Codexpulse_Core_V1_AccountSnapshotResponse?
    @Published private var codexCardAccountIsLoading = false
    @Published public private(set) var quotaPaceState:
        FeatureLoadState<Codexpulse_Core_V1_QuotaPaceResponse> = .idle
    @Published public private(set) var apiSubscriptionsState:
        FeatureLoadState<Codexpulse_Core_V1_APISubscriptionsCurrentResponse> = .idle
    @Published public private(set) var apiCredentialStatus: APISubscriptionCredentialStatus?
    @Published public private(set) var apiCredentialActionState: ActionState = .idle
    @Published public var deepSeekAPIKeyDraft = ""
    @Published public var openCodeGoAPIKeyDraft = ""
    @Published public private(set) var quotaRefreshState: ActionState = .idle
    @Published public private(set) var resetCreditsRefreshState: ActionState = .idle
    private var quotaRefreshStates: [AgentProvider: ActionState] = [:]
    private var resetCreditsRefreshStates: [AgentProvider: ActionState] = [:]
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
    @Published public private(set) var codexSubscriptionAccountsState:
        FeatureLoadState<Codexpulse_Core_V1_CodexSubscriptionAccountsResponse> = .idle
    @Published public private(set) var codexSubscriptionActionState: CodexSubscriptionActionState = .idle
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
	private static let selectedFeatureKey = "CodexPulse.selectedFeature"
    private var startTask: Task<Void, Never>?
    private var updatePolicyTask: Task<Void, Never>?
    private var overviewRefreshTask: Task<Void, Never>?
    private var overviewRefreshGeneration: UInt64 = 0
    private var featureTasks: [FeatureTaskKey: Task<Void, Never>] = [:]
    private var featureGenerations: [FeatureTaskKey: UInt64] = [:]
    private var consumedCursors: [FeatureTaskKey: Set<String>] = [:]
    private var refreshAllPendingTasks: Set<FeatureTaskKey> = []
    private var refreshAllWaitsForOverview = false
    private var globalRefreshTask: Task<Void, Never>?
    private var latestRuntimeState: CoreConnectionState = .idle
	private var loadFeaturesOnNextOverview = true
	private var statusOverviewCache: [AgentProvider: OverviewPresentation] = [:]
	private var statusOverviewResponses: [AgentProvider: OverviewResponses] = [:]
	private let codexCardAccountRetryScheduler = CodexCardAccountRetryScheduler()
	private var codexCardAccountEpoch: UInt64 = 0
	private var codexCardAccountRetried = false
	private var statusConsistencyRefreshKey: CodexAccountContextKey?
	private var quotaConsistencyRefreshKey: CodexAccountContextKey?
	private var statusRefreshPendingAfterLoad = false
	private var statusRefreshPendingOnlyIndex = false
	private var statusUsageCache:
		[AgentProvider: Codexpulse_Core_V1_UsageCostResponse] = [:]
    private let codexSubscriptionDayBoundaryScheduler =
        CodexSubscriptionDayBoundaryScheduler()
    private var codexSubscriptionSnapshotGeneration: UInt64 = 0
    private var timeZoneObserver: NSObjectProtocol?
    private var observedTimeZoneIdentifier = TimeZone.current.identifier

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
		selectedFeature = Self.restoredFeature(
			defaults: providerDefaults,
			persists: persistsProviderSelection
		)
    }

    public convenience init(
        runtime: AppRuntime,
        observesUpdatePolicy: Bool = false
    ) {
        self.init(
            runtime: runtime,
            observesUpdatePolicy: observesUpdatePolicy,
            providerDefaults: .standard,
            persistsProviderSelection: false
        )
    }

    public init(
        runtime: AppRuntime,
        observesUpdatePolicy: Bool = false,
			providerDefaults: UserDefaults,
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
		selectedFeature = Self.restoredFeature(
			defaults: providerDefaults,
			persists: persistsProviderSelection
		)
    }

	private static func restoredFeature(defaults: UserDefaults, persists: Bool) -> AppFeature {
		guard persists else { return .dashboardSummary }
		if let raw = defaults.string(forKey: selectedFeatureKey),
		   let feature = AppFeature(rawValue: raw)
		{
			return feature
		}
		if defaults.object(forKey: selectedProviderKey) != nil {
			return .overview
		}
		return .dashboardSummary
	}

	private func persistSelectedFeature() {
		guard persistsProviderSelection else { return }
		providerDefaults.set(selectedFeature.rawValue, forKey: Self.selectedFeatureKey)
	}

    private var runtimeAcceptsProviderQueries: Bool {
        switch latestRuntimeState {
        case .normal, .partial, .stale:
            true
        default:
            false
        }
    }

    private func persistProvider(_ provider: AgentProvider?, key: String) {
        guard persistsProviderSelection else { return }
        if let provider {
            providerDefaults.set(provider.rawValue, forKey: key)
        } else {
            providerDefaults.removeObject(forKey: key)
        }
    }

    private func assignMainProvider(_ provider: AgentProvider?) {
        let previous = selectedProvider
        selectedProvider = provider
        persistProvider(provider, key: Self.selectedProviderKey)
        if let provider {
            if provider == .cursor, sessionOptions.sortField == "estimatedCost" {
                sessionOptions.sortField = "lastActivityAt"
            }
            if provider == .cursor, projectOptions.sortField == "estimatedCost" {
                projectOptions.sortField = "lastActivityAt"
            }
            if !provider.supportsInvocationStatistics, selectedFeature == .invocationUsage {
                selectedFeature = .overview
                persistSelectedFeature()
            }
            overviewRange = provider.defaultOverviewRange
        }
        guard previous != provider else { return }
		loadFeaturesOnNextOverview = true
        overviewRefreshGeneration &+= 1
        overviewRefreshTask?.cancel()
        overviewRefreshTask = nil
        cancelPageFeatureTasks()
        resetProviderFeatureState()
        quotaRefreshState = provider.flatMap { quotaRefreshStates[$0] } ?? .idle
        resetCreditsRefreshState = provider.flatMap { resetCreditsRefreshStates[$0] } ?? .idle
    }

    private func assignStatusProvider(_ provider: AgentProvider?) {
        guard provider != statusProvider else { return }
        invalidateTasks([.statusOverview, .statusAccount])
        statusRefreshPendingAfterLoad = false
        statusRefreshPendingOnlyIndex = false
        statusProvider = provider
        persistProvider(provider, key: Self.statusProviderKey)
        guard let provider else {
            statusOverviewState = .idle
            statusUsageState = .idle
            statusInvocationState = .idle
            return
        }
        statusOverviewState = .loading(previous: statusOverviewCache[provider])
        statusUsageState = provider.usesOfficialPeriodRing
            ? .loading(previous: statusUsageCache[provider])
            : .idle
        statusInvocationState = .idle
    }

    public var enabledProviders: [AgentProvider] {
        providerCatalog.enabledProviders
    }

    public var statusItemTitle: String {
        guard let statusProvider else {
            return "Codex Pulse --"
        }
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

    public var statusAccountCardSummary: PopoverAccountSummaryPresentation? {
        guard let presentation = statusPresentation else { return nil }
        guard statusProvider == .codex else { return presentation.popoverAccountSummary }
        guard let key = presentation.codexAccountContextKey,
              let snapshot = codexCardAccountSnapshot,
              CodexAccountContext.key(fromAccount: snapshot) == key
        else { return nil }
        let summary = PopoverAccountSummaryPresentation(
            account: CodexAccountPresentation(snapshot),
            snapshot: snapshot
        )
        return summary.availability == .available ? summary : nil
    }

    public var statusAccountCardFallbackText: String {
        guard statusProvider == .codex else {
            return statusPresentation?.account.accessibilityLabel
                ?? localization.textValue("正在读取 Codex 账户与套餐信息")
        }
        return codexCardFallbackText(quota: statusOverviewResponses[.codex]?.quota)
    }

    public var quotaAccountCardSummary: PopoverAccountSummaryPresentation? {
        guard selectedProvider == .codex,
              let quota = quotaState.value,
              let snapshot = codexCardAccountSnapshot
        else { return nil }
        guard let summary = CodexQuotaAccountSummaryCopy.summary(quota: quota, snapshot: snapshot),
              summary.availability == .available
        else { return nil }
        return summary
    }

    public var quotaAccountCardFallbackText: String {
        codexCardFallbackText(quota: quotaState.value)
    }

    private func codexCardFallbackText(
        quota: Codexpulse_Core_V1_QuotaCurrentResponse?
    ) -> String {
        if let quota, quota.current.hasBinding {
            switch quota.current.binding.state {
            case "pending": return localization.textValue("账号确认中")
            case "signed_out": return localization.textValue("当前没有 Codex 账户信息")
            default: break
            }
        }
        if let account = quotaAccountState.value, account.hasBinding,
           account.binding.state == "pending" {
            return localization.textValue("账号确认中")
        }
        if codexCardAccountIsLoading || quotaAccountState.isLoading {
            return localization.textValue("账号确认中")
        }
        return localization.textValue("Codex 账户与套餐信息暂不可用")
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
        if isGlobalRefreshing { return true }
        return switch feature {
        case .overview:
            isOverviewRefreshing
        case .sessions:
            sessionsState.isLoading || sessionDetailState.isLoading
        case .projects:
            projectsState.isLoading || projectDetailState.isLoading
        case .quotaUsage:
            quotaState.isLoading || quotaPaceState.isLoading ||
                usageState.isLoading || pricingCatalogState.isLoading
        case .apiSubscriptions:
            apiSubscriptionsState.isLoading
        case .invocationUsage:
            invocationUsageState.isLoading
        case .localStatus:
            healthProjectionState.isLoading || dataHealthState.isLoading ||
                healthState.isLoading || healthDetailState.isLoading
        case .sourcesJobs:
            sourcesState.isLoading || sourceDetailState.isLoading ||
                jobsState.isLoading || jobDetailState.isLoading
        case .settings:
            settingsState.isLoading || codexSubscriptionAccountsState.isLoading
        case .dashboardSummary:
            dashboardSummaryState.isLoading
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
            await runtime.setCatalogSink { [weak self] catalog in
                await self?.applyProviderCatalog(catalog)
            }
            await runtime.start()
            if self.observesUpdatePolicy {
                self.loadSettings()
            }
            self.startTimeZoneObservationIfNeeded()
            self.startTask = nil
        }
    }

	public func selectProvider(_ provider: AgentProvider) {
        if !providerCatalog.states.isEmpty, !enabledProviders.contains(provider) {
            return
        }
		guard provider != selectedProvider else { return }
        assignMainProvider(provider)
		if selectedFeature == .overview {
			state = .loading(localization.textValue("正在切换客户端…"))
		} else {
			load(selectedFeature)
		}
		Task { [weak self, runtime] in
			await runtime.selectProvider(provider)
			guard let self, self.selectedProvider == provider else { return }
		}
	}

	public func selectStatusProvider(_ provider: AgentProvider) {
        if !providerCatalog.states.isEmpty, !enabledProviders.contains(provider) {
            return
        }
		guard provider != statusProvider else { return }
        assignStatusProvider(provider)
		loadStatusOverview()
	}

    @discardableResult
    public func applyProviderCatalog(_ catalog: ProviderCatalog) -> AgentProvider? {
        providerCatalog = catalog
        let resolved = catalog.resolvedSelection(preferred: selectedProvider)
        let resolvedStatus = catalog.resolvedSelection(preferred: statusProvider)
        if resolved != selectedProvider {
            assignMainProvider(resolved)
        }
        if resolvedStatus != statusProvider {
            assignStatusProvider(resolvedStatus)
            if runtimeAcceptsProviderQueries, resolvedStatus != nil {
                loadStatusOverview()
            }
        }
        return resolved
    }

	public func refreshStatusProvider() {
		runGlobalManualRefresh { [weak self] in
			self?.loadStatusOverview()
		}
	}

	public func openMainWindowProvider(_ provider: AgentProvider) {
		selectProvider(provider)
		navigate(to: .overview)
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
        runGlobalManualRefresh { [weak self] in
            self?.reloadFeature(feature)
        }
    }

    private func reloadFeature(_ feature: AppFeature) {
        switch feature {
        case .overview: refreshOrRestart()
        case .sessions: loadSessions(reset: true)
        case .projects: loadProjects(reset: true)
        case .quotaUsage: loadQuotaAndUsage()
        case .apiSubscriptions: loadAPISubscriptions()
        case .invocationUsage: loadInvocationUsage()
        case .localStatus: loadLocalStatus()
        case .sourcesJobs: loadSourcesAndJobs(reset: true)
        case .settings: loadSettings()
        case .dashboardSummary: loadDashboardSummary()
        }
        reloadSelectedDetails(for: feature, onlyIfNeeded: false)
    }

    public func load(_ feature: AppFeature) {
        if selectedFeature != feature {
            cancelCodexSubscriptionDayBoundaryReload()
        }
        selectedFeature = feature
        guard canRefreshOrRestart else { return }
        if feature.requiresEnabledProvider, selectedProvider == nil {
            return
        }
        switch feature {
        case .overview: break
        case .sessions:
            if sessionsState.shouldReloadOnNavigation { loadSessions(reset: true) }
        case .projects:
            if projectsState.shouldReloadOnNavigation { loadProjects(reset: true) }
        case .quotaUsage:
            if quotaState.shouldReloadOnNavigation || quotaPaceState.shouldReloadOnNavigation ||
                usageState.shouldReloadOnNavigation ||
                pricingCatalogState.shouldReloadOnNavigation ||
                (selectedProvider == .codex && quotaAccountState.shouldReloadOnNavigation)
            {
                loadQuotaAndUsage()
            }
        case .apiSubscriptions:
            if apiSubscriptionsState.shouldReloadOnNavigation { loadAPISubscriptions() }
        case .invocationUsage:
            if invocationUsageState.shouldReloadOnNavigation { loadInvocationUsage() }
        case .localStatus:
            if dataHealthState.shouldReloadOnNavigation || healthState.shouldReloadOnNavigation { loadLocalStatus() }
        case .sourcesJobs:
            if sourcesState.shouldReloadOnNavigation || jobsState.shouldReloadOnNavigation {
                loadSourcesAndJobs(reset: true)
            }
        case .settings:
            if settingsState.shouldReloadOnNavigation {
                loadSettings()
            } else {
                loadCodexSubscriptionAccountsIfNeeded()
            }
        case .dashboardSummary:
            if dashboardSummaryState.shouldReloadOnNavigation { loadDashboardSummary() }
        }
        reloadSelectedDetails(for: feature, onlyIfNeeded: true)
    }

    public func navigate(to feature: AppFeature) {
        let destination = feature == .invocationUsage &&
            selectedProvider?.supportsInvocationStatistics != true
            ? AppFeature.overview
            : feature
        if selectedFeature != destination {
            cancelCodexSubscriptionDayBoundaryReload()
        }
		selectedFeature = destination
		persistSelectedFeature()
        load(destination)
    }

    public func selectDashboardRange(_ range: DateRangePreset) {
        guard [.today, .sevenDays, .thirtyDays].contains(range),
              range != dashboardRange,
              canRefreshOrRestart
        else { return }
        dashboardRange = range
        loadDashboardSummary()
    }

    public func selectDashboardTrendMode(_ mode: DashboardTrendMode) {
        dashboardTrendMode = mode
    }

    public func loadDashboardSummary() {
        let previous = dashboardSummaryState.value
        dashboardSummaryState = .loading(previous: previous)
        let range = dashboardRange
        launch(
            .dashboardSummary,
            operation: { [runtime] in
                try await runtime.dashboardSummary(
                    FeatureRequestFactory.dashboardSummary(range: range)
                )
            }
        ) { [weak self] response in
            guard let self, self.dashboardRange == range else { return }
            self.dashboardSummaryState = loadState(
                value: response,
                meta: response.meta,
                isEmpty: false
            )
        } failure: { [weak self] error in
            self?.dashboardSummaryState = failedLoadState(previous: previous, error: error)
        }
    }

    public func navigateToInvocationUsageFromOverview() {
		guard selectedProvider?.supportsInvocationStatistics == true else { return }
        let contextChanged = invocationRange != overviewRange || invocationSourceClass != "all"
        invocationRange = overviewRange
        invocationSourceClass = "all"
        selectedFeature = .invocationUsage
		persistSelectedFeature()
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
        refreshAllWaitsForOverview = true
        runGlobalManualRefresh { [weak self] in
            guard let self else { return }
            self.refreshAllPendingTasks.removeAll()
            self.refreshAllWaitsForOverview = true
            self.loadSessions(reset: true)
            self.loadProjects(reset: true)
            self.loadQuotaAndUsage()
            self.loadAPISubscriptions()
            if self.selectedProvider?.supportsInvocationStatistics == true {
                self.loadInvocationUsage()
            }
            self.loadLocalStatus()
            self.loadSourcesAndJobs(reset: true)
            self.loadSettings()
            self.loadDashboardSummary()
            for feature in [AppFeature.sessions, .projects, .localStatus, .sourcesJobs] {
                self.reloadSelectedDetails(for: feature, onlyIfNeeded: false)
            }
            self.refresh()
            self.finishRefreshAllIfPossible()
        }
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
        case .overview, .quotaUsage, .invocationUsage, .apiSubscriptions, .settings, .dashboardSummary:
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
        cancelTimeZoneObservation()
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
        guard let provider = selectedProvider else { return }
        let previous = sessionsState.value
        let cursor = reset ? nil : previous?.meta.page.nextCursor
        guard reset || (previous.map { pageHasMore($0.meta) } == true) else { return }
        guard beginPage(.sessions, cursor: cursor, reset: reset) else {
            if let previous { sessionsState = stoppedPagination(previous) }
            return
        }
        sessionsState = .loading(previous: previous)
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
		guard let provider = selectedProvider else { return }
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
        guard let provider = selectedProvider else { return }
        let previous = projectsState.value
        let cursor = reset ? nil : previous?.meta.page.nextCursor
        guard reset || (previous.map { pageHasMore($0.meta) } == true) else { return }
        guard beginPage(.projects, cursor: cursor, reset: reset) else {
            if let previous { projectsState = stoppedPagination(previous) }
            return
        }
        projectsState = .loading(previous: previous)
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
        guard let provider = selectedProvider else { return }
        let request = FeatureRequestFactory.projectDetail(
            dimensionKey: dimensionKey,
            range: projectOptions.range,
			provider: provider,
            exactRange: projectOptions.exactRange,
            sessionCursor: sessionCursor,
            modelCursor: modelCursor
        )
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
        if selectedProvider == .codex {
            loadQuotaAccount()
        } else {
            quotaAccountState = .idle
        }
    }

    public func loadUsage() {
        guard let provider = selectedProvider else { return }
        let previous = usageState.value
        usageState = .loading(previous: previous)
		let request = FeatureRequestFactory.usage(range: usageRange, provider: provider)
        launch(.usage, operation: { [runtime] in try await runtime.usageCost(request) }) { [weak self] response in
			guard response.providerContext.effectiveProvider == provider.rawValue else { return }
			self?.usageState = loadState(value: response, meta: response.meta, isEmpty: false)
        } failure: { [weak self] error in
            self?.usageState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadAPISubscriptions() {
        let previous = apiSubscriptionsState.value
        apiSubscriptionsState = .loading(previous: previous)
        launch(
            .apiSubscriptions,
            operation: { [runtime] in try await runtime.apiSubscriptionsCurrent() }
        ) { [weak self] response in
            self?.apiSubscriptionsState = .ready(response)
        } failure: { [weak self] error in
            self?.apiSubscriptionsState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadAPICredentialStatus() {
        launch(
            .apiCredentialStatus,
            operation: { [runtime] in try await runtime.apiCredentialStatus() }
        ) { [weak self] status in
            self?.apiCredentialStatus = status
        } failure: { [weak self] error in
            self?.apiCredentialActionState = .unavailable(AppNotice.from(error))
        }
    }

    public func saveAPICredential(_ service: APISubscriptionCredentialService) {
        let key = switch service {
        case .deepSeek: deepSeekAPIKeyDraft
        case .openCodeGo: openCodeGoAPIKeyDraft
        }
        guard !key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        updateAPICredential(service, key: key)
    }

    public func deleteAPICredential(_ service: APISubscriptionCredentialService) {
        updateAPICredential(service, key: nil)
    }

    private func updateAPICredential(
        _ service: APISubscriptionCredentialService,
        key: String?
    ) {
        guard canRefreshOrRestart, !requiresCoreRestart else { return }
        apiCredentialActionState = .running
        launch(
            .apiCredentialSave,
            operation: { [runtime] in
                try await runtime.updateAPICredential(service: service, key: key)
            }
        ) { [weak self] status in
            guard let self else { return }
            apiCredentialStatus = status
            switch service {
            case .deepSeek: deepSeekAPIKeyDraft = ""
            case .openCodeGo: openCodeGoAPIKeyDraft = ""
            }
            apiCredentialActionState = .succeeded(key == nil ? "deleted" : "saved")
            apiSubscriptionsState = .idle
            if selectedFeature == .apiSubscriptions { loadAPISubscriptions() }
        } failure: { [weak self] error in
            self?.apiCredentialActionState = .unavailable(AppNotice.from(error))
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
		guard let provider = selectedProvider, provider.supportsInvocationStatistics else {
			invocationUsageState = .idle
			return
		}
        let previous = invocationUsageState.value
        invocationUsageState = .loading(previous: previous)
        let quotaCycleRange = [.quotaWeek, .quotaMonth].contains(invocationRange)
            ? availableInvocationQuotaCycleRange : nil
        invocationRangeFellBackFromQuotaWeek = invocationRange == .quotaWeek && quotaCycleRange == nil
        let request = FeatureRequestFactory.invocationUsage(
            range: invocationRange,
            sourceClass: invocationSourceClass,
			provider: provider,
            quotaCycleRange: quotaCycleRange
        )
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
        guard let provider = selectedProvider else { return }
        let previous = pricingCatalogState.value
        pricingCatalogState = .loading(previous: previous)
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
        guard let provider = selectedProvider else { return }
        let previous = quotaState.value
        quotaState = .loading(previous: previous)
		let request = FeatureRequestFactory.quota(provider: provider, now: now)
        launch(.quota, operation: { [runtime] in try await runtime.quotaCurrent(request) }) { [weak self] response in
			guard response.providerContext.effectiveProvider == provider.rawValue else { return }
            self?.quotaState = loadState(
                value: response,
                meta: response.meta,
                isEmpty: response.current.windows.isEmpty && response.current.sources.isEmpty
            )
            if let self {
                switch quotaAccountState {
                case .ready(let account), .partial(let account, _):
                    acceptCodexCardAccount(account, matching: response)
                default: break
                }
            }
        } failure: { [weak self] error in
            self?.quotaState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadQuotaPace(now: Date = Date()) {
        guard let provider = selectedProvider else { return }
        let previous = quotaPaceState.value
        quotaPaceState = .loading(previous: previous)
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

    public func loadQuotaAccount() {
        guard let provider = selectedProvider, provider == .codex else {
            quotaAccountState = .idle
            return
        }
        let previous = quotaAccountState.value
        quotaAccountState = .loading(previous: previous)
        codexCardAccountIsLoading = true
        launch(
            .quotaAccount,
            operation: { [runtime] in try await runtime.accountSnapshot(provider: provider) }
        ) { [weak self] response in
            guard let self, selectedProvider == provider else { return }
            quotaAccountState = .ready(response)
            codexCardAccountIsLoading = false
            if !response.hasAccount { codexCardAccountRetried = true }
            if let quota = quotaState.value, !quotaState.isLoading {
                if CodexAccountContext.key(fromQuota: quota) == CodexAccountContext.key(fromAccount: response) {
                    if !response.hasAccount { codexCardAccountSnapshot = nil }
                    acceptCodexCardAccount(response, matching: quota)
                    quotaConsistencyRefreshKey = nil
                } else if let key = CodexAccountContext.key(fromQuota: quota),
                          quotaConsistencyRefreshKey != key {
                    discardCodexCardAccount(matching: key)
                    quotaConsistencyRefreshKey = key
                    loadQuota()
                    scheduleCodexCardAccountRetry()
                }
            }
            scheduleCodexSubscriptionDayBoundaryReloadIfNeeded()
        } failure: { [weak self] error in
            guard let self, selectedProvider == provider else { return }
            quotaAccountState = failedLoadState(previous: previous, error: error)
            codexCardAccountIsLoading = false
            scheduleCodexCardAccountRetry()
        }
    }

    public func requestQuotaRefresh(source: String) {
        guard canRefreshOrRestart else { return }
		guard let provider = selectedProvider else { return }
		guard source == "quota" || provider.supportsResetCredits else { return }
        guard let taskKey = refreshTaskKey(source: source, provider: provider) else { return }
        if isRefreshRunning(source: source, provider: provider) { return }
        setRefreshState(.running, source: source, provider: provider)
        launch(taskKey, operation: { [runtime] in
			let receipt = try await runtime.requestQuotaRefresh(source: source, provider: provider)
			guard receipt.providerContext.effectiveProvider == provider.rawValue else {
				throw AppRuntimeError.unavailable
			}
			return receipt
        }) { [weak self] receipt in
            guard let self else { return }
            let title = source == "quota" ? "额度" : "重置次数"
            let result: ActionState = receipt.fetched
                ? .succeeded("\(title)已更新")
                : .skipped(QuotaRefreshFeedback.skippedText(
                    title: title, reason: receipt.reason,
                    nextDueAtMS: receipt.hasNextDueAtMs ? receipt.nextDueAtMs : nil
                ))
            setRefreshState(result, source: source, provider: provider)
            guard selectedProvider == provider else { return }
            let now = Date()
            loadQuota(now: now)
            loadQuotaPace(now: now)
            if provider == .codex {
                loadQuotaAccount()
            }
        } failure: { [weak self] error in
            self?.setRefreshState(.unavailable(AppNotice.from(error)), source: source, provider: provider)
        }
    }

    private func refreshTaskKey(source: String, provider: AgentProvider) -> FeatureTaskKey? {
        switch source {
        case "quota": .quotaRefresh(provider)
        case "reset_credits": .resetCreditsRefresh(provider)
        default: nil
        }
    }

    private func isRefreshRunning(source: String, provider: AgentProvider) -> Bool {
        switch source {
        case "quota":
            if case .running = quotaRefreshStates[provider] { return true }
        case "reset_credits":
            if case .running = resetCreditsRefreshStates[provider] { return true }
        default:
            break
        }
        return false
    }

    private func setRefreshState(_ state: ActionState, source: String, provider: AgentProvider) {
        switch source {
        case "quota":
            quotaRefreshStates[provider] = state
            if selectedProvider == provider { quotaRefreshState = state }
        case "reset_credits":
            resetCreditsRefreshStates[provider] = state
            if selectedProvider == provider { resetCreditsRefreshState = state }
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
        loadAPICredentialStatus()
        loadCodexSubscriptionAccounts()
        let previous = settingsState.value
        let draftAtStart = settingsDraft
        let hadUnsavedChanges = draftAtStart != nil && previous.map(SettingsDraft.init) != draftAtStart
        settingsState = .loading(previous: previous)
        launch(.settings, operation: { [runtime] in try await runtime.settings() }) { [weak self] response in
            guard let self else { return }
            settingsState = loadState(value: response, meta: response.meta, isEmpty: false)
            _ = applyProviderCatalog(ProviderCatalog(response))
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
                    _ = applyProviderCatalog(ProviderCatalog(readback))
                    settingsDraft = pendingDraft ?? draft
                    settingsSaveState = .conflict
                    return
                }
                settingsState = loadState(value: readback, meta: readback.meta, isEmpty: false)
                _ = applyProviderCatalog(ProviderCatalog(readback))
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
                    _ = applyProviderCatalog(ProviderCatalog(readback))
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

    public func loadCodexSubscriptionAccounts() {
        let previous = codexSubscriptionAccountsState.value
        let snapshotGeneration = beginCodexSubscriptionSnapshotRead()
        codexSubscriptionAccountsState = .loading(previous: previous)
        launch(
            .codexSubscriptionList,
            operation: { [runtime] in try await runtime.listCodexSubscriptionAccounts() }
        ) { [weak self] response in
            guard let self,
                  codexSubscriptionSnapshotGeneration == snapshotGeneration
            else { return }
            codexSubscriptionAccountsState = .ready(response)
            scheduleCodexSubscriptionDayBoundaryReloadIfNeeded()
        } failure: { [weak self] error in
            guard let self,
                  codexSubscriptionSnapshotGeneration == snapshotGeneration
            else { return }
            codexSubscriptionAccountsState = failedLoadState(previous: previous, error: error)
        }
    }

    public func createCodexSubscriptionAccount(
        _ draft: CodexSubscriptionManualDraft,
        manualEntryID: String
    ) {
        guard !draft.standaloneEmailMissing else { return }
        var request = Codexpulse_Core_V1_CreateCodexSubscriptionAccountRequest()
        request.manualEntryID = manualEntryID
        request.manual = draft.makeFields()
        let preparedRequest = request
        mutateCodexSubscription(
            expectedReadback: {
                CodexSubscriptionReadback.containsAccount(
                    $0,
                    manualEntryID: preparedRequest.manualEntryID,
                    detected: false,
                    hasManual: true,
                    linked: false,
                    fields: preparedRequest.manual
                )
            },
            operation: { [runtime] in
                try await runtime.createCodexSubscriptionAccount(preparedRequest)
            }
        )
    }

    public func updateCodexSubscriptionAccount(
        _ account: Codexpulse_Core_V1_CodexSubscriptionAccount,
        draft: CodexSubscriptionManualDraft
    ) {
        var request = Codexpulse_Core_V1_UpdateCodexSubscriptionAccountRequest()
        request.accountID = account.accountID
        request.manual = draft.makeFields()
        if let newManualEntryID = draft.newManualEntryID, !newManualEntryID.isEmpty {
            request.newManualEntryID = newManualEntryID
        }
        if account.hasManualRevision {
            request.expectedManualRevision = account.manualRevision
        }
        if account.hasLinkRevision {
            request.expectedLinkRevision = account.linkRevision
        }
        let preparedRequest = request
        let detectedAccountID = account.hasDetectedAccountID ? account.detectedAccountID : nil
        let manualEntryID = account.hasManualEntryID ? account.manualEntryID : nil
        let newManualEntryID = preparedRequest.hasNewManualEntryID
            ? preparedRequest.newManualEntryID
            : nil
        mutateCodexSubscription(
            expectedReadback: {
                CodexSubscriptionReadback.containsAccount(
                    $0,
                    detectedAccountID: detectedAccountID,
                    manualEntryID: newManualEntryID ?? manualEntryID,
                    detected: account.detected,
                    hasManual: true,
                    linked: account.linked || newManualEntryID != nil,
                    fields: preparedRequest.manual
                )
            },
            operation: { [runtime] in
                try await runtime.updateCodexSubscriptionAccount(preparedRequest)
            }
        )
    }

    public func deleteCodexSubscriptionAccount(
        _ account: Codexpulse_Core_V1_CodexSubscriptionAccount
    ) {
        guard !account.current else { return }
        var request = Codexpulse_Core_V1_DeleteCodexSubscriptionAccountRequest()
        request.accountID = account.accountID
        if account.hasDetectedRevision {
            request.expectedDetectedRevision = account.detectedRevision
        }
        if account.hasManualRevision {
            request.expectedManualRevision = account.manualRevision
        }
        if account.hasLinkRevision {
            request.expectedLinkRevision = account.linkRevision
        }
        let preparedRequest = request
        mutateCodexSubscription(
            expectedReadback: {
                CodexSubscriptionReadback.excludesAccount(
                    $0,
                    accountID: preparedRequest.accountID
                )
            },
            operation: { [runtime] in
                try await runtime.deleteCodexSubscriptionAccount(preparedRequest)
            }
        )
    }

    public func linkCodexSubscriptionAccount(
        detectedAccountID: String,
        manualEntryID: String,
        expectedManualRevision: Int64
    ) {
        var request = Codexpulse_Core_V1_LinkCodexSubscriptionAccountRequest()
        request.detectedAccountID = detectedAccountID
        request.manualEntryID = manualEntryID
        request.expectedManualRevision = expectedManualRevision
        let preparedRequest = request
        mutateCodexSubscription(
            expectedReadback: {
                CodexSubscriptionReadback.containsAccount(
                    $0,
                    detectedAccountID: preparedRequest.detectedAccountID,
                    manualEntryID: preparedRequest.manualEntryID,
                    detected: true,
                    hasManual: true,
                    linked: true
                )
            },
            operation: { [runtime] in
                try await runtime.linkCodexSubscriptionAccount(preparedRequest)
            }
        )
    }

    public func unlinkCodexSubscriptionAccount(
        _ account: Codexpulse_Core_V1_CodexSubscriptionAccount
    ) {
        guard account.hasDetectedAccountID, account.hasManualEntryID else { return }
        if !account.hasManualEmail
            || account.manualEmail.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        {
            return
        }
        var request = Codexpulse_Core_V1_UnlinkCodexSubscriptionAccountRequest()
        request.detectedAccountID = account.detectedAccountID
        request.manualEntryID = account.manualEntryID
        if account.hasManualRevision {
            request.expectedManualRevision = account.manualRevision
        }
        if account.hasLinkRevision {
            request.expectedLinkRevision = account.linkRevision
        }
        let preparedRequest = request
        mutateCodexSubscription(
            expectedReadback: {
                CodexSubscriptionReadback.containsAccount(
                    $0, detectedAccountID: preparedRequest.detectedAccountID,
                    detected: true, hasManual: false, linked: false
                ) && CodexSubscriptionReadback.containsAccount(
                    $0, manualEntryID: preparedRequest.manualEntryID,
                    detected: false, hasManual: true, linked: false
                )
            },
            operation: { [runtime] in
                try await runtime.unlinkCodexSubscriptionAccount(preparedRequest)
            }
        )
    }

    public func linkLegacyQuotaHistory(
        _ account: Codexpulse_Core_V1_CodexSubscriptionAccount
    ) {
        guard account.current,
              account.detected,
              account.hasDetectedAccountID,
              account.hasDetectedRevision,
              account.hasLegacyQuotaHistory,
              account.legacyQuotaHistory.state == .available
        else { return }
        var request = Codexpulse_Core_V1_LinkLegacyQuotaHistoryRequest()
        request.detectedAccountID = account.detectedAccountID
        request.expectedDetectedRevision = account.detectedRevision
        let preparedRequest = request
        mutateCodexSubscription(
            expectedReadback: {
                CodexSubscriptionReadback.containsLegacyQuotaHistory(
                    $0,
                    detectedAccountID: preparedRequest.detectedAccountID,
                    state: .linked
                )
            },
            operation: { [runtime] in
                try await runtime.linkLegacyQuotaHistory(preparedRequest)
            }
        )
    }

    public func unlinkLegacyQuotaHistory(
        _ account: Codexpulse_Core_V1_CodexSubscriptionAccount
    ) {
        guard account.detected,
              account.hasDetectedAccountID,
              account.hasDetectedRevision,
              account.hasLegacyQuotaHistory,
              account.legacyQuotaHistory.state == .linked,
              account.legacyQuotaHistory.hasAssociationRevision
        else { return }
        var request = Codexpulse_Core_V1_UnlinkLegacyQuotaHistoryRequest()
        request.detectedAccountID = account.detectedAccountID
        request.expectedDetectedRevision = account.detectedRevision
        request.expectedAssociationRevision = account.legacyQuotaHistory.associationRevision
        let preparedRequest = request
        mutateCodexSubscription(
            expectedReadback: {
                CodexSubscriptionReadback.containsLegacyQuotaHistory(
                    $0,
                    detectedAccountID: preparedRequest.detectedAccountID,
                    state: .available
                )
            },
            operation: { [runtime] in
                try await runtime.unlinkLegacyQuotaHistory(preparedRequest)
            }
        )
    }

    public func handleSystemTimeZoneChange() {
        let identifier = TimeZone.current.identifier
        observedTimeZoneIdentifier = identifier
        cancelCodexSubscriptionDayBoundaryReload()
        switch selectedFeature {
        case .settings:
            loadCodexSubscriptionAccounts()
        case .quotaUsage where selectedProvider == .codex:
            loadQuotaAccount()
        default:
            break
        }
    }

    private func mutateCodexSubscription(
        expectedReadback: @escaping @Sendable (
            Codexpulse_Core_V1_CodexSubscriptionAccountsResponse
        ) -> Bool,
        operation: @escaping @Sendable () async throws -> Codexpulse_Core_V1_CodexSubscriptionMutationReceipt
    ) {
        guard canRefreshOrRestart, !requiresCoreRestart else { return }
        if case .running = codexSubscriptionActionState { return }
        codexSubscriptionActionState = .running
        let generation = beginTask(.codexSubscriptionMutate)
        let runtime = runtime
        featureTasks[.codexSubscriptionMutate] = Task { [weak self] in
            guard let self else { return }
            do {
                let receipt = try await operation()
                invalidateTasks([.codexSubscriptionList])
                let snapshotGeneration = beginCodexSubscriptionSnapshotRead()
                let readback = try await runtime.listCodexSubscriptionAccounts()
                try Task.checkCancellation()
                guard isCurrent(.codexSubscriptionMutate, generation: generation) else { return }
                finishTask(.codexSubscriptionMutate)
                guard codexSubscriptionSnapshotGeneration == snapshotGeneration else {
                    codexSubscriptionActionState = .unavailable(
                        codexSubscriptionReadbackNotice(code: "mutation_readback_superseded")
                    )
                    return
                }
                applyCodexSubscriptionMutation(
                    receipt,
                    readback: readback,
                    expectedReadback: expectedReadback
                )
            } catch {
                guard isCurrent(.codexSubscriptionMutate, generation: generation) else { return }
                finishTask(.codexSubscriptionMutate)
                if error is CancellationError { return }
                codexSubscriptionActionState = .unavailable(AppNotice.from(error))
            }
        }
    }

    private func applyCodexSubscriptionMutation(
        _ receipt: Codexpulse_Core_V1_CodexSubscriptionMutationReceipt,
        readback: Codexpulse_Core_V1_CodexSubscriptionAccountsResponse,
        expectedReadback: (Codexpulse_Core_V1_CodexSubscriptionAccountsResponse) -> Bool
    ) {
        codexSubscriptionAccountsState = .ready(readback)
        scheduleCodexSubscriptionDayBoundaryReloadIfNeeded()
        switch receipt.result {
        case .applied:
            codexSubscriptionActionState = expectedReadback(readback)
                ? .applied
                : .unavailable(codexSubscriptionReadbackNotice(code: "mutation_readback_mismatch"))
        case .noop:
            codexSubscriptionActionState = expectedReadback(readback)
                ? .noop
                : .unavailable(codexSubscriptionReadbackNotice(code: "mutation_readback_mismatch"))
        case .conflict:
            let reason = receipt.hasReason ? receipt.reason : ""
            codexSubscriptionActionState = .conflict(reason: reason)
        case .unspecified, .UNRECOGNIZED:
            codexSubscriptionActionState = .unavailable(
                AppNotice(
                    code: "mutation_result_unknown",
                    messageKey: "app.error.mutation_result_unknown",
                    retryable: true
                )
            )
        }
    }

    private func beginCodexSubscriptionSnapshotRead() -> UInt64 {
        codexSubscriptionSnapshotGeneration &+= 1
        return codexSubscriptionSnapshotGeneration
    }

    private func codexSubscriptionReadbackNotice(code: String) -> AppNotice {
        AppNotice(
            code: code,
            messageKey: "app.error.subscription_readback",
            retryable: true
        )
    }

    private func loadCodexSubscriptionAccountsIfNeeded() {
        if codexSubscriptionAccountsState.shouldReloadOnNavigation {
            loadCodexSubscriptionAccounts()
        } else {
            scheduleCodexSubscriptionDayBoundaryReloadIfNeeded()
        }
    }

    private func scheduleCodexSubscriptionDayBoundaryReloadIfNeeded() {
        guard selectedFeature == .settings ||
            (selectedFeature == .quotaUsage && selectedProvider == .codex)
        else { return }
        let timeZone = TimeZone.current
        observedTimeZoneIdentifier = timeZone.identifier
        let now = Date()
        let next = CodexSubscriptionCalendar.nextLocalDayBoundary(now: now, timeZone: timeZone)
        let delay = next.timeIntervalSince(now)
        guard delay > 0 else {
            reloadCodexSubscriptionDateStateForSelectedFeature()
            return
        }
        codexSubscriptionDayBoundaryScheduler.schedule(
            at: next,
            timeZoneIdentifier: timeZone.identifier
        ) { [weak self] in
            guard let self else { return }
            if TimeZone.current.identifier != self.observedTimeZoneIdentifier {
                self.handleSystemTimeZoneChange()
                return
            }
            self.reloadCodexSubscriptionDateStateForSelectedFeature()
        }
    }

    private func reloadCodexSubscriptionDateStateForSelectedFeature() {
        switch selectedFeature {
        case .settings:
            loadCodexSubscriptionAccounts()
        case .quotaUsage where selectedProvider == .codex:
            loadQuotaAccount()
        default:
            cancelCodexSubscriptionDayBoundaryReload()
        }
    }

    private func cancelCodexSubscriptionDayBoundaryReload() {
        codexSubscriptionDayBoundaryScheduler.cancel()
    }

    private func startTimeZoneObservationIfNeeded() {
        guard timeZoneObserver == nil else { return }
        timeZoneObserver = NotificationCenter.default.addObserver(
            forName: .NSSystemTimeZoneDidChange,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                self?.handleSystemTimeZoneChange()
            }
        }
    }

    private func cancelTimeZoneObservation() {
        if let timeZoneObserver {
            NotificationCenter.default.removeObserver(timeZoneObserver)
            self.timeZoneObserver = nil
        }
        cancelCodexSubscriptionDayBoundaryReload()
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
        statusRefreshPendingAfterLoad = false
        statusRefreshPendingOnlyIndex = false
        cancelCodexSubscriptionDayBoundaryReload()
        codexCardAccountRetryScheduler.cancel()
        codexCardAccountEpoch &+= 1
        for key in featureTasks.keys {
            featureTasks[key]?.cancel()
            featureGenerations[key, default: 0] &+= 1
        }
        featureTasks.removeAll()
        cancelRefreshAll()
    }

	private func cancelPageFeatureTasks() {
			let providerIndependentKeys: Set<FeatureTaskKey> = [
				.statusOverview, .statusAccount, .codexCardAccount, .dashboardSummary,
                .codexSubscriptionList, .codexSubscriptionMutate,
			]
			let keys = featureTasks.keys.filter { !providerIndependentKeys.contains($0) && $0.isRead }
		for key in keys {
			featureTasks[key]?.cancel()
			featureTasks[key] = nil
			featureGenerations[key, default: 0] &+= 1
			refreshAllPendingTasks.remove(key)
		}
		cancelRefreshAll()
	}

    private func cancelFeatureReadTasks() {
        let keys = featureTasks.keys.filter(\.isRead)
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
        case .normal(let responses), .partial(let responses, _):
            if responses.provider == .codex {
                if let account = responses.account {
                    if account.hasAccount {
                        acceptCodexCardAccount(account, matching: responses.quota)
                    } else {
                        codexCardAccountSnapshot = nil
                    }
                }
            }
        default: break
        }
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
			if loadFeaturesOnNextOverview {
				loadFeaturesOnNextOverview = false
				if statusOverviewState.shouldReloadOnNavigation {
					loadStatusOverview()
				}
				if selectedFeature != .overview { load(selectedFeature) }
			}
        case .stale(_, let notice), .unavailable(let notice):
			loadFeaturesOnNextOverview = true
            cancelUpdatePolicyObservation()
            cancelAllFeatureTasks()
            markMutationsUncertain(notice)
            markFeatureStatesStale(notice)
        case .recovery, .restartRequired, .shuttingDown, .stopped:
			loadFeaturesOnNextOverview = true
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
              !refreshAllWaitsForOverview,
              !isGlobalRefreshing
        else {
            return
        }
        isRefreshingAll = false
    }

    private func cancelRefreshAll() {
        isRefreshingAll = false
        refreshAllPendingTasks.removeAll()
        refreshAllWaitsForOverview = false
        isGlobalRefreshing = false
        globalRefreshTask = nil
    }

    private func runGlobalManualRefresh(then reload: @escaping () -> Void) {
        if let existing = globalRefreshTask {
            Task { @MainActor [weak self] in
                await existing.value
                guard let self, self.canRefreshOrRestart else { return }
                reload()
            }
            return
        }
        isGlobalRefreshing = true
        let task = Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                let receipt = try await self.runtime.requestProviderRefresh(trigger: "manual")
                self.globalRefreshPresentation = ProviderRefreshPresentation(receipt)
            } catch {
                // Keep last committed presentation; do not mark the whole app unavailable.
            }
            self.isGlobalRefreshing = false
            self.globalRefreshTask = nil
            guard self.canRefreshOrRestart else { return }
            reload()
            self.finishRefreshAllIfPossible()
        }
        globalRefreshTask = task
    }

    private func markMutationsUncertain(_ notice: AppNotice) {
        for provider in quotaRefreshStates.keys {
            if case .running = quotaRefreshStates[provider] {
                quotaRefreshStates[provider] = .unavailable(notice)
            }
        }
        for provider in resetCreditsRefreshStates.keys {
            if case .running = resetCreditsRefreshStates[provider] {
                resetCreditsRefreshStates[provider] = .unavailable(notice)
            }
        }
        quotaRefreshState = selectedProvider.flatMap { quotaRefreshStates[$0] } ?? .idle
        resetCreditsRefreshState = selectedProvider.flatMap { resetCreditsRefreshStates[$0] } ?? .idle
        if case .running = runtimeActionState { runtimeActionState = .unavailable(notice) }
        if case .saving = settingsSaveState { settingsSaveState = .unavailable(notice) }
        if case .running = apiCredentialActionState { apiCredentialActionState = .unavailable(notice) }
        if case .running = codexSubscriptionActionState {
            codexSubscriptionActionState = .unavailable(notice)
        }
    }

    private func receiveInvalidation(domain: String) {
        let notice = AppNotice(
            code: "content_invalidated",
            messageKey: "app.notice.content_invalidated.\(domain)",
            retryable: true
        )
        let affected: Set<AppFeature>
		let refreshesStatus: Bool
        let quotaInvalidation = domain == "quota" || domain.hasPrefix("quota_")
        switch domain {
        case "index":
            invalidateTasks([
                .usage, .invocationUsage, .sessions, .sessionDetail, .projects, .projectDetail,
                .dashboardSummary,
            ])
            usageState = stale(usageState, notice)
            invocationUsageState = stale(invocationUsageState, notice)
            sessionsState = stale(sessionsState, notice)
            sessionDetailState = stale(sessionDetailState, notice)
            projectsState = stale(projectsState, notice)
            projectDetailState = stale(projectDetailState, notice)
            dashboardSummaryState = stale(dashboardSummaryState, notice)
            affected = [.sessions, .projects, .quotaUsage, .invocationUsage, .dashboardSummary]
			refreshesStatus = true
        case "quota", "quota_codex", "quota_cursor", "quota_grok":
            let sourceProvider: AgentProvider? = switch domain {
            case "quota_codex": .codex
            case "quota_cursor": .cursor
            case "quota_grok": .grok
            default: nil
            }
            let refreshesSelectedQuota = sourceProvider == nil || selectedProvider == sourceProvider
            if domain == "quota" { invalidateCodexCardAccount() }
            if refreshesSelectedQuota {
                invalidateTasks([.quota, .quotaAccount, .quotaPace])
                quotaState = stale(quotaState, notice)
                quotaAccountState = stale(quotaAccountState, notice)
                quotaPaceState = stale(quotaPaceState, notice)
            }
            invalidateTasks([.dashboardSummary])
            dashboardSummaryState = stale(dashboardSummaryState, notice)
            affected = refreshesSelectedQuota ? [.quotaUsage, .dashboardSummary] : [.dashboardSummary]
			refreshesStatus = sourceProvider == nil || statusProvider == sourceProvider
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
            invalidateTasks([.settings, .dashboardSummary])
            settingsState = stale(settingsState, notice)
            dashboardSummaryState = stale(dashboardSummaryState, notice)
            var next: Set<AppFeature> = [.settings, .dashboardSummary]
            if selectedFeature.requiresEnabledProvider {
                next.insert(selectedFeature)
            }
            affected = next
			refreshesStatus = true
        case "account":
            invalidateCodexCardAccount()
            invalidateTasks([.codexSubscriptionList, .quota, .quotaAccount, .quotaPace, .statusAccount])
            codexSubscriptionAccountsState = stale(codexSubscriptionAccountsState, notice)
            quotaAccountState = stale(quotaAccountState, notice)
            if selectedProvider == .codex {
                quotaState = stale(quotaState, notice)
                quotaPaceState = stale(quotaPaceState, notice)
            }
            if selectedFeature == .settings, !requiresCoreRestart {
                loadCodexSubscriptionAccounts()
            } else if selectedFeature == .quotaUsage, selectedProvider == .codex,
                      !requiresCoreRestart
            {
                let now = Date()
                loadQuota(now: now)
                loadQuotaPace(now: now)
                loadQuotaAccount()
            }
            affected = []
			refreshesStatus = true
        case "lifecycle":
            cancelFeatureReadTasks()
            markFeatureStatesStale(notice)
            affected = Set(AppFeature.allCases.filter { $0 != .overview })
			refreshesStatus = false
        default:
            return
        }
		if affected.contains(selectedFeature), !requiresCoreRestart {
			if domain == "index", selectedFeature == .quotaUsage {
				loadUsage()
			} else if quotaInvalidation, selectedFeature == .quotaUsage {
				let now = Date()
				loadQuota(now: now)
				loadQuotaPace(now: now)
				if selectedProvider == .codex { loadQuotaAccount() }
			} else {
				reloadFeature(selectedFeature)
			}
		}
			if refreshesStatus, !requiresCoreRestart {
				if !statusOverviewState.isLoading {
					invalidateTasks(domain == "index" ? [.statusOverview] : [.statusOverview, .statusAccount])
					statusOverviewState = stale(statusOverviewState, notice)
					loadStatusOverview(reuseAccountOnIndex: domain == "index")
				} else {
					statusRefreshPendingOnlyIndex = !statusRefreshPendingAfterLoad
						? domain == "index"
						: statusRefreshPendingOnlyIndex && domain == "index"
					statusRefreshPendingAfterLoad = true
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
		invalidateCodexCardAccount()
		statusOverviewState = .idle
		statusUsageState = .idle
		statusInvocationState = .idle
        dashboardSummaryState = .idle
        usageState = .idle
        invocationUsageState = .idle
        pricingCatalogState = .idle
        quotaState = .idle
        quotaAccountState = .idle
        quotaPaceState = .idle
        apiSubscriptionsState = .idle
        apiCredentialStatus = nil
        apiCredentialActionState = .idle
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
        codexSubscriptionAccountsState = .idle
        codexSubscriptionActionState = .idle
        cancelCodexSubscriptionDayBoundaryReload()
        selectedSessionID = nil
        selectedProjectKey = nil
        selectedSourceKey = nil
        selectedJobID = nil
        selectedHealthEventID = nil
        consumedCursors.removeAll()
    }

	private func resetProviderFeatureState() {
		cancelCodexSubscriptionDayBoundaryReload()
		usageState = .idle
		invocationUsageState = .idle
		pricingCatalogState = .idle
		quotaState = .idle
		quotaAccountState = .idle
		quotaPaceState = .idle
		quotaRefreshState = .idle
		resetCreditsRefreshState = .idle
		sessionsState = .idle
		sessionDetailState = .idle
		projectsState = .idle
		projectDetailState = .idle
		selectedSessionID = nil
		selectedProjectKey = nil
		consumedCursors.removeAll()
	}

	private func loadStatusOverview(reuseAccountOnIndex: Bool = false) {
		guard let provider = statusProvider else {
            statusOverviewState = .idle
            statusUsageState = .idle
            statusInvocationState = .idle
            return
        }
		let previous = statusOverviewState.value
		if !(reuseAccountOnIndex && featureTasks[.statusAccount] != nil) {
			invalidateTasks([.statusAccount])
		}
		statusOverviewState = .loading(previous: previous)
		if provider.usesOfficialPeriodRing {
			statusUsageState = .loading(previous: statusUsageState.value)
		}
		statusInvocationState = .idle
		launch(
			.statusOverview,
			operation: { [runtime] in try await runtime.statusOverview(provider: provider) }
		) { [weak self] responses in
			guard let self,
				self.statusProvider == provider,
				responses.provider == provider
			else { return }
			let presentation = OverviewPresentation(responses)
			statusOverviewResponses[provider] = responses
			statusOverviewCache[provider] = presentation
			statusOverviewState = presentation.isPartial
				? .partial(presentation, notices: presentation.notices)
				: .ready(presentation)
			if statusRefreshPendingAfterLoad {
				let reuseAccountOnIndex = statusRefreshPendingOnlyIndex
				statusRefreshPendingAfterLoad = false
				statusRefreshPendingOnlyIndex = false
				loadStatusOverview(reuseAccountOnIndex: reuseAccountOnIndex)
				return
			}
			let accountKey = CodexAccountContext.key(fromAccount: codexCardAccountSnapshot)
			if reuseAccountOnIndex, provider == .codex,
				let accountKey,
				accountKey == CodexAccountContext.key(fromQuota: responses.quota),
				let account = codexCardAccountSnapshot
			{
				let validated = CodexAccountContext.validatePublishedOverview(
					responses.replacingAccount(account)
				)
				let cached = OverviewPresentation(validated.responses)
				statusOverviewCache[provider] = cached
				statusOverviewState = cached.isPartial
					? .partial(cached, notices: cached.notices)
					: .ready(cached)
				codexCardAccountIsLoading = false
			} else if !reuseAccountOnIndex || provider != .codex ||
				(accountKey != nil &&
				CodexAccountContext.key(fromQuota: responses.quota) != nil &&
				accountKey != CodexAccountContext.key(fromQuota: responses.quota)) {
				loadStatusAccount(for: responses, provider: provider)
			}
			if provider.usesOfficialPeriodRing {
				statusUsageCache[provider] = responses.usage
				statusUsageState = loadState(
					value: responses.usage,
					meta: responses.usage.meta,
					isEmpty: false
				)
			} else {
				statusUsageState = .idle
			}
		} failure: { [weak self] error in
			guard let self, statusProvider == provider else { return }
			statusOverviewState = failedLoadState(previous: previous, error: error)
			if statusRefreshPendingAfterLoad {
				let reuseAccountOnIndex = statusRefreshPendingOnlyIndex
				statusRefreshPendingAfterLoad = false
				statusRefreshPendingOnlyIndex = false
				loadStatusOverview(reuseAccountOnIndex: reuseAccountOnIndex)
				return
			}
			if provider.usesOfficialPeriodRing {
				statusUsageState = failedLoadState(previous: statusUsageState.value, error: error)
			}
		}
	}

	private func loadStatusAccount(for responses: OverviewResponses, provider: AgentProvider) {
		if provider == .codex { codexCardAccountIsLoading = true }
		launch(
			.statusAccount,
			operation: { [runtime] in try await runtime.accountSnapshot(provider: provider) }
		) { [weak self] account in
			guard let self, statusProvider == provider else { return }
			let responses = statusOverviewResponses[provider] ?? responses
			if provider == .codex { codexCardAccountIsLoading = false }
			if provider == .codex && !account.hasAccount { codexCardAccountRetried = true }
			let validated = CodexAccountContext.validatePublishedOverview(
				responses.replacingAccount(account)
			)
			if provider == .codex {
				if CodexAccountContext.key(fromQuota: responses.quota)
					== CodexAccountContext.key(fromAccount: account) {
					if !account.hasAccount { codexCardAccountSnapshot = nil }
					acceptCodexCardAccount(account, matching: responses.quota)
					statusConsistencyRefreshKey = nil
				} else if let key = CodexAccountContext.key(fromQuota: responses.quota),
					statusConsistencyRefreshKey != key {
					discardCodexCardAccount(matching: key)
					statusConsistencyRefreshKey = key
					loadStatusOverview()
					scheduleCodexCardAccountRetry()
					return
				}
			}
			let presentation = OverviewPresentation(validated.responses)
			statusOverviewCache[provider] = presentation
			statusOverviewState = presentation.isPartial
				? .partial(presentation, notices: presentation.notices)
				: .ready(presentation)
		} failure: { [weak self] _ in
			guard let self, statusProvider == provider else { return }
			if provider == .codex {
				codexCardAccountIsLoading = false
				scheduleCodexCardAccountRetry()
			}
		}
	}

    public func ensureStatusAccountCard() {
        guard statusProvider == .codex, statusPresentation != nil,
              statusAccountCardSummary?.availability != .available,
              featureTasks[.statusOverview] == nil,
              featureTasks[.statusAccount] == nil
        else { return }
        if let accountKey = CodexAccountContext.key(fromAccount: codexCardAccountSnapshot),
           accountKey != statusPresentation?.codexAccountContextKey {
            loadStatusOverview()
            return
        }
        if let key = statusPresentation?.codexAccountContextKey,
           statusConsistencyRefreshKey == key {
            statusConsistencyRefreshKey = nil
            loadStatusOverview()
            return
        }
        loadCodexCardAccount()
    }

    private func acceptCodexCardAccount(
        _ account: Codexpulse_Core_V1_AccountSnapshotResponse,
        matching quota: Codexpulse_Core_V1_QuotaCurrentResponse
    ) {
        guard account.hasAccount,
              let key = CodexAccountContext.key(fromQuota: quota),
              CodexAccountContext.key(fromAccount: account) == key
        else { return }
        codexCardAccountSnapshot = account
        codexCardAccountIsLoading = false
        codexCardAccountRetried = false
        codexCardAccountRetryScheduler.cancel()
    }

    private func discardCodexCardAccount(matching key: CodexAccountContextKey) {
        if CodexAccountContext.key(fromAccount: codexCardAccountSnapshot) == key {
            codexCardAccountSnapshot = nil
        }
    }

    private func invalidateCodexCardAccount() {
        codexCardAccountEpoch &+= 1
        codexCardAccountRetryScheduler.cancel()
        codexCardAccountRetried = false
        codexCardAccountSnapshot = nil
        codexCardAccountIsLoading = false
        statusConsistencyRefreshKey = nil
        quotaConsistencyRefreshKey = nil
        invalidateTasks([.codexCardAccount])
    }

    private func scheduleCodexCardAccountRetry() {
        guard codexCardAccountSnapshot == nil,
              !codexCardAccountRetried, !codexCardAccountRetryScheduler.isScheduled
        else { return }
        codexCardAccountRetried = true
        let epoch = codexCardAccountEpoch
        codexCardAccountRetryScheduler.schedule(after: 1) { [weak self] in
            guard let self, epoch == codexCardAccountEpoch else { return }
            loadCodexCardAccount()
        }
    }

    private func loadCodexCardAccount() {
        guard canRefreshOrRestart,
              (statusProvider == .codex || selectedProvider == .codex),
              featureTasks[.codexCardAccount] == nil,
              featureTasks[.statusAccount] == nil,
              featureTasks[.quotaAccount] == nil
        else { return }
        codexCardAccountIsLoading = true
        launch(
            .codexCardAccount,
            operation: { [runtime] in try await runtime.accountSnapshot(provider: .codex) }
        ) { [weak self] account in
            guard let self else { return }
            codexCardAccountIsLoading = false
            let accountKey = CodexAccountContext.key(fromAccount: account)
            var accepted = false
            if statusProvider == .codex,
               let responses = statusOverviewResponses[.codex],
               accountKey != nil,
               accountKey == CodexAccountContext.key(fromQuota: responses.quota) {
                acceptCodexCardAccount(account, matching: responses.quota)
                accepted = account.hasAccount
            }
            if selectedProvider == .codex,
               let quota = quotaState.value,
               accountKey != nil,
               accountKey == CodexAccountContext.key(fromQuota: quota) {
                acceptCodexCardAccount(account, matching: quota)
                accepted = account.hasAccount
                quotaAccountState = .ready(account)
            }
            switch state {
            case .overview(let overview), .partial(let overview):
                if overview.provider == .codex, accountKey != nil,
                   accountKey == overview.codexAccountContextKey {
                    // The main Overview has already validated this binding.
                    codexCardAccountSnapshot = account
                    accepted = account.hasAccount
                }
            default: break
            }
            if !accepted {
                codexCardAccountSnapshot = nil
                codexCardAccountRetried = true
            }
            if statusProvider == .codex,
               let quotaKey = statusOverviewResponses[.codex].flatMap({ CodexAccountContext.key(fromQuota: $0.quota) }),
               quotaKey != accountKey,
               statusConsistencyRefreshKey != quotaKey,
               !statusOverviewState.isLoading {
                statusConsistencyRefreshKey = quotaKey
                loadStatusOverview()
            }
            if selectedProvider == .codex, selectedFeature == .quotaUsage,
               let quotaKey = quotaState.value.flatMap(CodexAccountContext.key(fromQuota:)),
               quotaKey != accountKey,
               quotaConsistencyRefreshKey != quotaKey,
               !quotaState.isLoading {
                quotaConsistencyRefreshKey = quotaKey
                loadQuota()
            }
        } failure: { [weak self] _ in
            self?.codexCardAccountIsLoading = false
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
		invalidateCodexCardAccount()
		statusOverviewState = stale(statusOverviewState, notice)
        dashboardSummaryState = stale(dashboardSummaryState, notice)
        usageState = stale(usageState, notice)
        invocationUsageState = stale(invocationUsageState, notice)
        pricingCatalogState = stale(pricingCatalogState, notice)
        quotaState = stale(quotaState, notice)
        quotaAccountState = stale(quotaAccountState, notice)
        quotaPaceState = stale(quotaPaceState, notice)
        apiSubscriptionsState = stale(apiSubscriptionsState, notice)
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
        codexSubscriptionAccountsState = stale(codexSubscriptionAccountsState, notice)
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
