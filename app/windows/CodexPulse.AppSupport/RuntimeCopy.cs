using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public static class RuntimeCopy
{
    public static string Discovery(ProviderDiscoveryState value,Localization l)=>value switch{ProviderDiscoveryState.Available=>l.Text("已发现","Discovered"),ProviderDiscoveryState.Missing=>l.Text("未找到来源","Source not found"),ProviderDiscoveryState.Inaccessible=>l.Text("来源不可访问","Source inaccessible"),ProviderDiscoveryState.Invalid=>l.Text("来源无效","Invalid source"),_=>l.Text("尚未检测","Not checked yet")};
    public static string Effective(ProviderEffectiveState value,Localization l)=>value switch{ProviderEffectiveState.Enabled=>l.Text("采集已启用","Collection enabled"),ProviderEffectiveState.Disabled=>l.Text("采集已关闭","Collection disabled"),ProviderEffectiveState.Disabling=>l.Text("正在停止采集","Stopping collection"),_=>l.Text("暂不可采集","Collection unavailable")};
    public static string Availability(string value,Localization l)=>value switch{"available"=>l.Text("来源可用","Source available"),"not_found"=>l.Text("当前设备没有找到来源","No source found on this device"),"disabled"=>l.Text("已按设置关闭","Disabled by settings"),"permission" or "permission_denied"=>l.Text("需要访问权限","Access permission required"),"unsafe_path"=>l.Text("路径不安全或包含符号链接","Unsafe path or symbolic link"),"invalid_type"=>l.Text("来源路径类型无效","Invalid source path type"),_=>l.Text("请检查来源与设置","Check the source and settings")};
    public static string? AgySourceKey(SourceItem item)
    {
        var key=item.SourceKey??"";
        if(key=="agy.summaries" || key.EndsWith(":agy.summaries",StringComparison.Ordinal))return "agy.summaries";
        if(key=="agy.trajectories" || key.EndsWith(":agy.trajectories",StringComparison.Ordinal))return "agy.trajectories";
        if(key=="agy.transcripts" || key.EndsWith(":agy.transcripts",StringComparison.Ordinal))return "agy.transcripts";
        if(key=="agy.ls_rpc" || key.EndsWith(":agy.ls_rpc",StringComparison.Ordinal))return "agy.ls_rpc";
        return null;
    }
    public static bool IsAgySource(SourceItem item)=>(item.HasProvider && item.Provider=="agy") || AgySourceKey(item) is not null;
    public static string SourceLabel(SourceItem item,Localization l)
    {
        if(AgySourceKey(item) is {} agyKey)
        {
            var title=agyKey switch
            {
                "agy.summaries"=>l.Text("会话摘要","Conversation summaries"),
                "agy.trajectories"=>l.Text("轨迹与步骤用量","Trajectories & step usage"),
                "agy.transcripts"=>l.Text("转录回退日志","Transcript fallback"),
                "agy.ls_rpc"=>l.Text("本机 LS 配额服务","Local LS quota RPC"),
                _=>agyKey
            };
            return Workspace.ProviderDisplayName("agy")+" · "+title+" ("+agyKey+")";
        }
        return (item.HasProvider?Workspace.ProviderDisplayName(item.Provider)+" · ":"")+(item.HasSourceType?item.SourceType switch{"app_server_rate_limits"=>l.Text("Codex 在线额度","Codex online quota"),"app_server_reset_credits"=>l.Text("Codex 重置库存","Codex reset credits"),_=>item.SourceType}:item.Kind switch{"local_file"=>l.Text("本机文件","Local file"),"online"=>l.Text("在线来源","Online source"),_=>l.Text("客户端来源","Client source")});
    }
    public static string Bytes(NumericValue? value,Localization l)
    {
        if(value is not {HasValue:true})return "—";
        // Match macOS bytesText: binary scaling, decimal labels, one fractional digit.
        string[] units=["B","KB","MB","GB","TB"];
        var amount=(decimal)value.Value;var unit=0;
        while(amount>=1024 && unit<units.Length-1){amount/=1024;unit++;}
        return amount.ToString("F1",l.Culture)+" "+units[unit];
    }
    public static string State(string value,Localization l)=>value switch
    {"healthy"=>l.Text("正常","Healthy"),"busy" or "running"=>l.Text("处理中","Running"),"active"=>l.Text("活动来源","Active source"),"discovered"=>l.Text("已发现，等待索引","Discovered; awaiting indexing"),"paused"=>l.Text("已暂停","Paused"),"degraded"=>l.Text("部分能力受影响","Degraded"),"blocked"=>l.Text("需要处理","Blocked"),"queued"=>l.Text("等待中","Queued"),"succeeded" or "completed"=>l.Text("已完成","Succeeded"),"failed"=>l.Text("失败","Failed"),"interrupted"=>l.Text("已中断，可恢复","Interrupted; resumable"),"cancelled"=>l.Text("已取消","Cancelled"),"current" or "available"=>l.Text("可用","Available"),"stale"=>l.Text("上次可信值","Last known value"),"unavailable"=>l.Text("暂不可用","Unavailable"),"disabled"=>l.Text("已关闭","Disabled"),"enabled"=>l.Text("已启用","Enabled"),"resolved"=>l.Text("已解决","Resolved"),_=>l.Text("尚未确定","Unknown")};
    public static string Component(string value,Localization l)=>value switch
    {"local_index"=>l.Text("本机索引","Local index"),"local_trajectories"=>l.Text("本地轨迹","Local trajectories"),"live_queue"=>l.Text("实时队列","Live queue"),"history_backfill"=>l.Text("历史补齐","History backfill"),"online_quota"=>l.Text("在线额度","Online quota"),"storage"=>l.Text("存储","Storage"),"runtime"=>l.Text("本机服务","Local service"),"updater"=>l.Text("更新","Updater"),_=>l.Text("本机数据","Local data")};
    public static string Reason(string value,Localization l)=>value switch
    {"healthy"=>l.Text("运行正常","Operating normally"),"not_configured"=>l.Text("尚未配置","Not configured"),"index_paused" or "backfill_paused"=>l.Text("用户已暂停更新","Updates paused by the user"),"index_draining"=>l.Text("等待在途工作结束","Draining active work"),"index_reconciling"=>l.Text("正在对账来源","Reconciling sources"),"system_sleeping"=>l.Text("系统休眠中","System sleeping"),"live_queue_stalled"=>l.Text("实时数据更新延迟","Live updates delayed"),"backfill_stalled"=>l.Text("历史补齐进度停滞","History backfill stalled"),"auth_required"=>l.Text("需要恢复来源登录","Source authentication required"),"source_permission" or "store_permission"=>l.Text("访问权限不足","Access permission required"),"disk_low" or "store_disk_full"=>l.Text("存储空间不足","Insufficient storage space"),"cpu_pressure" or "memory_pressure"=>l.Text("本机资源使用较高","High local resource use"),"metrics_stale"=>l.Text("观测数据等待更新","Metrics awaiting refresh"),"source_timeout" or "source_unavailable"=>l.Text("来源暂不可用","Source unavailable"),"source_corrupt" or "store_corrupt"=>l.Text("本机数据需要检查","Local data needs inspection"),"source_stale"=>l.Text("来源数据已过时","Source data is stale"),"source_failure_streak"=>l.Text("来源连续更新失败","Consecutive source failures"),"store_busy"=>l.Text("数据库正在使用中","Database is busy"),"store_read_only"=>l.Text("数据库只读","Database is read-only"),"job_interrupted" or "job_failed" or "job_cancelled"=>l.Text("数据更新未完成","Data update incomplete"),_=>l.Text("请查看当前证据与恢复建议","Review current evidence and recovery guidance")};
    public static string Impact(string value,Localization l)=>value switch
    {"none" or ""=>l.Text("暂无影响","No current impact"),"indexing_stopped"=>l.Text("新数据暂时停止索引","New data indexing stopped"),"indexing_paused"=>l.Text("索引已暂停","Indexing paused"),"live_data_delayed"=>l.Text("最新用量可能延迟","Latest usage may be delayed"),"history_incomplete"=>l.Text("历史用量尚未完整","History usage incomplete"),"online_quota_unavailable"=>l.Text("在线额度暂不能更新","Online quota cannot update"),"storage_at_risk"=>l.Text("本机存储可能受影响","Local storage may be affected"),"runtime_at_risk"=>l.Text("本机服务可能不稳定","Local service may be unstable"),"update_checks_unavailable"=>l.Text("暂不能检查更新","Update checks unavailable"),_=>l.Text("部分能力可能受影响","Some capabilities may be affected")};
    public static string Protection(string value,Localization l)=>value switch
    {"none" or ""=>l.Text("无需额外保护","No additional protection needed"),"writes_stopped"=>l.Text("已停止写入保护数据","Writes stopped to protect data"),"auto_retry_stopped"=>l.Text("自动重试已停止","Automatic retry stopped"),"user_pause_retained"=>l.Text("保留用户暂停设置","User pause retained"),"retry_backoff"=>l.Text("按退避策略重试","Retries use backoff"),"observation_only"=>l.Text("仅观测，不自动改动","Observation only; no automatic changes"),_=>l.Text("已启用保护措施","Protection is active")};
}
