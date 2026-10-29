using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using Windows.Foundation;

namespace CodexPulse.App.Views;

internal static class PaceChart
{
    public static FrameworkElement Create(QuotaPaceWindow window,Localization l)
    {
        var body=new StackPanel{Spacing=8};var canvas=new Canvas{Height=218,HorizontalAlignment=HorizontalAlignment.Stretch};
        var blue=new SolidColorBrush(ColorHelper.FromArgb(255,66,145,240));var gray=Charts.NeutralBrush;
        var amber=new SolidColorBrush(ColorHelper.FromArgb(255,211,157,50));
        bool Valid(double elapsed,double remaining)=>QuotaPresentation.Percent(true,elapsed) is not null && QuotaPresentation.Percent(true,remaining) is not null;
        void Draw(double width)
        {
            canvas.Children.Clear();if(width<60)return;
            double X(double percent)=>34+(width-42)*percent/100;
            double Y(double percent)=>180-160*percent/100;
            foreach(var tick in new[]{0,50,100})
            {
                var label=PresentationControls.Text(tick+"%",10);Canvas.SetLeft(label,0);Canvas.SetTop(label,Y(tick)-7);canvas.Children.Add(label);
                canvas.Children.Add(new Line{X1=34,X2=width-8,Y1=Y(tick),Y2=Y(tick),Stroke=gray,StrokeThickness=1});
                var x=PresentationControls.Text(tick+"%",10);Canvas.SetLeft(x,Math.Clamp(X(tick)-12,34,width-34));Canvas.SetTop(x,186);canvas.Children.Add(x);
            }
            void Line(IEnumerable<(double Elapsed,double Remaining)> values,Brush color,double thickness,bool dashed=false)
            {
                var line=new Polyline{Stroke=color,StrokeThickness=thickness,StrokeLineJoin=PenLineJoin.Round,StrokeStartLineCap=PenLineCap.Round,StrokeEndLineCap=PenLineCap.Round};if(dashed)line.StrokeDashArray=new DoubleCollection{3,3};
                foreach(var point in values.Where(point=>Valid(point.Elapsed,point.Remaining)).OrderBy(point=>point.Elapsed))line.Points.Add(new Point(X(point.Elapsed),Y(point.Remaining)));
                if(line.Points.Count>1)canvas.Children.Add(line);
            }
            var band=window.HistoryBand.Where(point=>Valid(point.ElapsedPercent,point.MinimumRemaining) && Valid(point.ElapsedPercent,point.MaximumRemaining)).OrderBy(point=>point.ElapsedPercent).ToArray();
            if(band.Length>1)
            {
                var area=new Polygon{Fill=blue,Opacity=.12};foreach(var point in band)area.Points.Add(new(X(point.ElapsedPercent),Y(point.MaximumRemaining)));foreach(var point in band.Reverse())area.Points.Add(new(X(point.ElapsedPercent),Y(point.MinimumRemaining)));canvas.Children.Add(area);
                Line(band.Select(point=>(point.ElapsedPercent,point.MedianRemaining)),gray,1.5,true);
            }
            foreach(var history in window.HistoricalCycles)Line(history.Points.Select(point=>(point.ElapsedPercent,point.RemainingPercent)),gray,1);
            if(window.PreviousCycle is {} previous)Line(previous.Points.Select(point=>(point.ElapsedPercent,point.RemainingPercent)),amber,2,true);
            Line(new[]{(0d,100d),(100d,0d)},gray,1,true);
            Line(window.CurrentPoints.Select(point=>(point.ElapsedPercent,point.RemainingPercent)),blue,2.5);
            var currentPoints=window.CurrentPoints.Where(point=>Valid(point.ElapsedPercent,point.RemainingPercent)).ToArray();
            if(currentPoints.Length==1)
            {
                // A lone observation cannot form a line; retain a stroke-sized mark.
                var point=currentPoints[0];var mark=new Ellipse{Width=2.5,Height=2.5,Fill=blue};Canvas.SetLeft(mark,X(point.ElapsedPercent)-1.25);Canvas.SetTop(mark,Y(point.RemainingPercent)-1.25);canvas.Children.Add(mark);
            }
            foreach(var point in currentPoints)
            {
                // Keep the precise observation tooltip without painting a dot over the line.
                var target=new Ellipse{Width=14,Height=14,Fill=new SolidColorBrush(Colors.Transparent)};Canvas.SetLeft(target,X(point.ElapsedPercent)-7);Canvas.SetTop(target,Y(point.RemainingPercent)-7);
                var text=l.Timestamp(point.ObservedAtMs)+" · "+QuotaPresentation.PercentText(point.RemainingPercent,l);ToolTipService.SetToolTip(target,text);AutomationProperties.SetName(target,text);canvas.Children.Add(target);
            }
        }
        canvas.SizeChanged+=(_,args)=>Draw(args.NewSize.Width);
        AutomationProperties.SetName(canvas,l.Text("额度节奏：横轴周期已过，纵轴剩余额度。完整数值见节奏数据明细。","Quota pace: elapsed period on the x-axis, remaining quota on the y-axis. Full values are in pace data details."));
        body.Children.Add(canvas);body.Children.Add(PresentationControls.Text(l.Text("蓝线：当前周期；金色虚线：上一周期；浅色区域：历史范围；对角虚线：均匀节奏。横轴为周期已过，纵轴为剩余额度。","Blue: current cycle; gold dashed: previous cycle; shaded: history range; diagonal dashed: even pace. X: elapsed period; Y: remaining quota."),12));
        if(window.CurrentPoints.Count==0)body.Children.Add(PresentationControls.Text(l.Text("当前周期尚无观察点；不补造轨迹。","No current-cycle observations; no trajectory is fabricated.")));
        return body;
    }
}
