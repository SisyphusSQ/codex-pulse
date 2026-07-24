package pricing

const (
	builtinPricingVersion               = "openai-api-2026-07-14"
	builtinPricingSource                = "openai-api"
	builtinPricingURL                   = "https://developers.openai.com/api/docs/pricing"
	builtinVerifiedAtMS                 = int64(1_783_987_200_000)
	builtinPricing20260722Version       = "openai-api-2026-07-22"
	builtinPricing20260722URL           = "https://developers.openai.com/api/docs/models/gpt-5.4-mini"
	builtinPricing20260722EffectiveAtMS = int64(1_773_705_600_000)
	builtinPricing20260722VerifiedAtMS  = int64(1_784_678_400_000)
)

type builtinModelRate struct {
	model                 string
	input, cached, output int64
}

var builtinOpenAIModelRates = [...]builtinModelRate{
	{model: "gpt-5-codex", input: 1_250_000, cached: 125_000, output: 10_000_000},
	{model: "gpt-5.1-codex", input: 1_250_000, cached: 125_000, output: 10_000_000},
	{model: "gpt-5.1-codex-max", input: 1_250_000, cached: 125_000, output: 10_000_000},
	{model: "gpt-5.2-codex", input: 1_750_000, cached: 175_000, output: 14_000_000},
	{model: "gpt-5.3-codex", input: 1_750_000, cached: 175_000, output: 14_000_000},
	{model: "gpt-5.4", input: 2_500_000, cached: 250_000, output: 15_000_000},
	{model: "gpt-5.5", input: 5_000_000, cached: 500_000, output: 30_000_000},
	{model: "gpt-5.6", input: 5_000_000, cached: 500_000, output: 30_000_000},
	{model: "gpt-5.6-sol", input: 5_000_000, cached: 500_000, output: 30_000_000},
	{model: "gpt-5.6-terra", input: 2_500_000, cached: 250_000, output: 15_000_000},
	{model: "gpt-5.6-luna", input: 1_000_000, cached: 100_000, output: 6_000_000},
}

// BuiltinOpenAI20260714 返回独立可变副本，调用方不能污染进程内 catalog 模板。
func BuiltinOpenAI20260714() CatalogVersion {
	return catalogFromRates(
		builtinPricingVersion, 0, builtinVerifiedAtMS, builtinPricingURL,
		builtinOpenAIModelRates[:],
	)
}

// BuiltinOpenAI20260722 增补 GPT-5.4 mini 的官方 API 价格；旧版本保持不可变，
// observed_at 早于模型发布日期的事实仍解析到先前 catalog。
func BuiltinOpenAI20260722() CatalogVersion {
	rates := append([]builtinModelRate(nil), builtinOpenAIModelRates[:]...)
	rates = append(rates, builtinModelRate{
		model: "gpt-5.4-mini", input: 750_000, cached: 75_000, output: 4_500_000,
	})
	return catalogFromRates(
		builtinPricing20260722Version, builtinPricing20260722EffectiveAtMS,
		builtinPricing20260722VerifiedAtMS, builtinPricing20260722URL, rates,
	)
}

// BuiltinOpenAICatalog 返回按生效时间升序排列的完整内置价格历史。
func BuiltinOpenAICatalog() []CatalogVersion {
	return []CatalogVersion{BuiltinOpenAI20260714(), BuiltinOpenAI20260722()}
}

func catalogFromRates(
	version string,
	effectiveFromMS int64,
	verifiedAtMS int64,
	sourceURL string,
	rates []builtinModelRate,
) CatalogVersion {
	models := make([]ModelPrice, 0, len(rates))
	for _, rate := range rates {
		input, cached, output := rate.input, rate.cached, rate.output
		models = append(models, ModelPrice{
			MatchKind: ModelMatchExact, ModelPattern: rate.model, Priority: 100,
			InputMicrosPerMillion: &input, CachedInputMicrosPerMillion: &cached,
			OutputMicrosPerMillion: &output,
		})
	}
	return CatalogVersion{
		PricingVersion: version, Source: builtinPricingSource, Currency: "USD",
		EffectiveFromMS: effectiveFromMS, CreatedAtMS: verifiedAtMS,
		SourceURL: sourceURL, VerifiedAtMS: verifiedAtMS,
		Models: models,
	}
}
