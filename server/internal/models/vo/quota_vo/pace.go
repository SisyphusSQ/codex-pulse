package quota_vo

type PacePoint struct {
	ObservedAtMS     int64   `json:"observed_at_ms"`
	ElapsedPercent   float64 `json:"elapsed_percent"`
	UsedPercent      float64 `json:"used_percent"`
	RemainingPercent float64 `json:"remaining_percent"`
	LinkedHistory    bool    `json:"linked_history"`
}
type PaceCycle struct {
	ID              string      `json:"id"`
	WindowStartAtMS int64       `json:"window_start_at_ms"`
	ResetsAtMS      int64       `json:"resets_at_ms"`
	Complete        bool        `json:"complete"`
	Points          []PacePoint `json:"points"`
}
type HistoryBandPoint struct {
	ElapsedPercent   float64 `json:"elapsed_percent"`
	MedianRemaining  float64 `json:"median_remaining"`
	MinimumRemaining float64 `json:"minimum_remaining"`
	MaximumRemaining float64 `json:"maximum_remaining"`
	CycleCount       int64   `json:"cycle_count"`
}
type Forecast struct {
	State             string  `json:"state"`
	Method            string  `json:"method"`
	ExhaustAtMS       *int64  `json:"exhaust_at_ms"`
	LeadBeforeResetMS *int64  `json:"lead_before_reset_ms"`
	EvidenceCount     int64   `json:"evidence_count"`
	EvidenceSpanMS    int64   `json:"evidence_span_ms"`
	UnknownReason     *string `json:"unknown_reason"`
}
type PaceWindow struct {
	Key                             string             `json:"key"`
	Provider                        string             `json:"provider"`
	AccountKey                      *string            `json:"account_key"`
	IdentityState                   string             `json:"identity_state"`
	LimitID                         string             `json:"limit_id"`
	WindowKind                      string             `json:"window_kind"`
	WindowMinutes                   *int64             `json:"window_minutes"`
	Current                         Current            `json:"current"`
	ElapsedPercent                  *float64           `json:"elapsed_percent"`
	PaceDeltaPP                     *float64           `json:"pace_delta_pp"`
	Forecast                        Forecast           `json:"forecast"`
	CurrentPoints                   []PacePoint        `json:"current_points"`
	PreviousCycle                   *PaceCycle         `json:"previous_cycle"`
	HistoricalCycles                []PaceCycle        `json:"historical_cycles"`
	HistoryBand                     []HistoryBandPoint `json:"history_band"`
	HistoryCycleCount               int64              `json:"history_cycle_count"`
	PreviousRemainingAtElapsed      *float64           `json:"previous_remaining_at_elapsed"`
	HistoryMedianRemainingAtElapsed *float64           `json:"history_median_remaining_at_elapsed"`
	UnknownReason                   *string            `json:"unknown_reason"`
	Coverage                        string             `json:"coverage"`
}
type PaceResponse struct {
	EvaluatedAtMS int64        `json:"evaluated_at_ms"`
	RuleVersion   string       `json:"rule_version"`
	Accounts      []Account    `json:"accounts"`
	Windows       []PaceWindow `json:"windows"`
	Coverage      string       `json:"coverage"`
}
