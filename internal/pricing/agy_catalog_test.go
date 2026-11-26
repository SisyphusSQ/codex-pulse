package pricing

import (
	"testing"
	"time"
)

func TestAGYRateAtResolvesPlaceholdersAndPhases(t *testing.T) {
	t.Parallel()

	beforeCutoff := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC).UnixMilli()
	afterCutoff := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

	rateBefore, ok := AGYRateAt("MODEL_PLACEHOLDER_M429", beforeCutoff)
	if !ok {
		t.Fatalf("expected MODEL_PLACEHOLDER_M429 to resolve")
	}
	if rateBefore.ModelID != "gemini-4-carbon-high" ||
		rateBefore.InputMicros != 2_000_000 ||
		rateBefore.CachedMicros != 100_000 ||
		rateBefore.OutputMicros != 10_000_000 ||
		rateBefore.PricingVersion() != AGYPriceVersion+":phase1" {
		t.Fatalf("unexpected phase1 rate: %+v", rateBefore)
	}

	rateAfter, ok := AGYRateAt("MODEL_PLACEHOLDER_M429", afterCutoff)
	if !ok {
		t.Fatalf("expected MODEL_PLACEHOLDER_M429 phase2 to resolve")
	}
	if rateAfter.InputMicros != 4_000_000 ||
		rateAfter.CachedMicros != 200_000 ||
		rateAfter.OutputMicros != 20_000_000 ||
		rateAfter.PricingVersion() != AGYPriceVersion+":phase2" {
		t.Fatalf("unexpected phase2 rate: %+v", rateAfter)
	}

	claudeRate, ok := AGYRateAt("MODEL_PLACEHOLDER_M402", beforeCutoff)
	if !ok || claudeRate.ModelID != "claude-opus-5-5-high" || claudeRate.Family != "Claude" ||
		claudeRate.InputMicros != 4_000_000 || claudeRate.CachedMicros != 200_000 || claudeRate.OutputMicros != 20_000_000 {
		t.Fatalf("unexpected claude rate: ok=%v rate=%+v", ok, claudeRate)
	}

	if _, ok := AGYRateAt("MODEL_PLACEHOLDER_M99999", beforeCutoff); ok {
		t.Fatalf("unknown placeholder must remain unpriced")
	}
	if _, ok := AGYRateAt("unknown-future-model", beforeCutoff); ok {
		t.Fatalf("unknown model must remain unpriced")
	}
}

func TestEstimateAGYCostHalfUpRounding(t *testing.T) {
	t.Parallel()

	rate := AGYRate{
		InputMicros:  1_500_000,
		CachedMicros: 75_000,
		OutputMicros: 7_500_000,
	}
	// 1*1_500_000 + 1*75_000 + 1*7_500_000 = 9_075_000 -> 9.075 -> 9 micros
	cost, ok := EstimateAGYCost(rate, 1, 1, 1)
	if !ok || cost != 9 {
		t.Fatalf("EstimateAGYCost(1,1,1) = %d, %v; want 9, true", cost, ok)
	}
	// Exact half-up boundary: 1 * 500_000 / 1_000_000 = 0.5 -> 1 micro
	halfRate := AGYRate{InputMicros: 500_000}
	cost, ok = EstimateAGYCost(halfRate, 1, 0, 0)
	if !ok || cost != 1 {
		t.Fatalf("EstimateAGYCost(half) = %d, %v; want 1, true", cost, ok)
	}
	if _, ok := EstimateAGYCost(rate, -1, 0, 0); ok {
		t.Fatalf("negative tokens must fail")
	}
}

func TestAGYReferenceRatesAt(t *testing.T) {
	t.Parallel()

	rates := AGYReferenceRatesAt(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC).UnixMilli())
	if len(rates) == 0 {
		t.Fatalf("expected non-empty AGY reference rates")
	}
	for i := 1; i < len(rates); i++ {
		if rates[i-1].ModelID >= rates[i].ModelID {
			t.Fatalf("rates not strictly sorted at %d: %s >= %s", i, rates[i-1].ModelID, rates[i].ModelID)
		}
	}
}
