package cursorprovider

import (
	"math/big"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	cursorPricingVersion   = pricing.CursorPricingVersion
	cursorPricingSourceURL = pricing.CursorPricingSourceURL
	cursorTokensPerMillion = int64(1_000_000)
)

func estimateCursorDashboardCost(events []store.CursorDashboardUsageEvent) (int64, string, bool) {
	numerator := new(big.Int)
	for _, event := range events {
		if !event.TokenBased || event.ModelKey == nil {
			return 0, "", false
		}
		rate, ok := pricing.CursorRateForModel(*event.ModelKey, event.OccurredAtMS)
		if !ok || event.CacheWriteTokens > 0 && rate.CacheWriteMicros == nil {
			return 0, "", false
		}
		occurrences := big.NewInt(event.OccurrenceCount)
		addCursorCostComponent(numerator, event.InputTokens, rate.InputMicros, occurrences)
		if rate.CacheWriteMicros != nil {
			addCursorCostComponent(numerator, event.CacheWriteTokens, *rate.CacheWriteMicros, occurrences)
		}
		addCursorCostComponent(numerator, event.CacheReadTokens, rate.CacheReadMicros, occurrences)
		addCursorCostComponent(numerator, event.OutputTokens, rate.OutputMicros, occurrences)
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, big.NewInt(cursorTokensPerMillion), remainder)
	if remainder.Cmp(big.NewInt(cursorTokensPerMillion/2)) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, "", false
	}
	return quotient.Int64(), cursorPricingVersion, true
}

func addCursorCostComponent(total *big.Int, tokens, rate int64, occurrences *big.Int) {
	if tokens == 0 || rate == 0 {
		return
	}
	component := new(big.Int).Mul(big.NewInt(tokens), big.NewInt(rate))
	component.Mul(component, occurrences)
	total.Add(total, component)
}
