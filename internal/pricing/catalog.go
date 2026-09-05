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
	builtinPricing20260729Version       = "openai-api-2026-07-29"
	builtinPricing20260729EffectiveAtMS = int64(1_785_254_400_000)
	builtinPricing20260729VerifiedAtMS  = int64(1_785_254_400_000)
	builtinPricing20260731Version       = "openai-api-2026-07-31"
	builtinPricing20260731EffectiveAtMS = int64(1_785_464_448_000)
	builtinPricing20260731VerifiedAtMS  = int64(1_785_464_448_000)
	builtinPricing20260903EffectiveAtMS = int64(1_788_393_600_000)
	builtinPricing20260905VerifiedAtMS  = int64(1_788_573_994_000)
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
	rates := builtinOpenAI20260722Rates()
	return catalogFromRates(
		builtinPricing20260722Version, builtinPricing20260722EffectiveAtMS,
		builtinPricing20260722VerifiedAtMS, builtinPricing20260722URL, rates,
	)
}

func builtinOpenAI20260722Rates() []builtinModelRate {
	rates := append([]builtinModelRate(nil), builtinOpenAIModelRates[:]...)
	rates = append(rates, builtinModelRate{
		model: "gpt-5.4-mini", input: 750_000, cached: 75_000, output: 4_500_000,
	})
	return rates
}

// BuiltinOpenAI20260729 固化通用官方价格页的完整当前快照；旧版本和其来源
// 元数据保持不可变，避免安装时把同一 pricing_version 解释成不同事实。
func BuiltinOpenAI20260729() CatalogVersion {
	return catalogFromRates(
		builtinPricing20260729Version, builtinPricing20260729EffectiveAtMS,
		builtinPricing20260729VerifiedAtMS, builtinPricingURL, builtinOpenAI20260722Rates(),
	)
}

// BuiltinOpenAI20260731 固化 GPT-5.6 Terra 与 Luna 的官方降价；它从上一版
// 独立复制费率后替换对应 exact 规则，旧目录及历史成本保持不可变。
func BuiltinOpenAI20260731() CatalogVersion {
	return catalogFromRates(
		builtinPricing20260731Version, builtinPricing20260731EffectiveAtMS,
		builtinPricing20260731VerifiedAtMS, builtinPricingURL, builtinOpenAI20260731Rates(),
	)
}

func builtinOpenAI20260731Rates() []builtinModelRate {
	rates := builtinOpenAI20260722Rates()
	for index := range rates {
		switch rates[index].model {
		case "gpt-5.6-terra":
			rates[index] = builtinModelRate{
				model: "gpt-5.6-terra", input: 2_000_000, cached: 200_000, output: 12_000_000,
			}
		case "gpt-5.6-luna":
			rates[index] = builtinModelRate{
				model: "gpt-5.6-luna", input: 200_000, cached: 20_000, output: 1_200_000,
			}
		}
	}
	return rates
}

// BuiltinOpenAI20260903 从 Astra 发布日（UTC 日界）增补基础参考价格；
// 发布日粒度用于本地估算，不声称精确 API 开放时刻。其它历史费率保持不变。
func BuiltinOpenAI20260903() CatalogVersion {
	rates := append(builtinOpenAI20260731Rates(), builtinModelRate{
		model: "gpt-6-astra", input: 10_000_000, cached: 1_000_000, output: 50_000_000,
	})
	return catalogFromRates("openai-api-2026-09-03", builtinPricing20260903EffectiveAtMS,
		builtinPricing20260905VerifiedAtMS, builtinPricingURL, rates)
}

// BuiltinOpenAI20260905 从官方核验时刻应用 Sol 及其官方 alias 的促销价；
// 未知的历史降价日期不回填，促销结束后也不猜测恢复价格。
func BuiltinOpenAI20260905() CatalogVersion {
	catalog := BuiltinOpenAI20260903()
	catalog.PricingVersion = "openai-api-2026-09-05"
	catalog.EffectiveFromMS = builtinPricing20260905VerifiedAtMS
	for index := range catalog.Models {
		model := &catalog.Models[index]
		if model.ModelPattern == "gpt-5.6-sol" || model.ModelPattern == "gpt-5.6" {
			*model.InputMicrosPerMillion = 4_000_000
			*model.CachedInputMicrosPerMillion = 400_000
			*model.OutputMicrosPerMillion = 20_000_000
		}
	}
	return catalog
}

// BuiltinOpenAICatalog 返回按生效时间升序排列的完整内置价格历史。
func BuiltinOpenAICatalog() []CatalogVersion {
	return []CatalogVersion{
		BuiltinOpenAI20260714(),
		BuiltinOpenAI20260722(),
		BuiltinOpenAI20260729(),
		BuiltinOpenAI20260731(),
		BuiltinOpenAI20260903(),
		BuiltinOpenAI20260905(),
	}
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
