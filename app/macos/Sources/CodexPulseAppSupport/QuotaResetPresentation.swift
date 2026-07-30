import Foundation

public struct QuotaResetPresentation: Equatable, Sendable {
    public static let unavailableText = "--"
    public static let pendingText = "重置时间待定"

    public let remainingText: String
    public let resetTimeText: String
    public let compactText: String

    public init(
        resetsAtMS: Int64?,
        resetRemainingMS: Int64?,
        referenceDate: Date = .now,
        timeZone: TimeZone = .current
    ) {
        guard let resetRemainingMS, resetRemainingMS > 0 else {
            remainingText = Self.unavailableText
            resetTimeText = Self.unavailableText
            compactText = Self.pendingText
            return
        }

        let remainingText = ProductCopy.duration(milliseconds: resetRemainingMS)
        guard let resetsAtMS else {
            self.remainingText = remainingText
            resetTimeText = Self.unavailableText
            compactText = "\(remainingText)后 · \(Self.unavailableText)"
            return
        }

        let resetDate = Date(timeIntervalSince1970: TimeInterval(resetsAtMS) / 1_000)
        guard resetsAtMS > 0, resetDate > referenceDate, resetDate <= .distantFuture else {
            self.remainingText = Self.unavailableText
            resetTimeText = Self.unavailableText
            compactText = Self.pendingText
            return
        }

        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone

        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.timeZone = timeZone
        formatter.dateFormat =
            calendar.component(.year, from: resetDate)
                == calendar.component(.year, from: referenceDate)
            ? "M月d日 HH:mm" : "yyyy年M月d日 HH:mm"

        let resetTimeText = formatter.string(from: resetDate)
        guard !resetTimeText.isEmpty else {
            self.remainingText = Self.unavailableText
            self.resetTimeText = Self.unavailableText
            compactText = Self.pendingText
            return
        }

        self.remainingText = remainingText
        self.resetTimeText = resetTimeText
        compactText = "\(remainingText)后 · \(resetTimeText)"
    }
}
