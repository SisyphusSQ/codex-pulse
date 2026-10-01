import CodexPulseProtocolGenerated
import Foundation

public enum QuotaRefreshPresentation {
    public static func notice(
        _ refresh: Codexpulse_Core_V1_CurrentRefresh,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String? {
        guard refresh.hasRuntime else { return nil }
        let runtime = refresh.runtime
        let stateText: String? = switch runtime.state {
        case "recoverable": "额度刷新已停止，可点击“刷新额度”恢复。"
        case "blocked": "额度刷新已停止，请检查本地数据后重启应用。"
        default: nil
        }
        if let stateText {
            let reason: String = switch runtime.failureReason {
            case "timeout": "请求超时"
            case "store_busy": "本地数据忙碌"
            case "store_full": "磁盘空间不足"
            case "store_read_only", "store_permission": "本地数据不可写"
            case "store_corrupt", "invalid_record": "本地数据需要检查"
            default: "后台刷新异常"
            }
            return localization.textValue(stateText) + " " + localization.textValue(reason)
        }
        if runtime.diagnosticsDropped > 0 {
            return localization.textValue("刷新诊断写入受限，部分错误记录未保存。")
        }
        return nil
    }
}
