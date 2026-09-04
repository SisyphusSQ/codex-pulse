import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

public struct DashboardSummaryTrendChartLayout: Equatable, Sendable {
    public let barWidth: Double
    public let horizontalPlotPadding: Double
    private let maximumAxisTickCount: Int

    public init(range: DateRangePreset) {
        barWidth = switch range {
        case .today: 24
        case .sevenDays: 64
        case .quotaWeek: 34
        case .thirtyDays, .quotaMonth, .all: 20
        }
        horizontalPlotPadding = barWidth / 2 + 4
        maximumAxisTickCount = range == .today ? 6 : 7
    }

    public func axisTickIndices(pointCount: Int) -> [Int] {
        guard pointCount > 0 else { return [] }
        let tickCount = min(pointCount, maximumAxisTickCount)
        guard tickCount > 1 else { return [0] }

        let lastIndex = pointCount - 1
        return (0..<tickCount).map { offset in
            Int(
                (Double(offset) * Double(lastIndex) / Double(tickCount - 1)).rounded()
            )
        }
    }
}

public struct DashboardSummaryPresentation: Equatable, Sendable {
    public let response: Codexpulse_Core_V1_DashboardSummaryResponse
    public let coverage: Coverage
    public let totals: Totals
    public let providers: [ProviderCard]
    public let distribution: [ProviderShare]
    public let trend: [TrendPoint]
    public let models: [ModelRow]
    public let quotas: [QuotaCard]
    public let activityCoverage: Coverage.State
    public let activityProviderCoverage: [ActivityProviderCoverage]
    public let reportingTimeZone: String
    public let rangeStart: Date?
    public let rangeEnd: Date?
    public let isPartial: Bool
    public let isEmpty: Bool
    public let isUnavailable: Bool

    public init(_ response: Codexpulse_Core_V1_DashboardSummaryResponse) {
        self.response = response
        coverage = Coverage(response.coverage)
        totals = Totals(response.totals, coverage: response.coverage)
        providers = response.providers.compactMap(ProviderCard.init)
        distribution = response.distribution.compactMap(ProviderShare.init)
        trend = response.trend.map(TrendPoint.init)
        models = response.models.compactMap(ModelRow.init)
        quotas = response.quotas.compactMap(QuotaCard.init)
        activityCoverage = Coverage.State(rawValue: response.activityCoverageState)
        activityProviderCoverage = response.activityProviderCoverage.compactMap(ActivityProviderCoverage.init)
        reportingTimeZone = response.reportingTimeZone
        rangeStart = Self.date(response.range.startAtMs)
        rangeEnd = response.range.endAtMs > response.range.startAtMs
            ? Self.date(response.range.endAtMs - 1)
            : nil
        let status = ResponseDisposition(status: response.meta.status)
        let containsUnknownProvider = response.providers.contains { AgentProvider(rawValue: $0.provider) == nil }
            || response.distribution.contains { AgentProvider(rawValue: $0.provider) == nil }
            || response.models.contains { AgentProvider(rawValue: $0.provider) == nil }
            || response.quotas.contains { AgentProvider(rawValue: $0.provider) == nil }
            || response.trend.contains { point in
                point.shares.contains { AgentProvider(rawValue: $0.provider) == nil }
            }
            || response.activityProviderCoverage.contains { AgentProvider(rawValue: $0.provider) == nil }
            || response.activity.contains { point in
                point.shares.contains { AgentProvider(rawValue: $0.provider) == nil }
            }
        isPartial = status == .partial || coverage.overall == .partial || containsUnknownProvider
        isEmpty = coverage.overall == .empty
        isUnavailable = status == .unavailable || coverage.overall == .unavailable
    }

    public func costCoverageCaption(localization: AppLocalization) -> String {
        let coverageText = localization.format(
            "dashboard.coverage.clients",
            localization.number(Int64(coverage.knownCostProviderCount)),
            localization.quantity("copy.count.client", count: Int64(coverage.totalProviderCount))
        )
        guard coverage.cost == .partial, totals.estimatedCost.isKnown else {
            return coverageText
        }
        return localization.format("dashboard.cost.partial_coverage", coverageText)
    }

    public struct Coverage: Equatable, Sendable {
        public let knownProviderCount: Int32
        public let knownCostProviderCount: Int32
        public let totalProviderCount: Int32
        public let token: State
        public let cost: State
        public let overall: State

        public init(_ value: Codexpulse_Core_V1_DashboardSummaryCoverage) {
            knownProviderCount = value.knownProviderCount
            knownCostProviderCount = value.knownCostProviderCount
            totalProviderCount = value.totalProviderCount
            token = State(rawValue: value.tokenState)
            cost = State(rawValue: value.costState)
            overall = State(rawValue: value.overallState)
        }

        public enum State: Equatable, Sendable {
            case complete, partial, empty, unavailable, unknown

            init(rawValue: String) {
                self = switch rawValue {
                case "complete": .complete
                case "partial": .partial
                case "empty": .empty
                case "unavailable": .unavailable
                default: .unknown
                }
            }
        }
    }

    public struct Totals: Equatable, Sendable {
        public let tokens: DisplayMetric
        public let estimatedCost: DisplayMetric
        public let activeProviders: DisplayMetric
        public let tokenCaption: String
        public let costCaption: String
        public let activeProviderCaption: String

        public init(
            _ value: Codexpulse_Core_V1_DashboardSummaryTotals,
            coverage: Codexpulse_Core_V1_DashboardSummaryCoverage
        ) {
            let localization = AppLocalizationRegistry.shared.current
            tokens = DisplayMetric(value.totalTokens)
            estimatedCost = DisplayMetric(value.estimatedUsdMicros)
            activeProviders = DisplayMetric(value.activeProviderCount)
            tokenCaption = Self.knownCaption(
                localization.textValue("已知 Token"),
                known: coverage.knownProviderCount,
                total: coverage.totalProviderCount,
                state: coverage.tokenState,
                localization: localization
            )
            costCaption = Self.knownCaption(
                localization.textValue("已知 API 等价成本"),
                known: coverage.knownCostProviderCount,
                total: coverage.totalProviderCount,
                state: coverage.costState,
                localization: localization
            )
            activeProviderCaption = Self.knownCaption(
                localization.textValue("活跃客户端"),
                known: coverage.knownProviderCount,
                total: coverage.totalProviderCount,
                state: coverage.tokenState,
                localization: localization
            )
        }

        private static func knownCaption(
            _ title: String,
            known: Int32,
            total: Int32,
            state: String,
            localization: AppLocalization
        ) -> String {
            if state == "complete" || state == "empty" {
                return title
            }
            return localization.format(
                "dashboard.coverage.known",
                title,
                localization.number(Int64(known)),
                localization.quantity("copy.count.client", count: Int64(total))
            )
        }
    }

    public struct ProviderCard: Equatable, Sendable, Identifiable {
        public var id: String { provider.rawValue }
        public let provider: AgentProvider
        public let coverage: Coverage.State
        public let costState: Coverage.State
        public let tokens: DisplayMetric
        public let estimatedCost: DisplayMetric
        public let reportedCost: DisplayMetric
        public let reportedCostSource: String?
        public let modelCount: Int32

        public init?(_ value: Codexpulse_Core_V1_DashboardSummaryProviderSlice) {
            guard let provider = AgentProvider(rawValue: value.provider) else { return nil }
            self.provider = provider
            coverage = Coverage.State(rawValue: value.coverageState)
            costState = Coverage.State(rawValue: value.costState)
            tokens = DisplayMetric(value.totals.totalTokens)
            estimatedCost = DisplayMetric(value.totals.estimatedUsdMicros)
            reportedCost = DisplayMetric(value.reportedUsdMicros)
            reportedCostSource = value.hasReportedCostSource ? value.reportedCostSource : nil
            modelCount = max(value.modelCount, 0)
        }

        public func costDetails(localization: AppLocalization) -> CostDetails {
            var estimatedText = Self.costText(estimatedCost, localization: localization)
            if costState == .partial, estimatedCost.isKnown {
                estimatedText = localization.format(
                    "dashboard.cost.partial_estimate",
                    estimatedText
                )
            }
            let reportedText = provider == .cursor && reportedCost.isKnown
                ? localization.format(
                    "dashboard.reported.cost",
                    Self.costText(reportedCost, localization: localization)
                )
                : nil
            return CostDetails(estimatedText: estimatedText, reportedText: reportedText)
        }

        public struct CostDetails: Equatable, Sendable {
            public let estimatedText: String
            public let reportedText: String?
        }

        private static func costText(
            _ metric: DisplayMetric,
            localization: AppLocalization
        ) -> String {
            guard case .known(let value, _) = metric else { return "--" }
            return String(
                format: "$%.2f",
                locale: localization.locale,
                Double(value) / 1_000_000
            )
        }
    }

    public struct ActivityProviderCoverage: Equatable, Sendable, Identifiable {
        public var id: String { provider.rawValue }
        public let provider: AgentProvider
        public let coverage: Coverage.State

        public init?(_ value: Codexpulse_Core_V1_DashboardSummaryActivityProviderCoverage) {
            guard let provider = AgentProvider(rawValue: value.provider) else { return nil }
            self.provider = provider
            coverage = Coverage.State(rawValue: value.coverageState)
        }
    }

    public struct TrendPoint: Equatable, Sendable, Identifiable {
        public var id: String { key }
        public let key: String
        public let date: Date
        public let tokens: DisplayMetric
        public let estimatedCost: DisplayMetric
        public let shares: [ProviderShare]

        public init(_ value: Codexpulse_Core_V1_DashboardSummaryTrendPoint) {
            key = value.key
            date = Date(timeIntervalSince1970: TimeInterval(value.startAtMs.value) / 1_000)
            tokens = DisplayMetric(value.totalTokens)
            estimatedCost = DisplayMetric(value.estimatedUsdMicros)
            shares = value.shares.compactMap(ProviderShare.init)
        }
    }

    public struct ProviderShare: Equatable, Sendable, Identifiable {
        public var id: String { provider.rawValue }
        public let provider: AgentProvider
        public let tokens: DisplayMetric
        public let estimatedCost: DisplayMetric

        public init?(_ value: Codexpulse_Core_V1_DashboardSummaryProviderShare) {
            guard let provider = AgentProvider(rawValue: value.provider) else { return nil }
            self.provider = provider
            tokens = DisplayMetric(value.totalTokens)
            estimatedCost = DisplayMetric(value.estimatedUsdMicros)
        }
    }

    public struct ModelRow: Equatable, Sendable, Identifiable {
        public var id: String { dimensionKey }
        public let provider: AgentProvider
        public let dimensionKey: String
        public let displayName: String
        public let tokens: DisplayMetric
        public let estimatedCost: DisplayMetric

        public init?(_ value: Codexpulse_Core_V1_DashboardSummaryModelItem) {
            guard let provider = AgentProvider(rawValue: value.provider) else { return nil }
            self.provider = provider
            dimensionKey = value.dimensionKey
            let name = value.model.hasDisplayName
                ? value.model.displayName
                : (value.model.hasID ? value.model.id : "")
            displayName = name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                ? value.dimensionKey : name
            tokens = DisplayMetric(value.totals.totalTokens)
            estimatedCost = DisplayMetric(value.totals.estimatedUsdMicros)
        }
    }

    public struct QuotaCard: Equatable, Sendable, Identifiable {
        public var id: String { provider.rawValue }
        public let provider: AgentProvider
        public let coverage: Coverage.State
        public let windows: [QuotaWindowPresentation]
        public let freshness: String

        public init?(_ value: Codexpulse_Core_V1_DashboardSummaryQuotaCard) {
            guard let provider = AgentProvider(rawValue: value.provider) else { return nil }
            self.provider = provider
            coverage = Coverage.State(rawValue: value.coverageState)
            windows = value.current.windows.map(QuotaWindowPresentation.init)
            freshness = value.current.windows.first?.freshness ?? value.meta.status
        }
    }

    private static func date(_ milliseconds: Int64) -> Date? {
        guard milliseconds > 0 else { return nil }
        return Date(timeIntervalSince1970: TimeInterval(milliseconds) / 1_000)
    }

    public func tokenActivity(provider: AgentProvider?) -> TokenActivityPresentation {
        var synthetic = Codexpulse_Core_V1_UsageCostResponse()
        synthetic.range = response.activityRange
        synthetic.reportingTimeZone = response.activityRange.timeZone
        let state = provider.flatMap { selected in
            activityProviderCoverage.first(where: { $0.provider == selected })?.coverage
        } ?? activityCoverage
        synthetic.meta.status = switch state {
        case .complete, .empty: "complete"
        case .partial, .unknown: "partial"
        case .unavailable: "unavailable"
        }

        var total: Int64 = 0
        var totalIsKnown = true
        synthetic.trend = response.activity.map { point in
            var result = Codexpulse_Core_V1_TrendPoint()
            result.key = point.key
            result.startAtMs = point.startAtMs
            result.endAtMs = point.endAtMs
            let metric: Codexpulse_Core_V1_NumericValue
            if let provider {
                metric = point.shares.first(where: { $0.provider == provider.rawValue })?.totalTokens
                    ?? Codexpulse_Core_V1_NumericValue()
            } else {
                metric = point.totalTokens
            }
            result.totals.totalTokens = metric
            if metric.hasValue && metric.value >= 0 {
                let addition = total.addingReportingOverflow(metric.value)
                if addition.overflow {
                    totalIsKnown = false
                } else {
                    total = addition.partialValue
                }
            } else {
                totalIsKnown = false
            }
            return result
        }
        if totalIsKnown {
            synthetic.totals.totalTokens.value = total
            synthetic.totals.totalTokens.unit = "tokens"
        }
        let now = response.activityRange.endAtMs > response.activityRange.startAtMs
            ? Date(timeIntervalSince1970: Double(response.activityRange.endAtMs - 1) / 1_000)
            : nil
        return TokenActivityPresentation(synthetic, now: now)
    }
}

public enum DashboardTrendMode: String, CaseIterable, Identifiable, Sendable {
    case total
    case stacked

    public var id: String { rawValue }

    public func title(localization: AppLocalization) -> String {
        switch self {
        case .total: localization.textValue("总量")
        case .stacked: localization.textValue("按客户端堆叠")
        }
    }
}
