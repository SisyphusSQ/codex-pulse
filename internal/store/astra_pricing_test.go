package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestAstraCatalogUpgradePricesExistingTokensWithoutRescan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := lightIndexRepositoryFixture(t)
	versions := pricing.BuiltinOpenAICatalog()
	for _, version := range versions[:4] {
		if err := repository.AddPricingVersion(ctx, version); err != nil {
			t.Fatal(err)
		}
	}
	identity := lightRolloutFixture()
	model := "gpt-6-astra"
	at := pricing.BuiltinOpenAI20260903().EffectiveFromMS
	generation, err := repository.StartLightTokenRebuild(ctx, "one", identity, "parser-v2", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitLightTokenBatch(ctx, storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: at + 1, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 1_000_000, CachedInputTokens: 200_000, OutputTokens: 100_000, ReasoningTokens: 50_000,
			CurrentModelKey: &model, CurrentModelSource: attribution.SourceModelCanonical,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: at, ModelKey: &model, ModelSource: attribution.SourceModelCanonical,
			InputTokens: 1_000_000, CachedInputTokens: 200_000, OutputTokens: 100_000, ReasoningTokens: 50_000,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	filter := AnalyticsRange{ReportingTimezone: "UTC", StartAtMS: at, EndAtMS: at + 86_400_000}
	before, err := repository.UsageCostRange(ctx, filter)
	if err != nil || len(before.Models) != 1 || before.Models[0].EstimatedUSDMicros != nil {
		t.Fatalf("before upgrade = %#v, %v", before, err)
	}
	scanBefore, err := repository.ActiveLightTokenScan(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range versions[4:] {
		for range 2 {
			if err := repository.AddPricingVersion(ctx, version); err != nil {
				t.Fatal(err)
			}
		}
	}
	after, err := repository.UsageCostRange(ctx, filter)
	if err != nil || len(after.Models) != 1 {
		t.Fatalf("after upgrade = %#v, %v", after, err)
	}
	row := after.Models[0]
	// 800k uncached + 200k cached + 150k output/reasoning = $15.70.
	if row.EstimatedUSDMicros == nil || *row.EstimatedUSDMicros != 15_700_000 ||
		row.TotalTokens == nil || *row.TotalTokens != 1_150_000 ||
		row.ModelDisplayName == nil || *row.ModelDisplayName != "GPT-6 Astra" {
		t.Fatalf("Astra model = %#v", row)
	}
	scanAfter, err := repository.ActiveLightTokenScan(ctx, "one")
	if err != nil || !reflect.DeepEqual(scanBefore, scanAfter) {
		t.Fatal("catalog upgrade changed token checkpoint")
	}
	detail, err := repository.SessionAnalytics(ctx, SessionAnalyticsDetailFilter{SessionID: "one", TurnLimit: 50})
	if err != nil || detail.Record.Rollup == nil || detail.Record.Rollup.EstimatedUSDMicros == nil ||
		*detail.Record.Rollup.EstimatedUSDMicros != 15_700_000 {
		t.Fatalf("session cost = %#v, %v", detail, err)
	}
}

func TestAstraAndSolExactPricingEffectiveBoundaries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := openRuntimeRepository(t)
	for _, version := range pricing.BuiltinOpenAICatalog() {
		if err := repository.AddPricingVersion(ctx, version); err != nil {
			t.Fatal(err)
		}
	}
	astraAt := pricing.BuiltinOpenAI20260903().EffectiveFromMS
	solAt := pricing.BuiltinOpenAI20260905().EffectiveFromMS
	for _, tc := range []struct {
		model     string
		at, input int64
	}{
		{"gpt-6-astra", astraAt - 1, -1}, {"gpt-6-astra", astraAt, 10_000_000},
		{"gpt-6-astra", solAt, 10_000_000}, {"gpt-6", solAt, -1},
		{"gpt-6-astra-future", solAt, -1},
		{"gpt-5.6-sol", solAt - 1, 5_000_000}, {"gpt-5.6-sol", solAt, 4_000_000},
		{"gpt-5.6", solAt - 1, 5_000_000}, {"gpt-5.6", solAt, 4_000_000},
	} {
		got, err := repository.PricingForModelAt(ctx, "openai-api", "USD", tc.model, tc.at)
		if tc.input < 0 {
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("%s at %d must be unpriced: %#v %v", tc.model, tc.at, got, err)
			}
			continue
		}
		if err != nil || got.Matched.InputMicrosPerMillion == nil || *got.Matched.InputMicrosPerMillion != tc.input {
			t.Fatalf("%s at %d: %#v %v", tc.model, tc.at, got, err)
		}
	}
}
