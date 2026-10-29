using System.Runtime.InteropServices;
using System.Text.Json;
using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.App.Views;
using CodexPulse.CoreClient;
using Grpc.Core;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;
using Microsoft.Win32;
using Windows.ApplicationModel.DataTransfer;
using Windows.Graphics;
using Windows.Graphics.Imaging;
using Windows.Storage.Streams;

namespace CodexPulse.App;

internal sealed record TrayPreferences(bool ShowCost=true,bool ShowProjects=true)
{
    public static TrayPreferences Load(string runtime)
    {using var key=Registry.CurrentUser.OpenSubKey(WindowPreferences.Key(runtime));var text=key?.GetValue("Tray") as string;if(text is null)return new();if(text.Length>4096)throw new IOException("tray_preferences_invalid");return JsonSerializer.Deserialize<TrayPreferences>(text)??new();}
    public void Save(string runtime){using var key=Registry.CurrentUser.CreateSubKey(WindowPreferences.Key(runtime));key.SetValue("Tray",JsonSerializer.Serialize(this),RegistryValueKind.String);}
}

// A separate tool window never activates the main window. Each query belongs to
// the visible tray Provider and Home/account context, independently of analytics.
internal sealed class TrayPanel:Window,IDisposable
{
    private sealed record Snapshot(string Provider,QuotaCurrentResponse? Quota,AccountSnapshotResponse? Account,QuotaPaceResponse? Pace,UsageCostResponse Usage,ProjectListResponse? Projects,UTCTimeRange Range,bool Fallback,long ReadAtMs);
    private readonly MainWindow host;
    private readonly QuerySlot<Snapshot> slot=new();
    private readonly StackPanel document=new(){Spacing=14,Padding=new Thickness(18)};
    private readonly Grid header=new(){ColumnSpacing=12,Padding=new Thickness(18,16,18,0)};
    private readonly Grid footer=new(){ColumnSpacing=8,Padding=new Thickness(18,12,18,16)};
    private readonly TextBlock feedback=PresentationControls.Text("");
    private readonly ComboBox picker=new(){HorizontalAlignment=HorizontalAlignment.Stretch};
    private readonly Border root;
    private readonly DispatcherTimer refresh=new(){Interval=TimeSpan.FromSeconds(30)};
    private TrayPreferences preferences;
    private bool visible,disposed,rendering,capturing,refreshing;
    private LoadKind presentedKind=LoadKind.Idle;
    private DateTimeOffset dismissed;
    private RectInt32 anchor;
    private string? activeProvider;
    public bool IsPanelVisible=>visible;
    public IntPtr Handle{get;}
    private Localization L=>host.Localization;
    public event Action<string>? TooltipChanged;
    public TrayPanel(MainWindow host)
    {
        this.host=host;Title="Codex Pulse · "+L.Text("托盘","Tray");Handle=WinRT.Interop.WindowNative.GetWindowHandle(this);
        try{preferences=TrayPreferences.Load(host.Options.Runtime);}catch(Exception e)when(e is IOException or UnauthorizedAccessException or JsonException or System.Security.SecurityException){preferences=new();}
        root=(Border)Microsoft.UI.Xaml.Markup.XamlReader.Load("<Border xmlns='http://schemas.microsoft.com/winfx/2006/xaml/presentation' Background='{ThemeResource ApplicationPageBackgroundThemeBrush}'/>");
        var layout=new Grid();layout.RowDefinitions.Add(new(){Height=GridLength.Auto});layout.RowDefinitions.Add(new(){Height=new GridLength(1,GridUnitType.Star)});layout.RowDefinitions.Add(new(){Height=GridLength.Auto});
        header.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});header.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
        footer.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});footer.ColumnDefinitions.Add(new(){Width=GridLength.Auto});footer.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
        layout.Children.Add(header);
        var scroll=new ScrollViewer{Content=document,HorizontalScrollBarVisibility=ScrollBarVisibility.Disabled,VerticalScrollBarVisibility=ScrollBarVisibility.Auto,HorizontalContentAlignment=HorizontalAlignment.Stretch};Grid.SetRow(scroll,1);layout.Children.Add(scroll);
        Grid.SetRow(footer,2);layout.Children.Add(footer);root.Child=layout;
        Content=root;
        var escape=new KeyboardAccelerator{Key=Windows.System.VirtualKey.Escape};escape.Invoked+=(_,e)=>{Hide();e.Handled=true;};root.KeyboardAccelerators.Add(escape);
        Activated+=(_,e)=>{if(e.WindowActivationState==WindowActivationState.Deactivated && visible && !capturing)Hide();else if(e.WindowActivationState!=WindowActivationState.Deactivated)host.QueueLifecycle("application_did_become_active");};
        AppWindow.Closing+=(_,e)=>{if(!disposed){e.Cancel=true;Hide();}};
        AppWindow.IsShownInSwitchers=false;
        var presenter=OverlappedPresenter.CreateForDialog();presenter.SetBorderAndTitleBar(false,false);presenter.IsResizable=false;presenter.IsMaximizable=false;presenter.IsMinimizable=false;presenter.IsAlwaysOnTop=true;AppWindow.SetPresenter(presenter);
        var style=GetWindowLongPtrW(Handle,-20).ToInt64();SetWindowLongPtrW(Handle,-20,new IntPtr((style|0x80)&~0x40000L));
        picker.SelectionChanged+=async(_,_)=>{if(rendering || picker.SelectedItem is not ComboBoxItem{Tag:string provider})return;host.SelectTrayProvider(provider);slot.Clear();await LoadAsync();};
        slot.Changed+=()=>host.Dispatch(Render);
        refresh.Tick+=async(_,_)=>{if(visible && !capturing)await LoadAsync();};
        Render();
    }
    public void Toggle(RectInt32 target)
    {
        if(visible){Hide();return;}
        if(DateTimeOffset.UtcNow-dismissed<TimeSpan.FromMilliseconds(180))return;
        Show(target);
    }
    public void Show(RectInt32 target)
    {
        if(disposed)return;anchor=target;visible=true;root.RequestedTheme=host.CurrentTheme;
        Position();Activate();refresh.Start();_ = LoadAsync();
    }
    public void Position()
    {
        if(!visible)return;
        var area=DisplayArea.GetFromRect(anchor,DisplayAreaFallback.Nearest).WorkArea;
        AppWindow.Move(new PointInt32(Math.Clamp(anchor.X,area.X,area.X+area.Width-1),Math.Clamp(anchor.Y,area.Y,area.Y+area.Height-1)));
        var scale=GetDpiForWindow(Handle)/96d;
        var width=Math.Min((int)(440*scale),area.Width-16);var height=Math.Min((int)(740*scale),area.Height-16);
        var x=Math.Clamp(anchor.X+anchor.Width-width,area.X+8,area.X+area.Width-width-8);
        var y=anchor.Y>=area.Y+area.Height/2?anchor.Y-height-8:anchor.Y+anchor.Height+8;
        y=Math.Clamp(y,area.Y+8,area.Y+area.Height-height-8);
        AppWindow.MoveAndResize(new RectInt32(x,y,width,height));
    }
    public void Hide(){if(!visible)return;visible=false;dismissed=DateTimeOffset.UtcNow;refresh.Stop();slot.Cancel();AppWindow.Hide();}
    public void Invalidate(string? domain)
    {
        if(domain is null or "settings" or "account"){slot.Clear();TooltipChanged?.Invoke("Codex Pulse");}
        if(visible && !capturing)_=LoadAsync();
    }
    public void UpdatePresentation()
    {
        root.RequestedTheme=host.CurrentTheme;
        if(activeProvider!=host.Workspace.TrayProvider){slot.Clear();if(visible && !capturing)_=LoadAsync();}
        else Render();
    }
    private async Task LoadAsync()
    {
        if(!visible || disposed || capturing)return;
        if(host.Session?.State!=CoreConnectionState.Connected){slot.Clear(host.Session?.State==CoreConnectionState.Recovery);Render();return;}
        var provider=host.Workspace.TrayProvider;
        if(provider is null){slot.Clear();Render();return;}
        activeProvider=provider;
        var context=host.Workspace.Context with{Provider=provider,Start=DateOnly.FromDateTime(DateTime.Today).AddDays(provider=="cursor"?-29:-6),EndExclusive=DateOnly.FromDateTime(DateTime.Today).AddDays(1),Preset=provider=="cursor"?"quota_month":provider=="dsh"?"seven_days":"quota_week"};
        await slot.LoadAsync((provider,context.Generation,context.TimeZone),async ct=>
        {
            var now=DateTimeOffset.Now.ToUnixTimeMilliseconds();QuotaCurrentResponse? quota=null;AccountSnapshotResponse? account=null;QuotaPaceResponse? pace=null;
            if(provider!="dsh")
            {
                var qr=new QuotaCurrentRequest{Provider=new(){Provider=provider},EvaluatedAtMs=now};
                var q=host.Client.QueryAsync("QuotaCurrent",qr,context,["quota","account"],(core,d,c)=>core.QuotaCurrentAsync(qr,deadline:d,cancellationToken:c),ct,true);
                Task<AccountSnapshotResponse?> a=Task.FromResult<AccountSnapshotResponse?>(null);
                if(provider is "codex" or "cursor" or "grok")
                {
                    var ar=new AccountSnapshotRequest{Provider=new(){Provider=provider},EvaluatedAtMs=now,TimeZone=context.TimeZone};
                    a=Optional(host.Client.QueryAsync("AccountSnapshot",ar,context,["account"],(core,d,c)=>core.AccountSnapshotAsync(ar,deadline:d,cancellationToken:c),ct,true));
                }
                var pr=new QuotaPaceRequest{Provider=new(){Provider=provider},EvaluatedAtMs=now};
                var p=host.Client.QueryAsync("QuotaPace",pr,context,["quota","account"],(core,d,c)=>core.QuotaPaceAsync(pr,deadline:d,cancellationToken:c),ct,true);
                var optionalQuota=Optional(q);var optionalAccount=a;var optionalPace=Optional(p);
                await Task.WhenAll(optionalQuota,optionalAccount,optionalPace);quota=await optionalQuota;account=await optionalAccount;pace=await optionalPace;
            }
            var resolution=DashboardPresentation.ResolveRange(context,quota?.Current,DateTimeOffset.Now);
            var ur=new UsageCostRequest{Provider=new(){Provider=provider},ExactRange=resolution.Exact.Clone(),Granularity=resolution.Granularity};
            var usageTask=host.Client.QueryAsync("UsageCost",ur,context,["index","account"],(core,d,c)=>core.UsageCostAsync(ur,deadline:d,cancellationToken:c),ct,true);
            Task<ProjectListResponse?> projectsTask=Task.FromResult<ProjectListResponse?>(null);
            if(preferences.ShowProjects)
            {
                var request=new ListProjectsRequest{Query=new(){Provider=new(){Provider=provider},ExactTimeRange=resolution.Exact.Clone(),Page=new(){Limit=5}}};
                request.Query.Sort.Add(new SortTerm{Field=provider=="cursor"?"lastActivityAt":"totalTokens",Direction="desc"});
                projectsTask=Optional(host.Client.QueryAsync("ListProjects",request,context,["index","account"],(core,d,c)=>core.ListProjectsAsync(request,deadline:d,cancellationToken:c),ct,true));
            }
            await Task.WhenAll(usageTask,projectsTask);
            return new Snapshot(provider,quota,account,pace,await usageTask,await projectsTask,resolution.Exact,resolution.Fallback,DateTimeOffset.Now.ToUnixTimeMilliseconds());
            async Task<T?> Optional<T>(Task<T> task) where T:class
            {
                try{return await task;}
                catch(Exception e)when(e is RpcException or IOException){ct.ThrowIfCancellationRequested();return null;}
            }
        },value=>DashboardPresentation.Kind(value.Usage.Meta));
    }
    private Button Action(string zh,string en,Action action)
    {var button=new Button{Content=L.Text(zh,en)};button.Click+=(_,_)=>action();return button;}
    private void Render()=>Render(capturing);
    private void Render(bool privacy)
    {
        if(disposed)return;rendering=true;root.Language=L.Culture.Name;
        try
        {
            document.Children.Clear();header.Children.Clear();footer.Children.Clear();
            picker.Header=null;AutomationProperties.SetName(picker,L.Text("托盘客户端","Tray client"));
            picker.Items.Clear();foreach(var provider in host.Workspace.EnabledProviders)picker.Items.Add(new ComboBoxItem{Content=MainWindow.ProviderName(provider),Tag=provider});
            picker.SelectedItem=picker.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(string)item.Tag==host.Workspace.TrayProvider);
            var title=PresentationControls.Heading("Codex Pulse");title.FontSize=16;title.VerticalAlignment=VerticalAlignment.Center;header.Children.Add(title);
            picker.MinWidth=120;picker.MaxWidth=160;Grid.SetColumn(picker,1);header.Children.Add(picker);
            if(slot.State.Kind!=LoadKind.Loading || slot.State.Value is null)presentedKind=slot.State.Kind;
            if(presentedKind!=LoadKind.Ready)document.Children.Add(PresentationControls.Caption(L.State(presentedKind)));
            if(!string.IsNullOrEmpty(feedback.Text))document.Children.Add(feedback);
            if(slot.State.Value is {} value && value.Provider==host.Workspace.TrayProvider)
            {
                var trusted=TrayPresentation.CanShowQuota(value.Provider,value.Quota,value.Account);
                if(value.Provider!="dsh")document.Children.Add(PresentationControls.Text(value.Provider!="codex" || trusted?TrayPresentation.Identity(value.Provider,value.Account,privacy,L):L.Text("正在确认账号；在线额度暂不展示。","Confirming account; online quota withheld.")));
                if(trusted && value.Quota?.Current is {} quota)
                {
                    foreach(var window in QuotaPresentation.Windows(quota))
                    {
                        var card=new StackPanel{Spacing=6};var name=QuotaPresentation.WindowName(window.LimitId,window.HasLimitName?window.LimitName:null,window.HasWindowMinutes?window.WindowMinutes:null,L);
                        var expired=window.HasResetsAtMs && DateTimeOffset.Now.ToUnixTimeMilliseconds()>=window.ResetsAtMs;
                        var remaining=expired?null:QuotaPresentation.Percent(window.HasRemainingPercent,window.RemainingPercent);
                        var quotaHeading=new Grid {ColumnSpacing=12};quotaHeading.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});quotaHeading.ColumnDefinitions.Add(new(){Width=GridLength.Auto});
                        var label=PresentationControls.Text(name,14);label.VerticalAlignment=VerticalAlignment.Center;quotaHeading.Children.Add(label);
                        var number=PresentationControls.Text(QuotaPresentation.PercentText(remaining,L),24);number.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold;Grid.SetColumn(number,1);quotaHeading.Children.Add(number);card.Children.Add(quotaHeading);QuotaPage.AddProgress(card,name,remaining);
                        card.Children.Add(PresentationControls.Text((expired?L.Text("等待新周期","Awaiting new cycle"):QuotaPresentation.Freshness(window.Freshness,L))+" · "+L.Text("重置 ","Resets ")+L.Timestamp(window.HasResetsAtMs?window.ResetsAtMs:null),12));document.Children.Add(PresentationControls.Card(card));
                    }
                    if(quota.ResetCredits is {} credits)document.Children.Add(PresentationControls.Text(L.Text("可用重置库存：","Available reset inventory: ")+(credits.HasAvailableCount?credits.AvailableCount.ToString(L.Culture):"—")+" · "+QuotaPresentation.Freshness(credits.Freshness,L)));
                    if(value.Pace?.Pace is {} pace && (value.Provider!="codex" || QuotaPresentation.SameAccount(quota.Binding,pace.Binding)))
                        foreach(var window in pace.Windows)document.Children.Add(PresentationControls.Disclosure(L.Text("额度节奏 · ","Quota pace · ")+QuotaPresentation.WindowName(window.LimitId,null,window.HasWindowMinutes?window.WindowMinutes:null,L),PaceChart.Create(window,L)));
                }
                else if(value.Provider=="dsh")document.Children.Add(PresentationControls.Text(L.Text("DSH 不提供官方额度。","DSH does not provide an official quota.")));
                else document.Children.Add(PresentationControls.Text(L.Text("在线额度暂不可用；Home 用量仍可独立查看。","Online quota is unavailable; Home usage remains independent.")));
                var rangeLabel=value.Provider=="dsh"?L.Text("最近 7 天的 Home 用量","Home usage in the last 7 days"):value.Fallback?value.Provider=="cursor"?L.Text("额度范围不可用，显示最近 30 天。","Quota period unavailable; showing the last 30 days."):L.Text("额度范围不可用，显示最近 7 天。","Quota period unavailable; showing the last 7 days."):L.Text("当前额度周期的 Home 用量","Home usage in the current quota period");
                document.Children.Add(PresentationControls.Text(rangeLabel+"\n"+L.Timestamp(value.Range.StartAtMs,value.Range.TimeZone)+" — "+L.Timestamp(value.Range.EndAtMs,value.Range.TimeZone),12));
                document.Children.Add(PresentationControls.Text("Token · "+L.Tokens(value.Usage.Totals?.TotalTokens),24));
                if(preferences.ShowCost)document.Children.Add(PresentationControls.Text(L.Text("API 等价估算 · ","API-equivalent estimate · ")+L.Money(value.Usage.Totals?.EstimatedUsdMicros)+L.Text("（非订阅账单）"," (not a subscription bill)")));
                document.Children.Add(Charts.Bars(DashboardPresentation.UsageTrend(value.Usage),L,"Token",true,compact:true));
                if(preferences.ShowProjects && value.Projects is {} projects)
                {
                    document.Children.Add(PresentationControls.Heading(L.Text("项目摘要 · 前 5","Project summary · Top 5")));
                    var ranking=new StackPanel {Spacing=2};var rank=0;
                    foreach(var item in projects.Items)
                    {
                        ranking.Children.Add(PresentationControls.RankingLink(++rank,DashboardPresentation.Name(item.Project,L.Text("未归类","Unattributed")),
                            L.Number(item.SessionCount)+L.Text(" 个会话",item.SessionCount is {HasValue:true,Value:1}?" session":" sessions"),L.Tokens(item.Totals?.TotalTokens)+" Token","",
                            ()=>{Hide();host.OpenProvider(value.Provider);host.OpenProject(item.DimensionKey,value.Range);host.ShowMain();}));
                    }
                    document.Children.Add(ranking);
                }
                else if(preferences.ShowProjects)document.Children.Add(PresentationControls.Text(L.Text("项目摘要暂不可用。","Project summary is unavailable.")));
                document.Children.Add(PresentationControls.Text(L.Text("快照读取时间 ","Snapshot read ")+L.Timestamp(value.ReadAtMs)+" · "+L.Text("Home 用量不归属当前账号","Home usage is independent of account identity"),12));
                var tooltip="Codex Pulse · "+MainWindow.ProviderName(value.Provider)+" · "+L.State(slot.State.Kind)+" · Token "+L.Tokens(value.Usage.Totals?.TotalTokens,compact:true);
                if(trusted && value.Quota is {} q)tooltip+=" · "+string.Join(" / ",QuotaPresentation.Windows(q.Current).Take(2).Select(w=>QuotaPresentation.PercentText(QuotaPresentation.Percent(w.HasRemainingPercent,w.RemainingPercent),L)));
                TooltipChanged?.Invoke(tooltip+" · "+L.Timestamp(value.ReadAtMs));
            }
            else if(slot.State.Kind!=LoadKind.Loading)document.Children.Add(PresentationControls.Text(host.Session?.State==CoreConnectionState.Recovery?L.Text("恢复模式：请打开主窗口。","Recovery mode: open the main window."):L.Text("尚无可信快照。可启用客户端或重新连接。","No trusted snapshot. Enable a client or reconnect.")));
            var open=Action("打开主窗口","Open main",()=>{Hide();host.ShowMain();});open.HorizontalAlignment=HorizontalAlignment.Stretch;footer.Children.Add(open);
            var reload=Action("刷新额度","Refresh quota",()=>{_=RefreshQuotaAsync();});reload.Content=new SymbolIcon(Symbol.Refresh);AutomationProperties.SetName(reload,L.Text("刷新额度","Refresh quota"));ToolTipService.SetToolTip(reload,L.Text("刷新额度","Refresh quota"));reload.IsEnabled=!refreshing && host.Workspace.TrayProvider is not(null or "dsh");Grid.SetColumn(reload,1);footer.Children.Add(reload);
            var menu=new MenuFlyout();
            var copy=new MenuFlyoutItem {Text=L.Text("复制脱敏截图","Copy private screenshot"),IsEnabled=!capturing};copy.Click+=(_,_)=>{_=CopyAsync();};menu.Items.Add(copy);
            var settings=new MenuFlyoutItem {Text=L.Text("设置","Settings")};settings.Click+=(_,_)=>{Hide();host.Navigate(Feature.Settings);host.ShowMain();};menu.Items.Add(settings);
            menu.Items.Add(new MenuFlyoutSeparator());var quit=new MenuFlyoutItem {Text=L.Text("退出","Quit")};quit.Click+=(_,_)=>{_=host.QuitAsync();};menu.Items.Add(quit);
            var more=new DropDownButton {Content=new SymbolIcon(Symbol.More),Flyout=menu};AutomationProperties.SetName(more,L.Text("更多操作","More actions"));Grid.SetColumn(more,2);footer.Children.Add(more);
            var options=new StackPanel{Spacing=8};var cost=new ToggleSwitch{Header=L.Text("显示成本摘要","Show cost summary"),IsOn=preferences.ShowCost};cost.Toggled+=async(_,_)=>{preferences=preferences with{ShowCost=cost.IsOn};SavePreferences();if(!capturing)await LoadAsync();};options.Children.Add(cost);
            var ranks=new ToggleSwitch{Header=L.Text("显示项目摘要","Show project summary"),IsOn=preferences.ShowProjects};ranks.Toggled+=async(_,_)=>{preferences=preferences with{ShowProjects=ranks.IsOn};SavePreferences();await LoadAsync();};options.Children.Add(ranks);document.Children.Add(PresentationControls.Disclosure(L.Text("显示设置","Display settings"),options));
        }
        finally{rendering=false;}
    }
    private void SavePreferences(){try{preferences.Save(host.Options.Runtime);}catch(Exception e)when(e is IOException or UnauthorizedAccessException or System.Security.SecurityException){feedback.Text=L.Text("显示偏好未保存。","Display preferences were not saved.");}}
    private async Task RefreshQuotaAsync()
    {
        if(refreshing || host.Workspace.TrayProvider is not {} provider || provider=="dsh")return;refreshing=true;var generation=host.Workspace.Generation;Render();
        try
        {
            var request=new QuotaRefreshRequest{Provider=new(){Provider=provider},Source="quota"};var result=await host.Client.MutateAsync((core,d,ct)=>core.RequestQuotaRefreshAsync(request,deadline:d,cancellationToken:ct));
            if(provider!=host.Workspace.TrayProvider || generation!=host.Workspace.Generation){feedback.Text=L.Text("上下文已变化，正在重读当前快照。","Context changed; reading the current snapshot.");await LoadAsync();return;}
            if(result.Receipt?.ProviderContext?.EffectiveProvider is {} received && received!=provider){feedback.Text=L.Text("回执客户端不一致，请重新读取。","Receipt client differs. Reload.");await LoadAsync();return;}
            feedback.Text=result.Receipt is {} receipt?receipt.Fetched?L.Text("已取得数据，重新读取快照。","Data fetched; reading snapshot."):L.Text("本次未获取新数据：","No new data: ")+receipt.Reason:L.Text("刷新结果未知；只回读，不重发。","Refresh outcome unknown; readback only, no replay.");await LoadAsync();
        }
        catch(Exception e)when(e is RpcException or IOException){feedback.Text=L.Text("刷新未完成；请重读。","Refresh incomplete. Reload.");}
        finally{refreshing=false;Render();}
    }
    private async Task CopyAsync()
    {
        if(capturing || !visible)return;capturing=true;
        try
        {
            Render(true);document.UpdateLayout();
            if(document.ActualWidth<=0 || document.ActualHeight<=0 || document.ActualWidth*document.ActualHeight>8_000_000)throw new IOException("capture_size_invalid");
            var image=new RenderTargetBitmap();await image.RenderAsync(document);
            var pixels=await image.GetPixelsAsync();using var stream=new InMemoryRandomAccessStream();
            var encoder=await BitmapEncoder.CreateAsync(BitmapEncoder.PngEncoderId,stream);var data=new byte[pixels.Length];using(var reader=DataReader.FromBuffer(pixels))reader.ReadBytes(data);
            encoder.SetPixelData(BitmapPixelFormat.Bgra8,BitmapAlphaMode.Premultiplied,(uint)image.PixelWidth,(uint)image.PixelHeight,96,96,data);await encoder.FlushAsync();stream.Seek(0);
            var package=new DataPackage{RequestedOperation=DataPackageOperation.Copy};package.SetText(TrayPresentation.ClipboardText);package.SetBitmap(RandomAccessStreamReference.CreateFromStream(stream));Clipboard.SetContent(package);Clipboard.Flush();
            feedback.Text=L.Text("脱敏截图已复制。","Private screenshot copied.");
        }
        catch(Exception e)when(e is IOException or COMException or ArgumentException or UnauthorizedAccessException){feedback.Text=L.Text("脱敏截图复制失败；未回退复制原文。","Private capture failed; original content was not copied.");}
        finally{capturing=false;Render();}
    }
    public void Dispose(){if(disposed)return;disposed=true;refresh.Stop();slot.Dispose();Close();}
    [DllImport("user32.dll")]private static extern uint GetDpiForWindow(IntPtr window);
    [DllImport("user32.dll",EntryPoint="GetWindowLongPtrW")]private static extern IntPtr GetWindowLongPtrW(IntPtr window,int index);
    [DllImport("user32.dll",EntryPoint="SetWindowLongPtrW")]private static extern IntPtr SetWindowLongPtrW(IntPtr window,int index,IntPtr value);
}
