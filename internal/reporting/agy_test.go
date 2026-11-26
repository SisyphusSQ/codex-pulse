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
	prefs := &dshTestPreferences{snapshot: preferences.Snapshot{Providers: preferences.DefaultProviderPreferences()}}

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
	prefs := &dshTestPreferences{snapshot: preferences.Snapshot{Providers: providers}}

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
