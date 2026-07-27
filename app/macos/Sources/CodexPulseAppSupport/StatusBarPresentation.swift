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

public enum StatusBarQuotaDataState: Equatable, Sendable {
    case fresh
    case stale
    case suspicious
    case unavailable

    public init(freshness: String) {
        switch freshness {
        case "fresh": self = .fresh
        case "stale": self = .stale
        case "suspicious": self = .suspicious
        default: self = .unavailable
        }
    }

    public var preservesRemainingColor: Bool {
        self != .unavailable
    }

    public var accessibilitySuffix: String {
        switch self {
        case .fresh: ""
        case .stale: "，显示上次可信额度"
        case .suspicious: "，新额度数据异常，显示上次可信额度"
        case .unavailable: "，额度数据可信度不可用"
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
        case .ringSummary: "基准圆环"
        case .openRingSummary: "缺口圆环"
        case .gaugeSummary: "仪表弧"
        }
    }

    public static func resolve(storedValue: String?) -> Self {
        if let storedValue, let style = Self(rawValue: storedValue) { return style }
        return .ringSummary
    }
}
