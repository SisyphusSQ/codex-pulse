import CodexPulseProtocolGenerated
import Foundation

public enum InvocationTrendSeries: String, Equatable, Sendable {
    case tool = "Tool"
    case skill = "Skill"
}

public struct InvocationTrendBar: Identifiable, Equatable, Sendable {
    public let id: String
    public let date: Date
    public let count: Int64
    public let series: InvocationTrendSeries

    public init(id: String, date: Date, count: Int64, series: InvocationTrendSeries) {
        self.id = id
        self.date = date
        self.count = count
        self.series = series
    }
}

public enum InvocationTrendBarPresentation {
    public static func bars(
        response: Codexpulse_Core_V1_InvocationUsageResponse,
        includesTools: Bool,
        includesSkills: Bool
    ) -> [InvocationTrendBar] {
        return response.trend.flatMap { point -> [InvocationTrendBar] in
            guard point.startAtMs.hasValue else { return [] }
            let date = Date(timeIntervalSince1970: TimeInterval(point.startAtMs.value) / 1_000)
            let bucketID = "\(point.key):\(point.startAtMs.value)"
            var bars: [InvocationTrendBar] = []
            if includesTools, point.toolCallCount.hasValue {
                bars.append(InvocationTrendBar(
                    id: "\(bucketID):tool",
                    date: date,
                    count: point.toolCallCount.value,
                    series: .tool
                ))
            }
            if includesSkills, point.skillActivityCount.hasValue {
                bars.append(InvocationTrendBar(
                    id: "\(bucketID):skill",
                    date: date,
                    count: point.skillActivityCount.value,
                    series: .skill
                ))
            }
            return bars
        }
    }

    public static func barWidth(for preset: DateRangePreset) -> CGFloat {
        switch preset {
        case .quotaWeek, .sevenDays:
            28
        case .today, .quotaMonth, .thirtyDays:
            7
        default:
            12
        }
    }
}
