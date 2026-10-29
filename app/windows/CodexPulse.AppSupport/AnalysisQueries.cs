using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public sealed record SessionFilters(string Activity="",string Project="",string Model="",string Sort="lastActivityAt",string Direction="desc",int Limit=20);
public sealed record ProjectFilters(string Confidence="",string Project="",string Sort="lastActivityAt",string Direction="desc",int Limit=20);

public sealed class CursorPager
{
    private readonly Stack<string?> previous=[];
    public string? Cursor {get;private set;}
    public int Page {get;private set;}=1;
    public bool CanPrevious=>previous.Count>0;
    public bool Next(PageInfo? info)
    {
        if(info is not {HasMore:true,HasNextCursor:true} || string.IsNullOrEmpty(info.NextCursor) || info.NextCursor==Cursor)return false;
        previous.Push(Cursor);Cursor=info.NextCursor;Page++;return true;
    }
    public bool Previous(){if(!previous.TryPop(out var cursor))return false;Cursor=cursor;Page--;return true;}
    public void Reset(){Cursor=null;Page=1;previous.Clear();}
    public PageRequest Request(int limit){var page=new PageRequest{Limit=limit};if(Cursor is not null)page.Cursor=Cursor;return page;}
}

public sealed class SessionNavigation
{
    public QueryContext? Context {get;set;}
    public UTCTimeRange? ExactRange{get;set;}
    public SessionFilters Filters {get;set;}=new();
    public CursorPager Pager {get;}=new();
    public string? Selected {get;set;}
}
public sealed class ProjectNavigation
{
    public QueryContext? Context {get;set;}
    public UTCTimeRange? ExactRange{get;set;}
    public ProjectFilters Filters {get;set;}=new();
    public CursorPager Pager {get;}=new();
    public string? Selected {get;set;}
}
public sealed class AnalysisNavigation
{
    private readonly Dictionary<string,SessionNavigation> sessions=[];
    private readonly Dictionary<string,ProjectNavigation> projects=[];
    public SessionNavigation Sessions(string provider){if(!sessions.TryGetValue(provider,out var state))sessions[provider]=state=new();return state;}
    public ProjectNavigation Projects(string provider){if(!projects.TryGetValue(provider,out var state))projects[provider]=state=new();return state;}
    public void Clear(){sessions.Clear();projects.Clear();}
}

public static class AnalysisQueries
{
    public static UTCTimeRange ExactScope(QueryContext context,UTCTimeRange? exact=null)=>exact?.Clone()??DashboardPresentation.ResolveRange(context with{Preset="custom"},null,DateTimeOffset.Now).Exact;
    private static QueryRequest Query(QueryContext context,CursorPager pager,int limit,string sort,string direction,UTCTimeRange? exact)
    {
        var query=new QueryRequest{Provider=new(){Provider=context.Provider},Page=pager.Request(limit)};
        query.Sort.Add(new SortTerm{Field=sort,Direction=direction});
        if(exact is null)query.TimeRange=DashboardPresentation.LocalRange(context);else query.ExactTimeRange=exact.Clone();
        return query;
    }
    private static void Filter(QueryRequest query,string field,string value)
    {
        if(value.Length==0)return;
        var term=new FilterTerm{Field=field,Operator="eq"};term.Values.Add(value);query.Filters.Add(term);
    }
    public static ListSessionsRequest Sessions(QueryContext context,SessionFilters filters,CursorPager pager,UTCTimeRange? exact=null)
    {
        if(filters.Activity is not ("" or "active" or "idle") || filters.Sort is not ("lastActivityAt" or "totalTokens" or "estimatedCost") || filters.Direction is not ("asc" or "desc") || filters.Limit is not (10 or 20 or 50 or 100))throw new ArgumentException("invalid_session_filters");
        var query=Query(context,pager,filters.Limit,filters.Sort,filters.Direction,exact);
        Filter(query,"activity",filters.Activity);Filter(query,"projectId",filters.Project);Filter(query,"modelKey",filters.Model);
        return new(){Query=query};
    }
    public static ListProjectsRequest Projects(QueryContext context,ProjectFilters filters,CursorPager pager,UTCTimeRange? exact=null)
    {
        if(filters.Confidence is not ("" or "high" or "medium" or "low" or "unknown" or "conflict" or "invalid") || filters.Sort is not ("lastActivityAt" or "totalTokens" or "estimatedCost" or "displayName") || filters.Direction is not ("asc" or "desc") || filters.Limit is not (10 or 20 or 50 or 100))throw new ArgumentException("invalid_project_filters");
        var query=Query(context,pager,filters.Limit,filters.Sort,filters.Direction,exact);
        Filter(query,"confidence",filters.Confidence);Filter(query,"projectId",filters.Project);
        return new(){Query=query};
    }
    public static ProjectDetailRequest ProjectDetail(QueryContext context,string dimension,CursorPager sessions,CursorPager models,UTCTimeRange? exact=null)
    {
        var request=new ProjectDetailRequest{Provider=new(){Provider=context.Provider},DimensionKey=dimension,SessionPage=sessions.Request(20),ModelPage=models.Request(20)};
        if(exact is null)request.Range=DashboardPresentation.LocalRange(context);else request.ExactRange=exact.Clone();
        return request;
    }
    public static string CacheRate(NumericValue? value,Localization l)=>value is {HasValue:true,HasUnknownReason:false,Unit:"basis_points",Value:>=0 and <=10000}?(value.Value/100m).ToString("N1",l.Culture)+"%":"—";
    public static string Tps(ThroughputStats? stats,Localization l)=>stats?.AverageOutputMilliTps is {HasValue:true,Unit:"milli_tokens_per_second",Value:>=0} value?(value.Value/1000m).ToString("N2",l.Culture)+" TPS":"—";
    public static string Duration(NumericValue? value,Localization l)=>value is {HasValue:true,Unit:"milliseconds",Value:>=0}?(value.Value/1000m).ToString("N3",l.Culture)+l.Text(" 秒"," s"):"—";
    public static string ThroughputStatus(ThroughputStats? stats,Localization l)=>stats?.Status switch
    {"complete"=>l.Text("已结束轮次平均","Average of completed turns"),"partial"=>l.Text("已知轮次平均","Average of known turns"),_=>l.Text("暂不可用","Unavailable")};
    public static string ThroughputReason(ThroughputStats? stats,Localization l)=>stats is null?l.Text("此来源暂不支持 TPS","TPS is not available for this source"):stats.Reason switch
    {
        "index_pending"=>l.Text("TPS 数据待补齐","TPS data is awaiting indexing"),
        "index_incomplete"=>l.Text("索引尚未追平，当前为已知轮次平均。","Indexing is catching up; the average uses known turns."),
        "open_turn"=>l.Text("当前轮待结束","The current turn is still open"),
        "no_closed_turns"=>l.Text("尚无可计算的已结束轮次","No completed turns are available for calculation"),
        "incomplete_coverage"=>l.Text("部分轮次未结束或缺少统计事实。","Some turns are open or lack usage facts."),
        "missing_facts"=>l.Text("轮次缺少有效输出或时长","Turns lack valid output or duration"),
        "numeric_overflow"=>l.Text("统计数值超出支持范围","Values exceed the supported range"),
        "state_limit"=>l.Text("轮次记录超出支持范围","Turn records exceed the supported range"),
        "inherited_history"=>l.Text("会话包含继承历史，无法确认独立 TPS","The session includes inherited history; independent TPS is unknown"),
        ""=>"",_=>l.Text("详细状态见计算口径","See calculation details for the full status")
    };
    public static string UsageCoverageReason(string reason,Localization l)=>reason switch
    {
        "rollup_missing"=>l.Text("部分汇总尚未补齐，当前显示已知用量。","Some summaries are still pending; known usage is shown."),
        "rollup_ambiguous"=>l.Text("部分汇总归属尚未确认，当前显示已知用量。","Some summary attribution is unresolved; known usage is shown."),
        _=>l.Text("部分用量尚未确认。","Some usage is not yet confirmed.")
    };
    public static string TrendGranularity(string granularity,Localization l)=>granularity switch
    {"hour"=>l.Text("按小时","Hourly"),"day"=>l.Text("按天","Daily"),"week"=>l.Text("按周","Weekly"),"month"=>l.Text("按月","Monthly"),_=>l.Text("统计粒度未知","Unknown granularity")};
}
