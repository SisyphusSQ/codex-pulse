package pricing

import (
	"strings"
	"time"
)

const (
	GrokPricingVersion      = "xai-docs-2026-09-23"
	GrokPricingSourceURL    = "https://docs.x.ai/developers/pricing"
	GrokPricingVerifiedAtMS = int64(1_790_123_508_000)
	grokLongContextTokens   = int64(200_000)
)

var grok47ReleaseAtMS = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC).UnixMilli()
var grokLegacyRedirectAtMS = time.Date(2026, 5, 15, 19, 0, 0, 0, time.UTC).UnixMilli() // 12:00 PDT

type GrokModelRate struct {
	ModelID                string
	Patterns               []string
	InputMicros            int64
	CachedMicros           int64
	OutputMicros           int64
	ContextThresholdTokens int64
}

// 上一版费率只参与旧事件估算，不作为现行官方目录展示。
var grok20260818Rates = []GrokModelRate{
	grokRate("grok-4.6", []string{"grok-4.6", "grok-4-6", "grok-4.6-build"}, 2_000_000, 500_000, 6_000_000),
	grokRate("grok-4.5", []string{"grok-4.5", "grok-4-5"}, 2_000_000, 300_000, 6_000_000),
	grokRate("grok-4.3", []string{"grok-4.3", "grok-4-3"}, 1_250_000, 200_000, 2_500_000),
	grokRate("grok-4", []string{"grok-4", "grok-4-0709"}, 3_000_000, 750_000, 15_000_000),
	grokRate("grok-4.1-fast", []string{"grok-4.1-fast", "grok-4-1-fast"}, 200_000, 50_000, 500_000),
	grokRate("grok-code-fast-1", []string{"grok-code-fast", "grok-code-fast-1"}, 200_000, 50_000, 1_500_000),
}

var builtinGrokModelRates = grok20260923Rates()

func grok20260923Rates() []GrokModelRate {
	short := []GrokModelRate{
		grokRate("grok-4.7", []string{"grok-4.7", "grok-4-7"}, 2_000_000, 500_000, 6_000_000),
		grokRate("grok-4.7 (Fast)", []string{"grok-4.7-fast", "grok-4-7-fast"}, 4_000_000, 1_000_000, 12_000_000),
		grokRate("grok-build-0.1", []string{"grok-build-0.1", "grok-build-0-1"}, 1_000_000, 200_000, 2_000_000),
		grokRate("grok-4.6", []string{"grok-4.6", "grok-4-6", "grok-4.6-build"}, 2_000_000, 500_000, 6_000_000),
		grokRate("grok-4.5", []string{"grok-4.5", "grok-4-5"}, 2_000_000, 300_000, 6_000_000),
		grokRate("grok-4.3", []string{"grok-4.3", "grok-4-3"}, 1_250_000, 200_000, 2_500_000),
		grokRate("grok-4.20-multi-agent-0309", []string{"grok-4.20-multi-agent-0309"}, 1_250_000, 200_000, 2_500_000),
		grokRate("grok-4.20-0309-reasoning", []string{"grok-4.20-0309-reasoning"}, 1_250_000, 200_000, 2_500_000),
		grokRate("grok-4.20-0309-non-reasoning", []string{"grok-4.20-0309-non-reasoning"}, 1_250_000, 200_000, 2_500_000),
	}
	rates := make([]GrokModelRate, 0, 2*len(short))
	for _, rate := range short {
		rate.ContextThresholdTokens = grokLongContextTokens
		rates = append(rates, rate)
		long := rate
		long.ModelID += " (long context ≥200k)"
		long.Patterns = nil // 聚合用量不能确定逐请求上下文，长档只作参考价展示。
		if rate.ModelID == "grok-4.7 (Fast)" {
			long.InputMicros, long.CachedMicros, long.OutputMicros = 6_000_000, 1_500_000, 18_000_000
		} else {
			long.InputMicros *= 2
			long.CachedMicros *= 2
			long.OutputMicros *= 2
		}
		rates = append(rates, long)
	}
	return rates
}

func grokRate(id string, patterns []string, input, cached, output int64) GrokModelRate {
	rate := GrokModelRate{ModelID: id, Patterns: patterns, InputMicros: input, CachedMicros: cached, OutputMicros: output}
	if id == "grok-4.6" || id == "grok-4.5" || id == "grok-4.3" {
		rate.ContextThresholdTokens = grokLongContextTokens
	}
	return rate
}

func BuiltinGrokModelRates() []GrokModelRate {
	rates := make([]GrokModelRate, len(builtinGrokModelRates))
	for index, rate := range builtinGrokModelRates {
		rates[index] = rate
		rates[index].Patterns = append([]string(nil), rate.Patterns...)
	}
	return rates
}

func GrokRateForModel(model string) (GrokModelRate, bool) {
	return grokRateForModelIn(model, builtinGrokModelRates)
}

func grokRateForModelAt(model string, occurredAtMS int64) (GrokModelRate, bool) {
	if occurredAtMS >= GrokPricingVerifiedAtMS {
		return GrokRateForModel(model)
	}
	if occurredAtMS >= grok47ReleaseAtMS {
		if rate, ok := grokRateForModelIn(model, builtinGrokModelRates[:4]); ok {
			return rate, true
		}
	}
	if occurredAtMS >= grokLegacyRedirectAtMS {
		normalized := strings.ToLower(strings.TrimSpace(model))
		if normalized == "grok-4-0709" || normalized == "grok-code-fast" || normalized == "grok-code-fast-1" {
			return GrokModelRate{}, false // 旧 alias 已重定向，历史目标费率没有可靠快照。
		}
	}
	return grokRateForModelIn(model, grok20260818Rates)
}

func grokRateForModelIn(model string, rates []GrokModelRate) (GrokModelRate, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	normalized = strings.NewReplacer("_", "-", " ", "-", "/", "-").Replace(normalized)
	for _, rate := range rates {
		for _, pattern := range rate.Patterns {
			if normalized == pattern {
				return rate, true
			}
		}
	}
	return GrokModelRate{}, false
}

// EstimateGrokUsageCostAt 只在聚合 input 低于门槛时使用短档；超过门槛时
// 无法从 modelUsage 的聚合 token 判断每次请求的 prompt 长度。
func EstimateGrokUsageCostAt(model string, occurredAtMS, input, cachedRead, cacheCreation, output int64) (int64, bool) {
	rate, ok := grokRateForModelAt(model, occurredAtMS)
	if !ok || input < 0 || cachedRead < 0 || cacheCreation < 0 || output < 0 ||
		(rate.ContextThresholdTokens > 0 && input >= rate.ContextThresholdTokens) {
		return 0, false
	}
	uncached := input - cachedRead
	if uncached < 0 {
		return 0, false
	}
	var total int64
	for _, part := range []struct {
		tokens int64
		rate   int64
	}{
		{uncached, rate.InputMicros},
		{cachedRead, rate.CachedMicros},
		{cacheCreation, rate.InputMicros},
		{output, rate.OutputMicros},
	} {
		value, ok := mulGrokCost(part.tokens, part.rate)
		if !ok || (value > 0 && total > (1<<62)-value) {
			return 0, false
		}
		total += value
	}
	return total, true
}

func mulGrokCost(tokens, microsPerMillion int64) (int64, bool) {
	if tokens == 0 || microsPerMillion == 0 {
		return 0, true
	}
	const million = int64(1_000_000)
	if microsPerMillion > 0 && tokens > (1<<62)/microsPerMillion {
		return 0, false
	}
	return tokens * microsPerMillion / million, true
}
