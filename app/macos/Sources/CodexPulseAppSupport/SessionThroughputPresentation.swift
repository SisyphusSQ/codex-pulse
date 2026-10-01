import CodexPulseProtocolGenerated
import Foundation

public struct SessionThroughputPresentation: Sendable {
    public let rateText: String
    public let durationText: String
    public let coverageText: String
    public let statusText: String
    public let reasonText: String
    public let durationSourceText: String

    public init(
        _ stats: Codexpulse_Core_V1_ThroughputStats?,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        guard let stats else {
            rateText = "--"
            durationText = "--"
            coverageText = ""
            statusText = localization.text("暂不可用")
            reasonText = localization.text("此来源暂不支持 TPS")
            durationSourceText = ""
            return
        }
        let rate = stats.averageOutputMilliTps
        if rate.hasValue, rate.value >= 0, rate.unit == "milli_tokens_per_second" {
            rateText = String(format: "%.2f TPS", locale: localization.locale, Double(rate.value) / 1_000)
        } else {
            rateText = "--"
        }
        let duration = stats.activeDurationMs
        if duration.hasValue, duration.value >= 0, duration.unit == "milliseconds" {
            durationText = localization.format("%.3f 秒", Double(duration.value) / 1_000)
        } else {
            durationText = "--"
        }
        switch stats.reason {
        case "index_pending", "inherited_history", "state_limit":
            coverageText = ""
        default:
            coverageText = localization.format(
                "%@ 轮参与 · %@ 轮排除 · %@ 轮未结束",
                stats.includedTurns.hasValue ? localization.number(stats.includedTurns.value) : "--",
                stats.excludedTurns.hasValue ? localization.number(stats.excludedTurns.value) : "--",
                stats.openTurns.hasValue ? localization.number(stats.openTurns.value) : "--"
            )
        }
        let statusKey = switch stats.status {
        case "complete": "已结束轮次平均"
        case "partial": "已知轮次平均"
        default: "暂不可用"
        }
        statusText = localization.text(statusKey)
        let reasonKey = switch stats.reason {
        case "index_pending": "TPS 数据待补齐"
        case "index_incomplete": "索引尚未追平，当前为已知轮次平均。"
        case "open_turn": "当前轮待结束"
        case "no_closed_turns": "尚无可计算的已结束轮次"
        case "incomplete_coverage": "部分轮次未结束或缺少统计事实。"
        case "missing_facts": "轮次缺少有效输出或时长"
        case "numeric_overflow": "统计数值超出支持范围"
        case "state_limit": "轮次记录超出支持范围"
        case "inherited_history": "会话包含继承历史，无法确认独立 TPS"
        default: ""
        }
        reasonText = localization.text(reasonKey)
        let durationKey = switch stats.durationSource {
        case "duration_ms": "日志提供的毫秒耗时"
        case "log_timestamp": "日志起止时间差"
        case "source_seconds": "秒级起止时间差"
        case "mixed": "混合时间来源"
        default: ""
        }
        durationSourceText = localization.text(durationKey)
    }
}
