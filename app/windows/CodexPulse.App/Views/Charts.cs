using CodexPulse.AppSupport;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using Windows.Foundation;

namespace CodexPulse.App.Views;

// Native vector marks with exact-value tooltips and an accessible data table.
// Floating point is used only to place pixels, never to calculate business facts.
internal static class Charts
{
    public static Brush NeutralBrush => new SolidColorBrush(ColorHelper.FromArgb(65,128,128,128));
    private static readonly Windows.UI.Color[] colors = [ColorHelper.FromArgb(255,42,119,213),ColorHelper.FromArgb(255,24,154,136),ColorHelper.FromArgb(255,171,112,208),ColorHelper.FromArgb(255,213,143,45),ColorHelper.FromArgb(255,212,96,126),ColorHelper.FromArgb(255,98,139,166)];
    private static Brush Color(int index) => new SolidColorBrush(colors[index%colors.Length]);
    private static void Tip(DependencyObject element, string text) { ToolTipService.SetToolTip(element,text); ToolTipService.SetPlacement(element,Microsoft.UI.Xaml.Controls.Primitives.PlacementMode.Mouse); AutomationProperties.SetName(element,text); }
    public static FrameworkElement Bars(IReadOnlyList<ChartDatum> points, Localization l, string unit, bool breakdownExpected=false)
    {
        var body = new StackPanel { Spacing=8 };
        var plot = new Canvas { Height=190, HorizontalAlignment=HorizontalAlignment.Stretch, MinWidth=280 };
        void Draw()
        {
            plot.Children.Clear();
            if(points.Count==0) return;
            var width=Math.Max(280,plot.ActualWidth); var pitch=width/points.Count;
            var maximum=points.Max(point=>point.Value??0);
            var keys=points.SelectMany(point=>point.Segments).Select(segment=>segment.Key).Distinct().ToArray();
            for(var index=0;index<points.Count;index++)
            {
                var point=points[index]; var x=index*pitch;
                var height=maximum>0 && point.Value is {} value ? 160d*(double)((decimal)value/maximum) : 0;
                var tooltip=point.Label+" · "+(point.Value?.ToString("N0",l.Culture)??"—")+" "+unit;
                if(point.Detail is not null) tooltip+="\n"+point.Detail;
                if(point.Segments.Count>0) tooltip+="\n"+string.Join("\n",point.Segments.Select(segment=>segment.Label+" · "+segment.Value.ToString("N0",l.Culture)+" · "+(point.Value>0?((decimal)segment.Value/point.Value.Value*100).ToString("N1",l.Culture):"0")+"%"));
                var mark=new Grid { Width=Math.Max(1,pitch-2), Height=165, Background=new SolidColorBrush(Colors.Transparent) };
                Tip(mark,tooltip); Canvas.SetLeft(mark,x); plot.Children.Add(mark);
                if(point.Value is null)
                    mark.Children.Add(new Border { Height=8, Background=NeutralBrush, VerticalAlignment=VerticalAlignment.Bottom, CornerRadius=new CornerRadius(2) });
                else if(point.Segments.Count>0 && point.Value>0)
                {
                    var stack=new StackPanel { Height=height, VerticalAlignment=VerticalAlignment.Bottom };
                    foreach(var segment in point.Segments.Where(segment=>segment.Value>0))
                        stack.Children.Add(new Border { Height=height*(double)((decimal)segment.Value/point.Value.Value), Background=Color(Array.IndexOf(keys,segment.Key)) });
                    mark.Children.Add(stack);
                }
                else mark.Children.Add(new Border { Height=height, Background=Color(0), VerticalAlignment=VerticalAlignment.Bottom, CornerRadius=new CornerRadius(2) });
                if(index==0 || index==points.Count-1 || points.Count<8 || index%Math.Max(1,points.Count/5)==0)
                {
                    var label=new TextBlock { Text=point.AxisLabel??DashboardPresentation.BucketLabel(point.Label), FontSize=11, Opacity=.7, MaxWidth=Math.Max(40,pitch), TextTrimming=TextTrimming.CharacterEllipsis };
                    Canvas.SetLeft(label,x); Canvas.SetTop(label,172); plot.Children.Add(label);
                }
            }
        }
        plot.SizeChanged+=(_,_)=>Draw(); Draw();
        body.Children.Add(PresentationControls.Text(unit+" · "+l.Text("峰值 ","Peak ")+(points.Any(point=>point.Value is not null)?points.Max(point=>point.Value??0).ToString("N0",l.Culture):"—"),12));
        body.Children.Add(plot);
        if(breakdownExpected && points.Any(point=>point.Value>0 && point.Segments.Count==0)) body.Children.Add(PresentationControls.Text(l.Text("部分分项无法与总量对账，仅展示已知总量。","Some breakdowns do not reconcile. Only known totals are shown."),12));
        body.Children.Add(PresentationControls.Disclosure(l.Text("图表明细","Chart data"),PresentationControls.Table([l.Text("时间","Time"),unit,l.Text("构成","Breakdown")],points.Select(point=>new[]{point.Label,point.Value?.ToString("N0",l.Culture)??"—",string.Join(" · ",point.Segments.Select(segment=>segment.Label+" "+segment.Value.ToString("N0",l.Culture)))}))));
        return body;
    }

    public static FrameworkElement Calendar(IReadOnlyList<CalendarDay> days, Localization l)
    {
        var grid=new Grid { ColumnSpacing=3, RowSpacing=3, HorizontalAlignment=HorizontalAlignment.Left };
        for(var row=0;row<7;row++) grid.RowDefinitions.Add(new() { Height=new GridLength(12) });
        var offset=days.Count>0?((int)days[0].Date.DayOfWeek+6)%7:0;
        for(var column=0;column<(offset+days.Count+6)/7;column++) grid.ColumnDefinitions.Add(new() { Width=new GridLength(12) });
        var peak=days.Select(day=>day.Tokens??0).DefaultIfEmpty().Max();
        for(var i=0;i<days.Count;i++)
        {
            var day=days[i]; var cell=new Border { Width=12, Height=12, CornerRadius=new CornerRadius(2), BorderThickness=day.Tokens is null?new Thickness(1):new Thickness(0), BorderBrush=NeutralBrush,
                Background=day.Tokens is null?null:day.Tokens==0?NeutralBrush:new SolidColorBrush(colors[0]), Opacity=day.Tokens is >0 && peak>0?.3+.7*Math.Pow((double)((decimal)day.Tokens.Value/peak),.35):1 };
            Tip(cell,day.Date.ToString("yyyy-MM-dd")+" · "+(day.Tokens?.ToString("N0",l.Culture)??l.Text("未知","Unknown"))+" Token · "+(day.Turns?.ToString("N0",l.Culture)??"—")+l.Text(" 轮"," turns"));
            Grid.SetColumn(cell,(offset+i)/7); Grid.SetRow(cell,(offset+i)%7); grid.Children.Add(cell);
        }
        var body=new StackPanel { Spacing=10 };
        if(days.Count>0) body.Children.Add(PresentationControls.Text(days[0].Date.ToString("yyyy-MM-dd")+" — "+days[^1].Date.ToString("yyyy-MM-dd")+" · "+l.Text("空框：未知；灰格：已知零","Outlined: unknown; gray: known zero"),12));
        body.Children.Add(new ScrollViewer { Content=grid, HorizontalScrollBarVisibility=ScrollBarVisibility.Auto, VerticalScrollBarVisibility=ScrollBarVisibility.Disabled });
        body.Children.Add(PresentationControls.Disclosure(l.Text("每日活动明细","Daily activity data"),PresentationControls.Table([l.Text("日期","Date"),"Token",l.Text("轮数","Turns")],days.Reverse().Select(day=>new[]{day.Date.ToString("yyyy-MM-dd"),day.Tokens?.ToString("N0",l.Culture)??"—",day.Turns?.ToString("N0",l.Culture)??"—"}))));
        return body;
    }

    public static FrameworkElement Distribution(IReadOnlyList<ChartSegment> segments, long? expectedTotal, Localization l, bool cost)
    {
        var body=new StackPanel { Spacing=12 };
        var verified=DashboardPresentation.Reconciled(expectedTotal,segments);
        if(expectedTotal is null || expectedTotal>0 && verified.Count==0) { body.Children.Add(PresentationControls.Text(l.Text("构成未知或无法对账。","Breakdown is unknown or does not reconcile."))); return body; }
        var row=new Grid { ColumnSpacing=20 };
        row.ColumnDefinitions.Add(new(){Width=GridLength.Auto}); row.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
        var canvas=new Canvas { Width=170,Height=170 };
        var legend=new StackPanel { Spacing=6 };
        double angle=-Math.PI/2; var index=0;
        foreach(var item in verified.Where(item=>item.Value>0))
        {
            var fraction=expectedTotal>0?(double)((decimal)item.Value/expectedTotal.Value):0;
            var next=angle+fraction*Math.PI*2;
            var tip=item.Label+" · "+(cost?l.Money(new(){Value=item.Value}):item.Value.ToString("N0",l.Culture)+" Token")+" · "+(fraction*100).ToString("N1",l.Culture)+"%";
            var brush=Color(index++);
            if(fraction>=.999999999999) { var ellipse=new Ellipse{Width=140,Height=140,Stroke=brush,StrokeThickness=24}; Canvas.SetLeft(ellipse,15);Canvas.SetTop(ellipse,15);Tip(ellipse,tip);canvas.Children.Add(ellipse); }
            else
            {
                var figure=new PathFigure { StartPoint=new Point(85+58*Math.Cos(angle),85+58*Math.Sin(angle)), IsClosed=false };
                figure.Segments.Add(new ArcSegment { Point=new Point(85+58*Math.Cos(next),85+58*Math.Sin(next)), Size=new Size(58,58), IsLargeArc=fraction>.5, SweepDirection=SweepDirection.Clockwise });
                var geometry=new PathGeometry(); geometry.Figures.Add(figure);
                var path=new Microsoft.UI.Xaml.Shapes.Path { Data=geometry,Stroke=brush,StrokeThickness=24 };
                Tip(path,tip);canvas.Children.Add(path);
            }
            var text=PresentationControls.Text(tip,13); Tip(text,tip); legend.Children.Add(text); angle=next;
        }
        var center=PresentationControls.Text(cost?l.Money(expectedTotal is {} total?new(){Value=total}:null):expectedTotal?.ToString("N0",l.Culture)??"—",14); center.Width=104;center.TextAlignment=TextAlignment.Center;Canvas.SetLeft(center,33);Canvas.SetTop(center,72);canvas.Children.Add(center);
        row.Children.Add(canvas);Grid.SetColumn(legend,1);row.Children.Add(legend);
        row.SizeChanged+=(_,args)=>{var narrow=args.NewSize.Width<480; row.ColumnDefinitions[0].Width=narrow?new GridLength(1,GridUnitType.Star):GridLength.Auto; if(row.RowDefinitions.Count==0){row.RowDefinitions.Add(new(){Height=GridLength.Auto});row.RowDefinitions.Add(new(){Height=GridLength.Auto});} Grid.SetColumn(legend,narrow?0:1);Grid.SetRow(legend,narrow?1:0);};
        body.Children.Add(row);
        // Thick horizontal composition, using the same reconciled facts as the donut.
        var bar=new Grid { Height=28 };
        index=0; foreach(var item in verified.Where(item=>item.Value>0))
        { bar.ColumnDefinitions.Add(new(){Width=new GridLength(Math.Max(.000000001,(double)((decimal)item.Value/Math.Max(1,expectedTotal!.Value))),GridUnitType.Star)}); var part=new Border{Background=Color(index),Margin=new Thickness(0,0,1,0)};Tip(part,item.Label+" · "+item.Value.ToString("N0",l.Culture));Grid.SetColumn(part,index++);bar.Children.Add(part); }
        body.Children.Add(bar); return body;
    }
}
