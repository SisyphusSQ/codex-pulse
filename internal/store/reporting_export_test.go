package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestReportingExportKeepsReasoningPricingAndExcludesLocalPaths(t *testing.T) {
	repo := lightIndexRepositoryFixture(t)
	ctx := t.Context()
	for _, c := range pricing.BuiltinOpenAICatalog() {
		if err := repo.AddPricingVersion(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	identity := lightRolloutFixture()
	model := "gpt-6.1-sol"
	at := pricing.BuiltinOpenAI20260929().EffectiveFromMS
	generation, err := repo.StartLightTokenRebuild(ctx, "one", identity, "parser-v1", at)
	if err != nil {
		t.Fatal(err)
	}
	batch := storelight.LightTokenBatch{SessionID: "one", Generation: generation, UpdatedAtMS: at + 1, Activate: true, Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 10, CachedInputTokens: 5, OutputTokens: 2, ReasoningTokens: 3}, TimedDeltas: []storelight.LightTokenTimedDelta{{SourceOffset: 4000, ObservedAtMS: at, ModelKey: &model, ModelSource: attribution.SourceModelCanonical, InputTokens: 10, OutputTokens: 2, ReasoningTokens: 3}, {SourceOffset: 6000, ObservedAtMS: at + 1, ModelKey: &model, ModelSource: attribution.SourceModelCanonical, CachedInputTokens: 5}}, InvocationDeltas: []storelight.LightInvocationDelta{{SourceOffset: 5000, Ordinal: 0, ObservedAtMS: at, Kind: "tool", Name: "exec_command", Source: "response_function", Outcome: "succeeded"}}}
	if err := repo.CommitLightTokenBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	source := ReportingSource{HomeID: "private-home", Path: identity.Home.Path, DeviceID: identity.Home.DeviceID, Inode: identity.Home.Inode}
	page, err := repo.coreTestRepository.Repository.ReportingPage(ctx, "codex", source, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 {
		t.Fatal("session missing")
	}
	s := page.Sessions[0]
	if s.ProjectName != "workspace" || s.CreatedAtMS == nil || s.CollectedAtMS != at+1 || len(s.Invocations) != 1 || len(s.Contributions) != 2 {
		t.Fatalf("snapshot: %+v", s)
	}
	if *s.Contributions[0].TotalTokens != 15 || s.Contributions[0].Rates == nil || s.Contributions[0].PricingMode != "codex_model_sum" || *s.Contributions[1].CachedTokens != 5 || *s.Contributions[1].InputTokens != 0 {
		t.Fatal("counter or pricing semantics changed")
	}
	s.Revision = 1
	if err := (reportingv1.Batch{Version: 1, ID: "00000000-0000-0000-0000-000000000001", Sessions: []reportingv1.SessionSnapshot{s}}).Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(s)
	for _, forbidden := range []string{"/workspace", "/confirmed-home", "rollout_path", "source_offset", "fingerprint_sha256", "auth.json"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("local data escaped whitelist")
		}
	}
	coverage, err := repo.coreTestRepository.Repository.ReportingStatus(ctx, "codex", source)
	if err != nil || coverage.CoverageStartMS == nil || *coverage.CoverageStartMS != at || coverage.CoverageEndMS == nil || *coverage.CoverageEndMS != at+1 {
		t.Fatal("coverage absent or receiving time substituted")
	}
	source.Inode++
	if _, err := repo.coreTestRepository.Repository.ReportingPage(ctx, "codex", source, ""); !errors.Is(err, ErrReportingSource) {
		t.Fatal("Home fence ignored")
	}
	// Index generation/offset change leaves stable structured contribution identities.
	original := s.Contributions[0].ID
	identity.FingerprintSHA256 = strings.Repeat("c", 64)
	generation, err = repo.StartLightTokenRebuild(ctx, "one", identity, "parser-v2", at+2)
	if err != nil {
		t.Fatal(err)
	}
	batch.Generation = generation
	batch.UpdatedAtMS = at + 3
	batch.TimedDeltas[0].SourceOffset = 4500
	if err := repo.CommitLightTokenBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	source.Inode--
	rebuilt, err := repo.coreTestRepository.Repository.ReportingPage(ctx, "codex", source, "")
	if err != nil || rebuilt.Sessions[0].Contributions[0].ID != original {
		t.Fatal("rebuild changed contribution identity")
	}
}
func TestReportingCursorSeparatesBillingCyclesAndUnknownSessionFacts(t *testing.T) {
	repo := openRuntimeRepository(t)
	ctx := t.Context()
	if err := repo.ReplaceCursorSnapshot(ctx, CursorSnapshot{Generation: 1, CollectedAtMS: 2000}); err != nil {
		t.Fatal(err)
	}
	snapshot := CursorDashboardSnapshot{Generation: 1, CollectedAtMS: 2000, WindowStartMS: 1000, WindowEndMS: 2000, BillingCycleEndMS: 3000, Events: []CursorDashboardUsageEvent{{EventFingerprint: strings.Repeat("a", 64), OccurrenceCount: 2, OccurredAtMS: 1500, TokenBased: true, InputTokens: 3, OutputTokens: 4, CacheReadTokens: 1, CacheWriteTokens: 2, ReportedChargeMicros: 5}}}
	if err := repo.CommitCursorDashboardSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	partition, err := repo.ReportingPartition(ctx, "cursor")
	if err != nil || partition != "dashboard-1000" {
		t.Fatal("cycle partition")
	}
	source := ReportingSource{HomeID: "cycle-key", Partition: partition}
	page, err := repo.ReportingPage(ctx, "cursor", source, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].SessionKind != "unassigned_usage" || page.Sessions[0].CreatedAtMS != nil {
		t.Fatal("unknown session masqueraded as real session")
	}
	c := page.Sessions[0].Contributions[0]
	if *c.TotalTokens != 20 || *c.CacheWriteTokens != 4 || *c.ReportedChargeMicroUSD != 10 || c.CostMicroUSD != nil {
		t.Fatal("occurrences or reported/estimated costs conflated")
	}
	snapshot.Generation = 2
	snapshot.WindowStartMS = 3000
	snapshot.WindowEndMS = 4000
	snapshot.BillingCycleEndMS = 5000
	snapshot.CollectedAtMS = 4000
	snapshot.Events = nil
	if err := repo.CommitCursorDashboardSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReportingPage(ctx, "cursor", source, ""); !errors.Is(err, ErrReportingSource) {
		t.Fatal("cycle switch used old partition")
	}
}
