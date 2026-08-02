import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

public struct OverviewResponses: Sendable {
    public let account: Codexpulse_Core_V1_AccountSnapshotResponse?
    public let usage: Codexpulse_Core_V1_UsageCostResponse
    public let weeklyUsage: Codexpulse_Core_V1_UsageCostResponse
    public let tokenActivityUsage: Codexpulse_Core_V1_UsageCostResponse
    public let quota: Codexpulse_Core_V1_QuotaCurrentResponse
    public let quotaPace: Codexpulse_Core_V1_QuotaPaceResponse
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
        quotaPace: Codexpulse_Core_V1_QuotaPaceResponse = .init(),
        account: Codexpulse_Core_V1_AccountSnapshotResponse? = nil,
        sessions: Codexpulse_Core_V1_SessionListResponse,
        projects: Codexpulse_Core_V1_ProjectListResponse,
        health: Codexpulse_Core_V1_HealthProjectionResponse,
        rangeResolution: OverviewRangeResolution? = nil,
        weeklyUsage: Codexpulse_Core_V1_UsageCostResponse? = nil,
        tokenActivityUsage: Codexpulse_Core_V1_UsageCostResponse = .init(),
        weeklyProjects: Codexpulse_Core_V1_ProjectListResponse? = nil,
        weeklyProjectRange: OverviewRangeResolution? = nil,
        additionalNotices: [AppNotice] = []
    ) {
        self.account = account
        self.usage = usage
        self.weeklyUsage = weeklyUsage ?? usage
        self.tokenActivityUsage = tokenActivityUsage
        self.quota = quota
        self.quotaPace = quotaPace
        self.sessions = sessions
        self.projects = projects
        self.weeklyProjects = weeklyProjects ?? projects
        self.health = health
        self.rangeResolution = rangeResolution
        self.weeklyProjectRange = weeklyProjectRange ?? rangeResolution
        self.additionalNotices = additionalNotices
    }

    func replacingAccount(
        _ account: Codexpulse_Core_V1_AccountSnapshotResponse?
    ) -> OverviewResponses {
        OverviewResponses(
            usage: usage,
            quota: quota,
            quotaPace: quotaPace,
            account: account,
            sessions: sessions,
            projects: projects,
            health: health,
            rangeResolution: rangeResolution,
            weeklyUsage: weeklyUsage,
            tokenActivityUsage: tokenActivityUsage,
            weeklyProjects: weeklyProjects,
            weeklyProjectRange: weeklyProjectRange,
            additionalNotices: additionalNotices
        )
    }
}

public enum CodexAccountAvailability: Equatable, Sendable {
    case available
    case empty
    case unavailable
}

public struct CodexAccountPresentation: Equatable, Sendable {
    public let availability: CodexAccountAvailability
    public let type: String?
    public let email: String?
    public let planType: String?
    public let planText: String
    public let emailText: String
    public let accessibilityLabel: String

    public init(_ response: Codexpulse_Core_V1_AccountSnapshotResponse?) {
        let localization = AppLocalizationRegistry.shared.current
        guard let response else {
            availability = .unavailable
            type = nil
            email = nil
            planType = nil
            planText = "--"
            emailText = "--"
            accessibilityLabel = localization.textValue("Codex 账户与套餐信息暂不可用")
            return
        }
        guard response.hasAccount else {
            availability = .empty
            type = nil
            email = nil
            planType = nil
            planText = "--"
            emailText = "--"
            accessibilityLabel = localization.textValue("当前没有 Codex 账户信息")
            return
        }

        let account = response.account
        let normalizedType = Self.nonEmpty(account.type)
        let normalizedEmail = account.hasEmail ? Self.nonEmpty(account.email) : nil
        let normalizedPlan = account.hasPlanType ? Self.nonEmpty(account.planType) : nil
        availability = .available
        type = normalizedType
        email = normalizedEmail
        planType = normalizedPlan

        guard normalizedType == "chatgpt" else {
            planText = "--"
            emailText = "--"
            accessibilityLabel = localization.format(
                "account.type", normalizedType ?? "--"
            )
            return
        }
        planText = Self.planDisplayName(normalizedPlan)
        emailText = normalizedEmail ?? "--"
        accessibilityLabel = localization.format("account.plan", planText, emailText)
    }

    private static func nonEmpty(_ value: String) -> String? {
        let normalized = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return normalized.isEmpty ? nil : normalized
    }

    private static func planDisplayName(_ value: String?) -> String {
        switch value {
        case "free": "Free"
        case "go": "Go"
        case "plus": "Plus"
        case "pro": "Pro"
        case "prolite": "Pro Lite"
        case "team": "Team"
        case "self_serve_business_usage_based", "business": "Business"
        case "enterprise_cbp_usage_based", "enterprise": "Enterprise"
        case "edu": "Edu"
        default: "--"
        }
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
    private struct Magnitude {
        let divisor: Int64
        let unitKey: String
    }

    public static func string(
        _ value: Int64,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String {
        formatted(value, maximumFractionDigits: 2, compact: false, localization: localization)
    }

    public static func compactString(
        _ value: Int64,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String {
        formatted(value, maximumFractionDigits: 1, compact: true, localization: localization)
    }

    public static func stringWithUnit(
        _ value: Int64,
        compact: Bool = false,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String {
        let number = compact
            ? compactString(value, localization: localization)
            : string(value, localization: localization)
        let category = localization.language == .englishUS && value == 1 ? "one" : "other"
        return localization.format("copy.token.count.\(category)", number)
    }

    private static func formatted(
        _ value: Int64,
        maximumFractionDigits: Int,
        compact: Bool,
        localization: AppLocalization
    ) -> String {
        guard value >= 0 else { return "--" }
        let magnitudes = magnitudes(compact: compact, localization: localization)
        guard var magnitudeIndex = magnitudes.lastIndex(where: { value >= $0.divisor }) else {
            return localization.number(value)
        }

        let roundingFactor = pow(10, Double(maximumFractionDigits))
        while magnitudeIndex + 1 < magnitudes.count {
            let current = magnitudes[magnitudeIndex]
            let next = magnitudes[magnitudeIndex + 1]
            let scaled = Double(value) / Double(current.divisor)
            let rounded = (scaled * roundingFactor).rounded() / roundingFactor
            let transition = Double(next.divisor) / Double(current.divisor)
            guard rounded >= transition else { break }
            magnitudeIndex += 1
        }

        let magnitude = magnitudes[magnitudeIndex]
        let formatter = NumberFormatter()
        formatter.locale = localization.locale
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = false
        formatter.minimumFractionDigits = 0
        formatter.maximumFractionDigits = maximumFractionDigits
        let scaled = Double(value) / Double(magnitude.divisor)
        let number = formatter.string(from: NSNumber(value: scaled)) ?? String(scaled)
        return localization.format(magnitude.unitKey, number)
    }

    private static func magnitudes(
        compact: Bool,
        localization: AppLocalization
    ) -> [Magnitude] {
        let prefix = compact ? "copy.token.unit" : "copy.token.displayUnit"
        switch localization.language {
        case .englishUS:
            return [
                Magnitude(divisor: 1_000, unitKey: "\(prefix).thousand"),
                Magnitude(divisor: 1_000_000, unitKey: "\(prefix).million"),
                Magnitude(divisor: 1_000_000_000, unitKey: "\(prefix).billion"),
            ]
        case .chineseSimplified:
            return [
                Magnitude(divisor: 10_000, unitKey: "\(prefix).tenThousand"),
                Magnitude(divisor: 100_000_000, unitKey: "\(prefix).hundredMillion"),
            ]
        }
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
    public let startAtMS: Int64?
    public let totalTokens: Int64
    public let tokenBreakdown: TokenBreakdownPresentation
    public let segments: [UsageModelTrendSegment]
    public let breakdownAvailable: Bool

    public init(
        key: String,
        startAtMS: Int64? = nil,
        totalTokens: Int64,
        tokenBreakdown: TokenBreakdownPresentation,
        segments: [UsageModelTrendSegment],
        breakdownAvailable: Bool
    ) {
        self.id = key
        self.key = key
        self.startAtMS = startAtMS
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
                        modelName: AppLocalizationRegistry.shared.current.textValue("全部模型"),
                        tokens: totalTokens
                    )
                ]
            }
            return UsageModelTrendBucket(
                key: point.key,
                startAtMS: point.startAtMs.hasValue ? point.startAtMs.value : nil,
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
        guard model.hasDisplayName else {
            return AppLocalizationRegistry.shared.current.textValue("其他模型")
        }
        let displayName = model.displayName.trimmingCharacters(in: .whitespacesAndNewlines)
        return displayName.isEmpty
            ? AppLocalizationRegistry.shared.current.textValue("其他模型")
            : displayName
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
        let localization = AppLocalizationRegistry.shared.current
        self.id = "\(window.windowKind):\(window.limitID)"
        self.limitID = window.limitID
        self.limitName = window.hasLimitName ? window.limitName : nil
        let quotaName = Self.quotaName(
            limitID: window.limitID, limitName: limitName, localization: localization
        )
        if let duration = Self.durationTitle(
            windowMinutes: window.hasWindowMinutes ? window.windowMinutes : nil,
            localization: localization
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

    private static func quotaName(
        limitID: String,
        limitName: String?,
        localization: AppLocalization
    ) -> String {
        let trimmedName = limitName?.trimmingCharacters(in: .whitespacesAndNewlines)
        if limitID == "codex" {
            if trimmedName == nil || trimmedName?.isEmpty == true || trimmedName?.lowercased() == "codex" {
                return localization.textValue("通用额度")
            }
        }
        if let trimmedName, !trimmedName.isEmpty { return trimmedName }
        return limitID.isEmpty
            ? localization.textValue("其他额度")
            : localization.textValue("模型专属额度")
    }

    private static func durationTitle(
        windowMinutes: Int64?,
        localization: AppLocalization
    ) -> String? {
        guard let windowMinutes, windowMinutes > 0 else { return nil }
        if windowMinutes >= 2_880, windowMinutes.isMultiple(of: 1_440) {
            return localization.quantity("copy.count.day", count: windowMinutes / 1_440)
        }
        if windowMinutes.isMultiple(of: 60) {
            return localization.quantity("copy.count.hour", count: windowMinutes / 60)
        }
        return localization.quantity("copy.count.minute", count: windowMinutes)
    }
}

public enum QuotaWindowDisplayResolver {
    private struct WindowKey: Hashable {
        let limitID: String
        let windowMinutes: Int64?
    }

    public static func displayWindows(
        _ windows: [Codexpulse_Core_V1_CurrentWindow]
    ) -> [Codexpulse_Core_V1_CurrentWindow] {
        var resolved: [Codexpulse_Core_V1_CurrentWindow] = []
        var indexByKey: [WindowKey: Int] = [:]

        for window in windows {
            let key = WindowKey(
                limitID: window.limitID,
                windowMinutes: window.hasWindowMinutes ? window.windowMinutes : nil
            )
            if let index = indexByKey[key] {
                if preferenceScore(window) > preferenceScore(resolved[index]) {
                    resolved[index] = window
                }
            } else {
                indexByKey[key] = resolved.count
                resolved.append(window)
            }
        }
        return resolved
    }

    private static func preferenceScore(
        _ window: Codexpulse_Core_V1_CurrentWindow
    ) -> Int {
        let trustedResetScore =
            window.hasResetRemainingMs && window.resetRemainingMs > 0 ? 1_000 : 0
        let freshnessScore: Int = switch window.freshness {
        case "fresh": 400
        case "stale": 300
        case "suspicious": 200
        case "expired_unknown": 100
        default: 0
        }
        let trimmedName =
            window.hasLimitName
            ? window.limitName.trimmingCharacters(in: .whitespacesAndNewlines) : ""
        let namedScore = trimmedName.isEmpty ? 0 : 10
        let primaryScore = window.windowKind == "primary" ? 1 : 0
        return trustedResetScore + freshnessScore + namedScore + primaryScore
    }
}

public struct StatusBarQuotaPresentation: Equatable, Sendable {
    public let periodLabel: String
    public let remainingPercent: Double?
    public let usageText: String
    public let freshness: String
    public let dataState: StatusBarQuotaDataState
    public let accessibilityLabel: String

    public init?(_ overview: OverviewPresentation) {
        let localization = AppLocalizationRegistry.shared.current
        guard let window = Self.preferredWindow(overview.quotaWindows) else { return nil }
        let periodLabel = Self.periodLabel(window.windowMinutes, localization: localization)
        self.periodLabel = periodLabel
        self.remainingPercent = window.remainingPercent
        self.freshness = window.freshness
        self.dataState = StatusBarQuotaDataState(freshness: window.freshness)

        let remainingText = window.remainingPercent.map { localization.percent($0) } ?? "--"
        let baseAccessibilityLabel: String
        if let tokens = Self.matchingPeriodTokens(window: window, overview: overview) {
            let total = Self.compact(tokens.total)
            self.usageText = localization.format("status.used", total)
            baseAccessibilityLabel = localization.format(
                "status.accessibility.used",
                periodLabel,
                remainingText,
                Self.compactWithUnit(tokens.total, localization: localization)
            )
        } else {
            self.usageText = localization.textValue("已用 --")
            baseAccessibilityLabel = localization.format(
                "status.accessibility.unavailable", periodLabel, remainingText
            )
        }
        self.accessibilityLabel = baseAccessibilityLabel + dataState.accessibilitySuffix
    }

    public var remainingText: String {
        let localization = AppLocalizationRegistry.shared.current
        let percent = remainingPercent.map { localization.percent($0) } ?? "--"
        return "\(periodLabel) \(percent)"
    }

    private static func preferredWindow(_ windows: [QuotaWindowPresentation]) -> QuotaWindowPresentation? {
        windows.first(where: { $0.limitID == "codex" && $0.windowMinutes == 7 * 24 * 60 })
            ?? windows.first(where: { $0.windowMinutes == 7 * 24 * 60 })
            ?? windows.max { ($0.windowMinutes ?? -1) < ($1.windowMinutes ?? -1) }
    }

    private static func periodLabel(
        _ windowMinutes: Int64?,
        localization: AppLocalization
    ) -> String {
        guard let windowMinutes, windowMinutes > 0 else { return localization.textValue("额度剩") }
        if windowMinutes == 7 * 24 * 60 { return localization.textValue("周剩") }
        if windowMinutes == 24 * 60 { return localization.textValue("日剩") }
        if windowMinutes >= 24 * 60, windowMinutes.isMultiple(of: 24 * 60) {
            return localization.format(
                "status.period",
                localization.quantity("copy.count.day", count: windowMinutes / (24 * 60))
            )
        }
        if windowMinutes.isMultiple(of: 60) {
            return localization.format(
                "status.period",
                localization.quantity("copy.count.hour", count: windowMinutes / 60)
            )
        }
        return localization.format(
            "status.period",
            localization.quantity("copy.count.minute", count: windowMinutes)
        )
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

    private static func compactWithUnit(
        _ metric: DisplayMetric,
        localization: AppLocalization
    ) -> String {
        guard case .known(let value, _) = metric else { return "--" }
        return TokenQuantityFormatter.stringWithUnit(
            value, compact: true, localization: localization
        )
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

public enum OverviewActivityAvailability: Equatable, Sendable {
    case available
    case partial
    case unavailable
}

public enum OverviewActivityTimelineGranularity: String, Equatable, Sendable {
    case hour
    case day
}

public enum OverviewActivityMetric: String, CaseIterable, Equatable, Identifiable, Sendable {
    case tokenConsumption
    case sessionCount

    public var id: String { rawValue }

    public var title: String {
        let value: String = switch self {
        case .tokenConsumption: "Token 消耗"
        case .sessionCount: "会话数量"
        }
        return AppLocalizationRegistry.shared.current.textValue(value)
    }
}

public struct OverviewActivityTimelinePoint: Equatable, Identifiable, Sendable {
    public let id: Int64
    public let startAtMS: Int64
    public let endAtMS: Int64
    public let totalTokens: DisplayMetric
    public let sessionCount: DisplayMetric

    public init(
        id: Int64,
        startAtMS: Int64,
        endAtMS: Int64,
        totalTokens: DisplayMetric,
        sessionCount: DisplayMetric
    ) {
        self.id = id
        self.startAtMS = startAtMS
        self.endAtMS = endAtMS
        self.totalTokens = totalTokens
        self.sessionCount = sessionCount
    }

    public func value(for metric: OverviewActivityMetric) -> Int64? {
        switch metric {
        case .tokenConsumption: Self.knownValue(totalTokens)
        case .sessionCount: Self.knownValue(sessionCount)
        }
    }

    private static func knownValue(_ metric: DisplayMetric) -> Int64? {
        guard case .known(let value, _) = metric else { return nil }
        return value
    }
}

public struct OverviewActivityAxisTick: Equatable, Identifiable, Sendable {
    public let id: Int64
    public let date: Date
    public let label: String

    public init(id: Int64, date: Date, label: String) {
        self.id = id
        self.date = date
        self.label = label
    }
}

public enum OverviewActivityTimelineResolver {
    public static func axisTicks(
        points: [OverviewActivityTimelinePoint],
        granularity: OverviewActivityTimelineGranularity,
        timeZoneID: String,
        maximumCount: Int = 7
    ) -> [OverviewActivityAxisTick] {
        guard maximumCount > 0,
              !points.isEmpty,
              let timeZone = TimeZone(identifier: timeZoneID)
        else { return [] }

        let ordered = points.sorted { $0.startAtMS < $1.startAtMS }
        let selectedDates = axisTickDates(
            points: ordered,
            granularity: granularity,
            maximumCount: maximumCount
        )

        let dayKeyFormatter = dateFormatter(
            format: "yyyy-MM-dd",
            timeZone: timeZone,
            locale: Locale(identifier: "en_US_POSIX")
        )
        let localization = AppLocalizationRegistry.shared.current
        let dateHourFormatter = dateFormatter(
            format: localization.language == .englishUS ? "MMM d\nH:mm" : "M月d日\nH时",
            timeZone: timeZone,
            locale: localization.locale
        )
        let hourFormatter = dateFormatter(
            format: localization.language == .englishUS ? "H:mm" : "H时",
            timeZone: timeZone,
            locale: localization.locale
        )
        let dayFormatter = dateFormatter(
            format: localization.language == .englishUS ? "MMM d" : "M月d日",
            timeZone: timeZone,
            locale: localization.locale
        )
        var previousDayKey: String?
        return selectedDates.map { date in
            let dayKey = dayKeyFormatter.string(from: date)
            let label: String
            if granularity == .day {
                label = dayFormatter.string(from: date)
            } else if dayKey == previousDayKey {
                label = hourFormatter.string(from: date)
            } else {
                label = dateHourFormatter.string(from: date)
            }
            previousDayKey = dayKey
            let id = Int64(date.timeIntervalSince1970 * 1_000)
            return OverviewActivityAxisTick(id: id, date: date, label: label)
        }
    }

    public static func nearest(
        to selectedDate: Date?,
        in points: [OverviewActivityTimelinePoint]
    ) -> OverviewActivityTimelinePoint? {
        guard let selectedDate else { return nil }
        let selectedAtMS = selectedDate.timeIntervalSince1970 * 1_000
        let ordered = points.sorted { $0.startAtMS < $1.startAtMS }
        if let containing = ordered.first(where: {
            selectedAtMS >= Double($0.startAtMS) && selectedAtMS < Double($0.endAtMS)
        }) {
            return containing
        }
        return ordered.min { left, right in
            let leftCenter = Double(left.startAtMS) + Double(left.endAtMS - left.startAtMS) / 2
            let rightCenter = Double(right.startAtMS)
                + Double(right.endAtMS - right.startAtMS) / 2
            let leftDistance = abs(leftCenter - selectedAtMS)
            let rightDistance = abs(rightCenter - selectedAtMS)
            if leftDistance == rightDistance {
                return left.startAtMS < right.startAtMS
            }
            return leftDistance < rightDistance
        }
    }

    public static func visibleRange(
        for point: OverviewActivityTimelinePoint,
        gapFraction: Double = 0.28
    ) -> ClosedRange<Date>? {
        guard point.endAtMS > point.startAtMS,
              gapFraction >= 0,
              gapFraction < 0.5
        else { return nil }
        let duration = Double(point.endAtMS - point.startAtMS)
        let inset = duration * gapFraction
        let lower = Date(
            timeIntervalSince1970: (Double(point.startAtMS) + inset) / 1_000
        )
        let upper = Date(
            timeIntervalSince1970: (Double(point.endAtMS) - inset) / 1_000
        )
        return lower...upper
    }

    private static func dateFormatter(
        format: String,
        timeZone: TimeZone,
        locale: Locale
    ) -> DateFormatter {
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = locale
        formatter.timeZone = timeZone
        formatter.dateFormat = format
        return formatter
    }

    private static func axisTickDates(
        points: [OverviewActivityTimelinePoint],
        granularity: OverviewActivityTimelineGranularity,
        maximumCount: Int
    ) -> [Date] {
        guard let first = points.first, let last = points.last else { return [] }
        let firstDate = Date(timeIntervalSince1970: Double(first.startAtMS) / 1_000)
        guard maximumCount > 1, first.startAtMS < last.startAtMS else {
            return [firstDate]
        }

        switch granularity {
        case .hour:
            let bucketDurationMS: Int64 = points
                .map { $0.endAtMS - $0.startAtMS }
                .filter { $0 > 0 }
                .max() ?? Int64(3_600_000)
            let spanMS = last.startAtMS - first.startAtMS
            let bucketCount = max(
                2,
                Int((spanMS + bucketDurationMS - 1) / bucketDurationMS) + 1
            )
            let tickCount = min(maximumCount, bucketCount)
            let candidates = (0..<tickCount).map { offset in
                if offset == 0 {
                    return firstDate
                }
                if offset == tickCount - 1 {
                    return Date(
                        timeIntervalSince1970: Double(last.startAtMS) / 1_000
                    )
                }
                let position = Double(offset) * Double(spanMS)
                    / Double(tickCount - 1)
                let bucketOffset = Int64(
                    (position / Double(bucketDurationMS)).rounded()
                )
                let timestamp = min(
                    last.startAtMS,
                    first.startAtMS + bucketOffset * bucketDurationMS
                )
                return Date(timeIntervalSince1970: Double(timestamp) / 1_000)
            }
            var seenTimestamps = Set<Int64>()
            return candidates.filter { date in
                seenTimestamps.insert(
                    Int64(date.timeIntervalSince1970 * 1_000)
                ).inserted
            }
        case .day:
            let selected: [OverviewActivityTimelinePoint]
            if points.count <= maximumCount {
                selected = points
            } else {
                selected = (0..<maximumCount).map { offset in
                    let position = Double(offset) * Double(points.count - 1)
                        / Double(maximumCount - 1)
                    return points[Int(position.rounded())]
                }
            }
            return selected.map { point in
                Date(timeIntervalSince1970: Double(point.startAtMS) / 1_000)
            }
        }
    }
}

public struct OverviewActivityHeatmapCell: Equatable, Identifiable, Sendable {
    public let id: String
    public let weekday: Int
    public let hour: Int
    public let totalTokens: DisplayMetric
    public let sessionCount: DisplayMetric

    public func value(for metric: OverviewActivityMetric) -> Int64? {
        switch metric {
        case .tokenConsumption: Self.knownValue(totalTokens)
        case .sessionCount: Self.knownValue(sessionCount)
        }
    }

    private static func knownValue(_ metric: DisplayMetric) -> Int64? {
        guard case .known(let value, _) = metric else { return nil }
        return value
    }
}

public struct OverviewActivityHeatmapRenderCell: Equatable, Identifiable, Sendable {
    public let cell: OverviewActivityHeatmapCell
    public let intensity: TokenActivityIntensity

    public var id: String { cell.id }

    public init(
        cell: OverviewActivityHeatmapCell,
        intensity: TokenActivityIntensity
    ) {
        self.cell = cell
        self.intensity = intensity
    }
}

public struct OverviewActivityHeatmapRenderRow: Equatable, Identifiable, Sendable {
    public let weekday: Int
    public let cells: [OverviewActivityHeatmapRenderCell]

    public var id: Int { weekday }

    public init(weekday: Int, cells: [OverviewActivityHeatmapRenderCell]) {
        self.weekday = weekday
        self.cells = cells
    }
}

public struct OverviewActivityHeatmapRenderPlan: Equatable, Sendable {
    public let rows: [OverviewActivityHeatmapRenderRow]

    public init(
        cells: [OverviewActivityHeatmapCell],
        metric: OverviewActivityMetric
    ) {
        let thresholds = ActivityIntensityScale.thresholds(
            for: cells.compactMap { $0.value(for: metric) }
        )
        rows = (1...7).map { weekday in
            OverviewActivityHeatmapRenderRow(
                weekday: weekday,
                cells: cells
                    .filter { $0.weekday == weekday }
                    .sorted { $0.hour < $1.hour }
                    .map { cell in
                        OverviewActivityHeatmapRenderCell(
                            cell: cell,
                            intensity: ActivityIntensityScale.intensity(
                                for: cell.value(for: metric),
                                thresholds: thresholds
                            )
                        )
                    }
            )
        }
    }
}

public struct OverviewActivityPresentation: Equatable, Sendable {
    public let availability: OverviewActivityAvailability
    public let timelineGranularity: OverviewActivityTimelineGranularity?
    public let timelineBucketMinutes: Int
    public let timeline: [OverviewActivityTimelinePoint]
    public let heatmap: [OverviewActivityHeatmapCell]
    public let reportingTimeZone: String

    public init(_ response: Codexpulse_Core_V1_UsageCostResponse) {
        let timeZoneID = response.reportingTimeZone.isEmpty
            ? response.range.timeZone : response.reportingTimeZone
        reportingTimeZone = timeZoneID
        let parsedGranularity = OverviewActivityTimelineGranularity(
            rawValue: response.activityDistribution.timelineGranularity
        )
        let reportedBucketMinutes = Int(
            response.activityDistribution.timelineBucketMinutes
        )
        let inferredBucketMinutes = parsedGranularity == .hour ? 60 : 1_440
        let bucketMinutes = reportedBucketMinutes > 0
            ? reportedBucketMinutes : inferredBucketMinutes
        guard ["complete", "partial"].contains(response.meta.status),
              response.hasActivityDistribution,
              TimeZone(identifier: timeZoneID) != nil,
              timeZoneID != "Local",
              response.range.timeZone == timeZoneID,
              response.range.startAtMs >= 0,
              response.range.endAtMs > response.range.startAtMs,
              [60, 120, 180, 360, 720, 1_440].contains(bucketMinutes),
              let granularity = parsedGranularity
        else {
            availability = .unavailable
            timelineGranularity = nil
            timelineBucketMinutes = 0
            timeline = []
            heatmap = []
            return
        }

        var timelineRows: [OverviewActivityTimelinePoint] = []
        var timelineIDs = Set<Int64>()
        var structurallyValid = true
        for point in response.activityDistribution.timeline {
            guard point.hasStartAtMs, point.startAtMs.hasValue,
                  point.startAtMs.unit == "milliseconds",
                  point.hasEndAtMs, point.endAtMs.hasValue,
                  point.endAtMs.unit == "milliseconds",
                  point.hasMetrics,
                  point.startAtMs.value >= response.range.startAtMs,
                  point.startAtMs.value < response.range.endAtMs,
                  point.endAtMs.value > point.startAtMs.value,
                  point.endAtMs.value <= response.range.endAtMs,
                  point.endAtMs.value - point.startAtMs.value
                    <= Int64(bucketMinutes) * 60_000,
                  timelineIDs.insert(point.startAtMs.value).inserted,
                  let tokens = Self.metric(point.metrics.totalTokens, unit: "tokens"),
                  let sessions = Self.metric(point.metrics.sessionCount, unit: "count")
            else {
                structurallyValid = false
                continue
            }
            timelineRows.append(OverviewActivityTimelinePoint(
                id: point.startAtMs.value,
                startAtMS: point.startAtMs.value,
                endAtMS: point.endAtMs.value,
                totalTokens: tokens,
                sessionCount: sessions
            ))
        }
        timelineRows.sort { $0.startAtMS < $1.startAtMS }

        var providedCells: [String: OverviewActivityHeatmapCell] = [:]
        for point in response.activityDistribution.weekdayHours {
            let weekday = Int(point.weekday)
            let hour = Int(point.hour)
            let id = "\(weekday)-\(hour)"
            guard (1...7).contains(weekday), (0...23).contains(hour),
                  point.hasMetrics, providedCells[id] == nil,
                  let tokens = Self.metric(point.metrics.totalTokens, unit: "tokens"),
                  let sessions = Self.metric(point.metrics.sessionCount, unit: "count")
            else {
                structurallyValid = false
                continue
            }
            providedCells[id] = OverviewActivityHeatmapCell(
                id: id,
                weekday: weekday,
                hour: hour,
                totalTokens: tokens,
                sessionCount: sessions
            )
        }

        guard structurallyValid else {
            availability = .unavailable
            timelineGranularity = nil
            timelineBucketMinutes = 0
            timeline = []
            heatmap = []
            return
        }
        let expectedTotal = Self.knownValue(DisplayMetric(response.totals.totalTokens))
        let timelineTotal = Self.sumKnown(timelineRows.map(\.totalTokens))
        let heatmapTotal = Self.sumKnown(providedCells.values.map(\.totalTokens))
        let sessionsAreKnown = timelineRows.allSatisfy {
            Self.knownValue($0.sessionCount) != nil
        } && providedCells.values.allSatisfy {
            Self.knownValue($0.sessionCount) != nil
        }
        let reconciled = expectedTotal != nil
            && timelineTotal == expectedTotal
            && heatmapTotal == expectedTotal
            && sessionsAreKnown
        let missingTokens: DisplayMetric = reconciled
            ? .known(0, unit: "tokens") : .absent(unit: "tokens")
        let missingSessions: DisplayMetric = reconciled
            ? .known(0, unit: "count") : .absent(unit: "count")
        var heatmapRows: [OverviewActivityHeatmapCell] = []
        heatmapRows.reserveCapacity(7 * 24)
        for weekday in 1...7 {
            for hour in 0...23 {
                let id = "\(weekday)-\(hour)"
                heatmapRows.append(providedCells[id] ?? OverviewActivityHeatmapCell(
                    id: id,
                    weekday: weekday,
                    hour: hour,
                    totalTokens: missingTokens,
                    sessionCount: missingSessions
                ))
            }
        }

        availability = reconciled ? .available : .partial
        timelineGranularity = granularity
        timelineBucketMinutes = bucketMinutes
        timeline = timelineRows
        heatmap = heatmapRows
    }

    private static func metric(
        _ value: Codexpulse_Core_V1_NumericValue,
        unit expectedUnit: String
    ) -> DisplayMetric? {
        let metric = DisplayMetric(value)
        switch metric {
        case .known(let value, let unit):
            return value >= 0 && unit == expectedUnit ? metric : nil
        case .unknown(let reason, let unit):
            return !reason.isEmpty && unit == expectedUnit ? metric : nil
        case .absent(let unit):
            return unit.isEmpty || unit == expectedUnit ? .absent(unit: expectedUnit) : nil
        }
    }

    private static func knownValue(_ metric: DisplayMetric) -> Int64? {
        guard case .known(let value, _) = metric, value >= 0 else { return nil }
        return value
    }

    private static func sumKnown(_ metrics: [DisplayMetric]) -> Int64? {
        var sum: Int64 = 0
        for metric in metrics {
            guard let value = knownValue(metric) else { return nil }
            let result = sum.addingReportingOverflow(value)
            guard !result.overflow else { return nil }
            sum = result.partialValue
        }
        return sum
    }
}

public enum TokenActivityAvailability: Equatable, Sendable {
    case available
    case partial
    case unavailable
}

public struct TokenActivityDay: Equatable, Identifiable, Sendable {
    public let id: String
    public let dateKey: String
    public let date: Date
    public let tokens: Int64?
    public let turnCount: Int64?

    public init(dateKey: String, date: Date, tokens: Int64?, turnCount: Int64?) {
        self.id = dateKey
        self.dateKey = dateKey
        self.date = date
        self.tokens = tokens
        self.turnCount = turnCount
    }
}

public struct TokenActivityPresentation: Equatable, Sendable {
    public let availability: TokenActivityAvailability
    public let days: [TokenActivityDay]
    public let totalTokens: Int64?
    public let peakDailyTokens: Int64?
    public let activeDays: Int?
    public let currentStreakDays: Int?
    public let longestStreakDays: Int?
    public let reportingTimeZone: String

    public init(
        _ response: Codexpulse_Core_V1_UsageCostResponse,
        now: Date? = nil
    ) {
        let timeZoneID = response.reportingTimeZone.isEmpty
            ? response.range.timeZone : response.reportingTimeZone
        guard let timeZone = TimeZone(identifier: timeZoneID),
              timeZoneID != "Local"
        else {
            availability = .unavailable
            days = []
            totalTokens = nil
            peakDailyTokens = nil
            activeDays = nil
            currentStreakDays = nil
            longestStreakDays = nil
            reportingTimeZone = timeZoneID
            return
        }

        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = timeZone
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.isLenient = false

        let requestedAvailability: TokenActivityAvailability
        switch response.meta.status {
        case "complete": requestedAvailability = .available
        case "partial": requestedAvailability = .partial
        default: requestedAvailability = .unavailable
        }

        let responseEnd = response.range.endAtMs > 0
            ? Date(timeIntervalSince1970: Double(response.range.endAtMs) / 1_000)
            : Date()
        let today = calendar.startOfDay(for: now ?? responseEnd)
        let firstDay = calendar.date(byAdding: .day, value: -364, to: today) ?? today
        var pointsByDate: [String: (tokens: Int64, turns: Int64?)] = [:]
        var pointsAreValid = true
        for point in response.trend {
            guard !point.key.isEmpty,
                  let pointDate = formatter.date(from: point.key),
                  pointDate >= firstDay,
                  pointDate <= today,
                  point.totals.totalTokens.hasValue,
                  point.totals.totalTokens.value >= 0,
                  pointsByDate[point.key] == nil
            else {
                pointsAreValid = false
                continue
            }
            let turns = point.totals.turnCount.hasValue && point.totals.turnCount.value >= 0
                ? point.totals.turnCount.value : nil
            pointsByDate[point.key] = (point.totals.totalTokens.value, turns)
        }

        let responseTotal = response.totals.totalTokens.hasValue
            && response.totals.totalTokens.value >= 0
            ? response.totals.totalTokens.value : nil
        var dailyTotal: Int64 = 0
        var dailyTotalOverflowed = false
        for point in pointsByDate.values {
            let result = dailyTotal.addingReportingOverflow(point.tokens)
            dailyTotal = result.partialValue
            dailyTotalOverflowed = dailyTotalOverflowed || result.overflow
        }
        let reconciledTokenFacts = pointsAreValid
            && !dailyTotalOverflowed
            && responseTotal == dailyTotal
        let completeFacts = requestedAvailability == .available
            && reconciledTokenFacts
        let canSummarizeLocalFacts = requestedAvailability != .unavailable
            && reconciledTokenFacts
        let resolvedAvailability: TokenActivityAvailability
        if requestedAvailability == .unavailable {
            resolvedAvailability = .unavailable
        } else {
            resolvedAvailability = completeFacts ? .available : .partial
        }

        var calendarDays: [TokenActivityDay] = []
        calendarDays.reserveCapacity(365)
        for offset in 0..<365 {
            guard let date = calendar.date(byAdding: .day, value: offset, to: firstDay) else {
                continue
            }
            let key = formatter.string(from: date)
            let point = pointsByDate[key]
            calendarDays.append(TokenActivityDay(
                dateKey: key,
                date: date,
                tokens: point?.tokens ?? (completeFacts ? 0 : nil),
                turnCount: point?.turns
            ))
        }

        availability = resolvedAvailability
        days = calendarDays
        reportingTimeZone = timeZoneID
        guard canSummarizeLocalFacts else {
            totalTokens = nil
            peakDailyTokens = nil
            activeDays = nil
            currentStreakDays = nil
            longestStreakDays = nil
            return
        }

        let observedTokenValues = calendarDays.compactMap(\.tokens)
        let tokenValues = calendarDays.map { $0.tokens ?? 0 }
        totalTokens = responseTotal
        peakDailyTokens = observedTokenValues.max() ?? 0
        activeDays = observedTokenValues.reduce(into: 0) { count, tokens in
            if tokens > 0 { count += 1 }
        }

        var longest = 0
        var running = 0
        for tokens in tokenValues {
            if tokens > 0 {
                running += 1
                longest = max(longest, running)
            } else {
                running = 0
            }
        }
        longestStreakDays = longest

        var currentIndex = tokenValues.count - 1
        if currentIndex >= 0, tokenValues[currentIndex] == 0 {
            currentIndex -= 1
        }
        var current = 0
        while currentIndex >= 0, tokenValues[currentIndex] > 0 {
            current += 1
            currentIndex -= 1
        }
        currentStreakDays = current
    }
}

public enum TokenActivityIntensity: Int, Equatable, Sendable {
    case unknown = -1
    case none = 0
    case low = 1
    case medium = 2
    case high = 3
    case veryHigh = 4
}

public struct ActivityIntensityThresholds: Equatable, Sendable {
    public let low: Int64
    public let medium: Int64
    public let high: Int64
}

public enum ActivityIntensityScale {
    public static func thresholds(for values: [Int64]) -> ActivityIntensityThresholds? {
        let positiveValues = values.filter { $0 > 0 }.sorted()
        guard !positiveValues.isEmpty else { return nil }
        func value(at fraction: Double) -> Int64 {
            positiveValues[Int(Double(positiveValues.count - 1) * fraction)]
        }
        return ActivityIntensityThresholds(
            low: value(at: 0.25),
            medium: value(at: 0.5),
            high: value(at: 0.75)
        )
    }

    public static func intensity(
        for value: Int64?,
        thresholds: ActivityIntensityThresholds?
    ) -> TokenActivityIntensity {
        guard let value else { return .unknown }
        guard value > 0 else { return .none }
        guard let thresholds else { return .low }
        if value <= thresholds.low { return .low }
        if value <= thresholds.medium { return .medium }
        if value <= thresholds.high { return .high }
        return .veryHigh
    }
}

public struct ActivityHeatmapGridPosition: Equatable, Sendable {
    public let column: Int
    public let row: Int

    public init(column: Int, row: Int) {
        self.column = column
        self.row = row
    }
}

public struct ActivityHeatmapGridLayout: Equatable, Sendable {
    public let columnCount: Int
    public let rowCount: Int
    public let horizontalSpacing: CGFloat
    public let verticalSpacing: CGFloat
    public let cellSize: CGFloat
    public let contentSize: CGSize

    public init(
        availableWidth: CGFloat,
        columnCount: Int,
        rowCount: Int,
        horizontalSpacing: CGFloat,
        verticalSpacing: CGFloat,
        minimumCellSize: CGFloat,
        maximumCellSize: CGFloat? = nil
    ) {
        self.columnCount = max(columnCount, 0)
        self.rowCount = max(rowCount, 0)
        self.horizontalSpacing = max(horizontalSpacing, 0)
        self.verticalSpacing = max(verticalSpacing, 0)

        let horizontalGapCount = max(self.columnCount - 1, 0)
        let availableForCells = max(
            availableWidth - self.horizontalSpacing * CGFloat(horizontalGapCount),
            0
        )
        let fittedCellSize = self.columnCount > 0
            ? availableForCells / CGFloat(self.columnCount)
            : 0
        let lowerBoundedSize = max(fittedCellSize, max(minimumCellSize, 0))
        if let maximumCellSize {
            cellSize = min(lowerBoundedSize, max(maximumCellSize, 0))
        } else {
            cellSize = lowerBoundedSize
        }

        contentSize = CGSize(
            width: CGFloat(self.columnCount) * cellSize
                + CGFloat(horizontalGapCount) * self.horizontalSpacing,
            height: CGFloat(self.rowCount) * cellSize
                + CGFloat(max(self.rowCount - 1, 0)) * self.verticalSpacing
        )
    }

    public func cellFrame(column: Int, row: Int) -> CGRect? {
        guard column >= 0, column < columnCount, row >= 0, row < rowCount else {
            return nil
        }
        return CGRect(
            x: CGFloat(column) * (cellSize + horizontalSpacing),
            y: CGFloat(row) * (cellSize + verticalSpacing),
            width: cellSize,
            height: cellSize
        )
    }

    public func position(at point: CGPoint) -> ActivityHeatmapGridPosition? {
        guard point.x >= 0, point.y >= 0,
              point.x < contentSize.width, point.y < contentSize.height,
              cellSize > 0
        else { return nil }

        let column = Int(point.x / (cellSize + horizontalSpacing))
        let row = Int(point.y / (cellSize + verticalSpacing))
        guard let frame = cellFrame(column: column, row: row), frame.contains(point) else {
            return nil
        }
        return ActivityHeatmapGridPosition(column: column, row: row)
    }
}

public enum ActivityHeatmapSelectionDirection: Sendable {
    case increment
    case decrement
}

public enum ActivityHeatmapSelectionResolver {
    public static func move(
        from currentID: String?,
        orderedIDs: [String],
        direction: ActivityHeatmapSelectionDirection
    ) -> String? {
        guard !orderedIDs.isEmpty else { return nil }
        guard let currentID, let index = orderedIDs.firstIndex(of: currentID) else {
            return direction == .increment ? orderedIDs.first : orderedIDs.last
        }
        switch direction {
        case .increment:
            return orderedIDs[min(index + 1, orderedIDs.count - 1)]
        case .decrement:
            return orderedIDs[max(index - 1, 0)]
        }
    }
}

public struct TokenActivityCalendarDay: Equatable, Identifiable, Sendable {
    public let day: TokenActivityDay
    public let intensity: TokenActivityIntensity

    public var id: String { day.id }

    public init(day: TokenActivityDay, intensity: TokenActivityIntensity) {
        self.day = day
        self.intensity = intensity
    }
}

public struct TokenActivityWeek: Equatable, Identifiable, Sendable {
    public let id: Int
    public let days: [TokenActivityCalendarDay?]

    public init(id: Int, days: [TokenActivityCalendarDay?]) {
        self.id = id
        self.days = days
    }
}

public struct TokenActivityMonthLabel: Equatable, Identifiable, Sendable {
    public let id: String
    public let title: String
    public let weekIndex: Int

    public init(id: String, title: String, weekIndex: Int) {
        self.id = id
        self.title = title
        self.weekIndex = weekIndex
    }
}

public struct TokenActivityHoverState: Equatable, Sendable {
    public private(set) var dayID: String?

    public init(dayID: String? = nil) {
        self.dayID = dayID
    }

    public mutating func update(isHovering: Bool, dayID: String) {
        if isHovering {
            self.dayID = dayID
        } else if self.dayID == dayID {
            self.dayID = nil
        }
    }

    public func isHovered(dayID: String) -> Bool {
        self.dayID == dayID
    }
}

public struct TokenActivityCalendarPresentation: Equatable, Sendable {
    public let weeks: [TokenActivityWeek]
    public let monthLabels: [TokenActivityMonthLabel]

    public init(
        _ activity: TokenActivityPresentation,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        guard let first = activity.days.first,
              let timeZone = TimeZone(identifier: activity.reportingTimeZone)
        else {
            weeks = []
            monthLabels = []
            return
        }

        let thresholds = ActivityIntensityScale.thresholds(
            for: activity.days.compactMap(\.tokens)
        )
        let calendarDays = activity.days.map { day in
            TokenActivityCalendarDay(
                day: day,
                intensity: ActivityIntensityScale.intensity(
                    for: day.tokens,
                    thresholds: thresholds
                )
            )
        }

        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        let firstWeekday = calendar.component(.weekday, from: first.date)
        let leadingPadding = (firstWeekday + 5) % 7
        var slots = Array<TokenActivityCalendarDay?>(repeating: nil, count: leadingPadding)
        slots.append(contentsOf: calendarDays.map(Optional.some))
        let trailingPadding = (7 - slots.count % 7) % 7
        slots.append(contentsOf: repeatElement(nil, count: trailingPadding))

        var resolvedWeeks: [TokenActivityWeek] = []
        resolvedWeeks.reserveCapacity(slots.count / 7)
        for start in stride(from: 0, to: slots.count, by: 7) {
            resolvedWeeks.append(TokenActivityWeek(
                id: start / 7,
                days: Array(slots[start..<min(start + 7, slots.count)])
            ))
        }
        weeks = resolvedWeeks

        monthLabels = activity.days.enumerated().compactMap { offset, day in
            guard calendar.component(.day, from: day.date) == 1 else { return nil }
            let month = calendar.component(.month, from: day.date)
            let monthTitle = localization.language == .englishUS
                ? localization.format("overview.month", month)
                : "\(month)月"
            return TokenActivityMonthLabel(
                id: String(day.dateKey.prefix(7)),
                title: monthTitle,
                weekIndex: (leadingPadding + offset) / 7
            )
        }
    }

}

public struct TokenActivityMetricPresentation: Equatable, Identifiable, Sendable {
    public let id: String
    public let title: String
    public let value: String

    public init(id: String, title: String, value: String) {
        self.id = id
        self.title = title
        self.value = value
    }
}

public struct TokenActivityCardPresentation: Equatable, Sendable {
    public let title: String
    public let scope: String
    public let availability: TokenActivityAvailability
    public let metrics: [TokenActivityMetricPresentation]
    public let calendar: TokenActivityCalendarPresentation
    public let notice: String?
    public let reportingTimeZone: String
    private let dayDetails: [String: String]

    public init(
        _ activity: TokenActivityPresentation,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        title = localization.textValue("Token 活动")
        scope = localization.textValue("过去 365 天")
        availability = activity.availability
        let resolvedCalendar = TokenActivityCalendarPresentation(
            activity,
            localization: localization
        )
        calendar = resolvedCalendar
        reportingTimeZone = activity.reportingTimeZone
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = localization.locale
        formatter.timeZone = TimeZone(identifier: activity.reportingTimeZone)
        formatter.dateFormat = localization.language == .englishUS ? "MMM d, yyyy" : "yyyy年M月d日"
        dayDetails = Dictionary(uniqueKeysWithValues: resolvedCalendar.weeks
            .flatMap(\.days)
            .compactMap { $0 }
            .map { value in
                (value.id, Self.dayDetail(value, formatter: formatter, localization: localization))
            })
        switch activity.availability {
        case .available: notice = nil
        case .partial: notice = localization.textValue("仅统计本机现有数据")
        case .unavailable: notice = localization.textValue("年度活动暂时不可用")
        }
        metrics = [
            TokenActivityMetricPresentation(
                id: "total",
                title: localization.textValue("近 365 天 Token"),
                value: Self.tokenText(activity.totalTokens, localization: localization)
            ),
            TokenActivityMetricPresentation(
                id: "peak",
                title: localization.textValue("峰值日 Token"),
                value: Self.tokenText(activity.peakDailyTokens, localization: localization)
            ),
            TokenActivityMetricPresentation(
                id: "active-days",
                title: localization.textValue("活跃天数"),
                value: Self.dayCountText(activity.activeDays, localization: localization)
            ),
            TokenActivityMetricPresentation(
                id: "current-streak",
                title: localization.textValue("当前连续天数"),
                value: Self.dayCountText(activity.currentStreakDays, localization: localization)
            ),
            TokenActivityMetricPresentation(
                id: "longest-streak",
                title: localization.textValue("最长连续天数"),
                value: Self.dayCountText(activity.longestStreakDays, localization: localization)
            ),
        ]
    }

    public func dayDetail(_ value: TokenActivityCalendarDay) -> String {
        dayDetails[value.id] ?? value.day.dateKey
    }

    private static func dayDetail(
        _ value: TokenActivityCalendarDay,
        formatter: DateFormatter,
        localization: AppLocalization
    ) -> String {
        let date = formatter.string(from: value.day.date)
        guard let tokens = value.day.tokens else {
            return localization.format("token.activity.unknown", date)
        }
        let tokenText = TokenQuantityFormatter.stringWithUnit(
            tokens, compact: true, localization: localization
        )
        guard let turns = value.day.turnCount else {
            return localization.format("token.activity.tokens", date, tokenText)
        }
        return localization.format(
            "token.activity.turns",
            date,
            tokenText,
            localization.quantity("copy.count.turn", count: Int64(turns))
        )
    }

    private static func tokenText(
        _ value: Int64?, localization: AppLocalization
    ) -> String {
        value.map { TokenQuantityFormatter.compactString($0, localization: localization) } ?? "--"
    }

    private static func dayCountText(
        _ value: Int?, localization: AppLocalization
    ) -> String {
        value.map { localization.quantity("copy.count.day", count: Int64($0)) } ?? "--"
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
        self.title = item.displayTitle.isEmpty
            ? AppLocalizationRegistry.shared.current.textValue("未命名会话")
            : item.displayTitle
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
        self.title = hasDisplayName
            ? item.project.displayName
            : AppLocalizationRegistry.shared.current.textValue("其他")
        self.tokens = DisplayMetric(item.totals.totalTokens)
        self.tokenBreakdown = TokenBreakdownPresentation(item.totals)
        self.estimatedCost = DisplayMetric(item.totals.estimatedUsdMicros)
        self.isOther = !hasDisplayName
    }

    fileprivate init(otherBreakdown: TokenBreakdownPresentation) {
        self.id = ""
        self.title = AppLocalizationRegistry.shared.current.textValue("其他")
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
    private static let maximumSessionRows = 5
    private static let maximumProjectRows = 5

    public let account: CodexAccountPresentation
    public let quotaWindows: [QuotaWindowPresentation]
    public let quotaPaceWindows: [QuotaPaceWindowPresentation]
    public let resetCredits: ResetCreditsPresentation
    public let evaluatedAtMS: Int64
    public let usageRangeLabel: String
    public let weeklyUsageRangeLabel: String
    public let estimatedCost: DisplayMetric
    public let totalTokens: DisplayMetric
    public let tokenBreakdown: TokenBreakdownPresentation
    public let weeklyTokenBreakdown: TokenBreakdownPresentation
    public let tokenActivity: TokenActivityPresentation
    public let activityDistribution: OverviewActivityPresentation
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
        let requestedRange = responses.rangeResolution?.requestedPreset ?? .quotaWeek
        let isWeeklyQuotaRange = requestedRange == .quotaWeek
        self.account = CodexAccountPresentation(responses.account)
        self.quotaWindows = responses.quota.current.windows.map(QuotaWindowPresentation.init)
        self.quotaPaceWindows = responses.quotaPace.pace.windows.map {
            QuotaPaceWindowPresentation(
                $0,
                evaluatedAtMS: responses.quotaPace.pace.evaluatedAtMs
            )
        }
        self.resetCredits = ResetCreditsPresentation(responses.quota.current.resetCredits)
        self.evaluatedAtMS = responses.quota.current.evaluatedAtMs
        self.usageRangeLabel = Self.usageRangeLabel(
            responses.usage.range,
            isDailyWeeklyRange: isWeeklyQuotaRange
        )
        self.weeklyUsageRangeLabel = Self.usageRangeLabel(
            responses.weeklyUsage.range,
            isDailyWeeklyRange: true
        )
        self.estimatedCost = DisplayMetric(responses.usage.totals.estimatedUsdMicros)
        self.totalTokens = DisplayMetric(responses.usage.totals.totalTokens)
        self.tokenBreakdown = TokenBreakdownPresentation(responses.usage.totals)
        self.weeklyTokenBreakdown = TokenBreakdownPresentation(responses.weeklyUsage.totals)
        self.tokenActivity = TokenActivityPresentation(responses.tokenActivityUsage)
        self.activityDistribution = OverviewActivityPresentation(responses.usage)
        self.trend = responses.usage.trend.map(TrendPresentation.init)
        self.usageModelTrend = UsageModelTrendResolver.buckets(responses.usage)
        self.weeklyUsageModelTrend = UsageModelTrendResolver.buckets(responses.weeklyUsage)
        self.sessions = Array(
            responses.sessions.items.map(SessionPresentation.init)
                .prefix(Self.maximumSessionRows)
        )
        let rawProjects = responses.projects.items.map(ProjectPresentation.init)
        let otherProjectBreakdown = Self.projectOtherBreakdown(
            matched: TokenBreakdownPresentation(responses.projects.matchedTotals),
            projects: rawProjects)
        self.projects = Array(
            Self.mergedProjectRows(rawProjects, otherBreakdown: otherProjectBreakdown)
                .prefix(Self.maximumProjectRows)
        )
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
        self.requestedRange = requestedRange
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

    private static func usageRangeLabel(
        _ range: Codexpulse_Core_V1_UTCTimeRange,
        isDailyWeeklyRange: Bool
    ) -> String {
        let localization = AppLocalizationRegistry.shared.current
        guard range.startAtMs > 0 else { return localization.textValue("周额度周期") }
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = localization.locale
        formatter.timeZone = TimeZone(identifier: range.timeZone) ?? .current
        formatter.dateFormat = if localization.language == .englishUS {
            isDailyWeeklyRange ? "MMM d" : "MMM d HH:mm"
        } else {
            isDailyWeeklyRange ? "M月d日" : "M月d日 HH:mm"
        }
        let start = Date(timeIntervalSince1970: Double(range.startAtMs) / 1_000)
        let label = localization.format("自 %@", formatter.string(from: start))
        return isDailyWeeklyRange
            ? localization.format("range.label.daily", label, localization.textValue("按天"))
            : label
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

    public init(
        _ state: CoreConnectionState,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        switch state {
        case .idle: self = .idle
        case .starting: self = .loading(localization.textValue("正在启动核心组件…"))
        case .handshaking: self = .loading(localization.textValue("正在连接核心组件…"))
        case .loadingOverview: self = .loading(localization.textValue("正在加载概览…"))
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
    public let quotaPace: Codexpulse_Core_V1_QuotaPaceRequest
    public let sessions: Codexpulse_Core_V1_ListSessionsRequest

    public static func make(
        now: Date = Date(),
        sessionLimit: Int32 = 5
    ) -> Self {
        var quota = Codexpulse_Core_V1_QuotaCurrentRequest()
        quota.evaluatedAtMs = Int64(now.timeIntervalSince1970 * 1_000)
        var quotaPace = Codexpulse_Core_V1_QuotaPaceRequest()
        quotaPace.evaluatedAtMs = quota.evaluatedAtMs

        var page = Codexpulse_Core_V1_PageRequest()
        page.limit = sessionLimit
        var query = Codexpulse_Core_V1_QueryRequest()
        query.page = page
        var sessions = Codexpulse_Core_V1_ListSessionsRequest()
        sessions.query = query

        return Self(quota: quota, quotaPace: quotaPace, sessions: sessions)
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

    public static func tokenActivityRequest(
        now: Date = Date(),
        calendar inputCalendar: Calendar = .current
    ) -> Codexpulse_Core_V1_UsageCostRequest {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = inputCalendar.timeZone
        let today = calendar.startOfDay(for: now)
        let start = calendar.date(byAdding: .day, value: -364, to: today) ?? today

        var exactRange = Codexpulse_Core_V1_UTCTimeRange()
        exactRange.startAtMs = Int64(start.timeIntervalSince1970 * 1_000)
        exactRange.endAtMs = Int64(now.timeIntervalSince1970 * 1_000)
        exactRange.timeZone = calendar.timeZone.identifier

        var request = Codexpulse_Core_V1_UsageCostRequest()
        request.exactRange = exactRange
        request.granularity = "day"
        return request
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
        usage.includeActivityDistribution = true

        var sessionPage = Codexpulse_Core_V1_PageRequest()
        sessionPage.limit = sessionLimit
        var sessionSort = Codexpulse_Core_V1_SortTerm()
        sessionSort.field = "totalTokens"
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
