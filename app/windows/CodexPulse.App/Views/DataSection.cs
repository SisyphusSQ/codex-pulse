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
    private readonly TextBlock status = new() { TextWrapping = TextWrapping.Wrap, Opacity = .75 };
    private readonly StackPanel content = new() { Spacing = 12 };
    private T? rendered;
    private readonly MainWindow host;
    private readonly Action<StackPanel,T> render;
    private bool disposed;
    public DataSection(MainWindow host, string title, Action<StackPanel,T> render, string id)
    {
        this.host = host; this.render = render;
        var heading = new TextBlock { Text = title, Style = (Style)Application.Current.Resources["SubtitleTextBlockStyle"], TextWrapping = TextWrapping.Wrap };
        AutomationProperties.SetAutomationId(View, id);
        AutomationProperties.SetHeadingLevel(heading, AutomationHeadingLevel.Level2);
        View.Children.Add(heading); View.Children.Add(status); View.Children.Add(content);
        status.Text = host.Localization.State(LoadKind.Idle);
        Slot.Changed += () => host.Dispatch(Publish);
    }
    private void Publish()
    {
        if (disposed) return;
        var state = Slot.State;
        status.Text = host.Localization.State(state.Kind);
        if (state.Value is null)
        {
            content.Children.Clear(); rendered = null;
        }
        else if (!EqualityComparer<T>.Default.Equals(rendered, state.Value))
        {
            content.Children.Clear(); render(content, state.Value); rendered = state.Value;
        }
    }
    public void Dispose() { disposed = true; Slot.Dispose(); }
}

internal static class PresentationControls
{
    public static TextBlock Text(string text, double size = 14) => new() { Text = text, FontSize = size, TextWrapping = TextWrapping.Wrap, IsTextSelectionEnabled = true };
    public static Border Card(UIElement child) => new()
    { Child = child, Padding = new Thickness(16), CornerRadius = new CornerRadius(8), BorderThickness = new Thickness(1), BorderBrush = Charts.NeutralBrush };
    public static FrameworkElement Metrics(params (string Label,string Value)[] metrics)
    {
        var grid = new Grid { ColumnSpacing = 12, RowSpacing = 12 };
        grid.SizeChanged += (_, args) => Layout(grid, metrics.Length, args.NewSize.Width);
        foreach (var metric in metrics)
        {
            var stack = new StackPanel { Spacing = 6 };
            var label = Text(metric.Label,13); label.Opacity=.75;
            stack.Children.Add(label); var value = Text(metric.Value,24); stack.Children.Add(value);
            grid.Children.Add(Card(stack));
        }
        Layout(grid, metrics.Length, 800);
        return grid;
    }
    public static void Layout(Grid grid, int count, double width)
    {
        var columns = Math.Max(1, Math.Min(count, (int)(width / 220)));
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
            var row = new Grid { ColumnSpacing=14, Padding=new Thickness(4,8,4,8) };
            for(var i=0;i<headings.Length;i++) row.ColumnDefinitions.Add(new() { Width=new GridLength(i==0?2:1,GridUnitType.Star), MinWidth=i==0?160:115 });
            for(var i=0;i<cells.Length;i++) { var text=Text(cells[i],header?12:14); if(header)text.Opacity=.75; Grid.SetColumn(text,i); row.Children.Add(text); }
            return row;
        }
        body.Children.Add(Row(headings,true));
        foreach(var cells in rows) body.Children.Add(Row(cells,false));
        return new ScrollViewer { Content=body, HorizontalScrollBarVisibility=ScrollBarVisibility.Auto, VerticalScrollBarVisibility=ScrollBarVisibility.Disabled, HorizontalContentAlignment=HorizontalAlignment.Stretch };
    }
    public static FrameworkElement Disclosure(string label, UIElement content) => new Expander { Header=label, Content=content, HorizontalAlignment=HorizontalAlignment.Stretch, HorizontalContentAlignment=HorizontalAlignment.Stretch };
}
