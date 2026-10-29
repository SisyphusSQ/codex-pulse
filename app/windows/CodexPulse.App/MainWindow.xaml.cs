using System.Reflection;
using System.Runtime.InteropServices;
using System.Text.Json;
using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.App.Views;
using CodexPulse.CoreClient;
using CodexPulse.Distribution;
using Grpc.Core;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Windows.Graphics;

namespace CodexPulse.App;

public sealed partial class MainWindow : Window
{
    public Workspace Workspace { get; } = new();
    public Localization Localization { get; } = new();
    public FeatureClient Client { get; }
    public SettingsEditor SettingsEditor{get;}
    public ReportingController Reporting{get;}
    public ApiSubscriptionsController ApiSubscriptions{get;}
    public WindowsUpdater WindowsUpdates{get;}
    public bool ApiCredentialsRequested{get;private set;}
    public HomeSwitchDraft HomeSwitchDraft{get;}=new();
    public bool DataHealthRequested{get;private set;}
    public bool WindowActive{get;private set;}
    public bool WindowVisible{get;private set;}
    public ElementTheme CurrentTheme=>Root.RequestedTheme;
    public SemaphoreSlim RuntimeCommandGate{get;}=new(1,1);
    public (string Kind,string Identity)? RuntimeSelection{get;private set;}
    public AnalysisNavigation Analysis {get;}=new();
    public HelperSession? Session { get; private set; }
    public SettingsResponse? Settings { get; private set; }
    public string BundleVersion { get; } = typeof(MainWindow).Assembly.GetCustomAttributes<AssemblyMetadataAttribute>()
        .FirstOrDefault(attribute => attribute.Key == "PulseVersion")?.Value ?? "dev";
    public PlatformOptions Options { get; }
    public PageSelection? Selection { get; private set; }
    public XamlRoot DialogRoot => Root.XamlRoot;
    private SubscriptionController? subscriptions;
    public SubscriptionController Subscriptions
    {
        get
        {
            var generation=Settings?.Snapshot.Home.Generation??"";
            if(subscriptions is null || subscriptions.HomeGeneration!=generation)
            {subscriptions?.Dispose();subscriptions=new(Client,()=>Workspace.Context,generation);}
            return subscriptions;
        }
    }
    private FeaturePage? page;
    private WindowPreferences preferences = new();
    private CancellationTokenSource? watchCancellation;
    private Task? watch;
    private readonly DispatcherTimer invalidations = new() { Interval = TimeSpan.FromMilliseconds(200) };
    private readonly HashSet<string> pendingDomains = [];
    private bool applying, starting, closing, allowClose;
    private bool localeSaving;
    private CalendarDatePicker StartDate = null!, EndDate = null!;
    private string? datePickerLanguage;
    private long renderGeneration;
    private long settingsRequest;
    private readonly CancellationTokenSource lifetime = new();
    private Task? startup;
    private readonly HashSet<FeaturePage> loadingPages = [];
    private readonly HashSet<FeaturePage> reloadPending = [];
    private QueryContext? renderedContext;
    private Feature renderedFeature;
    private NativeTray? nativeTray;
    private TrayPanel? trayPanel;
    private readonly RestartBudget restartBudget=new();
    private readonly LifecycleMailbox lifecycle;
    private readonly SemaphoreSlim connectionGate=new(1,1);
    private bool recovering,sleeping;
    private DateTimeOffset lastWake;
    private string? updateNonce;

    public MainWindow(PlatformOptions options)
    {
        Options = options;
        lifecycle=new(DeliverLifecycleAsync);
        Client = new(() => Session?.Connection.Raw ?? throw new IOException("helper_disconnected"));
        SettingsEditor=new(Client,()=>Workspace.Context);
        Reporting=new(Client,()=>Workspace.Context);
        ApiSubscriptions=new(Client,()=>Workspace.Context,async()=>{var previous=Session;await StartAsync();return Session?.State==CoreConnectionState.Connected && !ReferenceEquals(previous,Session);});
        WindowsUpdates=new(AppContext.BaseDirectory,Options.Runtime,Options.Home,()=>{var value=Settings?.Snapshot.Updates;return(value?.AutoCheckEnabled??false,value?.Channel??"stable",value?.CheckIntervalSeconds??3600);},QuitForUpdateAsync);
        PageCatalog.Register(Feature.Summary, host => new DashboardPage(host));
        PageCatalog.Register(Feature.Overview, host => new OverviewPage(host));
        PageCatalog.Register(Feature.Sessions, host => new SessionsPage(host));
        PageCatalog.Register(Feature.Projects, host => new ProjectsPage(host));
        PageCatalog.Register(Feature.Quota, host => new QuotaPage(host));
        PageCatalog.Register(Feature.AccountQuotas, host => new AccountQuotasPage(host));
        PageCatalog.Register(Feature.ApiSubscriptions,host=>new ApiSubscriptionsPage(host));
        PageCatalog.Register(Feature.Settings, host => new SettingsPage(host));
        PageCatalog.Register(Feature.LocalStatus, host => new LocalStatusPage(host));
        PageCatalog.Register(Feature.SourcesJobs, host => new SourcesJobsPage(host));
        InitializeComponent();
        AppWindow.SetIcon(Path.Combine(AppContext.BaseDirectory,"Assets","CodexPulse.ico"));
        try { preferences = WindowPreferences.Load(Options.Runtime); }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException or JsonException or System.Security.SecurityException)
        { preferences = new(); ShowNotice("窗口偏好无法读取，使用默认布局。", "Window preferences could not be read. Using the default layout."); }
        try{LaunchPreferences.Load(Options.Runtime).Apply();}
        catch(Exception error)when(error is IOException or UnauthorizedAccessException or JsonException or ArgumentException or System.Security.SecurityException)
        {ShowNotice("平台程序路径无法读取，沿用环境与自动发现。","Platform executable paths could not be read. Using the environment and discovery.");}
        Workspace.Changed += WorkspaceChanged;
        Navigation.DisplayModeChanged += (_, args) => UpdateHeaderInset();
        invalidations.Tick += async (_, _) =>
        {
            invalidations.Stop();
            var domains = pendingDomains.ToArray(); pendingDomains.Clear();
            if (domains.Contains("settings")) await ReloadSettingsAsync();
            if (WindowVisible && page is not null && domains.Any(page.DependsOn)) { page.Invalidate(domains); await ReloadPageAsync(true); }
        };
        AppWindow.Closing += ClosingWindow;
        Activated+=(_,args)=>{WindowActive=args.WindowActivationState!=WindowActivationState.Deactivated;if(WindowActive)QueueLifecycle("application_did_become_active");};
        Root.Loaded += (_, _) => UpdateHeaderInset();
        ApplyLabels();
        SetWindowBounds();
        try
        {
            trayPanel=new(this);
            nativeTray=new(WinRT.Interop.WindowNative.GetWindowHandle(this));
            trayPanel.TooltipChanged+=nativeTray.Tooltip;
            nativeTray.Toggle+=()=>trayPanel.Toggle(nativeTray.Anchor());
            nativeTray.ContextMenu+=ShowTrayMenu;
            nativeTray.Power+=QueueLifecycle;
            nativeTray.SessionEnding+=()=>{_=QuitAsync();};
            nativeTray.DisplayChanged+=()=>{trayPanel.Position();EnsureWindowVisible();};
            nativeTray.TaskbarRestored+=()=>{if(!nativeTray.Available)ShowMain();};
        }
        catch(IOException){ShowNotice("托盘无法创建，窗口关闭将退出应用。","Tray is unavailable. Closing the window will quit the app.");}
    }

    public async Task StartHostedAsync()
    {
        if(Options.Show)ShowMain();
        startup=StartAsync();await startup;
        if(nativeTray?.Available!=true || Session?.State==CoreConnectionState.Recovery || !Options.Background && (Options.Show || Settings?.Snapshot.Ui.LaunchBehavior!="tray"))ShowMain();
    }
    public void ShowMain()
    {
        if(closing)return;WindowVisible=true;
        ShowWindow(WinRT.Interop.WindowNative.GetWindowHandle(this),9);AppWindow.Show();Activate();
        QueueLifecycle("application_did_become_active");if(!starting)_=ReloadPageAsync(true);
    }
    private void HideMain(){WindowVisible=false;WindowActive=false;page?.Cancel();AppWindow.Hide();}
    public void SelectTrayProvider(string provider){Workspace.SelectProvider(provider,true);SaveWindowPreferences();}
    private void ShowTrayMenu()
    {
        if(trayPanel is null)return;
        trayPanel.Hide();
        try
        {
            var command=NativeTray.Menu(trayPanel.Handle,[(1,Localization.Text("打开主窗口","Open main window")),(2,Localization.Text("额度面板","Quota panel")),(3,Localization.Text("设置","Settings")),(0,""),(4,Localization.Text("退出 Codex Pulse","Quit Codex Pulse"))]);
            switch(command){case 1:ShowMain();break;case 2:trayPanel.Show(nativeTray!.Anchor());break;case 3:Navigate(Feature.Settings);ShowMain();break;case 4:_=QuitAsync();break;}
        }
        catch(IOException){ShowNotice("托盘菜单无法打开。","The tray menu could not be opened.");ShowMain();}
    }

    public void Dispatch(Action action) => DispatcherQueue.TryEnqueue(() => { if (!closing) action(); });
    public void Navigate(Feature feature) { Selection = null;RuntimeSelection=null;DataHealthRequested=false;Workspace.Navigate(feature); }
    public void OpenDataHealth(){DataHealthRequested=true;Workspace.Navigate(Feature.LocalStatus);}
    public void OpenRuntimeItem(string kind,string identity){if(kind is not("source" or "job"))return;RuntimeSelection=(kind,identity);Workspace.Navigate(Feature.SourcesJobs);}
    public void OpenSession(string id, UTCTimeRange range) { Selection = new(Feature.Sessions, id, range.Clone()); Workspace.Navigate(Feature.Sessions); }
    public void OpenProject(string dimension, UTCTimeRange range) { Selection = new(Feature.Projects, dimension, range.Clone()); Workspace.Navigate(Feature.Projects); }
    public void OpenProjectList(string project,UTCTimeRange range) {Selection=new(Feature.Projects,"",range.Clone(),project);Workspace.Navigate(Feature.Projects);}
    public void OpenProvider(string provider) { applying=true; Workspace.SelectProvider(provider); applying=false; Navigate(Feature.Overview); }
    public void SetPageState(LoadKind state) => PageStatus.Text = Localization.State(state);
    public void SetPageState(FeaturePage owner, LoadKind state) { if (ReferenceEquals(owner, page)) SetPageState(state); }
    public void ShowNotice(string chinese, string english, InfoBarSeverity severity = InfoBarSeverity.Warning)
    {
        ConnectionNotice.Message = Localization.Text(chinese, english);
        ConnectionNotice.Severity = severity;
        ConnectionNotice.IsOpen = true;
    }

    public Task StartAsync()
    {
        if(closing)return Task.CompletedTask;
        if(startup is{IsCompleted:false})return startup;
        startup=StartSerializedAsync();return startup;
    }
    private async Task StartSerializedAsync()
    {
        try
        {
            await connectionGate.WaitAsync(lifetime.Token);
            try{await StartCoreAsync();}finally{connectionGate.Release();}
        }
        catch(OperationCanceledException)when(closing){}
    }
    private async Task StartCoreAsync()
    {
        if (starting || closing) return;
        starting = true; ReconnectButton.IsEnabled = false;
        ShowNotice("正在连接本机数据…", "Connecting to local data…", InfoBarSeverity.Informational);
        try
        {
            if (Session is not null)
            {
                await StopWatchAsync();
                if (!Session.Exit.IsCompleted && !await Session.StopAsync()) { RecordShutdown("uncertain");ShowNotice("上次退出状态不确定，请退出应用后重新打开。", "The last shutdown is uncertain. Quit and reopen the app."); return; }
                await Session.DisposeAsync(); Session = null;
            }
            Client.Invalidate();ApiSubscriptions.ResetConnection(); Workspace.ResetContext();
            Session = await HelperSession.StartAsync(Options.Helper, Options.Runtime, Options.Home, BundleVersion, lifetime.Token);
            var connected=Session;
            connected.StateChanged += _ => Dispatch(()=>{if(ReferenceEquals(Session,connected))ConnectionChanged();});
            RecordShutdown("running");
            if (Session.State == CoreConnectionState.Recovery)
            {
                ShowNotice("数据库需要恢复，请打开恢复页面或退出应用。", "Database recovery is required. Open recovery or quit the app.");
                RenderPage();
            }
            else
            {
                await ReloadSettingsAsync();
                var restoredPreferences = preferences;
                applying = true;
                if (restoredPreferences.Provider is not null) Workspace.SelectProvider(restoredPreferences.Provider);
                if (restoredPreferences.TrayProvider is not null) Workspace.SelectProvider(restoredPreferences.TrayProvider, true);
                if (Enum.TryParse<Feature>(restoredPreferences.Feature, out var restored) && Enum.IsDefined(restored)) Workspace.Navigate(restored);
                applying = false; ApplyLabels(); RenderPage(); SaveWindowPreferences();
                ConnectionNotice.IsOpen = false;
            }
            ConnectionChanged();
            if(Options.StartupNonce is {} nonce)UpdateRuntime.Startup(Options.Runtime,nonce,BundleVersion,Session.State==CoreConnectionState.Recovery?"recovery":"connected");
            watchCancellation = new();
            watch = ObserveInvalidationsAsync(watchCancellation.Token);
            if(WindowVisible)await ReloadPageAsync();
            trayPanel?.Invalidate(null);
        }
        catch (OperationCanceledException) when (closing) { }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException or RpcException or ArgumentException)
        {
            var incompatible = error is IOException { Message: "helper_contract_mismatch" } or RpcException { StatusCode: StatusCode.FailedPrecondition };
            ShowNotice(incompatible ? "应用与本机服务版本不兼容，请使用同一安装包。" : "无法连接本机服务，请重连或退出应用。",
                incompatible ? "The app and helper versions differ. Use the same installation bundle." : "Cannot connect to the local service. Reconnect or quit.", InfoBarSeverity.Error);
            SetPageState(LoadKind.Unavailable);
        }
        finally { starting = false; ReconnectButton.IsEnabled = true;if(Session?.State==CoreConnectionState.Disconnected)_=RecoverConnectionAsync(); }
    }

    public async Task<bool> ReloadSettingsAsync()
    {
        if (Session?.State != CoreConnectionState.Connected) return false;
        var version = ++settingsRequest;
        try
        {
            var request = new SettingsRequest();
            var response = await Client.QueryAsync("Settings", request, Workspace.Context, ["settings"],
                (client, deadline, cancellation) => client.SettingsAsync(request, deadline: deadline, cancellationToken: cancellation), force: true);
            if (version != settingsRequest || closing) return false;
            Settings = response;
            WindowsUpdates.Configure();
            SettingsEditor.Observe(response);
            Workspace.OverviewDefault = Settings.Snapshot.Ui.OverviewRange;
            Localization.Set(Settings.Snapshot.Ui.Locale);
            applying = true;
            Workspace.ApplyProviders(Settings.Snapshot.Providers);
            applying = false;
            ApplyLabels(); RenderPage();
            return true;
        }
        catch (RpcException) { ShowNotice("设置暂时无法读取，可重试。", "Settings are unavailable. You can retry.");return false; }
    }

    private async Task ObserveInvalidationsAsync(CancellationToken cancellation)
    {
        try
        {
            await Session!.WatchAsync(change =>
            {
                Client.Invalidate(change?.Domain);
                Dispatch(() =>
                {
                    if (change is null || change.Domain is "account" or "settings")
                    {
                        Analysis.Clear();Selection=null;RuntimeSelection=null;
                        page?.Cancel(); Client.Invalidate(); Workspace.ResetContext();
                    }
                    trayPanel?.Invalidate(change?.Domain);
                    pendingDomains.Add(change?.Domain ?? "settings");
                    if (change is null) pendingDomains.UnionWith(["index", "quota", "health", "account"]);
                    invalidations.Start();
                });
                return Task.CompletedTask;
            }, cancellation);
        }
        catch (OperationCanceledException) when (cancellation.IsCancellationRequested) { }
        catch (Exception error) when (error is RpcException or IOException)
        { Dispatch(() => { page?.Cancel(); Client.Invalidate(); Workspace.ResetContext();trayPanel?.Invalidate(null); ShowNotice("本机数据通知已断开，正在有限重连。", "Local data updates disconnected. Attempting bounded recovery.");_=RecoverConnectionAsync(); }); }
    }

    private void UpdateConnectionLabel()
    {
        ConnectionLabel.Text = Session?.State switch
        {
            CoreConnectionState.Connected => Localization.Text("本机数据 · 已连接", "Local data · Connected"),
            CoreConnectionState.Recovery => Localization.Text("本机数据 · 恢复模式", "Local data · Recovery"),
            _ => Localization.Text("本机数据 · 未连接", "Local data · Disconnected")
        };
    }

    private void ConnectionChanged()
    {
        UpdateConnectionLabel();
        if (Session?.State is CoreConnectionState.Disconnected or CoreConnectionState.Uncertain)
        {
            page?.Cancel(); Client.Invalidate(); Workspace.ResetContext();
            ShowNotice("本机服务已停止，请重新连接。", "The local service stopped. Reconnect to continue.");
            trayPanel?.Invalidate(null);
            if(Session.State==CoreConnectionState.Disconnected)_=RecoverConnectionAsync();
        }
    }

    private async Task RecoverConnectionAsync()
    {
        if(recovering || starting || closing || sleeping)return;
        recovering=true;
        try
        {
            while(!closing && !sleeping)
            {
                var delay=restartBudget.Claim(DateTimeOffset.UtcNow);
                if(delay is null){ShowNotice("自动恢复已达上限，请检查本机状态后手动重连。","Automatic recovery reached its limit. Check Local status and reconnect manually.",InfoBarSeverity.Error);return;}
                await Task.Delay(delay.Value,lifetime.Token);if(closing || sleeping)return;
                await StartAsync();
                if(Session?.State is CoreConnectionState.Connected or CoreConnectionState.Recovery or CoreConnectionState.Uncertain)return;
            }
        }
        catch(OperationCanceledException)when(closing){}
        finally{recovering=false;}
    }
    public void QueueLifecycle(string name)
    {
        if(closing)return;
        if(name=="system_did_wake")
        {
            if(DateTimeOffset.UtcNow-lastWake<TimeSpan.FromSeconds(3))return;
            lastWake=DateTimeOffset.UtcNow;
        }
        lifecycle.Post(name);
    }
    private async Task DeliverLifecycleAsync(string name)
    {
        if(closing)return;
        try
        {
            await connectionGate.WaitAsync(lifetime.Token);
            try
            {
            if(name=="system_will_sleep"){sleeping=true;page?.Cancel();trayPanel?.Hide();await StopWatchAsync();}
            if(name=="system_did_wake")
            {
                sleeping=false;await StopWatchAsync();Client.Invalidate();Workspace.ResetContext();
                if(Session is null || Session.Exit.IsCompleted){await StartCoreAsync();return;}
                await Session.Connection.HandshakeAsync(BundleVersion,lifetime.Token);await Session.RefreshBootstrapAsync(lifetime.Token);
            }
            if(Session?.State==CoreConnectionState.Connected)
            {
                var receipt=await Session.Connection.Raw.NotifyLifecycleAsync(new LifecycleNotificationRequest{Event=name},deadline:DateTime.UtcNow.AddSeconds(3),cancellationToken:lifetime.Token);
                if(!receipt.Accepted || receipt.Event!=name)ShowNotice("系统状态通知未被确认。","System lifecycle notification was not acknowledged.");
            }
            if(name=="application_did_become_active" && WindowVisible && Workspace.Feature==Feature.ApiSubscriptions)await ReloadPageAsync(true);
            if(name=="system_did_wake" && Session is not null)
            {
                if(Session.State==CoreConnectionState.Connected)await ReloadSettingsAsync();else RenderPage();
                watchCancellation=new();watch=ObserveInvalidationsAsync(watchCancellation.Token);
                trayPanel?.Invalidate(null);if(WindowVisible)await ReloadPageAsync(true);
            }
            }
            finally{connectionGate.Release();}
        }
        catch(OperationCanceledException)when(closing){}
        catch(Exception e)when(e is RpcException or IOException)
        {if(!closing){ShowNotice("系统状态同步未完成，将尝试恢复连接。","Lifecycle synchronization incomplete. Attempting connection recovery.");if(name!="system_will_sleep")_=RecoverConnectionAsync();}}
    }

    private void WorkspaceChanged()
    {
        if (applying || closing) return;
        if (renderedContext is {} old && (old.Provider!=Workspace.Provider || old.Generation!=Workspace.Generation ||
            renderedFeature==Workspace.Feature && (old.Start!=Workspace.Start || old.EndExclusive!=Workspace.EndExclusive || old.Preset!=Workspace.RangePreset))) Selection=null;
        page?.Cancel();
        if (renderedContext is {} previous && previous.Provider==Workspace.Provider && previous.Generation==Workspace.Generation && renderedFeature==Workspace.Feature)
        { applying=true; ApplyRangeLabels(false); applying=false; }
        else ApplyLabels();
        RenderPage();
        if (!starting) SaveWindowPreferences();
        if(WindowVisible)_ = ReloadPageAsync();
    }

    private void RenderPage()
    {
        renderGeneration++;
        renderedContext=Workspace.Context;renderedFeature=Workspace.Feature;
        page?.Dispose();
        page = Session?.State == CoreConnectionState.Recovery ? new RecoveryPage(this) : PageCatalog.Create(Workspace.Feature, this);
        page.View.Language = Localization.Culture.Name;
        PageContainer.Content = page.View;
        PageTitle.Text = Localization.FeatureTitle(Workspace.Feature);
        SetPageState(Session?.State == CoreConnectionState.Recovery ? LoadKind.RecoveryBlocked : LoadKind.Idle);
        applying = true;
        foreach (var item in Navigation.MenuItems.OfType<NavigationViewItem>())
            if (item.Tag is Feature feature && feature == Workspace.Feature) Navigation.SelectedItem = item;
        applying = false;
    }

    public async Task ReloadPageAsync(bool force = false)
    {
        var current = page; var generation = renderGeneration;
        if (current is null || Session is null) return;
        if (loadingPages.Contains(current)) { reloadPending.Add(current); return; }
        loadingPages.Add(current);
        try
        {
            do
            {
                reloadPending.Remove(current);
                await current.LoadAsync(force);
                force = true;
            } while (ReferenceEquals(current, page) && !closing && reloadPending.Remove(current));
        }
        catch (OperationCanceledException) { if (generation == renderGeneration) SetPageState(LoadKind.Cancelled); }
        catch (Exception error) when (error is RpcException or IOException)
        { if (generation == renderGeneration) SetPageState(LoadKind.Unavailable); }
        finally { loadingPages.Remove(current); reloadPending.Remove(current); }
    }

    private void ApplyLabels()
    {
        applying = true;
        ProviderPicker.Header = Localization.Text("客户端", "Client");
        AutomationProperties.SetName(ProviderPicker, Localization.Text("客户端", "Client"));
        ProviderPicker.Items.Clear();
        foreach (var provider in Workspace.EnabledProviders) ProviderPicker.Items.Add(new ComboBoxItem { Content = ProviderName(provider), Tag = provider });
        ProviderPicker.IsEnabled = Workspace.EnabledProviders.Count > 0;
        ProviderPicker.PlaceholderText = Localization.Text("尚未启用客户端", "No enabled clients");
        ProviderPicker.SelectedItem = ProviderPicker.Items.Cast<ComboBoxItem>().FirstOrDefault(item => (string)item.Tag == Workspace.Provider);
        Navigation.MenuItems.Clear();
        AddNavigation(Feature.Summary, Symbol.AllApps);
        if (Workspace.Provider is not null)
        {
            Navigation.MenuItems.Add(new NavigationViewItemSeparator());
            AddNavigation(Feature.Overview, Symbol.Home); AddNavigation(Feature.Sessions, Symbol.Message);
            AddNavigation(Feature.Projects, Symbol.Folder); AddNavigation(Feature.Quota, Symbol.ReportHacked);
            if (Workspace.Provider == "codex") AddNavigation(Feature.AccountQuotas, Symbol.People);
        }
        Navigation.MenuItems.Add(new NavigationViewItemSeparator());
        AddNavigation(Feature.ApiSubscriptions, Symbol.Shop);
        AddNavigation(Feature.LocalStatus, Symbol.Important); AddNavigation(Feature.SourcesJobs, Symbol.Library);
        AddNavigation(Feature.Settings, Symbol.Setting);
        RefreshButton.Content = Localization.Text("重新加载", "Reload"); CancelButton.Content = Localization.Text("取消", "Cancel");
        OpenTrayMenu.Text=Localization.Text("额度面板","Quota panel");HideMenu.Text=Localization.Text("收起到托盘","Hide to tray");QuitMenu.Text=Localization.Text("退出","Quit");
        AutomationProperties.SetName(AppMenu,Localization.Text("应用菜单","App menu"));
        ReconnectButton.Content = Localization.Text("重新连接", "Reconnect");
        ApplyRangeLabels(true);
        LanguagePicker.Items.Clear();
        foreach (var item in new[] { ("system", Localization.Text("系统语言", "System language")), ("zh-CN", "简体中文"), ("en-US", "English") })
            LanguagePicker.Items.Add(new ComboBoxItem { Tag = item.Item1, Content = item.Item2 });
        LanguagePicker.SelectedItem = LanguagePicker.Items.Cast<ComboBoxItem>().Single(item => (string)item.Tag == Localization.Locale);
        AutomationProperties.SetName(LanguagePicker, Localization.Text("语言", "Language"));
        ThemePicker.Items.Clear();
        foreach (var item in new[] { ("system", Localization.Text("系统外观", "System theme")), ("light", Localization.Text("浅色", "Light")), ("dark", Localization.Text("深色", "Dark")) })
            ThemePicker.Items.Add(new ComboBoxItem { Tag = item.Item1, Content = item.Item2 });
        ThemePicker.SelectedItem = ThemePicker.Items.Cast<ComboBoxItem>().FirstOrDefault(item => (string)item.Tag == preferences.Theme) ?? ThemePicker.Items[0];
        Root.RequestedTheme = preferences.Theme switch { "light" => ElementTheme.Light, "dark" => ElementTheme.Dark, _ => ElementTheme.Default };
        trayPanel?.UpdatePresentation();
        AutomationProperties.SetName(ThemePicker, Localization.Text("外观", "Theme"));
        UpdateConnectionLabel();
        applying = false;
    }

    private void ApplyRangeLabels(bool rebuild)
    {
        RangeBar.Visibility=Workspace.Feature is Feature.Summary or Feature.Overview or Feature.Sessions or Feature.Projects or Feature.Quota?Visibility.Visible:Visibility.Collapsed;
        if(rebuild) { RangePicker.Items.Clear();
        foreach (var item in new[] { ("today",Localization.Text("今天", "Today")), ("seven_days",Localization.Text("最近 7 天", "Last 7 days")), ("thirty_days",Localization.Text("最近 30 天", "Last 30 days")), ("custom",Localization.Text("自定义", "Custom")), ("quota_week",Localization.Text("周额度周期", "Weekly quota period")), ("quota_month",Localization.Text("月额度周期", "Monthly quota period")) })
            if (Workspace.Feature==Feature.Overview || item.Item1 is not ("quota_week" or "quota_month")) RangePicker.Items.Add(new ComboBoxItem { Tag=item.Item1, Content=item.Item2 });
        }
        RangePicker.Width=Workspace.Feature==Feature.Overview?200:128;
        RangePicker.SelectedItem=RangePicker.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(string)item.Tag==Workspace.RangePreset);
        var quotaRange=Workspace.Feature==Feature.Overview && Workspace.RangePreset is "quota_week" or "quota_month";
        EnsureDatePickers();
        StartDate.Visibility=EndDate.Visibility=DateSeparator.Visibility=quotaRange?Visibility.Collapsed:Visibility.Visible;
        StartDate.Date = new DateTimeOffset(Workspace.Start.ToDateTime(TimeOnly.MinValue));
        EndDate.Date = new DateTimeOffset(Workspace.EndExclusive.AddDays(-1).ToDateTime(TimeOnly.MinValue));
        AutomationProperties.SetName(StartDate, Localization.Text("开始日期", "Start date"));
        AutomationProperties.SetName(EndDate, Localization.Text("结束日期（含）", "End date (inclusive)"));
        TimeZoneLabel.Text = Workspace.TimeZone;
    }

    private void EnsureDatePickers()
    {
        var language = Localization.Culture.Name;
        if (datePickerLanguage == language) return;
        // Changing Language/DateFormat on an opened WinUI calendar can cause a deferred native crash.
        // Configure replacement controls before attaching them; ordinary refreshes retain the controls.
        if (StartDate is not null) { StartDate.DateChanged -= DatesChanged; StartDate.IsCalendarOpen = false; }
        if (EndDate is not null) { EndDate.DateChanged -= DatesChanged; EndDate.IsCalendarOpen = false; }
        CalendarDatePicker Create(string automationId)
        {
            var picker = new CalendarDatePicker
            {
                MinWidth = 140,
                Language = language,
                DateFormat = Localization.Chinese ? "{year.full}/{month.integer(2)}/{day.integer(2)}" : "{month.integer(2)}/{day.integer(2)}/{year.full}"
            };
            AutomationProperties.SetAutomationId(picker, automationId);
            picker.DateChanged += DatesChanged;
            return picker;
        }
        StartDate = Create("start-date"); EndDate = Create("end-date");
        StartDateHost.Content = StartDate; EndDateHost.Content = EndDate;
        datePickerLanguage = language;
    }

    public static string ProviderName(string provider) => provider switch { "codex" => "Codex", "cursor" => "Cursor", "grok" => "Grok", "dsh" => "DSH", _ => provider };
    private void AddNavigation(Feature feature, Symbol symbol)
    {
        var item = new NavigationViewItem { Content = Localization.FeatureTitle(feature), Tag = feature, Icon = new SymbolIcon(symbol) };
        AutomationProperties.SetAutomationId(item, "nav-" + feature.ToString().ToLowerInvariant()); Navigation.MenuItems.Add(item);
    }
    private void NavigationChanged(NavigationView sender, NavigationViewSelectionChangedEventArgs args)
    { if (!applying && args.SelectedItemContainer?.Tag is Feature feature && feature != Workspace.Feature) Navigate(feature); }
    private void ProviderChanged(object sender, SelectionChangedEventArgs args)
    { if (!applying && ProviderPicker.SelectedItem is ComboBoxItem { Tag: string provider }) Workspace.SelectProvider(provider); }
    private void RangeChanged(object sender, SelectionChangedEventArgs args)
    {
        if (applying || RangePicker.SelectedItem is not ComboBoxItem {Tag:string preset}) return;
        if (preset=="custom") { Workspace.SetRange(Workspace.Start,Workspace.EndExclusive,Workspace.TimeZone); return; }
        Workspace.SetPreset(preset);
    }
    private void DatesChanged(CalendarDatePicker sender, CalendarDatePickerDateChangedEventArgs args)
    {
        if (applying || StartDate.Date is not { } start || EndDate.Date is not { } end) return;
        var first = DateOnly.FromDateTime(start.Date); var last = DateOnly.FromDateTime(end.Date).AddDays(1);
        if (last <= first || last.DayNumber-first.DayNumber > 366) { PageStatus.Text = Localization.Text("请选择有效范围（最多 366 天）", "Choose a valid range (up to 366 days)"); return; }
        applying = true; RangePicker.SelectedIndex = 3; applying = false;
        Workspace.SetRange(first, last, Workspace.TimeZone);
    }
    private async void LanguageChanged(object sender, SelectionChangedEventArgs args)
    {
        if (applying || localeSaving || LanguagePicker.SelectedItem is not ComboBoxItem { Tag: string locale } || Settings is null || locale == Localization.Locale) return;
        localeSaving = true; LanguagePicker.IsEnabled = false;
        try
        {
            var draft = SettingsDraft.From(Settings.Snapshot); draft.Ui.Locale = locale;
            var result = await Client.MutateAsync((client, deadline, cancellation) => client.UpdateSettingsAsync(draft, deadline: deadline, cancellationToken: cancellation));
            await ReloadSettingsAsync();
            if (Settings?.Snapshot.Ui.Locale != locale || result.Outcome == MutationOutcome.Unknown)
                ShowNotice("已重新读取设置；请核对语言是否生效。", "Settings were read back. Check whether the language changed.");
            await ReloadPageAsync();
        }
        catch (RpcException) { ShowNotice("语言保存失败，已保留原设置。", "Language could not be saved. The previous setting is retained."); await ReloadSettingsAsync(); }
        finally { localeSaving = false; LanguagePicker.IsEnabled = true; }
    }
    private void ThemeChanged(object sender, SelectionChangedEventArgs args)
    {
        if (applying || ThemePicker.SelectedItem is not ComboBoxItem { Tag: string theme }) return;
        preferences = preferences with { Theme = theme };
        Root.RequestedTheme = theme switch { "light" => ElementTheme.Light, "dark" => ElementTheme.Dark, _ => ElementTheme.Default };
        trayPanel?.UpdatePresentation();
        SaveWindowPreferences();
    }
    private async void RefreshClicked(object sender, RoutedEventArgs args) => await ReloadPageAsync(true);
    private void CancelClicked(object sender, RoutedEventArgs args) => page?.Cancel();
    private async void ReconnectClicked(object sender, RoutedEventArgs args) { startup = StartAsync(); await startup; }
    public void ShowApiCredentials(){ApiCredentialsRequested=true;Navigate(Feature.Settings);}
    private void OpenTrayClicked(object sender,RoutedEventArgs args)=>trayPanel?.Show(nativeTray?.Anchor()??new RectInt32(AppWindow.Position.X,AppWindow.Position.Y,1,1));
    private void HideClicked(object sender,RoutedEventArgs args){if(nativeTray?.Available==true)HideMain();}
    private async void QuitClicked(object sender,RoutedEventArgs args)=>await QuitAsync();
    private async void RefreshAccelerator(KeyboardAccelerator sender, KeyboardAcceleratorInvokedEventArgs args) { args.Handled = true; await ReloadPageAsync(true); }
    private void CancelAccelerator(KeyboardAccelerator sender, KeyboardAcceleratorInvokedEventArgs args)
    {
        if (VisualTreeHelper.GetOpenPopupsForXamlRoot(Root.XamlRoot).Count > 0 ||
            FocusManager.GetFocusedElement(Root.XamlRoot) is TextBox or PasswordBox or ComboBox or CalendarDatePicker) return;
        page?.Cancel(); args.Handled = true;
    }
    private void SaveWindowPreferences()
    {
        preferences = preferences with { Feature = Workspace.Feature.ToString(), Provider = Workspace.Provider, TrayProvider = Workspace.TrayProvider };
        try { preferences.Save(Options.Runtime); }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException or System.Security.SecurityException) { ShowNotice("窗口偏好无法保存。", "Window preferences could not be saved."); }
    }
    private async Task StopWatchAsync()
    {
        watchCancellation?.Cancel();
        if (watch is not null) await watch;
        watchCancellation?.Dispose(); watchCancellation = null; watch = null;
    }
    private async void ClosingWindow(AppWindow sender, AppWindowClosingEventArgs args)
    {
        if (allowClose) return;
        args.Cancel = true;
        if (closing) return;
        var scale = GetDpiForWindow(WinRT.Interop.WindowNative.GetWindowHandle(this)) / 96d;
        preferences = preferences with { Width = (int)(AppWindow.Size.Width/scale), Height = (int)(AppWindow.Size.Height/scale) };
        SaveWindowPreferences();
        if(nativeTray?.Available==true){HideMain();return;}
        await QuitAsync();
    }
    public async Task<bool> QuitAsync()
    {
        if(closing)return false;
        closing = true; invalidations.Stop(); page?.Dispose();subscriptions?.Dispose();SettingsEditor.Dispose();Reporting.Dispose();ApiSubscriptions.Dispose();WindowsUpdates.Dispose();trayPanel?.Hide();lifecycle.Stop();
        lifetime.Cancel();
        if (startup is not null) await startup;
        await StopWatchAsync();
        await lifecycle.Completion;
        var clean=true;
        if (Session is not null)
        {
            clean=await Session.StopAsync(updateNonce is null?"client_exit":"client_restart");
            try{using var key=Microsoft.Win32.Registry.CurrentUser.CreateSubKey(WindowPreferences.Key(Options.Runtime));key.SetValue("ShutdownDetail",JsonSerializer.Serialize(new{ExitCode=Session.ShutdownExitCode,Reason=Session.ShutdownReason}));}
            catch(Exception e)when(e is IOException or UnauthorizedAccessException or System.Security.SecurityException){clean=false;}
            try{await Session.DisposeAsync();}catch(IOException){clean=false;}
            Session=null;
        }
        RecordShutdown(clean?"clean":"uncertain");nativeTray?.Dispose();trayPanel?.Dispose();
        if(updateNonce is {} nonce){try{UpdateRuntime.Shutdown(Options.Runtime,nonce,Environment.ProcessId,clean);}catch(Exception error)when(error is IOException or UnauthorizedAccessException or System.Security.SecurityException){clean=false;}}
        allowClose = true; Close();
        Application.Current.Exit();return clean;
    }
    public Task<bool> QuitForUpdateAsync(string nonce){if(!Guid.TryParseExact(nonce,"N",out _))throw new ArgumentException("update_nonce_invalid");updateNonce=nonce;return QuitAsync();}
    private void RecordShutdown(string state)
    {try{using var key=Microsoft.Win32.Registry.CurrentUser.CreateSubKey(WindowPreferences.Key(Options.Runtime));key.SetValue("Shutdown",state);}catch(Exception e)when(e is IOException or UnauthorizedAccessException or System.Security.SecurityException){ShowNotice("退出状态无法持久化；更新须另行确认进程退出。","Shutdown status could not be saved; updates must independently confirm process exit.");}}
    private void EnsureWindowVisible()
    {
        if(!WindowVisible)return;var area=DisplayArea.GetFromWindowId(AppWindow.Id,DisplayAreaFallback.Nearest).WorkArea;var size=AppWindow.Size;var position=AppWindow.Position;
        var width=Math.Min(size.Width,area.Width);var height=Math.Min(size.Height,area.Height);
        AppWindow.MoveAndResize(new RectInt32(Math.Clamp(position.X,area.X,area.X+area.Width-width),Math.Clamp(position.Y,area.Y,area.Y+area.Height-height),width,height));
    }
    private void SetWindowBounds()
    {
        var area = DisplayArea.GetFromWindowId(AppWindow.Id, DisplayAreaFallback.Primary).WorkArea;
        var scale = GetDpiForWindow(WinRT.Interop.WindowNative.GetWindowHandle(this)) / 96d;
        var width = Math.Min((int)(Math.Clamp(preferences.Width,640,16384) * scale), area.Width - 32);
        var height = Math.Min((int)(Math.Clamp(preferences.Height,480,16384) * scale), area.Height - 32);
        AppWindow.MoveAndResize(new RectInt32(area.X + (area.Width-width)/2, area.Y + (area.Height-height)/2, width, height));
    }
    private void UpdateHeaderInset() => PageHeading.Margin = Navigation.DisplayMode == NavigationViewDisplayMode.Minimal ? new Thickness(48,0,0,0) : new Thickness(0);
    [DllImport("user32.dll")] private static extern uint GetDpiForWindow(IntPtr window);
    [DllImport("user32.dll")] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool ShowWindow(IntPtr window,int command);
}
