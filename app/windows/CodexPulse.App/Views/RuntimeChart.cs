using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using Windows.Foundation;

namespace CodexPulse.App.Views;

internal static class RuntimeChart
{
    public static FrameworkElement Create(IEnumerable<DataHealthRuntimePoint> data,Localization l)
    {
        var points=data.Where(point=>point.CapturedAtMs?.HasValue==true && double.IsFinite(point.CpuPercent) && point.CpuPercent>=0).OrderBy(point=>point.CapturedAtMs.Value).ToArray();
        var body=new StackPanel{Spacing=8};
        if(points.Length==0){body.Children.Add(PresentationControls.Text(l.Text("没有可用资源样本。","No resource samples available.")));return body;}
        var peak=points.Max(point=>point.CpuPercent);body.Children.Add(PresentationControls.Text(l.Text("进程 CPU · 单核 100% · 峰值 ","Process CPU · One core = 100% · Peak ")+peak.ToString("0.###",l.Culture)+"%"));
        var canvas=new Canvas{Height=160,HorizontalAlignment=HorizontalAlignment.Stretch};
        canvas.SizeChanged+=(_,args)=>
        {
            canvas.Children.Clear();var width=args.NewSize.Width;if(width<20)return;
            var start=points[0].CapturedAtMs.Value;var span=Math.Max(1,points[^1].CapturedAtMs.Value-start);var ceiling=Math.Max(1,peak);var brush=new SolidColorBrush(ColorHelper.FromArgb(255,66,145,240));
            var line=new Polyline{Stroke=brush,StrokeThickness=2};long? previous=null;
            foreach(var point in points)
            {
                if(previous is {} last && point.CapturedAtMs.Value-last>65000){if(line.Points.Count>1)canvas.Children.Add(line);line=new Polyline{Stroke=brush,StrokeThickness=2};}
                previous=point.CapturedAtMs.Value;
                var x=6+(width-12)*(point.CapturedAtMs.Value-start)/span;var y=145-130*point.CpuPercent/ceiling;
                line.Points.Add(new Point(x,y));var dot=new Ellipse{Width=5,Height=5,Fill=brush};Canvas.SetLeft(dot,x-2.5);Canvas.SetTop(dot,y-2.5);ToolTipService.SetToolTip(dot,l.Timestamp(point.CapturedAtMs.Value)+" · CPU "+point.CpuPercent.ToString("0.###",l.Culture)+"% · RSS "+RuntimeCopy.Bytes(point.RssBytes,l));canvas.Children.Add(dot);
            }
            canvas.Children.Add(new Line{X1=6,X2=width-6,Y1=145,Y2=145,Stroke=Charts.NeutralBrush,StrokeThickness=1});if(line.Points.Count>1)canvas.Children.Add(line);
        };
        body.Children.Add(canvas);body.Children.Add(PresentationControls.Text(l.Timestamp(points[0].CapturedAtMs.Value)+" — "+l.Timestamp(points[^1].CapturedAtMs.Value),12));
        body.Children.Add(PresentationControls.Text(l.Text("采样缺口保留在精确值表；图表不推断健康或补造缺失样本。资源来自 Helper 进程，未归因到外部 Codex 会话。","Sampling gaps remain in the values table. The chart neither infers health nor fabricates samples. Resources belong to the Helper process and are not attributed to external Codex sessions."),12));return body;
    }
}
