package pricing

import "testing"

func TestCursorCurrentCatalogIncludesExpandedOfficialRows(t *testing.T) {
	t.Parallel()
	rates := BuiltinCursorModelRates()
	if len(rates) < 45 {
		t.Fatalf("published Cursor rows = %d, want expanded list", len(rates))
	}
	byID := make(map[string]CursorModelRate, len(rates))
	for _, rate := range rates {
		if _, duplicate := byID[rate.ModelID]; duplicate {
			t.Fatalf("duplicate Cursor model %q", rate.ModelID)
		}
		byID[rate.ModelID] = rate
	}
	for id, want := range map[string][3]int64{
		"Grok 4.7":             {2_000_000, 500_000, 6_000_000},
		"Grok 4.7 500k (Fast)": {6_000_000, 1_500_000, 18_000_000},
		"Claude Fable 5.1":     {10_000_000, 250_000, 50_000_000},
		"Claude Opus 5.5":      {4_000_000, 200_000, 20_000_000},
		"Gemini 3.8 Flash":     {750_000, 75_000, 3_500_000},
		"Muse Spark 1.3":       {1_250_000, 150_000, 4_250_000},
		"GLM 5.2":              {1_400_000, 260_000, 4_400_000},
		"Kimi K3":              {3_000_000, 300_000, 15_000_000},
	} {
		rate, ok := byID[id]
		if !ok || [3]int64{rate.InputMicros, rate.CacheReadMicros, rate.OutputMicros} != want {
			t.Fatalf("%s = %#v, want %#v", id, rate, want)
		}
	}
}

func TestCursorPricingUsesEventTimeAndExactModelIdentity(t *testing.T) {
	t.Parallel()
	before := CursorPricingVerifiedAtMS - 1
	after := CursorPricingVerifiedAtMS
	for _, tc := range []struct {
		model        string
		occurredAtMS int64
		wantModel    string
		wantInput    int64
		wantOutput   int64
	}{
		{"gpt-5.6-sol", before, "GPT-5.6 Sol", 5_000_000, 30_000_000},
		{"gpt-5.6-sol", after, "GPT-5.6 Sol", 4_000_000, 20_000_000},
		{"grok-4.5-fast", before, "Grok 4.5 (Fast)", 4_000_000, 12_000_000},
		{"grok-4.5-fast", after, "Grok 4.5 (Fast)", 4_000_000, 18_000_000},
		{"grok-4.7", cursorGrok47ReleaseAtMS, "Grok 4.7", 2_000_000, 6_000_000},
		{"claude-opus-5-5", after, "Claude Opus 5.5", 4_000_000, 20_000_000},
		{"cursor-claude-opus-5-5-fast", after, "Claude Opus 5.5 (Fast)", 8_000_000, 40_000_000},
		{"claude-opus-5", after, "Claude Opus 5", 5_000_000, 25_000_000},
	} {
		rate, ok := CursorRateForModel(tc.model, tc.occurredAtMS)
		if !ok || rate.ModelID != tc.wantModel || rate.InputMicros != tc.wantInput ||
			rate.OutputMicros != tc.wantOutput {
			t.Fatalf("%s at %d = %#v, %t", tc.model, tc.occurredAtMS, rate, ok)
		}
	}
	for _, tc := range []struct {
		model string
		at    int64
	}{
		{"grok-4.7", cursorGrok47ReleaseAtMS - 1},
		{"claude-opus-5-5", before},
		{"future-grok-4.7", after},
		{"grok-4-7-future", after},
	} {
		if rate, ok := CursorRateForModel(tc.model, tc.at); ok {
			t.Fatalf("%s at %d unexpectedly matches %#v", tc.model, tc.at, rate)
		}
	}
	if pool := CursorUsagePoolForModel("cursor-grok-4.7-xhigh-fast", cursorGrok47ReleaseAtMS); pool != CursorUsagePoolModels {
		t.Fatalf("Grok 4.7 Fast pool = %q", pool)
	}
}

func TestCursorGrok47UsesPromptLengthFor500kTier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		model      string
		input      int64
		cacheRead  int64
		wantModel  string
		wantOutput int64
	}{
		{"grok-4.7-500k", 100_000, 0, "Grok 4.7", 6_000_000},
		{"grok-4.7", 200_000, 56_000, "Grok 4.7", 6_000_000},
		{"grok-4.7", 200_000, 56_001, "Grok 4.7 500k", 12_000_000},
		{"grok-4.7-500k-fast", 50_000, 0, "Grok 4.7 (Fast)", 12_000_000},
		{"grok-4.7-fast", 200_000, 56_001, "Grok 4.7 500k (Fast)", 18_000_000},
	} {
		rate, ok := CursorRateForUsage(tc.model, CursorPricingVerifiedAtMS, tc.input, 0, tc.cacheRead)
		if !ok || rate.ModelID != tc.wantModel || rate.OutputMicros != tc.wantOutput {
			t.Fatalf("%s input=%d cached=%d: %#v, %t", tc.model, tc.input, tc.cacheRead, rate, ok)
		}
	}
}

func TestCursorNewThirdPartyFamiliesRemainInOtherModelsBeforePriceVerification(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"glm-5-2", "kimi-k3", "muse-spark-1-3"} {
		if pool := CursorUsagePoolForModel(model, CursorPricingVerifiedAtMS-1); pool != CursorUsagePoolOtherModels {
			t.Fatalf("%s pool = %q", model, pool)
		}
		if _, ok := CursorRateForModel(model, CursorPricingVerifiedAtMS-1); ok {
			t.Fatalf("%s price before verification must remain unknown", model)
		}
	}
}
