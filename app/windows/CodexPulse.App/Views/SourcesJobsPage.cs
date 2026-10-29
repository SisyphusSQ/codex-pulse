using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class SourcesJobsPage:FeaturePage
{
    private readonly DataSection<SourceListResponse> sources;
    private readonly DataSection<JobListResponse> jobs;
    private readonly DataSection<SourceDetailResponse> source;
    private readonly DataSection<JobDetailResponse> job;
    private readonly CursorPager sourcePager=new(),jobPager=new();
    private readonly RuntimeActions actions;
    private string sourceKind="",jobState="";
    private readonly Expander sourceView,jobView;
    private (string Kind,string Identity)? selected;
    private bool disposed;
    public SourcesJobsPage(MainWindow host):base(host)
    {
        actions=new(host,()=>LoadAsync(true));
        Body.Children.Add(Text(L.Text("这里只显示结构化来源、任务状态与有限错误类别；不显示路径、原始 JSONL 或日志正文。","Structured sources, jobs and bounded error classes only. Paths, raw JSONL and log content are excluded.")));
        Body.Children.Add(actions.Action("reconcile"));Body.Children.Add(actions.Status);
        sources=new(host,L.Text("数据来源","Data sources"),RenderSources,"sources-list");jobs=new(host,L.Text("后台任务","Background jobs"),RenderJobs,"jobs-list");
        source=new(host,L.Text("来源详情","Source details"),(panel,response)=>RenderSource(panel,response.Item),"source-detail");
        job=new(host,L.Text("任务详情","Job details"),(panel,response)=>RenderJob(panel,response.Item),"job-detail");
        var kinds=new ComboBox{Header=L.Text("来源类别","Source kind")};
        foreach(var item in new[]{("",L.Text("全部来源","All sources")),("local_file",L.Text("本机文件","Local files")),("online",L.Text("在线来源","Online sources")),("provider_source",L.Text("客户端来源","Client sources"))})kinds.Items.Add(new ComboBoxItem{Tag=item.Item1,Content=item.Item2});
        kinds.SelectedIndex=0;kinds.SelectionChanged+=async(_,_)=>{sourceKind=(string)((ComboBoxItem)kinds.SelectedItem).Tag;sourcePager.Reset();await LoadAsync(true);};
        sourceView=(Expander)PresentationControls.Disclosure(L.Text("选中来源的详情","Selected source details"),source.View);
        Body.Children.Add(kinds);Body.Children.Add(PresentationControls.Card(sources.View));Body.Children.Add(sourceView);
        var states=new ComboBox{Header=L.Text("任务状态","Job state")};
        foreach(var state in new[]{"","queued","running","interrupted","failed","succeeded","cancelled"})states.Items.Add(new ComboBoxItem{Tag=state,Content=state==""?L.Text("全部状态","All states"):RuntimeCopy.State(state,L)});
        states.SelectedIndex=0;states.SelectionChanged+=async(_,_)=>{jobState=(string)((ComboBoxItem)states.SelectedItem).Tag;jobPager.Reset();await LoadAsync(true);};
        jobView=(Expander)PresentationControls.Disclosure(L.Text("选中任务的详情","Selected job details"),job.View);
        Body.Children.Add(states);Body.Children.Add(PresentationControls.Card(jobs.View));Body.Children.Add(jobView);
        selected=Host.RuntimeSelection;
    }
    public override bool DependsOn(string domain)=>domain is "index" or "health" or "settings" or "account" or "quota" or "quota_codex" or "quota_cursor" or "quota_grok";
    public override async Task LoadAsync(bool force=false)
    {
        var context=Host.Workspace.Context;
        var sourceRequest=new ListSourcesRequest{Query=new(){Page=sourcePager.Request(20)}};sourceRequest.Query.Sort.Add(new SortTerm{Field="updatedAt",Direction="desc"});
        if(sourceKind!="")sourceRequest.Query.Filters.Add(new FilterTerm{Field="kind",Operator="eq",Values={sourceKind}});
        var jobRequest=new ListJobsRequest{Query=new(){Page=jobPager.Request(20)}};jobRequest.Query.Sort.Add(new SortTerm{Field="updatedAt",Direction="desc"});
        if(jobState!="")jobRequest.Query.Filters.Add(new FilterTerm{Field="state",Operator="eq",Values={jobState}});
        await Task.WhenAll(sources.Slot.LoadAsync((context.Generation,sourceKind,sourcePager.Cursor),ct=>Host.Client.QueryAsync("ListSources",sourceRequest,context,["index","quota","health"],(core,deadline,cancel)=>core.ListSourcesAsync(sourceRequest,deadline:deadline,cancellationToken:cancel),ct,force),r=>DashboardPresentation.Kind(r.Meta,r.Items.Count==0)),
            jobs.Slot.LoadAsync((context.Generation,jobState,jobPager.Cursor),ct=>Host.Client.QueryAsync("ListJobs",jobRequest,context,["index","health"],(core,deadline,cancel)=>core.ListJobsAsync(jobRequest,deadline:deadline,cancellationToken:cancel),ct,force),r=>DashboardPresentation.Kind(r.Meta,r.Items.Count==0)));
        if(disposed)return;
        State(sources.Slot.State.Kind);
        if(selected is {} selection)await Detail(selection.Kind,selection.Identity,false);
    }
    private string SourceName(SourceItem item)=>(item.HasProvider?MainWindow.ProviderName(item.Provider)+" · ":"")+(item.HasSourceType?item.SourceType switch{"app_server_rate_limits"=>L.Text("Codex 在线额度","Codex online quota"),"app_server_reset_credits"=>L.Text("Codex 重置库存","Codex reset credits"),_=>item.SourceType}:item.Kind switch{"local_file"=>L.Text("本机文件","Local file"),"online"=>L.Text("在线来源","Online source"),_=>L.Text("客户端来源","Client source")});
    private void RenderSources(StackPanel panel,SourceListResponse response)
    {
        panel.Children.Add(PresentationControls.Metrics((L.Text("全部来源","All sources"),L.Number(response.Summary?.Total)),(L.Text("需关注","Attention"),L.Number(response.Summary?.Attention)),(L.Text("匹配条数","Matched"),L.Number(response.MatchedCount))));
        if(response.UnavailableKinds.Count>0)panel.Children.Add(Text(L.Text("部分来源类别暂不可用：","Some source kinds are unavailable: ")+string.Join(", ",response.UnavailableKinds)));
        foreach(var item in response.Items)
        {
            var row=new StackPanel{Spacing=6};var button=new Button{Content=SourceName(item)+" · "+RuntimeCopy.State(item.State,L)};button.Click+=async(_,_)=>await Detail("source",item.SourceKey);row.Children.Add(button);
            row.Children.Add(Text(L.Text("已解析 / 文件大小 ","Parsed / size ")+RuntimeCopy.Bytes(item.ParsedBytes,L)+" / "+RuntimeCopy.Bytes(item.SizeBytes,L)+L.Text(" · 上次成功 "," · Last success ")+Time(item.LastSuccessAtMs)));
            if(item.HasCoverageState)row.Children.Add(Text(L.Text("覆盖状态：","Coverage: ")+item.CoverageState));panel.Children.Add(row);
        }
        Pager(panel,sourcePager,response.Meta?.Page,()=>LoadAsync(true));
    }
    private void RenderJobs(StackPanel panel,JobListResponse response)
    {
        panel.Children.Add(PresentationControls.Metrics((L.Text("等待","Queued"),L.Number(response.Summary?.Queued)),(L.Text("运行","Running"),L.Number(response.Summary?.Running)),(L.Text("中断","Interrupted"),L.Number(response.Summary?.Interrupted)),(L.Text("匹配条数","Matched"),L.Number(response.MatchedCount))));
        foreach(var item in response.Items)
        {
            var row=new StackPanel{Spacing=6};var button=new Button{Content=RuntimePresentation.Phase(item.Phase,L)+" · "+RuntimeCopy.State(item.State,L)+" · "+Time(item.CreatedAtMs)};button.Click+=async(_,_)=>await Detail("job",item.JobId);row.Children.Add(button);
            row.Children.Add(Text(L.Text("进度 ","Progress ")+L.Number(item.Progress?.Current)+" / "+L.Number(item.Progress?.Total)+L.Text(" · 下次重试 "," · Next retry ")+Time(item.NextRetryAtMs)));panel.Children.Add(row);
        }
        Pager(panel,jobPager,response.Meta?.Page,()=>LoadAsync(true));
    }
    private void Pager(StackPanel panel,CursorPager pager,PageInfo? info,Func<Task> reload)
    {
        var row=new StackPanel{Orientation=Microsoft.UI.Xaml.Controls.Orientation.Horizontal,Spacing=8};var previous=new Button{Content=L.Text("上一页","Previous"),IsEnabled=pager.CanPrevious};var next=new Button{Content=L.Text("下一页","Next"),IsEnabled=info is{HasMore:true,HasNextCursor:true}};var first=new Button{Content=L.Text("首页","First")};
        previous.Click+=async(_,_)=>{if(pager.Previous())await reload();};next.Click+=async(_,_)=>{if(pager.Next(info))await reload();};first.Click+=async(_,_)=>{pager.Reset();await reload();};row.Children.Add(first);row.Children.Add(previous);row.Children.Add(Text(pager.Page.ToString(L.Culture)));row.Children.Add(next);panel.Children.Add(row);
    }
    private async Task Detail(string kind,string identity,bool reveal=true)
    {
        if(disposed)return;selected=(kind,identity);var context=Host.Workspace.Context;
        var expanded=kind=="source"?sourceView:jobView;if(reveal || Host.RuntimeSelection is not null)expanded.IsExpanded=true;
        if(kind=="source")
        {var request=new SourceRequest{SourceKey=identity};await source.Slot.LoadAsync((context.Generation,identity),ct=>Host.Client.QueryAsync("Source",request,context,["index","quota","health"],(core,deadline,cancel)=>core.SourceAsync(request,deadline:deadline,cancellationToken:cancel),ct,true),r=>DashboardPresentation.Kind(r.Meta));}
        else if(kind=="job")
        {var request=new JobRequest{JobId=identity};await job.Slot.LoadAsync((context.Generation,identity),ct=>Host.Client.QueryAsync("Job",request,context,["index","health"],(core,deadline,cancel)=>core.JobAsync(request,deadline:deadline,cancellationToken:cancel),ct,true),r=>DashboardPresentation.Kind(r.Meta));}
        if(reveal && !disposed)expanded.StartBringIntoView();
    }
    private void RenderSource(StackPanel panel,SourceItem item)
    {
        panel.Children.Add(Text(SourceName(item)+" · "+RuntimeCopy.State(item.State,L)));
        panel.Children.Add(PresentationControls.Table([L.Text("字段","Field"),L.Text("值","Value")],[new[]{L.Text("文件 / 已解析","Size / parsed"),RuntimeCopy.Bytes(item.SizeBytes,L)+" / "+RuntimeCopy.Bytes(item.ParsedBytes,L)},new[]{L.Text("行数","Rows"),L.Number(item.RowCount)},new[]{L.Text("上次扫描","Last scan"),Time(item.LastScannedAtMs)},new[]{L.Text("上次尝试 / 成功","Last attempt / success"),Time(item.LastAttemptAtMs)+" / "+Time(item.LastSuccessAtMs)},new[]{L.Text("下次到期","Next due"),Time(item.NextDueAtMs)},new[]{L.Text("连续失败","Consecutive failures"),L.Number(item.ConsecutiveFailures)},new[]{L.Text("有限错误类别","Bounded error class"),item.HasErrorClass?item.ErrorClass:"—"},new[]{L.Text("覆盖 / 检查点","Coverage / checkpoint"),(item.HasCoverageState?item.CoverageState:"—")+" / "+(item.HasCheckpointKind?item.CheckpointKind:"—")}]));
        if(actions.Recovery(item.RecoveryAction?.Kind??"") is {} recovery)panel.Children.Add(recovery);
    }
    private void RenderJob(StackPanel panel,JobItem item)
    {
        panel.Children.Add(Text(RuntimePresentation.Phase(item.Phase,L)+" · "+RuntimeCopy.State(item.State,L)));
        panel.Children.Add(PresentationControls.Metrics((L.Text("进度","Progress"),L.Number(item.Progress?.Current)+" / "+L.Number(item.Progress?.Total)),(L.Text("失败次数","Failures"),L.Number(item.FailureCount))));
        panel.Children.Add(Text(L.Text("创建 / 开始 / 完成 ","Created / started / finished ")+Time(item.CreatedAtMs)+" / "+Time(item.StartedAtMs)+" / "+Time(item.FinishedAtMs)));
        panel.Children.Add(Text(L.Text("上次成功 / 下次重试 ","Last success / next retry ")+Time(item.LastSuccessAtMs)+" / "+Time(item.NextRetryAtMs)));
        if(item.HasErrorClass)panel.Children.Add(Text(L.Text("错误类别：","Error class: ")+item.ErrorClass));
        if(item.HasSourceKey){var open=new Button{Content=L.Text("查看关联来源","View related source")};open.Click+=async(_,_)=>await Detail("source",item.SourceKey);panel.Children.Add(open);}
        if(actions.Recovery(item.RecoveryAction?.Kind??"") is {} recovery)panel.Children.Add(recovery);
    }
    private string Time(NumericValue? value)=>L.Timestamp(value?.HasValue==true?value.Value:null);
    public override void Cancel(){sources.Slot.Cancel();jobs.Slot.Cancel();source.Slot.Cancel();job.Slot.Cancel();}
    public override void Dispose(){disposed=true;sources.Dispose();jobs.Dispose();source.Dispose();job.Dispose();}
}
