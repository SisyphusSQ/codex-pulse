using CodexPulse.AppSupport;
using CodexPulse.CoreClient;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

public abstract class FeaturePage : IDisposable
{
    protected MainWindow Host { get; }
    protected Localization L => Host.Localization;
    protected StackPanel Body { get; } = new() { Spacing = 20, Padding = new Thickness(0, 8, 0, 24) };
    public FrameworkElement View { get; protected set; }
    protected FeaturePage(MainWindow host)
    {
        Host = host;
        View = new ScrollViewer { Content = Body, VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
            HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled, HorizontalContentAlignment = HorizontalAlignment.Stretch };
    }
    public abstract Task LoadAsync(bool force = false);
    public virtual void Cancel() { }
    public virtual bool DependsOn(string domain) => domain is "index" or "settings" or "account";
    public virtual void Invalidate(IReadOnlyCollection<string> domains) { }
    public virtual void Dispose() => Cancel();
    protected TextBlock Text(string value, string? style = null)
    {
        var block = new TextBlock { Text = value, TextWrapping = TextWrapping.Wrap, IsTextSelectionEnabled = true };
        if (style is not null) block.Style = (Style)Application.Current.Resources[style];
        return block;
    }
    protected void State(LoadKind kind) => Host.SetPageState(this, kind);
}

public static class PageCatalog
{
    private static readonly Dictionary<Feature, Func<MainWindow, FeaturePage>> factories = [];
    public static void Register(Feature feature, Func<MainWindow, FeaturePage> factory) => factories[feature] = factory;
    public static FeaturePage Create(Feature feature, MainWindow host) => factories.TryGetValue(feature, out var factory) ? factory(host) : new RuntimeHomePage(host);
}

internal sealed class RuntimeHomePage(MainWindow host) : FeaturePage(host)
{
    public override bool DependsOn(string domain) => domain == "settings";
    public override Task LoadAsync(bool force = false)
    {
        Body.Children.Clear();
        if (Host.Session?.State is not (CoreConnectionState.Connected or CoreConnectionState.Recovery))
        {
            Body.Children.Add(Text(L.Text("本机服务未连接，请重新连接或退出应用。", "The local service is disconnected. Reconnect or quit the app.")));
            State(LoadKind.Unavailable); return Task.CompletedTask;
        }
        Body.Children.Add(Text(L.Text("本机服务", "Local service"), "SubtitleTextBlockStyle"));
        Body.Children.Add(Text("Codex Pulse · " + Host.BundleVersion));
        Body.Children.Add(Text(Host.Session.Bootstrap.Mode == "recovery" ? L.Text("数据库需要恢复", "Database recovery required") : L.Text("本机服务已连接", "Connected to local service")));
        foreach (var provider in Host.Settings?.Snapshot.Providers ?? [])
            Body.Children.Add(Text(MainWindow.ProviderName(provider.Provider) + " · " + (provider.EffectiveState == Codexpulse.Core.V1.ProviderEffectiveState.Enabled
                ? L.Text("已启用", "Enabled") : L.Text("未启用或暂不可用", "Disabled or unavailable"))));
        if (Host.Workspace.EnabledProviders.Count == 0)
        {
            Body.Children.Add(Text(L.Text("尚未启用客户端。可在设置中选择数据来源。", "No clients are enabled. Choose a data source in Settings.")));
            var settings = new Button { Content = L.Text("打开设置", "Open Settings") };
            settings.Click += (_, _) => Host.Navigate(Feature.Settings);
            Body.Children.Add(settings);
        }
        State(Host.Session.State == CoreConnectionState.Recovery ? LoadKind.RecoveryBlocked : LoadKind.Ready);
        return Task.CompletedTask;
    }
}
