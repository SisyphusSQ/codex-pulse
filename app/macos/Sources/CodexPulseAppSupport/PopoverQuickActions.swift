import AppKit
import Foundation

public enum PopoverQuickActionKind: Hashable, Sendable {
    case accountSummary
    case openProject
    case copyPrivacySummary
}

@MainActor
public enum PopoverPasteboardPayload {
    public static func write(
        text: String,
        png: Data,
        to pasteboard: NSPasteboard
    ) -> Bool {
        let item = NSPasteboardItem()
        guard item.setString(text, forType: .string),
              item.setData(png, forType: .png)
        else { return false }

        pasteboard.clearContents()
        return pasteboard.writeObjects([item])
    }
}

public enum PopoverAccountSummaryAvailability: Equatable, Sendable {
    case available
    case empty
    case unavailable
}

public struct PopoverAccountSummaryPresentation: Equatable, Sendable {
    public let availability: PopoverAccountSummaryAvailability
    public let title: String
    public let detail: String
    public let accessibilityLabel: String

    public init(quotaWindows: [QuotaWindowPresentation], quotaAvailable: Bool) {
        self.title = "当前 Codex 账号"
        if !quotaAvailable {
            self.availability = .unavailable
            self.detail = "套餐与额度信息暂不可用"
        } else if quotaWindows.isEmpty {
            self.availability = .empty
            self.detail = "套餐信息未提供 · 暂无额度数据"
        } else {
            self.availability = .available
            self.detail = "套餐信息未提供 · \(quotaWindows.count) 项额度"
        }
        self.accessibilityLabel = "\(title)，\(detail)"
    }
}

public extension OverviewPresentation {
    var popoverAccountSummary: PopoverAccountSummaryPresentation {
        PopoverAccountSummaryPresentation(
            quotaWindows: quotaWindows,
            quotaAvailable: quotaAvailable
        )
    }
}

public struct PopoverPrivacySummary: Equatable, Sendable {
    public let accountTitle: String
    public let accountDetail: String
    public let quotaRows: [String]
    public let resetCreditsRow: String
    public let plainText: String

    public init(_ overview: OverviewPresentation) {
        let account = overview.popoverAccountSummary
        let quotaRows = overview.quotaWindows.prefix(2).map { window in
            "\(window.title)，剩余 \(Self.percentText(window.remainingPercent))，"
                + "下次重置 \(Self.durationText(window.resetRemainingMS))"
        }
        let resetCreditsRow =
            "\(Self.countText(overview.resetCredits.availableCount)) 可用 / "
            + "\(Self.countText(overview.resetCredits.totalCount)) 总数"
        var lines = [
            "Codex Pulse 隐私摘要",
            "账户：\(account.title)",
            "套餐与额度：\(account.detail)",
        ]
        lines.append(contentsOf: quotaRows.enumerated().map { index, row in
            "额度 \(index + 1)：\(row)"
        })
        lines.append("重置次数：\(resetCreditsRow)")
        lines.append("仅包含 Popover 已展示的聚合信息")

        self.accountTitle = account.title
        self.accountDetail = account.detail
        self.quotaRows = quotaRows
        self.resetCreditsRow = resetCreditsRow
        self.plainText = lines.joined(separator: "\n")
    }

    private static func percentText(_ value: Double?) -> String {
        value.map { String(format: "%.0f%%", $0) } ?? "--"
    }

    private static func countText(_ value: Int64?) -> String {
        value?.formatted() ?? "--"
    }

    private static func durationText(_ milliseconds: Int64?) -> String {
        guard let milliseconds, milliseconds >= 0 else { return "--" }
        let totalMinutes = milliseconds / 60_000
        let days = totalMinutes / 1_440
        let hours = (totalMinutes % 1_440) / 60
        let minutes = totalMinutes % 60
        if days > 0 {
            return hours > 0 ? "\(days) 天 \(hours) 小时" : "\(days) 天"
        }
        if hours > 0 {
            return minutes > 0 ? "\(hours) 小时 \(minutes) 分钟" : "\(hours) 小时"
        }
        return "\(minutes) 分钟"
    }
}

public enum PopoverQuickActionResult: Equatable, Sendable {
    case success(title: String, message: String)
    case failure(title: String, message: String)

    public var title: String {
        switch self {
        case .success(let title, _), .failure(let title, _): title
        }
    }

    public var message: String {
        switch self {
        case .success(_, let message), .failure(_, let message): message
        }
    }

    public var isFailure: Bool {
        if case .failure = self { return true }
        return false
    }
}

public enum PopoverQuickActions {
    private static let projectURL = URL(
        string: "https://github.com/SisyphusSQ/codex-pulse"
    )!

    public static func openProject(
        using opener: (URL) -> Bool
    ) -> PopoverQuickActionResult {
        guard opener(projectURL) else {
            return .failure(
                title: "无法打开项目主页",
                message: "系统未能打开 GitHub 项目主页，请稍后再试。"
            )
        }
        return .success(
            title: "已打开项目主页",
            message: "已交给默认浏览器处理。"
        )
    }

    public static func copyPrivacySummary(
        _ summary: PopoverPrivacySummary,
        renderPNG: (PopoverPrivacySummary) -> Data?,
        writeClipboard: (String, Data) -> Bool
    ) -> PopoverQuickActionResult {
        guard let png = renderPNG(summary) else {
            return .failure(
                title: "无法复制安全摘要",
                message: "安全截图生成失败，未写入剪贴板。"
            )
        }
        guard writeClipboard(summary.plainText, png) else {
            return .failure(
                title: "无法复制安全摘要",
                message: "剪贴板写入失败，未复制任何数据。"
            )
        }
        return .success(
            title: "已复制安全摘要",
            message: "安全截图和脱敏摘要已写入剪贴板。"
        )
    }
}
