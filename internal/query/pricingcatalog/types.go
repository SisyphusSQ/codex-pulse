package pricingcatalog

import basequery "github.com/SisyphusSQ/codex-pulse/internal/query"

const (
	ContractVersion = "pricing-catalog-v1"
	BasisStandard   = "openai_api_standard_short_context_text"
	SourceOpenAIAPI = "openai-api"
	CurrencyUSD     = "USD"
	UnitTokens      = int64(1_000_000)
)

type ModelReferencePrice struct {
	ModelID           string                 `json:"modelId"`
	InputMicros       basequery.NumericValue `json:"inputMicros"`
	CachedInputMicros basequery.NumericValue `json:"cachedInputMicros"`
	OutputMicros      basequery.NumericValue `json:"outputMicros"`
}

type CurrentResponse struct {
	Meta            basequery.ResponseMeta `json:"meta"`
	EvaluatedAtMS   basequery.NumericValue `json:"evaluatedAtMs"`
	PricingVersion  string                 `json:"pricingVersion"`
	Source          string                 `json:"source"`
	Currency        string                 `json:"currency"`
	Basis           string                 `json:"basis"`
	UnitTokens      basequery.NumericValue `json:"unitTokens"`
	EffectiveFromMS basequery.NumericValue `json:"effectiveFromMs"`
	VerifiedAtMS    basequery.NumericValue `json:"verifiedAtMs"`
	SourceURL       *string                `json:"sourceUrl,omitempty"`
	Items           []ModelReferencePrice  `json:"items"`
}
