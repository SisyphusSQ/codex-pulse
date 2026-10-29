using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
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
    private readonly Border sourceView,jobView;
    private readonly TextBlock sourcePrompt,jobPrompt;
    private readonly Button sourceBack,jobBack;
    private readonly SelectorBar sections=new();
    private readonly SelectorBarItem sourceTab=new(),jobTab=new();
    private readonly StackPanel sourcePane=new(){Spacing=16},jobPane=new(){Spacing=16};
    private (string Kind,string Identity)? selected;
    private bool disposed;
    public SourcesJobsPage(MainWindow host):base(host)
    {
        actions=new(host,()=>LoadAsync(true));
        Body.Children.Add(PresentationControls.Caption(L.Text("查看采集来源与后台任务的进度，选择一项查看最近状态和恢复操作。","Monitor data sources and background jobs. Select an item for its latest status and recovery actions.")));
        Body.Children.Add(actions.Action("reconcile"));Body.Children.Add(actions.Status);
        sourceTab.Text=L.Text("数据来源","Data sources");jobTab.Text=L.Text("后台任务","Background jobs");sections.Items.Add(sourceTab);sections.Items.Add(jobTab);Body.Children.Add(sections);
        sources=new(host,L.Text("数据来源","Data sources"),RenderSources,"sources-list",showHeader:false,showEmptyStatus:false);jobs=new(host,L.Text("后台任务","Background jobs"),RenderJobs,"jobs-list",showHeader:false,showEmptyStatus:false);
        source=new(host,L.Text("来源详情","Source details"),(panel,response)=>RenderSource(panel,response.Item),"source-detail",showHeader:false);
        job=new(host,L.Text("任务详情","Job details"),(panel,response)=>RenderJob(panel,response.Item),"job-detail",showHeader:false);
        var kinds=new ComboBox{MinWidth=160};Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(kinds,L.Text("来源类别","Source kind"));
        foreach(var item in new[]{("",L.Text("全部来源","All sources")),("local_file",L.Text("本机文件","Local files")),("online",L.Text("在线来源","Online sources")),("provider_source",L.Text("客户端来源","Client sources"))})kinds.Items.Add(new ComboBoxItem{Tag=item.Item1,Content=item.Item2});
        kinds.SelectedIndex=0;kinds.SelectionChanged+=async(_,_)=>{sourceKind=(string)((ComboBoxItem)kinds.SelectedItem).Tag;sourcePager.Reset();await LoadAsync(true);};
        sourcePrompt=PresentationControls.Caption(L.Text("选择一个来源，查看采集进度与最近状态。","Select a source to view its progress and recent status."));
        sourceBack=new Button{Content=L.Text("返回来源列表","Back to sources"),Visibility=Visibility.Collapsed};
        var sourceDetails=new StackPanel{Spacing=16};sourceDetails.Children.Add(sourceBack);sourceDetails.Children.Add(PresentationControls.Heading(L.Text("来源详情","Source details")));sourceDetails.Children.Add(sourcePrompt);sourceDetails.Children.Add(source.View);source.View.Visibility=Visibility.Collapsed;sourceView=PresentationControls.Card(sourceDetails);
        var sourceCard=new StackPanel {Spacing=16};sourceCard.Children.Add(PresentationControls.SectionHeader(L.Text("数据来源","Data sources"),kinds));sourceCard.Children.Add(sources.View);
        sourceBack.Click+=(_,_)=>sourceCard.StartBringIntoView(new BringIntoViewOptions{VerticalAlignmentRatio=0});
        sourcePane.SizeChanged+=(_,_)=>UpdateBackButtons();
        sourcePane.Children.Add(PresentationControls.Split(PresentationControls.Card(sourceCard),sourceView));Body.Children.Add(sourcePane);
        var states=new ComboBox{MinWidth=160};Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(states,L.Text("任务状态","Job state"));
        foreach(var state in new[]{"","queued","running","interrupted","failed","succeeded","cancelled"})states.Items.Add(new ComboBoxItem{Tag=state,Content=state==""?L.Text("全部状态","All states"):RuntimeCopy.State(state,L)});
        states.SelectedIndex=0;states.SelectionChanged+=async(_,_)=>{jobState=(string)((ComboBoxItem)states.SelectedItem).Tag;jobPager.Reset();await LoadAsync(true);};
        jobPrompt=PresentationControls.Caption(L.Text("选择一个任务，查看进度、重试与关联来源。","Select a job to view progress, retries and its related source."));
        jobBack=new Button{Content=L.Text("返回任务列表","Back to jobs"),Visibility=Visibility.Collapsed};
        var jobDetails=new StackPanel{Spacing=16};jobDetails.Children.Add(jobBack);jobDetails.Children.Add(PresentationControls.Heading(L.Text("任务详情","Job details")));jobDetails.Children.Add(jobPrompt);jobDetails.Children.Add(job.View);job.View.Visibility=Visibility.Collapsed;jobView=PresentationControls.Card(jobDetails);
        var jobCard=new StackPanel {Spacing=16};jobCard.Children.Add(PresentationControls.SectionHeader(L.Text("后台任务","Background jobs"),states));jobCard.Children.Add(jobs.View);
        jobBack.Click+=(_,_)=>jobCard.StartBringIntoView(new BringIntoViewOptions{VerticalAlignmentRatio=0});
        jobPane.SizeChanged+=(_,_)=>UpdateBackButtons();
        jobPane.Children.Add(PresentationControls.Split(PresentationControls.Card(jobCard),jobView));Body.Children.Add(jobPane);
        selected=Host.RuntimeSelection;
        sections.SelectionChanged+=(_,_)=>
        {
            var showJobs=sections.SelectedItem==jobTab;
            sourcePane.Visibility=showJobs?Visibility.Collapsed:Visibility.Visible;jobPane.Visibility=showJobs?Visibility.Visible:Visibility.Collapsed;
            State(showJobs?jobs.Slot.State.Kind:sources.Slot.State.Kind);
        };
        sections.SelectedItem=selected?.Kind=="job"?jobTab:sourceTab;
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
        State(sections.SelectedItem==jobTab?jobs.Slot.State.Kind:sources.Slot.State.Kind);
        if(selected is {} selection)await Detail(selection.Kind,selection.Identity,false);
    }
    private string SourceName(SourceItem item)=>(item.HasProvider?MainWindow.ProviderName(item.Provider)+" · ":"")+(item.HasSourceType?item.SourceType switch{"app_server_rate_limits"=>L.Text("Codex 在线额度","Codex online quota"),"app_server_reset_credits"=>L.Text("Codex 重置库存","Codex reset credits"),_=>item.SourceType}:item.Kind switch{"local_file"=>L.Text("本机文件","Local file"),"online"=>L.Text("在线来源","Online source"),_=>L.Text("客户端来源","Client source")});
    private void RenderSources(StackPanel panel,SourceListResponse response)
    {
        sourceView.Visibility=response.Items.Count==0 && selected?.Kind!="source"?Visibility.Collapsed:Visibility.Visible;
        panel.Children.Add(PresentationControls.MetricStrip((L.Text("全部来源","All sources"),L.Number(response.Summary?.Total)),(L.Text("需关注","Attention"),L.Number(response.Summary?.Attention)),(L.Text("匹配条数","Matched"),L.Number(response.MatchedCount))));
        if(response.UnavailableKinds.Count>0)panel.Children.Add(Text(L.Text("部分来源类别暂不可用：","Some source kinds are unavailable: ")+string.Join(", ",response.UnavailableKinds)));
        foreach(var item in response.Items)
        {
            var description=item.Kind=="local_file"?L.Text("已解析 ","Parsed ")+RuntimeCopy.Bytes(item.ParsedBytes,L)+" / "+RuntimeCopy.Bytes(item.SizeBytes,L):L.Text("上次成功 ","Last success ")+Time(item.LastSuccessAtMs);
            if(item.HasCoverageState)description+=" · "+L.Text("覆盖 ","Coverage ")+item.CoverageState;
            var button=ItemRow(SourceName(item),RuntimeCopy.State(item.State,L),description);button.Click+=async(_,_)=>await Detail("source",item.SourceKey);panel.Children.Add(button);
        }
        if(response.Items.Count==0)panel.Children.Add(PresentationControls.Caption(L.Text("当前条件下没有数据来源。","No data sources match these filters.")));
        if(sourcePager.CanPrevious || response.Meta?.Page?.HasMore==true)Pager(panel,sourcePager,response.Meta?.Page,()=>LoadAsync(true));
    }
    private void RenderJobs(StackPanel panel,JobListResponse response)
    {
        jobView.Visibility=response.Items.Count==0 && selected?.Kind!="job"?Visibility.Collapsed:Visibility.Visible;
        panel.Children.Add(PresentationControls.MetricStrip((L.Text("等待","Queued"),L.Number(response.Summary?.Queued)),(L.Text("运行","Running"),L.Number(response.Summary?.Running)),(L.Text("中断","Interrupted"),L.Number(response.Summary?.Interrupted)),(L.Text("匹配条数","Matched"),L.Number(response.MatchedCount))));
        foreach(var item in response.Items)
        {
            var button=ItemRow(RuntimePresentation.Phase(item.Phase,L),RuntimeCopy.State(item.State,L),L.Text("进度 ","Progress ")+L.Number(item.Progress?.Current)+" / "+L.Number(item.Progress?.Total)+" · "+Time(item.CreatedAtMs));
            button.Click+=async(_,_)=>await Detail("job",item.JobId);panel.Children.Add(button);
        }
        if(response.Items.Count==0)panel.Children.Add(PresentationControls.Caption(L.Text("当前条件下没有后台任务。","No background jobs match these filters.")));
        if(jobPager.CanPrevious || response.Meta?.Page?.HasMore==true)Pager(panel,jobPager,response.Meta?.Page,()=>LoadAsync(true));
    }
    private Button ItemRow(string title,string state,string description)
    {
        var grid=new Grid{ColumnSpacing=16,RowSpacing=5,IsHitTestVisible=false};grid.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});grid.ColumnDefinitions.Add(new(){Width=GridLength.Auto});grid.RowDefinitions.Add(new(){Height=GridLength.Auto});grid.RowDefinitions.Add(new(){Height=GridLength.Auto});
        var heading=Text(title);heading.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold;grid.Children.Add(heading);
        var status=PresentationControls.Caption(state);Grid.SetColumn(status,1);grid.Children.Add(status);
        var detail=PresentationControls.Caption(description);Grid.SetRow(detail,1);Grid.SetColumnSpan(detail,2);grid.Children.Add(detail);
        var button=new Button{Content=grid,HorizontalAlignment=HorizontalAlignment.Stretch,HorizontalContentAlignment=HorizontalAlignment.Stretch,Padding=new Thickness(12,10,12,10)};
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(button,title+" · "+state+" · "+description);return button;
    }
    private void Pager(StackPanel panel,CursorPager pager,PageInfo? info,Func<Task> reload)
    {
        var row=new StackPanel{Orientation=Microsoft.UI.Xaml.Controls.Orientation.Horizontal,Spacing=8};var previous=new Button{Content=L.Text("上一页","Previous"),IsEnabled=pager.CanPrevious};var next=new Button{Content=L.Text("下一页","Next"),IsEnabled=info is{HasMore:true,HasNextCursor:true}};var first=new Button{Content=L.Text("首页","First")};
        previous.Click+=async(_,_)=>{if(pager.Previous())await reload();};next.Click+=async(_,_)=>{if(pager.Next(info))await reload();};first.Click+=async(_,_)=>{pager.Reset();await reload();};row.Children.Add(first);row.Children.Add(previous);row.Children.Add(Text(pager.Page.ToString(L.Culture)));row.Children.Add(next);panel.Children.Add(row);
    }
    private async Task Detail(string kind,string identity,bool reveal=true)
    {
        if(disposed)return;selected=(kind,identity);var context=Host.Workspace.Context;
        if(reveal || Host.RuntimeSelection is not null)sections.SelectedItem=kind=="job"?jobTab:sourceTab;
        var expanded=kind=="source"?sourceView:jobView;
        if(kind=="source"){sourcePrompt.Visibility=Visibility.Collapsed;source.View.Visibility=Visibility.Visible;}
        else{jobPrompt.Visibility=Visibility.Collapsed;job.View.Visibility=Visibility.Visible;}
        UpdateBackButtons();
        if(kind=="source")
        {var request=new SourceRequest{SourceKey=identity};await source.Slot.LoadAsync((context.Generation,identity),ct=>Host.Client.QueryAsync("Source",request,context,["index","quota","health"],(core,deadline,cancel)=>core.SourceAsync(request,deadline:deadline,cancellationToken:cancel),ct,true),r=>DashboardPresentation.Kind(r.Meta));}
        else if(kind=="job")
        {var request=new JobRequest{JobId=identity};await job.Slot.LoadAsync((context.Generation,identity),ct=>Host.Client.QueryAsync("Job",request,context,["index","health"],(core,deadline,cancel)=>core.JobAsync(request,deadline:deadline,cancellationToken:cancel),ct,true),r=>DashboardPresentation.Kind(r.Meta));}
        if(reveal)Host.Dispatch(()=>
        {
            if(disposed || selected!=(kind,identity))return;
            expanded.UpdateLayout();expanded.StartBringIntoView(new BringIntoViewOptions{VerticalAlignmentRatio=0});
        });
    }
    private void UpdateBackButtons()
    {
        sourceBack.Visibility=sourcePane.ActualWidth<900 && source.View.Visibility==Visibility.Visible?Visibility.Visible:Visibility.Collapsed;
        jobBack.Visibility=jobPane.ActualWidth<900 && job.View.Visibility==Visibility.Visible?Visibility.Visible:Visibility.Collapsed;
    }
    private void RenderSource(StackPanel panel,SourceItem item)
    {
        panel.Children.Add(Text(SourceName(item)+" · "+RuntimeCopy.State(item.State,L)));
        var local=item.Kind=="local_file";
        if(local || item.SizeBytes?.HasValue==true)panel.Children.Add(PresentationControls.KeyValue(L.Text("文件大小","File size"),RuntimeCopy.Bytes(item.SizeBytes,L)));
        if(local || item.ParsedBytes?.HasValue==true)panel.Children.Add(PresentationControls.KeyValue(L.Text("已解析","Parsed"),RuntimeCopy.Bytes(item.ParsedBytes,L)));
        if(local || item.RowCount?.HasValue==true)panel.Children.Add(PresentationControls.KeyValue(L.Text("行数","Rows"),L.Number(item.RowCount)));
        if(local || item.LastScannedAtMs?.HasValue==true)panel.Children.Add(PresentationControls.KeyValue(L.Text("上次扫描","Last scan"),Time(item.LastScannedAtMs)));
        if(!local || item.LastAttemptAtMs?.HasValue==true)panel.Children.Add(PresentationControls.KeyValue(L.Text("上次尝试","Last attempt"),Time(item.LastAttemptAtMs)));
        if(!local || item.LastSuccessAtMs?.HasValue==true)panel.Children.Add(PresentationControls.KeyValue(L.Text("上次成功","Last success"),Time(item.LastSuccessAtMs)));
        if(!local || item.NextDueAtMs?.HasValue==true)panel.Children.Add(PresentationControls.KeyValue(L.Text("下次到期","Next due"),Time(item.NextDueAtMs)));
        panel.Children.Add(PresentationControls.KeyValue(L.Text("连续失败","Consecutive failures"),L.Number(item.ConsecutiveFailures)));
        if(item.HasErrorClass)panel.Children.Add(PresentationControls.KeyValue(L.Text("错误类别","Error class"),item.ErrorClass));
        if(item.HasCoverageState)panel.Children.Add(PresentationControls.KeyValue(L.Text("覆盖状态","Coverage"),item.CoverageState));
        if(item.HasCheckpointKind)panel.Children.Add(PresentationControls.KeyValue(L.Text("检查点","Checkpoint"),item.CheckpointKind));
        if(actions.Recovery(item.RecoveryAction?.Kind??"") is {} recovery)panel.Children.Add(recovery);
    }
    private void RenderJob(StackPanel panel,JobItem item)
    {
        panel.Children.Add(Text(RuntimePresentation.Phase(item.Phase,L)+" · "+RuntimeCopy.State(item.State,L)));
        panel.Children.Add(PresentationControls.MetricStrip((L.Text("进度","Progress"),L.Number(item.Progress?.Current)+" / "+L.Number(item.Progress?.Total)),(L.Text("失败次数","Failures"),L.Number(item.FailureCount))));
        foreach(var field in new[]{(L.Text("创建","Created"),Time(item.CreatedAtMs)),(L.Text("开始","Started"),Time(item.StartedAtMs)),(L.Text("完成","Finished"),Time(item.FinishedAtMs)),(L.Text("上次成功","Last success"),Time(item.LastSuccessAtMs)),(L.Text("下次重试","Next retry"),Time(item.NextRetryAtMs))})panel.Children.Add(PresentationControls.KeyValue(field.Item1,field.Item2));
        if(item.HasErrorClass)panel.Children.Add(Text(L.Text("错误类别：","Error class: ")+item.ErrorClass));
        if(item.HasSourceKey){var open=new Button{Content=L.Text("查看关联来源","View related source")};open.Click+=async(_,_)=>await Detail("source",item.SourceKey);panel.Children.Add(open);}
        if(actions.Recovery(item.RecoveryAction?.Kind??"") is {} recovery)panel.Children.Add(recovery);
    }
    private string Time(NumericValue? value)=>L.Timestamp(value?.HasValue==true?value.Value:null);
    public override void Cancel(){sources.Slot.Cancel();jobs.Slot.Cancel();source.Slot.Cancel();job.Slot.Cancel();}
    public override void Dispose(){disposed=true;sources.Dispose();jobs.Dispose();source.Dispose();job.Dispose();}
}
