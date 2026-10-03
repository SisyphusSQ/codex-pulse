package quota_vo

type Account struct {
	Key           string  `json:"key"`
	Provider      string  `json:"provider"`
	RawID         string  `json:"raw_id"`
	Email         *string `json:"email"`
	Plan          *string `json:"plan"`
	CollectedAtMS int64   `json:"collected_at_ms"`
}
type Current struct {
	UsedPercent           *float64 `json:"used_percent"`
	RemainingPercent      *float64 `json:"remaining_percent"`
	WindowStartAtMS       *int64   `json:"window_start_at_ms"`
	ResetsAtMS            *int64   `json:"resets_at_ms"`
	ResetRemainingMS      *int64   `json:"reset_remaining_ms"`
	ObservedAtMS          *int64   `json:"observed_at_ms"`
	Freshness             string   `json:"freshness"`
	Conflict              bool     `json:"conflict"`
	Reason                string   `json:"reason"`
	SelectedObservationID *string  `json:"selected_observation_id"`
	SelectedClientID      *string  `json:"selected_client_id"`
	Source                *string  `json:"source"`
}
type Observation struct {
	ID                 string   `json:"id"`
	ClientID           string   `json:"client_id"`
	ClientName         string   `json:"client_name"`
	ObservedAtMS       int64    `json:"observed_at_ms"`
	ReceivedAtMS       int64    `json:"received_at_ms"`
	UsedPercent        *float64 `json:"used_percent"`
	ResetsAtMS         *int64   `json:"resets_at_ms"`
	WindowStartAtMS    *int64   `json:"window_start_at_ms"`
	Source             string   `json:"source"`
	Validity           string   `json:"validity"`
	HistoryOrigin      string   `json:"history_origin"`
	Disposition        string   `json:"disposition"`
	Reason             *string  `json:"reason"`
	CycleID            *string  `json:"cycle_id"`
	CanonicalResetAtMS *int64   `json:"canonical_reset_at_ms"`
}
type Cycle struct {
	ID             string   `json:"id"`
	StartAtMS      int64    `json:"start_at_ms"`
	ResetsAtMS     int64    `json:"resets_at_ms"`
	ObservationIDs []string `json:"observation_ids"`
	LinkedHistory  bool     `json:"linked_history"`
}
type Window struct {
	Key           string        `json:"key"`
	Provider      string        `json:"provider"`
	AccountKey    *string       `json:"account_key"`
	IdentityState string        `json:"identity_state"`
	LimitID       string        `json:"limit_id"`
	WindowKind    string        `json:"window_kind"`
	WindowMinutes *int64        `json:"window_minutes"`
	Current       Current       `json:"current"`
	Cycles        []Cycle       `json:"cycles"`
	Observations  []Observation `json:"observations"`
	Coverage      string        `json:"coverage"`
}
type CreditExpiry struct {
	ExpiresAtMS *int64 `json:"expires_at_ms"`
	Count       int64  `json:"count,string"`
}
type Credits struct {
	Key                string         `json:"key"`
	Provider           string         `json:"provider"`
	AccountKey         *string        `json:"account_key"`
	ClientID           string         `json:"client_id"`
	ObservedAtMS       int64          `json:"observed_at_ms"`
	ObservedInventory  *int64         `json:"observed_inventory,string"`
	AvailableInventory *int64         `json:"available_inventory,string"`
	DetailsStatus      string         `json:"details_status"`
	Freshness          string         `json:"freshness"`
	Conflict           bool           `json:"conflict"`
	NextResetAtMS      *int64         `json:"next_reset_at_ms"`
	NextExpiresAtMS    *int64         `json:"next_expires_at_ms"`
	ExpirySchedule     []CreditExpiry `json:"expiry_schedule"`
}
type Response struct {
	EvaluatedAtMS int64     `json:"evaluated_at_ms"`
	RuleVersion   string    `json:"rule_version"`
	Accounts      []Account `json:"accounts"`
	Windows       []Window  `json:"windows"`
	Credits       []Credits `json:"credits"`
	Coverage      string    `json:"coverage"`
}
