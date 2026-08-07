import CodexPulseProtocolGenerated
import Foundation

public struct UsageTrendChartPresentation: Sendable {
    public enum Granularity: Sendable {
        case hour
        case day
    }

    public let granularity: Granularity
    public let reportingTimeZone: TimeZone
    public let domain: ClosedRange<Date>
    private let firstBucketStart: Date
    private let finalBucketStart: Date

    public init?(preset: DateRangePreset, range: Codexpulse_Core_V1_UTCTimeRange) {
        guard range.startAtMs >= 0,
              range.endAtMs > range.startAtMs,
              !range.timeZone.isEmpty,
              range.timeZone != "Local",
              let reportingTimeZone = TimeZone(identifier: range.timeZone)
        else { return nil }

        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = reportingTimeZone
        let start = Date(timeIntervalSince1970: TimeInterval(range.startAtMs) / 1_000)
        let endExclusive = Date(timeIntervalSince1970: TimeInterval(range.endAtMs) / 1_000)
        let dayStart = calendar.startOfDay(for: start)
        let finalDay = preset == .today
            ? dayStart
            : calendar.startOfDay(for: endExclusive.addingTimeInterval(-0.001))
        granularity = preset == .today ? .hour : .day
        self.reportingTimeZone = reportingTimeZone
        firstBucketStart = dayStart
        finalBucketStart = preset == .today
            ? calendar.date(byAdding: .hour, value: 23, to: dayStart) ?? dayStart
            : finalDay

        switch granularity {
        case .hour:
            guard let domainStart = calendar.date(byAdding: .minute, value: -30, to: dayStart),
                  let domainEnd = calendar.date(byAdding: .minute, value: 30, to: finalBucketStart)
            else { return nil }
            domain = domainStart...domainEnd
        case .day:
            guard let domainStart = calendar.date(byAdding: .hour, value: -12, to: dayStart),
                  let domainEnd = calendar.date(byAdding: .hour, value: 12, to: finalDay)
            else { return nil }
            domain = domainStart...domainEnd
        }
    }

    public var sectionTitle: String {
        AppLocalizationRegistry.shared.current.textValue(
            granularity == .hour ? "每小时趋势" : "每日趋势"
        )
    }

    public var rangeLabel: String {
        let localization = AppLocalizationRegistry.shared.current
        let format: String = if localization.language == .englishUS {
            granularity == .hour ? "MMM d HH:mm" : "MMM d"
        } else {
            granularity == .hour ? "M月d日 HH:mm" : "M月d日"
        }
        let startLabel = localization.format("自 %@", formatted(firstBucketStart, format: format))
        let granularityLabel = localization.textValue(
            granularity == .hour ? "按小时" : "按天"
        )
        return localization.format("range.label.daily", startLabel, granularityLabel)
    }

    public var axisTicks: [Date] {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = reportingTimeZone
        switch granularity {
        case .hour:
            return stride(from: 0, through: 20, by: 4).compactMap {
                calendar.date(byAdding: .hour, value: $0, to: firstBucketStart)
            }
        case .day:
            let dayCount = max(
                0,
                calendar.dateComponents([.day], from: firstBucketStart, to: finalBucketStart).day ?? 0
            )
            let step = max(1, Int(ceil(Double(dayCount) / 6)))
            var ticks = stride(from: 0, through: dayCount, by: step).compactMap {
                calendar.date(byAdding: .day, value: $0, to: firstBucketStart)
            }
            if ticks.last != finalBucketStart { ticks.append(finalBucketStart) }
            return ticks
        }
    }

    public func axisText(for date: Date) -> String {
        formatted(date, format: granularity == .hour ? "HH:mm" : "M/d")
    }

    public func detailText(for date: Date) -> String {
        formatted(date, format: granularity == .hour ? "yyyy年M月d日 HH:mm" : "yyyy年M月d日")
    }

    private func formatted(_ date: Date, format: String) -> String {
        let localization = AppLocalizationRegistry.shared.current
        let formatter = DateFormatter()
        formatter.locale = localization.locale
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = reportingTimeZone
        formatter.dateFormat = localization.language == .englishUS
            ? format.replacingOccurrences(of: "yyyy年M月d日", with: "MMM d, yyyy")
            : format
        return formatter.string(from: date)
    }
}
