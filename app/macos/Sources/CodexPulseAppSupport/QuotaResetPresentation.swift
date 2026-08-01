import Foundation

public struct QuotaResetPresentation: Equatable, Sendable {
    public static let unavailableText = "--"

    public static var pendingText: String {
        AppLocalizationRegistry.shared.current.textValue("重置时间待定")
    }

    public let remainingText: String
    public let resetTimeText: String
    public let compactText: String

    public init(
        resetsAtMS: Int64?,
        resetRemainingMS: Int64?,
        referenceDate: Date = .now,
        timeZone: TimeZone = .current
    ) {
        let localization = AppLocalizationRegistry.shared.current
        let pendingText = localization.textValue("重置时间待定")
        guard let resetRemainingMS, resetRemainingMS > 0 else {
            remainingText = Self.unavailableText
            resetTimeText = Self.unavailableText
            compactText = pendingText
            return
        }

        let remainingText = ProductCopy.duration(
            milliseconds: resetRemainingMS,
            localization: localization
        )
        guard let resetsAtMS else {
            self.remainingText = remainingText
            resetTimeText = Self.unavailableText
            compactText = localization.format(
                "quota.reset.compact", remainingText, Self.unavailableText
            )
            return
        }

        let resetDate = Date(timeIntervalSince1970: TimeInterval(resetsAtMS) / 1_000)
        guard resetsAtMS > 0, resetDate > referenceDate, resetDate <= .distantFuture else {
            self.remainingText = Self.unavailableText
            resetTimeText = Self.unavailableText
            compactText = pendingText
            return
        }

        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone

        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.locale = localization.locale
        formatter.timeZone = timeZone
        let sameYear = calendar.component(.year, from: resetDate)
            == calendar.component(.year, from: referenceDate)
        formatter.dateFormat = if localization.language == .englishUS {
            sameYear ? "MMM d HH:mm" : "yyyy MMM d HH:mm"
        } else {
            sameYear ? "M月d日 HH:mm" : "yyyy年M月d日 HH:mm"
        }

        let resetTimeText = formatter.string(from: resetDate)
        guard !resetTimeText.isEmpty else {
            self.remainingText = Self.unavailableText
            self.resetTimeText = Self.unavailableText
            compactText = pendingText
            return
        }

        self.remainingText = remainingText
        self.resetTimeText = resetTimeText
        compactText = localization.format("quota.reset.compact", remainingText, resetTimeText)
    }
}
