import CodexPulseProtocolGenerated
import Foundation

public struct ProviderRefreshPresentation: Equatable, Sendable {
    public let trigger: String
    public let refreshedCount: Int
    public let skippedCount: Int
    public let failedCount: Int
    public let providers: [Codexpulse_Core_V1_ProviderRefreshResult]

    public init(_ receipt: Codexpulse_Core_V1_ProviderRefreshReceipt) {
        trigger = receipt.trigger
        providers = receipt.providers
        var refreshed = 0
        var skipped = 0
        var failed = 0
        for provider in receipt.providers {
            switch provider.status {
            case "refreshed":
                refreshed += 1
            case "failed":
                failed += 1
            default:
                skipped += 1
            }
        }
        refreshedCount = refreshed
        skippedCount = skipped
        failedCount = failed
    }

    public var isPartialFailure: Bool {
        failedCount > 0 && failedCount < providers.count
    }

    public func summary(localization: AppLocalization) -> String {
        if failedCount > 0 {
            return localization.format(
                "app.global_refresh.partial",
                refreshedCount,
                skippedCount,
                failedCount
            )
        }
        return localization.format(
            "app.global_refresh.summary",
            refreshedCount,
            skippedCount
        )
    }
}
