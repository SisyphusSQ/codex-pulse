package usagecost

import (
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

// ThroughputStats reports output efficiency independently of pricing coverage.
type ThroughputStats struct {
	AverageOutputMilliTPS basequery.NumericValue `json:"averageOutputMilliTps"`
	OutputTokens          basequery.NumericValue `json:"outputTokens"`
	ActiveDurationMS      basequery.NumericValue `json:"activeDurationMs"`
	IncludedTurns         basequery.NumericValue `json:"includedTurns"`
	ExcludedTurns         basequery.NumericValue `json:"excludedTurns"`
	OpenTurns             basequery.NumericValue `json:"openTurns"`
	UnattributedEvents    basequery.NumericValue `json:"unattributedEvents"`
	Status                string                 `json:"status"`
	Reason                string                 `json:"reason"`
	DurationSource        string                 `json:"durationSource"`
	Basis                 string                 `json:"basis"`
}

// ThroughputTurn is a bounded content-free drilldown, separate from cost pages.
type ThroughputTurn struct {
	TimelineKey string                 `json:"timelineKey"`
	StartedAt   basequery.NumericValue `json:"startedAtMs"`
	CompletedAt basequery.NumericValue `json:"completedAtMs"`
	Throughput  *ThroughputStats       `json:"throughput"`
}

func mapThroughput(stats *throughput.Stats) (*ThroughputStats, error) {
	if stats == nil {
		return nil, nil
	}
	value := &ThroughputStats{Status: stats.Status, Reason: stats.Reason, DurationSource: stats.DurationSource, Basis: "closed_turn_lifetime_output"}
	for _, field := range []struct {
		target *basequery.NumericValue
		source *int64
		unit   basequery.NumericUnit
	}{
		{&value.AverageOutputMilliTPS, stats.AverageMilliTPS, basequery.NumericMilliTPS},
		{&value.OutputTokens, stats.OutputTokens, basequery.NumericTokens},
		{&value.ActiveDurationMS, stats.ActiveMS, basequery.NumericMilliseconds},
		{&value.IncludedTurns, &stats.IncludedTurns, basequery.NumericCount},
		{&value.ExcludedTurns, &stats.ExcludedTurns, basequery.NumericCount},
		{&value.OpenTurns, &stats.OpenTurns, basequery.NumericCount},
		{&value.UnattributedEvents, &stats.UnattributedEvents, basequery.NumericCount},
	} {
		var err error
		if field.source == nil {
			*field.target, err = basequery.UnknownNumeric(field.unit, basequery.UnknownNotComputed)
		} else {
			*field.target, err = basequery.KnownNumeric(*field.source, field.unit)
		}
		if err != nil {
			return nil, err
		}
	}
	return value, nil
}

func mapThroughputTurns(records []throughput.Turn) ([]ThroughputTurn, error) {
	items := make([]ThroughputTurn, 0, len(records))
	for _, record := range records {
		stats, err := mapThroughput(&record.Stats)
		if err != nil {
			return nil, err
		}
		start, err := throughputTime(record.StartedAtMS)
		if err != nil {
			return nil, err
		}
		end, err := throughputTime(record.EndedAtMS)
		if err != nil {
			return nil, err
		}
		items = append(items, ThroughputTurn{TimelineKey: sessionTurnTimelineKey(record.ID), StartedAt: start, CompletedAt: end, Throughput: stats})
	}
	return items, nil
}

func throughputTime(value *int64) (basequery.NumericValue, error) {
	if value == nil {
		return basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNotComputed)
	}
	return basequery.KnownNumeric(*value, basequery.NumericMilliseconds)
}
