import Foundation

public enum QuotaLevel: Equatable, Sendable {
    case healthy
    case warning
    case critical
    case unavailable

    public init(remainingPercent: Double?) {
        guard let remainingPercent, remainingPercent.isFinite else {
            self = .unavailable
            return
        }
        if remainingPercent <= 20 {
            self = .critical
        } else if remainingPercent <= 40 {
            self = .warning
        } else {
            self = .healthy
        }
    }

    public init(usedPercent: Double?) {
        guard let usedPercent, usedPercent.isFinite else {
            self = .unavailable
            return
        }
        self.init(remainingPercent: 100 - usedPercent)
    }

    public func accessibilityStatus(
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String {
        let status = switch self {
        case .healthy: "healthy"
        case .warning: "warning"
        case .critical: "critical"
        case .unavailable: "unavailable"
        }
        return ProductCopy.status(status, localization: localization)
    }
}

public struct QuotaProgressPresentation: Equatable, Sendable {
    public let fraction: Double
    public let level: QuotaLevel
    public let percentText: String
    public let accessibilityValue: String

    public init(
        usedPercent: Double?,
        levelOverride: QuotaLevel? = nil,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        self.init(
            percent: usedPercent,
            level: levelOverride ?? QuotaLevel(usedPercent: usedPercent),
            localization: localization
        )
    }

    public init(
        remainingPercent: Double?,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        self.init(
            percent: remainingPercent,
            level: QuotaLevel(remainingPercent: remainingPercent),
            localization: localization
        )
    }

    private init(percent: Double?, level: QuotaLevel, localization: AppLocalization) {
        if let percent, percent.isFinite {
            fraction = min(max(percent / 100, 0), 1)
            percentText = localization.percent(percent)
        } else {
            fraction = 0
            percentText = "--"
        }
        self.level = level
        accessibilityValue = "\(percentText) · \(level.accessibilityStatus(localization: localization))"
    }
}
