package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
)

func TestReportingAGYExportPricingCapsulesHistoryFilterAndStatus(t *testing.T) {
	repo := openRuntimeRepository(t)
	ctx := t.Context()

	phase1At := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC).UnixMilli()
	phase2At := time.Date(2026, 12, 2, 8, 0, 0, 0, time.UTC).UnixMilli()
	projectKey := reportingv1.Key("agy", "workspace")
	turnKey1 := reportingv1.Key("agy", "agy-cascade-1", "turn", "0")
	turnKey2 := reportingv1.Key("agy", "agy-cascade-1", "turn", "1")
	ev1Start, ev1End := phase1At-2000, phase1At
	ev2Start, ev2End := phase2At-4000, phase2At

	capsule := &reportingv1.ThroughputCapsule{
		Version: 1,
		Basis:   "closed_turn_lifetime_output",
		Measures: reportingv1.ThroughputMeasures{
			OutputTokens:     new(int64(300)),
			ActiveDurationMS: new(int64(6000)),
			IncludedTurns:    2,
			CoverageKnown:    true,
			Status:           "complete",
			DurationSource:   "step_metadata",
		},
		TurnsTotal: 2,
		RecentTurns: []reportingv1.ThroughputTurn{
			{
				Key:         turnKey1,
				StartedAtMS: &ev1Start,
				EndedAtMS:   &ev1End,
				Measures: reportingv1.ThroughputMeasures{
					OutputTokens:     new(int64(100)),
					ActiveDurationMS: new(int64(2000)),
					IncludedTurns:    1,
					CoverageKnown:    true,
					Status:           "complete",
					DurationSource:   "step_metadata",
				},
			},
			{
				Key:         turnKey2,
				StartedAtMS: &ev2Start,
				EndedAtMS:   &ev2End,
				Measures: reportingv1.ThroughputMeasures{
					OutputTokens:     new(int64(200)),
					ActiveDurationMS: new(int64(4000)),
					IncludedTurns:    1,
					CoverageKnown:    true,
					Status:           "complete",
					DurationSource:   "step_metadata",
				},
			},
		},
	}

	err := repo.ReplaceAGYSnapshot(ctx, AGYSnapshot{
		Generation:    1,
		CollectedAtMS: phase2At + 1000,
		Sources: []CursorSourceStatus{
			{Provider: "agy", SourceKey: "agy.conversation_summaries", SourceType: "sqlite", State: "available", CoverageState: "exact", CheckpointKind: "snapshot", LastAttemptAtMS: phase2At, LastSuccessAtMS: &phase2At, UpdatedAtMS: phase2At},
			{Provider: "agy", SourceKey: "agy.conversation_steps", SourceType: "sqlite", State: "available", CoverageState: "exact", CheckpointKind: "snapshot", LastAttemptAtMS: phase2At, LastSuccessAtMS: &phase2At, UpdatedAtMS: phase2At},
			{Provider: "agy", SourceKey: "agy.transcript_fallback", SourceType: "jsonl", State: "not_configured", CoverageState: "unknown", CheckpointKind: "filesystem_scan", LastAttemptAtMS: phase2At, UpdatedAtMS: phase2At},
		},
		Sessions: []AGYSession{{
			ExternalSessionID:  "agy-cascade-1",
			DisplayTitle:       "AGY 合成会话",
			TitleSource:        "agy_summary",
			SessionRole:        "main",
			ProjectKey:         projectKey,
			ProjectDisplayName: "workspace",
			ProjectSource:      "workspace",
			CreatedAtMS:        phase1At - 2000,
			LastActivityAtMS:   phase2At,
			ModelKey:           new("gemini-4-carbon"),
			RequestCount:       3,
			CoverageState:      "exact",
			Throughput:         capsule,
			UpdatedAtMS:        phase2At,
		}},
		UsageEvents: []AGYUsageEvent{
			{
				EventID:             reportingv1.Key("agy-ev", "1"),
				ExternalSessionID:   "agy-cascade-1",
				StepIndex:           0,
				OccurredAtMS:        phase1At,
				ModelKey:            new("gemini-4-carbon"),
				UncachedInputTokens: 1000,
				CacheReadTokens:     500,
				OutputTokens:        100,
				ReasoningTokens:     40,
				TotalTokens:         1600,
				CacheReadKnown:      true,
				ReasoningKnown:      true,
				TotalKnown:          true,
				StartedAtMS:         &ev1Start,
				EndedAtMS:           &ev1End,
				Provenance:          "agy_step_metadata",
				UpdatedAtMS:         phase1At,
			},
			{
				EventID:             reportingv1.Key("agy-ev", "2"),
				ExternalSessionID:   "agy-cascade-1",
				StepIndex:           1,
				OccurredAtMS:        phase2At,
				ModelKey:            new("gemini-4-carbon"),
				UncachedInputTokens: 1000,
				CacheReadTokens:     500,
				OutputTokens:        200,
				ReasoningTokens:     80,
				TotalTokens:         1700,
				CacheReadKnown:      true,
				ReasoningKnown:      true,
				TotalKnown:          true,
				StartedAtMS:         &ev2Start,
				EndedAtMS:           &ev2End,
				Provenance:          "agy_step_metadata",
				UpdatedAtMS:         phase2At,
			},
			{
				EventID:             reportingv1.Key("agy-ev", "3"),
				ExternalSessionID:   "agy-cascade-1",
				StepIndex:           2,
				OccurredAtMS:        phase2At + 500,
				ModelKey:            new("unpublished-agy-model"),
				UncachedInputTokens: 200,
				CacheReadTokens:     100,
				OutputTokens:        50,
				TotalTokens:         350,
				CacheReadKnown:      true,
				TotalKnown:          true,
				Provenance:          "agy_step_metadata",
				UpdatedAtMS:         phase2At + 500,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Mark agy.ls_rpc unavailable to verify it does not degrade local trajectory authority/status.
	if err := repo.UpsertAGYSourceStatus(ctx, CursorSourceStatus{
		Provider: "agy", SourceKey: "agy.ls_rpc", SourceType: "ls_rpc",
		State: "unavailable", CoverageState: "unknown", CheckpointKind: "snapshot",
		LastAttemptAtMS: phase2At, UpdatedAtMS: phase2At,
	}); err != nil {
		t.Fatal(err)
	}

	source := ReportingSource{HomeID: "home-agy", Partition: "local"}
	page, err := repo.ReportingPage(ctx, "agy", source, "")
	if err != nil {
		t.Fatal(err)
	}
	if !page.Authority || len(page.Sessions) != 1 {
		t.Fatalf("expected authority=true and 1 session, got authority=%v sessions=%d", page.Authority, len(page.Sessions))
	}
	snap := page.Sessions[0]
	if snap.Provider != "agy" || snap.SourceKind != "agy_local" || !snap.Complete || len(snap.Contributions) != 3 {
		t.Fatalf("unexpected AGY snapshot: %+v", snap)
	}
	if snap.CacheUsage == nil || snap.CacheUsage.InputTokens == nil || *snap.CacheUsage.InputTokens != 3300 || *snap.CacheUsage.CachedInputTokens != 1100 {
		t.Fatalf("unexpected AGY cache usage: %+v", snap.CacheUsage)
	}
	if snap.Throughput == nil || snap.Throughput.Measures.DurationSource != "step_metadata" {
		t.Fatalf("unexpected AGY throughput: %+v", snap.Throughput)
	}
	// Verify phase1 vs phase2 event_cost matches pricing.AGYRateAt + EstimateAGYCost.
	r1, _ := pricing.AGYRateAt("gemini-4-carbon", ev1Start)
	wantCost1, _ := pricing.EstimateAGYCost(r1, 1000, 500, 100)
	r2, _ := pricing.AGYRateAt("gemini-4-carbon", ev2Start)
	wantCost2, _ := pricing.EstimateAGYCost(r2, 1000, 500, 200)
	if snap.Contributions[0].CostMicroUSD == nil || *snap.Contributions[0].CostMicroUSD != wantCost1 || *snap.Contributions[0].PricingVersion != "agy-api-2026-11-25:phase1" {
		t.Fatalf("phase1 pricing mismatch: got=%+v want=%d", snap.Contributions[0], wantCost1)
	}
	if snap.Contributions[1].CostMicroUSD == nil || *snap.Contributions[1].CostMicroUSD != wantCost2 || *snap.Contributions[1].PricingVersion != "agy-api-2026-11-25:phase2" {
		t.Fatalf("phase2 pricing mismatch: got=%+v want=%d", snap.Contributions[1], wantCost2)
	}
	if snap.Contributions[2].CostStatus != "unpriced" || snap.Contributions[2].CostMicroUSD != nil {
		t.Fatalf("unknown AGY model must stay unpriced: %+v", snap.Contributions[2])
	}
	snap.Revision = 1
	batch := reportingv1.Batch{Version: 1, ID: "00000000-0000-0000-0000-000000000010", Sessions: []reportingv1.SessionSnapshot{snap}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("exported AGY batch failed validation: %v", err)
	}

	// HistoryStartAtMS > 0 must redact lifetime capsules to history_filtered.
	source.StartAtMS = phase2At
	filtered, err := repo.ReportingPage(ctx, "agy", source, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Sessions) != 1 || len(filtered.Sessions[0].Contributions) != 2 {
		t.Fatalf("expected 2 filtered contributions, got %+v", filtered.Sessions)
	}
	if filtered.Sessions[0].CacheUsage.Reason != "history_filtered" || filtered.Sessions[0].Throughput.Measures.Reason != "history_filtered" {
		t.Fatalf("expected history_filtered capsules, got cache=%+v throughput=%+v", filtered.Sessions[0].CacheUsage, filtered.Sessions[0].Throughput)
	}

	// ReportingStatus ignores agy.ls_rpc and not_configured fallback.
	st, err := repo.ReportingStatus(ctx, "agy", ReportingSource{HomeID: "home-agy", Partition: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "ready" || st.CoverageStartMS == nil || *st.CoverageStartMS != phase1At || st.CoverageEndMS == nil || *st.CoverageEndMS != phase2At+500 {
		t.Fatalf("unexpected AGY ReportingStatus: %+v", st)
	}
}

func TestReportingAGYQuotaObservationsPaginationAndSnapshotFallback(t *testing.T) {
	repo := openRuntimeRepository(t)
	ctx := t.Context()

	scope := reportingv1.Key("agy-account", "synthetic-user-digest")
	at := int64(1784000000000)
	reset5h := at + 300*60000
	resetWeekly := at + 10080*60000
	plan := "Google AI Ultra 20×"

	// Snapshot-only fallback before observations are appended.
	err := repo.ReplaceAGYQuotaSnapshots(ctx, scope, []AGYQuotaSnapshot{
		{AccountScope: scope, Generation: at, PlanType: &plan, LimitID: "agy.gemini", LimitName: "Gemini", WindowKind: "primary", WindowMinutes: 300, UsedPercent: new(int64(20)), ResetsAtMS: &reset5h, DataAsOfMS: at, UpdatedAtMS: at},
		{AccountScope: scope, Generation: at, PlanType: &plan, LimitID: "agy.claude_gpt", LimitName: "Claude & GPT", WindowKind: "secondary", WindowMinutes: 10080, UsedPercent: new(int64(5)), ResetsAtMS: &resetWeekly, DataAsOfMS: at, UpdatedAtMS: at},
	})
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := repo.ReportingAGYAccounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].ID != scope || accounts[0].Email != nil || accounts[0].Plan == nil || *accounts[0].Plan != plan {
		t.Fatalf("unexpected AGY accounts: %+v err=%v", accounts, err)
	}
	fallback, err := repo.ReportingQuotaPage(ctx, "agy", "", 0)
	if err != nil || !fallback.Done || len(fallback.Batch.Quotas) != 2 {
		t.Fatalf("expected 2 fallback AGY quota snapshots, got %+v err=%v", fallback, err)
	}
	fallbackByLimit := map[string]reportingv1.QuotaObservation{}
	for _, q := range fallback.Batch.Quotas {
		fallbackByLimit[q.LimitID] = q
	}
	if q := fallbackByLimit["agy.gemini"]; q.Source != "agy_ls_rpc" || q.WindowMinutes == nil || *q.WindowMinutes != 300 || q.WindowKind != "primary" {
		t.Fatalf("unexpected AGY gemini fallback quota: %+v", q)
	}
	if q := fallbackByLimit["agy.claude_gpt"]; q.Source != "agy_ls_rpc" || q.WindowMinutes == nil || *q.WindowMinutes != 10080 || q.WindowKind != "secondary" {
		t.Fatalf("unexpected AGY claude_gpt fallback quota: %+v", q)
	}

	// Append observations across multiple pools and timestamps.
	for i := int64(0); i < 3; i++ {
		if err := repo.AppendAGYQuotaObservation(ctx, AGYQuotaObservation{
			AccountScope: scope, LimitID: "agy.gemini", WindowKind: "primary",
			ObservedAtMS: at + i*60000, UsedPercent: 20 + i*10, ResetsAtMS: &reset5h,
		}, 256); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.AppendAGYQuotaObservation(ctx, AGYQuotaObservation{
		AccountScope: scope, LimitID: "agy.claude_gpt", WindowKind: "secondary",
		ObservedAtMS: at, UsedPercent: 5, ResetsAtMS: &resetWeekly,
	}, 256); err != nil {
		t.Fatal(err)
	}

	hist, err := repo.ReportingQuotaPage(ctx, "agy", "", 0)
	if err != nil || len(hist.Batch.Quotas) != 4 {
		t.Fatalf("expected 4 AGY history quota observations, got %d err=%v", len(hist.Batch.Quotas), err)
	}
	next, err := repo.ReportingQuotaPage(ctx, "agy", hist.Next, 0)
	if err != nil || !next.Done || len(next.Batch.Quotas) != 0 {
		t.Fatalf("expected completed AGY quota paging, got %+v err=%v", next, err)
	}
	cur, err := repo.ReportingCurrentQuotaPage(ctx, "agy", 0)
	if err != nil || !cur.Done || len(cur.Batch.Quotas) != 2 {
		t.Fatalf("expected 2 current AGY pool windows, got %+v err=%v", cur, err)
	}
	body, _ := json.Marshal(cur.Batch)
	if strings.Contains(string(body), "@") || strings.Contains(string(body), "csrf") {
		t.Fatalf("sensitive info leaked in AGY quota export: %s", string(body))
	}
}
