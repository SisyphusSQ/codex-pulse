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

type cursorDashboardCostEstimate struct {
	estimatedUSDMicros  int64
	pricingVersion      string
	pricedTurnCount     int64
	unpricedTurnCount   int64
	unpricedReasonCount map[pricing.CostReason]int64
	hasKnownEstimate    bool
}

func estimateCursorDashboardCost(events []store.CursorDashboardUsageEvent) (int64, string, bool) {
	estimate := summarizeCursorDashboardCost(events)
	if !estimate.hasKnownEstimate || estimate.unpricedTurnCount > 0 {
		return 0, "", false
	}
	return estimate.estimatedUSDMicros, estimate.pricingVersion, true
}

func summarizeCursorDashboardCost(events []store.CursorDashboardUsageEvent) cursorDashboardCostEstimate {
	estimate := cursorDashboardCostEstimate{
		unpricedReasonCount: make(map[pricing.CostReason]int64),
	}
	numerator := new(big.Int)
	hasPriceableEvent := false
	for _, event := range events {
		reason, rate := cursorDashboardEventPrice(event)
		if reason != pricing.CostReasonPriced {
			estimate.unpricedTurnCount += event.OccurrenceCount
			estimate.unpricedReasonCount[reason] += event.OccurrenceCount
			continue
		}
		hasPriceableEvent = true
		estimate.pricedTurnCount += event.OccurrenceCount
		occurrences := big.NewInt(event.OccurrenceCount)
		addCursorCostComponent(numerator, event.InputTokens, rate.InputMicros, occurrences)
		if rate.CacheWriteMicros != nil {
			addCursorCostComponent(numerator, event.CacheWriteTokens, *rate.CacheWriteMicros, occurrences)
		}
		addCursorCostComponent(numerator, event.CacheReadTokens, rate.CacheReadMicros, occurrences)
		addCursorCostComponent(numerator, event.OutputTokens, rate.OutputMicros, occurrences)
	}
	if len(events) > 0 && !hasPriceableEvent {
		return estimate
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, big.NewInt(cursorTokensPerMillion), remainder)
	if remainder.Cmp(big.NewInt(cursorTokensPerMillion/2)) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return estimate
	}
	estimate.estimatedUSDMicros = quotient.Int64()
	estimate.pricingVersion = cursorPricingVersion
	estimate.hasKnownEstimate = true
	return estimate
}

func cursorDashboardEventPrice(
	event store.CursorDashboardUsageEvent,
) (pricing.CostReason, pricing.CursorModelRate) {
	if !event.TokenBased {
		return pricing.CostReasonMissingToken, pricing.CursorModelRate{}
	}
	if event.ModelKey == nil {
		return pricing.CostReasonMissingModel, pricing.CursorModelRate{}
	}
	rate, ok := pricing.CursorRateForModel(*event.ModelKey, event.OccurredAtMS)
	if !ok {
		return pricing.CostReasonModelNotListed, pricing.CursorModelRate{}
	}
	if event.CacheWriteTokens > 0 && rate.CacheWriteMicros == nil {
		return pricing.CostReasonMissingPriceComponent, pricing.CursorModelRate{}
	}
	return pricing.CostReasonPriced, rate
}

func addCursorCostComponent(total *big.Int, tokens, rate int64, occurrences *big.Int) {
	if tokens == 0 || rate == 0 {
		return
	}
	component := new(big.Int).Mul(big.NewInt(tokens), big.NewInt(rate))
	component.Mul(component, occurrences)
	total.Add(total, component)
}
