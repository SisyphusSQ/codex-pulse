package store

import (
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
)

var ErrAnalyticsUnavailable = errors.New("analytics rollup is unavailable")

// AnalyticsReadMode 说明 range query 使用 active rollup 还是只读 detail fallback。
type AnalyticsReadMode string

const (
	AnalyticsReadActiveRollup      AnalyticsReadMode = "active_rollup"
	AnalyticsReadDetailFallback    AnalyticsReadMode = "detail_fallback"
	AnalyticsReadAmbiguousFallback AnalyticsReadMode = "ambiguous_fallback"
	AnalyticsReadLightIndex        AnalyticsReadMode = "light_index"
)

// AnalyticsRange 是已由业务 query 归一化的 IANA 本地日 UTC 半开区间。
type AnalyticsRange struct {
	ReportingTimezone string
	StartAtMS         int64
	EndAtMS           int64
}

// CostReasonCount 汇总 range 内未定价 turn 的稳定原因。
type CostReasonCount struct {
	Reason pricing.CostReason
	Count  int64
}

// UsageCostRangeSnapshot 是同一 SQLite read snapshot 内的 range 事实。
type UsageCostRangeSnapshot struct {
	Mode            AnalyticsReadMode
	Generation      *CostRollupGeneration
	Daily           []UsageDaily
	PricingVersions []string
	UnpricedReasons []CostReasonCount
}

type AnalyticsSortDirection string

const (
	AnalyticsSortAscending  AnalyticsSortDirection = "asc"
	AnalyticsSortDescending AnalyticsSortDirection = "desc"
)

type SessionActivity string

const (
	SessionActivityActive SessionActivity = "active"
	SessionActivityIdle   SessionActivity = "idle"
)

type SessionAnalyticsSortField string

const (
	SessionAnalyticsSortLastActivity  SessionAnalyticsSortField = "last_activity_at"
	SessionAnalyticsSortTotalTokens   SessionAnalyticsSortField = "total_tokens"
	SessionAnalyticsSortEstimatedCost SessionAnalyticsSortField = "estimated_cost"
)

// SessionAnalyticsCursor 是 service 解码后的 typed keyset；Store 不接受 opaque
// client string，也不把 offset 当分页真相。
type SessionAnalyticsCursor struct {
	SessionID string
	Null      bool
	Value     *int64
}

type SessionAnalyticsFilter struct {
	ReportingTimezone       *string
	ProjectIDs              []string
	ModelKeys               []string
	Activity                *SessionActivity
	LastActivityAtOrAfterMS *int64
	LastActivityBeforeMS    *int64
	Limit                   int
	SortField               SessionAnalyticsSortField
	SortDirection           AnalyticsSortDirection
	Cursor                  *SessionAnalyticsCursor
}

type SessionAnalyticsDetailFilter struct {
	SessionID         string
	ReportingTimezone *string
	TurnLimit         int
	TurnCursor        *SessionTurnAnalyticsCursor
}

// SessionTurnAnalyticsCursor 是 Session detail turn 时间线的typed keyset。
// TurnID只在Store与opaque cursor codec之间流转，不进入跨端DTO。
type SessionTurnAnalyticsCursor struct {
	SessionID   string
	TurnID      string
	StartedAtMS int64
}

// SessionTurnUsageAnalytics 保留turn当前usage的unknown与真实零事实，
// 不暴露source generation、offset或其它parser内部位置。
type SessionTurnUsageAnalytics struct {
	ObservedAtMS      int64
	IsFinal           bool
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
	ReasoningTokens   *int64
}

// SessionTurnCostAnalytics 只携带active generation的安全pricing evidence。
type SessionTurnCostAnalytics struct {
	PricingVersion     *string
	EstimatedUSDMicros *int64
	Status             pricing.CostStatus
	Reason             pricing.CostReason
}

// SessionTurnAnalyticsRecord 是content-free turn timeline read model。
type SessionTurnAnalyticsRecord struct {
	TurnID        string
	Model         ModelAttribution
	StartedAtMS   int64
	CompletedAtMS *int64
	Usage         *SessionTurnUsageAnalytics
	Cost          *SessionTurnCostAnalytics
}

// SessionAnalyticsRecord 只包含安全 attribution 和聚合数字；canonical cwd、
// raw model 与本机路径不会进入该 read model。
type SessionAnalyticsRecord struct {
	SessionID        string
	DisplayTitle     string
	TitleConfidence  AttributionConfidence
	TitleSource      AttributionSource
	TitleReason      AttributionReason
	Project          ProjectAttribution
	Model            ModelAttribution
	Activity         SessionActivity
	LastActivityAtMS *int64
	Rollup           *RollupTotals
}

type SessionAnalyticsPage struct {
	Mode          AnalyticsReadMode
	Generation    *CostRollupGeneration
	Records       []SessionAnalyticsRecord
	MatchedCount  int64
	MatchedTotals *RollupTotals
	PageTotals    *RollupTotals
	NextCursor    *SessionAnalyticsCursor
}

type SessionAnalyticsSnapshot struct {
	Mode            AnalyticsReadMode
	Generation      *CostRollupGeneration
	Record          SessionAnalyticsRecord
	Turns           []SessionTurnAnalyticsRecord
	NextTurnCursor  *SessionTurnAnalyticsCursor
	PricingVersions []string
	UnpricedReasons []CostReasonCount
}

type ProjectAnalyticsSortField string

const (
	ProjectAnalyticsSortLastActivity  ProjectAnalyticsSortField = "last_activity_at"
	ProjectAnalyticsSortTotalTokens   ProjectAnalyticsSortField = "total_tokens"
	ProjectAnalyticsSortEstimatedCost ProjectAnalyticsSortField = "estimated_cost"
	ProjectAnalyticsSortDisplayName   ProjectAnalyticsSortField = "display_name"
)

type ProjectAnalyticsCursor struct {
	DimensionKey string
	Null         bool
	NumericValue *int64
	TextValue    *string
}

type ProjectAnalyticsFilter struct {
	Range         AnalyticsRange
	ProjectIDs    []string
	Confidences   []string
	Limit         int
	SortField     ProjectAnalyticsSortField
	SortDirection AnalyticsSortDirection
	Cursor        *ProjectAnalyticsCursor
}

type ProjectAnalyticsDetailFilter struct {
	Range         AnalyticsRange
	DimensionKey  string
	SessionLimit  int
	SessionCursor *ProjectSessionAnalyticsCursor
	ModelLimit    int
	ModelCursor   *ProjectModelAnalyticsCursor
}

type ProjectSessionAnalyticsCursor struct {
	GenerationID     string
	DimensionKey     string
	SessionID        string
	LastActivityAtMS int64
}

type ProjectModelAnalyticsCursor struct {
	GenerationID      string
	DimensionKey      string
	ModelDimensionKey string
	Null              bool
	TotalTokens       *int64
}

type ProjectAnalyticsRecord struct {
	DimensionKey          string
	ProjectID             *string
	ProjectDisplayName    *string
	AttributionConfidence string
	AttributionSource     string
	AttributionReason     string
	SessionCount          int64
	Trend                 []ProjectUsageDaily
	Totals                RollupTotals
}

type ProjectAnalyticsPage struct {
	Mode            AnalyticsReadMode
	Generation      CostRollupGeneration
	Records         []ProjectAnalyticsRecord
	MatchedCount    int64
	GlobalTotals    RollupTotals
	MatchedTotals   RollupTotals
	PageTotals      RollupTotals
	PricingVersions []string
	NextCursor      *ProjectAnalyticsCursor
}

type ProjectSessionAnalyticsRecord struct {
	SessionID        string
	DisplayTitle     string
	TitleConfidence  AttributionConfidence
	TitleSource      AttributionSource
	TitleReason      AttributionReason
	Model            ModelAttribution
	Activity         SessionActivity
	LastActivityAtMS int64
	Totals           RollupTotals
}

type ProjectModelAnalyticsRecord struct {
	DimensionKey string
	Model        ModelAttribution
	Totals       RollupTotals
}

type ProjectAnalyticsSnapshot struct {
	Mode              AnalyticsReadMode
	Generation        CostRollupGeneration
	Record            ProjectAnalyticsRecord
	Daily             []ProjectUsageDaily
	Sessions          []ProjectSessionAnalyticsRecord
	NextSessionCursor *ProjectSessionAnalyticsCursor
	Models            []ProjectModelAnalyticsRecord
	NextModelCursor   *ProjectModelAnalyticsCursor
	GlobalTotals      RollupTotals
	PricingVersions   []string
}
