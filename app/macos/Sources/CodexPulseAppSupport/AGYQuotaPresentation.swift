import CodexPulseProtocolGenerated
import Foundation

public struct AGYQuotaPoolGroup: Equatable, Identifiable, Sendable {
    public let id: String
    public let limitID: String
    public let displayName: String
    public let compactName: String
    public let primaryWindow: QuotaWindowPresentation?
    public let secondaryWindow: QuotaWindowPresentation?

    public var windows: [QuotaWindowPresentation] {
        [primaryWindow, secondaryWindow].compactMap { $0 }
    }

    public var isStale: Bool {
        windows.contains { $0.freshness == "stale" || $0.freshness == "expired_unknown" }
    }

    public var lastSuccessAtMS: Int64? {
        windows.compactMap(\.lastSuccessAtMS).max()
    }
}

public struct AGYQuotaPacePoolGroup: Equatable, Identifiable, Sendable {
    public let id: String
    public let limitID: String
    public let displayName: String
    public let windows: [QuotaPaceWindowPresentation]
}

public enum AGYQuotaPoolResolver {
    public static let geminiLimitID = "agy.gemini"
    public static let argonLimitID = "agy.argon"
    public static let carbonLimitID = "agy.carbon"
    public static let claudeGPTLimitID = "agy.claude_gpt"

    public static func poolOrderRank(_ limitID: String) -> Int {
        switch limitID {
        case geminiLimitID: 0
        case argonLimitID: 1
        case carbonLimitID: 2
        case claudeGPTLimitID: 3
        default: 4
        }
    }

    public static func isPrimaryWindow(
        windowKind: String,
        windowMinutes: Int64?
    ) -> Bool {
        if windowKind == "primary" { return true }
        if windowKind == "secondary" { return false }
        if let windowMinutes, windowMinutes > 0, windowMinutes <= 360 { return true }
        return false
    }

    public static func displayPoolName(limitID: String, limitName: String?) -> String {
        if let trimmed = limitName?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty {
            return trimmed
        }
        switch limitID {
        case geminiLimitID: return "Gemini"
        case argonLimitID: return "Gemini 4 Argon"
        case carbonLimitID: return "Gemini 4 Carbon"
        case claudeGPTLimitID: return "Claude & GPT"
        default:
            if limitID.hasPrefix("agy.pool.") {
                let slug = String(limitID.dropFirst("agy.pool.".count))
                    .replacingOccurrences(of: "_", with: " ")
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                if !slug.isEmpty { return slug.capitalized }
            }
            return limitID
        }
    }

    public static func compactPoolName(limitID: String, limitName: String?) -> String {
        switch limitID {
        case geminiLimitID: return "Gemini"
        case argonLimitID: return "Argon"
        case carbonLimitID: return "Carbon"
        case claudeGPTLimitID: return "Claude"
        default:
            return displayPoolName(limitID: limitID, limitName: limitName)
        }
    }

    public static func compactWindowTag(
        windowKind: String,
        windowMinutes: Int64?,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String {
        if isPrimaryWindow(windowKind: windowKind, windowMinutes: windowMinutes) {
            return "5h"
        }
        return localization.textValue("周")
    }

    public static func compactMenuBarLabel(
        for window: QuotaWindowPresentation,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String {
        let pool = compactPoolName(limitID: window.limitID, limitName: window.limitName)
        let tag = compactWindowTag(
            windowKind: window.windowKind,
            windowMinutes: window.windowMinutes,
            localization: localization
        )
        return "\(pool) · \(tag)"
    }

    public static func isValidNonExpired(
        _ window: QuotaWindowPresentation,
        evaluatedAtMS: Int64
    ) -> Bool {
        guard let remaining = window.remainingPercent, remaining.isFinite else {
            return false
        }
        guard ["fresh", "stale", "suspicious"].contains(window.freshness) else {
            return false
        }
        if let resetsAtMS = window.resetsAtMS, evaluatedAtMS > 0, evaluatedAtMS >= resetsAtMS {
            return false
        }
        if let resetRemainingMS = window.resetRemainingMS, resetRemainingMS <= 0 {
            return false
        }
        return true
    }

    public static func tightestNonExpiredWindow(
        from windows: [QuotaWindowPresentation],
        evaluatedAtMS: Int64
    ) -> QuotaWindowPresentation? {
        let candidates = windows.filter { isValidNonExpired($0, evaluatedAtMS: evaluatedAtMS) }
        return candidates.min { lhs, rhs in
            let lhsRemaining = lhs.remainingPercent ?? .infinity
            let rhsRemaining = rhs.remainingPercent ?? .infinity
            if lhsRemaining != rhsRemaining {
                return lhsRemaining < rhsRemaining
            }
            let lhsPrimary = isPrimaryWindow(windowKind: lhs.windowKind, windowMinutes: lhs.windowMinutes)
            let rhsPrimary = isPrimaryWindow(windowKind: rhs.windowKind, windowMinutes: rhs.windowMinutes)
            if lhsPrimary != rhsPrimary {
                return lhsPrimary && !rhsPrimary
            }
            let lhsRank = poolOrderRank(lhs.limitID)
            let rhsRank = poolOrderRank(rhs.limitID)
            if lhsRank != rhsRank {
                return lhsRank < rhsRank
            }
            if lhs.limitID != rhs.limitID {
                return lhs.limitID < rhs.limitID
            }
            return lhs.id < rhs.id
        }
    }

    public static func groupPools(
        _ windows: [QuotaWindowPresentation]
    ) -> [AGYQuotaPoolGroup] {
        var grouped: [String: (name: String?, primary: QuotaWindowPresentation?, secondary: QuotaWindowPresentation?)] = [:]
        for window in windows {
            var entry = grouped[window.limitID] ?? (window.limitName, nil, nil)
            if entry.name == nil, let limitName = window.limitName, !limitName.isEmpty {
                entry.name = limitName
            }
            if isPrimaryWindow(windowKind: window.windowKind, windowMinutes: window.windowMinutes) {
                if entry.primary == nil { entry.primary = window }
            } else {
                if entry.secondary == nil { entry.secondary = window }
            }
            grouped[window.limitID] = entry
        }
        let sortedIDs = grouped.keys.sorted { lhs, rhs in
            let lhsRank = poolOrderRank(lhs)
            let rhsRank = poolOrderRank(rhs)
            if lhsRank != rhsRank { return lhsRank < rhsRank }
            return lhs < rhs
        }
        return sortedIDs.compactMap { limitID in
            guard let entry = grouped[limitID] else { return nil }
            return AGYQuotaPoolGroup(
                id: limitID,
                limitID: limitID,
                displayName: displayPoolName(limitID: limitID, limitName: entry.name),
                compactName: compactPoolName(limitID: limitID, limitName: entry.name),
                primaryWindow: entry.primary,
                secondaryWindow: entry.secondary
            )
        }
    }

    public static func groupPacePools(
        _ windows: [QuotaPaceWindowPresentation],
        currentWindows: [Codexpulse_Core_V1_CurrentWindow]
    ) -> [AGYQuotaPacePoolGroup] {
        let nameByLimitID = Dictionary(
            currentWindows.compactMap { window -> (String, String)? in
                guard window.hasLimitName, !window.limitName.isEmpty else { return nil }
                return (window.limitID, window.limitName)
            },
            uniquingKeysWith: { first, _ in first }
        )
        var byLimitID: [String: [QuotaPaceWindowPresentation]] = [:]
        for window in windows {
            byLimitID[window.limitID, default: []].append(window)
        }
        let sortedIDs = byLimitID.keys.sorted { lhs, rhs in
            let lhsRank = poolOrderRank(lhs)
            let rhsRank = poolOrderRank(rhs)
            if lhsRank != rhsRank { return lhsRank < rhsRank }
            return lhs < rhs
        }
        return sortedIDs.map { limitID in
            let sortedWindows = (byLimitID[limitID] ?? []).sorted { lhs, rhs in
                let lhsPrimary = isPrimaryWindow(windowKind: lhs.windowKind, windowMinutes: nil)
                let rhsPrimary = isPrimaryWindow(windowKind: rhs.windowKind, windowMinutes: nil)
                if lhsPrimary != rhsPrimary { return lhsPrimary && !rhsPrimary }
                return lhs.id < rhs.id
            }
            return AGYQuotaPacePoolGroup(
                id: limitID,
                limitID: limitID,
                displayName: displayPoolName(limitID: limitID, limitName: nameByLimitID[limitID]),
                windows: sortedWindows
            )
        }
    }
}

public enum SessionRolePresentation: String, Equatable, Sendable {
    case subagent
    case swarmWorker = "swarm_worker"
    case sideQuestion = "side_question"

    public init?(titleReason: String) {
        let trimmed = titleReason.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.hasPrefix("role:") else { return nil }
        let rawRole = String(trimmed.dropFirst("role:".count))
        self.init(rawValue: rawRole)
    }

    public func badgeTitle(
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String {
        switch self {
        case .subagent: localization.textValue("子代理")
        case .swarmWorker: "Swarm"
        case .sideQuestion: localization.textValue("旁白提问")
        }
    }
}

public enum PopoverAccountEmailMasker {
    public static func mask(_ email: String) -> String {
        let trimmed = email.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != "--" else { return trimmed }
        guard let atIndex = trimmed.firstIndex(of: "@") else {
            return String(trimmed.prefix(1)) + "***"
        }
        let local = trimmed[..<atIndex]
        let domain = trimmed[atIndex...]
        guard !local.isEmpty else { return "***\(domain)" }
        let visibleCount = local.count <= 2 ? 1 : 2
        return String(local.prefix(visibleCount)) + "***" + domain
    }
}
