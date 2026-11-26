using System.Globalization;
using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public sealed record AgyTraySummary(CurrentWindow? TightestWindow,double? RemainingPercent,string PoolLabel,string WindowLabel,string CompactText,bool IsFallback);

public static class TrayPresentation
{
    public const string ClipboardText="Codex Pulse · Usage snapshot · Account details hidden";
    public static bool CanShowQuota(string provider,QuotaCurrentResponse? quota,AccountSnapshotResponse? account)=>
        quota?.ProviderContext?.EffectiveProvider==provider &&
        (provider!="codex" || QuotaPresentation.SameAccount(quota.Current?.Binding,account?.Binding));
    public static string Identity(AccountSnapshotResponse? account,bool privacy,Localization l)
        =>Identity("codex",account,privacy,l);
    public static string Identity(string provider,AccountSnapshotResponse? account,bool privacy,Localization l)
    {
        if(privacy)return l.Text("账号资料已隐藏","Account details hidden");
        if(provider is "cursor" or "grok" or "agy")
        {
            if(account?.Account is not {} identity || identity.Type!=provider)return l.Text("账号资料暂不可用","Account details unavailable");
            var email=identity.HasEmail && identity.Email.Length>0?identity.Email:l.Text("邮箱未提供","Email not provided");
            var plan=identity.HasPlanType && identity.PlanType.Length>0?identity.PlanType:l.Text("套餐未提供","Plan not provided");
            if(provider=="cursor")plan=plan.ToLowerInvariant() switch{"pro_plus" or "pro+"=>"Pro+","pro"=>"Pro","ultra"=>"Ultra","free"=>"Free",_=>plan};
            return email+" · "+plan;
        }
        if(provider!="codex")return l.Text("此客户端不提供账号资料","This client does not provide account details");
        if(account?.Subscription is not {} profile)return account?.Account?.Email??l.Text("当前已确认账号","Current confirmed account");
        return QuotaPresentation.AccountTitle(profile,l)+" · "+QuotaPresentation.Plan(profile.HasResolvedPlan?profile.ResolvedPlan:null,l)+"\n"+QuotaPresentation.SubscriptionDate(profile,l);
    }
    public static CurrentWindow? TightestAgyWindow(CurrentQuota? quota,long evaluatedAtMs)
    {
        if(quota is null)return null;
        return QuotaPresentation.Windows(quota)
            .Select(window=>(Window:window,Remaining:QuotaPresentation.EffectiveRemaining(window,evaluatedAtMs)))
            .Where(item=>item.Remaining is not null)
            .OrderBy(item=>item.Remaining!.Value)
            .ThenBy(item=>item.Window.WindowKind=="primary" || item.Window.HasWindowMinutes && item.Window.WindowMinutes==300?0
                :item.Window.WindowKind=="secondary" || item.Window.HasWindowMinutes && item.Window.WindowMinutes==10080?1:2)
            .ThenBy(item=>QuotaPresentation.AgyPoolOrderRank(item.Window.LimitId))
            .ThenBy(item=>item.Window.LimitId,StringComparer.Ordinal)
            .Select(item=>item.Window)
            .FirstOrDefault();
    }
    public static string AgyCompactPoolLabel(string limitId,string? limitName,Localization l)=>limitId switch
    {
        QuotaPresentation.AgyLimitGemini=>"Gemini",
        QuotaPresentation.AgyLimitArgon=>"Argon",
        QuotaPresentation.AgyLimitCarbon=>"Carbon",
        QuotaPresentation.AgyLimitClaudeGpt=>"Claude & GPT",
        _=>QuotaPresentation.PoolDisplayName(limitId,limitName,l)
    };
    public static long? TodayTokens(UsageCostResponse? usage,DateOnly today)
    {
        if(usage is null || usage.Meta?.Status=="unavailable")return null;
        var key=today.ToString("yyyy-MM-dd",CultureInfo.InvariantCulture);
        var bucket=usage.Trend.FirstOrDefault(point=>point.Key==key);
        if(bucket is not null)return DashboardPresentation.Value(bucket.Totals?.TotalTokens);
        return DashboardPresentation.Complete(usage.Meta?.Status)?0:null;
    }
    public static AgyTraySummary SummarizeAgyTray(QuotaCurrentResponse? quota,AccountSnapshotResponse? account,UsageCostResponse? usage,DateOnly today,long evaluatedAtMs,Localization l)
    {
        if(CanShowQuota("agy",quota,account) && TightestAgyWindow(quota!.Current,evaluatedAtMs) is {} tightest)
        {
            var remaining=QuotaPresentation.EffectiveRemaining(tightest,evaluatedAtMs)!.Value;
            var poolLabel=AgyCompactPoolLabel(tightest.LimitId,tightest.HasLimitName?tightest.LimitName:null,l);
            var windowLabel=tightest.WindowKind=="primary" || tightest.HasWindowMinutes && tightest.WindowMinutes==300?"5h"
                :tightest.WindowKind=="secondary" || tightest.HasWindowMinutes && tightest.WindowMinutes==10080?l.Text("周","7d")
                :QuotaPresentation.Duration(tightest.HasWindowMinutes?tightest.WindowMinutes:null,l);
            var compact=Workspace.ProviderCompactName("agy")+" · "+poolLabel+" "+windowLabel+" "+QuotaPresentation.PercentText(remaining,l);
            return new(tightest,remaining,poolLabel,windowLabel,compact,false);
        }
        var todayTokens=TodayTokens(usage,today);
        var fallback=todayTokens is not null
            ?Workspace.ProviderCompactName("agy")+" · "+l.Text("今日 ","Today ")+l.Tokens(todayTokens,compact:true)
            :Workspace.ProviderCompactName("agy")+" · --";
        return new(null,null,"","",fallback,true);
    }
    public static string Tooltip(string provider,QuotaCurrentResponse? quota,AccountSnapshotResponse? account,UsageCostResponse? usage,DateOnly today,LoadKind loadKind,long readAtMs,Localization l)
    {
        if(provider=="agy")
        {
            var summary=SummarizeAgyTray(quota,account,usage,today,readAtMs,l);
            return "Codex Pulse · "+summary.CompactText+" · "+l.Timestamp(readAtMs);
        }
        var trusted=CanShowQuota(provider,quota,account);
        var tooltip="Codex Pulse · "+Workspace.ProviderDisplayName(provider)+" · "+l.State(loadKind)+" · Token "+l.Tokens(usage?.Totals?.TotalTokens,compact:true);
        if(trusted && quota?.Current is {} current)
            tooltip+=" · "+string.Join(" / ",QuotaPresentation.Windows(current).Take(2).Select(w=>QuotaPresentation.PercentText(QuotaPresentation.EffectiveRemaining(w,readAtMs),l)));
        return tooltip+" · "+l.Timestamp(readAtMs);
    }
}
