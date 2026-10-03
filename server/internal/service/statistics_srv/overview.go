package statistics_srv

import (
	"slices"
	"strconv"
	"time"

	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
)

func statisticsHour(at int64, zone *time.Location) time.Time {
	local := time.UnixMilli(at).In(zone)
	// 按真实偏移保留夏令时重复小时；不能按 UTC 截断非整小时的时区。
	return local.Add(-time.Duration(local.Minute())*time.Minute - time.Duration(local.Second())*time.Second - time.Duration(local.Nanosecond()))
}
func statisticsHourKey(at int64, zone *time.Location) string {
	return strconv.FormatInt(statisticsHour(at, zone).UnixMilli(), 10)
}
func (o *statisticsRead) activityGranularity() string {
	if statisticsDay(o.q.StartAtMS, o.q.Location).Equal(statisticsDay(o.q.EndAtMS-1, o.q.Location)) {
		return "hour"
	}
	return "day"
}
func (o *statisticsRead) activityTimeline() []statistics_vo.StatisticsActivityBucket {
	out := []statistics_vo.StatisticsActivityBucket{}
	hourly := o.activityGranularity() == "hour"
	at := statisticsDay(o.q.StartAtMS, o.q.Location)
	if hourly {
		at = statisticsHour(o.q.StartAtMS, o.q.Location)
	}
	for at.UnixMilli() < o.q.EndAtMS {
		next := at.AddDate(0, 0, 1)
		g := o.days[at.Format(time.DateOnly)]
		if hourly {
			next = at.Add(time.Hour)
			g = o.timeline[strconv.FormatInt(at.UnixMilli(), 10)]
		}
		b := statistics_vo.StatisticsActivityBucket{StartAtMS: max(at.UnixMilli(), o.q.StartAtMS), EndAtMS: min(next.UnixMilli(), o.q.EndAtMS)}
		if g != nil {
			t, _ := g.finish(false)
			b.Tokens = t.TotalTokens
			b.Sessions = new(strconv.FormatInt(t.Sessions, 10))
		}
		out = append(out, b)
		at = next
	}
	return out
}
func (o *statisticsRead) topSessions() []statistics_vo.StatisticsSession {
	out := []statistics_vo.StatisticsSession{}
	for id, g := range o.sessions {
		m := o.metadata[id]
		if m.SessionKind == "unassigned_usage" || !g.tokens.seen || g.tokens.value.Sign() <= 0 {
			continue
		}
		out = append(out, o.sessionView(m))
	}
	q := o.q
	q.Sort = "tokens"
	q.Direction = "desc"
	slices.SortFunc(out, func(a, b statistics_vo.StatisticsSession) int { return compareSession(a, b, q) })
	return out[:min(5, len(out))]
}
