import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

public struct OverviewResponses: Sendable {
    public let usage: Codexpulse_Core_V1_UsageCostResponse
    public let quota: Codexpulse_Core_V1_QuotaCurrentResponse
    public let sessions: Codexpulse_Core_V1_SessionListResponse
    public let health: Codexpulse_Core_V1_HealthProjectionResponse

    public init(
        usage: Codexpulse_Core_V1_UsageCostResponse,
        quota: Codexpulse_Core_V1_QuotaCurrentResponse,
        sessions: Codexpulse_Core_V1_SessionListResponse,
        health: Codexpulse_Core_V1_HealthProjectionResponse
    ) {
        self.usage = usage
        self.quota = quota
        self.sessions = sessions
        self.health = health
    }
}

public struct AppNotice: Equatable, Sendable {
    public let code: String
    public let messageKey: String
    public let retryable: Bool

    public init(code: String, messageKey: String, retryable: Bool) {
        self.code = code
        self.messageKey = messageKey
        self.retryable = retryable
    }

    public static func from(_ error: any Error) -> Self {
        if error is CancellationError {
            return Self(code: "cancelled", messageKey: "app.error.cancelled", retryable: true)
        }
        if let detail = CoreErrorDetail.decode(from: error) {
            return Self(code: detail.code, messageKey: detail.messageKey, retryable: detail.retryable)
        }
        if error is CoreClientError {
            return Self(
                code: "contract_unavailable",
                messageKey: "app.error.core_contract",
                retryable: false
            )
        }
        return Self(code: "core_unavailable", messageKey: "app.error.core_unavailable", retryable: true)
    }
}

public enum CoreConnectionState: Sendable {
    case idle
    case starting
    case handshaking
    case loadingOverview
    case normal(OverviewResponses)
    case partial(OverviewResponses, [AppNotice])
    case recovery(Codexpulse_Core_V1_MigrationRecoverySnapshot)
    case restartRequired
    case stale(OverviewResponses, AppNotice)
    case unavailable(AppNotice)
    case cancelled
    case shuttingDown
    case stopped
}

public enum DisplayMetric: Equatable, Sendable {
    case known(Int64, unit: String)
    case unknown(reason: String, unit: String)
    case absent(unit: String)

    public init(_ value: Codexpulse_Core_V1_NumericValue) {
        switch NumericState(value) {
        case .known(let value, let unit): self = .known(value, unit: unit)
        case .unknown(let reason, let unit): self = .unknown(reason: reason, unit: unit)
        case .absent(let unit): self = .absent(unit: unit)
        }
    }
}

public struct QuotaWindowPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let title: String
    public let remainingPercent: Double?
    public let freshness: String
    public let unknownReason: String?

    public init(_ window: Codexpulse_Core_V1_CurrentWindow) {
        self.id = "\(window.windowKind):\(window.limitID)"
        switch window.windowKind {
        case "primary": self.title = "5 小时"
        case "secondary": self.title = "本周"
        default: self.title = "其他额度"
        }
        self.remainingPercent = window.hasRemainingPercent ? window.remainingPercent : nil
        self.freshness = window.freshness
        self.unknownReason = window.hasUnknownReason ? window.unknownReason : nil
    }
}

public struct TrendPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let key: String
    public let tokens: DisplayMetric
    public let estimatedCost: DisplayMetric

    public init(_ point: Codexpulse_Core_V1_TrendPoint) {
        self.id = point.key
        self.key = point.key
        self.tokens = DisplayMetric(point.totals.totalTokens)
        self.estimatedCost = DisplayMetric(point.totals.estimatedUsdMicros)
    }
}

public struct SessionPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let title: String
    public let activity: String
    public let tokens: DisplayMetric

    public init(_ item: Codexpulse_Core_V1_SessionItem) {
        self.id = item.sessionID
        self.title = item.displayTitle.isEmpty ? "未命名会话" : item.displayTitle
        self.activity = item.activity
        self.tokens = DisplayMetric(item.totals.totalTokens)
    }
}

public struct HealthPresentation: Equatable, Sendable {
    public let hasValue: Bool
    public let stale: Bool
    public let level: String?
    public let failure: String?
    public let primaryReason: String?

    public init(_ response: Codexpulse_Core_V1_HealthProjectionResponse) {
        self.hasValue = response.hasValue_p
        self.stale = response.stale
        self.level = response.hasLevel ? response.level : nil
        self.failure = response.failure.isEmpty ? nil : response.failure
        self.primaryReason = response.hasPrimary ? response.primary.reason : nil
    }
}

public struct OverviewPresentation: Equatable, Sendable {
    public let quotaWindows: [QuotaWindowPresentation]
    public let estimatedCost: DisplayMetric
    public let totalTokens: DisplayMetric
    public let trend: [TrendPresentation]
    public let sessions: [SessionPresentation]
    public let health: HealthPresentation
    public let notices: [AppNotice]
    public let isPartial: Bool

    public init(_ responses: OverviewResponses) {
        self.quotaWindows = responses.quota.current.windows.map(QuotaWindowPresentation.init)
        self.estimatedCost = DisplayMetric(responses.usage.totals.estimatedUsdMicros)
        self.totalTokens = DisplayMetric(responses.usage.totals.totalTokens)
        self.trend = responses.usage.trend.map(TrendPresentation.init)
        self.sessions = responses.sessions.items.map(SessionPresentation.init)
        self.health = HealthPresentation(responses.health)

        let metas = [responses.usage.meta, responses.quota.meta, responses.sessions.meta]
        self.notices = metas.flatMap(\.issues).map {
            AppNotice(code: $0.code, messageKey: $0.messageKey, retryable: $0.retryable)
        }
        self.isPartial = metas.contains {
            switch ResponseDisposition(status: $0.status) {
            case .complete: false
            case .partial, .unavailable, .unsupported: true
            }
        } || responses.health.stale || !responses.health.failure.isEmpty
    }
}

public enum AppViewState: Equatable, Sendable {
    case idle
    case loading(String)
    case overview(OverviewPresentation)
    case partial(OverviewPresentation)
    case stale(OverviewPresentation, AppNotice)
    case recovery(phase: String, stage: String, code: String)
    case restartRequired
    case unavailable(AppNotice)
    case cancelled
    case shuttingDown
    case stopped

    public init(_ state: CoreConnectionState) {
        switch state {
        case .idle: self = .idle
        case .starting: self = .loading("正在启动核心组件…")
        case .handshaking: self = .loading("正在连接核心组件…")
        case .loadingOverview: self = .loading("正在加载概览…")
        case .normal(let responses):
            let presentation = OverviewPresentation(responses)
            self = presentation.isPartial ? .partial(presentation) : .overview(presentation)
        case .partial(let responses, _): self = .partial(OverviewPresentation(responses))
        case .recovery(let snapshot):
            self = .recovery(phase: snapshot.phase, stage: snapshot.stage, code: snapshot.code)
        case .restartRequired: self = .restartRequired
        case .stale(let responses, let notice):
            self = .stale(OverviewPresentation(responses), notice)
        case .unavailable(let notice): self = .unavailable(notice)
        case .cancelled: self = .cancelled
        case .shuttingDown: self = .shuttingDown
        case .stopped: self = .stopped
        }
    }
}

public struct OverviewRequestSet: Sendable {
    public let usage: Codexpulse_Core_V1_UsageCostRequest
    public let quota: Codexpulse_Core_V1_QuotaCurrentRequest
    public let sessions: Codexpulse_Core_V1_ListSessionsRequest

    public static func make(
        now: Date = Date(),
        calendar inputCalendar: Calendar = .current,
        sessionLimit: Int32 = 5
    ) -> Self {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = inputCalendar.timeZone
        let timezone = calendar.timeZone.identifier
        let today = calendar.startOfDay(for: now)
        let start = calendar.date(byAdding: .day, value: -6, to: today) ?? today
        let end = calendar.date(byAdding: .day, value: 1, to: today) ?? now
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.timeZone = calendar.timeZone
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"

        var range = Codexpulse_Core_V1_LocalDateRange()
        range.startDate = formatter.string(from: start)
        range.endDateExclusive = formatter.string(from: end)
        range.timeZone = timezone

        var usage = Codexpulse_Core_V1_UsageCostRequest()
        usage.range = range
        usage.granularity = "day"

        var quota = Codexpulse_Core_V1_QuotaCurrentRequest()
        quota.evaluatedAtMs = Int64(now.timeIntervalSince1970 * 1_000)

        var page = Codexpulse_Core_V1_PageRequest()
        page.limit = sessionLimit
        var query = Codexpulse_Core_V1_QueryRequest()
        query.page = page
        var sessions = Codexpulse_Core_V1_ListSessionsRequest()
        sessions.query = query

        return Self(usage: usage, quota: quota, sessions: sessions)
    }
}
