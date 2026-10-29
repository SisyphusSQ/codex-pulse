using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Automation.Peers;

namespace CodexPulse.App.Views;

// A section retains its own successful view during background refresh. A failure
// in annual activity, for example, cannot erase current-range usage or quotas.
internal sealed class DataSection<T> : IDisposable where T : class
{
    public QuerySlot<T> Slot { get; } = new();
    public StackPanel View { get; } = new() { Spacing = 12 };
    private readonly TextBlock status = PresentationControls.Caption("");
    private readonly StackPanel content = new() { Spacing = 12 };
    private T? rendered;
    private LoadKind presentedKind=LoadKind.Idle;
    private readonly MainWindow host;
    private readonly Action<StackPanel,T> render;
    private readonly bool showEmptyStatus;
    private bool disposed;
    public DataSection(MainWindow host, string title, Action<StackPanel,T> render, string id, bool showHeader=true, bool showEmptyStatus=true, bool showStatus=true)
    {
        this.host = host; this.render = render;this.showEmptyStatus=showEmptyStatus;
        var heading = PresentationControls.Heading(title);
        AutomationProperties.SetAutomationId(View, id);
        AutomationProperties.SetHeadingLevel(heading, AutomationHeadingLevel.Level2);
        if(showHeader)
        {
            var header=new Grid {ColumnSpacing=12};header.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});header.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
            header.Children.Add(heading);Grid.SetColumn(status,1);status.VerticalAlignment=VerticalAlignment.Center;if(showStatus)header.Children.Add(status);View.Children.Add(header);
        }
        else if(showStatus)View.Children.Add(status);
        View.Children.Add(content);
        status.Text = host.Localization.State(LoadKind.Idle);
        Slot.Changed += () => host.Dispatch(Publish);
    }
    private void Publish()
    {
        if (disposed) return;
        var state = Slot.State;
        // A same-context refresh retains its data and coverage label. The window
        // shows progress without inserting a new line above an existing section.
        if(state.Kind!=LoadKind.Loading || state.Value is null)presentedKind=state.Kind;
        status.Text = host.Localization.State(presentedKind);
        status.Visibility = presentedKind == LoadKind.Ready || presentedKind==LoadKind.Empty && !showEmptyStatus ? Visibility.Collapsed : Visibility.Visible;
        if (state.Value is null)
        {
            content.Children.Clear(); rendered = null;
        }
        else if (!EqualityComparer<T>.Default.Equals(rendered, state.Value))
        {
            content.Children.Clear(); render(content, state.Value); rendered = state.Value;
        }
    }
    public void RefreshPresentation(){rendered=default;Publish();}
    public void Dispose() { disposed = true; Slot.Dispose(); }
}

internal static class PresentationControls
{
    public static TextBlock Text(string text, double size = 14) => new() { Text = text, FontSize = size, TextWrapping = TextWrapping.Wrap, IsTextSelectionEnabled = true };
    public static TextBlock Caption(string text) => new() { Text=text, Style=(Style)Application.Current.Resources["PulseCaptionStyle"], IsTextSelectionEnabled=true };
    public static TextBlock Heading(string text) => new() { Text=text, Style=(Style)Application.Current.Resources["PulseSectionHeadingStyle"] };
    public static TextBlock Feedback()
    {
        var text=Caption("");text.Visibility=Visibility.Collapsed;
        text.RegisterPropertyChangedCallback(TextBlock.TextProperty,(_,_)=>text.Visibility=string.IsNullOrEmpty(text.Text)?Visibility.Collapsed:Visibility.Visible);
        AutomationProperties.SetLiveSetting(text,AutomationLiveSetting.Polite);
        return text;
    }
    public static Border Card(UIElement child) => new()
    { Child = child, Style=(Style)Application.Current.Resources["PulseCardStyle"] };
    public static Button RankingLink(int rank,string title,string subtitle,string value,string secondary,Action open,int titleLines=1)
    {
        var grid=new Grid {ColumnSpacing=12,RowSpacing=3,IsHitTestVisible=false};
        grid.ColumnDefinitions.Add(new(){Width=new GridLength(20)});grid.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});grid.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
        for(var i=0;i<3;i++)grid.RowDefinitions.Add(new(){Height=GridLength.Auto});
        var ordinal=Caption(rank.ToString());ordinal.IsTextSelectionEnabled=false;ordinal.VerticalAlignment=VerticalAlignment.Center;Grid.SetRowSpan(ordinal,2);grid.Children.Add(ordinal);
        var name=new TextBlock {Text=title,FontSize=14,FontWeight=Microsoft.UI.Text.FontWeights.SemiBold,TextTrimming=TextTrimming.CharacterEllipsis,TextWrapping=titleLines>1?TextWrapping.Wrap:TextWrapping.NoWrap,MaxLines=titleLines};Grid.SetColumn(name,1);grid.Children.Add(name);
        var detail=Caption(subtitle);detail.IsTextSelectionEnabled=false;detail.TextWrapping=TextWrapping.NoWrap;detail.TextTrimming=TextTrimming.CharacterEllipsis;Grid.SetColumn(detail,1);Grid.SetRow(detail,1);grid.Children.Add(detail);
        var numbers=new StackPanel {Spacing=2};
        var primary=new TextBlock {Text=value,FontSize=16,FontWeight=Microsoft.UI.Text.FontWeights.SemiBold};numbers.Children.Add(primary);
        var cost=Caption(secondary);cost.IsTextSelectionEnabled=false;cost.Visibility=secondary.Length==0?Visibility.Collapsed:Visibility.Visible;numbers.Children.Add(cost);grid.Children.Add(numbers);
        void Arrange(double width)
        {
            var narrow=width<340;Grid.SetColumn(numbers,narrow?1:2);Grid.SetRow(numbers,narrow?2:0);Grid.SetRowSpan(numbers,narrow?1:2);
            numbers.HorizontalAlignment=narrow?HorizontalAlignment.Left:HorizontalAlignment.Right;
            primary.TextAlignment=cost.TextAlignment=narrow?TextAlignment.Left:TextAlignment.Right;
        }
        grid.SizeChanged+=(_,args)=>Arrange(args.NewSize.Width);Arrange(400);
        var link=new Button {Content=grid,Style=(Style)Application.Current.Resources["PulseRankingButtonStyle"]};
        AutomationProperties.SetName(link,rank+". "+string.Join(" · ",new[]{title,subtitle,value,secondary}.Where(text=>text.Length>0)));
        ToolTipService.SetToolTip(link,title);link.Click+=(_,_)=>open();return link;
    }
    public static FrameworkElement KeyValue(string label,string value)
    {
        var row=new Grid {ColumnSpacing=16};
        row.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});row.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
        row.Children.Add(Caption(label));var text=Text(value,13);text.TextAlignment=TextAlignment.Right;Grid.SetColumn(text,1);row.Children.Add(text);return row;
    }
    public static Grid SettingRow(string label, FrameworkElement control, string? description=null)
    {
        var row=new Grid {ColumnSpacing=24,Padding=new Thickness(0,8,0,8)};
        row.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});row.ColumnDefinitions.Add(new(){Width=new GridLength(240)});
        row.RowDefinitions.Add(new(){Height=GridLength.Auto});row.RowDefinitions.Add(new(){Height=GridLength.Auto});
        var text=new StackPanel {Spacing=4,VerticalAlignment=VerticalAlignment.Center};text.Children.Add(Text(label));
        if(!string.IsNullOrEmpty(description))text.Children.Add(Caption(description));row.Children.Add(text);
        control.VerticalAlignment=VerticalAlignment.Center;control.HorizontalAlignment=HorizontalAlignment.Stretch;
        AutomationProperties.SetName(control,label);Grid.SetColumn(control,1);row.Children.Add(control);
        row.SizeChanged+=(_,args)=>
        {
            var narrow=args.NewSize.Width<500;row.ColumnDefinitions[1].Width=narrow?new GridLength(0):new GridLength(240);
            row.RowSpacing=narrow?8:0;
            Grid.SetColumn(control,narrow?0:1);Grid.SetRow(control,narrow?1:0);
        };
        return row;
    }
    public static FrameworkElement Columns(IEnumerable<UIElement> items, double minimumWidth=320)
    {
        var grid=new Grid { ColumnSpacing=16, RowSpacing=16 };
        foreach(var item in items)grid.Children.Add(item);
        grid.SizeChanged+=(_,args)=>Layout(grid,grid.Children.Count,args.NewSize.Width,minimumWidth);
        Layout(grid,grid.Children.Count,minimumWidth,minimumWidth);
        return grid;
    }
    // One surface for related values; avoids nesting a card around every number.
    public static FrameworkElement MetricStrip(params (string Label,string Value)[] metrics)
    {
        var grid=new Grid { ColumnSpacing=24, RowSpacing=16 };
        foreach(var metric in metrics)
        {
            var stack=new StackPanel { Spacing=4, Padding=new Thickness(0,4,0,4) };
            var value=Text(metric.Value,24);value.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold;
            stack.Children.Add(value);stack.Children.Add(Caption(metric.Label));grid.Children.Add(stack);
        }
        grid.SizeChanged+=(_,args)=>Layout(grid,metrics.Length,args.NewSize.Width,155);
        Layout(grid,metrics.Length,1024,155);
        return grid;
    }
    public static FrameworkElement TokenBreakdown(UsageTotals? totals, Localization l)
    {
        StackPanel Group(string label,NumericValue? value,string detail,NumericValue? detailValue)
        {
            var panel=new StackPanel {Spacing=4};panel.Children.Add(Caption(label));
            var number=Text(l.Tokens(value),20);number.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold;panel.Children.Add(number);
            panel.Children.Add(Caption(detail+" "+l.Tokens(detailValue)));return panel;
        }
        return Columns([
            Group(l.Text("输入","Input"),totals?.InputTokens,l.Text("其中缓存","Cached"),totals?.CachedInputTokens),
            Group(l.Text("输出","Output"),totals?.OutputTokens,l.Text("其中推理","Reasoning"),totals?.ReasoningTokens)
        ],145);
    }
    public static FrameworkElement Split(UIElement primary, UIElement secondary, double breakpoint=900)
    {
        var grid=new Grid { ColumnSpacing=20, RowSpacing=20 };
        if(secondary is FrameworkElement secondaryElement)secondaryElement.VerticalAlignment=VerticalAlignment.Top;
        grid.Children.Add(primary);grid.Children.Add(secondary);
        bool? previous=null;
        void Arrange(double width)
        {
            var narrow=width<breakpoint;if(previous==narrow)return;previous=narrow;
            grid.ColumnDefinitions.Clear();grid.RowDefinitions.Clear();
            grid.ColumnDefinitions.Add(new(){Width=new GridLength(2,GridUnitType.Star)});
            grid.RowDefinitions.Add(new(){Height=GridLength.Auto});
            if(narrow)grid.RowDefinitions.Add(new(){Height=GridLength.Auto});
            else grid.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
            Grid.SetRow((FrameworkElement)secondary,narrow?1:0);Grid.SetColumn((FrameworkElement)secondary,narrow?0:1);
        }
        grid.SizeChanged+=(_,args)=>Arrange(args.NewSize.Width);Arrange(1024);
        return grid;
    }
    public static FrameworkElement SectionHeader(string title, FrameworkElement action)
    {
        var grid=new Grid { ColumnSpacing=12, RowSpacing=8 };
        var heading=Heading(title);heading.VerticalAlignment=VerticalAlignment.Center;
        grid.Children.Add(heading);grid.Children.Add(action);
        bool? previousNarrow=null;
        void Arrange(double width)
        {
            var narrow=width<520;
            if(previousNarrow==narrow)return;
            previousNarrow=narrow;
            grid.ColumnDefinitions.Clear();grid.RowDefinitions.Clear();
            grid.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
            grid.RowDefinitions.Add(new(){Height=GridLength.Auto});
            if(narrow)grid.RowDefinitions.Add(new(){Height=GridLength.Auto});
            else grid.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
            Grid.SetRow(action,narrow?1:0);Grid.SetColumn(action,narrow?0:1);
            action.HorizontalAlignment=narrow?HorizontalAlignment.Left:HorizontalAlignment.Right;
        }
        grid.SizeChanged+=(_,args)=>Arrange(args.NewSize.Width);Arrange(600);
        return grid;
    }
    public static FrameworkElement Metrics(params (string Label,string Value)[] metrics)
    {
        var grid = new Grid { ColumnSpacing = 12, RowSpacing = 12 };
        grid.SizeChanged += (_, args) => Layout(grid, metrics.Length, args.NewSize.Width);
        foreach (var metric in metrics)
        {
            var stack = new StackPanel { Spacing = 8 };
            stack.Children.Add(Caption(metric.Label));
            var value = new TextBlock { Text=metric.Value, Style=(Style)Application.Current.Resources["PulseMetricStyle"], IsTextSelectionEnabled=true };
            stack.Children.Add(value);
            grid.Children.Add(Card(stack));
        }
        Layout(grid, metrics.Length, 800);
        return grid;
    }
    public static void Layout(Grid grid, int count, double width, double minimumWidth=220)
    {
        var columns = Math.Max(1, Math.Min(count, (int)((width+grid.ColumnSpacing) / (minimumWidth+grid.ColumnSpacing))));
        if(count>0)columns=Math.Min(columns,(int)Math.Ceiling(count/Math.Ceiling((double)count/columns)));
        if (grid.ColumnDefinitions.Count == columns && grid.RowDefinitions.Count == (count+columns-1)/columns) return;
        grid.ColumnDefinitions.Clear(); grid.RowDefinitions.Clear();
        for (var i=0;i<columns;i++) grid.ColumnDefinitions.Add(new() { Width=new GridLength(1,GridUnitType.Star) });
        for (var i=0;i<(count+columns-1)/columns;i++) grid.RowDefinitions.Add(new() { Height=GridLength.Auto });
        for(var i=0;i<grid.Children.Count;i++) { Grid.SetColumn((FrameworkElement)grid.Children[i],i%columns); Grid.SetRow((FrameworkElement)grid.Children[i],i/columns); }
    }
    public static FrameworkElement Table(string[] headings, IEnumerable<string[]> rows)
    {
        var body = new StackPanel { Spacing=0 };
        Grid Row(string[] cells, bool header)
        {
            var row = new Grid { ColumnSpacing=16, Padding=new Thickness(12,10,12,10) };
            if(header) row.Style=(Style)Application.Current.Resources["PulseTableHeaderStyle"];
            for(var i=0;i<headings.Length;i++) row.ColumnDefinitions.Add(new() { Width=new GridLength(i==0?2:1,GridUnitType.Star), MinWidth=i==0?160:115 });
            for(var i=0;i<cells.Length;i++) { var text=header?Caption(cells[i]):Text(cells[i],13); if(header)text.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold; Grid.SetColumn(text,i); row.Children.Add(text); }
            return row;
        }
        body.Children.Add(Row(headings,true));
        foreach(var cells in rows) body.Children.Add(Row(cells,false));
        return new ScrollViewer { Content=body, HorizontalScrollBarVisibility=ScrollBarVisibility.Auto, VerticalScrollBarVisibility=ScrollBarVisibility.Disabled, HorizontalContentAlignment=HorizontalAlignment.Stretch };
    }
    public static FrameworkElement Disclosure(string label, UIElement content) => new Expander { Header=label, Content=content, HorizontalAlignment=HorizontalAlignment.Stretch, HorizontalContentAlignment=HorizontalAlignment.Stretch };
    public static FrameworkElement DataButton(string label, UIElement content)=>DataButton(label,()=>content);
    public static FrameworkElement DataButton(string label, Func<UIElement> content)
    {
        var button=new Button {Content=label,Padding=new Thickness(10,4,10,4),HorizontalAlignment=HorizontalAlignment.Right};
        var scroll=new ScrollViewer {Width=560,MaxHeight=360,VerticalScrollBarVisibility=ScrollBarVisibility.Auto,HorizontalScrollBarVisibility=ScrollBarVisibility.Auto};
        var flyout=new Flyout
        {
            Content=scroll,
            Placement=Microsoft.UI.Xaml.Controls.Primitives.FlyoutPlacementMode.TopEdgeAlignedRight
        };
        var presenterStyle=new Style {TargetType=typeof(FlyoutPresenter)};
        presenterStyle.Setters.Add(new Setter(FrameworkElement.MaxWidthProperty,double.PositiveInfinity));
        flyout.FlyoutPresenterStyle=presenterStyle;
        flyout.Opening+=(_,_)=>
        {
            scroll.Content??=content();
            if(button.XamlRoot is not {} root)return;
            scroll.Width=Math.Min(560,Math.Max(240,root.Size.Width-80));
            scroll.MaxHeight=Math.Min(360,Math.Max(160,root.Size.Height-120));
        };
        button.Flyout=flyout;
        return button;
    }
}
