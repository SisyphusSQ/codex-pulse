package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func openAGYReportingRepo(t *testing.T) *store.Repository {
	t.Helper()
	ctx := t.Context()
	root := filepath.Join(t.TempDir(), "private")
	if err := platformfs.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := storesqlite.Open(ctx, storesqlite.Config{Path: filepath.Join(root, "facts.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	r := store.NewRepository(db)
	if err := r.EnsureApplicationSchema(ctx); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAGYExporterQuotaAccountPrivacyDurableQueueAndFullSync(t *testing.T) {
	ctx := t.Context()
	r := openAGYReportingRepo(t)
	state, statePath := testState(t)
	prefs := &testPreferences{snapshot: preferences.Snapshot{Providers: preferences.DefaultProviderPreferences()}}

	at := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC).UnixMilli()
	evStart, evEnd := at-2000, at
	scope := reportingv1.Key("agy-account", "opaque-user-digest")
	plan := "Google AI Ultra 20×"
	reset5h := at + 300*60000
	resetWeekly := at + 10080*60000

	if err := r.ReplaceAGYSnapshot(ctx, store.AGYSnapshot{
		Generation:    1,
		CollectedAtMS: at + 1000,
		Sources: []store.CursorSourceStatus{
			{Provider: "agy", SourceKey: "agy.conversation_steps", SourceType: "sqlite", State: "available", CoverageState: "exact", CheckpointKind: "snapshot", LastAttemptAtMS: at, LastSuccessAtMS: &at, UpdatedAtMS: at},
			{Provider: "agy", SourceKey: "agy.transcript_fallback", SourceType: "jsonl", State: "not_configured", CoverageState: "unknown", CheckpointKind: "filesystem_scan", LastAttemptAtMS: at, UpdatedAtMS: at},
		},
		Sessions: []store.AGYSession{{
			ExternalSessionID:  "agy-session-1",
			DisplayTitle:       "AGY 中心同步测试",
			TitleSource:        "agy_annotation",
			SessionRole:        "main",
			ProjectKey:         reportingv1.Key("agy", "workspace"),
			ProjectDisplayName: "workspace",
			ProjectSource:      "workspace",
			CreatedAtMS:        evStart,
			LastActivityAtMS:   at,
			ModelKey:           new("gemini-4-carbon"),
			RequestCount:       1,
			CoverageState:      "exact",
			Throughput: &reportingv1.ThroughputCapsule{
				Version: 1,
				Basis:   "closed_turn_lifetime_output",
				Measures: reportingv1.ThroughputMeasures{
					OutputTokens:     new(int64(250)),
					ActiveDurationMS: new(int64(2000)),
					IncludedTurns:    1,
					CoverageKnown:    true,
					Status:           "complete",
					DurationSource:   "step_metadata",
				},
				TurnsTotal: 1,
			},
			UpdatedAtMS: at,
		}},
		UsageEvents: []store.AGYUsageEvent{{
			EventID:             reportingv1.Key("agy-ev", "1"),
			ExternalSessionID:   "agy-session-1",
			StepIndex:           0,
			OccurredAtMS:        at,
			ModelKey:            new("gemini-4-carbon"),
			UncachedInputTokens: 1000,
			CacheReadTokens:     500,
			OutputTokens:        250,
			ReasoningTokens:     50,
			TotalTokens:         1750,
			CacheReadKnown:      true,
			ReasoningKnown:      true,
			TotalKnown:          true,
			StartedAtMS:         &evStart,
			EndedAtMS:           &evEnd,
			Provenance:          "agy_step_metadata",
			UpdatedAtMS:         at,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceAGYQuotaSnapshots(ctx, scope, []store.AGYQuotaSnapshot{
		{AccountScope: scope, Generation: at, PlanType: &plan, LimitID: "agy.gemini", LimitName: "Gemini", WindowKind: "primary", WindowMinutes: 300, UsedPercent: new(int64(30)), ResetsAtMS: &reset5h, DataAsOfMS: at, UpdatedAtMS: at},
		{AccountScope: scope, Generation: at, PlanType: &plan, LimitID: "agy.claude_gpt", LimitName: "Claude & GPT", WindowKind: "secondary", WindowMinutes: 10080, UsedPercent: new(int64(15)), ResetsAtMS: &resetWeekly, DataAsOfMS: at, UpdatedAtMS: at},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendAGYQuotaObservation(ctx, store.AGYQuotaObservation{
		AccountScope: scope, LimitID: "agy.gemini", WindowKind: "primary",
		ObservedAtMS: at, UsedPercent: 30, ResetsAtMS: &reset5h,
	}, 256); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendAGYQuotaObservation(ctx, store.AGYQuotaObservation{
		AccountScope: scope, LimitID: "agy.claude_gpt", WindowKind: "secondary",
		ObservedAtMS: at, UsedPercent: 15, ResetsAtMS: &resetWeekly,
	}, 256); err != nil {
		t.Fatal(err)
	}

	exporter := NewExporter(r, prefs, state)
	page, err := exporter.Page(ctx, "agy", "", 0)
	if err != nil || len(page.Sessions) != 1 || !page.Authority {
		t.Fatalf("AGY page export failed: page=%+v err=%v", page, err)
	}
	if err := state.Enqueue(ctx, "center", "sweep", page.Sessions[0]); err != nil {
		t.Fatal(err)
	}
	factsPage, err := exporter.Facts(ctx, "agy", "", 0)
	if err != nil || len(factsPage.Groups) != 3 {
		t.Fatalf("expected 1 account group + 2 quota groups for AGY, got %d err=%v", len(factsPage.Groups), err)
	}
	if _, err := state.EnqueueFactGroups(ctx, "center", factsPage.Groups); err != nil {
		t.Fatal(err)
	}

	// Verify queued batches pass contract validation and leak zero sensitive fields.
	var sawAccount, sawGemini, sawClaude, sawSession bool
	for {
		item, err := state.next(ctx, "center")
		if err != nil {
			break
		}
		var batch reportingv1.Batch
		if err := json.Unmarshal(item.Body, &batch); err != nil {
			t.Fatal(err)
		}
		if err := batch.Validate(); err != nil {
			t.Fatalf("queued AGY batch invalid: %v body=%s", err, string(item.Body))
		}
		if strings.Contains(string(item.Body), "@") || strings.Contains(string(item.Body), "/Users/") || strings.Contains(string(item.Body), "csrf") {
			t.Fatalf("sensitive info leaked: %s", string(item.Body))
		}
		for _, a := range batch.Accounts {
			if a.Provider == "agy" && a.ID == scope && a.Email == nil && a.Plan != nil && *a.Plan == plan {
				sawAccount = true
			}
		}
		for _, q := range batch.Quotas {
			if q.Provider == "agy" && q.Source == "agy_ls_rpc" && q.HistoryOrigin == "confirmed" && q.AccountID != nil && *q.AccountID == scope {
				if q.LimitID == "agy.gemini" && q.WindowKind == "primary" {
					sawGemini = true
				}
				if q.LimitID == "agy.claude_gpt" && q.WindowKind == "secondary" {
					sawClaude = true
				}
			}
		}
		if len(batch.Sessions) == 1 && batch.Sessions[0].Provider == "agy" {
			sawSession = true
		}
		if err := state.Ack(ctx, item, reportingv1.Receipt{Version: 1, BatchID: item.BatchID, ReceivedAtMS: at + 2000}); err != nil {
			t.Fatal(err)
		}
	}
	if !sawAccount || !sawGemini || !sawClaude || !sawSession {
		t.Fatalf("missing AGY exported facts: account=%v gemini=%v claude=%v session=%v", sawAccount, sawGemini, sawClaude, sawSession)
	}

	// Verify durable restart + full sync re-exports AGY facts.
	if err := state.Close(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenState(ctx, statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	if err := restarted.startFullSync(ctx, "center"); err != nil {
		t.Fatal(err)
	}
	reExporter := NewExporter(r, prefs, restarted)
	rePage, err := reExporter.Page(ctx, "agy", "", 0)
	if err != nil || len(rePage.Sessions) != 1 {
		t.Fatalf("full-sync AGY re-export failed: %+v err=%v", rePage, err)
	}
	if err := restarted.Enqueue(ctx, "center", "full-sync", rePage.Sessions[0]); err != nil {
		t.Fatal(err)
	}
	queuedAgain, err := restarted.next(ctx, "center")
	if err != nil {
		t.Fatalf("full-sync did not re-enqueue AGY session: %v", err)
	}
	var reBatch reportingv1.Batch
	if err := json.Unmarshal(queuedAgain.Body, &reBatch); err != nil {
		t.Fatal(err)
	}
	if reBatch.Sessions[0].Revision != 2 {
		t.Fatalf("expected monotonic revision 2 on full-sync, got %d", reBatch.Sessions[0].Revision)
	}
}

func TestAGYDisabledDoesNotCausePartialCenterSyncStatus(t *testing.T) {
	ctx := t.Context()
	r := openAGYReportingRepo(t)
	state, _ := testState(t)

	// Enable DSH (with healthy local data), disable Codex/Cursor/Grok/AGY explicitly (TOO-715).
	providers := preferences.DefaultProviderPreferences()
	providers.Codex.Intent = preferences.ProviderIntentDisabled
	providers.Cursor.Intent = preferences.ProviderIntentDisabled
	providers.Grok.Intent = preferences.ProviderIntentDisabled
	providers.DSH.Intent = preferences.ProviderIntentEnabled
	providers.AGY.Intent = preferences.ProviderIntentDisabled
	prefs := &testPreferences{snapshot: preferences.Snapshot{Providers: providers}}

	at := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC).UnixMilli()
	if err := r.ReplaceDSHSnapshot(ctx, store.DSHSnapshot{
		Generation:    1,
		CollectedAtMS: at,
		Sources: []store.CursorSourceStatus{{
			Provider: "dsh", SourceKey: "dsh.sessions", SourceType: "jsonl",
			State: "available", CoverageState: "exact", CheckpointKind: "filesystem_scan",
			LastAttemptAtMS: at, LastSuccessAtMS: &at, UpdatedAtMS: at,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	exporter := NewExporter(r, prefs, state)
	if _, err := exporter.Page(ctx, "agy", "", 0); !errors.Is(err, store.ErrReportingProviderDisabled) || !errors.Is(err, store.ErrReportingSource) {
		t.Fatalf("disabled AGY must return ErrReportingProviderDisabled wrapping ErrReportingSource, got %v", err)
	}
	if _, err := exporter.FactsPartition(ctx, "agy"); !errors.Is(err, store.ErrReportingProviderDisabled) {
		t.Fatalf("disabled AGY FactsPartition must return ErrReportingProviderDisabled, got %v", err)
	}
	st, err := exporter.Status(ctx, "agy", 0)
	if err != nil || st.Status != "disabled" {
		t.Fatalf("disabled AGY status mismatch: %+v err=%v", st, err)
	}

	var mu sync.Mutex
	var received []reportingv1.Batch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/v1/sync" {
			_, _ = w.Write([]byte(`{"code":200,"data":{"paid_credits":false}}`))
			return
		}
		var b reportingv1.Batch
		if err := json.NewDecoder(req.Body).Decode(&b); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := b.Validate(); err != nil {
			t.Error("invalid batch from runtime:", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = append(received, b)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reportingv1.Receipt{Version: 1, BatchID: b.ID, ReceivedAtMS: at + 1000}})
	}))
	defer server.Close()

	cfg := credentialSettings{ID: 1, Endpoint: server.URL, ClientID: "client", Credential: strings.Repeat("c", 43), AllowHTTP: true, Enabled: true, IntervalSeconds: 60, State: "ready"}
	if err := state.saveSettings(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := state.startFullSync(ctx, cfg.partition()); err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{state: state, source: exporter, ctx: ctx, version: "test"}
	for range 3 {
		again, err := runtime.cycle(ctx, cfg)
		if err != nil {
			t.Fatalf("cycle failed with disabled AGY: %v", err)
		}
		if !again {
			break
		}
	}
	finalStatus, err := state.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if finalStatus.State != "ready" {
		t.Fatalf("explicitly disabled AGY degraded center sync state to %q (want ready)", finalStatus.State)
	}
	if finalStatus.FullSyncState != "completed" {
		t.Fatalf("explicitly disabled AGY blocked full sync completion: %q", finalStatus.FullSyncState)
	}
	mu.Lock()
	defer mu.Unlock()
	sawAGYDisabled := false
	for _, b := range received {
		for _, ds := range b.Status {
			if ds.Provider == "agy" && ds.Status == "disabled" && ds.SyncState == "ready" {
				sawAGYDisabled = true
			}
		}
		for _, s := range b.Sessions {
			if s.Provider == "agy" {
				t.Fatal("disabled AGY exported session")
			}
		}
		for _, q := range b.Quotas {
			if q.Provider == "agy" {
				t.Fatal("disabled AGY exported quota")
			}
		}
	}
	if !sawAGYDisabled {
		t.Fatal("expected AGY DeviceStatus{status=disabled, sync_state=ready} in center upload")
	}
}

func TestAGYAccountUsageExportReconcilesContributionIDsAndRespectsSyncCapability(t *testing.T) {
	ctx := t.Context()
	r := openAGYReportingRepo(t)
	state, _ := testState(t)
	prefs := &testPreferences{snapshot: preferences.Snapshot{Providers: preferences.DefaultProviderPreferences()}}

	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC).UnixMilli()
	scopeA := reportingv1.Key("agy-account", "user-a")
	scopeB := reportingv1.Key("agy-account", "user-b")
	plan := "Google AI Ultra 20×"
	reset5h := now + 2*3600000
	resetWeekly := now + 4*86400000
	// Account A confirmed on [now-60m, now-30m], Account B on [now-20m, now-10m].
	// Gap (now-30m, now-20m) must remain unattributed.
	tA1, tA2 := now-60*60000, now-30*60000
	tB1, tB2 := now-20*60000, now-10*60000
	evA1 := tA1 + 5*60000
	evA2 := tA1 + 10*60000
	evGap := tA2 + 5*60000
	evB1 := tB1 + 5*60000

	if err := r.ReplaceAGYSnapshot(ctx, store.AGYSnapshot{
		Generation:    1,
		CollectedAtMS: now,
		Sources: []store.CursorSourceStatus{
			{Provider: "agy", SourceKey: "agy.conversation_steps", SourceType: "sqlite", State: "available", CoverageState: "exact", CheckpointKind: "snapshot", LastAttemptAtMS: now, LastSuccessAtMS: &now, UpdatedAtMS: now},
		},
		Sessions: []store.AGYSession{{
			ExternalSessionID: "agy-sess-acct", DisplayTitle: "AGY 账号用量测试", TitleSource: "agy_annotation",
			SessionRole: "main", ProjectKey: reportingv1.Key("agy", "proj"), ProjectDisplayName: "proj", ProjectSource: "workspace",
			CreatedAtMS: tA1, LastActivityAtMS: evB1, ModelKey: new("gemini-4-carbon-high"), RequestCount: 4, CoverageState: "exact", UpdatedAtMS: now,
		}},
		UsageEvents: []store.AGYUsageEvent{
			{
				EventID: reportingv1.Key("ev", "a1"), ExternalSessionID: "agy-sess-acct", StepIndex: 0, OccurredAtMS: evA1,
				ModelKey: new("gemini-4-carbon-high"), UncachedInputTokens: 1000, CacheReadTokens: 500, OutputTokens: 100, ReasoningTokens: 40, TotalTokens: 1600,
				CacheReadKnown: true, ReasoningKnown: true, TotalKnown: true, Provenance: "agy_step_metadata", UpdatedAtMS: now,
			},
			{
				EventID: reportingv1.Key("ev", "a2"), ExternalSessionID: "agy-sess-acct", StepIndex: 1, OccurredAtMS: evA2,
				ModelKey: new("claude-opus-5-5-high"), UncachedInputTokens: 200, CacheReadTokens: 100, OutputTokens: 50, TotalTokens: 350,
				CacheReadKnown: true, ReasoningKnown: false, TotalKnown: true, Provenance: "agy_step_metadata", UpdatedAtMS: now,
			},
			{
				EventID: reportingv1.Key("ev", "gap"), ExternalSessionID: "agy-sess-acct", StepIndex: 2, OccurredAtMS: evGap,
				ModelKey: new("gemini-4-carbon-high"), UncachedInputTokens: 500, CacheReadTokens: 0, OutputTokens: 50, TotalTokens: 550,
				CacheReadKnown: true, ReasoningKnown: false, TotalKnown: true, Provenance: "agy_step_metadata", UpdatedAtMS: now,
			},
			{
				EventID: reportingv1.Key("ev", "b1"), ExternalSessionID: "agy-sess-acct", StepIndex: 3, OccurredAtMS: evB1,
				ModelKey: new("gemini-3.8-flash-high"), UncachedInputTokens: 400, CacheReadTokens: 100, OutputTokens: 100, TotalTokens: 600,
				CacheReadKnown: true, ReasoningKnown: false, TotalKnown: true, Provenance: "agy_step_metadata", UpdatedAtMS: now,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{scopeA, scopeB} {
		if err := r.ReplaceAGYQuotaSnapshots(ctx, scope, []store.AGYQuotaSnapshot{
			{AccountScope: scope, Generation: now, PlanType: &plan, LimitID: "agy.gemini", LimitName: "Gemini", WindowKind: "primary", WindowMinutes: 300, UsedPercent: new(int64(20)), ResetsAtMS: &reset5h, DataAsOfMS: now, UpdatedAtMS: now},
			{AccountScope: scope, Generation: now, PlanType: &plan, LimitID: "agy.gemini", LimitName: "Gemini", WindowKind: "secondary", WindowMinutes: 10080, UsedPercent: new(int64(10)), ResetsAtMS: &resetWeekly, DataAsOfMS: now, UpdatedAtMS: now},
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, obs := range []store.AGYQuotaObservation{
		{AccountScope: scopeA, LimitID: "agy.gemini", WindowKind: "primary", ObservedAtMS: tA1, UsedPercent: 10, ResetsAtMS: &reset5h},
		{AccountScope: scopeA, LimitID: "agy.gemini", WindowKind: "primary", ObservedAtMS: tA2, UsedPercent: 20, ResetsAtMS: &reset5h},
		{AccountScope: scopeA, LimitID: "agy.gemini", WindowKind: "secondary", ObservedAtMS: tA2, UsedPercent: 10, ResetsAtMS: &resetWeekly},
		{AccountScope: scopeB, LimitID: "agy.gemini", WindowKind: "primary", ObservedAtMS: tB1, UsedPercent: 5, ResetsAtMS: &reset5h},
		{AccountScope: scopeB, LimitID: "agy.gemini", WindowKind: "primary", ObservedAtMS: tB2, UsedPercent: 15, ResetsAtMS: &reset5h},
	} {
		if err := r.AppendAGYQuotaObservation(ctx, obs, 256); err != nil {
			t.Fatal(err)
		}
	}

	exporter := NewExporter(r, prefs, state)
	exporter.now = func() time.Time { return time.UnixMilli(now) }

	// Export session snapshot first to capture authoritative ContributionIDs.
	page, err := exporter.Page(ctx, "agy", "", 0)
	if err != nil || len(page.Sessions) != 1 || len(page.Sessions[0].Contributions) != 4 {
		t.Fatalf("AGY session export failed: %+v err=%v", page, err)
	}
	wantIDs := map[int64]string{}
	for _, c := range page.Sessions[0].Contributions {
		wantIDs[*c.ObservedAtMS] = c.ID
	}

	if _, err := exporter.ExportAGYAccountUsage(ctx, "center", 0); err != nil {
		t.Fatalf("ExportAGYAccountUsage failed: %v", err)
	}
	gotByAccount := map[string]map[int64]reportingv1.AccountTokenFact{}
	periodSpans := map[string]map[int64]bool{}
	for {
		item, err := state.next(ctx, "center")
		if err != nil {
			break
		}
		var batch reportingv1.Batch
		if err := json.Unmarshal(item.Body, &batch); err != nil {
			t.Fatal(err)
		}
		if err := batch.Validate(); err != nil {
			t.Fatalf("invalid AGY account usage batch: %v", err)
		}
		for _, u := range batch.AccountUsage {
			if periodSpans[u.AccountID] == nil {
				periodSpans[u.AccountID] = map[int64]bool{}
			}
			periodSpans[u.AccountID][u.ResetsAtMS-u.WindowStartAtMS] = true
			if gotByAccount[u.AccountID] == nil {
				gotByAccount[u.AccountID] = map[int64]reportingv1.AccountTokenFact{}
			}
			for _, f := range u.Facts {
				gotByAccount[u.AccountID][f.ObservedAtMS] = f
			}
		}
		if err := state.Ack(ctx, item, reportingv1.Receipt{Version: 1, BatchID: item.BatchID, ReceivedAtMS: now}); err != nil {
			t.Fatal(err)
		}
	}
	if !periodSpans[scopeA][300*60000] || !periodSpans[scopeA][10080*60000] || !periodSpans[scopeB][300*60000] {
		t.Fatalf("missing AGY period spans: %+v", periodSpans)
	}
	if len(gotByAccount[scopeA]) != 2 || len(gotByAccount[scopeB]) != 1 {
		t.Fatalf("unexpected AGY attributed facts (gap must be excluded): A=%+v B=%+v", gotByAccount[scopeA], gotByAccount[scopeB])
	}
	if gotByAccount[scopeA][evA1].ID != wantIDs[evA1] || gotByAccount[scopeA][evA2].ID != wantIDs[evA2] || gotByAccount[scopeB][evB1].ID != wantIDs[evB1] {
		t.Fatal("AGY AccountTokenFact IDs did not match SessionSnapshot ContributionIDs")
	}
	if gotByAccount[scopeA][evA1].ReasoningTokens == nil || *gotByAccount[scopeA][evA1].ReasoningTokens != 40 || gotByAccount[scopeA][evA2].ReasoningTokens != nil {
		t.Fatalf("AGY reasoning parity lost: a1=%+v a2=%+v", gotByAccount[scopeA][evA1], gotByAccount[scopeA][evA2])
	}

	// Verify runtime gates AGY account_usage on /api/v1/sync multi_provider_account_usage capability.
	for _, supported := range []bool{false, true} {
		rtState, _ := testState(t)
		rtProviders := preferences.DefaultProviderPreferences()
		rtProviders.Codex.Intent = preferences.ProviderIntentDisabled
		rtProviders.Cursor.Intent = preferences.ProviderIntentDisabled
		rtProviders.Grok.Intent = preferences.ProviderIntentDisabled
		rtProviders.DSH.Intent = preferences.ProviderIntentDisabled
		rtExporter := NewExporter(r, &testPreferences{snapshot: preferences.Snapshot{Providers: rtProviders}}, rtState)
		rtExporter.now = func() time.Time { return time.UnixMilli(now) }
		var mu sync.Mutex
		var sawAGYAccountUsage bool
		syncBody := `{"code":200,"data":{"protocol_version":1,"paid_credits":true}}`
		if supported {
			syncBody = `{"code":200,"data":{"protocol_version":1,"paid_credits":true,"multi_provider_account_usage":true}}`
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/api/v1/sync" {
				_, _ = w.Write([]byte(syncBody))
				return
			}
			var b reportingv1.Batch
			if err := json.NewDecoder(req.Body).Decode(&b); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if err := b.Validate(); err != nil {
				t.Error("invalid batch:", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			for _, u := range b.AccountUsage {
				if u.Provider == "agy" {
					sawAGYAccountUsage = true
				}
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reportingv1.Receipt{Version: 1, BatchID: b.ID, ReceivedAtMS: now}})
		}))
		cfg := credentialSettings{ID: 1, Endpoint: srv.URL, ClientID: "client", Credential: strings.Repeat("d", 43), AllowHTTP: true, Enabled: true, IntervalSeconds: 60, State: "ready"}
		if err := rtState.saveSettings(ctx, cfg); err != nil {
			srv.Close()
			t.Fatal(err)
		}
		rt := &Runtime{state: rtState, source: rtExporter, ctx: ctx, version: "test"}
		for range 3 {
			again, err := rt.cycle(ctx, cfg)
			if err != nil {
				srv.Close()
				t.Fatalf("cycle (supported=%v) failed: %v", supported, err)
			}
			if !again {
				break
			}
		}
		srv.Close()
		mu.Lock()
		got := sawAGYAccountUsage
		mu.Unlock()
		if got != supported {
			t.Fatalf("sync multi_provider_account_usage=%v: sawAGYAccountUsage=%v", supported, got)
		}
	}
}
