package pricing

import "strings"

const (
	GrokPricingVersion      = "xai-docs-2026-08-18"
	GrokPricingSourceURL    = "https://docs.x.ai/developers/pricing"
	GrokPricingVerifiedAtMS = int64(1_787_011_200_000)
)

type GrokModelRate struct {
	ModelID      string
	Patterns     []string
	InputMicros  int64
	CachedMicros int64
	OutputMicros int64
}

var builtinGrokModelRates = []GrokModelRate{
	grokRate("grok-4.6", []string{"grok-4.6", "grok-4-6", "grok-4.6-build"}, 2_000_000, 500_000, 6_000_000),
	grokRate("grok-4.5", []string{"grok-4.5", "grok-4-5"}, 2_000_000, 300_000, 6_000_000),
	grokRate("grok-4.3", []string{"grok-4.3", "grok-4-3"}, 1_250_000, 200_000, 2_500_000),
	grokRate("grok-4", []string{"grok-4", "grok-4-0709"}, 3_000_000, 750_000, 15_000_000),
	grokRate("grok-4.1-fast", []string{"grok-4.1-fast", "grok-4-1-fast"}, 200_000, 50_000, 500_000),
	grokRate("grok-code-fast-1", []string{"grok-code-fast", "grok-code-fast-1"}, 200_000, 50_000, 1_500_000),
}

func grokRate(id string, patterns []string, input, cached, output int64) GrokModelRate {
	return GrokModelRate{ModelID: id, Patterns: patterns, InputMicros: input, CachedMicros: cached, OutputMicros: output}
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
	normalized := strings.ToLower(strings.TrimSpace(model))
	normalized = strings.NewReplacer("_", "-", " ", "-", "/", "-").Replace(normalized)
	best, bestLen := GrokModelRate{}, -1
	for _, rate := range builtinGrokModelRates {
		for _, pattern := range rate.Patterns {
			if !grokPatternMatches(normalized, pattern) {
				continue
			}
			if len(pattern) > bestLen {
				best, bestLen = rate, len(pattern)
			}
		}
	}
	if bestLen < 0 {
		return GrokModelRate{}, false
	}
	return best, true
}

func grokPatternMatches(model, pattern string) bool {
	return model == pattern || strings.HasPrefix(model, pattern+"-")
}

func EstimateGrokUsageCost(model string, input, cachedRead, cacheCreation, output int64) (int64, bool) {
	rate, ok := GrokRateForModel(model)
	if !ok || input < 0 || cachedRead < 0 || cacheCreation < 0 || output < 0 {
		return 0, false
	}
	uncached := input - cachedRead
	if uncached < 0 {
		uncached = input
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
