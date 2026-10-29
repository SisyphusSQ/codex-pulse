using System.Globalization;
using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public sealed class Localization
{
    public string Locale { get; private set; } = "system";
    public CultureInfo Culture => Locale == "system" ? CultureInfo.CurrentUICulture : CultureInfo.GetCultureInfo(Locale);
    public bool Chinese => Culture.TwoLetterISOLanguageName == "zh";
    public void Set(string locale) => Locale = locale is "zh-CN" or "en-US" ? locale : "system";
    public string Text(string chinese, string english) => Chinese ? chinese : english;
    public string FeatureTitle(Feature feature) => feature switch
    {
        Feature.Summary => Text("汇总", "Summary"), Feature.Overview => Text("概览", "Overview"),
        Feature.Sessions => Text("会话", "Sessions"), Feature.Projects => Text("项目", "Projects"),
        Feature.Quota => Text("额度与用量", "Quota & usage"), Feature.AccountQuotas => Text("账号额度", "Account quotas"),
        Feature.ApiSubscriptions => Text("API 与订阅", "API & subscriptions"), Feature.LocalStatus => Text("本机状态", "Local status"),
        Feature.SourcesJobs => Text("来源与任务", "Sources & jobs"), _ => Text("设置", "Settings")
    };
    public string Number(NumericValue? value) => value is { HasValue: true } ? value.Value.ToString("N0", Culture) : "—";
    public string Tokens(NumericValue? value, bool compact=false) => Tokens(value is {HasValue:true}?value.Value:null,compact);
    public string Tokens(long? value, bool compact=false)
    {
        if(value is null or <0)return "—";
        // Match macOS TokenQuantityFormatter, with decimal arithmetic preserving int64 input.
        (long Divisor,string Unit)[] magnitudes=Chinese
            ? [(10_000,"万"),(100_000_000,"亿")]
            : [(1_000,"K"),(1_000_000,"M"),(1_000_000_000,"B")];
        var index=Array.FindLastIndex(magnitudes,item=>value.Value>=item.Divisor);
        if(index<0)return value.Value.ToString("N0",Culture);
        var digits=compact?1:2;
        while(index+1<magnitudes.Length && Math.Round((decimal)value.Value/magnitudes[index].Divisor,digits,MidpointRounding.AwayFromZero)>=(decimal)magnitudes[index+1].Divisor/magnitudes[index].Divisor)index++;
        var scaled=Math.Round((decimal)value.Value/magnitudes[index].Divisor,digits,MidpointRounding.AwayFromZero);
        return scaled.ToString(compact?"0.#":"0.##",Culture)+(Chinese && !compact?" ":"")+magnitudes[index].Unit;
    }
    public string Money(NumericValue? value)
    {
        if (value is not { HasValue: true }) return "—";
        var amount = value.Value / 1_000_000m;
        var format = amount == 0 || Math.Abs(amount) >= 0.01m ? "N2" : Math.Abs(amount) >= 0.0001m ? "N4" : "N6";
        return "$" + amount.ToString(format, Culture);
    }
    public string Timestamp(long? milliseconds, string? timeZone=null, string format="g")
    {
        if(milliseconds is not (>=0 and <=253402300799999))return "—";
        var instant=DateTimeOffset.FromUnixTimeMilliseconds(milliseconds.Value);
        var time=timeZone is null?instant.ToLocalTime():TimeZoneInfo.ConvertTime(instant,TimeZoneInfo.FindSystemTimeZoneById(timeZone));
        return time.ToString(format,Culture);
    }
    public string State(LoadKind state) => state switch
    {
        LoadKind.Idle => Text("尚未加载", "Not loaded"), LoadKind.Loading => Text("正在加载", "Loading"),
        LoadKind.Ready => Text("已更新", "Updated"), LoadKind.Empty => Text("当前范围没有数据", "No data in this range"),
        LoadKind.Partial => Text("部分数据可用", "Partial data"), LoadKind.Stale => Text("显示上次成功数据", "Showing last successful data"),
        LoadKind.Cancelled => Text("已取消", "Cancelled"), LoadKind.RecoveryBlocked => Text("需要恢复数据库", "Database recovery required"),
        _ => Text("暂时无法读取，可重试", "Unavailable. You can retry.")
    };
}
