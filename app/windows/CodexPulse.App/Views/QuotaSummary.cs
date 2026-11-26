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
    public static FrameworkElement Create(CurrentQuota? quota, string provider, Localization l, bool compact=false)
    {
        var body=new StackPanel { Spacing=12 };
        if(provider=="dsh") { body.Children.Add(PresentationControls.Text(l.Text("DSH 不提供官方额度。","DSH does not provide an official quota."))); return body; }
        if(provider=="agy" && quota is not null)
        {
            var pools=QuotaPresentation.AgyPools(quota,l);
            if(pools.Count==0)
            {
                body.Children.Add(PresentationControls.Text(l.Text("尚无可用额度；用量分析仍可独立读取。","No quota is available yet. Usage analysis can load independently.")));
                return body;
            }
            var poolCards=new List<UIElement>();
            foreach(var pool in pools)
            {
                var poolBox=new StackPanel { Spacing=6, Margin=new Thickness(0,2,0,2) };
                var title=PresentationControls.Text(pool.DisplayName,compact?13:15);
                title.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold;
                poolBox.Children.Add(title);
                var pair=new Grid { ColumnSpacing=12, RowSpacing=6 };
                var slots=new List<(string Label,CurrentWindow Window)>();
                if(pool.Primary is {} primary) slots.Add((QuotaPresentation.WindowShortLabel(primary.WindowKind,primary.HasWindowMinutes?primary.WindowMinutes:null,l),primary));
                if(pool.Secondary is {} secondary) slots.Add((QuotaPresentation.WindowShortLabel(secondary.WindowKind,secondary.HasWindowMinutes?secondary.WindowMinutes:null,l),secondary));
                for(var i=0;i<slots.Count;i++) pair.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
                for(var i=0;i<slots.Count;i++)
                {
                    var (slotLabel,window)=slots[i];
                    var expired=QuotaPresentation.IsExpired(window,quota.EvaluatedAtMs);
                    var value=QuotaPresentation.EffectiveRemaining(window,quota.EvaluatedAtMs);
                    var known=value is not null;
                    var remaining=QuotaPresentation.PercentText(value,l);
                    var cell=new StackPanel { Spacing=4 };
                    var heading=new Grid { ColumnSpacing=8 };
                    heading.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
                    heading.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
                    var cap=PresentationControls.Caption(slotLabel);cap.VerticalAlignment=VerticalAlignment.Center;
                    var num=PresentationControls.Text(remaining,compact?16:20);num.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold;
                    Grid.SetColumn(num,1);heading.Children.Add(cap);heading.Children.Add(num);cell.Children.Add(heading);
                    if(known)
                    {
                        var progress=new ProgressBar { Minimum=0,Maximum=100,Value=value!.Value,Height=5,Foreground=new SolidColorBrush(value.Value>40?ColorHelper.FromArgb(255,44,157,89):value.Value>20?ColorHelper.FromArgb(255,184,135,13):ColorHelper.FromArgb(255,210,66,66)) };
                        AutomationProperties.SetName(progress,pool.DisplayName+" · "+slotLabel+" · "+remaining);
                        cell.Children.Add(progress);
                    }
                    var freshness=window.Freshness is "fresh" or "current" || !known?"":" · "+QuotaPresentation.Freshness(window.Freshness,l);
                    cell.Children.Add(PresentationControls.Caption(expired?l.Text("等待确认","Awaiting reset"):l.Text("重置 ","Resets ")+l.Timestamp(window.HasResetsAtMs?window.ResetsAtMs:null)+freshness));
                    Grid.SetColumn(cell,i);pair.Children.Add(cell);
                }
                poolBox.Children.Add(pair);
                poolCards.Add(poolBox);
            }
            var columns=PresentationControls.Columns(poolCards,compact?240:280);
            body.Children.Add(columns);
            return body;
        }
        var windows=quota is null?[]:QuotaPresentation.Windows(quota);
        if(windows.Count==0) body.Children.Add(PresentationControls.Text(l.Text("尚无可用额度；用量分析仍可独立读取。","No quota is available yet. Usage analysis can load independently.")));
        var summaries=new List<UIElement>();
        foreach(var window in windows)
        {
            var name=QuotaPresentation.WindowName(window.LimitId,window.HasLimitName?window.LimitName:null,window.HasWindowMinutes?window.WindowMinutes:null,l);
            var expired=window.HasResetsAtMs && window.ResetsAtMs<=quota!.EvaluatedAtMs;
            var value=expired?null:QuotaPresentation.Percent(window.HasRemainingPercent,window.RemainingPercent);
            var known=value is not null;
            var remaining=QuotaPresentation.PercentText(value,l);
            var row=new StackPanel { Spacing=6, Margin=new Thickness(0,2,0,2) };
            var title=PresentationControls.Caption(name);
            var valueRow=new StackPanel { Orientation=Orientation.Horizontal, Spacing=8 };
            var number=PresentationControls.Text(remaining,compact?18:24);number.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold;
            valueRow.Children.Add(number);
            var label=PresentationControls.Caption(l.Text("剩余","remaining"));label.VerticalAlignment=VerticalAlignment.Bottom;label.Margin=new Thickness(0,0,0,5);
            valueRow.Children.Add(label);
            if(compact)
            {
                var heading=new Grid {ColumnSpacing=12};heading.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});heading.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
                title.VerticalAlignment=VerticalAlignment.Center;heading.Children.Add(title);Grid.SetColumn(valueRow,1);heading.Children.Add(valueRow);row.Children.Add(heading);
            }
            else {row.Children.Add(title);row.Children.Add(valueRow);}
            if(known)
            {
                var progress=new ProgressBar { Minimum=0,Maximum=100,Value=window.RemainingPercent,Height=6,Foreground=new SolidColorBrush(window.RemainingPercent>40?ColorHelper.FromArgb(255,44,157,89):window.RemainingPercent>20?ColorHelper.FromArgb(255,184,135,13):ColorHelper.FromArgb(255,210,66,66)) };
                AutomationProperties.SetName(progress,name+" · "+remaining+" · "+(window.RemainingPercent>40?l.Text("正常","Normal"):window.RemainingPercent>20?l.Text("需要关注","Needs attention"):l.Text("需要处理","Action required")));
                row.Children.Add(progress);
            }
            var freshness=window.Freshness is "fresh" or "current" || !known?"":" · "+QuotaPresentation.Freshness(window.Freshness,l);
            row.Children.Add(PresentationControls.Caption(expired?l.Text("周期已结束，等待确认","Period ended; awaiting confirmation"):l.Text("重置时间 ","Resets ")+l.Timestamp(window.HasResetsAtMs?window.ResetsAtMs:null)+freshness));
            summaries.Add(row);
        }
        if(summaries.Count>0)
        {
            var columns=PresentationControls.Columns(summaries,220);
            columns.Width=summaries.Count*300;columns.HorizontalAlignment=HorizontalAlignment.Left;
            body.SizeChanged+=(_,args)=>columns.Width=Math.Min(summaries.Count*300,args.NewSize.Width);
            body.Children.Add(columns);
        }
        return body;
    }
}
