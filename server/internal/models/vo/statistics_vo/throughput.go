package statistics_vo

// ThroughputView 的均值由纯 Go 定点计算，覆盖未知不伪造成0。
type ThroughputView struct {
	SourceClientID        *string `json:"source_client_id"`
	AverageOutputMilliTPS *string `json:"average_output_milli_tps"`
	OutputTokens          *string `json:"output_tokens"`
	ActiveDurationMS      *string `json:"active_duration_ms"`
	IncludedTurns         *string `json:"included_turns"`
	ExcludedTurns         *string `json:"excluded_turns"`
	OpenTurns             *string `json:"open_turns"`
	UnattributedEvents    *string `json:"unattributed_events"`
	Status                string  `json:"status"`
	Reason                string  `json:"reason"`
	DurationSource        string  `json:"duration_source"`
	Basis                 string  `json:"basis"`
	AverageUnit           string  `json:"average_unit"`
	DurationUnit          string  `json:"duration_unit"`
	Conflict              bool    `json:"conflict"`
}

type ThroughputTurnView struct {
	Key         string         `json:"key"`
	StartedAtMS *int64         `json:"started_at_ms"`
	EndedAtMS   *int64         `json:"ended_at_ms"`
	Throughput  ThroughputView `json:"throughput"`
}

type ThroughputTurnsView struct {
	Items     []ThroughputTurnView `json:"items"`
	Total     *string              `json:"total"`
	Limit     int                  `json:"limit"`
	Truncated bool                 `json:"truncated"`
}
