using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class ReportingSection:IDisposable
{
    public Expander View{get;}=new(){HorizontalAlignment=HorizontalAlignment.Stretch,HorizontalContentAlignment=HorizontalAlignment.Stretch};
    private readonly MainWindow host;
    private readonly ReportingController controller;
    private readonly StackPanel body=new(){Spacing=14},facts=new(){Spacing=8};
    private readonly Dictionary<string,TextBlock> factRows=[];
    private readonly TextBlock result=PresentationControls.Text("");
    private readonly TextBox endpoint=new();
    private readonly PasswordBox code=new(){MaxLength=64,PasswordRevealMode=PasswordRevealMode.Hidden};
    private readonly ToggleSwitch allowHttp=new(),enabled=new(),allHistory=new();
    private readonly NumberBox interval=new(){Minimum=15,Maximum=3600,SmallChange=15,SpinButtonPlacementMode=NumberBoxSpinButtonPlacementMode.Compact};
    private readonly CalendarDatePicker date=new(){MinDate=new DateTimeOffset(1970,1,1,0,0,0,TimeSpan.Zero),MaxDate=DateTimeOffset.Now};
    private readonly TimePicker time=new(){ClockIdentifier="24HourClock"};
    private readonly Button pair=new(),save=new(),sync=new(),full=new(),clear=new(),reset=new(),open=new();
    private readonly DispatcherTimer poll=new(){Interval=TimeSpan.FromSeconds(5)};
    private ReportingDraft? rendered;
    private bool applying,disposed,actionRunning;
    private Localization L=>host.Localization;
    public ReportingSection(MainWindow host)
    {
        this.host=host;controller=host.Reporting;
        View.Header=L.Text("多机中心与同步","Center & synchronization");View.Content=body;
        body.Children.Add(PresentationControls.Text(L.Text("上报结构化用量、额度、Reset Credits、缓存/TPS，以及账号 ID/邮箱、项目名和会话标题/原始 Session ID。原始 JSONL、正文、工具内容、API key、Cookie 与 Agent 凭据留在本机。","Report structured usage, quota, Reset Credits, cache/TPS, account IDs/email, project names and session titles/IDs. Raw JSONL, content, tool data, API keys, cookies and Agent credentials stay local.")));
        body.Children.Add(result);
        endpoint.Header=L.Text("中心地址","Center address");endpoint.PlaceholderText="https://pulse.example";AutomationProperties.SetAutomationId(endpoint,"reporting-endpoint");
        allowHttp.Header=L.Text("允许私网 HTTP（LAN / Tailscale / 环回）","Allow private HTTP (LAN / Tailscale / loopback)");
        code.Header=L.Text("采集设备配对码","Collector pairing code");AutomationProperties.SetAutomationId(code,"reporting-code");
        pair.Content=L.Text("配对 / 重新配对中心…","Pair / pair again…");AutomationProperties.SetAutomationId(pair,"reporting-pair");
        endpoint.TextChanged+=(_,_)=>{if(!applying && !controller.Busy){controller.Draft.Endpoint=endpoint.Text;controller.Edited();}};
        allowHttp.Toggled+=(_,_)=>{if(!applying && !controller.Busy){controller.Draft.AllowHttp=allowHttp.IsOn;controller.Edited();}};
        code.PasswordChanged+=(_,_)=>Publish();
        pair.Click+=async(_,_)=>await Execute(pair,async()=>
        {
            var transient=code.Password;code.Password="";
            if(!await ProductDialog.Confirm(host,L.Text("配对中心","Pair center"),L.Text("将使用一次性码连接所填中心，并保持同步关闭。原中心待传队列和中心历史保留。HTTP 许可只在配对时生效，HTTPS 不会降级。","Use the one-time code to connect to the entered center and leave sync disabled. Prior queues and center history remain. HTTP consent applies at pairing; HTTPS never falls back."),L.Text("配对并读回","Pair and read back")))return;
            await controller.PairAsync(transient);
        });
        body.Children.Add(endpoint);body.Children.Add(allowHttp);body.Children.Add(PresentationControls.Text(L.Text("普通 LAN HTTP 会明文传输配对材料与允许元数据；Tailscale 使用节点隧道。公网使用 HTTPS。","LAN HTTP transmits pairing material and allowed metadata without TLS. Tailscale uses its node tunnel. Use HTTPS on public networks."),12));body.Children.Add(code);body.Children.Add(pair);
        enabled.Header=L.Text("启用上报","Enable reporting");interval.Header=L.Text("同步间隔（15–3600 秒）","Sync interval (15–3600 seconds)");allHistory.Header=L.Text("补传全部已索引历史","Include all indexed history");date.Header=L.Text("首次补传起点（本机时区）","Initial history date (local time zone)");time.Header=L.Text("起点时间","Start time");
        enabled.Toggled+=(_,_)=>{if(!applying && !controller.Busy){controller.Draft.Enabled=enabled.IsOn;controller.Edited();}};
        interval.ValueChanged+=(_,_)=>{if(!applying && !controller.Busy){if(double.IsFinite(interval.Value) && interval.Value==Math.Truncate(interval.Value))controller.Draft.IntervalSeconds=(long)interval.Value;controller.Edited();}};
        allHistory.Toggled+=(_,_)=>DateEdited();date.DateChanged+=(_,_)=>DateEdited();time.TimeChanged+=(_,_)=>DateEdited();
        body.Children.Add(enabled);body.Children.Add(interval);body.Children.Add(allHistory);body.Children.Add(date);body.Children.Add(time);
        body.Children.Add(PresentationControls.Text(L.Text("开始导出后起点固定；改变范围需重新配对。关闭上报保留队列，退出 App 停止发送，下次启动续传。未观测的额度历史不能补造。","History start locks after export begins; pair again to change it. Disabling retains queues. Quitting stops sending; the next launch resumes. Unobserved quota history cannot be reconstructed."),12));
        save.Content=L.Text("保存同步设置","Save sync settings");sync.Content=L.Text("立即增量补传","Sync incrementally now");full.Content=L.Text("全量补传当前范围…","Full sync current range…");clear.Content=L.Text("关闭上报并清理当前中心队列…","Disable and clear this center's queue…");reset.Content=L.Text("放弃同步草稿并读回","Discard sync draft and reload");open.Content=L.Text("打开中心 Web","Open center Web");
        save.Click+=async(_,_)=>await Execute(save,()=>controller.ConfigureAsync());sync.Click+=async(_,_)=>await Execute(sync,()=>controller.SyncAsync(false));
        full.Click+=async(_,_)=>await Execute(full,async()=>{if(await ProductDialog.Confirm(host,L.Text("全量补传当前范围","Full sync current range"),L.Text("重新导出已保存起点内的已索引事实，保留不可变批次；不重扫原始文件、不扩大范围或删除中心历史。完成仅以所有有效确认判定。","Re-export indexed facts within the saved range and retain immutable batches. Do not rescan source files, expand the range or delete center history. Completion requires every valid final acknowledgment."),L.Text("开始全量补传","Start full sync")))await controller.SyncAsync(true);});
        clear.Click+=async(_,_)=>await Execute(clear,async()=>{if(await ProductDialog.Confirm(host,L.Text("丢弃当前中心待发送队列","Discard this center's pending queue"),L.Text("将关闭上报并丢弃尚未确认的本机批次；不删除中心历史，不影响其他中心保留队列，也不改变已固定的历史起点。","Disable reporting and discard unconfirmed local batches. Center history and other centers' retained queues remain. The locked history start is unchanged."),L.Text("关闭并清理","Disable and discard")))await controller.ConfigureAsync(true);});
        reset.Click+=async(_,_)=>{code.Password="";await controller.ReloadAsync();controller.Reset();Publish();};
        open.Click+=async(_,_)=>{if(controller.Current.State.Value is {} value && ReportingController.WebAddress(value) is {} uri && !await Windows.System.Launcher.LaunchUriAsync(uri))result.Text=L.Text("无法打开中心浏览器。","Could not open the center browser.");};
        foreach(var button in new[]{save,sync,full,clear,reset,open})body.Children.Add(button);
        var reload=new Button{Content=L.Text("刷新同步状态","Refresh sync status")};reload.Click+=async(_,_)=>await LoadAsync();body.Children.Add(reload);body.Children.Add(facts);
        body.Children.Add(PresentationControls.Text(L.Text("设备撤销在中心 Web 管理。重新配对不会删除旧设备历史；当前 Windows 设备不复制其他机器的同步凭证或数据库。","Revoke devices in center Web. Pairing again keeps prior device history. This Windows device does not copy another machine's sync credentials or database."),12));
        View.Expanding+=async(_,_)=>{await LoadAsync();poll.Start();};View.Collapsed+=(_,_)=>poll.Stop();
        poll.Tick+=async(_,_)=>{if(View.IsExpanded && host.WindowVisible && host.WindowActive && !controller.Busy)await controller.ReloadAsync();};
        controller.Changed+=Changed;controller.Current.Changed+=Changed;Publish();
    }
    private void DateEdited()
    {
        if(applying || controller.Busy)return;
        if(allHistory.IsOn)controller.Draft.HistoryStartAtMs=0;
        else if(date.Date is {} value)
        {var local=DateTime.SpecifyKind(value.Date+time.Time,DateTimeKind.Local);controller.Draft.HistoryStartAtMs=new DateTimeOffset(local).ToUnixTimeMilliseconds();}
        controller.Edited();
    }
    public Task LoadAsync()=>controller.ReloadAsync();
    private void Changed()=>host.Dispatch(Publish);
    private async Task Execute(Button button,Func<Task> action)
    {
        if(disposed || actionRunning || controller.Busy)return;actionRunning=true;button.IsEnabled=false;
        try{await action();}
        catch(ArgumentException){if(!disposed)result.Text=L.Text("请检查同步间隔与历史起点。","Check the sync interval and history start.");}
        finally{actionRunning=false;if(!disposed)Publish();}
    }
    private void Publish()
    {
        if(disposed)return;applying=true;
        try
        {
            var draft=controller.Draft;var status=controller.Current.State.Value;var paired=!string.IsNullOrEmpty(status?.ClientId);var busy=controller.Busy || actionRunning;
            result.Text=L.State(controller.Current.State.Kind)+" · "+(controller.Action switch
            {ReportingAction.Running=>L.Text("正在操作并权威读回","Working and reading back"),ReportingAction.Applied=>L.Text("操作已确认并读回；上报结果见下方状态","Action confirmed and read back; delivery status follows"),ReportingAction.ReadbackConfirmed=>L.Text("回执未知，最新状态已确认目标值","Receipt unknown; authoritative status confirms the requested values"),ReportingAction.ReadbackChanged=>L.Text("最新状态与提交不同，请核对后再操作","Readback differs; review before another action"),ReportingAction.CommandAccepted=>L.Text("补传请求已受理，尚不代表全部发布完成","Sync request accepted; full publication is not implied"),ReportingAction.Unknown=>L.Text("结果未知；已尝试读回，不会自动重放","Outcome unknown; readback attempted, no automatic replay"),ReportingAction.Unavailable=>L.Text("操作未完成，请检查地址、许可、码或同步范围","Action incomplete; check address, consent, code or range"),_=>controller.PairDirty || controller.ConfigurationDirty?L.Text("有未保存的同步草稿","Unsaved sync draft"):""});
            if(!ReferenceEquals(rendered,draft))
            {
                rendered=draft;endpoint.Text=draft.Endpoint;allowHttp.IsOn=draft.AllowHttp;enabled.IsOn=draft.Enabled;interval.Value=draft.IntervalSeconds;allHistory.IsOn=draft.HistoryStartAtMs==0;
                var start=draft.HistoryStartAtMs is >0 and <=253402300799999?DateTimeOffset.FromUnixTimeMilliseconds(draft.HistoryStartAtMs).ToLocalTime():DateTimeOffset.Now.AddDays(-30);date.Date=start;time.Time=start.TimeOfDay;
            }
            endpoint.IsEnabled=allowHttp.IsEnabled=code.IsEnabled=!busy;pair.IsEnabled=!busy && !string.IsNullOrWhiteSpace(endpoint.Text) && code.Password.Length>=16;
            enabled.IsEnabled=interval.IsEnabled=paired && !busy;allHistory.IsEnabled=paired && !busy && !controller.HistoryLocked;date.IsEnabled=time.IsEnabled=allHistory.IsEnabled && !allHistory.IsOn;
            save.IsEnabled=paired && !busy && controller.ConfigurationDirty && double.IsFinite(interval.Value) && interval.Value==Math.Truncate(interval.Value) && interval.Value is >=15 and <=3600;
            sync.IsEnabled=paired && !busy && status?.Enabled==true;full.IsEnabled=sync.IsEnabled && status?.FullSyncState!="running";clear.IsEnabled=paired && !busy && status?.PendingBatches>0;reset.IsEnabled=!busy;open.IsEnabled=status is not null && ReportingController.WebAddress(status) is not null;
            if(status is null){foreach(var row in factRows.Values)row.Visibility=Visibility.Collapsed;return;}
            var present=new HashSet<string>();
            void Fact(string zh,string en,string value)
            {
                present.Add(en);
                if(!factRows.TryGetValue(en,out var row)){row=PresentationControls.Text("");factRows.Add(en,row);facts.Children.Add(row);}
                var text=L.Text(zh,en)+" · "+value;if(row.Text!=text)row.Text=text;row.Visibility=Visibility.Visible;
            }
            string Number(long value)=>value>=0?value.ToString("N0",L.Culture):"—";
            Fact("当前中心","Saved center",string.IsNullOrEmpty(status.Endpoint)?L.Text("尚未配对","Not paired"):status.Endpoint);Fact("设备 ID","Device ID",paired?status.ClientId:L.Text("尚未配对","Not paired"));
            Fact("已保存的上报开关","Saved reporting state",status.Enabled?L.Text("已启用","Enabled"):L.Text("已关闭","Disabled"));Fact("同步状态","Sync status",ReportingCopy.State(status.State,L));Fact("待确认批次","Unconfirmed batches",Number(status.PendingBatches));
            Fact("队列大小","Queue size",RuntimeCopy.Bytes(status.PendingBytes>=0?new(){Value=status.PendingBytes}:new(),L));Fact("其他中心保留批次","Other centers' retained batches",Number(status.RetainedBatches));
            Fact("最近尝试","Last attempt",L.Timestamp(status.HasLastAttemptAtMs?status.LastAttemptAtMs:null));Fact("最近有效确认","Last valid acknowledgment",L.Timestamp(status.HasLastSuccessAtMs?status.LastSuccessAtMs:null));
            Fact("实际历史起点","Saved history start",status.HistoryStartAtMs==0?L.Text("全部已索引历史","All indexed history"):L.Timestamp(status.HistoryStartAtMs));
            if(!string.IsNullOrEmpty(status.FullSyncState))
            {
                Fact("全量任务","Full sync",status.FullSyncState switch{"completed"=>L.Text("已完成（全部有效确认）","Completed (all valid acknowledgments)"),"running"=>status.Enabled?L.Text("进行中","Running"):L.Text("已暂停，启用后续传","Paused; resumes when enabled"),_=>L.Text("未知任务状态","Unknown task state")});
                Fact("排入快照数","Enqueued snapshots",Number(status.FullSyncExportedSessions));Fact("已确认批次数","Acknowledged batches",Number(status.FullSyncAcknowledgedBatches));Fact("任务开始","Task started",L.Timestamp(status.HasFullSyncStartedAtMs?status.FullSyncStartedAtMs:null));
            }
            foreach(var row in factRows.Where(row=>!present.Contains(row.Key)))row.Value.Visibility=Visibility.Collapsed;
        }
        finally{applying=false;}
    }
    public void Dispose(){disposed=true;poll.Stop();code.Password="";controller.Changed-=Changed;controller.Current.Changed-=Changed;}
}
