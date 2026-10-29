using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class LocalStatusPage:FeaturePage
{
    private readonly bool detailed;
    private readonly DataSection<HealthProjectionResponse> projection;
    private readonly DataSection<DataHealthResponse> health;
    private readonly DataSection<HealthListResponse>? events;
    private readonly DataSection<HealthDetailResponse>? detail;
    private readonly Expander? detailView;
    private readonly RuntimeActions actions;
    private readonly CursorPager pager=new();
    private readonly Microsoft.UI.Xaml.DispatcherTimer timer=new(){Interval=TimeSpan.FromSeconds(5)};
    private string active="";
    private string? selectedEvent;
    private bool disposed,loading;
    public LocalStatusPage(MainWindow host):base(host)
    {
        Body.HorizontalAlignment=HorizontalAlignment.Left;
        View.SizeChanged+=(_,args)=>Body.Width=Math.Min(1200,Math.Max(0,args.NewSize.Width-8));
        detailed=Host.DataHealthRequested;actions=new(host,()=>LoadAsync(true));
        var open=new Button{Content=detailed?L.Text("返回本机状态","Back to local status"):L.Text("查看诊断详情","View diagnostics")};open.Click+=(_,_)=>{if(detailed)Host.Navigate(Feature.LocalStatus);else Host.OpenDataHealth();};Body.Children.Add(PresentationControls.SectionHeader(L.Text("检查数据更新和本机资源使用情况","Monitor data updates and local resources"),open));
        projection=new(host,L.Text("当前健康与保护状态","Current health and protection"),RenderProjection,"health-projection");Body.Children.Add(PresentationControls.Card(projection.View));
        health=new(host,detailed?L.Text("Data Health · 最近 24 小时","Data Health · Last 24 hours"):L.Text("本机数据状态","Local data status"),RenderHealth,"data-health");Body.Children.Add(PresentationControls.Card(health.View));
        Body.Children.Add(actions.Status);
        if(detailed)
        {
            var controls=new StackPanel{Spacing=8};foreach(var action in new[]{"pause_backfill","pause_all","resume","reconcile"})controls.Children.Add(actions.Action(action));controls.Children.Add(actions.Analyze());Body.Children.Add(PresentationControls.Disclosure(L.Text("受控运行操作","Controlled runtime actions"),controls));
            events=new(host,L.Text("状态提醒","Health events"),RenderEvents,"health-events");
            var filter=new ComboBox{Header=L.Text("事件范围","Event scope")};foreach(var pair in new[]{("",L.Text("全部事件","All events")),("true",L.Text("当前未解决","Open events")),("false",L.Text("已解决","Resolved events"))})filter.Items.Add(new ComboBoxItem{Tag=pair.Item1,Content=pair.Item2});filter.SelectedIndex=0;filter.SelectionChanged+=async(_,_)=>{active=(string)((ComboBoxItem)filter.SelectedItem).Tag;pager.Reset();await LoadAsync(true);};Body.Children.Add(filter);Body.Children.Add(events.View);
            detail=new(host,L.Text("健康事件详情","Health event details"),RenderDetail,"health-detail");detailView=(Expander)PresentationControls.Disclosure(L.Text("选中事件的详情","Selected event details"),detail.View);Body.Children.Add(detailView);
        }
        else
        {
            var sources=new Button{Content=L.Text("查看来源与后台任务","View sources and jobs")};sources.Click+=(_,_)=>Host.Navigate(Feature.SourcesJobs);Body.Children.Add(sources);
        }
        timer.Tick+=async(_,_)=>{if(!loading && !disposed && Host.WindowActive)await LoadAsync(true);};
    }
    public override bool DependsOn(string domain)=>domain is "health" or "index" or "settings" or "account" or "quota" || domain.StartsWith("quota_",StringComparison.Ordinal);
    public override async Task LoadAsync(bool force=false)
    {
        if(disposed || loading)return;loading=true;
        try
        {
            var context=Host.Workspace.Context;var now=DateTimeOffset.Now.ToUnixTimeMilliseconds();var request=new HealthProjectionRequest();var dataRequest=new DataHealthRequest{EvaluatedAtMs=now};
            var tasks=new List<Task>{projection.Slot.LoadAsync(context.Generation,ct=>Host.Client.QueryAsync("HealthProjection",request,context,["health"],(core,deadline,cancel)=>core.HealthProjectionAsync(request,deadline:deadline,cancellationToken:cancel),ct,true),value=>!value.HasValue?LoadKind.Unavailable:value.Stale?LoadKind.Stale:LoadKind.Ready),
                health.Slot.LoadAsync(context.Generation,ct=>Host.Client.QueryAsync("DataHealth",dataRequest,context,["health","index","quota"],(core,deadline,cancel)=>core.DataHealthAsync(dataRequest,deadline:deadline,cancellationToken:cancel),ct,force),value=>DashboardPresentation.Kind(value.Meta))};
            if(events is not null)
            {
                var eventRequest=new ListHealthRequest{Query=new(){Page=pager.Request(20)}};eventRequest.Query.Sort.Add(new SortTerm{Field="lastSeenAt",Direction="desc"});if(active!="")eventRequest.Query.Filters.Add(new FilterTerm{Field="active",Operator="eq",Values={active}});
                tasks.Add(events.Slot.LoadAsync((context.Generation,active,pager.Cursor),ct=>Host.Client.QueryAsync("ListHealth",eventRequest,context,["health"],(core,deadline,cancel)=>core.ListHealthAsync(eventRequest,deadline:deadline,cancellationToken:cancel),ct,force),value=>DashboardPresentation.Kind(value.Meta,value.Items.Count==0)));
            }
            await Task.WhenAll(tasks);if(!disposed){if(selectedEvent is {} id)await Detail(id,false);State(projection.Slot.State.Kind);timer.Start();}
        }
        finally{loading=false;}
    }
    private void RenderProjection(StackPanel panel,HealthProjectionResponse value)
    {
        if(!value.HasValue){panel.Children.Add(Text(L.Text("健康结论尚未取得，不能视为正常。","Health is not available yet; it cannot be treated as healthy.")));return;}
        panel.Children.Add(Text((value.Stale?L.Text("上次可信状态 · ","Last known state · "):"")+RuntimeCopy.State(value.HasLevel?value.Level:"unknown",L),"SubtitleTextBlockStyle"));
        panel.Children.Add(PresentationControls.Caption(L.Text("健康评估时间 ","Health evaluated ")+Time(value.EvaluatedAtMs)));
        if(value.Primary is {} primary)
        {
            panel.Children.Add(Text(RuntimeCopy.Impact(primary.Impact,L)+" · "+RuntimeCopy.Reason(primary.Reason,L)));
            panel.Children.Add(Text(RuntimeCopy.Protection(primary.Protection,L)));
            if(actions.Recovery(primary.RecoveryAction) is {} button)panel.Children.Add(button);
        }
        if(!detailed)return;
        var components=new List<UIElement>();
        foreach(var component in value.Components)
        {
            var row=new StackPanel{Spacing=6};row.Children.Add(Text(RuntimeCopy.Component(component.Component,L)+" · "+RuntimeCopy.State(component.Level,L),"SubtitleTextBlockStyle"));row.Children.Add(Text(RuntimeCopy.Reason(component.Reason,L)));row.Children.Add(Text(RuntimeCopy.Impact(component.Impact,L)));row.Children.Add(Text(RuntimeCopy.Protection(component.Protection,L)));
            row.Children.Add(PresentationControls.Caption(L.Text("证据状态：","Evidence state: ")+component.Evidence));if(actions.Recovery(component.RecoveryAction) is {} button)row.Children.Add(button);components.Add(PresentationControls.Card(row));
        }
        panel.Children.Add(PresentationControls.Columns(components,280));
    }
    private void RenderHealth(StackPanel panel,DataHealthResponse value)
    {
        panel.Children.Add(PresentationControls.Caption(L.Text("资源与活动更新于 ","Resources and activity updated ")+Time(value.EvaluatedAtMs)));
        if(value.Latest is {} latest)
        {
            panel.Children.Add(PresentationControls.MetricStrip(("CPU",double.IsFinite(latest.CpuPercent)&&latest.CpuPercent>=0?latest.CpuPercent.ToString("0.0",L.Culture)+"%":"—"),(L.Text("内存","Memory"),RuntimeCopy.Bytes(latest.RssBytes,L)),(L.Text("数据库","Database"),RuntimeCopy.Bytes(latest.DbBytes,L)),(L.Text("待处理","Pending updates"),L.Number(latest.LiveQueueDepth))));
            panel.Children.Add(PresentationControls.Caption(L.Text("最近采样 ","Latest sample ")+Time(latest.CapturedAtMs)+L.Text(" · CPU 单核占满为 100%"," · CPU: one core = 100%")));
            panel.Children.Add(PresentationControls.DataButton(L.Text("资源详情","Resource details"),()=>
            {
                var resources=new StackPanel{Spacing=12};
                resources.Children.Add(PresentationControls.KeyValue("WAL",RuntimeCopy.Bytes(latest.WalBytes,L)));
                resources.Children.Add(PresentationControls.KeyValue(L.Text("磁盘可用","Disk free"),RuntimeCopy.Bytes(latest.DiskFreeBytes,L)));
                resources.Children.Add(PresentationControls.KeyValue(L.Text("后台补采队列","Backfill queue"),L.Number(latest.BackfillQueueDepth)));
                return resources;
            }));
        }
        else panel.Children.Add(Text(L.Text("尚无资源样本；不将缺失指标显示为零。","No resource samples yet; missing metrics remain unknown.")));
        var updates=new StackPanel{Spacing=10,MaxWidth=720,HorizontalAlignment=HorizontalAlignment.Left};updates.Children.Add(PresentationControls.Heading(L.Text("数据更新","Data updates")));
        updates.Children.Add(PresentationControls.Caption(L.Text("统计窗口 ","Observation window ")+Time(value.Window?.FromMs)+" — "+Time(value.Window?.UntilMs)));
        foreach(var field in new[]{(L.Text("完成 / 失败更新","Completed / failed updates"),L.Number(value.Scheduler?.CompletedCycles)+" / "+L.Number(value.Scheduler?.FailedCycles)),(L.Text("正常 / 待更新来源","Current / stale sources"),L.Number(value.Sources?.Current)+" / "+L.Number(value.Sources?.Stale)),(L.Text("暂不可用来源","Unavailable sources"),L.Number(value.Sources?.Unavailable)),(L.Text("在线请求成功 / 失败","Online requests succeeded / failed"),L.Number(value.Sources?.SucceededAttempts)+" / "+L.Number(value.Sources?.FailedAttempts)),(L.Text("运行 / 等待任务","Running / queued jobs"),L.Number(value.Jobs?.Running)+" / "+L.Number(value.Jobs?.Queued)),(L.Text("最近后台进展","Last backfill progress"),Time(value.Scheduler?.LastBackfillProgressAtMs))})updates.Children.Add(PresentationControls.KeyValue(field.Item1,field.Item2));
        panel.Children.Add(updates);
        if(!detailed)return;
        panel.Children.Add(Text(L.Text("观测窗口 ","Observation window ")+Time(value.Window?.FromMs)+" — "+Time(value.Window?.UntilMs)));
        panel.Children.Add(PresentationControls.MetricStrip((L.Text("对账周期","Scheduler cycles"),L.Number(value.Scheduler?.CycleCount)),(L.Text("读取字节","Bytes read"),RuntimeCopy.Bytes(value.Scheduler?.BytesRead,L)),(L.Text("当前连续失败","Current consecutive failures"),L.Number(value.Sources?.ConsecutiveFailures))));
        panel.Children.Add(PresentationControls.Caption(L.Text("来源下次重试 ","Next source retry ")+Time(value.Sources?.NextRetryAtMs)));
        panel.Children.Add(Text(L.Text("当前与最近任务","Current and recent jobs"),"SubtitleTextBlockStyle"));
        foreach(var job in value.CurrentJobs.Concat(value.RecentJobs))
        {var row=new StackPanel{Spacing=6};var open=new Button{Content=RuntimePresentation.Phase(job.Phase,L)+" · "+RuntimeCopy.State(job.State,L)+" · "+Time(job.CreatedAtMs)};open.Click+=(_,_)=>Host.OpenRuntimeItem("job",job.JobId);row.Children.Add(open);row.Children.Add(Text(L.Text("进度 ","Progress ")+L.Number(job.Progress?.Current)+" / "+L.Number(job.Progress?.Total)+L.Text(" · 下次重试 "," · Next retry ")+Time(job.NextRetryAtMs)));panel.Children.Add(row);}
        if(value.CurrentJobs.Count+value.RecentJobs.Count==0)panel.Children.Add(Text(L.Text("当前窗口没有任务记录。","No jobs in the current window.")));
        panel.Children.Add(Text(L.Text("当前和最近事件","Open and recent events"),"SubtitleTextBlockStyle"));foreach(var item in value.OpenEvents.Concat(value.RecentEvents))panel.Children.Add(EventRow(item));
        panel.Children.Add(Text(L.Text("最近 24 小时进程资源","Process resources over the last 24 hours"),"SubtitleTextBlockStyle"));
        panel.Children.Add(RuntimeChart.Create(value.Runtime,L));
        panel.Children.Add(PresentationControls.Disclosure(L.Text("资源精确值与缺口","Resource values and gaps"),PresentationControls.Table([L.Text("时间","Time"),"CPU %","RSS","DB / WAL",L.Text("丢失采样","Dropped samples")],value.Runtime.Select(point=>new[]{Time(point.CapturedAtMs),double.IsFinite(point.CpuPercent)?point.CpuPercent.ToString("0.###",L.Culture):"—",RuntimeCopy.Bytes(point.RssBytes,L),RuntimeCopy.Bytes(point.DbBytes,L)+" / "+RuntimeCopy.Bytes(point.WalBytes,L),L.Number(point.DroppedSamples)}))));
    }
    private FrameworkElement EventRow(HealthItem item)
    {
        var row=new StackPanel{Spacing=6};var button=new Button{Content=RuntimeCopy.Component(item.Component,L)+" · "+RuntimeCopy.Reason(item.Code,L)+" · "+(item.Active?L.Text("未解决","Open"):L.Text("已解决","Resolved"))};button.Click+=async(_,_)=>await Detail(item.EventId);row.Children.Add(button);
        row.Children.Add(Text(RuntimeCopy.Impact(item.Impact,L)+L.Text(" · 累计 "," · Occurrences ")+L.Number(item.OccurrenceCount)+L.Text(" · 最近 "," · Last seen ")+Time(item.LastSeenAtMs)));return row;
    }
    private void RenderEvents(StackPanel panel,HealthListResponse response)
    {
        panel.Children.Add(PresentationControls.MetricStrip((L.Text("当前未解决","Open events"),L.Number(response.Summary?.Active)),(L.Text("历史已解决","Resolved events"),L.Number(response.Summary?.Resolved)),(L.Text("当前匹配","Matched"),L.Number(response.MatchedCount))));foreach(var item in response.Items)panel.Children.Add(EventRow(item));
        if(!pager.CanPrevious && response.Meta?.Page?.HasMore!=true)return;
        var controls=new StackPanel{Orientation=Orientation.Horizontal,Spacing=8};var previous=new Button{Content=L.Text("上一页","Previous"),IsEnabled=pager.CanPrevious};var next=new Button{Content=L.Text("下一页","Next"),IsEnabled=response.Meta?.Page is{HasMore:true,HasNextCursor:true}};previous.Click+=async(_,_)=>{if(pager.Previous())await LoadAsync(true);};next.Click+=async(_,_)=>{if(pager.Next(response.Meta?.Page))await LoadAsync(true);};controls.Children.Add(previous);controls.Children.Add(Text(pager.Page.ToString(L.Culture)));controls.Children.Add(next);panel.Children.Add(controls);
    }
    private async Task Detail(string id,bool reveal=true)
    {
        if(disposed || detail is null || detailView is null)return;selectedEvent=id;if(reveal)detailView.IsExpanded=true;
        var request=new HealthRequest{EventId=id};var context=Host.Workspace.Context;
        await detail.Slot.LoadAsync((context.Generation,id),ct=>Host.Client.QueryAsync("Health",request,context,["health"],(core,deadline,cancel)=>core.HealthAsync(request,deadline:deadline,cancellationToken:cancel),ct,true),value=>DashboardPresentation.Kind(value.Meta));if(reveal && !disposed)detailView.StartBringIntoView();
    }
    private void RenderDetail(StackPanel panel,HealthDetailResponse response)
    {
        var item=response.Item;panel.Children.Add(Text(RuntimeCopy.Component(item.Component,L)+" · "+RuntimeCopy.Reason(item.Code,L)));panel.Children.Add(Text(RuntimeCopy.Impact(item.Impact,L)));panel.Children.Add(Text(RuntimeCopy.Protection(item.Protection,L)));
        panel.Children.Add(Text(L.Text("首次 / 最近 / 恢复 ","First / latest / resolved ")+Time(item.FirstSeenAtMs)+" / "+Time(item.LastSeenAtMs)+" / "+Time(item.ResolvedAtMs)));panel.Children.Add(Text(L.Text("累计次数：","Occurrence count: ")+L.Number(item.OccurrenceCount)));
        if(item.HasErrorClass)panel.Children.Add(Text(L.Text("有限错误类别：","Bounded error class: ")+item.ErrorClass));
        if(item.HasSourceKey){var open=new Button{Content=L.Text("查看关联来源","View related source")};open.Click+=(_,_)=>Host.OpenRuntimeItem("source",item.SourceKey);panel.Children.Add(open);}
        if(item.HasJobId){var open=new Button{Content=L.Text("查看关联任务","View related job")};open.Click+=(_,_)=>Host.OpenRuntimeItem("job",item.JobId);panel.Children.Add(open);}
        if(actions.Recovery(item.RecoveryAction?.Kind??"") is {} action)panel.Children.Add(action);
    }
    private string Time(NumericValue? value)=>L.Timestamp(value?.HasValue==true?value.Value:null);
    public override void Cancel(){projection.Slot.Cancel();health.Slot.Cancel();events?.Slot.Cancel();detail?.Slot.Cancel();}
    public override void Dispose(){disposed=true;timer.Stop();projection.Dispose();health.Dispose();events?.Dispose();detail?.Dispose();}
}
