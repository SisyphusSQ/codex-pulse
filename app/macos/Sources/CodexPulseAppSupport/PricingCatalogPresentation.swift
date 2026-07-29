import CodexPulseProtocolGenerated
import Foundation

public enum ReferencePriceFormatter {
    private static let hiddenModelFamilies = ["gpt-5", "gpt-5.1", "gpt-5.2"]
    private static let hiddenModelIDs = ["gpt-5.6"]

    public static func shouldDisplay(modelID: String) -> Bool {
        let normalized = modelID.lowercased()
        return !hiddenModelIDs.contains(normalized) && !hiddenModelFamilies.contains {
            normalized == $0 || normalized.hasPrefix("\($0)-")
        }
    }

    public static func rate(
        _ value: Codexpulse_Core_V1_NumericValue,
        currency: String
    ) -> String {
        guard value.hasValue, value.value >= 0, value.unit == "micro_usd" else { return "暂无" }
        let precision = value.value.isMultiple(of: 10_000) ? 2 : 3
        let amount = Double(value.value) / 1_000_000
        let number = String(format: "%.\(precision)f", locale: Locale(identifier: "en_US_POSIX"), amount)
        return currency == "USD" ? "$\(number)" : "\(currency) \(number)"
    }

    public static func verifiedDate(
        _ value: Codexpulse_Core_V1_NumericValue
    ) -> String? {
        guard value.hasValue, value.value >= 0, value.unit == "milliseconds" else { return nil }
        return Date(timeIntervalSince1970: TimeInterval(value.value) / 1_000)
            .formatted(date: .abbreviated, time: .omitted)
    }

    public static func sourceURL(
        _ response: Codexpulse_Core_V1_PricingCatalogCurrentResponse
    ) -> URL? {
        guard response.hasSourceURL,
              let url = URL(string: response.sourceURL),
              url.scheme == "https",
              url.host == "developers.openai.com",
              url.user == nil,
              url.password == nil
        else {
            return nil
        }
        return url
    }
}
