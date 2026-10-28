using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class ProjectsPage:FeaturePage
{
    private readonly AnalysisChrome chrome;
    private readonly ProjectNavigation navigation;
    private readonly QueryContext context;
    private readonly QuerySlot<ProjectListResponse> list=new();
    private readonly QuerySlot<ProjectDetailResponse> detail=new();
    private readonly CursorPager sessionPage=new(),modelPage=new();
    private readonly ComboBox confidence,project,sort,direction,limit;
    private ProjectListResponse? renderedList;
    private ProjectDetailResponse? renderedDetail;
    private bool disposed,loadingList,listReloadPending;
    private long detailSequence;
    public ProjectsPage(MainWindow host):base(host)
    {
        context=host.Workspace.Context;navigation=host.Analysis.Projects(context.Provider);
        if(navigation.Context!=context){navigation.Pager.Reset();navigation.Selected=null;navigation.ExactRange=null;navigation.Context=context;}
        if(host.Selection is{Feature:Feature.Projects} selection)
        {navigation.Selected=string.IsNullOrEmpty(selection.Identity)?null:selection.Identity;navigation.ExactRange=selection.Range.Clone();navigation.Pager.Reset();if(selection.ProjectFilter is {} filter)navigation.Filters=navigation.Filters with{Project=filter};}
        chrome=new(L);View=chrome.View;
        confidence=AnalysisChrome.Combo(L.Text("归因置信度","Attribution confidence"),[("",L.Text("全部","All")),("high",L.Text("高","High")),("medium",L.Text("中","Medium")),("low",L.Text("低","Low")),("unknown",L.Text("未知","Unknown")),("conflict",L.Text("冲突","Conflict")),("invalid",L.Text("无效","Invalid"))],navigation.Filters.Confidence);
        project=AnalysisChrome.Combo(L.Text("项目 · 精确筛选","Project · Exact filter"),[("",L.Text("全部项目","All projects"))],"");
        sort=AnalysisChrome.Combo(L.Text("排序","Sort"),[("lastActivityAt",L.Text("最近活动","Last activity")),("totalTokens","Token"),("estimatedCost",L.Text("API 等价成本","API-equivalent cost")),("displayName",L.Text("项目名称","Project name"))],navigation.Filters.Sort);
        direction=AnalysisChrome.Combo(L.Text("顺序","Direction"),[("desc",L.Text("降序","Descending")),("asc",L.Text("升序","Ascending"))],navigation.Filters.Direction);
        limit=AnalysisChrome.Combo(L.Text("每页条数","Page size"),[("10","10"),("20","20"),("50","50"),("100","100")],navigation.Filters.Limit.ToString());
        var controls=new Grid {RowSpacing=10,ColumnSpacing=12};foreach(var control in new[]{confidence,project,sort,direction,limit})controls.Children.Add(control);
        controls.SizeChanged+=(_,args)=>PresentationControls.Layout(controls,5,args.NewSize.Width);chrome.Filters.Children.Add(controls);
        chrome.Filters.Children.Add(Text(L.Text("置信度按当前范围聚合后的结果筛选；项目选项来自当前页，选择后由服务端精确匹配。","Confidence filters use the range-level result. Project choices come from the current page and run as exact service filters.")));
        var actions=new StackPanel {Orientation=Orientation.Horizontal,Spacing=8};
        var apply=new Button {Content=L.Text("应用筛选","Apply filters")};apply.Click+=async(_,_)=>{if(loadingList)return;navigation.Filters=new(AnalysisChrome.Choice(confidence),AnalysisChrome.Choice(project),AnalysisChrome.Choice(sort),AnalysisChrome.Choice(direction),int.Parse(AnalysisChrome.Choice(limit)));navigation.Pager.Reset();ClearSelection();await LoadListAsync(true);};
        var reset=new Button{Content=L.Text("重置筛选","Reset filters")};reset.Click+=async(_,_)=>{if(loadingList)return;navigation.Filters=new();confidence.SelectedIndex=sort.SelectedIndex=direction.SelectedIndex=project.SelectedIndex=0;limit.SelectedIndex=1;navigation.Pager.Reset();ClearSelection();await LoadListAsync(true);};actions.Children.Add(apply);actions.Children.Add(reset);chrome.Filters.Children.Add(actions);
        chrome.Selected+=row=>Select((ProjectItem)row.Source);
        chrome.First.Click+=async(_,_)=>{if(loadingList)return;navigation.Pager.Reset();ClearSelection();await LoadListAsync();};
        chrome.Previous.Click+=async(_,_)=>{if(!loadingList && navigation.Pager.Previous()){ClearSelection();await LoadListAsync();}};
        chrome.Next.Click+=async(_,_)=>{if(!loadingList && navigation.Pager.Next(list.State.Value?.Meta?.Page)){ClearSelection();await LoadListAsync();}};
        list.Changed+=()=>Host.Dispatch(RenderList);detail.Changed+=()=>Host.Dispatch(RenderDetail);
    }
    public override async Task LoadAsync(bool force=false)
    {await Task.WhenAll(LoadListAsync(force),navigation.Selected is not null?LoadDetailAsync(force):Task.CompletedTask);}
    public override void Invalidate(IReadOnlyCollection<string> domains)
    {
        if(domains.Contains("index") && (sessionPage.Cursor is not null || modelPage.Cursor is not null))
        {sessionPage.Reset();modelPage.Reset();chrome.Notice.Text=L.Text("索引已更新，贡献分页已回到首页。","The index changed. Contribution pages returned to the first page.");}
    }
    private async Task LoadListAsync(bool force=false)
    {
        if(disposed)return;if(loadingList){listReloadPending|=force;return;}loadingList=true;
        try
        {
            do { listReloadPending=false;
            for(var attempt=0;attempt<2;attempt++)
            {
                var request=AnalysisQueries.Projects(context,navigation.Filters,navigation.Pager,navigation.ExactRange);
                await list.LoadAsync((context,navigation.Filters,navigation.Pager.Cursor,navigation.ExactRange?.StartAtMs,navigation.ExactRange?.EndAtMs),token=>Host.Client.QueryAsync("ListProjects",request,context,["index","account"],
                    (client,deadline,cancel)=>client.ListProjectsAsync(request,deadline:deadline,cancellationToken:cancel),token,force),response=>DashboardPresentation.Kind(response.Meta,response.Items.Count==0));
                if(disposed)return;
                if(attempt==0 && navigation.Pager.Cursor is not null && list.State is{Reason:"validation",Field:"page.cursor"}){navigation.Pager.Reset();chrome.Notice.Text=L.Text("分页已更新，已回到首页。","Pagination changed. Returned to the first page.");continue;}
                break;
            }
            } while(listReloadPending && !disposed);
        }
        finally{loadingList=false;}
    }
    private void Select(ProjectItem item)
    {
        chrome.ShowDetail(true);
        if(navigation.Selected==item.DimensionKey){if(detail.State.Kind is LoadKind.Cancelled or LoadKind.Unavailable)_=LoadDetailAsync(true);return;}
        detail.Clear();sessionPage.Reset();modelPage.Reset();navigation.Selected=item.DimensionKey;_ = LoadDetailAsync();
    }
    private void ClearSelection()
    {detailSequence++;navigation.Selected=null;detail.Clear();sessionPage.Reset();modelPage.Reset();renderedDetail=null;chrome.DetailBody.Children.Clear();chrome.DetailBody.Children.Add(Text(L.Text("选择项目查看详情。","Select a project for details.")));chrome.ShowDetail(false);}
    private async Task LoadDetailAsync(bool force=false)
    {
        if(disposed || navigation.Selected is not {} dimension)return;
        var sequence=++detailSequence;
        for(var attempt=0;attempt<3;attempt++)
        {
            var request=AnalysisQueries.ProjectDetail(context,dimension,sessionPage,modelPage,navigation.ExactRange);
            await detail.LoadAsync((context,dimension,sessionPage.Cursor,modelPage.Cursor,navigation.ExactRange?.StartAtMs,navigation.ExactRange?.EndAtMs),token=>Host.Client.QueryAsync("ProjectDetail",request,context,["index","account"],
                (client,deadline,cancel)=>client.ProjectDetailAsync(request,deadline:deadline,cancellationToken:cancel),token,force),response=>DashboardPresentation.Kind(response.Meta));
            if(disposed || navigation.Selected!=dimension || sequence!=detailSequence)return;
            if(detail.State.Reason is "not_found" or "NotFound"){ClearSelection();chrome.Notice.Text=L.Text("该项目已不在当前范围中，列表保留。","This project is no longer in the range. The list is retained.");return;}
            if(attempt<2 && detail.State.Reason=="validation")
            {
                var reset=false;
                if(detail.State.Field=="sessionPage.cursor" && sessionPage.Cursor is not null){sessionPage.Reset();reset=true;}
                if(detail.State.Field=="modelPage.cursor" && modelPage.Cursor is not null){modelPage.Reset();reset=true;}
                if(reset){chrome.Notice.Text=L.Text("贡献分页已失效，已重新读取对应首页。","A contribution cursor expired. Reloaded that first page.");continue;}
            }
            break;
        }
    }
    private void RenderList()
    {
        if(disposed)return;State(list.State.Kind);chrome.Summary.Text=L.State(list.State.Kind);chrome.Paging(navigation.Pager,list.State.Value?.Meta?.Page,list.State.Kind==LoadKind.Loading);
        if(list.State.Value is not {} response){renderedList=null;chrome.Rows([],navigation.Selected);return;}
        chrome.Summary.Text=L.State(list.State.Kind)+" · "+L.Text("匹配项目 ","Matched projects ")+L.Number(response.MatchedCount)+" · "+L.Number(response.MatchedTotals?.TotalTokens)+" Token · "+L.Money(response.MatchedTotals?.EstimatedUsdMicros);
        if(Equals(renderedList,response))return;renderedList=response;
        chrome.Notice.Text=L.Timestamp(response.Range?.StartAtMs)+" — "+L.Timestamp(response.Range?.EndAtMs)+" · "+response.ReportingTimeZone+"\n"+L.Text("所选范围内贡献；全局 ","Contributions in this range. Global ")+L.Number(response.GlobalTotals?.TotalTokens)+" Token · "+L.Money(response.GlobalTotals?.EstimatedUsdMicros)+L.Text("；本页 ","; this page ")+L.Number(response.PageTotals?.TotalTokens)+" Token";
        chrome.Rows(response.Items.Select(item=>new AnalysisRow{Identity=item.DimensionKey,Source=item,Title=DashboardPresentation.Name(item.Project,L.Text("其他","Other")),
            Subtitle=L.Number(item.SessionCount)+L.Text(" 个会话 · "," sessions · ")+item.Project?.Confidence,
            Metrics=L.Number(item.Totals?.TotalTokens)+" Token · "+L.Money(item.Totals?.EstimatedUsdMicros),
            Evidence=L.Text("来源 ","Source ")+item.Project?.Source+" · "+item.Project?.Reason}),navigation.Selected);
        AnalysisChrome.Options(project,response.Items.Where(item=>item.Project?.HasId==true).Select(item=>(item.Project.Id,DashboardPresentation.Name(item.Project,"—"))),navigation.Filters.Project,L.Text("全部项目","All projects"),L.Text("已选项目","Selected project"));
    }
    private void RenderDetail()
    {
        if(disposed)return;chrome.DetailStatus.Text=L.State(detail.State.Kind);
        if(detail.State.Value is not {} response){renderedDetail=null;chrome.DetailBody.Children.Clear();return;}
        if(Equals(renderedDetail,response))return;renderedDetail=response;var panel=chrome.DetailBody;panel.Children.Clear();var item=response.Item;
        panel.Children.Add(Text(DashboardPresentation.Name(item.Project,L.Text("其他","Other")),"SubtitleTextBlockStyle"));
        panel.Children.Add(Text(L.Timestamp(response.Range?.StartAtMs)+" — "+L.Timestamp(response.Range?.EndAtMs)+" · "+response.ReportingTimeZone));
        panel.Children.Add(Text(L.Text("项目区间贡献；Session/模型分页不会改变这些权威总量。","Project range contributions. Session and model pages do not change these authoritative totals.")));
        panel.Children.Add(PresentationControls.Metrics(("Token",L.Number(item.Totals?.TotalTokens)),(L.Text("API 等价估算成本","API-equivalent estimate"),L.Money(item.Totals?.EstimatedUsdMicros)),(L.Text("会话数","Sessions"),L.Number(item.SessionCount))));
        panel.Children.Add(PresentationControls.Table([L.Text("输入","Input"),L.Text("缓存输入","Cached input"),L.Text("输出","Output"),L.Text("推理","Reasoning")],
            [new[]{L.Number(item.Totals?.InputTokens),L.Number(item.Totals?.CachedInputTokens),L.Number(item.Totals?.OutputTokens),L.Number(item.Totals?.ReasoningTokens)}]));
        panel.Children.Add(Text(L.Text("归因 / 来源：","Attribution / source: ")+item.Project?.Confidence+" · "+item.Project?.Source+" · "+item.Project?.Reason));
        panel.Children.Add(Text(L.Text("定价依据：","Pricing basis: ")+response.PricingSource+" · "+string.Join(", ",response.PricingVersions)+"\n"+L.Text("API 等价估算不是订阅账单；未定价轮数 ","API estimates are not subscription bills. Unpriced turns ")+L.Number(item.Totals?.UnpricedTurnCount)));
        panel.Children.Add(Text(L.Text("每日贡献","Daily contributions"),"SubtitleTextBlockStyle"));
        panel.Children.Add(Charts.Bars(response.Daily.Select(point=>new ChartDatum(L.Timestamp(point.BucketStartAtMs?.HasValue==true?point.BucketStartAtMs.Value:null),DashboardPresentation.Value(point.Totals?.TotalTokens),[],null,point.BucketStartAtMs?.HasValue==true?DateTimeOffset.FromUnixTimeMilliseconds(point.BucketStartAtMs.Value).ToLocalTime().ToString("MM-dd",L.Culture):"—")).ToArray(),L,"Token"));
        panel.Children.Add(PresentationControls.Disclosure(L.Text("每日贡献明细","Daily contribution data"),PresentationControls.Table([L.Text("日期","Date"),"Token",L.Text("API 等价估算","API estimate"),L.Text("置信度","Confidence")],response.Daily.Select(point=>new[]{L.Timestamp(point.BucketStartAtMs?.HasValue==true?point.BucketStartAtMs.Value:null),L.Number(point.Totals?.TotalTokens),L.Money(point.Totals?.EstimatedUsdMicros),point.Confidence}))));
        panel.Children.Add(Text(L.Text("模型贡献 · 独立分页","Model contributions · Independent pages"),"SubtitleTextBlockStyle"));
        if(response.Models.Count==0)panel.Children.Add(Text(L.Text("当前页没有模型贡献明细。","No model contribution details are available on this page.")));
        panel.Children.Add(PresentationControls.Table([L.Text("模型","Model"),"Token",L.Text("API 等价估算","API estimate"),L.Text("未定价轮数","Unpriced turns")],response.Models.Select(model=>new[]{DashboardPresentation.Name(model.Model,L.Text("未归类模型","Unattributed model")),L.Number(model.Totals?.TotalTokens),L.Money(model.Totals?.EstimatedUsdMicros),L.Number(model.Totals?.UnpricedTurnCount)})));
        panel.Children.Add(ContributionPager(modelPage,response.ModelPage,L.Text("模型","Models")));
        panel.Children.Add(Text(L.Text("会话贡献 · 独立分页","Session contributions · Independent pages"),"SubtitleTextBlockStyle"));
        if(response.Sessions.Count==0)panel.Children.Add(Text(L.Text("当前页没有会话贡献明细。","No session contribution details are available on this page.")));
        foreach(var session in response.Sessions)
        {
            var link=new Button{Content=new TextBlock{Text=session.DisplayTitle,TextWrapping=TextWrapping.Wrap,MaxLines=2},HorizontalAlignment=HorizontalAlignment.Stretch,HorizontalContentAlignment=HorizontalAlignment.Left};
            var range=response.Range?.Clone();link.IsEnabled=range is not null;link.Click+=(_,_)=>{if(range is not null)Host.OpenSession(session.SessionId,range);};panel.Children.Add(link);
            panel.Children.Add(Text(L.Number(session.Totals?.TotalTokens)+" Token · "+L.Text("API 等价估算 ","API estimate ")+L.Money(session.Totals?.EstimatedUsdMicros)+" · "+L.Text("模型 ","Model ")+DashboardPresentation.Name(session.Model,"—")));
        }
        panel.Children.Add(Text(L.Text("上列数值仅是本项目在当前范围的贡献；打开会话后查看完整生命周期。","Values above are contributions to this project in this range. Opening a session shows its full lifetime.")));
        panel.Children.Add(ContributionPager(sessionPage,response.SessionPage,L.Text("会话","Sessions")));
    }
    private FrameworkElement ContributionPager(CursorPager pager,PageInfo info,string label)
    {
        var row=new StackPanel{Orientation=Orientation.Horizontal,Spacing=8};var first=new Button{Content=label+L.Text("首页"," first")};var previous=new Button{Content=L.Text("上一页","Previous"),IsEnabled=pager.CanPrevious};var next=new Button{Content=L.Text("下一页","Next"),IsEnabled=info is{HasMore:true,HasNextCursor:true}};
        first.Click+=async(_,_)=>{if(detail.State.Kind==LoadKind.Loading)return;pager.Reset();await LoadDetailAsync();};previous.Click+=async(_,_)=>{if(detail.State.Kind!=LoadKind.Loading && pager.Previous())await LoadDetailAsync();};next.Click+=async(_,_)=>{if(detail.State.Kind!=LoadKind.Loading && pager.Next(info))await LoadDetailAsync();};
        row.Children.Add(first);row.Children.Add(previous);row.Children.Add(Text(pager.Page.ToString()));row.Children.Add(next);return new ScrollViewer{Content=row,HorizontalScrollBarVisibility=ScrollBarVisibility.Auto,VerticalScrollBarVisibility=ScrollBarVisibility.Disabled};
    }
    public override void Cancel(){listReloadPending=false;list.Cancel();detail.Cancel();}
    public override void Dispose(){disposed=true;detailSequence++;list.Dispose();detail.Dispose();}
}
