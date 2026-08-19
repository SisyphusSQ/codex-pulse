package apisubscriptions

import "errors"

const (
	ServiceDeepSeek   = "deepseek"
	ServiceOpenCodeGo = "opencode_go"

	StateCurrent      = "current"
	StateStale        = "stale"
	StateUnavailable  = "unavailable"
	StateUnconfigured = "unconfigured"

	FailureNetwork   = "network"
	FailureTimeout   = "timeout"
	FailureAuth      = "auth"
	FailureForbidden = "forbidden"
	FailureRateLimit = "rate_limit"
	FailureServer    = "server"
	FailureProtocol  = "protocol"

	WindowFiveHour = "five_hour"
	WindowWeekly   = "weekly"
	WindowMonthly  = "monthly"

	PeriodToday = "today"
	PeriodWeek  = "week"
	PeriodMonth = "month"

	StatusOK          = "ok"
	StatusRateLimited = "rate-limited"
)

var (
	ErrUnconfigured = errors.New("API subscription is not configured")
	ErrProtocol     = errors.New("API subscription response is incompatible")
	ErrAuth         = errors.New("API subscription authentication failed")
	ErrForbidden    = errors.New("API subscription is not available for this account")
	ErrRateLimit    = errors.New("API subscription request was rate limited")
	ErrServer       = errors.New("API subscription service failed")
)

type Balance struct {
	IsAvailable bool              `json:"isAvailable"`
	Balances    []CurrencyBalance `json:"balances"`
}

type CurrencyBalance struct {
	Currency string `json:"currency"`
	Total    string `json:"total"`
	Granted  string `json:"granted"`
	ToppedUp string `json:"toppedUp"`
}

type Quota struct {
	Windows []QuotaWindow `json:"windows"`
}

type QuotaWindow struct {
	Kind             string  `json:"kind"`
	Status           string  `json:"status"`
	UsedPercent      float64 `json:"usedPercent"`
	RemainingPercent float64 `json:"remainingPercent"`
	ResetsAtMS       int64   `json:"resetsAtMs"`
}

type SourceStatus struct {
	State           string  `json:"state"`
	LastSuccessAtMS *int64  `json:"lastSuccessAtMs,omitempty"`
	FailureCode     *string `json:"failureCode,omitempty"`
}

type DeepSeekSnapshot struct {
	Status  SourceStatus    `json:"status"`
	Balance *Balance        `json:"balance,omitempty"`
	Periods []BalancePeriod `json:"periods"`
}

type BalancePeriod struct {
	Kind         string                  `json:"kind"`
	StartsAtMS   int64                   `json:"startsAtMs"`
	BaselineAtMS *int64                  `json:"baselineAtMs,omitempty"`
	Changes      []CurrencyBalanceChange `json:"changes"`
	Series       []CurrencyBalanceSeries `json:"series"`
}

type CurrencyBalanceSeries struct {
	Currency string              `json:"currency"`
	Points   []BalanceTrendPoint `json:"points"`
}

type BalanceTrendPoint struct {
	ObservedAtMS int64  `json:"observedAtMs"`
	Total        string `json:"total"`
	Granted      string `json:"granted"`
	ToppedUp     string `json:"toppedUp"`
}

type CurrencyBalanceChange struct {
	Currency         string `json:"currency"`
	StartingTotal    string `json:"startingTotal"`
	TotalDelta       string `json:"totalDelta"`
	StartingGranted  string `json:"startingGranted"`
	GrantedDelta     string `json:"grantedDelta"`
	StartingToppedUp string `json:"startingToppedUp"`
	ToppedUpDelta    string `json:"toppedUpDelta"`
	TotalRecharged   string `json:"totalRecharged"`
	TotalConsumed    string `json:"totalConsumed"`
}

type OpenCodeGoSnapshot struct {
	Status SourceStatus `json:"status"`
	Quota  *Quota       `json:"quota,omitempty"`
}

type CurrentSnapshot struct {
	EvaluatedAtMS    int64                           `json:"evaluatedAtMs"`
	DeepSeek         DeepSeekSnapshot                `json:"deepSeek"`
	OpenCodeGo       OpenCodeGoSnapshot              `json:"openCodeGo"`
	ActivityCalendar APISubscriptionActivityCalendar `json:"activityCalendar"`
}

type APISubscriptionActivityCalendar struct {
	ReportingTimeZone string                       `json:"reportingTimeZone"`
	Days              []APISubscriptionActivityDay `json:"days"`
}

type APISubscriptionActivityDay struct {
	DateKey    string                           `json:"dateKey"`
	StartsAtMS int64                            `json:"startsAtMs"`
	DeepSeek   []DeepSeekDailyActivity          `json:"deepSeek"`
	OpenCodeGo *OpenCodeGoFiveHourDailyActivity `json:"openCodeGo,omitempty"`
}

type DeepSeekDailyActivity struct {
	Currency       string `json:"currency"`
	TotalRecharged string `json:"totalRecharged"`
	TotalConsumed  string `json:"totalConsumed"`
	SampleCount    int    `json:"sampleCount"`
}

type OpenCodeGoFiveHourDailyActivity struct {
	MaxFiveHourUsedPercent         float64 `json:"maxFiveHourUsedPercent"`
	LatestFiveHourUsedPercent      float64 `json:"latestFiveHourUsedPercent"`
	LatestFiveHourRemainingPercent float64 `json:"latestFiveHourRemainingPercent"`
	SampleCount                    int     `json:"sampleCount"`
}
