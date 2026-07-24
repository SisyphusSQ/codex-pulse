import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

public struct OverviewResponses: Sendable {
    public let usage: Codexpulse_Core_V1_UsageCostResponse
    public let weeklyUsage: Codexpulse_Core_V1_UsageCostResponse
    public let quota: Codexpulse_Core_V1_QuotaCurrentResponse
    public let sessions: Codexpulse_Core_V1_SessionListResponse
    public let projects: Codexpulse_Core_V1_ProjectListResponse
    public let weeklyProjects: Codexpulse_Core_V1_ProjectListResponse
    public let health: Codexpulse_Core_V1_HealthProjectionResponse
    public let rangeResolution: OverviewRangeResolution?
    public let weeklyProjectRange: OverviewRangeResolution?
    public let additionalNotices: [AppNotice]

    public init(
        usage: Codexpulse_Core_V1_UsageCostResponse,
        quota: Codexpulse_Core_V1_QuotaCurrentResponse,
        sessions: Codexpulse_Core_V1_SessionListResponse,
        projects: Codexpulse_Core_V1_ProjectListResponse,
        health: Codexpulse_Core_V1_HealthProjectionResponse,
        rangeResolution: OverviewRangeResolution? = nil,
        weeklyUsage: Codexpulse_Core_V1_UsageCostResponse? = nil,
        weeklyProjects: Codexpulse_Core_V1_ProjectListResponse? = nil,
        weeklyProjectRange: OverviewRangeResolution? = nil,
        additionalNotices: [AppNotice] = []
    ) {
        self.usage = usage
        self.weeklyUsage = weeklyUsage ?? usage
        self.quota = quota
        self.sessions = sessions
        self.projects = projects
        self.weeklyProjects = weeklyProjects ?? projects
        self.health = health
        self.rangeResolution = rangeResolution
        self.weeklyProjectRange = weeklyProjectRange ?? rangeResolution
        self.additionalNotices = additionalNotices
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

public enum TokenQuantityFormatter {
    public static func string(_ value: Int64) -> String {
        guard value >= 0 else { return "--" }
        let divisor: Double
        let unit: String
        switch value {
        case 100_000_000...:
            divisor = 100_000_000
            unit = "亿"
        case 10_000_000...:
            divisor = 10_000_000
            unit = "千万"
        default:
            divisor = 1_000_000
            unit = "百万"
        }
        return String(format: "%.2f %@", Double(value) / divisor, unit)
    }

    public static func compactString(_ value: Int64) -> String {
        guard value >= 0 else { return "--" }
        let divisor: Double
        let unit: String
        switch value {
        case 100_000_000...:
            divisor = 100_000_000
            unit = "亿"
        case 10_000_000...:
            divisor = 10_000_000
            unit = "千万"
        case 1_000_000...:
            divisor = 1_000_000
            unit = "百万"
        case 10_000...:
            divisor = 10_000
            unit = "万"
        default:
            return String(value)
        }
        var number = String(format: "%.1f", Double(value) / divisor)
        if number.hasSuffix(".0") { number.removeLast(2) }
        return number + unit
    }
}

public struct TokenBreakdownPresentation: Equatable, Sendable {
    public let input: DisplayMetric
    public let cachedInput: DisplayMetric
    public let output: DisplayMetric
    public let reasoning: DisplayMetric
    public let total: DisplayMetric

    public init(_ totals: Codexpulse_Core_V1_UsageTotals) {
        self.init(
            input: DisplayMetric(totals.inputTokens),
            cachedInput: DisplayMetric(totals.cachedInputTokens),
            output: DisplayMetric(totals.outputTokens),
            reasoning: DisplayMetric(totals.reasoningTokens),
            total: DisplayMetric(totals.totalTokens)
        )
    }

    public init(
        input: DisplayMetric,
        cachedInput: DisplayMetric,
        output: DisplayMetric,
        reasoning: DisplayMetric,
        total: DisplayMetric
    ) {
        self.input = input
        self.cachedInput = cachedInput
        self.output = output
        self.reasoning = reasoning
        self.total = total
    }
}

public struct UsageModelTrendSegment: Equatable, Identifiable, Sendable {
    public let id: String
    public let bucketKey: String
    public let modelKey: String
    public let modelName: String
    public let tokens: Int64

    public init(
        id: String,
        bucketKey: String,
        modelKey: String,
        modelName: String,
        tokens: Int64
    ) {
        self.id = id
        self.bucketKey = bucketKey
        self.modelKey = modelKey
        self.modelName = modelName
        self.tokens = tokens
    }
}

public struct UsageModelTrendBucket: Equatable, Identifiable, Sendable {
    public let id: String
    public let key: String
    public let totalTokens: Int64
    public let tokenBreakdown: TokenBreakdownPresentation
    public let segments: [UsageModelTrendSegment]
    public let breakdownAvailable: Bool

    public init(
        key: String,
        totalTokens: Int64,
        tokenBreakdown: TokenBreakdownPresentation,
        segments: [UsageModelTrendSegment],
        breakdownAvailable: Bool
    ) {
        self.id = key
        self.key = key
        self.totalTokens = totalTokens
        self.tokenBreakdown = tokenBreakdown
        self.segments = segments
        self.breakdownAvailable = breakdownAvailable
    }
}

public enum UsageModelTrendResolver {
    private struct ModelDescriptor {
        let key: String
        let name: String
        let order: Int
    }

    private struct ModelValue {
        let descriptor: ModelDescriptor
        var tokens: Int64
    }

    public static func buckets(
        _ response: Codexpulse_Core_V1_UsageCostResponse
    ) -> [UsageModelTrendBucket] {
        var valuesByBucket: [String: [String: ModelValue]] = [:]
        var invalidBuckets = Set<String>()

        for (order, model) in response.models.enumerated() {
            let modelKey = model.dimensionKey.isEmpty ? "model-\(order)" : model.dimensionKey
            let descriptor = ModelDescriptor(
                key: modelKey,
                name: modelDisplayName(model.model),
                order: order
            )
            for point in model.trend {
                guard !point.key.isEmpty else { continue }
                guard point.totals.totalTokens.hasValue,
                      point.totals.totalTokens.value >= 0
                else {
                    invalidBuckets.insert(point.key)
                    continue
                }
                let tokens = point.totals.totalTokens.value
                var bucketValues = valuesByBucket[point.key, default: [:]]
                if var existing = bucketValues[modelKey] {
                    let (sum, overflow) = existing.tokens.addingReportingOverflow(tokens)
                    if overflow {
                        invalidBuckets.insert(point.key)
                    } else {
                        existing.tokens = sum
                        bucketValues[modelKey] = existing
                    }
                } else {
                    bucketValues[modelKey] = ModelValue(descriptor: descriptor, tokens: tokens)
                }
                valuesByBucket[point.key] = bucketValues
            }
        }

        return response.trend.compactMap { point in
            guard point.totals.totalTokens.hasValue,
                  point.totals.totalTokens.value >= 0,
                  !point.key.isEmpty
            else { return nil }

            let totalTokens = point.totals.totalTokens.value
            let modelValues = valuesByBucket[point.key, default: [:]].values.sorted {
                if $0.descriptor.order != $1.descriptor.order {
                    return $0.descriptor.order < $1.descriptor.order
                }
                return $0.descriptor.key < $1.descriptor.key
            }
            var modelTotal: Int64 = 0
            var overflow = false
            for value in modelValues {
                let result = modelTotal.addingReportingOverflow(value.tokens)
                modelTotal = result.partialValue
                overflow = overflow || result.overflow
            }

            let breakdownAvailable = !invalidBuckets.contains(point.key)
                && !overflow
                && modelTotal == totalTokens
                && (!modelValues.isEmpty || totalTokens == 0)
            let segments: [UsageModelTrendSegment]
            if breakdownAvailable {
                segments = modelValues.map { value in
                    UsageModelTrendSegment(
                        id: "\(point.key)|\(value.descriptor.key)",
                        bucketKey: point.key,
                        modelKey: value.descriptor.key,
                        modelName: value.descriptor.name,
                        tokens: value.tokens
                    )
                }
            } else {
                segments = [
                    UsageModelTrendSegment(
                        id: "\(point.key)|all-models",
                        bucketKey: point.key,
                        modelKey: "all-models",
                        modelName: "全部模型",
                        tokens: totalTokens
                    )
                ]
            }
            return UsageModelTrendBucket(
                key: point.key,
                totalTokens: totalTokens,
                tokenBreakdown: TokenBreakdownPresentation(point.totals),
                segments: segments,
                breakdownAvailable: breakdownAvailable
            )
        }
    }

    private static func modelDisplayName(
        _ model: Codexpulse_Core_V1_AttributionValue
    ) -> String {
        guard model.hasDisplayName else { return "其他模型" }
        let displayName = model.displayName.trimmingCharacters(in: .whitespacesAndNewlines)
        return displayName.isEmpty ? "其他模型" : displayName
    }
}

public struct QuotaWindowPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let limitID: String
    public let limitName: String?
    public let title: String
    public let remainingPercent: Double?
    public let freshness: String
    public let unknownReason: String?
    public let windowMinutes: Int64?
    public let resetsAtMS: Int64?
    public let resetRemainingMS: Int64?

    public init(_ window: Codexpulse_Core_V1_CurrentWindow) {
        self.id = "\(window.windowKind):\(window.limitID)"
        self.limitID = window.limitID
        self.limitName = window.hasLimitName ? window.limitName : nil
        let quotaName = Self.quotaName(limitID: window.limitID, limitName: limitName)
        if let duration = Self.durationTitle(
            windowMinutes: window.hasWindowMinutes ? window.windowMinutes : nil
        ) {
            self.title = "\(quotaName) · \(duration)"
        } else {
            self.title = quotaName
        }
        self.remainingPercent = window.hasRemainingPercent ? window.remainingPercent : nil
        self.freshness = window.freshness
        self.unknownReason = window.hasUnknownReason ? window.unknownReason : nil
        self.windowMinutes = window.hasWindowMinutes ? window.windowMinutes : nil
        self.resetsAtMS = window.hasResetsAtMs ? window.resetsAtMs : nil
        self.resetRemainingMS = window.hasResetRemainingMs ? window.resetRemainingMs : nil
    }

    private static func quotaName(limitID: String, limitName: String?) -> String {
        let trimmedName = limitName?.trimmingCharacters(in: .whitespacesAndNewlines)
        if limitID == "codex" {
            if trimmedName == nil || trimmedName?.isEmpty == true || trimmedName?.lowercased() == "codex" {
                return "通用额度"
            }
        }
        if let trimmedName, !trimmedName.isEmpty { return trimmedName }
        return limitID.isEmpty ? "其他额度" : "模型专属额度"
    }

    private static func durationTitle(windowMinutes: Int64?) -> String? {
        guard let windowMinutes, windowMinutes > 0 else { return nil }
        if windowMinutes >= 2_880, windowMinutes.isMultiple(of: 1_440) {
            return "\(windowMinutes / 1_440) 天"
        }
        if windowMinutes.isMultiple(of: 60) {
            return "\(windowMinutes / 60) 小时"
        }
        return "\(windowMinutes) 分钟"
    }
}

public struct StatusBarQuotaPresentation: Equatable, Sendable {
    public let periodLabel: String
    public let remainingPercent: Double?
    public let usageText: String
    public let freshness: String
    public let accessibilityLabel: String

    public init?(_ overview: OverviewPresentation) {
        guard let window = Self.preferredWindow(overview.quotaWindows) else { return nil }
        let periodLabel = Self.periodLabel(window.windowMinutes)
        self.periodLabel = periodLabel
        self.remainingPercent = window.remainingPercent
        self.freshness = window.freshness

        let remainingText = window.remainingPercent.map { String(format: "%.0f%%", $0) } ?? "--"
        if let tokens = Self.matchingPeriodTokens(window: window, overview: overview) {
            let total = Self.compact(tokens.total)
            self.usageText = "已用 \(total)"
            self.accessibilityLabel = "\(periodLabel) \(remainingText)，已用 \(total) Token"
        } else {
            self.usageText = "已用 --"
            self.accessibilityLabel = "\(periodLabel) \(remainingText)，本周期用量暂不可用"
        }
    }

    public var remainingText: String {
        let percent = remainingPercent.map { String(format: "%.0f%%", $0) } ?? "--"
        return "\(periodLabel) \(percent)"
    }

    private static func preferredWindow(_ windows: [QuotaWindowPresentation]) -> QuotaWindowPresentation? {
        windows.first(where: { $0.limitID == "codex" && $0.windowMinutes == 7 * 24 * 60 })
            ?? windows.first(where: { $0.windowMinutes == 7 * 24 * 60 })
            ?? windows.max { ($0.windowMinutes ?? -1) < ($1.windowMinutes ?? -1) }
    }

    private static func periodLabel(_ windowMinutes: Int64?) -> String {
        guard let windowMinutes, windowMinutes > 0 else { return "额度剩" }
        if windowMinutes == 7 * 24 * 60 { return "周剩" }
        if windowMinutes == 24 * 60 { return "日剩" }
        if windowMinutes >= 24 * 60, windowMinutes.isMultiple(of: 24 * 60) {
            return "\(windowMinutes / (24 * 60))天剩"
        }
        if windowMinutes.isMultiple(of: 60) { return "\(windowMinutes / 60)小时剩" }
        return "\(windowMinutes)分钟剩"
    }

    private static func matchingPeriodTokens(
        window: QuotaWindowPresentation,
        overview: OverviewPresentation
    ) -> TokenBreakdownPresentation? {
        guard overview.weeklyUsageAvailable,
              let windowMinutes = window.windowMinutes,
              let resetsAtMS = window.resetsAtMS
        else { return nil }

        let (durationMS, durationOverflow) = windowMinutes.multipliedReportingOverflow(by: 60_000)
        let (periodStartMS, startOverflow) = resetsAtMS.subtractingReportingOverflow(durationMS)
        guard !durationOverflow, !startOverflow,
              overview.weeklyUsageRange.startAtMs == periodStartMS,
              overview.weeklyUsageRange.endAtMs == overview.evaluatedAtMS
        else { return nil }
        return overview.weeklyTokenBreakdown
    }

    private static func compact(_ metric: DisplayMetric) -> String {
        guard case .known(let value, _) = metric else { return "--" }
        return TokenQuantityFormatter.compactString(value)
    }
}

public struct ResetCreditItemPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let status: String
    public let type: String
    public let grantedAtMS: Int64
    public let expiresAtMS: Int64
    public let redeemedAtMS: Int64?
    public let remainingMS: Int64?

    public init(_ item: Codexpulse_Core_V1_CurrentResetCreditItem, index: Int) {
        self.id = "\(index):\(item.grantedAtMs):\(item.expiresAtMs):\(item.status)"
        self.status = item.status
        self.type = item.type
        self.grantedAtMS = item.grantedAtMs
        self.expiresAtMS = item.expiresAtMs
        self.redeemedAtMS = item.hasRedeemedAtMs ? item.redeemedAtMs : nil
        self.remainingMS = item.hasRemainingMs ? item.remainingMs : nil
    }
}

public struct ResetCreditsPresentation: Equatable, Sendable {
    public let availableCount: Int64?
    public let totalCount: Int64?
    public let redeemedCount: Int64?
    public let cumulativeRemainingMS: Int64?
    public let nextExpiresAtMS: Int64?
    public let lastSuccessAtMS: Int64?
    public let freshness: String
    public let unknownReason: String?
    public let items: [ResetCreditItemPresentation]

    public init(_ credits: Codexpulse_Core_V1_CurrentResetCredits) {
        self.availableCount = credits.hasAvailableCount ? credits.availableCount : nil
        self.totalCount = credits.hasTotalCount ? credits.totalCount : nil
        self.redeemedCount = credits.hasRedeemedCount ? credits.redeemedCount : nil
        self.cumulativeRemainingMS = credits.hasCumulativeRemainingMs ? credits.cumulativeRemainingMs : nil
        self.nextExpiresAtMS = credits.hasNextExpiresAtMs ? credits.nextExpiresAtMs : nil
        self.lastSuccessAtMS = credits.hasLastSuccessAtMs ? credits.lastSuccessAtMs : nil
        self.freshness = credits.freshness
        self.unknownReason = credits.hasUnknownReason ? credits.unknownReason : nil
        self.items = credits.items.enumerated().map { ResetCreditItemPresentation($0.element, index: $0.offset) }
    }
}

public struct TrendPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let key: String
    public let startAtMS: Int64?
    public let endAtMS: Int64?
    public let tokens: DisplayMetric
    public let tokenBreakdown: TokenBreakdownPresentation
    public let estimatedCost: DisplayMetric

    public init(_ point: Codexpulse_Core_V1_TrendPoint) {
        self.id = point.key
        self.key = point.key
        self.startAtMS = Self.knownValue(point.startAtMs)
        self.endAtMS = Self.knownValue(point.endAtMs)
        self.tokens = DisplayMetric(point.totals.totalTokens)
        self.tokenBreakdown = TokenBreakdownPresentation(point.totals)
        self.estimatedCost = DisplayMetric(point.totals.estimatedUsdMicros)
    }

    private static func knownValue(_ value: Codexpulse_Core_V1_NumericValue) -> Int64? {
        if case .known(let known, _) = DisplayMetric(value) { return known }
        return nil
    }
}

public enum TrendSelectionResolver {
    public static func nearest(
        to selectedDate: Date?,
        in points: [TrendPresentation]
    ) -> TrendPresentation? {
        guard let selectedDate else { return nil }
        let selectedAtMS = selectedDate.timeIntervalSince1970 * 1_000
        return points.compactMap { point -> (point: TrendPresentation, startAtMS: Int64)? in
            guard let startAtMS = point.startAtMS else { return nil }
            return (point, startAtMS)
        }.min { left, right in
            let leftDistance = abs(Double(left.startAtMS) - selectedAtMS)
            let rightDistance = abs(Double(right.startAtMS) - selectedAtMS)
            if leftDistance == rightDistance {
                return left.startAtMS < right.startAtMS
            }
            return leftDistance < rightDistance
        }?.point
    }
}

public struct SessionPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let title: String
    public let activity: String
    public let tokens: DisplayMetric
    public let tokenBreakdown: TokenBreakdownPresentation
    public let estimatedCost: DisplayMetric
    public let project: String?
    public let lastActivityAtMS: Int64?

    public init(_ item: Codexpulse_Core_V1_SessionItem) {
        self.id = item.sessionID
        self.title = item.displayTitle.isEmpty ? "未命名会话" : item.displayTitle
        self.activity = item.activity
        self.tokens = DisplayMetric(item.totals.totalTokens)
        self.tokenBreakdown = TokenBreakdownPresentation(item.totals)
        self.estimatedCost = DisplayMetric(item.totals.estimatedUsdMicros)
        self.project = item.project.hasDisplayName && !item.project.displayName.isEmpty
            ? item.project.displayName : nil
        self.lastActivityAtMS = item.lastActivityAtMs.hasValue ? item.lastActivityAtMs.value : nil
    }
}

public struct ProjectPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let title: String
    public let tokens: DisplayMetric
    public let tokenBreakdown: TokenBreakdownPresentation
    public let estimatedCost: DisplayMetric
    public let isOther: Bool

    public init(_ item: Codexpulse_Core_V1_ProjectItem) {
        let hasDisplayName = item.project.hasDisplayName && !item.project.displayName.isEmpty
        self.id = item.dimensionKey
        self.title = hasDisplayName ? item.project.displayName : "其他"
        self.tokens = DisplayMetric(item.totals.totalTokens)
        self.tokenBreakdown = TokenBreakdownPresentation(item.totals)
        self.estimatedCost = DisplayMetric(item.totals.estimatedUsdMicros)
        self.isOther = !hasDisplayName
    }

    fileprivate init(otherBreakdown: TokenBreakdownPresentation) {
        self.id = ""
        self.title = "其他"
        self.tokens = otherBreakdown.total
        self.tokenBreakdown = otherBreakdown
        self.estimatedCost = .absent(unit: "usd_micros")
        self.isOther = true
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
    public let resetCredits: ResetCreditsPresentation
    public let evaluatedAtMS: Int64
    public let usageRangeLabel: String
    public let weeklyUsageRangeLabel: String
    public let estimatedCost: DisplayMetric
    public let totalTokens: DisplayMetric
    public let tokenBreakdown: TokenBreakdownPresentation
    public let weeklyTokenBreakdown: TokenBreakdownPresentation
    public let trend: [TrendPresentation]
    public let usageModelTrend: [UsageModelTrendBucket]
    public let weeklyUsageModelTrend: [UsageModelTrendBucket]
    public let sessions: [SessionPresentation]
    public let projects: [ProjectPresentation]
    public let weeklyProjectRanking: [ProjectPresentation]
    public let otherProjectTokens: DisplayMetric?
    public let health: HealthPresentation
    public let usageAvailable: Bool
    public let weeklyUsageAvailable: Bool
    public let quotaAvailable: Bool
    public let sessionsAvailable: Bool
    public let projectsAvailable: Bool
    public let weeklyProjectRankingAvailable: Bool
    public let requestedRange: DateRangePreset
    public let effectiveRange: DateRangePreset
    public let contentRange: Codexpulse_Core_V1_UTCTimeRange
    public let usageRange: Codexpulse_Core_V1_UTCTimeRange
    public let weeklyUsageRange: Codexpulse_Core_V1_UTCTimeRange
    public let fellBackFromQuotaWeek: Bool
    public let notices: [AppNotice]
    public let isPartial: Bool

    public init(_ responses: OverviewResponses) {
        self.quotaWindows = responses.quota.current.windows.map(QuotaWindowPresentation.init)
        self.resetCredits = ResetCreditsPresentation(responses.quota.current.resetCredits)
        self.evaluatedAtMS = responses.quota.current.evaluatedAtMs
        self.usageRangeLabel = Self.usageRangeLabel(responses.usage.range)
        self.weeklyUsageRangeLabel = Self.usageRangeLabel(responses.weeklyUsage.range)
        self.estimatedCost = DisplayMetric(responses.usage.totals.estimatedUsdMicros)
        self.totalTokens = DisplayMetric(responses.usage.totals.totalTokens)
        self.tokenBreakdown = TokenBreakdownPresentation(responses.usage.totals)
        self.weeklyTokenBreakdown = TokenBreakdownPresentation(responses.weeklyUsage.totals)
        self.trend = responses.usage.trend.map(TrendPresentation.init)
        self.usageModelTrend = UsageModelTrendResolver.buckets(responses.usage)
        self.weeklyUsageModelTrend = UsageModelTrendResolver.buckets(responses.weeklyUsage)
        self.sessions = responses.sessions.items.map(SessionPresentation.init)
        let rawProjects = responses.projects.items.map(ProjectPresentation.init)
        let otherProjectBreakdown = Self.projectOtherBreakdown(
            matched: TokenBreakdownPresentation(responses.projects.matchedTotals),
            projects: rawProjects)
        self.projects = Self.mergedProjectRows(rawProjects, otherBreakdown: otherProjectBreakdown)
        let weeklyProjectRows = responses.weeklyProjects.items
            .map(ProjectPresentation.init)
            .filter { !$0.isOther }
        self.weeklyProjectRanking = Array(
            Self.mergedProjectRows(weeklyProjectRows, otherBreakdown: nil).prefix(5))
        self.otherProjectTokens = otherProjectBreakdown?.total
        self.health = HealthPresentation(responses.health)
        self.usageAvailable = Self.isAvailable(responses.usage.meta)
        self.weeklyUsageAvailable = Self.isAvailable(responses.weeklyUsage.meta)
        self.quotaAvailable = Self.isAvailable(responses.quota.meta)
        self.sessionsAvailable = Self.isAvailable(responses.sessions.meta)
        self.projectsAvailable = Self.isAvailable(responses.projects.meta)
        self.weeklyProjectRankingAvailable = Self.isAvailable(responses.weeklyProjects.meta)
        self.requestedRange = responses.rangeResolution?.requestedPreset ?? .quotaWeek
        self.effectiveRange = responses.rangeResolution?.effectivePreset ?? .quotaWeek
        if let resolved = responses.rangeResolution {
            var range = Codexpulse_Core_V1_UTCTimeRange()
            range.startAtMs = resolved.startAtMS
            range.endAtMs = resolved.endAtMS
            range.timeZone = resolved.timeZone
            self.contentRange = range
        } else {
            self.contentRange = responses.usage.range
        }
        self.usageRange = responses.usage.range
        self.weeklyUsageRange = responses.weeklyUsage.range
        self.fellBackFromQuotaWeek = responses.rangeResolution?.fellBackFromQuotaWeek ?? false

        let metas = [
            responses.usage.meta, responses.quota.meta, responses.sessions.meta,
            responses.projects.meta,
        ]
        var notices = responses.additionalNotices + metas.flatMap(\.issues).map {
            AppNotice(code: $0.code, messageKey: $0.messageKey, retryable: $0.retryable)
        }
        if fellBackFromQuotaWeek {
            notices.append(AppNotice(
                code: "quota_week_unavailable",
                messageKey: "app.notice.quota_week_fallback",
                retryable: true
            ))
        }
        self.notices = notices
        self.isPartial = metas.contains {
            switch ResponseDisposition(status: $0.status) {
            case .complete: false
            case .partial, .unavailable, .unsupported: true
            }
        } || responses.health.stale || !responses.health.failure.isEmpty || fellBackFromQuotaWeek
            || !responses.additionalNotices.isEmpty
    }

    private static func usageRangeLabel(_ range: Codexpulse_Core_V1_UTCTimeRange) -> String {
        guard range.startAtMs > 0 else { return "周额度周期" }
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.timeZone = TimeZone(identifier: range.timeZone) ?? .current
        formatter.dateFormat = "M月d日 HH:mm"
        let start = Date(timeIntervalSince1970: Double(range.startAtMs) / 1_000)
        return "自 \(formatter.string(from: start))"
    }

    private static func projectOtherBreakdown(
        matched: TokenBreakdownPresentation,
        projects: [ProjectPresentation]
    ) -> TokenBreakdownPresentation? {
        let total = projectOtherMetric(matched: matched.total, projects: projects, field: \.total)
        guard total != nil else { return nil }
        return TokenBreakdownPresentation(
            input: projectOtherMetric(matched: matched.input, projects: projects, field: \.input)
                ?? .absent(unit: "tokens"),
            cachedInput: projectOtherMetric(
                matched: matched.cachedInput, projects: projects, field: \.cachedInput)
                ?? .absent(unit: "tokens"),
            output: projectOtherMetric(matched: matched.output, projects: projects, field: \.output)
                ?? .absent(unit: "tokens"),
            reasoning: projectOtherMetric(
                matched: matched.reasoning, projects: projects, field: \.reasoning)
                ?? .absent(unit: "tokens"),
            total: total ?? .absent(unit: "tokens")
        )
    }

    private static func projectOtherMetric(
        matched: DisplayMetric,
        projects: [ProjectPresentation],
        field: KeyPath<TokenBreakdownPresentation, DisplayMetric>
    ) -> DisplayMetric? {
        let explicitOther = explicitOtherMetric(projects, field: field)
        guard case .known(let matchedValue, let matchedUnit) = matched else {
            return explicitOther
        }
        var classified: Int64 = 0
        for project in projects where !project.isOther {
            guard case .known(let value, let unit) = project.tokenBreakdown[keyPath: field],
                  unit == matchedUnit
            else {
                return explicitOther
            }
            let (sum, overflow) = classified.addingReportingOverflow(value)
            guard !overflow else { return explicitOther }
            classified = sum
        }
        let (other, overflow) = matchedValue.subtractingReportingOverflow(classified)
        guard !overflow, other > 0 else { return explicitOther }
        return .known(other, unit: matchedUnit)
    }

    private static func explicitOtherMetric(
        _ projects: [ProjectPresentation],
        field: KeyPath<TokenBreakdownPresentation, DisplayMetric>
    ) -> DisplayMetric? {
        var total: Int64 = 0
        var unit: String?
        var fallback: DisplayMetric?
        for project in projects where project.isOther {
            let metric = project.tokenBreakdown[keyPath: field]
            fallback = fallback ?? metric
            guard case .known(let value, let currentUnit) = metric else {
                return fallback
            }
            if let unit, unit != currentUnit { return fallback }
            unit = currentUnit
            let (sum, overflow) = total.addingReportingOverflow(value)
            guard !overflow else { return fallback }
            total = sum
        }
        guard let unit else { return nil }
        return .known(total, unit: unit)
    }

    private static func mergedProjectRows(
        _ projects: [ProjectPresentation],
        otherBreakdown: TokenBreakdownPresentation?
    ) -> [ProjectPresentation] {
        var rows = projects.filter { !$0.isOther }
        if let otherBreakdown {
            rows.append(ProjectPresentation(otherBreakdown: otherBreakdown))
        }
        return rows.enumerated().sorted { left, right in
            let leftValue = knownValue(left.element.tokens)
            let rightValue = knownValue(right.element.tokens)
            switch (leftValue, rightValue) {
            case let (.some(left), .some(right)) where left != right:
                return left > right
            case (.some, .none):
                return true
            case (.none, .some):
                return false
            default:
                return left.offset < right.offset
            }
        }.map(\.element)
    }

    private static func knownValue(_ metric: DisplayMetric) -> Int64? {
        guard case .known(let value, _) = metric else { return nil }
        return value
    }

    private static func isAvailable(_ meta: Codexpulse_Core_V1_ResponseMeta) -> Bool {
        switch ResponseDisposition(status: meta.status) {
        case .complete, .partial: true
        case .unavailable, .unsupported: false
        }
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
    public let quota: Codexpulse_Core_V1_QuotaCurrentRequest
    public let sessions: Codexpulse_Core_V1_ListSessionsRequest

    public static func make(
        now: Date = Date(),
        sessionLimit: Int32 = 5
    ) -> Self {
        var quota = Codexpulse_Core_V1_QuotaCurrentRequest()
        quota.evaluatedAtMs = Int64(now.timeIntervalSince1970 * 1_000)

        var page = Codexpulse_Core_V1_PageRequest()
        page.limit = sessionLimit
        var query = Codexpulse_Core_V1_QueryRequest()
        query.page = page
        var sessions = Codexpulse_Core_V1_ListSessionsRequest()
        sessions.query = query

        return Self(quota: quota, sessions: sessions)
    }

    public static func weeklyUsageRequest(
        quota: Codexpulse_Core_V1_QuotaCurrentResponse,
        calendar inputCalendar: Calendar = .current
    ) -> Codexpulse_Core_V1_UsageCostRequest? {
        let now = Date(timeIntervalSince1970: TimeInterval(quota.current.evaluatedAtMs) / 1_000)
        let range = resolveRange(.quotaWeek, quota: quota, now: now, calendar: inputCalendar)
        guard !range.fellBackFromQuotaWeek else { return nil }
        return content(range: range).usage
    }

    public static func resolveRange(
        _ requestedPreset: DateRangePreset,
        quota: Codexpulse_Core_V1_QuotaCurrentResponse,
        now: Date = Date(),
        calendar inputCalendar: Calendar = .current
    ) -> OverviewRangeResolution {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = inputCalendar.timeZone
        let nowAtMS = Int64(now.timeIntervalSince1970 * 1_000)
        if requestedPreset == .quotaWeek,
           let exact = weeklyQuotaRange(quota: quota, timeZone: calendar.timeZone) {
            return OverviewRangeResolution(
                requestedPreset: requestedPreset,
                effectivePreset: .quotaWeek,
                startAtMS: exact.startAtMs,
                endAtMS: exact.endAtMs,
                timeZone: exact.timeZone,
                granularity: "day",
                fellBackFromQuotaWeek: false
            )
        }

        let effectivePreset: DateRangePreset = requestedPreset == .quotaWeek ? .sevenDays : requestedPreset
        let days: Int
        switch effectivePreset {
        case .today: days = 1
        case .sevenDays, .quotaWeek: days = 7
        case .thirtyDays, .all: days = 30
        }
        let today = calendar.startOfDay(for: now)
        let start = calendar.date(byAdding: .day, value: -(days - 1), to: today) ?? today
        return OverviewRangeResolution(
            requestedPreset: requestedPreset,
            effectivePreset: effectivePreset,
            startAtMS: Int64(start.timeIntervalSince1970 * 1_000),
            endAtMS: nowAtMS,
            timeZone: calendar.timeZone.identifier,
            granularity: effectivePreset == .today ? "hour" : "day",
            fellBackFromQuotaWeek: requestedPreset == .quotaWeek
        )
    }

    public static func content(
        range: OverviewRangeResolution,
        sessionLimit: Int32 = 5,
        projectLimit: Int32 = 5
    ) -> OverviewContentRequestSet {
        var exactRange = Codexpulse_Core_V1_UTCTimeRange()
        exactRange.startAtMs = range.startAtMS
        exactRange.endAtMs = range.endAtMS
        exactRange.timeZone = range.timeZone

        var usage = Codexpulse_Core_V1_UsageCostRequest()
        usage.exactRange = exactRange
        usage.granularity = range.granularity

        var sessionPage = Codexpulse_Core_V1_PageRequest()
        sessionPage.limit = sessionLimit
        var sessionSort = Codexpulse_Core_V1_SortTerm()
        sessionSort.field = "lastActivityAt"
        sessionSort.direction = "desc"
        var sessionQuery = Codexpulse_Core_V1_QueryRequest()
        sessionQuery.page = sessionPage
        sessionQuery.sort = [sessionSort]
        sessionQuery.exactTimeRange = exactRange
        var sessions = Codexpulse_Core_V1_ListSessionsRequest()
        sessions.query = sessionQuery

        var projectPage = Codexpulse_Core_V1_PageRequest()
        projectPage.limit = projectLimit
        var projectSort = Codexpulse_Core_V1_SortTerm()
        projectSort.field = "totalTokens"
        projectSort.direction = "desc"
        var projectQuery = Codexpulse_Core_V1_QueryRequest()
        projectQuery.page = projectPage
        projectQuery.sort = [projectSort]
        projectQuery.exactTimeRange = exactRange
        var projects = Codexpulse_Core_V1_ListProjectsRequest()
        projects.query = projectQuery

        return OverviewContentRequestSet(usage: usage, sessions: sessions, projects: projects)
    }

    public static func weeklyProjectRanking(
        range: OverviewRangeResolution,
        limit: Int32 = 5
    ) -> Codexpulse_Core_V1_ListProjectsRequest? {
        guard range.effectivePreset == .quotaWeek, !range.fellBackFromQuotaWeek else { return nil }

        var exactRange = Codexpulse_Core_V1_UTCTimeRange()
        exactRange.startAtMs = range.startAtMS
        exactRange.endAtMs = range.endAtMS
        exactRange.timeZone = range.timeZone

        var page = Codexpulse_Core_V1_PageRequest()
        page.limit = min(max(limit, 1), 100)
        var sort = Codexpulse_Core_V1_SortTerm()
        sort.field = "totalTokens"
        sort.direction = "desc"
        var classified = Codexpulse_Core_V1_FilterTerm()
        classified.field = "confidence"
        classified.operator = "in"
        classified.values = ["high", "medium", "low"]

        var query = Codexpulse_Core_V1_QueryRequest()
        query.page = page
        query.sort = [sort]
        query.filters = [classified]
        query.exactTimeRange = exactRange
        var request = Codexpulse_Core_V1_ListProjectsRequest()
        request.query = query
        return request
    }

    private static func weeklyQuotaRange(
        quota: Codexpulse_Core_V1_QuotaCurrentResponse,
        timeZone: TimeZone
    ) -> Codexpulse_Core_V1_UTCTimeRange? {
        let weeklyMinutes: Int64 = 7 * 24 * 60
        let weeklyWindows = quota.current.windows.filter {
            $0.hasWindowMinutes && $0.windowMinutes == weeklyMinutes && $0.hasResetsAtMs
        }
        guard let window = weeklyWindows.first(where: { $0.limitID == "codex" })
            ?? weeklyWindows.first
        else { return nil }
        let (durationMS, durationOverflow) = window.windowMinutes.multipliedReportingOverflow(by: 60_000)
        let (startAtMS, startOverflow) = window.resetsAtMs.subtractingReportingOverflow(durationMS)
        let endAtMS = quota.current.evaluatedAtMs
        guard !durationOverflow, !startOverflow, startAtMS >= 0,
              endAtMS > startAtMS, endAtMS <= window.resetsAtMs
        else { return nil }
        var range = Codexpulse_Core_V1_UTCTimeRange()
        range.startAtMs = startAtMS
        range.endAtMs = endAtMS
        range.timeZone = timeZone.identifier
        return range
    }
}

public struct OverviewRangeResolution: Equatable, Sendable {
    public let requestedPreset: DateRangePreset
    public let effectivePreset: DateRangePreset
    public let startAtMS: Int64
    public let endAtMS: Int64
    public let timeZone: String
    public let granularity: String
    public let fellBackFromQuotaWeek: Bool
}

public struct OverviewContentRequestSet: Sendable {
    public let usage: Codexpulse_Core_V1_UsageCostRequest
    public let sessions: Codexpulse_Core_V1_ListSessionsRequest
    public let projects: Codexpulse_Core_V1_ListProjectsRequest
}
