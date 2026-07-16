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
	Range        AnalyticsRange
	DimensionKey string
}

type ProjectAnalyticsRecord struct {
	DimensionKey          string
	ProjectID             *string
	ProjectDisplayName    *string
	AttributionConfidence string
	AttributionSource     string
	AttributionReason     string
	Totals                RollupTotals
}

type ProjectAnalyticsPage struct {
	Generation      CostRollupGeneration
	Records         []ProjectAnalyticsRecord
	MatchedCount    int64
	GlobalTotals    RollupTotals
	MatchedTotals   RollupTotals
	PageTotals      RollupTotals
	PricingVersions []string
	NextCursor      *ProjectAnalyticsCursor
}

type ProjectAnalyticsSnapshot struct {
	Generation      CostRollupGeneration
	Record          ProjectAnalyticsRecord
	Daily           []ProjectUsageDaily
	GlobalTotals    RollupTotals
	PricingVersions []string
}
