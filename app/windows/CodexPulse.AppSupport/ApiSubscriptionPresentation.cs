using System.Globalization;
using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public sealed record ApiActivityCell(string DateKey,int Intensity,string Detail);
public static class ApiSubscriptionPresentation
{
    public static decimal? Amount(string value)=>decimal.TryParse(value,NumberStyles.AllowDecimalPoint|NumberStyles.AllowLeadingSign,CultureInfo.InvariantCulture,out var amount)?amount:null;
    public static string DecimalText(string? value)=>string.IsNullOrEmpty(value)?"—":value;
    public static bool HasFacts(APISubscriptionSourceStatus? status)=>status?.State is "current" or "stale";
    public static LoadKind Kind(APISubscriptionsCurrentResponse value)=>new[]{value.DeepSeek?.Status?.State,value.OpenCodeGo?.Status?.State}.Any(state=>state is "stale" or "unavailable")?LoadKind.Partial:LoadKind.Ready;
    public static string Status(APISubscriptionSourceStatus? value,Localization l)=>value?.State switch
    {"current"=>l.Text("本次已更新","Updated this time"),"stale"=>l.Text("显示上次成功数据","Showing last successful data"),"unconfigured"=>l.Text("尚未配置 API Key","API key not configured"),"unavailable"=>l.Text("本次不可用，无成功数据","Unavailable; no successful data"),_=>l.Text("状态未知","Unknown state")};
    public static string Failure(string value,Localization l)=>value switch
    {"network"=>l.Text("网络连接失败","Network failure"),"timeout"=>l.Text("请求超时","Request timed out"),"auth"=>l.Text("凭据未通过认证","Credential rejected"),"forbidden"=>l.Text("账户无访问权限","Account access denied"),"rate_limit"=>l.Text("请求受到限流","Request rate limited"),"server"=>l.Text("远端服务失败","Remote service failure"),"protocol"=>l.Text("响应协议不兼容","Incompatible response"),_=>l.Text("来源失败","Source failure")};
    public static string Window(string kind,Localization l)=>kind switch{"five_hour"=>l.Text("5 小时","5 hours"),"weekly"=>l.Text("每周","Weekly"),"monthly"=>l.Text("每月","Monthly"),_=>l.Text("未知窗口","Unknown window")};
    public static string Period(string kind,Localization l)=>kind switch{"today"=>l.Text("今天","Today"),"week"=>l.Text("本周","This week"),_=>l.Text("本月","This month")};
    public static IReadOnlyList<string> Currencies(APISubscriptionsCurrentResponse value)=>
        (value.DeepSeek?.Balance?.Balances.Select(v=>v.Currency)??[]).Concat(value.DeepSeek?.Periods.SelectMany(v=>v.Series.Select(s=>s.Currency))??[])
        .Concat(value.ActivityCalendar?.Days.SelectMany(v=>v.DeepSeek.Select(s=>s.Currency))??[]).Where(v=>v.Length>0).Distinct().Order().ToArray();
    public static IReadOnlyList<ApiActivityCell> Activity(APISubscriptionActivityCalendar? calendar,string currency,Localization l)
    {
        if(calendar is null)return [];
        var values=calendar.Days.Select(day=>(Day:day,Deep:day.DeepSeek.FirstOrDefault(v=>v.Currency==currency && v.SampleCount>0),Go:day.OpenCodeGo is {SampleCount:>0} go?go:null)).ToArray();
        var deepScale=values.Select(v=>v.Deep is {} d?Amount(d.TotalConsumed):null).Where(v=>v>0).Select(v=>v!.Value).Order().ToArray();
        var goScale=values.Where(v=>v.Go is not null).Select(v=>Percent(v.Go!.MaxFiveHourUsedPercent)).Where(v=>v>0).Select(v=>v!.Value).Order().ToArray();
        return values.Select(v=>new ApiActivityCell(v.Day.DateKey,Math.Max(Level(v.Deep is {} d?Amount(d.TotalConsumed):null,deepScale),Level(v.Go is {} g?Percent(g.MaxFiveHourUsedPercent):null,goScale)),
            v.Day.DateKey+"\nDeepSeek · "+currency+" · "+l.Text("采样估算充值 / 消耗：","Sampled recharge / consumption: ")+(v.Deep is {} ds?ds.TotalRecharged+" / "+ds.TotalConsumed+" · "+ds.SampleCount+l.Text(" 个样本"," samples"):"—")+
            "\nOpenCode Go · "+l.Text("5 小时峰值已用 / 最新剩余：","5-hour peak used / latest remaining: ")+(v.Go is {} oc?oc.MaxFiveHourUsedPercent.ToString("0.##",l.Culture)+"% / "+oc.LatestFiveHourRemainingPercent.ToString("0.##",l.Culture)+"% · "+oc.SampleCount+l.Text(" 个样本"," samples"):"—"))).ToArray();
    }
    private static decimal? Percent(double value)=>double.IsFinite(value) && value is >=0 and <=100?(decimal)value:null;
    private static int Level(decimal? value,decimal[] positive)
    {
        if(value is null)return -1;if(value<=0)return 0;if(positive.Length==0)return 1;
        if(value<=positive[(int)((positive.Length-1)*.25)])return 1;
        if(value<=positive[(int)((positive.Length-1)*.5)])return 2;
        if(value<=positive[(int)((positive.Length-1)*.75)])return 3;return 4;
    }
}
