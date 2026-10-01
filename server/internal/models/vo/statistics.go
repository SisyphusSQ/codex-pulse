package vo

// StatisticsTotals 的 NULL 表示未知，真实零为字符串0，累加不丢浏览器整数精度。
type StatisticsTotals struct {
	CostBasis              string   `json:"cost_basis"`
	PricingVersions        []string `json:"pricing_versions"`
	CostStatus             string   `json:"cost_status"`
	InputTokens            *string  `json:"input_tokens"`
	CachedTokens           *string  `json:"cached_tokens"`
	CacheWriteTokens       *string  `json:"cache_write_tokens"`
	OutputTokens           *string  `json:"output_tokens"`
	ReasoningTokens        *string  `json:"reasoning_tokens"`
	TotalTokens            *string  `json:"total_tokens"`
	CostMicroUSD           *string  `json:"cost_micro_usd"`
	ReportedChargeMicroUSD *string  `json:"reported_charge_micro_usd"`
	Sessions               int64    `json:"sessions"`
	Invocations            int64    `json:"invocations"`
}
type StatisticsCoverage struct {
	Scope              string `json:"scope"`
	RangeCovered       bool   `json:"range_covered"`
	Stale              bool   `json:"stale"`
	State              string `json:"state"`
	KnownProviders     int    `json:"known_providers"`
	UnpricedFacts      int64  `json:"unpriced_facts"`
	UnknownTokenFacts  int64  `json:"unknown_token_facts"`
	UntimedFacts       int64  `json:"untimed_facts"`
	PartialSessions    int64  `json:"partial_sessions"`
	ConflictedSessions int64  `json:"conflicted_sessions"`
	CollectedAtMS      *int64 `json:"collected_at_ms"`
}
type StatisticsRange struct {
	StartAtMS int64  `json:"start_at_ms"`
	EndAtMS   int64  `json:"end_at_ms"`
	TimeZone  string `json:"time_zone"`
}
type StatisticsSlice struct {
	Key    string           `json:"key"`
	Name   string           `json:"name"`
	Totals StatisticsTotals `json:"totals"`
}
type StatisticsDay struct {
	Date      string           `json:"date"`
	StartAtMS int64            `json:"start_at_ms"`
	Totals    StatisticsTotals `json:"totals"`
}
type StatisticsHour struct {
	Weekday  int     `json:"weekday"`
	Hour     int     `json:"hour"`
	Tokens   *string `json:"tokens"`
	Sessions int64   `json:"sessions"`
}
type StatisticsSummary struct {
	CostBasis                      string             `json:"cost_basis"`
	TrendCostRoundingDeltaMicroUSD *string            `json:"trend_cost_rounding_delta_micro_usd"`
	Tools                          []StatisticsSlice  `json:"tools"`
	Skills                         []StatisticsSlice  `json:"skills"`
	Range                          StatisticsRange    `json:"range"`
	Scope                          string             `json:"scope"`
	Totals                         StatisticsTotals   `json:"totals"`
	Coverage                       StatisticsCoverage `json:"coverage"`
	Providers                      []StatisticsSlice  `json:"providers"`
	Models                         []StatisticsSlice  `json:"models"`
	Devices                        []StatisticsSlice  `json:"devices"`
	Trend                          []StatisticsDay    `json:"trend"`
	Heatmap                        []StatisticsDay    `json:"heatmap"`
	HeatmapRange                   StatisticsRange    `json:"heatmap_range"`
	HeatmapCoverage                StatisticsCoverage `json:"heatmap_coverage"`
	WeekdayHours                   []StatisticsHour   `json:"weekday_hours"`
}
type StatisticsPage struct {
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}
type StatisticsSession struct {
	ID             string             `json:"id"`
	Provider       string             `json:"provider"`
	SessionID      *string            `json:"session_id"`
	Title          string             `json:"title"`
	SessionKind    string             `json:"session_kind"`
	ProjectID      string             `json:"project_id"`
	ProjectGroupID string             `json:"project_group_id"`
	ProjectName    string             `json:"project_name"`
	CreatedAtMS    *int64             `json:"created_at_ms"`
	LastActiveAtMS *int64             `json:"last_active_at_ms"`
	CollectedAtMS  int64              `json:"collected_at_ms"`
	Complete       bool               `json:"complete"`
	Conflict       bool               `json:"conflict"`
	Sources        []StatisticsSource `json:"sources"`
	Totals         StatisticsTotals   `json:"totals"`
}
type StatisticsSource struct {
	ID            string `json:"id"`
	ClientID      string `json:"client_id"`
	ClientName    string `json:"client_name"`
	CollectedAtMS int64  `json:"collected_at_ms"`
	Revision      int64  `json:"revision"`
	SourceKind    string `json:"source_kind"`
	Complete      bool   `json:"complete"`
	Deleted       bool   `json:"deleted"`
}
type StatisticsSessions struct {
	Range    StatisticsRange     `json:"range"`
	Scope    string              `json:"scope"`
	Page     StatisticsPage      `json:"page"`
	Items    []StatisticsSession `json:"items"`
	Totals   StatisticsTotals    `json:"totals"`
	Coverage StatisticsCoverage  `json:"coverage"`
}
type StatisticsSessionDetail struct {
	Session  StatisticsSession  `json:"session"`
	Range    StatisticsRange    `json:"range"`
	Trend    []StatisticsDay    `json:"trend"`
	Tools    []StatisticsSlice  `json:"tools"`
	Skills   []StatisticsSlice  `json:"skills"`
	Coverage StatisticsCoverage `json:"coverage"`
}
type StatisticsProject struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Members        []string         `json:"members"`
	Totals         StatisticsTotals `json:"totals"`
	LastActiveAtMS *int64           `json:"last_active_at_ms"`
	Conflict       bool             `json:"conflict"`
}
type StatisticsProjects struct {
	Range    StatisticsRange     `json:"range"`
	Scope    string              `json:"scope"`
	Page     StatisticsPage      `json:"page"`
	Items    []StatisticsProject `json:"items"`
	Totals   StatisticsTotals    `json:"totals"`
	Coverage StatisticsCoverage  `json:"coverage"`
}
type StatisticsProjectDetail struct {
	Project  StatisticsProject  `json:"project"`
	Sessions StatisticsSessions `json:"sessions"`
	Trend    []StatisticsDay    `json:"trend"`
}

// Device 状态来自最后采集时间，收到批次不能冒充采集完整或仍然在线。
type StatisticsDevice struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	RevokedAtMS      *int64                     `json:"revoked_at_ms"`
	LastReceivedAtMS *int64                     `json:"last_received_at_ms"`
	Providers        []StatisticsDeviceProvider `json:"providers"`
}
type StatisticsDeviceProvider struct {
	Provider        string `json:"provider"`
	Version         string `json:"version"`
	CollectedAtMS   *int64 `json:"collected_at_ms"`
	CoverageStartMS *int64 `json:"coverage_start_ms"`
	CoverageEndMS   *int64 `json:"coverage_end_ms"`
	PendingBatches  int64  `json:"pending_batches"`
	Status          string `json:"status"`
	ReceivedAtMS    int64  `json:"received_at_ms"`
	Stale           bool   `json:"stale"`
}
