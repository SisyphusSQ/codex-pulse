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
    // Provider colors stay stable when a date range adds or removes a client.
    private static Brush Color(string key,int fallback) => Color(key switch {"codex"=>0,"cursor"=>3,"grok"=>2,"dsh"=>1,_=>fallback});
    private static void Tip(DependencyObject element, string text) { ToolTipService.SetToolTip(element,text); ToolTipService.SetPlacement(element,Microsoft.UI.Xaml.Controls.Primitives.PlacementMode.Mouse); AutomationProperties.SetName(element,text); }
    public static FrameworkElement Bars(IReadOnlyList<ChartDatum> points, Localization l, string unit, bool breakdownExpected=false, bool continuous=false, bool showData=true, bool compact=false, bool showLegend=false)
    {
        var body = new StackPanel { Spacing=8 };
        var plot = new Canvas { Height=compact?142:190, HorizontalAlignment=HorizontalAlignment.Stretch, MinWidth=280 };
        var chartHeight=plot.Height-30;var baseline=chartHeight+5;
        var segments=points.SelectMany(point=>point.Segments).DistinctBy(segment=>segment.Key).ToArray();
        var keys=segments.Select(segment=>segment.Key).ToArray();
        void Draw()
        {
            plot.Children.Clear();
            if(points.Count==0) return;
            var width=Math.Max(280,plot.ActualWidth);const double axisWidth=54;var plotWidth=width-axisWidth;var pitch=plotWidth/points.Count;
            var maximum=points.Max(point=>point.Value??0);
            for(var line=0;line<=4;line++)
            {
                var rule=new Border {Width=plotWidth,Height=1,Background=NeutralBrush,Opacity=.35};
                Canvas.SetLeft(rule,axisWidth);Canvas.SetTop(rule,5+line*chartHeight/4);plot.Children.Add(rule);
                if(maximum==0 && line!=4)continue;
                var axisValue=(long)((decimal)maximum*(4-line)/4);
                var axisLabel=PresentationControls.Caption(unit=="Token"?l.Tokens(axisValue,true):axisValue.ToString("N0",l.Culture));
                axisLabel.FontSize=10;axisLabel.Width=46;axisLabel.TextAlignment=TextAlignment.Right;
                Canvas.SetTop(axisLabel,Math.Max(0,line*chartHeight/4-2));plot.Children.Add(axisLabel);
            }
            if(continuous)
            {
                var run=new List<Point>();
                void AddRun()
                {
                    if(run.Count<2){run.Clear();return;}
                    var area=new Polygon {Fill=Color(0),Opacity=.12};
                    area.Points.Add(new(run[0].X,baseline));foreach(var point in run)area.Points.Add(point);area.Points.Add(new(run[^1].X,baseline));plot.Children.Add(area);
                    var line=new Polyline {Stroke=Color(0),StrokeThickness=2,StrokeLineJoin=PenLineJoin.Round};
                    foreach(var point in run)line.Points.Add(point);plot.Children.Add(line);run.Clear();
                }
                for(var index=0;index<points.Count;index++)
                {
                    if(points[index].Value is not {} value){AddRun();continue;}
                    run.Add(new(axisWidth+(index+.5)*pitch,baseline-(maximum>0?chartHeight*(double)((decimal)value/maximum):0)));
                }
                AddRun();
            }
            for(var index=0;index<points.Count;index++)
            {
                var point=points[index]; var x=axisWidth+index*pitch;
                var height=maximum>0 && point.Value is {} value ? chartHeight*(double)((decimal)value/maximum) : 0;
                var tooltip=point.Label+" · "+(point.Value?.ToString("N0",l.Culture)??"—")+" "+unit;
                if(point.Detail is not null) tooltip+="\n"+point.Detail;
                if(point.Segments.Count>0) tooltip+="\n"+string.Join("\n",point.Segments.Select(segment=>segment.Label+" · "+segment.Value.ToString("N0",l.Culture)+" · "+(point.Value>0?((decimal)segment.Value/point.Value.Value*100).ToString("N1",l.Culture):"0")+"%"));
                var barWidth=Math.Max(1,Math.Min(48,pitch-4));
                var mark=new Grid { Width=barWidth, Height=baseline, Background=new SolidColorBrush(Colors.Transparent) };
                Tip(mark,tooltip); Canvas.SetLeft(mark,x+(pitch-barWidth)/2); plot.Children.Add(mark);
                if(point.Value is null)
                    mark.Children.Add(new Border { Height=8, Background=NeutralBrush, VerticalAlignment=VerticalAlignment.Bottom, CornerRadius=new CornerRadius(2) });
                else if(continuous)
                    mark.Children.Add(new Ellipse {Width=6,Height=6,Fill=Color(0),VerticalAlignment=VerticalAlignment.Top,HorizontalAlignment=HorizontalAlignment.Center,Margin=new Thickness(0,baseline-3-height,0,0)});
                else if(point.Segments.Count>0 && point.Value>0)
                {
                    var stack=new StackPanel { Height=height, VerticalAlignment=VerticalAlignment.Bottom };
                    foreach(var segment in point.Segments.Where(segment=>segment.Value>0))
                        stack.Children.Add(new Border { Height=height*(double)((decimal)segment.Value/point.Value.Value), Background=Color(segment.Key,Array.IndexOf(keys,segment.Key)) });
                    mark.Children.Add(stack);
                }
                else mark.Children.Add(new Border { Height=height, Background=showLegend?NeutralBrush:Color(0), VerticalAlignment=VerticalAlignment.Bottom, CornerRadius=new CornerRadius(2) });
                if(index==0 || index==points.Count-1 || points.Count<8 || index%Math.Max(1,points.Count/5)==0)
                {
                    var label=new TextBlock { Text=point.AxisLabel??DashboardPresentation.BucketLabel(point.Label), FontSize=11, Opacity=.7, Width=52,TextAlignment=TextAlignment.Center, TextTrimming=TextTrimming.CharacterEllipsis };
                    Canvas.SetLeft(label,Math.Clamp(x+pitch/2-26,axisWidth,width-52)); Canvas.SetTop(label,baseline+7); plot.Children.Add(label);
                }
            }
        }
        plot.SizeChanged+=(_,_)=>Draw(); Draw();
        var peak=points.Any(point=>point.Value is not null)?points.Max(point=>point.Value??0):(long?)null;
        body.Children.Add(PresentationControls.Caption(unit+" · "+l.Text("峰值 ","Peak ")+(unit=="Token"?l.Tokens(peak):peak?.ToString("N0",l.Culture)??"—")));
        if(showLegend && segments.Length>0)
        {
            var legend=PresentationControls.Caption("");
            for(var index=0;index<segments.Length;index++)
            {
                legend.Inlines.Add(new Microsoft.UI.Xaml.Documents.Run {Text="● ",Foreground=Color(segments[index].Key,index)});
                legend.Inlines.Add(new Microsoft.UI.Xaml.Documents.Run {Text=segments[index].Label+"    "});
            }
            body.Children.Add(legend);
        }
        body.Children.Add(plot);
        if(breakdownExpected && points.Any(point=>point.Value>0 && point.Segments.Count==0)) body.Children.Add(PresentationControls.Text(l.Text("部分分项无法与总量对账，仅展示已知总量。","Some breakdowns do not reconcile. Only known totals are shown."),12));
        if(showData)body.Children.Add(PresentationControls.DataButton(l.Text("图表明细","Chart data"),()=>PresentationControls.Table([l.Text("时间","Time"),unit,l.Text("构成","Breakdown")],points.Select(point=>new[]{point.Label,point.Value?.ToString("N0",l.Culture)??"—",string.Join(" · ",point.Segments.Select(segment=>segment.Label+" "+segment.Value.ToString("N0",l.Culture)))}))));
        return body;
    }

    public static FrameworkElement Calendar(IReadOnlyList<CalendarDay> days, Localization l)
    {
        var grid=new Grid { ColumnSpacing=3, RowSpacing=3, HorizontalAlignment=HorizontalAlignment.Left };
        grid.RowDefinitions.Add(new(){Height=new GridLength(22)});
        for(var row=0;row<7;row++) grid.RowDefinitions.Add(new() { Height=new GridLength(12) });
        var offset=days.Count>0?((int)days[0].Date.DayOfWeek+6)%7:0;
        var weeks=Math.Max(1,(offset+days.Count+6)/7);
        grid.ColumnDefinitions.Add(new(){Width=new GridLength(28)});
        for(var column=0;column<weeks;column++) grid.ColumnDefinitions.Add(new() { Width=new GridLength(12) });
        foreach(var weekday in new[]{0,2,4})
        {
            var label=PresentationControls.Caption(l.Culture.DateTimeFormat.GetAbbreviatedDayName((DayOfWeek)(weekday+1)));
            label.FontSize=10;label.LineHeight=12;label.VerticalAlignment=VerticalAlignment.Top;
            Grid.SetRow(label,weekday+1);Grid.SetRowSpan(label,2);grid.Children.Add(label);
        }
        var peak=days.Select(day=>day.Tokens??0).DefaultIfEmpty().Max();
        var lastMonthColumn=-4;
        for(var i=0;i<days.Count;i++)
        {
            var day=days[i]; var column=(offset+i)/7+1;
            if((i==0 || day.Date.Day==1) && column-lastMonthColumn>=4)
            {
                var month=PresentationControls.Caption(day.Date.ToString("MMM",l.Culture));month.FontSize=11;
                Grid.SetColumn(month,column);Grid.SetColumnSpan(month,Math.Min(4,weeks-column+1));grid.Children.Add(month);lastMonthColumn=column;
            }
            var cell=new Border { Style=(Style)Application.Current.Resources["PulseCalendarCellStyle"], BorderThickness=day.Tokens is null?new Thickness(1):new Thickness(0),
                Background=day.Tokens is null?null:day.Tokens==0?NeutralBrush:new SolidColorBrush(colors[0]), Opacity=day.Tokens is >0 && peak>0?.3+.7*Math.Pow((double)((decimal)day.Tokens.Value/peak),.35):1 };
            Tip(cell,day.Date.ToString("yyyy-MM-dd")+" · "+(day.Tokens?.ToString("N0",l.Culture)??l.Text("未知","Unknown"))+" Token · "+(day.Turns?.ToString("N0",l.Culture)??"—")+l.Text(" 轮"," turns"));
            Grid.SetColumn(cell,column); Grid.SetRow(cell,(offset+i)%7+1); grid.Children.Add(cell);
        }
        var calendarScroll=new ScrollViewer { Content=grid, HorizontalContentAlignment=HorizontalAlignment.Left, HorizontalScrollBarVisibility=ScrollBarVisibility.Auto, VerticalScrollBarVisibility=ScrollBarVisibility.Disabled };
        calendarScroll.SizeChanged+=(_,args)=>
        {
            var size=Math.Clamp(Math.Floor((args.NewSize.Width-28-weeks*3)/weeks),6,24);
            for(var column=1;column<=weeks;column++)grid.ColumnDefinitions[column].Width=new GridLength(size);
            for(var row=1;row<8;row++)grid.RowDefinitions[row].Height=new GridLength(size);
        };
        var body=new StackPanel { Spacing=10 };
        var header=new Grid {ColumnSpacing=16};
        header.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
        header.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
        if(days.Count>0)
        {
            var scope=PresentationControls.Caption(days[0].Date.ToString("yyyy-MM-dd")+" — "+days[^1].Date.ToString("yyyy-MM-dd")+" · "+l.Text("空框：未知；灰格：已知零","Outlined: unknown; gray: known zero"));
            scope.VerticalAlignment=VerticalAlignment.Center;header.Children.Add(scope);
        }
        var data=PresentationControls.DataButton(l.Text("每日明细","Daily data"),()=>PresentationControls.Table([l.Text("日期","Date"),"Token",l.Text("轮数","Turns")],days.Reverse().Select(day=>new[]{day.Date.ToString("yyyy-MM-dd"),day.Tokens?.ToString("N0",l.Culture)??"—",day.Turns?.ToString("N0",l.Culture)??"—"})));
        Grid.SetColumn(data,1);header.Children.Add(data);body.Children.Add(header);
        body.Children.Add(calendarScroll);
        return body;
    }

    public static FrameworkElement Distribution(IReadOnlyList<ChartSegment> segments, long? expectedTotal, Localization l, bool cost)
    {
        var body=new StackPanel { Spacing=12 };
        var verified=DashboardPresentation.Reconciled(expectedTotal,segments);
        if(expectedTotal is null || expectedTotal>0 && verified.Count==0) { body.Children.Add(PresentationControls.Text(l.Text("构成未知或无法对账。","Breakdown is unknown or does not reconcile."))); return body; }
        var row=new Grid { ColumnSpacing=20,RowSpacing=12 };
        row.ColumnDefinitions.Add(new(){Width=GridLength.Auto}); row.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
        var canvas=new Canvas { Width=170,Height=170 };
        var legend=new StackPanel { Spacing=6 };
        double angle=-Math.PI/2; var index=0;
        foreach(var item in verified.Where(item=>item.Value>0))
        {
            var fraction=expectedTotal>0?(double)((decimal)item.Value/expectedTotal.Value):0;
            var next=angle+fraction*Math.PI*2;
            var tip=item.Label+" · "+(cost?l.Money(new(){Value=item.Value}):item.Value.ToString("N0",l.Culture)+" Token")+" · "+(fraction*100).ToString("N1",l.Culture)+"%";
            var brush=Color(item.Key,index++);
            if(fraction>=.999999999999) { var ellipse=new Ellipse{Width=140,Height=140,Stroke=brush,StrokeThickness=24}; Canvas.SetLeft(ellipse,15);Canvas.SetTop(ellipse,15);Tip(ellipse,tip);canvas.Children.Add(ellipse); }
            else
            {
                var figure=new PathFigure { StartPoint=new Point(85+58*Math.Cos(angle),85+58*Math.Sin(angle)), IsClosed=false };
                figure.Segments.Add(new ArcSegment { Point=new Point(85+58*Math.Cos(next),85+58*Math.Sin(next)), Size=new Size(58,58), IsLargeArc=fraction>.5, SweepDirection=SweepDirection.Clockwise });
                var geometry=new PathGeometry(); geometry.Figures.Add(figure);
                var path=new Microsoft.UI.Xaml.Shapes.Path { Data=geometry,Stroke=brush,StrokeThickness=24 };
                Tip(path,tip);canvas.Children.Add(path);
            }
            var entry=new Grid {ColumnSpacing=8,Padding=new Thickness(0,4,0,4)};
            entry.ColumnDefinitions.Add(new(){Width=GridLength.Auto});entry.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});entry.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
            var swatch=new Ellipse {Width=8,Height=8,Fill=brush,VerticalAlignment=VerticalAlignment.Top,Margin=new Thickness(0,6,0,0)};entry.Children.Add(swatch);
            var description=new StackPanel {Spacing=3};description.Children.Add(PresentationControls.Text(item.Label,13));description.Children.Add(PresentationControls.Caption(cost?l.Money(new(){Value=item.Value}):l.Tokens(item.Value)+" Token"));Grid.SetColumn(description,1);entry.Children.Add(description);
            var percentage=PresentationControls.Caption((fraction*100).ToString("N1",l.Culture)+"%");Grid.SetColumn(percentage,2);entry.Children.Add(percentage);Tip(entry,tip);legend.Children.Add(entry);angle=next;
        }
        var center=PresentationControls.Text(cost?l.Money(expectedTotal is {} total?new(){Value=total}:null):l.Tokens(expectedTotal),14); center.Width=104;center.TextAlignment=TextAlignment.Center;Canvas.SetLeft(center,33);Canvas.SetTop(center,72);canvas.Children.Add(center);
        row.Children.Add(canvas);Grid.SetColumn(legend,1);row.Children.Add(legend);
        row.SizeChanged+=(_,args)=>
        {
            var narrow=args.NewSize.Width<400;
            row.ColumnDefinitions[0].Width=narrow?new GridLength(1,GridUnitType.Star):GridLength.Auto;
            row.ColumnDefinitions[1].Width=narrow?new GridLength(0):new GridLength(1,GridUnitType.Star);
            if(row.RowDefinitions.Count==0){row.RowDefinitions.Add(new(){Height=GridLength.Auto});row.RowDefinitions.Add(new(){Height=GridLength.Auto});}
            canvas.HorizontalAlignment=narrow?HorizontalAlignment.Center:HorizontalAlignment.Left;
            Grid.SetColumn(legend,narrow?0:1);Grid.SetRow(legend,narrow?1:0);
        };
        body.Children.Add(row);
        return body;
    }
}
