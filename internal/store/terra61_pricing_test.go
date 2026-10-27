package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestGPT61TerraCatalogUpgradePricesExistingTokensWithoutRescan(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repository := lightIndexRepositoryFixture(t)
	versions := pricing.BuiltinOpenAICatalog()
	for _, catalog := range versions[:8] {
		if err := repository.AddPricingVersion(ctx, catalog); err != nil {
			t.Fatal(err)
		}
	}
	model := "gpt-6.1-terra"
	at := pricing.BuiltinOpenAI20261026().EffectiveFromMS
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
	source := ReportingSource{HomeID: "synthetic-home", Path: identity.Home.Path, DeviceID: identity.Home.DeviceID, Inode: identity.Home.Inode}
	exportedBefore, err := repository.coreTestRepository.Repository.ReportingPage(ctx, "codex", source, "")
	if err != nil || len(exportedBefore.Sessions) != 1 || len(exportedBefore.Sessions[0].Contributions) != 1 {
		t.Fatalf("before-upgrade export: %#v, %v", exportedBefore, err)
	}
	unpriced := exportedBefore.Sessions[0].Contributions[0]
	if unpriced.CostStatus != "unpriced" || unpriced.Rates != nil {
		t.Fatal("Terra prematurely priced", unpriced)
	}
	scanBefore, err := repository.ActiveLightTokenScan(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repository.AddPricingVersion(ctx, versions[8]); err != nil {
			t.Fatal(err)
		}
	}
	after, err := repository.UsageCostRange(ctx, filter)
	if err != nil || len(after.Models) != 1 {
		t.Fatalf("after upgrade = %#v, %v", after, err)
	}
	row := after.Models[0]
	// 800k uncached + 200k cached + 150k output/reasoning = $1.56.
	if row.EstimatedUSDMicros == nil || *row.EstimatedUSDMicros != 1_560_000 ||
		row.TotalTokens == nil || *row.TotalTokens != 1_150_000 ||
		row.ModelDisplayName == nil || *row.ModelDisplayName != "GPT-6.1 Terra" {
		t.Fatalf("6.1 Terra model = %#v", row)
	}
	exportedAfter, err := repository.coreTestRepository.Repository.ReportingPage(ctx, "codex", source, "")
	if err != nil || len(exportedAfter.Sessions) != 1 || len(exportedAfter.Sessions[0].Contributions) != 1 {
		t.Fatalf("after-upgrade export: %#v, %v", exportedAfter, err)
	}
	priced := exportedAfter.Sessions[0].Contributions[0]
	if priced.ID != unpriced.ID || priced.CostStatus != "known" || priced.Rates == nil || priced.PricingVersion == nil || *priced.PricingVersion != versions[8].PricingVersion ||
		*priced.Rates.InputMicroUSD != 1_000_000 || *priced.Rates.CachedMicroUSD != 50_000 || *priced.Rates.OutputMicroUSD != 5_000_000 {
		t.Fatal("catalog upgrade changed contribution identity or lost exact rates", priced)
	}
	scanAfter, err := repository.ActiveLightTokenScan(ctx, "one")
	if err != nil || !reflect.DeepEqual(scanBefore, scanAfter) {
		t.Fatal("catalog upgrade changed token checkpoint")
	}
	session, err := repository.SessionAnalytics(ctx, SessionAnalyticsDetailFilter{SessionID: "one", TurnLimit: 50})
	if err != nil || session.Record.Rollup == nil || session.Record.Rollup.EstimatedUSDMicros == nil ||
		*session.Record.Rollup.EstimatedUSDMicros != 1_560_000 ||
		session.Record.Rollup.TotalTokens == nil || *session.Record.Rollup.TotalTokens != 1_150_000 {
		t.Fatalf("session cost/tokens = %#v, %v", session, err)
	}
	projects, err := repository.ListProjectAnalytics(ctx, ProjectAnalyticsFilter{
		Range: filter, Limit: 20, SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || len(projects.Records) != 1 || projects.Records[0].Totals.EstimatedUSDMicros == nil ||
		*projects.Records[0].Totals.EstimatedUSDMicros != 1_560_000 {
		t.Fatalf("project cost = %#v, %v", projects, err)
	}
	project, err := repository.ProjectAnalytics(ctx, ProjectAnalyticsDetailFilter{
		Range: filter, DimensionKey: projects.Records[0].DimensionKey, SessionLimit: 20, ModelLimit: 20,
	})
	if err != nil || len(project.Models) != 1 || project.Models[0].DimensionKey != model ||
		project.Models[0].Totals.EstimatedUSDMicros == nil || *project.Models[0].Totals.EstimatedUSDMicros != 1_560_000 ||
		project.Models[0].Totals.TotalTokens == nil || *project.Models[0].Totals.TotalTokens != 1_150_000 ||
		len(project.Sessions) != 1 || project.Sessions[0].SessionID != "one" {
		t.Fatalf("project model/session reconciliation = %#v, %v", project, err)
	}
}

func TestGPT61TerraExactPricingBoundaries(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repository := openRuntimeRepository(t)
	for _, catalog := range pricing.BuiltinOpenAICatalog() {
		if err := repository.AddPricingVersion(ctx, catalog); err != nil {
			t.Fatal(err)
		}
	}
	at := pricing.BuiltinOpenAI20261026().EffectiveFromMS
	for _, tc := range []struct {
		model  string
		at     int64
		cached int64
	}{
		{"gpt-6.1-terra", at - 1, -1},
		{"gpt-6.1-terra", at, 50_000},
		{"gpt-6.1-terra", at + 1, 50_000},
		{"gpt-6-sol", at - 1, 200_000},
		{"gpt-6-sol", at, 200_000},
		{"gpt-6.1", at, -1},
		{"gpt-6.1-terra-future", at, -1},
		{"gpt-6.1-terra-2026-10-26", at, -1},
	} {
		got, err := repository.PricingForModelAt(ctx, "openai-api", "USD", tc.model, tc.at)
		if tc.cached < 0 {
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("%s at %d must remain unpriced: %#v, %v", tc.model, tc.at, got, err)
			}
			continue
		}
		input, output := int64(1_000_000), int64(5_000_000)
		if tc.model == "gpt-6-sol" {
			input, output = 2_000_000, 10_000_000
		}
		if err != nil || got.Matched.InputMicrosPerMillion == nil || *got.Matched.InputMicrosPerMillion != input ||
			got.Matched.CachedInputMicrosPerMillion == nil || *got.Matched.CachedInputMicrosPerMillion != tc.cached ||
			got.Matched.OutputMicrosPerMillion == nil || *got.Matched.OutputMicrosPerMillion != output {
			t.Fatalf("%s at %d = %#v, %v", tc.model, tc.at, got, err)
		}
	}
}
