using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Google.Protobuf;

internal static class AnalysisTests
{
    private static void Require(bool value,string name){if(!value)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    public static void Run()
    {
        var pager=new CursorPager();
        Require(pager.Cursor is null && pager.Page==1 && !pager.CanPrevious,"analysis cursor starts at first page without a fabricated token");
        const string opaque="opaque-+/中文==not-for-client-decoding";
        Require(pager.Next(new PageInfo{HasMore=true,NextCursor=opaque}) && pager.Request(20).Cursor==opaque && pager.Page==2,"opaque keyset cursor is passed through unchanged");
        Require(!pager.Next(new PageInfo{HasMore=true,NextCursor=opaque}) && !pager.Next(new PageInfo{HasMore=false}),"missing or repeated next cursor cannot advance the page");
        Require(pager.Previous() && pager.Cursor is null && pager.Page==1 && !pager.Previous(),"previous page restores the exact prior cursor including first-page absence");
        pager.Next(new(){HasMore=true,NextCursor=opaque});pager.Reset();Require(pager.Cursor is null && !pager.CanPrevious,"range and filter reset removes cursor history");
        var context=new QueryContext("grok",new(2026,10,23),new(2026,10,30),"Asia/Hong_Kong",7);
        var sessions=AnalysisQueries.Sessions(context,new("active","opaque-project","safe-model","totalTokens","desc",50),pager);
        Require(sessions.Query.Provider.Provider=="grok" && sessions.Query.TimeRange.TimeZone==context.TimeZone && sessions.Query.ExactTimeRange is null && sessions.Query.Page.Limit==50,"session query keeps provider, local half-open range, and server page size");
        Require(sessions.Query.Filters.Count==3 && sessions.Query.Filters.All(filter=>filter.Operator=="eq") && sessions.Query.Filters.Single(filter=>filter.Field=="projectId").Values.Single()=="opaque-project","project, model, and activity filters run on the service without local title filtering");
        Require(sessions.Query.Sort.Single().Field=="totalTokens" && sessions.Query.Sort.Single().Direction=="desc","analysis sorting is a server query term");
        var exact=new UTCTimeRange{StartAtMs=1793156734123,EndAtMs=1793192345678,TimeZone=context.TimeZone};
        var projects=AnalysisQueries.Projects(context,new("unknown","","estimatedCost","asc",20),pager,exact);
        var roundtrip=ListProjectsRequest.Parser.ParseFrom(projects.ToByteArray());
        Require(roundtrip.Query.ExactTimeRange.Equals(exact) && roundtrip.Query.TimeRange is null && roundtrip.Query.Provider.Provider=="grok","project drilldown preserves exact millisecond boundaries and provider on the wire");
        Require(AnalysisQueries.ExactScope(context).EndAtMs==DateTimeOffset.Parse("2026-10-30T00:00:00+08:00").ToUnixTimeMilliseconds(),"related project keeps a list's full local half-open range instead of clipping it to now");
        exact.StartAtMs=1;Require(projects.Query.ExactTimeRange.StartAtMs!=1,"drilldown request owns a clone of the range");
        Require(projects.Query.Filters.Single().Field=="confidence" && projects.Query.Filters.Single().Values.Single()=="unknown","unknown project confidence remains a server filter rather than disappearing");
        var rejected=false;try{AnalysisQueries.Sessions(context,new(Sort:"title-contains"),pager);}catch(ArgumentException){rejected=true;}
        Require(rejected,"unsupported title-search contract is not invented in the UI");
        var l=new Localization();l.Set("en-US");
        Require(AnalysisQueries.CacheRate(null,l)=="—" && AnalysisQueries.CacheRate(new(){Value=0,Unit="basis_points"},l)=="0.0%" && AnalysisQueries.CacheRate(new(){Value=10000,Unit="basis_points"},l)=="100.0%","cache-rate presentation distinguishes unknown, zero, and full hit rate without recomputing tokens");
        Require(AnalysisQueries.Tps(null,l)=="—" && AnalysisQueries.Tps(new(){AverageOutputMilliTps=new(){Value=0,Unit="milli_tokens_per_second"}},l)=="0.00 TPS" && AnalysisQueries.Tps(new(){AverageOutputMilliTps=new(){Value=12345,Unit="milli_tokens_per_second"}},l)=="12.35 TPS","TPS formats backend milli-TPS and retains real zero");
        Require(AnalysisQueries.Duration(new(){Value=1250,Unit="milliseconds"},l)=="1.250 s","active duration retains millisecond precision like macOS");
        var state=new AnalysisNavigation();state.Sessions("codex").Selected="old-codex-entity";
        Require(state.Sessions("cursor").Selected is null && !ReferenceEquals(state.Projects("codex"),state.Projects("cursor")),"analysis navigation cannot share selected entities across providers");
        state.Clear();Require(state.Sessions("codex").Selected is null,"account or Home-context reset removes prior analysis selections");
        var sessionsCursor=new CursorPager();var modelsCursor=new CursorPager();sessionsCursor.Next(new(){HasMore=true,NextCursor="sessions-page-two"});
        Require(modelsCursor.Cursor is null && sessionsCursor.Cursor=="sessions-page-two","project session and model contributions have independent cursors");
        var localDetail=AnalysisQueries.ProjectDetail(context,"opaque-dimension",sessionsCursor,modelsCursor);
        var exactDetail=AnalysisQueries.ProjectDetail(context,"opaque-dimension",sessionsCursor,modelsCursor,projects.Query.ExactTimeRange);
        Require(localDetail.Range is not null && localDetail.ExactRange is null && exactDetail.Range is null && exactDetail.ExactRange.Equals(projects.Query.ExactTimeRange) && exactDetail.Provider.Provider==context.Provider,"project detail sends exactly one range representation and preserves the provider and independent cursors");
    }
}
