using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class SettingsPage:FeaturePage
{
    private readonly SettingsEditor settings;
    private readonly CodexAccountsSection accounts;
    private readonly HomeSection home;
    private readonly StartupSection startup;
    private readonly ReportingSection reporting;
    private readonly ApiCredentialsSection apiCredentials;
    private readonly StackPanel form=new(){Spacing=14};
    private readonly TextBlock result;
    private readonly Button save,rebase,discard;
    private UpdateSettingsRequest? rendered;
    private bool disposed;
    private readonly HashSet<NumberBox> invalid=[];
    private readonly Dictionary<Control,bool> editable=[];
    public SettingsPage(MainWindow host):base(host)
    {
        settings=host.SettingsEditor;result=Text("");Body.Children.Add(result);
        var actions=new StackPanel{Spacing=8,Orientation=Orientation.Horizontal};
        save=new(){Content=L.Text("保存更改","Save changes")};save.Click+=async(_,_)=>{await settings.SaveAsync();await Host.ReloadSettingsAsync();};
        rebase=new(){Content=L.Text("保留改动并载入新版本","Rebase edited fields")};rebase.Click+=(_,_)=>settings.Rebase();
        discard=new(){Content=L.Text("放弃草稿","Discard draft")};discard.Click+=(_,_)=>settings.Reset();
        actions.Children.Add(save);actions.Children.Add(rebase);actions.Children.Add(discard);Body.Children.Add(new ScrollViewer{Content=actions,HorizontalScrollBarVisibility=ScrollBarVisibility.Auto,VerticalScrollBarVisibility=ScrollBarVisibility.Disabled});
        Body.Children.Add(form);
        accounts=new(host);Body.Children.Add(PresentationControls.Disclosure(L.Text("Codex 账号与订阅 · 独立即时保存","Codex accounts & subscriptions · Independent immediate changes"),accounts.View));
        home=new(host);Body.Children.Add(PresentationControls.Disclosure(L.Text("Codex Home 与切换恢复","Codex Home and switch recovery"),home.View));
        Body.Children.Add(PresentationControls.Disclosure(L.Text("Windows 程序路径","Windows executable paths"),PlatformPaths()));
        startup=new(host);Body.Children.Add(PresentationControls.Disclosure(L.Text("Windows 登录启动","Windows login startup"),startup.View));
        reporting=new(host);Body.Children.Add(reporting.View);
        apiCredentials=new(host);var apiPanel=PresentationControls.Disclosure(L.Text("API Key · 独立即时保存","API keys · Independent immediate changes"),apiCredentials.View);if(apiPanel is Expander apiExpander)apiExpander.IsExpanded=host.ApiCredentialsRequested;if(host.ApiCredentialsRequested)apiPanel.Loaded+=(_,_)=>apiPanel.StartBringIntoView(new BringIntoViewOptions{VerticalAlignmentRatio=0});Body.Children.Add(apiPanel);
        var api=new Button{Content=L.Text("管理 API 与订阅","Manage API & subscriptions")};api.Click+=(_,_)=>Host.Navigate(Feature.ApiSubscriptions);Body.Children.Add(api);
        var status=new Button{Content=L.Text("查看本机状态与数据健康","View local status and Data Health")};status.Click+=(_,_)=>Host.Navigate(Feature.LocalStatus);Body.Children.Add(status);
        settings.Changed+=Changed;settings.Current.Changed+=Changed;Publish();
    }
    public override bool DependsOn(string domain)=>domain is "settings" or "account";
    public override async Task LoadAsync(bool force=false)
    {await Task.WhenAll(settings.ReloadAsync(),accounts.LoadAsync(),reporting.LoadAsync(),apiCredentials.LoadAsync());if(!disposed){Publish();home.Publish();startup.Read();State(settings.Current.State.Kind);}}
    private void Changed()=>Host.Dispatch(Publish);
    private void Publish()
    {
        if(disposed)return;
        result.Text=(settings.Dirty?L.Text("有未保存的更改。 ","Unsaved changes. "):"")+L.State(settings.Current.State.Kind)+" · "+(settings.Action switch
        {SettingsAction.Saving=>L.Text("正在保存并读回","Saving and reading back"),SettingsAction.Applied=>L.Text("已保存并权威读回","Saved and read back"),SettingsAction.ReconcileRequired=>L.Text("设置已保存，运行状态需要对账","Settings saved; runtime reconciliation required"),SettingsAction.Conflict=>L.Text("版本冲突；草稿已保留，请重查后合并新版本","Revision conflict; draft retained. Reload and rebase"),SettingsAction.ReadbackConfirmed=>L.Text("回执未知，最新设置已确认目标值","Receipt unknown; latest settings confirm the requested values"),SettingsAction.ReadbackChanged=>L.Text("最新设置与提交值不同，草稿已保留","Latest settings differ; draft retained"),SettingsAction.Unknown=>L.Text("结果未确认，请重查，勿重复提交","Result unconfirmed. Reload before submitting again"),SettingsAction.Unavailable=>L.Text("保存未完成，请核对填写内容","Save incomplete. Check the entered values"),_=>""});
        var response=settings.Current.State.Value??Host.Settings;
        if(invalid.Count>0)result.Text+=" · "+L.Text("请为周期填写范围内的整数。","Enter whole-number intervals within their allowed ranges.");
        save.IsEnabled=!settings.Busy && settings.Dirty && invalid.Count==0 && response?.Snapshot.Home.SwitchStatus=="stable";
        rebase.IsEnabled=discard.IsEnabled=!settings.Busy && settings.Current.State.Kind==LoadKind.Ready;
        if(settings.Draft is not {} draft || response is null)return;
        if(ReferenceEquals(rendered,draft)){Enable(form,!settings.Busy);return;}
        rendered=draft;form.Children.Clear();invalid.Clear();editable.Clear();
        EditableField? Field(string key)=>response.EditableFields.FirstOrDefault(field=>field.Key==key);
        var providers=new StackPanel{Spacing=10};providers.Children.Add(Text(L.Text("客户端采集开关","Client collection switches"),"SubtitleTextBlockStyle"));
        foreach(var state in response.Snapshot.Providers)
        {
            var providerDraft=draft.Providers.FirstOrDefault(item=>item.Provider==state.Provider);if(providerDraft is null)continue;
            var choice=new ComboBox{Header=MainWindow.ProviderName(state.Provider)+" · "+RuntimeCopy.Discovery(state.DiscoveryState,L)+" / "+RuntimeCopy.Effective(state.EffectiveState,L),HorizontalAlignment=HorizontalAlignment.Stretch,IsEnabled=Field("providers."+state.Provider+".intent")?.Editable==true};
            editable[choice]=choice.IsEnabled;
            if(providerDraft.Intent==ProviderIntent.Auto)choice.Items.Add(new ComboBoxItem{Content=L.Text("自动（保留当前设置）","Auto (keep current setting)"),Tag=ProviderIntent.Auto});
            choice.Items.Add(new ComboBoxItem{Content=L.Text("启用","Enabled"),Tag=ProviderIntent.Enabled});choice.Items.Add(new ComboBoxItem{Content=L.Text("关闭","Disabled"),Tag=ProviderIntent.Disabled});
            choice.SelectedItem=choice.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(ProviderIntent)item.Tag==providerDraft.Intent);
            choice.SelectionChanged+=(_,_)=>{if(choice.SelectedItem is ComboBoxItem{Tag:ProviderIntent value}){providerDraft.Intent=value;settings.Edited();}};providers.Children.Add(choice);
            if(!string.IsNullOrEmpty(state.ReasonCode))providers.Children.Add(Text(RuntimeCopy.Availability(state.ReasonCode,L)));
        }
        form.Children.Add(PresentationControls.Card(providers));
        var online=new StackPanel{Spacing=12};
        Toggle(online,"online.quotaEnabled",L.Text("Codex 在线额度","Codex online quota"),draft.Online.QuotaEnabled,value=>draft.Online.QuotaEnabled=value);
        Toggle(online,"online.resetCreditsEnabled",L.Text("Codex 重置库存","Codex reset credits"),draft.Online.ResetCreditsEnabled,value=>draft.Online.ResetCreditsEnabled=value);
        Toggle(online,"online.cursorOnlineEnabled",L.Text("Cursor 在线用量","Cursor online usage"),draft.Online.CursorOnlineEnabled,value=>draft.Online.CursorOnlineEnabled=value);
        Toggle(online,"online.grokQuotaEnabled",L.Text("Grok 在线额度","Grok online quota"),draft.Online.GrokQuotaEnabled,value=>draft.Online.GrokQuotaEnabled=value);
        Toggle(online,"online.grokAutoRefreshEnabled",L.Text("Grok 自动刷新","Grok automatic refresh"),draft.Online.GrokAutoRefreshEnabled,value=>draft.Online.GrokAutoRefreshEnabled=value);
        online.Children.Add(Text(L.Text("关闭后停止对应新请求，已有结构化历史保持保留。","Disabling stops new requests for that source; existing structured history is retained.")));form.Children.Add(PresentationControls.Disclosure(L.Text("在线来源设置","Online source settings"),online));
        var intervals=new StackPanel{Spacing=12};
        Number(intervals,"refresh.quotaIntervalSeconds",L.Text("额度刷新周期（秒）","Quota interval (seconds)"),draft.Refresh.QuotaIntervalSeconds,value=>draft.Refresh.QuotaIntervalSeconds=value);
        Number(intervals,"refresh.resetCreditsIntervalSeconds",L.Text("重置库存刷新周期（秒）","Reset-credit interval (seconds)"),draft.Refresh.ResetCreditsIntervalSeconds,value=>draft.Refresh.ResetCreditsIntervalSeconds=value);
        Number(intervals,"refresh.reconcileIntervalSeconds",L.Text("来源对账周期（秒）","Reconcile interval (seconds)"),draft.Refresh.ReconcileIntervalSeconds,value=>draft.Refresh.ReconcileIntervalSeconds=value);
        Number(intervals,"refresh.jsonlDebounceMilliseconds",L.Text("JSONL 防抖（毫秒）","JSONL debounce (milliseconds)"),draft.Refresh.JsonlDebounceMilliseconds,value=>draft.Refresh.JsonlDebounceMilliseconds=value);
        form.Children.Add(PresentationControls.Disclosure(L.Text("刷新周期","Refresh intervals"),intervals));
        var ui=new StackPanel{Spacing=12};
        Choice(ui,"ui.locale",L.Text("语言","Language"),draft.Ui.Locale,value=>draft.Ui.Locale=value);
        Choice(ui,"ui.launchBehavior",L.Text("启动行为","Launch behavior"),draft.Ui.LaunchBehavior,value=>draft.Ui.LaunchBehavior=value);
        Choice(ui,"ui.overviewRange",L.Text("概览默认范围","Default overview range"),draft.Ui.OverviewRange,value=>draft.Ui.OverviewRange=value);
        Toggle(ui,"updates.autoCheckEnabled",L.Text("自动检查更新","Check for updates automatically"),draft.Updates.AutoCheckEnabled,value=>draft.Updates.AutoCheckEnabled=value);
        Choice(ui,"updates.channel",L.Text("更新通道","Update channel"),draft.Updates.Channel,value=>draft.Updates.Channel=value);
        Number(ui,"updates.checkIntervalSeconds",L.Text("更新检查周期（秒）","Update-check interval (seconds)"),draft.Updates.CheckIntervalSeconds,value=>draft.Updates.CheckIntervalSeconds=value);
        ui.Children.Add(Text(L.Text("自动下载关闭；Windows 安装更新状态由客户端单独管理。","Automatic download is disabled; Windows installation updates are managed by the client.")));form.Children.Add(PresentationControls.Disclosure(L.Text("界面与更新偏好","Interface and update preferences"),ui));
        Enable(form,!settings.Busy);
        void Toggle(StackPanel panel,string key,string label,bool value,Action<bool> change)
        {var control=new ToggleSwitch{Header=label,IsOn=value,IsEnabled=Field(key)?.Editable==true};editable[control]=control.IsEnabled;control.Toggled+=(_,_)=>{change(control.IsOn);settings.Edited();};panel.Children.Add(control);}
        void Number(StackPanel panel,string key,string label,long value,Action<long> change)
        {
            var field=Field(key);var control=new NumberBox{Header=label,Value=value,IsEnabled=field?.Editable==true,Minimum=field?.HasMinimum==true?field.Minimum:double.NegativeInfinity,Maximum=field?.HasMaximum==true?field.Maximum:double.PositiveInfinity,SpinButtonPlacementMode=NumberBoxSpinButtonPlacementMode.Compact};
            editable[control]=control.IsEnabled;
            control.ValueChanged+=(_,_)=>{if(double.IsFinite(control.Value) && control.Value==Math.Truncate(control.Value) && control.Value>=control.Minimum && control.Value<=control.Maximum){invalid.Remove(control);change((long)control.Value);}else invalid.Add(control);settings.Edited();};panel.Children.Add(control);
        }
        void Choice(StackPanel panel,string key,string label,string value,Action<string> change)
        {
            var field=Field(key);var control=new ComboBox{Header=label,IsEnabled=field?.Editable==true,HorizontalAlignment=HorizontalAlignment.Stretch};
            editable[control]=control.IsEnabled;
            foreach(var option in field?.Options??[])control.Items.Add(new ComboBoxItem{Tag=option,Content=Option(option)});
            control.SelectedItem=control.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(string)item.Tag==value);control.SelectionChanged+=(_,_)=>{if(control.SelectedItem is ComboBoxItem{Tag:string selected}){change(selected);settings.Edited();}};panel.Children.Add(control);
        }
    }
    private string Option(string value)=>value switch{"system"=>L.Text("系统语言","System language"),"zh-CN"=>"简体中文","en-US"=>"English","main_window"=>L.Text("主窗口","Main window"),"tray"=>L.Text("托盘","Tray"),"quota_week"=>L.Text("周额度周期","Weekly quota period"),"today"=>L.Text("今天","Today"),"seven_days"=>L.Text("最近 7 天","Last 7 days"),"thirty_days"=>L.Text("最近 30 天","Last 30 days"),"stable"=>L.Text("稳定版","Stable"),"prerelease"=>L.Text("预发布","Prerelease"),_=>value};
    private void Enable(UIElement element,bool enabled)
    {if(element is Control control && element is not Expander)control.IsEnabled=enabled && (!editable.TryGetValue(control,out var allowed) || allowed);if(element is Panel panel)foreach(var item in panel.Children)Enable(item,enabled);if(element is Border{Child:{} child})Enable(child,enabled);if(element is Expander{Content:UIElement content})Enable(content,enabled);}
    private FrameworkElement PlatformPaths()
    {
        var panel=new StackPanel{Spacing=12};panel.Children.Add(Text(L.Text("留空使用自动发现。这里只保存可执行文件路径，不读取或保存 Agent 凭据；应用后会重新连接 Helper。","Blank uses automatic discovery. Only executable paths are saved; Agent credentials are not accessed. Applying reconnects the Helper.")));
        var codex=new TextBox{Header=L.Text("Codex 原生程序（绝对 .exe 路径）","Codex native executable (absolute .exe path)"),Text=Environment.GetEnvironmentVariable("CODEX_PULSE_CODEX_BINARY")??""};
        var node=new TextBox{Header=L.Text("Node 程序（npm 回退使用）","Node executable (npm fallback)"),Text=Environment.GetEnvironmentVariable("CODEX_PULSE_NODE_BINARY")??""};panel.Children.Add(codex);panel.Children.Add(node);
        var feedback=Text("");var apply=new Button{Content=L.Text("保存路径并重新连接…","Save paths and reconnect…")};apply.Click+=async(_,_)=>
        {
            if(!await ProductDialog.Confirm(Host,L.Text("重新连接本机服务","Reconnect local service"),L.Text("将排空并停止当前 Helper，然后用新程序路径重新启动。源文件保持只读。","Drain and stop the current Helper, then start with the new executable paths. Source files remain read-only."),L.Text("保存并重新连接","Save and reconnect")) || disposed)return;
            apply.IsEnabled=false;
            try{var preferences=new LaunchPreferences(codex.Text.Trim(),node.Text.Trim());preferences.Save(Host.Options.Runtime);preferences.Apply();await Host.StartAsync();}
            catch(Exception error)when(error is IOException or UnauthorizedAccessException or ArgumentException or System.Security.SecurityException){if(!disposed)feedback.Text=L.Text("路径保存未完成，请选择现有的绝对 .exe 路径。","Paths could not be saved. Choose existing absolute .exe paths.");}
            finally{if(!disposed)apply.IsEnabled=true;}
        };panel.Children.Add(apply);panel.Children.Add(feedback);return panel;
    }
    public override void Cancel()=>settings.Current.Cancel();
    public override void Dispose(){disposed=true;settings.Changed-=Changed;settings.Current.Changed-=Changed;accounts.Dispose();home.Dispose();reporting.Dispose();apiCredentials.Dispose();}
}
