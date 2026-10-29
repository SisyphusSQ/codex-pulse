using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public static class QuotaPresentation
{
    public static bool Confirmed(CodexAccountBinding? binding)=>binding is{State:"confirmed",HasAccountScope:true,BindingGeneration:>0} && !string.IsNullOrWhiteSpace(binding.AccountScope);
    public static bool SameAccount(CodexAccountBinding? left,CodexAccountBinding? right)=>Confirmed(left) && Confirmed(right) && left!.AccountScope==right!.AccountScope && left.BindingGeneration==right.BindingGeneration;
    public static double? Percent(bool present,double value)=>present && double.IsFinite(value) && value is>=0 and<=100?value:null;
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
    public static string WindowName(string id,string? name,long? minutes,Localization l)
    {
        var title=!string.IsNullOrWhiteSpace(name)?name:id switch
        {"codex"=>l.Text("通用额度","General quota"),"cursor.models"=>"Cursor Models","cursor.other_models"=>"Other Models","cursor.grok_bot"=>"Grok Bot","grok.included_credits"=>l.Text("包含的 credits","Included credits"),_=>l.Text("模型专属额度","Model quota")};
        return title+" · "+Duration(minutes,l);
    }
    public static string Freshness(string state,Localization l)=>state switch
    {"fresh" or "current"=>l.Text("最新可信值","Fresh verified value"),"stale"=>l.Text("上次可信值","Last known value"),"expired_unknown"=>l.Text("周期已结束，等待确认","Period ended; awaiting confirmation"),"never_loaded" or "unknown"=>l.Text("尚未取得可信值","No verified value yet"),"suspicious"=>l.Text("新数据异常","New data is suspicious"),_=>l.Text("暂不可用","Unavailable")};
    public static string Forecast(string state,Localization l)=>state switch
    {"on_track"=>l.Text("按当前节奏可持续至重置","Current pace is sustainable until reset"),"at_risk"=>l.Text("可能在重置前耗尽","May exhaust before reset"),"exhausted"=>l.Text("额度已耗尽","Quota exhausted"),_=>l.Text("证据不足，暂不预测","Insufficient evidence for a forecast")};
    public static IReadOnlyList<CurrentWindow> Windows(CurrentQuota quota)=>quota.Windows
        .GroupBy(window=>(window.LimitId,Minutes:window.HasWindowMinutes?window.WindowMinutes:(long?)null))
        .Select(group=>group.OrderByDescending(window=>window.HasResetsAtMs && window.ResetsAtMs>quota.EvaluatedAtMs)
            .ThenBy(window=>window.Freshness=="fresh"?0:1).ThenByDescending(window=>window.HasLimitName && !string.IsNullOrWhiteSpace(window.LimitName))
            .ThenBy(window=>window.WindowKind=="primary"?0:1).First())
        .OrderBy(window=>window.HasWindowMinutes?window.WindowMinutes:long.MaxValue).ThenBy(window=>window.LimitId,StringComparer.Ordinal).ToArray();
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
