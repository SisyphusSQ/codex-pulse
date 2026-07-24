import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

public enum FeatureLoadState<Value: Sendable>: Sendable {
    case idle
    case loading(previous: Value?)
    case ready(Value)
    case partial(Value, notices: [AppNotice])
    case stale(Value, notice: AppNotice)
    case empty
    case unavailable(AppNotice)
    case cancelled(previous: Value?)

    public var value: Value? {
        switch self {
        case .loading(let previous), .cancelled(let previous): previous
        case .ready(let value), .partial(let value, _), .stale(let value, _): value
        case .idle, .empty, .unavailable: nil
        }
    }

    public var isLoading: Bool {
        if case .loading = self { return true }
        return false
    }

    public var shouldReloadOnNavigation: Bool {
        switch self {
        case .idle, .stale, .unavailable, .cancelled: true
        case .loading, .ready, .partial, .empty: false
        }
    }
}

public enum SettingsSaveState: Equatable, Sendable {
    case idle
    case saving
    case applied(revision: String)
    case reconcileRequired(revision: String)
    case conflict
    case unavailable(AppNotice)
}

public enum ActionState: Equatable, Sendable {
    case idle
    case running
    case succeeded(String)
    case unavailable(AppNotice)
}

public enum RuntimeControlAction: String, CaseIterable, Equatable, Sendable {
    case pauseBackfill = "pause_backfill"
    case pauseAll = "pause_all"
    case resume
    case reconcile

    public init?(commandKey: String) {
        self.init(rawValue: commandKey)
    }

    public var title: String {
        switch self {
        case .pauseBackfill: "暂停回填"
        case .pauseAll: "暂停全部任务"
        case .resume: "恢复任务"
        case .reconcile: "立即对账"
        }
    }
}

public struct PrimaryPagesSmokeSummary: Equatable, Sendable {
    public let sessions: Int
    public let projects: Int
    public let sources: Int
    public let jobs: Int
    public let healthEvents: Int
    public let usageTrend: Int
    public let usageModels: Int
    public let usageModelTrend: Int
    public let usageModelReconciled: Int
    public let usageCostKnown: Bool
    public let quotaWindows: Int
    public let projectDetailCostKnown: Bool
    public let projectDetailModels: Int
    public let detailsRead: Int
    public let settingsMutation: String
    public let unavailableSteps: [String]

    public init(
        sessions: Int,
        projects: Int,
        sources: Int,
        jobs: Int,
        healthEvents: Int,
        usageTrend: Int,
        usageModels: Int,
        usageModelTrend: Int,
        usageModelReconciled: Int,
        usageCostKnown: Bool,
        quotaWindows: Int,
        projectDetailCostKnown: Bool = false,
        projectDetailModels: Int = 0,
        detailsRead: Int,
        settingsMutation: String,
        unavailableSteps: [String]
    ) {
        self.sessions = sessions
        self.projects = projects
        self.sources = sources
        self.jobs = jobs
        self.healthEvents = healthEvents
        self.usageTrend = usageTrend
        self.usageModels = usageModels
        self.usageModelTrend = usageModelTrend
        self.usageModelReconciled = usageModelReconciled
        self.usageCostKnown = usageCostKnown
        self.quotaWindows = quotaWindows
        self.projectDetailCostKnown = projectDetailCostKnown
        self.projectDetailModels = projectDetailModels
        self.detailsRead = detailsRead
        self.settingsMutation = settingsMutation
        self.unavailableSteps = unavailableSteps
    }

    public var stableDescription: String {
        "sessions=\(sessions) projects=\(projects) sources=\(sources) jobs=\(jobs) "
            + "health_events=\(healthEvents) usage_trend=\(usageTrend) usage_models=\(usageModels) "
            + "usage_model_trend=\(usageModelTrend) usage_model_reconciled=\(usageModelReconciled) "
            + "usage_cost=\(usageCostKnown ? "known" : "unknown") quota_windows=\(quotaWindows) "
            + "project_detail_cost=\(projectDetailCostKnown ? "known" : "unknown") "
            + "project_detail_models=\(projectDetailModels) "
            + "details_read=\(detailsRead) settings=\(settingsMutation) "
            + "unavailable=\(unavailableSteps.isEmpty ? "none" : unavailableSteps.joined(separator: ","))"
    }
}

public struct PrimaryPagesSmokeError: Error, Equatable, Sendable {
    public let step: String

    public init(step: String) {
        self.step = step
    }
}

public struct SettingsDraft: Equatable, Sendable {
    public var quotaEnabled: Bool
    public var resetCreditsEnabled: Bool
    public var quotaIntervalSeconds: Int64
    public var resetCreditsIntervalSeconds: Int64
    public var reconcileIntervalSeconds: Int64
    public var jsonlDebounceMilliseconds: Int64
    public var autoCheckEnabled: Bool
    public var checkIntervalSeconds: Int64
    public var launchBehavior: String
    public var overviewRange: String

    public init(_ response: Codexpulse_Core_V1_SettingsResponse) {
        let snapshot = response.snapshot
        quotaEnabled = snapshot.online.quotaEnabled
        resetCreditsEnabled = snapshot.online.resetCreditsEnabled
        quotaIntervalSeconds = snapshot.refresh.quotaIntervalSeconds
        resetCreditsIntervalSeconds = snapshot.refresh.resetCreditsIntervalSeconds
        reconcileIntervalSeconds = snapshot.refresh.reconcileIntervalSeconds
        jsonlDebounceMilliseconds = snapshot.refresh.jsonlDebounceMilliseconds
        autoCheckEnabled = snapshot.updates.autoCheckEnabled
        checkIntervalSeconds = snapshot.updates.checkIntervalSeconds
        launchBehavior = snapshot.ui.launchBehavior
        overviewRange = snapshot.ui.overviewRange
    }

    public func makeRequest(
        authoritative response: Codexpulse_Core_V1_SettingsResponse
    ) -> Codexpulse_Core_V1_UpdateSettingsRequest {
        let editable = Set(response.editableFields.filter(\.editable).map(\.key))
        let current = SettingsDraft(response)
        var request = Codexpulse_Core_V1_UpdateSettingsRequest()
        request.expectedRevision = response.snapshot.revision

        var online = Codexpulse_Core_V1_SettingsOnlineUpdate()
        online.quotaEnabled = editable.contains("online.quotaEnabled") ? quotaEnabled : current.quotaEnabled
        online.resetCreditsEnabled = editable.contains("online.resetCreditsEnabled")
            ? resetCreditsEnabled : current.resetCreditsEnabled
        request.online = online

        var refresh = Codexpulse_Core_V1_SettingsRefreshUpdate()
        refresh.quotaIntervalSeconds = editable.contains("refresh.quotaIntervalSeconds")
            ? quotaIntervalSeconds : current.quotaIntervalSeconds
        refresh.resetCreditsIntervalSeconds = editable.contains("refresh.resetCreditsIntervalSeconds")
            ? resetCreditsIntervalSeconds : current.resetCreditsIntervalSeconds
        refresh.reconcileIntervalSeconds = editable.contains("refresh.reconcileIntervalSeconds")
            ? reconcileIntervalSeconds : current.reconcileIntervalSeconds
        refresh.jsonlDebounceMilliseconds = editable.contains("refresh.jsonlDebounceMilliseconds")
            ? jsonlDebounceMilliseconds : current.jsonlDebounceMilliseconds
        request.refresh = refresh

        var updates = Codexpulse_Core_V1_SettingsUpdatesUpdate()
        updates.autoCheckEnabled = editable.contains("updates.autoCheckEnabled")
            ? autoCheckEnabled : current.autoCheckEnabled
        updates.checkIntervalSeconds = editable.contains("updates.checkIntervalSeconds")
            ? checkIntervalSeconds : current.checkIntervalSeconds
        request.updates = updates

        var ui = Codexpulse_Core_V1_SettingsUIUpdate()
        ui.launchBehavior = editable.contains("ui.launchBehavior") ? launchBehavior : current.launchBehavior
        ui.overviewRange = editable.contains("ui.overviewRange") ? overviewRange : current.overviewRange
        request.ui = ui
        return request
    }
}

public func notices(from meta: Codexpulse_Core_V1_ResponseMeta) -> [AppNotice] {
    meta.issues.map {
        AppNotice(code: $0.code, messageKey: $0.messageKey, retryable: $0.retryable)
    }
}

public func loadState<Value: Sendable>(
    value: Value,
    meta: Codexpulse_Core_V1_ResponseMeta,
    isEmpty: Bool
) -> FeatureLoadState<Value> {
    switch ResponseDisposition(status: meta.status) {
    case .complete:
        return isEmpty ? .empty : .ready(value)
    case .partial:
        return .partial(value, notices: notices(from: meta))
    case .unavailable:
        return .unavailable(notices(from: meta).first ?? AppNotice(
            code: "unavailable",
            messageKey: "app.error.response_unavailable",
            retryable: true
        ))
    case .unsupported(let status):
        return .unavailable(AppNotice(
            code: "contract_unavailable",
            messageKey: "app.error.unsupported_response_status.\(status)",
            retryable: false
        ))
    }
}

public func failedLoadState<Value: Sendable>(
    previous: Value?,
    error: any Error
) -> FeatureLoadState<Value> {
    let notice = AppNotice.from(error)
    if error is CancellationError || notice.code == "cancelled" {
        return .cancelled(previous: previous)
    }
    if let previous { return .stale(previous, notice: notice) }
    return .unavailable(notice)
}

public enum FeatureResponseMerge {
    public static func sessions(
        _ previous: Codexpulse_Core_V1_SessionListResponse?,
        _ next: Codexpulse_Core_V1_SessionListResponse,
        append: Bool
    ) -> Codexpulse_Core_V1_SessionListResponse {
        guard append, let previous else { return next }
        var result = next
        result.items = appendUnique(previous.items, next.items, id: \.sessionID)
        return result
    }

    public static func sessionDetail(
        _ previous: Codexpulse_Core_V1_SessionDetailResponse?,
        _ next: Codexpulse_Core_V1_SessionDetailResponse,
        append: Bool
    ) -> Codexpulse_Core_V1_SessionDetailResponse {
        guard append, let previous else { return next }
        var result = next
        result.turns = appendUnique(previous.turns, next.turns, id: \.timelineKey)
        return result
    }

    public static func projects(
        _ previous: Codexpulse_Core_V1_ProjectListResponse?,
        _ next: Codexpulse_Core_V1_ProjectListResponse,
        append: Bool
    ) -> Codexpulse_Core_V1_ProjectListResponse {
        guard append, let previous else { return next }
        var result = next
        result.items = appendUnique(previous.items, next.items, id: \.dimensionKey)
        return result
    }

    public static func projectDetail(
        _ previous: Codexpulse_Core_V1_ProjectDetailResponse?,
        _ next: Codexpulse_Core_V1_ProjectDetailResponse,
        append: Bool,
        appendSessions: Bool = true,
        appendModels: Bool = true
    ) -> Codexpulse_Core_V1_ProjectDetailResponse {
        guard append, let previous else { return next }
        var result = next
        if appendSessions {
            result.sessions = appendUnique(previous.sessions, next.sessions, id: \.sessionID)
        } else {
            result.sessions = previous.sessions
            result.sessionPage = previous.sessionPage
        }
        if appendModels {
            result.models = appendUnique(previous.models, next.models, id: \.dimensionKey)
        } else {
            result.models = previous.models
            result.modelPage = previous.modelPage
        }
        return result
    }

    public static func sources(
        _ previous: Codexpulse_Core_V1_SourceListResponse?,
        _ next: Codexpulse_Core_V1_SourceListResponse,
        append: Bool
    ) -> Codexpulse_Core_V1_SourceListResponse {
        guard append, let previous else { return next }
        var result = next
        result.items = appendUnique(previous.items, next.items, id: \.sourceKey)
        return result
    }

    public static func jobs(
        _ previous: Codexpulse_Core_V1_JobListResponse?,
        _ next: Codexpulse_Core_V1_JobListResponse,
        append: Bool
    ) -> Codexpulse_Core_V1_JobListResponse {
        guard append, let previous else { return next }
        var result = next
        result.items = appendUnique(previous.items, next.items, id: \.jobID)
        return result
    }

    public static func health(
        _ previous: Codexpulse_Core_V1_HealthListResponse?,
        _ next: Codexpulse_Core_V1_HealthListResponse,
        append: Bool
    ) -> Codexpulse_Core_V1_HealthListResponse {
        guard append, let previous else { return next }
        var result = next
        result.items = appendUnique(previous.items, next.items, id: \.eventID)
        return result
    }

    private static func appendUnique<Value, ID: Hashable>(
        _ previous: [Value],
        _ next: [Value],
        id: KeyPath<Value, ID>
    ) -> [Value] {
        var seen = Set(previous.map { $0[keyPath: id] })
        var result = previous
        for item in next where seen.insert(item[keyPath: id]).inserted {
            result.append(item)
        }
        return result
    }
}

public func pageHasMore(_ meta: Codexpulse_Core_V1_ResponseMeta) -> Bool {
    meta.hasPage && meta.page.hasMore_p && meta.page.hasNextCursor
}

public func safeTimestamp(_ metric: Codexpulse_Core_V1_NumericValue) -> Date? {
    guard metric.hasValue else { return nil }
    return Date(timeIntervalSince1970: TimeInterval(metric.value) / 1_000)
}
