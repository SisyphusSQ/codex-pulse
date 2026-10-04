package statistics_vo

type TotalsStatistics struct {
	Cache    *StatisticsCache   `json:"cache,omitempty"`
	Range    StatisticsRange    `json:"range"`
	Totals   StatisticsTotals   `json:"totals"`
	Coverage StatisticsCoverage `json:"coverage"`
}

type AnnualStatistics struct {
	Cache           *StatisticsCache   `json:"cache,omitempty"`
	Heatmap         []StatisticsDay    `json:"heatmap"`
	HeatmapRange    StatisticsRange    `json:"heatmap_range"`
	HeatmapCoverage StatisticsCoverage `json:"heatmap_coverage"`
	HeatmapTotals   StatisticsTotals   `json:"heatmap_totals"`
	HeatmapActivity StatisticsActivity `json:"heatmap_activity"`
}
type ActivityStatistics struct {
	Cache               *StatisticsCache           `json:"cache,omitempty"`
	Range               StatisticsRange            `json:"range"`
	Coverage            StatisticsCoverage         `json:"coverage"`
	ActivityGranularity string                     `json:"activity_granularity"`
	ActivityTimeline    []StatisticsActivityBucket `json:"activity_timeline"`
	WeekdayHours        []StatisticsHour           `json:"weekday_hours"`
}
type TopStatistics struct {
	Cache       *StatisticsCache    `json:"cache,omitempty"`
	Range       StatisticsRange     `json:"range"`
	Coverage    StatisticsCoverage  `json:"coverage"`
	TopSessions []StatisticsSession `json:"top_sessions"`
}
type BreakdownStatistics struct {
	Cache     *StatisticsCache   `json:"cache,omitempty"`
	Range     StatisticsRange    `json:"range"`
	Coverage  StatisticsCoverage `json:"coverage"`
	Providers []StatisticsSlice  `json:"providers"`
	Models    []StatisticsSlice  `json:"models"`
}
