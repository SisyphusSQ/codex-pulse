package statistics_vo

type UsageModel struct {
	CacheHitRate *CacheHitRateView `json:"cache_hit_rate"`
	Provider     string            `json:"provider"`
	Model        string            `json:"model"`
	Totals       StatisticsTotals  `json:"totals"`
}
type UsageModelDay struct {
	Provider string           `json:"provider"`
	Model    string           `json:"model"`
	Date     string           `json:"date"`
	Totals   StatisticsTotals `json:"totals"`
}
type UsageResponse struct {
	Cache                          *StatisticsCache   `json:"cache,omitempty"`
	CacheHitRate                   *CacheHitRateView  `json:"cache_hit_rate"`
	Range                          StatisticsRange    `json:"range"`
	Scope                          string             `json:"scope"`
	Totals                         StatisticsTotals   `json:"totals"`
	Coverage                       StatisticsCoverage `json:"coverage"`
	Models                         []UsageModel       `json:"models"`
	ModelDays                      []UsageModelDay    `json:"model_days"`
	Trend                          []StatisticsDay    `json:"trend"`
	Providers                      []StatisticsSlice  `json:"providers"`
	CursorPools                    []StatisticsSlice  `json:"cursor_pools"`
	ModelCostRoundingDeltaMicroUSD *string            `json:"model_cost_rounding_delta_micro_usd"`
	TrendCostRoundingDeltaMicroUSD *string            `json:"trend_cost_rounding_delta_micro_usd"`
}
