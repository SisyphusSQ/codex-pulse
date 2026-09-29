package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestGPT61SolCatalogUpgradePricesExistingTokensWithoutRescan(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repository := lightIndexRepositoryFixture(t)
	versions := pricing.BuiltinOpenAICatalog()
	for _, catalog := range versions[:7] {
		if err := repository.AddPricingVersion(ctx, catalog); err != nil {
			t.Fatal(err)
		}
	}
	model := "gpt-6.1-sol"
	at := pricing.BuiltinOpenAI20260929().EffectiveFromMS
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(ctx, "one", identity, "parser-v2", at)
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
	for range 2 {
		if err := repository.AddPricingVersion(ctx, versions[7]); err != nil {
			t.Fatal(err)
		}
	}
	after, err := repository.UsageCostRange(ctx, filter)
	if err != nil || len(after.Models) != 1 {
		t.Fatalf("after upgrade = %#v, %v", after, err)
	}
	row := after.Models[0]
	// 800k uncached + 200k cached + 150k output/reasoning = $3.12.
	if row.EstimatedUSDMicros == nil || *row.EstimatedUSDMicros != 3_120_000 ||
		row.TotalTokens == nil || *row.TotalTokens != 1_150_000 ||
		row.ModelDisplayName == nil || *row.ModelDisplayName != "GPT-6.1 Sol" {
		t.Fatalf("6.1 Sol model = %#v", row)
	}
	scanAfter, err := repository.ActiveLightTokenScan(ctx, "one")
	if err != nil || !reflect.DeepEqual(scanBefore, scanAfter) {
		t.Fatal("catalog upgrade changed token checkpoint")
	}
	session, err := repository.SessionAnalytics(ctx, SessionAnalyticsDetailFilter{SessionID: "one", TurnLimit: 50})
	if err != nil || session.Record.Rollup == nil || session.Record.Rollup.EstimatedUSDMicros == nil ||
		*session.Record.Rollup.EstimatedUSDMicros != 3_120_000 ||
		session.Record.Rollup.TotalTokens == nil || *session.Record.Rollup.TotalTokens != 1_150_000 {
		t.Fatalf("session cost/tokens = %#v, %v", session, err)
	}
	projects, err := repository.ListProjectAnalytics(ctx, ProjectAnalyticsFilter{
		Range: filter, Limit: 20, SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || len(projects.Records) != 1 || projects.Records[0].Totals.EstimatedUSDMicros == nil ||
		*projects.Records[0].Totals.EstimatedUSDMicros != 3_120_000 {
		t.Fatalf("project cost = %#v, %v", projects, err)
	}
	project, err := repository.ProjectAnalytics(ctx, ProjectAnalyticsDetailFilter{
		Range: filter, DimensionKey: projects.Records[0].DimensionKey, SessionLimit: 20, ModelLimit: 20,
	})
	if err != nil || len(project.Models) != 1 || project.Models[0].DimensionKey != model ||
		project.Models[0].Totals.EstimatedUSDMicros == nil || *project.Models[0].Totals.EstimatedUSDMicros != 3_120_000 ||
		project.Models[0].Totals.TotalTokens == nil || *project.Models[0].Totals.TotalTokens != 1_150_000 ||
		len(project.Sessions) != 1 || project.Sessions[0].SessionID != "one" {
		t.Fatalf("project model/session reconciliation = %#v, %v", project, err)
	}
}

func TestGPT61SolExactPricingBoundaries(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repository := openRuntimeRepository(t)
	for _, catalog := range pricing.BuiltinOpenAICatalog() {
		if err := repository.AddPricingVersion(ctx, catalog); err != nil {
			t.Fatal(err)
		}
	}
	at := pricing.BuiltinOpenAI20260929().EffectiveFromMS
	for _, tc := range []struct {
		model  string
		at     int64
		cached int64
	}{
		{"gpt-6.1-sol", at - 1, -1},
		{"gpt-6.1-sol", at, 100_000},
		{"gpt-6.1-sol", at + 1, 100_000},
		{"gpt-6-sol", at - 1, 200_000},
		{"gpt-6-sol", at, 200_000},
		{"gpt-6.1", at, -1},
		{"gpt-6.1-sol-future", at, -1},
		{"gpt-6.1-sol-2026-09-29", at, -1},
	} {
		got, err := repository.PricingForModelAt(ctx, "openai-api", "USD", tc.model, tc.at)
		if tc.cached < 0 {
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("%s at %d must remain unpriced: %#v, %v", tc.model, tc.at, got, err)
			}
			continue
		}
		if err != nil || got.Matched.InputMicrosPerMillion == nil || *got.Matched.InputMicrosPerMillion != 2_000_000 ||
			got.Matched.CachedInputMicrosPerMillion == nil || *got.Matched.CachedInputMicrosPerMillion != tc.cached ||
			got.Matched.OutputMicrosPerMillion == nil || *got.Matched.OutputMicrosPerMillion != 10_000_000 {
			t.Fatalf("%s at %d = %#v, %v", tc.model, tc.at, got, err)
		}
	}
}
