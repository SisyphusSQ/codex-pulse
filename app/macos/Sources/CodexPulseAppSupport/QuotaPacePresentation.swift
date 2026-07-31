import CodexPulseProtocolGenerated
import Foundation

public struct QuotaPaceChartPoint: Equatable, Identifiable, Sendable {
    public let id: String
    public let elapsedPercent: Double
    public let remainingPercent: Double

    init(series: String, point: Codexpulse_Core_V1_QuotaPacePoint) {
        self.id = "\(series):\(point.observedAtMs)"
        self.elapsedPercent = point.elapsedPercent
        self.remainingPercent = point.remainingPercent
    }
}

public struct QuotaPaceBandPoint: Equatable, Identifiable, Sendable {
    public let id: Double
    public let elapsedPercent: Double
    public let medianRemaining: Double
    public let minimumRemaining: Double
    public let maximumRemaining: Double
    public let cycleCount: Int64

    init(_ point: Codexpulse_Core_V1_QuotaPaceHistoryBandPoint) {
        self.id = point.elapsedPercent
        self.elapsedPercent = point.elapsedPercent
        self.medianRemaining = point.medianRemaining
        self.minimumRemaining = point.minimumRemaining
        self.maximumRemaining = point.maximumRemaining
        self.cycleCount = point.cycleCount
    }
}

public struct QuotaPaceWindowPresentation: Equatable, Identifiable, Sendable {
    public let id: String
    public let windowKind: String
    public let limitID: String
    public let usedPercent: Double?
    public let remainingPercent: Double?
    public let elapsedPercent: Double?
    public let paceDeltaPP: Double?
    public let paceDeltaMetricText: String
    public let paceText: String
    public let forecastText: String
    public let evidenceText: String?
    public let previousComparisonText: String?
    public let historyComparisonText: String?
    public let historyCycleCount: Int64
    public let currentPoints: [QuotaPaceChartPoint]
    public let previousPoints: [QuotaPaceChartPoint]
    public let historyBand: [QuotaPaceBandPoint]
    public let forecastState: String

    public init(
        _ window: Codexpulse_Core_V1_QuotaPaceWindow,
        evaluatedAtMS: Int64
    ) {
        self.id = "\(window.windowKind):\(window.limitID)"
        self.windowKind = window.windowKind
        self.limitID = window.limitID
        self.usedPercent = window.hasUsedPercent ? window.usedPercent : nil
        self.remainingPercent = window.hasRemainingPercent ? window.remainingPercent : nil
        self.elapsedPercent = window.hasElapsedPercent ? window.elapsedPercent : nil
        self.paceDeltaPP = window.hasPaceDeltaPp ? window.paceDeltaPp : nil
        self.paceDeltaMetricText = Self.paceDeltaMetricText(
            window.hasPaceDeltaPp ? window.paceDeltaPp : nil
        )
        self.paceText = Self.paceText(window.hasPaceDeltaPp ? window.paceDeltaPp : nil)
        self.forecastText = Self.forecastText(
            window.forecast,
            usedPercent: window.hasUsedPercent ? window.usedPercent : nil,
            evaluatedAtMS: evaluatedAtMS
        )
        self.evidenceText = Self.evidenceText(window.forecast)
        self.previousComparisonText = Self.comparisonText(
            currentRemaining: window.hasRemainingPercent ? window.remainingPercent : nil,
            comparisonRemaining: window.hasPreviousRemainingAtElapsed
                ? window.previousRemainingAtElapsed : nil,
            target: "上一周期"
        )
        self.historyCycleCount = max(window.historyCycleCount, 0)
        self.historyComparisonText = Self.comparisonText(
            currentRemaining: window.hasRemainingPercent ? window.remainingPercent : nil,
            comparisonRemaining: window.hasHistoryMedianRemainingAtElapsed
                ? window.historyMedianRemainingAtElapsed : nil,
            target: "近 \(max(window.historyCycleCount, 0)) 个周期"
        )
        self.currentPoints = window.currentPoints.map {
            QuotaPaceChartPoint(series: "current", point: $0)
        }
        self.previousPoints = window.hasPreviousCycle
            ? window.previousCycle.points.map {
                QuotaPaceChartPoint(series: "previous", point: $0)
            }
            : []
        self.historyBand = window.historyBand.map(QuotaPaceBandPoint.init)
        self.forecastState = window.forecast.state
    }

    private static func paceText(_ delta: Double?) -> String {
        guard let delta, delta.isFinite else { return "暂时无法比较消耗节奏" }
        let points = Int(abs(delta).rounded())
        if points == 0 { return "用量与周期进度基本一致" }
        return delta > 0
            ? "用量快于周期进度 \(points)%"
            : "用量慢于周期进度 \(points)%"
    }

    private static func paceDeltaMetricText(_ delta: Double?) -> String {
        guard let delta, delta.isFinite else { return "--" }
        let rounded = Int(delta.rounded())
        if rounded == 0 { return "0%" }
        return rounded > 0 ? "+\(rounded)%" : "\(rounded)%"
    }

    private static func forecastText(
        _ forecast: Codexpulse_Core_V1_QuotaPaceForecast,
        usedPercent: Double?,
        evaluatedAtMS: Int64
    ) -> String {
        switch forecast.state {
        case "at_risk":
            guard forecast.hasExhaustAtMs, evaluatedAtMS >= 0,
                  forecast.exhaustAtMs > evaluatedAtMS
            else {
                return "暂无法预测耗尽时间"
            }
            let (remainingMS, overflow) = forecast.exhaustAtMs.subtractingReportingOverflow(
                evaluatedAtMS
            )
            guard !overflow, let duration = approximateForecastDuration(milliseconds: remainingMS)
            else {
                return "暂无法预测耗尽时间"
            }
            return "预计约 \(duration)后耗尽"
        case "on_track":
            return "预计本周期不会耗尽"
        case "exhausted":
            return "额度已用尽"
        default:
            let unknownReason = forecast.hasUnknownReason ? forecast.unknownReason : ""
            switch unknownReason {
            case "evidence_stale": return "数据更新不及时，暂无法预测"
            case "source_conflict": return "额度数据不一致，暂无法预测"
            default: break
            }
            if let usedPercent, usedPercent.isFinite, usedPercent == 0 {
                return "当前暂无消耗，暂不预测"
            }
            switch unknownReason {
            case "evidence_sparse": return "观测不足，暂无法预测"
            case "evidence_flat": return "用量变化不足，暂无法预测"
            default: return "暂无法预测耗尽时间"
            }
        }
    }

    private static func approximateForecastDuration(milliseconds: Int64) -> String? {
        guard milliseconds > 0 else { return nil }

        let minuteMS = 60_000.0
        let hourMS = 60 * minuteMS
        let dayMS = 24 * hourMS
        let duration = Double(milliseconds)

        if duration < hourMS {
            let minutes = max(10, Int((duration / (10 * minuteMS)).rounded()) * 10)
            return minutes >= 60 ? "1 小时" : "\(minutes) 分钟"
        }
        if duration < dayMS {
            let hours = max(1, Int((duration / hourMS).rounded()))
            return hours >= 24 ? "1 天" : "\(hours) 小时"
        }
        let days = max(1, Int((duration / dayMS).rounded()))
        return "\(days) 天"
    }

    private static func evidenceText(
        _ forecast: Codexpulse_Core_V1_QuotaPaceForecast
    ) -> String? {
        guard forecast.evidenceCount > 0, forecast.evidenceSpanMs > 0 else { return nil }
        return "\(forecast.evidenceCount) 个观测 · 跨度 \(ProductCopy.duration(milliseconds: forecast.evidenceSpanMs))"
    }

    private static func comparisonText(
        currentRemaining: Double?,
        comparisonRemaining: Double?,
        target: String
    ) -> String? {
        guard let currentRemaining, let comparisonRemaining,
              currentRemaining.isFinite, comparisonRemaining.isFinite
        else { return nil }
        let difference = currentRemaining - comparisonRemaining
        let points = Int(abs(difference).rounded())
        if points == 0 { return "与\(target)基本一致" }
        return difference > 0
            ? "比\(target)多 \(points)%"
            : "比\(target)少 \(points)%"
    }
}
