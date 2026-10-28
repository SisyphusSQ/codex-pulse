using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class SessionsPage:FeaturePage
{
    private readonly AnalysisChrome chrome;
    private readonly SessionNavigation navigation;
    private readonly QueryContext context;
    private readonly QuerySlot<SessionListResponse> list=new();
    private readonly QuerySlot<SessionDetailResponse> detail=new();
    private readonly CursorPager turns=new();
    private readonly ComboBox activity,project,model,sort,direction,limit;
    private SessionListResponse? renderedList;
    private SessionDetailResponse? renderedDetail;
    private bool disposed;
    private bool loadingList,listReloadPending;
    private long detailSequence;
    private string? selectedForTurns;
    public SessionsPage(MainWindow host):base(host)
    {
        context=host.Workspace.Context;navigation=host.Analysis.Sessions(context.Provider);
        if(navigation.Context!=context){navigation.Pager.Reset();navigation.Selected=null;navigation.ExactRange=null;navigation.Context=context;}
        if(host.Selection is{Feature:Feature.Sessions} selection){navigation.Selected=selection.Identity;navigation.ExactRange=selection.Range.Clone();navigation.Pager.Reset();}
        chrome=new(L);View=chrome.View;
        activity=AnalysisChrome.Combo(L.Text("活动状态","Activity"),[("",L.Text("全部","All")),("active",L.Text("进行中","Active")),("idle",L.Text("空闲","Idle"))],navigation.Filters.Activity);
        project=AnalysisChrome.Combo(L.Text("项目 · 精确筛选","Project · Exact filter"),[("",L.Text("全部项目","All projects"))],"");
        model=AnalysisChrome.Combo(L.Text("模型 · 精确筛选","Model · Exact filter"),[("",L.Text("全部模型","All models"))],"");
        sort=AnalysisChrome.Combo(L.Text("排序","Sort"),[("lastActivityAt",L.Text("最近活动","Last activity")),("totalTokens","Token"),("estimatedCost",L.Text("API 等价成本","API-equivalent cost"))],navigation.Filters.Sort);
        direction=AnalysisChrome.Combo(L.Text("顺序","Direction"),[("desc",L.Text("降序","Descending")),("asc",L.Text("升序","Ascending"))],navigation.Filters.Direction);
        limit=AnalysisChrome.Combo(L.Text("每页条数","Page size"),[("10","10"),("20","20"),("50","50"),("100","100")],navigation.Filters.Limit.ToString());
        var controls=new Grid {RowSpacing=10,ColumnSpacing=12};foreach(var control in new[]{activity,project,model,sort,direction,limit})controls.Children.Add(control);
        controls.SizeChanged+=(_,args)=>PresentationControls.Layout(controls,6,args.NewSize.Width);chrome.Filters.Children.Add(controls);
        chrome.Filters.Children.Add(Text(L.Text("项目和模型选项来自当前页；选择后由服务端精确筛选，不是标题全文搜索。","Project and model choices come from the current page. Exact filters run on the service; title full-text search is not available.")));
        var actions=new StackPanel {Orientation=Orientation.Horizontal,Spacing=8};
        var apply=new Button{Content=L.Text("应用筛选","Apply filters")};apply.Click+=async(_,_)=>{if(loadingList)return;navigation.Filters=new(AnalysisChrome.Choice(activity),AnalysisChrome.Choice(project),AnalysisChrome.Choice(model),AnalysisChrome.Choice(sort),AnalysisChrome.Choice(direction),int.Parse(AnalysisChrome.Choice(limit)));navigation.Pager.Reset();ClearSelection();await LoadListAsync(true);};
        var reset=new Button{Content=L.Text("重置筛选","Reset filters")};reset.Click+=async(_,_)=>{if(loadingList)return;navigation.Filters=new();activity.SelectedIndex=sort.SelectedIndex=direction.SelectedIndex=project.SelectedIndex=model.SelectedIndex=0;limit.SelectedIndex=1;navigation.Pager.Reset();ClearSelection();await LoadListAsync(true);};actions.Children.Add(apply);actions.Children.Add(reset);chrome.Filters.Children.Add(actions);
        chrome.Selected+=row=>Select((SessionItem)row.Source);
        chrome.First.Click+=async(_,_)=>{if(loadingList)return;navigation.Pager.Reset();ClearSelection();await LoadListAsync();};
        chrome.Previous.Click+=async(_,_)=>{if(!loadingList && navigation.Pager.Previous()){ClearSelection();await LoadListAsync();}};
        chrome.Next.Click+=async(_,_)=>{if(!loadingList && navigation.Pager.Next(list.State.Value?.Meta?.Page)){ClearSelection();await LoadListAsync();}};
        list.Changed+=()=>Host.Dispatch(RenderList);detail.Changed+=()=>Host.Dispatch(RenderDetail);
    }
    public override async Task LoadAsync(bool force=false)
    {await Task.WhenAll(LoadListAsync(force),navigation.Selected is not null?LoadDetailAsync(force):Task.CompletedTask);}
    private async Task LoadListAsync(bool force=false)
    {
        if(disposed)return;if(loadingList){listReloadPending|=force;return;}loadingList=true;
        try
        {
            do { listReloadPending=false;
            for(var attempt=0;attempt<2;attempt++)
            {
                var request=AnalysisQueries.Sessions(context,navigation.Filters,navigation.Pager,navigation.ExactRange);
                await list.LoadAsync((context,navigation.Filters,navigation.Pager.Cursor,navigation.ExactRange?.StartAtMs,navigation.ExactRange?.EndAtMs),token=>Host.Client.QueryAsync("ListSessions",request,context,["index","account"],
                    (client,deadline,cancel)=>client.ListSessionsAsync(request,deadline:deadline,cancellationToken:cancel),token,force),response=>DashboardPresentation.Kind(response.Meta,response.Items.Count==0));
                if(disposed)return;
                if(attempt==0 && navigation.Pager.Cursor is not null && list.State is{Reason:"validation",Field:"page.cursor"}){navigation.Pager.Reset();chrome.Notice.Text=L.Text("分页已更新，已回到首页。","Pagination changed. Returned to the first page.");continue;}
                break;
            }
            } while(listReloadPending && !disposed);
        }
        finally{loadingList=false;}
    }
    private void Select(SessionItem item)
    {
        chrome.ShowDetail(true);
        if(navigation.Selected==item.SessionId){if(detail.State.Kind is LoadKind.Cancelled or LoadKind.Unavailable)_=LoadDetailAsync(true);return;}
        detail.Clear();turns.Reset();navigation.Selected=item.SessionId;_ = LoadDetailAsync();
    }
    private void ClearSelection()
    {detailSequence++;navigation.Selected=null;detail.Clear();turns.Reset();renderedDetail=null;chrome.DetailBody.Children.Clear();chrome.DetailBody.Children.Add(Text(L.Text("选择会话查看详情。","Select a session for details.")));chrome.ShowDetail(false);}
    private async Task LoadDetailAsync(bool force=false)
    {
        if(disposed || navigation.Selected is not {} id)return;
        var sequence=++detailSequence;
        if(selectedForTurns!=id){turns.Reset();selectedForTurns=id;}
            for(var attempt=0;attempt<2;attempt++)
            {
                var request=new SessionDetailRequest {Provider=new(){Provider=context.Provider},SessionId=id,ReportingTimezone=context.TimeZone,TurnPage=turns.Request(20)};
                await detail.LoadAsync((context,id,turns.Cursor),token=>Host.Client.QueryAsync("SessionDetail",request,context,["index","account"],
                    (client,deadline,cancel)=>client.SessionDetailAsync(request,deadline:deadline,cancellationToken:cancel),token,force),response=>DashboardPresentation.Kind(response.Meta));
                if(disposed || navigation.Selected!=id || sequence!=detailSequence)return;
                if(detail.State.Reason is "not_found" or "NotFound"){ClearSelection();chrome.Notice.Text=L.Text("该会话已不在当前来源中，列表保留。","This session is no longer in the source. The list is retained.");return;}
                if(attempt==0 && turns.Cursor is not null && detail.State is{Reason:"validation",Field:"turnPage.cursor"}){turns.Reset();chrome.Notice.Text=L.Text("轮次分页已失效，已重新读取首页。","Turn pagination expired. Reloaded the first page.");continue;}
                break;
            }
    }
    private void RenderList()
    {
        if(disposed)return;
        State(list.State.Kind);chrome.Summary.Text=L.State(list.State.Kind);chrome.Paging(navigation.Pager,list.State.Value?.Meta?.Page,list.State.Kind==LoadKind.Loading);
        if(list.State.Value is not {} response){renderedList=null;chrome.Rows([],navigation.Selected);return;}
        chrome.Summary.Text=L.State(list.State.Kind)+" · "+L.Text("匹配会话 ","Matched sessions ")+L.Number(response.MatchedCount)+" · "+L.Number(response.MatchedTotals?.TotalTokens)+" Token · "+L.Money(response.MatchedTotals?.EstimatedUsdMicros);
        if(Equals(renderedList,response))return;renderedList=response;
        var scope=navigation.ExactRange is {} exact?L.Text("下钻精确范围 ","Drilldown exact range ")+L.Timestamp(exact.StartAtMs)+" — "+L.Timestamp(exact.EndAtMs)+" · "+exact.TimeZone:context.Start.ToString("yyyy-MM-dd")+" — "+context.EndExclusive.ToString("yyyy-MM-dd")+L.Text("（结束日不含）"," (end exclusive)")+" · "+context.TimeZone;
        chrome.Notice.Text=scope+"\n"+L.Text("按最近活动时间筛选；Token、成本、缓存命中率和 TPS 都是会话生命周期指标。","Filtered by last activity; tokens, cost, cache rate, and TPS describe the session lifetime.");
        chrome.Rows(response.Items.Select(item=>new AnalysisRow {Identity=item.SessionId,Source=item,Title=item.DisplayTitle,
            Subtitle=DashboardPresentation.Name(item.Project,L.Text("未归类项目","Unclassified project"))+" · "+DashboardPresentation.Name(item.Model,L.Text("未归类模型","Unattributed model")),
            Metrics=L.Number(item.Totals?.TotalTokens)+" Token · "+L.Money(item.Totals?.EstimatedUsdMicros),
            Evidence=(item.Activity=="active"?L.Text("进行中","Active"):L.Text("空闲","Idle"))+" · "+L.Timestamp(item.LastActivityAtMs?.HasValue==true?item.LastActivityAtMs.Value:null)+(context.Provider=="codex"?" · "+L.Text("缓存 ","Cache ")+AnalysisQueries.CacheRate(item.CacheHitRate,L)+" · "+AnalysisQueries.Tps(item.Throughput,L):"")}),navigation.Selected);
        AnalysisChrome.Options(project,response.Items.Where(item=>item.Project?.HasId==true).Select(item=>(item.Project.Id,DashboardPresentation.Name(item.Project,"—"))),navigation.Filters.Project,L.Text("全部项目","All projects"),L.Text("已选项目","Selected project"));
        AnalysisChrome.Options(model,response.Items.Where(item=>item.Model?.HasId==true).Select(item=>(item.Model.Id,DashboardPresentation.Name(item.Model,"—"))),navigation.Filters.Model,L.Text("全部模型","All models"),L.Text("已选模型","Selected model"));
    }
    private void RenderDetail()
    {
        if(disposed)return;chrome.DetailStatus.Text=L.State(detail.State.Kind);
        if(detail.State.Value is not {} response){renderedDetail=null;chrome.DetailBody.Children.Clear();return;}
        if(Equals(renderedDetail,response))return;renderedDetail=response;var panel=chrome.DetailBody;panel.Children.Clear();var item=response.Item;
        panel.Children.Add(Text(item.DisplayTitle,"SubtitleTextBlockStyle"));
        panel.Children.Add(Text(DashboardPresentation.Name(item.Project,L.Text("未归类项目","Unclassified project"))+" · "+DashboardPresentation.Name(item.Model,L.Text("未归类模型","Unattributed model"))));
        panel.Children.Add(Text(L.Text("完整会话生命周期；不随列表范围或轮次翻页重新累计。","Full session lifetime. List ranges and turn pages do not recalculate these totals.")));
        panel.Children.Add(PresentationControls.Metrics(("Token",L.Number(item.Totals?.TotalTokens)),(L.Text("API 等价估算成本","API-equivalent estimate"),L.Money(item.Totals?.EstimatedUsdMicros))));
        panel.Children.Add(PresentationControls.Table([L.Text("输入","Input"),L.Text("缓存输入","Cached input"),L.Text("输出","Output"),L.Text("推理","Reasoning")],
            [new[]{L.Number(item.Totals?.InputTokens),L.Number(item.Totals?.CachedInputTokens),L.Number(item.Totals?.OutputTokens),L.Number(item.Totals?.ReasoningTokens)}]));
        panel.Children.Add(Text(L.Text("归因/来源：","Attribution/source: ")+item.Project?.Confidence+" · "+item.Project?.Source+" · "+item.Project?.Reason+" / "+item.Model?.Confidence+" · "+item.Model?.Source));
        if(response.HasDegradedReason)panel.Children.Add(Text(L.Text("部分数据：","Partial data: ")+response.DegradedReason));
        panel.Children.Add(Text(L.Text("定价依据：","Pricing basis: ")+(response.HasPricingSource?response.PricingSource:"—")+" · "+string.Join(", ",response.PricingVersions)+"\n"+L.Text("估算不是订阅账单；未定价轮数 ","This estimate is not a subscription bill. Unpriced turns ")+L.Number(item.Totals?.UnpricedTurnCount)));
        if(response.UnpricedReasons.Count>0)panel.Children.Add(Text(string.Join(" · ",response.UnpricedReasons.Select(reason=>reason.Reason+" "+L.Number(reason.Count)))));
        if(context.Provider=="codex")
        {
            panel.Children.Add(Text(L.Text("缓存命中率 ","Cache hit rate ")+AnalysisQueries.CacheRate(item.CacheHitRate,L)+"\n"+L.Text("已索引的会话累计缓存输入 / 输入；缓存已包含在输入中。","Indexed lifetime cached input / input. Cached tokens are already included in input.")));
            panel.Children.Add(Text(L.Text("活跃期间平均输出 ","Average active output ")+AnalysisQueries.Tps(item.Throughput,L),"SubtitleTextBlockStyle"));
            if(item.Throughput is {} throughput)
            {
                panel.Children.Add(Text(L.Text("输出 Token / 活跃区间并集；含思考、工具和轮内等待，不含轮间空闲。","Output tokens / union of active intervals. Includes reasoning, tools, and in-turn waits; excludes idle time between turns.")));
                panel.Children.Add(PresentationControls.Table([L.Text("输出 Token","Output tokens"),L.Text("活跃时长","Active duration"),L.Text("参与 / 排除 / 未结束","Included / excluded / open")],
                    [new[]{L.Number(throughput.OutputTokens),AnalysisQueries.Duration(throughput.ActiveDurationMs,L),L.Number(throughput.IncludedTurns)+" / "+L.Number(throughput.ExcludedTurns)+" / "+L.Number(throughput.OpenTurns)}]));
                panel.Children.Add(Text(L.Text("覆盖 / 原因 / 时间来源：","Coverage / reason / time source: ")+throughput.Status+" · "+throughput.Reason+" · "+throughput.DurationSource));
            }
            panel.Children.Add(PresentationControls.Disclosure(L.Text("最近轮次 TPS","Recent turn TPS"),PresentationControls.Table([L.Text("开始时间","Started"),"TPS",L.Text("输出 Token","Output tokens"),L.Text("活跃时长","Active duration"),L.Text("覆盖","Coverage")],
                response.ThroughputTurns.Select(turn=>new[]{L.Timestamp(turn.StartedAtMs?.HasValue==true?turn.StartedAtMs.Value:null),AnalysisQueries.Tps(turn.Throughput,L),L.Number(turn.Throughput?.OutputTokens),AnalysisQueries.Duration(turn.Throughput?.ActiveDurationMs,L),turn.Throughput?.Status+" · "+turn.Throughput?.Reason}))));
        }
        else panel.Children.Add(Text(L.Text("此来源不提供同口径的缓存命中率和 TPS。","This source does not provide comparable cache-hit or TPS metrics.")));
        if(response.Trend.Count>0)
        {
            panel.Children.Add(Text(L.Text("会话用量趋势 · ","Session usage trend · ")+response.TrendGranularity+" · "+response.ReportingTimeZone,"SubtitleTextBlockStyle"));
            var usage=new UsageCostResponse();usage.Trend.Add(response.Trend);usage.Models.Add(response.Models);panel.Children.Add(Charts.Bars(DashboardPresentation.UsageTrend(usage),L,"Token",true));
        }
        if(item.Project?.HasId==true)
        {
            var related=new Button{Content=L.Text("查看此项目的区间贡献","View this project's range contributions")};
            related.Click+=(_,_)=>Host.OpenProjectList(item.Project.Id,AnalysisQueries.ExactScope(context,navigation.ExactRange));panel.Children.Add(related);
        }
        panel.Children.Add(Text(L.Text("轮次用量时间线 · 本页不是整段会话合计","Turn usage timeline · This page is not the session total"),"SubtitleTextBlockStyle"));
        if(response.Turns.Count==0)panel.Children.Add(Text(L.Text("当前未返回轮次用量明细。上方生命周期指标按 Helper 已返回的事实展示。","No turn usage details were returned. Lifetime metrics above use the facts returned by the Helper.")));
        panel.Children.Add(PresentationControls.Table([L.Text("开始 / 结束","Started / ended"),L.Text("模型 / 状态","Model / state"),"Token",L.Text("API 等价成本","API estimate"),L.Text("定价依据","Pricing evidence")],
            response.Turns.Select(turn=>new[]{L.Timestamp(turn.StartedAtMs?.HasValue==true?turn.StartedAtMs.Value:null)+" / "+L.Timestamp(turn.CompletedAtMs?.HasValue==true?turn.CompletedAtMs.Value:null),DashboardPresentation.Name(turn.Model,"—")+" · "+turn.State,L.Number(turn.Totals?.TotalTokens),L.Money(turn.Totals?.EstimatedUsdMicros),turn.PricingStatus+" · "+(turn.HasPricingVersion?turn.PricingVersion:turn.HasUnpricedReason?turn.UnpricedReason:"—")})));
        var pager=new StackPanel{Orientation=Orientation.Horizontal,Spacing=8};var first=new Button{Content=L.Text("轮次首页","First turns")};var previous=new Button{Content=L.Text("上一页","Previous"),IsEnabled=turns.CanPrevious};var next=new Button{Content=L.Text("下一页轮次","Next turns"),IsEnabled=response.TurnPage is{HasMore:true,HasNextCursor:true}};
        first.Click+=async(_,_)=>{if(detail.State.Kind==LoadKind.Loading)return;turns.Reset();await LoadDetailAsync();};previous.Click+=async(_,_)=>{if(detail.State.Kind!=LoadKind.Loading && turns.Previous())await LoadDetailAsync();};next.Click+=async(_,_)=>{if(detail.State.Kind!=LoadKind.Loading && turns.Next(response.TurnPage))await LoadDetailAsync();};pager.Children.Add(first);pager.Children.Add(previous);pager.Children.Add(Text(turns.Page.ToString()));pager.Children.Add(next);panel.Children.Add(new ScrollViewer{Content=pager,HorizontalScrollBarVisibility=ScrollBarVisibility.Auto,VerticalScrollBarVisibility=ScrollBarVisibility.Disabled});
    }
    public override void Cancel(){listReloadPending=false;list.Cancel();detail.Cancel();}
    public override void Dispose(){disposed=true;detailSequence++;list.Dispose();detail.Dispose();}
}
