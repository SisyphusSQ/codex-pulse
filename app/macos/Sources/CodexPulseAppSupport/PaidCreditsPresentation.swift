import CodexPulseProtocolGenerated
import Foundation

public enum PaidCreditsPresentation {
    public static func amount(_ value: String?, unlimited: Bool = false, localization: AppLocalization) -> String {
        if unlimited { return localization.textValue("无限额") }
        guard let value, value.range(of: #"^(0|[1-9][0-9]*)(\.[0-9]+)?$"#, options: .regularExpression) != nil else { return "--" }
        let parts = value.split(separator: ".", omittingEmptySubsequences: false)
        let formatter = NumberFormatter()
        formatter.locale = localization.locale
        let separator = formatter.groupingSeparator ?? ","
        let integer = String(parts[0])
        var groups: [String] = []
        var end = integer.endIndex
        while end > integer.startIndex {
            let start = integer.index(end, offsetBy: -3, limitedBy: integer.startIndex) ?? integer.startIndex
            groups.append(String(integer[start..<end]))
            end = start
        }
        let fraction = parts.count > 1 ? (formatter.decimalSeparator ?? ".") + parts[1] : ""
        return groups.reversed().joined(separator: separator) + fraction
    }

    public static func balance(_ credit: Codexpulse_Core_V1_PaidCredits, localization: AppLocalization) -> String {
        amount(credit.hasBalance ? credit.balance : nil, unlimited: credit.hasUnlimited && credit.unlimited, localization: localization)
    }
}
