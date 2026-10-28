using Codexpulse.Core.V1;
using CodexPulse.AppSupport;

internal static class DashboardTests
{
    private static void Require(bool condition,string name) {if(!condition)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    private static NumericValue Number(long value)=>new(){Value=value};
    public static void Run()
    {
        var context=new QueryContext("codex",new(2026,10,23),new(2026,10,30),"Asia/Hong_Kong",1,"quota_week");
        var now=DateTimeOffset.Parse("2026-10-29T08:00:00Z");var reset=now.AddDays(2).ToUnixTimeMilliseconds();
        var quota=new CurrentQuota {EvaluatedAtMs=now.ToUnixTimeMilliseconds()};
        quota.Windows.Add(new CurrentWindow{LimitId="model-only",WindowMinutes=10080,ResetsAtMs=reset-1000});
        quota.Windows.Add(new CurrentWindow{LimitId="codex",WindowMinutes=10080,ResetsAtMs=reset});
        var period=DashboardPresentation.ResolveRange(context,quota,now);
        Require(period.Exact.StartAtMs==reset-604800000 && period.Exact.EndAtMs==now.ToUnixTimeMilliseconds() && !period.Fallback,"overview uses the general quota's exact UTC period rather than recent seven days");
        var fallback=DashboardPresentation.ResolveRange(context,new CurrentQuota(),now);
        Require(fallback.Fallback && fallback.Exact.StartAtMs==DateTimeOffset.Parse("2026-10-23T00:00:00+08:00").ToUnixTimeMilliseconds(),"missing quota period has explicit local-calendar fallback");
        var expired=quota.Clone();expired.Windows.Clear();expired.Windows.Add(new CurrentWindow{LimitId="codex",WindowMinutes=long.MaxValue,ResetsAtMs=1});
        Require(DashboardPresentation.ResolveRange(context,expired,now).Fallback,"overflowing quota duration cannot create a fabricated period");
        var dstContext=context with{TimeZone="America/New_York",Start=new(2026,11,1),EndExclusive=new(2026,11,2),Preset="custom"};
        var dst=DashboardPresentation.ResolveRange(dstContext,null,DateTimeOffset.Parse("2026-11-03T00:00:00Z"));
        Require(dst.Exact.EndAtMs-dst.Exact.StartAtMs==25*3600000L,"custom local dates preserve the 25-hour DST day");
        var segments=DashboardPresentation.Reconciled(long.MaxValue,[new("a",long.MaxValue-1,"a"),new("b",1,"b")]);
        Require(segments.Count==2 && DashboardPresentation.Reconciled(long.MaxValue,[new("a",long.MaxValue,"a"),new("b",long.MaxValue,"b")]).Count==0,"chart reconciliation retains int64 precision and rejects overflowed sums");
        Require(DashboardPresentation.Reconciled(0,[null]).Count==0,"unknown model share is never converted to a real zero");
        var usage=new UsageCostResponse();
        usage.Trend.Add(new TrendPoint{Key="2026-10-29",StartAtMs=Number(1),EndAtMs=Number(2),Totals=new(){TotalTokens=Number(100)}});
        var model=new UsageModelItem{DimensionKey="a",Model=new(){DisplayName="A very long synthetic model display name / 中文模型"}};
        model.Trend.Add(new TrendPoint{Key="2026-10-29",StartAtMs=Number(1),EndAtMs=Number(2),Totals=new(){TotalTokens=Number(99)}});usage.Models.Add(model);
        Require(DashboardPresentation.UsageTrend(usage)[0].Segments.Count==0 && DashboardPresentation.UsageTrend(usage)[0].Value==100,"unreconciled model trend displays authoritative total without proportional guesses");
        model.Trend[0].Totals.TotalTokens=Number(100);
        Require(DashboardPresentation.UsageTrend(usage)[0].Segments.Count==1,"matching model buckets can render an exact stacked trend");
        var today=new DateOnly(2026,10,29);var at=DateTimeOffset.Parse("2026-10-28T00:00:00+08:00").ToUnixTimeMilliseconds();
        (NumericValue?,NumericValue?,NumericValue?)[] points=[(Number(at),Number(12),Number(1))];
        var complete=DashboardPresentation.Calendar(points,today,"Asia/Hong_Kong",true);
        var partial=DashboardPresentation.Calendar(points,today,"Asia/Hong_Kong",false);
        Require(complete.Count==365 && complete[^1].Tokens==0 && partial[^1].Tokens is null,"only complete annual coverage turns missing dates into true zero");
        var stats=DashboardPresentation.Statistics(complete,12);
        Require(stats.Total==12 && stats.CurrentStreak==1 && stats.ActiveDays==1 && stats.Complete,"annual streak can continue from yesterday when today has no activity");
        Require(DashboardPresentation.Statistics(complete,13).Total is null,"annual totals mismatch does not produce plausible-looking statistics");
        Require(DashboardPresentation.Statistics(partial,12) is{Total:12,Complete:false},"reconciled local subset is explicitly partial rather than globally complete");
        Require(DashboardPresentation.Statistics(DashboardPresentation.Calendar([],today,"Asia/Hong_Kong",false)).Total is null,"an entirely unknown annual window cannot display zero activity statistics");
        var workspace=new Workspace();workspace.ApplyProviders([new(){Provider="codex",EffectiveState=ProviderEffectiveState.Enabled}]);workspace.Navigate(Feature.Overview);
        Require(workspace.RangePreset=="quota_week","Codex overview honors the quota-week default");
        workspace.SetPreset("today");workspace.Navigate(Feature.Summary);Require(workspace.RangePreset=="seven_days","summary range is independent of overview");
        workspace.Navigate(Feature.Overview);Require(workspace.RangePreset=="today","overview range restores independently");
        var annualA=DashboardPresentation.AnnualRange(context.TimeZone,today);workspace.SetPreset("thirty_days");
        Require(annualA.Equals(DashboardPresentation.AnnualRange(context.TimeZone,today)),"annual request remains independent of current range selection");
        var activity=new UsageCostResponse{Totals=new(){TotalTokens=Number(100)},ActivityDistribution=new()};
        activity.ActivityDistribution.Timeline.Add(new ActivityTimelinePoint{Metrics=new(){TotalTokens=Number(100)}});activity.ActivityDistribution.WeekdayHours.Add(new ActivityWeekdayHourPoint{Weekday=1,Hour=1,Metrics=new(){TotalTokens=Number(99)}});
        Require(!DashboardPresentation.ActivityReconciles(activity),"weekday-hour gaps remain unknown if either distribution does not reconcile");
        activity.ActivityDistribution.WeekdayHours[0].Metrics.TotalTokens=Number(100);
        Require(DashboardPresentation.ActivityReconciles(activity),"both activity views must reconcile before zero-filling sparse cells");
        Require(DashboardPresentation.BucketLabel("2026-10-29T01:00:00+08:00")=="01:00" && DashboardPresentation.BucketLabel("2026-10-29")=="10-29","hour-axis labels use bucket time instead of its UTC offset suffix");
    }
}
