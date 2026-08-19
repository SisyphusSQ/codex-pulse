import CodexPulseProtocolGenerated
import Foundation

public struct APISubscriptionActivityDayPresentation: Identifiable, Equatable, Sendable {
    public let id: String
    public let date: Date
    public let intensity: TokenActivityIntensity
    public let deepSeekTotalRecharged: String?
    public let deepSeekTotalConsumed: String?
    public let deepSeekSampleCount: Int?
    public let openCodeGoMaxUsedPercent: Double?
    public let openCodeGoLatestUsedPercent: Double?
    public let openCodeGoLatestRemainingPercent: Double?
    public let openCodeGoSampleCount: Int?
}

public struct APISubscriptionActivityCalendarPresentation: Sendable {
    public let currency: String?
    public let availableCurrencies: [String]
    public let days: [APISubscriptionActivityDayPresentation]
    public let calendar: TokenActivityCalendarPresentation
    public let reportingTimeZone: String

    public init?(
        _ response: Codexpulse_Core_V1_APISubscriptionActivityCalendar,
        currency preferredCurrency: String? = nil,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        guard !response.days.isEmpty,
              let timeZone = TimeZone(identifier: response.reportingTimeZone)
        else { return nil }

        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = timeZone
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.isLenient = false

        let currencies = Set(response.days.flatMap { $0.deepSeek.map(\.currency) })
            .filter { !$0.isEmpty }
            .sorted()
        let selectedCurrency = preferredCurrency.flatMap { currencies.contains($0) ? $0 : nil }
            ?? currencies.first
        let consumedValues: [Int64] = response.days.compactMap { day in
            guard let selectedCurrency,
                  let activity = day.deepSeek.first(where: { $0.currency == selectedCurrency }),
                  activity.sampleCount > 0
            else { return nil }
            return Self.scaledAmount(activity.totalConsumed)
        }
        let thresholds = ActivityIntensityScale.thresholds(for: consumedValues)
        let openCodeGoValues: [Int64] = response.days.compactMap { day in
            guard day.hasOpenCodeGo, day.openCodeGo.sampleCount > 0 else { return nil }
            return Self.scaledPercent(day.openCodeGo.maxFiveHourUsedPercent)
        }
        let openCodeGoThresholds = ActivityIntensityScale.thresholds(for: openCodeGoValues)

        var resolvedDays: [APISubscriptionActivityDayPresentation] = []
        var calendarDays: [TokenActivityCalendarDay] = []
        resolvedDays.reserveCapacity(response.days.count)
        calendarDays.reserveCapacity(response.days.count)
        var previousDate: Date?
        var dateCalendar = Calendar(identifier: .gregorian)
        dateCalendar.timeZone = timeZone
        for value in response.days {
            guard !value.dateKey.isEmpty,
                  value.startsAtMs > 0,
                  let date = formatter.date(from: value.dateKey),
                  formatter.string(from: date) == value.dateKey,
                  formatter.string(from: Date(
                    timeIntervalSince1970: Double(value.startsAtMs) / 1_000
                  )) == value.dateKey,
                  previousDate.map({ dateCalendar.date(byAdding: .day, value: 1, to: $0) == date }) ?? true
            else { return nil }
            previousDate = date

            let deepSeek = selectedCurrency.flatMap { selectedCurrency in
                value.deepSeek.first(where: { $0.currency == selectedCurrency && $0.sampleCount > 0 })
            }
            let deepSeekValue = deepSeek.flatMap { Self.scaledAmount($0.totalConsumed) }
            if deepSeek != nil, deepSeekValue == nil {
                return nil
            }
            let deepSeekIntensity = ActivityIntensityScale.intensity(
                for: deepSeekValue,
                thresholds: thresholds
            )
            let openCodeGo = value.hasOpenCodeGo && value.openCodeGo.sampleCount > 0
                ? value.openCodeGo : nil
            let openCodeGoValue = openCodeGo.flatMap { Self.scaledPercent($0.maxFiveHourUsedPercent) }
            if openCodeGo != nil, openCodeGoValue == nil {
                return nil
            }
            let openCodeGoIntensity = ActivityIntensityScale.intensity(
                for: openCodeGoValue,
                thresholds: openCodeGoThresholds
            )
            let intensity = Self.combinedIntensity(deepSeekIntensity, openCodeGoIntensity)

            resolvedDays.append(APISubscriptionActivityDayPresentation(
                id: value.dateKey,
                date: date,
                intensity: intensity,
                deepSeekTotalRecharged: deepSeek?.totalRecharged,
                deepSeekTotalConsumed: deepSeek?.totalConsumed,
                deepSeekSampleCount: deepSeek.map { Int($0.sampleCount) },
                openCodeGoMaxUsedPercent: openCodeGo?.maxFiveHourUsedPercent,
                openCodeGoLatestUsedPercent: openCodeGo?.latestFiveHourUsedPercent,
                openCodeGoLatestRemainingPercent: openCodeGo?.latestFiveHourRemainingPercent,
                openCodeGoSampleCount: openCodeGo.map { Int($0.sampleCount) }
            ))
            calendarDays.append(TokenActivityCalendarDay(
                day: TokenActivityDay(
                    dateKey: value.dateKey,
                    date: date,
                    tokens: nil,
                    turnCount: nil
                ),
                intensity: intensity
            ))
        }

        currency = selectedCurrency
        availableCurrencies = currencies
        days = resolvedDays
        calendar = TokenActivityCalendarPresentation(
            days: calendarDays,
            reportingTimeZone: response.reportingTimeZone,
            localization: localization
        )
        reportingTimeZone = response.reportingTimeZone
    }

    private static func scaledAmount(_ value: String) -> Int64? {
        guard let amount = Double(value), amount.isFinite, amount >= 0 else { return nil }
        let scaled = amount * 1_000_000
        guard scaled.isFinite, scaled <= Double(Int64.max) else { return nil }
        let rounded = Int64(scaled.rounded())
        return amount > 0 ? max(rounded, 1) : 0
    }

    private static func scaledPercent(_ value: Double) -> Int64? {
        guard value.isFinite, (0...100).contains(value) else { return nil }
        return Int64((value * 10_000).rounded())
    }

    private static func combinedIntensity(
        _ left: TokenActivityIntensity,
        _ right: TokenActivityIntensity
    ) -> TokenActivityIntensity {
        let known = [left, right].filter { $0 != .unknown }
        return known.max(by: { $0.rawValue < $1.rawValue }) ?? .unknown
    }
}
