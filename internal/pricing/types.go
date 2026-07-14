package pricing

import "errors"

const TokensPerMillion int64 = 1_000_000

var (
	ErrInvalidCalculation = errors.New("pricing calculation input is invalid")
	ErrCostOverflow       = errors.New("pricing result exceeds integer microUSD range")
)

type ModelMatchKind string

const (
	ModelMatchExact   ModelMatchKind = "exact"
	ModelMatchPrefix  ModelMatchKind = "prefix"
	ModelMatchDefault ModelMatchKind = "default"
)

// ModelPrice 使用整数微美元/百万 token；nil 与真实零保持不同语义。
type ModelPrice struct {
	MatchKind                   ModelMatchKind
	ModelPattern                string
	Priority                    int64
	InputMicrosPerMillion       *int64
	CachedInputMicrosPerMillion *int64
	OutputMicrosPerMillion      *int64
}

// CatalogVersion 是 source/currency 时间线上的不可变价格快照。
type CatalogVersion struct {
	PricingVersion  string
	Source          string
	Currency        string
	EffectiveFromMS int64
	CreatedAtMS     int64
	SourceURL       string
	VerifiedAtMS    int64
	Models          []ModelPrice
}

// Rates 表示一个 model rule 的整数微美元/百万 token 价格。
type Rates struct {
	InputMicrosPerMillion       *int64
	CachedInputMicrosPerMillion *int64
	OutputMicrosPerMillion      *int64
}

// Usage 保留 Codex JSONL 的 nullable token 类别。ReasoningTokens 独立于 OutputTokens。
type Usage struct {
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
	ReasoningTokens   *int64
}

type CostStatus string

const (
	CostStatusPriced   CostStatus = "priced"
	CostStatusUnpriced CostStatus = "unpriced"
)

type CostReason string

const (
	CostReasonPriced                CostReason = "priced"
	CostReasonMissingAttribution    CostReason = "missing_attribution"
	CostReasonMissingModel          CostReason = "missing_model"
	CostReasonConflictModel         CostReason = "conflict_model"
	CostReasonInvalidModel          CostReason = "invalid_model"
	CostReasonCatalogNotEffective   CostReason = "catalog_not_effective"
	CostReasonModelNotListed        CostReason = "model_not_listed"
	CostReasonMissingToken          CostReason = "missing_token"
	CostReasonMissingPriceComponent CostReason = "missing_price_component"
)

// Calculation 是纯计算结果；unpriced 时金额必须为 nil，真实零使用非 nil 0。
type Calculation struct {
	Status             CostStatus
	Reason             CostReason
	EstimatedUSDMicros *int64
}
