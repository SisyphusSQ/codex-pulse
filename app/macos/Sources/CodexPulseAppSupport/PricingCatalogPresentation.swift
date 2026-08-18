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
        let localization = AppLocalizationRegistry.shared.current
        guard value.hasValue, value.value >= 0, value.unit == "micro_usd" else {
            return localization.textValue("暂无")
        }
        let precision = value.value.isMultiple(of: 10_000) ? 2 : 3
        let amount = Double(value.value) / 1_000_000
        let number = String(format: "%.\(precision)f", locale: localization.locale, amount)
        return currency == "USD" ? "$\(number)" : "\(currency) \(number)"
    }

    public static func verifiedDate(
        _ value: Codexpulse_Core_V1_NumericValue
    ) -> String? {
        guard value.hasValue, value.value >= 0, value.unit == "milliseconds" else { return nil }
        let formatter = DateFormatter()
        formatter.locale = AppLocalizationRegistry.shared.current.locale
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        return formatter.string(from: Date(timeIntervalSince1970: TimeInterval(value.value) / 1_000))
    }

    public static func sourceURL(
        _ response: Codexpulse_Core_V1_PricingCatalogCurrentResponse
    ) -> URL? {
        guard response.hasSourceURL,
              let url = URL(string: response.sourceURL),
              url.scheme == "https",
			  let host = url.host,
			  ["developers.openai.com", "cursor.com", "docs.x.ai"].contains(host),
              url.user == nil,
              url.password == nil
        else {
            return nil
        }
        return url
    }
}
