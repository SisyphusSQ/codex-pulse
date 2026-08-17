package pricingcatalog

import "github.com/SisyphusSQ/codex-pulse/internal/agentprovider"

import basequery "github.com/SisyphusSQ/codex-pulse/internal/query"

const (
	ContractVersion  = "pricing-catalog-v1"
	BasisStandard    = "openai_api_standard_short_context_text"
	BasisCursor      = "cursor_published_model_token_rates"
	SourceOpenAIAPI  = "openai-api"
	SourceCursorDocs = "cursor_pricing_docs"
	CurrencyUSD      = "USD"
	UnitTokens       = int64(1_000_000)
)

type ModelReferencePrice struct {
	ModelID           string                 `json:"modelId"`
	InputMicros       basequery.NumericValue `json:"inputMicros"`
	CachedInputMicros basequery.NumericValue `json:"cachedInputMicros"`
	OutputMicros      basequery.NumericValue `json:"outputMicros"`
	CacheWriteMicros  basequery.NumericValue `json:"cacheWriteMicros"`
}

type CurrentResponse struct {
	ProviderContext agentprovider.Context  `json:"providerContext"`
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
