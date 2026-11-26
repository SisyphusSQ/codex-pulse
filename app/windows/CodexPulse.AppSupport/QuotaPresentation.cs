using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public sealed record QuotaPoolGroup(string LimitId,string DisplayName,CurrentWindow? Primary,CurrentWindow? Secondary);
public sealed record QuotaPacePoolGroup(string LimitId,string DisplayName,QuotaPaceWindow? Primary,QuotaPaceWindow? Secondary);

public static class QuotaPresentation
{
    public const string AgyLimitGemini="agy.gemini";
    public const string AgyLimitArgon="agy.argon";
    public const string AgyLimitCarbon="agy.carbon";
    public const string AgyLimitClaudeGpt="agy.claude_gpt";
    public static bool IsAgyPool(string? limitId)=>!string.IsNullOrEmpty(limitId) && limitId.StartsWith("agy.",StringComparison.Ordinal);
    public static int AgyPoolOrderRank(string? limitId)=>limitId switch
    {
        AgyLimitGemini=>0,
        AgyLimitArgon=>1,
        AgyLimitCarbon=>2,
        AgyLimitClaudeGpt=>3,
        _=>4
    };
    public static bool Confirmed(CodexAccountBinding? binding)=>binding is{State:"confirmed",HasAccountScope:true,BindingGeneration:>0} && !string.IsNullOrWhiteSpace(binding.AccountScope);
    public static bool SameAccount(CodexAccountBinding? left,CodexAccountBinding? right)=>Confirmed(left) && Confirmed(right) && left!.AccountScope==right!.AccountScope && left.BindingGeneration==right.BindingGeneration;
    public static double? Percent(bool present,double value)=>present && double.IsFinite(value) && value is>=0 and<=100?value:null;
    public static bool IsExpired(CurrentWindow window,long evaluatedAtMs)=>window.Freshness=="expired_unknown" || window.HasResetsAtMs && window.ResetsAtMs<=evaluatedAtMs;
    public static double? EffectiveRemaining(CurrentWindow window,long evaluatedAtMs)=>IsExpired(window,evaluatedAtMs)?null:Percent(window.HasRemainingPercent,window.RemainingPercent);
    public static string PercentText(double? value,Localization l)
    {
        if(value is null)return "—";
        var text=value.Value.ToString("0.##",l.Culture);
        if(value is>0 and<100 && (text=="0" || text=="100"))text=value.Value.ToString("R",l.Culture);
        return text+"%";
    }
    public static string Duration(long? minutes,Localization l)=>minutes is>0
        ?minutes%1440==0?(minutes.Value/1440).ToString(l.Culture)+l.Text(" 天"," days")
        :minutes%60==0?(minutes.Value/60).ToString(l.Culture)+l.Text(" 小时"," hours")
        :minutes.Value.ToString(l.Culture)+l.Text(" 分钟"," minutes"):l.Text("周期未知","Unknown period");
    public static string PoolDisplayName(string id,string? name,Localization l)
    {
        if(!string.IsNullOrWhiteSpace(name))return name;
        if(id.StartsWith("agy.pool.",StringComparison.Ordinal))
        {
            var slug=id["agy.pool.".Length..].Replace('_',' ').Trim();
            return slug.Length>0?slug:l.Text("其他配额池","Other quota pool");
        }
        return id switch
        {
            "codex"=>l.Text("通用额度","General quota"),
            "cursor.models"=>"Cursor Models",
            "cursor.other_models"=>"Other Models",
            "cursor.grok_bot"=>"Grok Bot",
            "grok.included_credits"=>l.Text("包含的 credits","Included credits"),
            AgyLimitGemini=>"Gemini",
            AgyLimitArgon=>"Gemini 4 Argon",
            AgyLimitCarbon=>"Gemini 4 Carbon",
            AgyLimitClaudeGpt=>"Claude & GPT",
            _=>l.Text("模型专属额度","Model quota")
        };
    }
    public static string WindowShortLabel(string windowKind,long? minutes,Localization l)=>
        windowKind=="primary" || minutes==300?l.Text("5 小时","5h")
        :windowKind=="secondary" || minutes==10080?l.Text("7 天","Weekly")
        :Duration(minutes,l);
    public static string WindowName(string id,string? name,long? minutes,Localization l)=>PoolDisplayName(id,name,l)+" · "+Duration(minutes,l);
    public static string Freshness(string state,Localization l)=>state switch
    {"fresh" or "current"=>l.Text("最新可信值","Fresh verified value"),"stale"=>l.Text("上次可信值","Last known value"),"expired_unknown"=>l.Text("周期已结束，等待确认","Period ended; awaiting confirmation"),"never_loaded" or "unknown"=>l.Text("尚未取得可信值","No verified value yet"),"suspicious"=>l.Text("新数据异常","New data is suspicious"),_=>l.Text("暂不可用","Unavailable")};
    public static string Forecast(string state,Localization l)=>state switch
    {"on_track"=>l.Text("按当前节奏可持续至重置","Current pace is sustainable until reset"),"at_risk"=>l.Text("可能在重置前耗尽","May exhaust before reset"),"exhausted"=>l.Text("额度已耗尽","Quota exhausted"),_=>l.Text("证据不足，暂不预测","Insufficient evidence for a forecast")};
    public static IReadOnlyList<CurrentWindow> Windows(CurrentQuota quota)
    {
        var deduped=quota.Windows
            .GroupBy(window=>(window.LimitId,Minutes:window.HasWindowMinutes?window.WindowMinutes:(long?)null,Kind:IsAgyPool(window.LimitId)?window.WindowKind:""))
            .Select(group=>group.OrderByDescending(window=>window.HasResetsAtMs && window.ResetsAtMs>quota.EvaluatedAtMs)
                .ThenBy(window=>window.Freshness=="fresh"?0:1).ThenByDescending(window=>window.HasLimitName && !string.IsNullOrWhiteSpace(window.LimitName))
                .ThenBy(window=>window.WindowKind=="primary"?0:1).First());
        if(quota.Windows.Any(window=>IsAgyPool(window.LimitId)))
            return deduped
                .OrderBy(window=>AgyPoolOrderRank(window.LimitId))
                .ThenBy(window=>window.LimitId,StringComparer.Ordinal)
                .ThenBy(window=>window.WindowKind=="primary"?0:window.WindowKind=="secondary"?1:2)
                .ThenBy(window=>window.HasWindowMinutes?window.WindowMinutes:long.MaxValue)
                .ToArray();
        return deduped.OrderBy(window=>window.HasWindowMinutes?window.WindowMinutes:long.MaxValue).ThenBy(window=>window.LimitId,StringComparer.Ordinal).ToArray();
    }
    public static IReadOnlyList<QuotaPoolGroup> AgyPools(CurrentQuota quota,Localization l)=>Windows(quota)
        .GroupBy(window=>window.LimitId,StringComparer.Ordinal)
        .OrderBy(group=>AgyPoolOrderRank(group.Key))
        .ThenBy(group=>group.Key,StringComparer.Ordinal)
        .Select(group=>
        {
            var items=group.ToArray();
            var primary=items.FirstOrDefault(window=>window.WindowKind=="primary")??items.FirstOrDefault(window=>window.HasWindowMinutes && window.WindowMinutes==300);
            var secondary=items.FirstOrDefault(window=>!ReferenceEquals(window,primary) && window.WindowKind=="secondary")??items.FirstOrDefault(window=>!ReferenceEquals(window,primary));
            var rawName=items.Select(window=>window.HasLimitName && !string.IsNullOrWhiteSpace(window.LimitName)?window.LimitName:null).FirstOrDefault(name=>name is not null);
            return new QuotaPoolGroup(group.Key,PoolDisplayName(group.Key,rawName,l),primary,secondary);
        }).ToArray();
    public static IReadOnlyList<QuotaPacePoolGroup> AgyPacePools(CurrentQuotaPace pace,CurrentQuota? current,Localization l)=>pace.Windows
        .GroupBy(window=>window.LimitId,StringComparer.Ordinal)
        .OrderBy(group=>AgyPoolOrderRank(group.Key))
        .ThenBy(group=>group.Key,StringComparer.Ordinal)
        .Select(group=>
        {
            var items=group.ToArray();
            var primary=items.FirstOrDefault(window=>window.WindowKind=="primary")??items.FirstOrDefault(window=>window.HasWindowMinutes && window.WindowMinutes==300);
            var secondary=items.FirstOrDefault(window=>!ReferenceEquals(window,primary) && window.WindowKind=="secondary")??items.FirstOrDefault(window=>!ReferenceEquals(window,primary));
            var rawName=current?.Windows.Where(window=>window.LimitId==group.Key && window.HasLimitName && !string.IsNullOrWhiteSpace(window.LimitName)).Select(window=>window.LimitName).FirstOrDefault();
            return new QuotaPacePoolGroup(group.Key,PoolDisplayName(group.Key,rawName,l),primary,secondary);
        }).ToArray();
    public static IReadOnlyList<CodexAccountQuotaWindow> Windows(CodexAccountQuota quota,long evaluatedAt)=>quota.Windows
        .GroupBy(window=>(window.LimitId,Minutes:window.HasWindowMinutes?window.WindowMinutes:(long?)null))
        .Select(group=>group.OrderByDescending(window=>window.HasResetsAtMs && window.ResetsAtMs>evaluatedAt).ThenBy(window=>window.Freshness=="fresh"?0:1).ThenByDescending(window=>window.HasLastCollectedAtMs?window.LastCollectedAtMs:0).ThenBy(window=>window.WindowKind=="primary"?0:1).First())
        .OrderBy(window=>window.HasWindowMinutes?window.WindowMinutes:long.MaxValue).ThenBy(window=>window.LimitId,StringComparer.Ordinal).ToArray();
    public static string Plan(CodexSubscriptionPlan? plan,Localization l)=>plan switch
    {CodexSubscriptionPlan.Free=>"Free",CodexSubscriptionPlan.Go=>"Go",CodexSubscriptionPlan.Plus=>"Plus",CodexSubscriptionPlan.Pro5X=>"Pro 5×",CodexSubscriptionPlan.Pro20X=>"Pro 20×",CodexSubscriptionPlan.Team=>"Team",CodexSubscriptionPlan.Business=>"Business",CodexSubscriptionPlan.Enterprise=>"Enterprise",CodexSubscriptionPlan.Edu=>"Edu",_=>l.Text("套餐未知","Unknown plan")};
    public static string AccountTitle(CodexSubscriptionAccount account,Localization l)=>account.HasAlias && !string.IsNullOrWhiteSpace(account.Alias)?account.Alias:account.HasDisplayEmail && !string.IsNullOrWhiteSpace(account.DisplayEmail)?account.DisplayEmail:l.Text("已识别账号","Detected account");
    public static bool ReferenceModelVisible(string model)
    {
        var normalized=model.ToLowerInvariant();
        return normalized!="gpt-5.6" && !new[]{"gpt-5","gpt-5.1","gpt-5.2","gpt-5.3","gpt-5.4","gpt-5.5"}.Any(family=>normalized==family || normalized.StartsWith(family+"-",StringComparison.Ordinal));
    }
    public static string SubscriptionDate(CodexSubscriptionAccount account,Localization l)
    {
        if(!account.HasMembershipDate || !account.HasDateKind)return l.Text("尚未设置会员日期","No membership date set");
        if(!DateOnly.TryParseExact(account.MembershipDate,"yyyy-MM-dd",System.Globalization.CultureInfo.InvariantCulture,System.Globalization.DateTimeStyles.None,out _) || account.DateKind is not(CodexSubscriptionDateKind.NextRenewal or CodexSubscriptionDateKind.MembershipExpiry))return l.Text("会员日期暂不可用","Membership date unavailable");
        var title=account.DateKind==CodexSubscriptionDateKind.NextRenewal?l.Text("每月续费日 ","Monthly renewal day ")+account.MembershipDate[^2..]:l.Text("会员到期日 ","Membership expiry ")+account.MembershipDate;
        if(!account.HasDayDelta)return title;
        return title+" · "+(account.DayDelta==0?l.Text("今天","Today"):account.DayDelta>0?account.DayDelta.ToString(l.Culture)+l.Text(" 天后"," days remaining"):l.Text("已过 ","Overdue by ")+(-(long)account.DayDelta).ToString(l.Culture)+l.Text(" 天，需要更新"," days; update needed"));
    }
}
