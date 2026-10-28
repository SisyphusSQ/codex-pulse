using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Media;

namespace CodexPulse.App.Views;

internal static class QuotaSummary
{
    public static FrameworkElement Create(CurrentQuota? quota, string provider, Localization l)
    {
        var body=new StackPanel { Spacing=12 };
        if(provider=="dsh") { body.Children.Add(PresentationControls.Text(l.Text("DSH 不提供官方额度。","DSH does not provide an official quota."))); return body; }
        var windows=quota?.Windows.GroupBy(window=>(window.LimitId,Minutes:window.HasWindowMinutes?window.WindowMinutes:(long?)null))
            .Select(group=>group.OrderByDescending(window=>window.HasResetsAtMs).ThenBy(window=>window.Freshness=="fresh"?0:1)
                .ThenByDescending(window=>window.HasLimitName && !string.IsNullOrEmpty(window.LimitName)).ThenBy(window=>window.WindowKind=="primary"?0:1).First()).ToArray()??[];
        if(windows.Length==0) body.Children.Add(PresentationControls.Text(l.Text("尚无可用额度；用量分析仍可独立读取。","No quota is available yet. Usage analysis can load independently.")));
        foreach(var window in windows)
        {
            var name=window.HasLimitName && !string.IsNullOrEmpty(window.LimitName)?window.LimitName:window.LimitId=="codex"?l.Text("通用额度","General quota"):l.Text("模型专属额度","Model quota");
            var duration=window.HasWindowMinutes?(window.WindowMinutes%1440==0?(window.WindowMinutes/1440).ToString(l.Culture)+l.Text(" 天"," days"):window.WindowMinutes%60==0?(window.WindowMinutes/60).ToString(l.Culture)+l.Text(" 小时"," hours"):window.WindowMinutes.ToString(l.Culture)+l.Text(" 分钟"," minutes")):l.Text("周期未知","Unknown period");
            var known=window.HasRemainingPercent && double.IsFinite(window.RemainingPercent) && window.RemainingPercent is >=0 and <=100;
            var remaining=known?window.RemainingPercent.ToString("N0",l.Culture)+"%":"—";
            var row=new StackPanel { Spacing=5 };
            row.Children.Add(PresentationControls.Text(name+" · "+duration+" · "+l.Text("剩余 ","Remaining ")+remaining,16));
            var progress=new ProgressBar { Minimum=0,Maximum=100,Value=known?window.RemainingPercent:0, Height=6, Foreground=known?new SolidColorBrush(window.RemainingPercent>40?ColorHelper.FromArgb(255,44,157,89):window.RemainingPercent>20?ColorHelper.FromArgb(255,184,135,13):ColorHelper.FromArgb(255,210,66,66)):Charts.NeutralBrush };
            AutomationProperties.SetName(progress,name+" · "+remaining+" · "+(known?window.RemainingPercent>40?l.Text("正常","Normal"):window.RemainingPercent>20?l.Text("需要关注","Needs attention"):l.Text("需要处理","Action required"):l.Text("暂不可用","Unavailable")));
            row.Children.Add(progress);
            var freshness=window.Freshness=="fresh" || !known?"":l.Text(" · 上次可信额度"," · Last known quota");
            row.Children.Add(PresentationControls.Text(l.Text("重置时间 ","Resets ")+l.Timestamp(window.HasResetsAtMs?window.ResetsAtMs:null)+freshness,12));
            body.Children.Add(row);
        }
        return body;
    }
}
