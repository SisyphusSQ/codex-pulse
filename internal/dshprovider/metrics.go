package dshprovider

import (
	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/cachehitrate"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

func cacheRate(t usagecost.UsageTotals) basequery.NumericValue {
	if t.InputTokens.Value == nil || t.CachedInputTokens.Value == nil {
		return unknown(basequery.NumericBasisPoints, basequery.UnknownUnavailable)
	}
	value := cachehitrate.BasisPoints(*t.InputTokens.Value, *t.CachedInputTokens.Value)
	if value == nil {
		return unknown(basequery.NumericBasisPoints, basequery.UnknownNotApplicable)
	}
	return known(*value, basequery.NumericBasisPoints)
}
func optionalNumber(value *int64, unit basequery.NumericUnit) basequery.NumericValue {
	if value == nil {
		return unknown(unit, basequery.UnknownUnavailable)
	}
	return known(*value, unit)
}
func measureStats(m reportingv1.ThroughputMeasures) *usagecost.ThroughputStats {
	var average *int64
	if m.OutputTokens != nil && m.ActiveDurationMS != nil {
		average = throughput.Measure(*m.OutputTokens, *m.ActiveDurationMS).AverageMilliTPS
	}
	return &usagecost.ThroughputStats{AverageOutputMilliTPS: optionalNumber(average, basequery.NumericMilliTPS), OutputTokens: optionalNumber(m.OutputTokens, basequery.NumericTokens), ActiveDurationMS: optionalNumber(m.ActiveDurationMS, basequery.NumericMilliseconds), IncludedTurns: known(m.IncludedTurns, basequery.NumericCount), ExcludedTurns: known(m.ExcludedTurns, basequery.NumericCount), OpenTurns: known(m.OpenTurns, basequery.NumericCount), UnattributedEvents: known(m.UnattributedEvents, basequery.NumericCount), Status: m.Status, Reason: m.Reason, DurationSource: m.DurationSource, Basis: "closed_turn_lifetime_output"}
}
func sessionThroughput(c *reportingv1.ThroughputCapsule) *usagecost.ThroughputStats {
	if c == nil {
		return nil
	}
	return measureStats(c.Measures)
}
func throughputTurns(c *reportingv1.ThroughputCapsule) []usagecost.ThroughputTurn {
	if c == nil {
		return nil
	}
	items := make([]usagecost.ThroughputTurn, 0, len(c.RecentTurns))
	for _, turn := range c.RecentTurns {
		items = append(items, usagecost.ThroughputTurn{TimelineKey: turn.Key, StartedAt: optionalNumber(turn.StartedAtMS, basequery.NumericMilliseconds), CompletedAt: optionalNumber(turn.EndedAtMS, basequery.NumericMilliseconds), Throughput: measureStats(turn.Measures)})
	}
	return items
}
