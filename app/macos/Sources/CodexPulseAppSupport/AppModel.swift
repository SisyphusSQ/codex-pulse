import CodexPulseProtocolGenerated
import Combine
import Foundation

public enum AppFeature: String, CaseIterable, Hashable, Identifiable, Sendable {
    case overview
    case sessions
    case projects
    case quotaUsage
    case localStatus
    case sourcesJobs
    case settings

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .overview: "概览"
        case .sessions: "会话"
        case .projects: "项目"
        case .quotaUsage: "额度与用量"
        case .localStatus: "本机状态"
        case .sourcesJobs: "数据源与任务"
        case .settings: "设置"
        }
    }

    public var symbol: String {
        switch self {
        case .overview: "gauge.with.dots.needle.67percent"
        case .sessions: "text.bubble"
        case .projects: "folder"
        case .quotaUsage: "chart.xyaxis.line"
        case .localStatus: "heart.text.square"
        case .sourcesJobs: "externaldrive.connected.to.line.below"
        case .settings: "gearshape"
        }
    }
}

private enum FeatureTaskKey: Hashable {
    case usage, quota, quotaRefresh
    case runtimeAction
    case sessions, sessionDetail
    case projects, projectDetail
    case sources, sourceDetail
    case jobs, jobDetail
    case healthProjection, dataHealth, healthList, healthDetail
    case settings, settingsSave
}

@MainActor
public final class AppModel: ObservableObject {
    @Published public private(set) var state: AppViewState = .idle
    @Published public private(set) var lastShutdownOutcome: ShutdownOutcome?
    @Published public private(set) var isOverviewRefreshing = false
    @Published public var selectedFeature: AppFeature = .overview
    @Published public private(set) var renderedFeatures: Set<AppFeature> = []

    @Published public var sessionOptions = SessionQueryOptions()
    @Published public var projectOptions = ProjectQueryOptions()
    @Published public var sourceOptions = RuntimeQueryOptions()
    @Published public var jobOptions = RuntimeQueryOptions()
    @Published public var healthOptions = RuntimeQueryOptions(firstField: "active", firstValues: ["true"])
    @Published public var usageRange: DateRangePreset = .sevenDays
    @Published public private(set) var overviewRange: DateRangePreset = .quotaWeek

    @Published public private(set) var usageState: FeatureLoadState<Codexpulse_Core_V1_UsageCostResponse> = .idle
    @Published public private(set) var quotaState: FeatureLoadState<Codexpulse_Core_V1_QuotaCurrentResponse> = .idle
    @Published public private(set) var quotaRefreshState: ActionState = .idle
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

    @Published public private(set) var selectedSessionID: String?
    @Published public private(set) var selectedProjectKey: String?
    @Published public private(set) var selectedSourceKey: String?
    @Published public private(set) var selectedJobID: String?
    @Published public private(set) var selectedHealthEventID: String?

    private let runtime: AppRuntime
    private var startTask: Task<Void, Never>?
    private var overviewRefreshTask: Task<Void, Never>?
    private var overviewRefreshGeneration: UInt64 = 0
    private var featureTasks: [FeatureTaskKey: Task<Void, Never>] = [:]
    private var featureGenerations: [FeatureTaskKey: UInt64] = [:]
    private var consumedCursors: [FeatureTaskKey: Set<String>] = [:]

    public init(configuration: AppLaunchConfiguration) {
        runtime = AppRuntime(configuration: configuration)
    }

    public init(runtime: AppRuntime) {
        self.runtime = runtime
    }

    public var statusItemTitle: String {
        switch presentation {
        case .some(let overview):
            let values = overview.quotaWindows.prefix(2).map { window in
                let percent = window.remainingPercent.map { String(format: "%.0f%%", $0) } ?? "--"
                return "\(window.title) \(percent)"
            }
            return values.isEmpty ? "Codex Pulse --" : values.joined(separator: " · ")
        case .none:
            switch state {
            case .loading: return "Codex Pulse …"
            case .recovery, .restartRequired: return "Codex Pulse 恢复"
            case .unavailable: return "Codex Pulse 离线"
            default: return "Codex Pulse --"
            }
        }
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
            quotaState.isLoading || usageState.isLoading
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
            await runtime.start()
            self.startTask = nil
        }
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
        case .localStatus: loadLocalStatus()
        case .sourcesJobs: loadSourcesAndJobs(reset: true)
        case .settings: loadSettings()
        }
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
            if quotaState.shouldReloadOnNavigation || usageState.shouldReloadOnNavigation { loadQuotaAndUsage() }
        case .localStatus:
            if dataHealthState.shouldReloadOnNavigation || healthState.shouldReloadOnNavigation { loadLocalStatus() }
        case .sourcesJobs:
            if sourcesState.shouldReloadOnNavigation || jobsState.shouldReloadOnNavigation {
                loadSourcesAndJobs(reset: true)
            }
        case .settings:
            if settingsState.shouldReloadOnNavigation { loadSettings() }
        }
    }

    public func navigate(to feature: AppFeature) {
        selectedFeature = feature
        load(feature)
    }

    public func markFeatureRendered(_ feature: AppFeature) {
        renderedFeatures.insert(feature)
    }

    public func refreshAllFeatures() {
        guard canRefreshOrRestart else { return }
        if requiresCoreRestart {
            restartCore()
            return
        }
        loadSessions(reset: true)
        loadProjects(reset: true)
        loadQuotaAndUsage()
        loadLocalStatus()
        loadSourcesAndJobs(reset: true)
        loadSettings()
        refresh()
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
        cancelFeatureReadTasks()
        markFeatureStatesStale(AppNotice(
            code: "system_sleeping",
            messageKey: "app.notice.system_sleeping",
            retryable: true
        ))
        Task { await runtime.prepareForSleep() }
    }

    public func resumeAfterWake() {
        Task { await runtime.resumeAfterWake() }
    }

    public func shutdown() async -> ShutdownOutcome {
        startTask?.cancel()
        startTask = nil
        overviewRefreshTask?.cancel()
        overviewRefreshTask = nil
        overviewRefreshGeneration &+= 1
        isOverviewRefreshing = false
        cancelAllFeatureTasks()
        let outcome = await runtime.shutdown()
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
        let request = FeatureRequestFactory.sessions(options: sessionOptions, cursor: cursor)
        launch(.sessions, operation: { [runtime] in try await runtime.listSessions(request) }) { [weak self] response in
            guard let self else { return }
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
        let request = FeatureRequestFactory.sessionDetail(sessionID: sessionID, turnCursor: cursor)
        launch(.sessionDetail, operation: { [runtime] in try await runtime.sessionDetail(request) }) { [weak self] response in
            guard let self, selectedSessionID == sessionID else { return }
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
        let request = FeatureRequestFactory.projects(options: projectOptions, cursor: cursor)
        launch(.projects, operation: { [runtime] in try await runtime.listProjects(request) }) { [weak self] response in
            guard let self else { return }
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
            exactRange: projectOptions.exactRange,
            sessionCursor: sessionCursor,
            modelCursor: modelCursor
        )
        launch(.projectDetail, operation: { [runtime] in try await runtime.projectDetail(request) }) { [weak self] response in
            guard let self, selectedProjectKey == dimensionKey else { return }
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
        loadUsage()
        loadQuota()
    }

    public func loadUsage() {
        let previous = usageState.value
        usageState = .loading(previous: previous)
        let request = FeatureRequestFactory.usage(range: usageRange)
        launch(.usage, operation: { [runtime] in try await runtime.usageCost(request) }) { [weak self] response in
            self?.usageState = loadState(value: response, meta: response.meta, isEmpty: false)
        } failure: { [weak self] error in
            self?.usageState = failedLoadState(previous: previous, error: error)
        }
    }

    public func loadQuota() {
        let previous = quotaState.value
        quotaState = .loading(previous: previous)
        let request = FeatureRequestFactory.quota()
        launch(.quota, operation: { [runtime] in try await runtime.quotaCurrent(request) }) { [weak self] response in
            self?.quotaState = loadState(
                value: response,
                meta: response.meta,
                isEmpty: response.current.windows.isEmpty && response.current.sources.isEmpty
            )
        } failure: { [weak self] error in
            self?.quotaState = failedLoadState(previous: previous, error: error)
        }
    }

    public func requestQuotaRefresh(source: String) {
        guard canRefreshOrRestart else { return }
        if case .running = quotaRefreshState { return }
        quotaRefreshState = .running
        launch(.quotaRefresh, operation: { [runtime] in
            try await runtime.requestQuotaRefresh(source: source)
        }) { [weak self] receipt in
            guard let self else { return }
            quotaRefreshState = .succeeded(receipt.reason)
            loadQuota()
        } failure: { [weak self] error in
            self?.quotaRefreshState = .unavailable(AppNotice.from(error))
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
            if let preservedDraft {
                settingsDraft = preservedDraft
                if previous?.snapshot.revision != response.snapshot.revision {
                    settingsSaveState = .conflict
                } else {
                    settingsSaveState = .idle
                }
            } else {
                settingsDraft = SettingsDraft(response)
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
                    settingsDraft = pendingDraft ?? SettingsDraft(readback)
                    settingsSaveState = pendingDraft == nil ? .applied(revision: receipt.revision) : .idle
                case "applied_reconcile_required":
                    settingsDraft = pendingDraft ?? SettingsDraft(readback)
                    settingsSaveState = .reconcileRequired(revision: receipt.revision)
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
        return generation
    }

    private func isCurrent(_ key: FeatureTaskKey, generation: UInt64) -> Bool {
        featureGenerations[key] == generation
    }

    private func finishTask(_ key: FeatureTaskKey) {
        featureTasks[key] = nil
    }

    private func cancelAllFeatureTasks() {
        for key in featureTasks.keys {
            featureTasks[key]?.cancel()
            featureGenerations[key, default: 0] &+= 1
        }
        featureTasks.removeAll()
    }

    private func cancelFeatureReadTasks() {
        let mutationKeys: Set<FeatureTaskKey> = [.quotaRefresh, .runtimeAction, .settingsSave]
        let keys = featureTasks.keys.filter { !mutationKeys.contains($0) }
        for key in keys {
            featureTasks[key]?.cancel()
            featureTasks[key] = nil
            featureGenerations[key, default: 0] &+= 1
        }
    }

    private func receive(_ runtimeState: CoreConnectionState) {
        switch runtimeState {
        case .normal, .partial, .stale, .unavailable, .cancelled:
            finishOverviewRefresh()
        case .idle, .starting, .handshaking, .loadingOverview, .recovery, .restartRequired, .shuttingDown, .stopped:
            break
        }
        state = AppViewState(runtimeState)
        switch runtimeState {
        case .normal, .partial:
            if selectedFeature != .overview { load(selectedFeature) }
        case .stale(_, let notice), .unavailable(let notice):
            cancelAllFeatureTasks()
            markMutationsUncertain(notice)
            markFeatureStatesStale(notice)
        case .recovery, .restartRequired, .shuttingDown, .stopped:
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

    private func finishOverviewRefresh() {
        isOverviewRefreshing = false
        overviewRefreshTask = nil
    }

    private func markMutationsUncertain(_ notice: AppNotice) {
        if case .running = quotaRefreshState { quotaRefreshState = .unavailable(notice) }
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
        switch domain {
        case "index":
            invalidateTasks([.usage, .sessions, .sessionDetail, .projects, .projectDetail])
            usageState = stale(usageState, notice)
            sessionsState = stale(sessionsState, notice)
            sessionDetailState = stale(sessionDetailState, notice)
            projectsState = stale(projectsState, notice)
            projectDetailState = stale(projectDetailState, notice)
            affected = [.sessions, .projects, .quotaUsage]
        case "quota":
            invalidateTasks([.quota])
            quotaState = stale(quotaState, notice)
            affected = [.quotaUsage]
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
        case "settings":
            invalidateTasks([.settings])
            settingsState = stale(settingsState, notice)
            affected = [.settings]
        case "lifecycle":
            cancelFeatureReadTasks()
            markFeatureStatesStale(notice)
            affected = Set(AppFeature.allCases.filter { $0 != .overview })
        default:
            return
        }
        if affected.contains(selectedFeature), !requiresCoreRestart {
            refresh(selectedFeature)
        }
    }

    private func invalidateTasks(_ keys: Set<FeatureTaskKey>) {
        for key in keys {
            featureTasks[key]?.cancel()
            featureTasks[key] = nil
            featureGenerations[key, default: 0] &+= 1
        }
    }

    private func resetFeatureState() {
        usageState = .idle
        quotaState = .idle
        quotaRefreshState = .idle
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
        usageState = stale(usageState, notice)
        quotaState = stale(quotaState, notice)
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
