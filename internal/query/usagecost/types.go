package usagecost

import (
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

const ContractVersion = "usage-cost-v1"

var ErrInvalidService = errors.New("usage cost query service is invalid")

type TrendGranularity string

const (
	TrendDay   TrendGranularity = "day"
	TrendWeek  TrendGranularity = "week"
	TrendMonth TrendGranularity = "month"
)

type DegradedReason string

const DegradedRollupMissing DegradedReason = "rollup_missing"

const DegradedRollupAmbiguous DegradedReason = "rollup_ambiguous"

type UsageCostRequest struct {
	Range       basequery.LocalDateRange `json:"range"`
	Granularity TrendGranularity         `json:"granularity"`
}

type UsageTotals struct {
	TurnCount          basequery.NumericValue `json:"turnCount"`
	InputTokens        basequery.NumericValue `json:"inputTokens"`
	CachedInputTokens  basequery.NumericValue `json:"cachedInputTokens"`
	OutputTokens       basequery.NumericValue `json:"outputTokens"`
	ReasoningTokens    basequery.NumericValue `json:"reasoningTokens"`
	TotalTokens        basequery.NumericValue `json:"totalTokens"`
	EstimatedUSDMicros basequery.NumericValue `json:"estimatedUsdMicros"`
	PricedTurnCount    basequery.NumericValue `json:"pricedTurnCount"`
	UnpricedTurnCount  basequery.NumericValue `json:"unpricedTurnCount"`
	FirstActivityAtMS  basequery.NumericValue `json:"firstActivityAtMs"`
	LastActivityAtMS   basequery.NumericValue `json:"lastActivityAtMs"`
}

type TrendPoint struct {
	Key       string                 `json:"key"`
	StartAtMS basequery.NumericValue `json:"startAtMs"`
	EndAtMS   basequery.NumericValue `json:"endAtMs"`
	Totals    UsageTotals            `json:"totals"`
}

type ReasonCount struct {
	Reason pricing.CostReason     `json:"reason"`
	Count  basequery.NumericValue `json:"count"`
}

type UsageModelItem struct {
	DimensionKey string           `json:"dimensionKey"`
	Model        AttributionValue `json:"model"`
	Totals       UsageTotals      `json:"totals"`
}

type UsageCostResponse struct {
	Meta              basequery.ResponseMeta `json:"meta"`
	Range             basequery.UTCTimeRange `json:"range"`
	ReportingTimeZone string                 `json:"reportingTimeZone"`
	PricingSource     *string                `json:"pricingSource"`
	Currency          *string                `json:"currency"`
	PricingVersions   []string               `json:"pricingVersions"`
	Totals            UsageTotals            `json:"totals"`
	Trend             []TrendPoint           `json:"trend"`
	UnpricedReasons   []ReasonCount          `json:"unpricedReasons"`
	DegradedReason    *DegradedReason        `json:"degradedReason"`
	Models            []UsageModelItem       `json:"models"`
}

type AttributionValue struct {
	ID          *string `json:"id"`
	DisplayName *string `json:"displayName"`
	Confidence  string  `json:"confidence"`
	Source      string  `json:"source"`
	Reason      string  `json:"reason"`
}

type SessionItem struct {
	SessionID       string                 `json:"sessionId"`
	DisplayTitle    string                 `json:"displayTitle"`
	TitleConfidence string                 `json:"titleConfidence"`
	TitleSource     string                 `json:"titleSource"`
	TitleReason     string                 `json:"titleReason"`
	Project         AttributionValue       `json:"project"`
	Model           AttributionValue       `json:"model"`
	Activity        string                 `json:"activity"`
	LastActivityAt  basequery.NumericValue `json:"lastActivityAtMs"`
	Totals          UsageTotals            `json:"totals"`
}

type SessionListResponse struct {
	Meta           basequery.ResponseMeta `json:"meta"`
	PricingSource  *string                `json:"pricingSource"`
	Currency       *string                `json:"currency"`
	Items          []SessionItem          `json:"items"`
	MatchedCount   basequery.NumericValue `json:"matchedCount"`
	MatchedTotals  UsageTotals            `json:"matchedTotals"`
	PageTotals     UsageTotals            `json:"pageTotals"`
	DegradedReason *DegradedReason        `json:"degradedReason"`
}

type SessionDetailRequest struct {
	SessionID         string                `json:"sessionId"`
	ReportingTimezone *string               `json:"reportingTimezone"`
	TurnPage          basequery.PageRequest `json:"turnPage"`
}

// SessionTurnState 只表达安全的turn lifecycle，不推导业务状态机。
type SessionTurnState string

const (
	SessionTurnActive   SessionTurnState = "active"
	SessionTurnComplete SessionTurnState = "complete"
)

// SessionTurnPricingStatus 区分尚不可定价、已定价和明确未定价。
type SessionTurnPricingStatus string

const (
	SessionTurnPricingUnknown  SessionTurnPricingStatus = "unknown"
	SessionTurnPricingPriced   SessionTurnPricingStatus = "priced"
	SessionTurnPricingUnpriced SessionTurnPricingStatus = "unpriced"
)

// SessionTurnItem 是content-free turn usage/cost时间线条目。
type SessionTurnItem struct {
	TimelineKey    string                   `json:"timelineKey"`
	State          SessionTurnState         `json:"state"`
	Model          AttributionValue         `json:"model"`
	StartedAt      basequery.NumericValue   `json:"startedAtMs"`
	CompletedAt    basequery.NumericValue   `json:"completedAtMs"`
	ObservedAt     basequery.NumericValue   `json:"observedAtMs"`
	Totals         UsageTotals              `json:"totals"`
	PricingStatus  SessionTurnPricingStatus `json:"pricingStatus"`
	PricingVersion *string                  `json:"pricingVersion"`
	UnpricedReason *pricing.CostReason      `json:"unpricedReason"`
}

type SessionDetailResponse struct {
	Meta            basequery.ResponseMeta `json:"meta"`
	PricingSource   *string                `json:"pricingSource"`
	Currency        *string                `json:"currency"`
	PricingVersions []string               `json:"pricingVersions"`
	UnpricedReasons []ReasonCount          `json:"unpricedReasons"`
	Item            SessionItem            `json:"item"`
	TurnPage        basequery.PageInfo     `json:"turnPage"`
	Turns           []SessionTurnItem      `json:"turns"`
	DegradedReason  *DegradedReason        `json:"degradedReason"`
}

type ProjectItem struct {
	DimensionKey string                 `json:"dimensionKey"`
	Project      AttributionValue       `json:"project"`
	SessionCount basequery.NumericValue `json:"sessionCount"`
	Trend        []ProjectDailyPoint    `json:"trend"`
	Totals       UsageTotals            `json:"totals"`
}

type ProjectDailyPoint struct {
	BucketStartAt basequery.NumericValue `json:"bucketStartAtMs"`
	Confidence    string                 `json:"confidence"`
	Source        string                 `json:"source"`
	Reason        string                 `json:"reason"`
	Totals        UsageTotals            `json:"totals"`
}

type ProjectListResponse struct {
	Meta              basequery.ResponseMeta `json:"meta"`
	Range             basequery.UTCTimeRange `json:"range"`
	ReportingTimeZone string                 `json:"reportingTimeZone"`
	PricingSource     *string                `json:"pricingSource"`
	Currency          *string                `json:"currency"`
	PricingVersions   []string               `json:"pricingVersions"`
	Items             []ProjectItem          `json:"items"`
	MatchedCount      basequery.NumericValue `json:"matchedCount"`
	GlobalTotals      UsageTotals            `json:"globalTotals"`
	MatchedTotals     UsageTotals            `json:"matchedTotals"`
	PageTotals        UsageTotals            `json:"pageTotals"`
}

type ProjectDetailRequest struct {
	DimensionKey string                   `json:"dimensionKey"`
	Range        basequery.LocalDateRange `json:"range"`
	SessionPage  basequery.PageRequest    `json:"sessionPage"`
	ModelPage    basequery.PageRequest    `json:"modelPage"`
}

type ProjectSessionItem struct {
	SessionID       string                 `json:"sessionId"`
	DisplayTitle    string                 `json:"displayTitle"`
	TitleConfidence string                 `json:"titleConfidence"`
	TitleSource     string                 `json:"titleSource"`
	TitleReason     string                 `json:"titleReason"`
	Model           AttributionValue       `json:"model"`
	Activity        string                 `json:"activity"`
	LastActivityAt  basequery.NumericValue `json:"lastActivityAtMs"`
	Totals          UsageTotals            `json:"totals"`
}

type ProjectModelItem struct {
	DimensionKey string           `json:"dimensionKey"`
	Model        AttributionValue `json:"model"`
	Totals       UsageTotals      `json:"totals"`
}

type ProjectDetailResponse struct {
	Meta              basequery.ResponseMeta `json:"meta"`
	Range             basequery.UTCTimeRange `json:"range"`
	ReportingTimeZone string                 `json:"reportingTimeZone"`
	PricingSource     *string                `json:"pricingSource"`
	Currency          *string                `json:"currency"`
	PricingVersions   []string               `json:"pricingVersions"`
	Item              ProjectItem            `json:"item"`
	Daily             []ProjectDailyPoint    `json:"daily"`
	SessionPage       basequery.PageInfo     `json:"sessionPage"`
	Sessions          []ProjectSessionItem   `json:"sessions"`
	ModelPage         basequery.PageInfo     `json:"modelPage"`
	Models            []ProjectModelItem     `json:"models"`
	GlobalTotals      UsageTotals            `json:"globalTotals"`
}
