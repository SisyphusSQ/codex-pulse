using System.Globalization;
using System.Numerics;
using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public sealed record ChartSegment(string Label, long Value, string Key);
public sealed record ChartDatum(string Label, long? Value, IReadOnlyList<ChartSegment> Segments, string? Detail = null, string? AxisLabel = null);
public sealed record CalendarDay(DateOnly Date, long? Tokens, long? Turns = null);
public sealed record CalendarStatistics(long? Total, long? Peak, int ActiveDays, int CurrentStreak, int LongestStreak, bool Complete);
public sealed record OverviewRange(UTCTimeRange Exact, string Granularity, bool Fallback);

// Presentation transforms only: values remain generated Helper facts. Integer sums
// below validate chart completeness; they never replace Helper business totals.
public static class DashboardPresentation
{
    public static long? Value(NumericValue? value) => value is { HasValue: true, Value: >= 0 } ? value.Value : null;
    public static string Name(AttributionValue? value,string fallback)=>!string.IsNullOrWhiteSpace(value?.DisplayName)?value.DisplayName:fallback;
    public static bool Complete(string? state) => state is "complete" or "empty" or "ok";
    public static string BucketLabel(string key) => key.Length >= 16 && key[10] == 'T' ? key.Substring(11,5) : key.Length == 10 && key[4]=='-' && key[7]=='-' ? key[5..] : key;
    public static LoadKind Kind(ResponseMeta? meta, bool empty = false) => meta?.Status switch
    { "unavailable" => LoadKind.Unavailable, "partial" => LoadKind.Partial, _ => empty ? LoadKind.Empty : LoadKind.Ready };
    public static LocalDateRange LocalRange(QueryContext context) => new()
    { StartDate = context.Start.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture), EndDateExclusive = context.EndExclusive.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture), TimeZone = context.TimeZone };
    public static LocalDateRange AnnualRange(string timeZone, DateOnly today) => new()
    { StartDate = today.AddDays(-364).ToString("yyyy-MM-dd", CultureInfo.InvariantCulture), EndDateExclusive = today.AddDays(1).ToString("yyyy-MM-dd", CultureInfo.InvariantCulture), TimeZone = timeZone };

    public static OverviewRange ResolveRange(QueryContext context, CurrentQuota? quota, DateTimeOffset now)
    {
        var period = context.Preset is "quota_week" or "quota_month";
        if (period && quota is not null)
        {
            var windows = quota.Windows.Where(window => window.HasWindowMinutes && window.WindowMinutes > 0 && window.HasResetsAtMs &&
                (context.Preset == "quota_week"
                    ? window.WindowMinutes == 10080 || window.LimitId == "grok.included_credits" && window.WindowMinutes is >= 8640 and <= 11520
                    : window.LimitId is "cursor.models" or "cursor.other_models" or "grok.included_credits"))
                .Where(window => window.LimitId != "cursor.grok_bot")
                .OrderBy(window => window.LimitId is "codex" or "cursor.models" ? 0 : window.LimitId == "grok.included_credits" ? 1 : 2);
            foreach (var window in windows)
            {
                var start = (BigInteger)window.ResetsAtMs - (BigInteger)window.WindowMinutes * 60000;
                if (start < 0 || start > long.MaxValue || quota.EvaluatedAtMs <= start || quota.EvaluatedAtMs > window.ResetsAtMs) continue;
                return new(new() { StartAtMs = (long)start, EndAtMs = quota.EvaluatedAtMs, TimeZone = context.TimeZone }, "day", false);
            }
        }
        var zone = TimeZoneInfo.FindSystemTimeZoneById(context.TimeZone);
        var startDate = context.Start.ToDateTime(TimeOnly.MinValue, DateTimeKind.Unspecified);
        var endDate = context.EndExclusive.ToDateTime(TimeOnly.MinValue, DateTimeKind.Unspecified);
        var startMs = new DateTimeOffset(TimeZoneInfo.ConvertTimeToUtc(startDate, zone)).ToUnixTimeMilliseconds();
        var endMs = new DateTimeOffset(TimeZoneInfo.ConvertTimeToUtc(endDate, zone)).ToUnixTimeMilliseconds();
        if (context.Preset != "custom" && now.ToUnixTimeMilliseconds() > startMs) endMs = Math.Min(endMs, now.ToUnixTimeMilliseconds());
        return new(new() { StartAtMs = startMs, EndAtMs = endMs, TimeZone = context.TimeZone }, context.Preset == "today" ? "hour" : "day", period);
    }

    public static IReadOnlyList<ChartSegment> Reconciled(long? total, IEnumerable<ChartSegment?> source)
    {
        if (total is null) return [];
        var items = source.ToArray();
        if (items.Any(item => item is null || item.Value < 0)) return [];
        return items.Aggregate(BigInteger.Zero, (sum, item) => sum + item!.Value) == total ? items.Select(item => item!).ToArray() : [];
    }

    public static IReadOnlyList<ChartDatum> UsageTrend(UsageCostResponse response)
    {
        return response.Trend.Select(point =>
        {
            var total = Value(point.Totals?.TotalTokens);
            var segments = Reconciled(total, response.Models.Select(model =>
            {
                var matches = model.Trend.Where(bucket => bucket.Key == point.Key && bucket.StartAtMs?.Value == point.StartAtMs?.Value && bucket.EndAtMs?.Value == point.EndAtMs?.Value).ToArray();
                var value = matches.Length == 1 ? Value(matches[0].Totals?.TotalTokens) : null;
                return value is { } known ? new ChartSegment(Name(model.Model,"—"), known, model.DimensionKey) : null;
            }));
            return new ChartDatum(point.Key, total, segments);
        }).ToArray();
    }

    public static IReadOnlyList<CalendarDay> Calendar(IEnumerable<(NumericValue? Start, NumericValue? Tokens, NumericValue? Turns)> points,
        DateOnly today, string timeZone, bool complete)
    {
        var zone = TimeZoneInfo.FindSystemTimeZoneById(timeZone);
        var buckets = new Dictionary<DateOnly, (long? Tokens, long? Turns)>();
        foreach (var point in points)
        {
            if (Value(point.Start) is not { } start || start > 253402300799999) continue;
            var day = DateOnly.FromDateTime(TimeZoneInfo.ConvertTime(DateTimeOffset.FromUnixTimeMilliseconds(start), zone).DateTime);
            buckets[day] = buckets.ContainsKey(day) ? (null, null) : (Value(point.Tokens), Value(point.Turns));
        }
        return Enumerable.Range(0,365).Select(index =>
        {
            var date = today.AddDays(index - 364);
            var known = buckets.TryGetValue(date, out var bucket);
            return new CalendarDay(date, known ? bucket.Tokens : complete ? 0 : null, known ? bucket.Turns : complete ? 0 : null);
        }).ToArray();
    }

    public static CalendarStatistics Statistics(IReadOnlyList<CalendarDay> days, long? expectedTotal = null)
    {
        var complete = days.Count == 365 && days.All(day => day.Tokens is not null);
        if(!days.Any(day=>day.Tokens is not null)) return new(null,null,0,0,0,false);
        var sum = days.Aggregate(BigInteger.Zero, (total, day) => total + (day.Tokens ?? 0));
        var reconciles = expectedTotal is null || sum == expectedTotal;
        if (!reconciles) return new(null, null, 0, 0, 0, false);
        var streak = 0; var longest = 0;
        foreach (var day in days) { streak = day.Tokens > 0 ? streak + 1 : 0; longest = Math.Max(longest, streak); }
        var last = days.Count - 1;
        if (last >= 0 && days[last].Tokens == 0) last--;
        var current = 0;
        while (last >= 0 && days[last--].Tokens > 0) current++;
        return new(sum <= long.MaxValue ? (long)sum : null, days.Where(day => day.Tokens is not null).Select(day => day.Tokens).DefaultIfEmpty(null).Max(),
            days.Count(day => day.Tokens > 0), current, longest, complete);
    }

    public static bool ActivityReconciles(UsageCostResponse response)
    {
        var total = Value(response.Totals?.TotalTokens);
        if (total is null || response.ActivityDistribution is null) return false;
        bool Matches(IEnumerable<NumericValue?> points)
        {
            var values = points.Select(Value).ToArray();
            return values.All(value => value is not null) && values.Aggregate(BigInteger.Zero, (sum,value) => sum + value!.Value) == total;
        }
        return Matches(response.ActivityDistribution.Timeline.Select(point => point.Metrics?.TotalTokens)) &&
            Matches(response.ActivityDistribution.WeekdayHours.Select(point => point.Metrics?.TotalTokens));
    }
}
