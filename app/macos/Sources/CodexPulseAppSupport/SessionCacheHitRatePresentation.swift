import CodexPulseProtocolGenerated

public struct SessionCacheHitRatePresentation: Equatable, Sendable {
    public let rateText: String

    public init(
        _ rate: Codexpulse_Core_V1_NumericValue?,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        guard let rate, rate.hasValue, !rate.hasUnknownReason,
              rate.unit == "basis_points", (0...10_000).contains(rate.value) else {
            rateText = "--"
            return
        }
        rateText = localization.percent(Double(rate.value) / 100, fractionDigits: 1)
    }
}
