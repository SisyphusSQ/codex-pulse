package store

import "github.com/SisyphusSQ/codex-pulse/internal/pricing"

type CostRollupGenerationState string

const (
	CostRollupGenerationBuilding   CostRollupGenerationState = "building"
	CostRollupGenerationActive     CostRollupGenerationState = "active"
	CostRollupGenerationSuperseded CostRollupGenerationState = "superseded"
)

// CostRollupGeneration 标识一次不可变成本快照及其原子可见状态。
type CostRollupGeneration struct {
	GenerationID      string
	ReportingTimezone string
	PricingSource     string
	Currency          string
	RollupVersion     int64
	State             CostRollupGenerationState
	CreatedAtMS       int64
	CompletedAtMS     *int64
	UpdatedAtMS       int64
}

// RebuildCostLedgerRequest 使用显式 generation identity，支持安全幂等重放。
type RebuildCostLedgerRequest struct {
	GenerationID      string
	ReportingTimezone string
	PricingSource     string
	Currency          string
	RollupVersion     int64
	CalculatedAtMS    int64
}

type RebuildCostLedgerReport struct {
	GenerationID       string
	FinalTurns         int64
	PricedTurns        int64
	UnpricedTurns      int64
	EstimatedUSDMicros *int64
	Replayed           bool
}

type TurnCost struct {
	GenerationID       string
	TurnID             string
	PricingVersion     *string
	EstimatedUSDMicros *int64
	Status             pricing.CostStatus
	Reason             pricing.CostReason
	CalculatedAtMS     int64
}

// RollupTotals 保留各 token 类别的 unknown/zero 差异，并允许 partial priced subtotal。
type RollupTotals struct {
	TurnCount          int64
	InputTokens        *int64
	CachedInputTokens  *int64
	OutputTokens       *int64
	ReasoningTokens    *int64
	TotalTokens        *int64
	EstimatedUSDMicros *int64
	PricedTurnCount    int64
	UnpricedTurnCount  int64
	FirstActivityAtMS  int64
	LastActivityAtMS   int64
	UpdatedAtMS        int64
}

type SessionUsageRollup struct {
	GenerationID string
	SessionID    string
	RollupTotals
}

type UsageDaily struct {
	GenerationID      string
	BucketStartMS     int64
	ReportingTimezone string
	RollupTotals
}

type ProjectUsageDaily struct {
	GenerationID          string
	BucketStartMS         int64
	ReportingTimezone     string
	DimensionKey          string
	ProjectID             *string
	ProjectDisplayName    *string
	AttributionConfidence string
	AttributionSource     string
	AttributionReason     string
	RollupTotals
}

type ModelUsageDaily struct {
	GenerationID          string
	BucketStartMS         int64
	ReportingTimezone     string
	DimensionKey          string
	ModelKey              *string
	ModelDisplayName      *string
	AttributionConfidence string
	AttributionSource     string
	AttributionReason     string
	RollupTotals
}

// CostLedgerSnapshot 只返回同一 active generation 的完整成本与聚合。
type CostLedgerSnapshot struct {
	Generation     CostRollupGeneration
	TurnCosts      []TurnCost
	SessionRollups []SessionUsageRollup
	DailyRollups   []UsageDaily
	ProjectDaily   []ProjectUsageDaily
	ModelDaily     []ModelUsageDaily
}
