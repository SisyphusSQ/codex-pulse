using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;

namespace CodexPulse.App.Views;

internal static class ApiSubscriptionCharts
{
    private static SolidColorBrush Blue=>new(ColorHelper.FromArgb(255,42,119,213));
    public static FrameworkElement Balance(APISubscriptionCurrencyBalanceSeries series,Localization l)
    {
        var body=new StackPanel{Spacing=8};body.Children.Add(PresentationControls.Text(series.Currency+" · "+l.Text("余额观察趋势","Observed balance trend"),18));
        var points=series.Points.OrderBy(point=>point.ObservedAtMs).Select(point=>(Point:point,Amount:ApiSubscriptionPresentation.Amount(point.Total))).ToArray();
        if(points.Length==0 || points.Any(point=>point.Amount is null)){body.Children.Add(PresentationControls.Text(l.Text("没有可绘制的余额样本；原始十进制值见表格。","No plottable balance samples; see original decimal values in the table.")));}
        else
        {
            var selected=PresentationControls.Text(l.Text("悬停或聚焦样本查看观察时间和原始余额。","Hover or focus a sample for its timestamp and original balance."),12);
            var plot=new Canvas{Height=195,MinWidth=260,HorizontalAlignment=HorizontalAlignment.Stretch};
            void Draw()
            {
                plot.Children.Clear();var width=Math.Max(plot.ActualWidth,260)-20;var min=points.Min(v=>v.Amount!.Value);var max=points.Max(v=>v.Amount!.Value);var span=max-min;
                double X(int index)=>points.Length==1?width/2:10+width*(points[index].Point.ObservedAtMs-points[0].Point.ObservedAtMs)/(double)Math.Max(1,points[^1].Point.ObservedAtMs-points[0].Point.ObservedAtMs);
                double Y(int index)=>span==0?85:160-140*(double)((points[index].Amount!.Value-min)/span);
                for(var index=0;index<points.Length;index++)
                {
                    if(index>0)
                    {
                        plot.Children.Add(new Line{X1=X(index-1),Y1=Y(index-1),X2=X(index),Y2=Y(index-1),Stroke=Blue,StrokeThickness=2});
                        plot.Children.Add(new Line{X1=X(index),Y1=Y(index-1),X2=X(index),Y2=Y(index),Stroke=Blue,StrokeThickness=2});
                    }
                    var value=points[index].Point;var detail=l.Timestamp(value.ObservedAtMs)+" · "+series.Currency+"\n"+l.Text("总余额 / 赠送 / 充值：","Total / granted / topped up: ")+value.Total+" / "+value.Granted+" / "+value.ToppedUp;
                    var point=new Button{Width=12,Height=12,MinWidth=0,MinHeight=0,Padding=new Thickness(0),Background=Blue,BorderThickness=new Thickness(0),CornerRadius=new CornerRadius(6)};
                    ToolTipService.SetToolTip(point,detail);AutomationProperties.SetName(point,detail);point.PointerEntered+=(_,_)=>selected.Text=detail;point.GotFocus+=(_,_)=>selected.Text=detail;
                    Canvas.SetLeft(point,X(index)-6);Canvas.SetTop(point,Y(index)-6);plot.Children.Add(point);
                }
                var first=PresentationControls.Text(l.Timestamp(points[0].Point.ObservedAtMs),11);Canvas.SetLeft(first,0);Canvas.SetTop(first,173);plot.Children.Add(first);
                if(points.Length>1){var last=PresentationControls.Text(l.Timestamp(points[^1].Point.ObservedAtMs),11);Canvas.SetLeft(last,Math.Max(0,width-120));Canvas.SetTop(last,173);plot.Children.Add(last);}
            }
            plot.SizeChanged+=(_,_)=>Draw();Draw();body.Children.Add(selected);body.Children.Add(plot);
        }
        body.Children.Add(PresentationControls.Disclosure(l.Text("原始采样明细","Original sample data"),PresentationControls.Table([l.Text("时间","Time"),l.Text("总余额","Total"),l.Text("赠送","Granted"),l.Text("充值","Topped up")],series.Points.Select(point=>new[]{l.Timestamp(point.ObservedAtMs),point.Total,point.Granted,point.ToppedUp}))));
        return body;
    }
    public static FrameworkElement Calendar(IReadOnlyList<ApiActivityCell> days,string zone,Localization l)
    {
        var body=new StackPanel{Spacing=10};body.Children.Add(PresentationControls.Text(l.Text("过去 365 天 · API 活动","Last 365 days · API activity"),20));
        body.Children.Add(PresentationControls.Text(zone+" · "+l.Text("DeepSeek 消耗采样估算 / OpenCode Go 5 小时峰值；两来源各按 25%/50%/75% 分位分档，方格取较高档。空框为没有样本，浅色格为已知零。","Sampled DeepSeek consumption / OpenCode Go 5-hour peak. Each source uses its 25th/50th/75th percentiles; a cell uses the higher level. Outlined cells have no samples; pale cells are known zero."),12));
        var selected=PresentationControls.Text(l.Text("悬停或聚焦日期查看两来源详情。","Hover or focus a date for both sources."),12);body.Children.Add(selected);
        if(days.Count==0){body.Children.Add(PresentationControls.Text(l.Text("没有日历数据。","No calendar data.")));return body;}
        var grid=new Grid{ColumnSpacing=3,RowSpacing=3,HorizontalAlignment=HorizontalAlignment.Left};for(var row=0;row<7;row++)grid.RowDefinitions.Add(new(){Height=new GridLength(13)});
        var offset=DateOnly.TryParseExact(days[0].DateKey,"yyyy-MM-dd",out var first)?((int)first.DayOfWeek+6)%7:0;
        for(var column=0;column<(offset+days.Count+6)/7;column++)grid.ColumnDefinitions.Add(new(){Width=new GridLength(13)});
        foreach(var (day,index) in days.Select((day,index)=>(day,index)))
        {
            var level=day.Intensity;var color=level<0?Colors.Transparent:ColorHelper.FromArgb((byte)(level switch{0=>30,1=>75,2=>125,3=>185,_=>255}),42,119,213);
            var cell=new Button{Width=13,Height=13,MinWidth=0,MinHeight=0,Padding=new Thickness(0),CornerRadius=new CornerRadius(2),Background=new SolidColorBrush(color),BorderThickness=new Thickness(level<0?1:0),BorderBrush=Charts.NeutralBrush};
            AutomationProperties.SetName(cell,day.Detail);ToolTipService.SetToolTip(cell,day.Detail);cell.PointerEntered+=(_,_)=>selected.Text=day.Detail;cell.GotFocus+=(_,_)=>selected.Text=day.Detail;Grid.SetColumn(cell,(offset+index)/7);Grid.SetRow(cell,(offset+index)%7);grid.Children.Add(cell);
        }
        body.Children.Add(new ScrollViewer{Content=grid,HorizontalScrollBarVisibility=ScrollBarVisibility.Auto,VerticalScrollBarVisibility=ScrollBarVisibility.Disabled});
        body.Children.Add(PresentationControls.Disclosure(l.Text("每日活动明细","Daily activity data"),PresentationControls.Table([l.Text("日期","Date"),l.Text("两来源采样","Samples from both sources")],days.Reverse().Select(day=>new[]{day.DateKey,day.Detail}))));return body;
    }
}
