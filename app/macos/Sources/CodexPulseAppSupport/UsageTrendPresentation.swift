import Foundation

public struct UsageTrendPresentation: Sendable {
    public enum Granularity: String, Sendable {
        case hour
        case day
    }

    public let granularity: Granularity
    public let reportingTimeZone: TimeZone

    public init?(granularity: String, reportingTimeZone: String) {
        guard let granularity = Granularity(rawValue: granularity),
              !reportingTimeZone.isEmpty,
              reportingTimeZone != "Local",
              let timeZone = TimeZone(identifier: reportingTimeZone)
        else {
            return nil
        }
        self.granularity = granularity
        self.reportingTimeZone = timeZone
    }

    public var sectionTitle: String {
        switch granularity {
        case .hour: "每小时趋势"
        case .day: "每日趋势"
        }
    }

    public func axisText(for date: Date) -> String {
        formatted(
            date,
            format: granularity == .hour
                ? (hourNeedsOffset(date) ? "HH:mm XXX" : "HH:mm")
                : "M月d日"
        )
    }

    public func detailText(for date: Date) -> String {
        formatted(
            date,
            format: granularity == .hour
                ? (hourNeedsOffset(date) ? "yyyy年M月d日 HH:mm XXX" : "yyyy年M月d日 HH:mm")
                : "yyyy年M月d日"
        )
    }

    private func hourNeedsOffset(_ date: Date) -> Bool {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = reportingTimeZone
        let displayedComponents = calendar.dateComponents(
            [.era, .year, .month, .day, .hour, .minute],
            from: date
        )
        let offset = reportingTimeZone.secondsFromGMT(for: date)

        return stride(from: -7_200, through: 7_200, by: 900).contains { delta in
            guard delta != 0 else { return false }
            let candidate = date.addingTimeInterval(TimeInterval(delta))
            return reportingTimeZone.secondsFromGMT(for: candidate) != offset
                && calendar.dateComponents(
                    [.era, .year, .month, .day, .hour, .minute],
                    from: candidate
                ) == displayedComponents
        }
    }

    private func formatted(_ date: Date, format: String) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = reportingTimeZone
        formatter.dateFormat = format
        return formatter.string(from: date)
    }
}
