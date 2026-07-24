import Foundation

public enum ProductCopy {
    public static func unknownMetric(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "not_computed": "未计算"
        case "not_applicable": "不适用"
        default: "暂不可用"
        }
    }

    public static func status(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "healthy", "current", "ready", "normal", "fresh":
            "正常"
        case "succeeded", "complete", "completed", "applied":
            "已完成"
        case "active", "running", "in_progress":
            "进行中"
        case "queued", "pending":
            "等待中"
        case "idle":
            "空闲"
        case "warning", "degraded", "partial", "interrupted", "stale":
            "需要关注"
        case "failed", "critical", "error", "blocked":
            "需要处理"
        case "unavailable":
            "暂不可用"
        case "cancelled", "canceled":
            "已取消"
        case "resolved":
            "已解决"
        case "available":
            "可用"
        case "configured":
            "已配置"
        case "not_configured":
            "未配置"
        case "switching":
            "正在切换"
        case "redeemed", "used":
            "已使用"
        case "expired":
            "已过期"
        case "", "unknown":
            "暂时未知"
        default:
            "其他状态"
        }
    }

    public static func confidence(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "high": "高"
        case "medium": "中"
        case "low": "低"
        default: "暂时未知"
        }
    }

    public static func phase(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "discover", "discovery": "正在查找数据"
        case "backfill": "正在补充历史数据"
        case "index", "indexing": "正在整理数据"
        case "reconcile": "正在校准数据"
        case "complete", "completed", "done": "已完成"
        case "queued", "pending": "等待中"
        case "running", "active": "处理中"
        case "failed", "error": "处理失败"
        case "", "unknown": "暂时未知"
        default: "处理中"
        }
    }

    public static func sourceName(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "codex", "codex_cli", "local_jsonl", "session_jsonl": "Codex 本机会话"
        case "chatgpt", "openai", "quota", "quota_api": "Codex 额度"
        case "reset_credits": "重置次数"
        case "", "unknown": "本机数据"
        default: "本机数据"
        }
    }

    public static func jobName(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "discover", "discovery": "查找本机数据"
        case "backfill": "补充历史数据"
        case "index", "indexing": "整理会话数据"
        case "reconcile": "校准使用量"
        case "quota_refresh": "更新额度"
        case "reset_credits_refresh": "更新重置次数"
        default: "数据更新任务"
        }
    }

    public static func settingOption(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "main_window": "打开主窗口"
        case "tray", "menu_bar": "仅显示菜单栏"
        case "today": "今天"
        case "quota_week": "周额度"
        case "7d", "seven_days": "最近 7 天"
        case "30d", "thirty_days": "最近 30 天"
        case "stable": "稳定版"
        case "beta": "测试版"
        default: rawValue.isEmpty ? "默认" : "其他选项"
        }
    }

    public static func interval(seconds: Int64) -> String {
        guard seconds > 0 else { return "关闭" }
        if seconds.isMultiple(of: 3_600) { return "\(seconds / 3_600) 小时" }
        if seconds.isMultiple(of: 60) { return "\(seconds / 60) 分钟" }
        return "\(seconds) 秒"
    }

    public static func duration(milliseconds: Int64?) -> String {
        guard let milliseconds, milliseconds > 0 else { return "时间待定" }
        let totalMinutes = milliseconds / 60_000
        if totalMinutes >= 1_440 {
            let days = totalMinutes / 1_440
            let hours = totalMinutes % 1_440 / 60
            return hours > 0 ? "\(days) 天 \(hours) 小时" : "\(days) 天"
        }
        if totalMinutes >= 60 {
            let hours = totalMinutes / 60
            let minutes = totalMinutes % 60
            return minutes > 0 ? "\(hours) 小时 \(minutes) 分钟" : "\(hours) 小时"
        }
        return "\(max(totalMinutes, 1)) 分钟"
    }

    public static func recoveryAction(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "retry": "重试"
        case "resume": "继续处理"
        case "reconcile": "重新校准"
        case "restart": "重新连接"
        case "check_source": "检查数据来源"
        case "grant_permission": "授予访问权限"
        case "free_space": "释放存储空间"
        case "repair_store": "修复本机数据"
        case "none", "": "暂无建议操作"
        default: "查看处理建议"
        }
    }

    public static func component(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "scheduler": "数据更新"
        case "database", "sqlite", "store": "本地数据库"
        case "source", "sources", "discovery": "数据来源"
        case "index", "indexer", "local_index": "会话索引"
        case "live_queue": "实时数据"
        case "history_backfill": "历史数据"
        case "quota", "online_quota": "额度数据"
        case "storage": "本机存储"
        case "runtime": "本地服务"
        case "updater": "版本更新"
        case "", "unknown": "本地数据"
        default: "本地数据"
        }
    }

    public static func reason(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "healthy": "运行正常"
        case "not_configured": "尚未配置"
        case "index_paused", "backfill_paused": "数据更新已暂停"
        case "index_draining": "正在完成剩余任务"
        case "index_reconciling": "正在校准用量"
        case "system_sleeping": "系统已进入休眠"
        case "live_queue_stalled": "实时数据更新延迟"
        case "backfill_stalled": "历史数据更新延迟"
        case "auth_required", "source_permission", "store_permission": "需要授予访问权限"
        case "disk_low", "store_disk_full": "存储空间不足"
        case "cpu_pressure", "memory_pressure": "本机资源使用较高"
        case "updater_unavailable", "updater_unknown": "暂时无法检查更新"
        case "metrics_stale": "状态信息等待更新"
        case "source_timeout", "source_unavailable": "数据来源暂时不可用"
        case "source_corrupt", "store_corrupt": "本机数据需要修复"
        case "source_stale": "数据来源需要更新"
        case "job_interrupted", "job_failed", "job_cancelled": "数据更新未完成"
        case "store_busy": "本机数据正在使用中"
        case "store_read_only": "本机数据当前只读"
        case "store_io", "store_unavailable": "本机数据暂时不可用"
        case "wal_pressure": "本机数据正在整理"
        case "pricing_unavailable", "pricing_invalid": "价格信息暂时不可用"
        case "source_failure_streak": "数据来源连续更新失败"
        case "runtime_unknown", "lifecycle_unknown", "source_unknown", "store_unknown", "", "unknown":
            "当前状态尚未确定"
        default: "当前状态需要关注"
        }
    }

    public static func impact(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "none", "": "暂无影响"
        case "indexing_stopped": "新会话将暂时无法整理"
        case "indexing_paused": "会话整理已暂停"
        case "live_data_delayed": "最新用量可能延迟显示"
        case "history_incomplete": "部分历史用量可能缺失"
        case "online_quota_unavailable": "额度暂时无法更新"
        case "storage_at_risk": "本机数据存储可能受影响"
        case "runtime_at_risk": "本地数据服务可能不稳定"
        case "update_checks_unavailable": "暂时无法检查新版本"
        default: "部分功能可能暂时受影响"
        }
    }

    public static func protection(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "none", "": "无需额外保护"
        case "writes_stopped": "已停止写入以保护本机数据"
        case "auto_retry_stopped": "已暂停自动重试"
        case "user_pause_retained": "已保留你的暂停设置"
        case "retry_backoff": "将自动降低重试频率"
        case "observation_only": "仅监测，不会自动更改数据"
        default: "已启用保护措施"
        }
    }

    public static func eventName(_ rawValue: String) -> String {
        switch normalized(rawValue) {
        case "source_unavailable": "数据来源暂时不可用"
        case "source_stale": "数据来源需要更新"
        case "index_incomplete": "部分会话尚未整理"
        case "scheduler_failed": "数据更新未完成"
        case "quota_unavailable": "额度暂时无法获取"
        default: "本机数据提醒"
        }
    }

    private static func normalized(_ value: String) -> String {
        value.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    }
}
