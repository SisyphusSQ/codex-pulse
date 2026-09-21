import CodexPulseProtocolGenerated
import Foundation

public enum CodexAccountQuotaWindowDisplayResolver {
    private struct WindowKey: Hashable {
        let limitID: String
        let windowMinutes: Int64?
    }

    public static func displayWindows(
        _ windows: [Codexpulse_Core_V1_CodexAccountQuotaWindow]
    ) -> [Codexpulse_Core_V1_CodexAccountQuotaWindow] {
        var resolved: [Codexpulse_Core_V1_CodexAccountQuotaWindow] = []
        var indexByKey: [WindowKey: Int] = [:]

        for window in windows {
            let key = WindowKey(
                limitID: window.limitID,
                windowMinutes: window.hasWindowMinutes ? window.windowMinutes : nil
            )
            if let index = indexByKey[key] {
                if shouldPrefer(window, over: resolved[index]) {
                    resolved[index] = window
                }
            } else {
                indexByKey[key] = resolved.count
                resolved.append(window)
            }
        }

        return resolved.sorted(by: displayOrder)
    }

    private static func shouldPrefer(
        _ candidate: Codexpulse_Core_V1_CodexAccountQuotaWindow,
        over current: Codexpulse_Core_V1_CodexAccountQuotaWindow
    ) -> Bool {
        let candidateScore = preferenceScore(candidate)
        let currentScore = preferenceScore(current)
        if candidateScore != currentScore { return candidateScore > currentScore }
        let candidateCollected = candidate.hasLastCollectedAtMs ? candidate.lastCollectedAtMs : -1
        let currentCollected = current.hasLastCollectedAtMs ? current.lastCollectedAtMs : -1
        if candidateCollected != currentCollected { return candidateCollected > currentCollected }
        return candidate.windowKind == "primary" && current.windowKind != "primary"
    }

    private static func preferenceScore(
        _ window: Codexpulse_Core_V1_CodexAccountQuotaWindow
    ) -> Int {
        let valueScore = window.hasRemainingPercent ? 1_000 : 0
        let freshnessScore: Int = switch window.freshness {
        case "fresh": 400
        case "stale": 300
        case "suspicious": 200
        case "expired_unknown": 100
        default: 0
        }
        let conflictScore = window.conflict == "conflict" ? 0 : 20
        let resetScore = window.hasResetsAtMs && window.resetsAtMs > 0 ? 10 : 0
        return valueScore + freshnessScore + conflictScore + resetScore
    }

    private static func displayOrder(
        _ left: Codexpulse_Core_V1_CodexAccountQuotaWindow,
        _ right: Codexpulse_Core_V1_CodexAccountQuotaWindow
    ) -> Bool {
        let leftGeneralRank = left.limitID == "codex" ? 0 : 1
        let rightGeneralRank = right.limitID == "codex" ? 0 : 1
        if leftGeneralRank != rightGeneralRank { return leftGeneralRank < rightGeneralRank }
        let leftDuration = left.hasWindowMinutes ? left.windowMinutes : .max
        let rightDuration = right.hasWindowMinutes ? right.windowMinutes : .max
        if leftDuration != rightDuration { return leftDuration < rightDuration }
        if left.limitID != right.limitID { return left.limitID < right.limitID }
        return left.windowKind < right.windowKind
    }
}

public struct CodexAccountQuotaWindowCardPresentation: Equatable, Sendable, Identifiable {
    public let id: String
    public let title: String
    public let metricTitle: String
    public let remainingPercent: Double?
    public let resetRemainingText: String?
    public let resetTimeText: String
    public let dataStatusText: String
    public let lastCollectedText: String?
    public let notice: String?

    public init(
        _ window: Codexpulse_Core_V1_CodexAccountQuotaWindow,
        current: Bool,
        evaluatedAtMS: Int64,
        timeZoneIdentifier: String,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        let quota = QuotaWindowPresentation(window)
        let timeZone = TimeZone(identifier: timeZoneIdentifier) ?? .current
        let remaining = window.hasRemainingPercent ? window.remainingPercent : nil
        id = "\(quota.id):\(window.hasWindowMinutes ? String(window.windowMinutes) : "unknown")"
        title = quota.title
        remainingPercent = remaining
        metricTitle = Self.metricTitle(
            current: current,
            freshness: window.freshness,
            localization: localization
        )
        resetRemainingText = Self.resetRemainingText(
            window: window,
            current: current,
            evaluatedAtMS: evaluatedAtMS,
            localization: localization
        )
        resetTimeText = Self.resetTimeText(
            window: window,
            evaluatedAtMS: evaluatedAtMS,
            timeZone: timeZone,
            localization: localization
        )
        dataStatusText = Self.dataStatusText(
            freshness: window.freshness,
            conflict: window.conflict,
            current: current,
            localization: localization
        )
        lastCollectedText = window.hasLastCollectedAtMs
            ? Self.timestampText(
                window.lastCollectedAtMs,
                timeZone: timeZone,
                localization: localization
            ) : nil
        notice = window.conflict == "conflict"
            ? localization.textValue("不同来源存在冲突，当前显示经仲裁后的可信值。") : nil
    }

    private static func metricTitle(
        current: Bool,
        freshness: String,
        localization: AppLocalization
    ) -> String {
        guard current else { return localization.textValue("最后记录剩余") }
        return switch freshness {
        case "fresh": localization.textValue("剩余")
        case "stale": localization.textValue("上次可信剩余")
        default: localization.textValue("最后记录剩余")
        }
    }

    private static func resetRemainingText(
        window: Codexpulse_Core_V1_CodexAccountQuotaWindow,
        current: Bool,
        evaluatedAtMS: Int64,
        localization: AppLocalization
    ) -> String? {
        guard current, window.hasResetsAtMs, window.resetsAtMs > evaluatedAtMS else { return nil }
        return ProductCopy.duration(
            milliseconds: window.resetsAtMs - evaluatedAtMS,
            localization: localization
        )
    }

    private static func resetTimeText(
        window: Codexpulse_Core_V1_CodexAccountQuotaWindow,
        evaluatedAtMS: Int64,
        timeZone: TimeZone,
        localization: AppLocalization
    ) -> String {
        guard window.hasResetsAtMs, window.resetsAtMs > 0 else { return "--" }
        let timestamp = timestampText(
            window.resetsAtMs,
            timeZone: timeZone,
            localization: localization
        )
        guard window.resetsAtMs > evaluatedAtMS else {
            return localization.format("已结束 · %@", timestamp)
        }
        return timestamp
    }

    private static func dataStatusText(
        freshness: String,
        conflict: String,
        current: Bool,
        localization: AppLocalization
    ) -> String {
        var status = switch freshness {
        case "fresh": current
            ? localization.textValue("最新数据")
            : localization.textValue("采集时有效")
        case "stale": localization.textValue("上次可信记录（更新延迟）")
        case "expired_unknown": localization.textValue("周期已结束，当前额度未知")
        case "suspicious": localization.textValue("数据需核对")
        case "never_loaded": localization.textValue("尚未加载")
        default: ProductCopy.status(freshness, localization: localization)
        }
        if conflict == "conflict" {
            status += localization.textValue(" · 存在冲突")
        }
        return status
    }

    private static func timestampText(
        _ milliseconds: Int64,
        timeZone: TimeZone,
        localization: AppLocalization
    ) -> String {
        let date = Date(timeIntervalSince1970: TimeInterval(milliseconds) / 1_000)
        let formatter = DateFormatter()
        formatter.locale = localization.locale
        formatter.timeZone = timeZone
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter.string(from: date)
    }
}
