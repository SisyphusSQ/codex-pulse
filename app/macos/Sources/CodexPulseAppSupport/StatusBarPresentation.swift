import Foundation

public enum QuotaRemainingLevel: Equatable, Sendable {
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
}

public enum StatusBarStyle: String, CaseIterable, Identifiable, Sendable {
    case ringSummary = "ring_summary"
    case openRingSummary = "open_ring_summary"
    case gaugeSummary = "gauge_summary"

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .ringSummary: "A · 基准圆环"
        case .openRingSummary: "B · 缺口圆环"
        case .gaugeSummary: "D · 仪表弧"
        }
    }

    public static func resolve(storedValue: String?) -> Self {
        if let storedValue, let style = Self(rawValue: storedValue) { return style }
        return .ringSummary
    }
}
