package pricing

import "testing"

func TestGrokCurrentRatesUseExactModelKeysAndPublishedTiers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		model string
		id    string
		input int64
	}{
		{"grok-4.7", "grok-4.7", 2_000_000},
		{"grok-4.7-fast", "grok-4.7 (Fast)", 4_000_000},
		{"grok-build-0.1", "grok-build-0.1", 1_000_000},
		{"grok-4.6-build", "grok-4.6", 2_000_000},
	} {
		rate, ok := GrokRateForModel(tc.model)
		if !ok || rate.ModelID != tc.id || rate.InputMicros != tc.input {
			t.Fatalf("%s = %#v, %t", tc.model, rate, ok)
		}
	}
	for _, unknown := range []string{"grok-4", "grok-4.7-future", "grok-build", "cursor-grok-4.7-xhigh"} {
		if rate, ok := GrokRateForModel(unknown); ok {
			t.Fatalf("%s incorrectly matches %#v", unknown, rate)
		}
	}
	rates := BuiltinGrokModelRates()
	if len(rates) != 18 || rates[1].ModelID != "grok-4.7 (long context ≥200k)" ||
		rates[1].InputMicros != 4_000_000 || rates[3].OutputMicros != 18_000_000 ||
		rates[5].CachedMicros != 400_000 {
		t.Fatalf("published long-context rates = %#v", rates)
	}
}

func TestGrokEstimatePreservesHistoryAndRejectsAmbiguousLongContext(t *testing.T) {
	t.Parallel()
	at := GrokPricingVerifiedAtMS
	if got, ok := EstimateGrokUsageCostAt("grok-4.7", at, 100_000, 20_000, 0, 10_000); !ok || got != 230_000 {
		t.Fatalf("short Grok 4.7 = %d, %t", got, ok)
	}
	if _, ok := EstimateGrokUsageCostAt("grok-4.7", at, 200_000, 0, 0, 1); ok {
		t.Fatal("aggregated 200k input must not assume short or long tier")
	}
	if _, ok := EstimateGrokUsageCostAt("grok-4.7", grok47ReleaseAtMS-1, 1, 0, 0, 1); ok {
		t.Fatal("Grok 4.7 cannot be priced before release")
	}
	if got, ok := EstimateGrokUsageCostAt("grok-4", grok47ReleaseAtMS-1, 1_000_000, 0, 0, 0); !ok || got != 3_000_000 {
		t.Fatalf("legacy Grok 4 price = %d, %t", got, ok)
	}
	if _, ok := EstimateGrokUsageCostAt("grok-4", at, 1, 0, 0, 0); ok {
		t.Fatal("retired Grok 4 must not be presented as a current price")
	}
	if _, ok := EstimateGrokUsageCostAt("grok-code-fast-1", grokLegacyRedirectAtMS, 1, 0, 0, 0); ok {
		t.Fatal("redirected Grok Code alias must not use its former rate")
	}
}
