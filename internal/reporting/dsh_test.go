package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/dshprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type dshTestPreferences struct{ snapshot preferences.Snapshot }

func (p *dshTestPreferences) LoadPreferences(context.Context) (preferences.Snapshot, error) {
	return p.snapshot, nil
}

func TestDSHCollectorExporterDurableQueueAndDisable(t *testing.T) {
	for _, tc := range []struct {
		route, model string
		cost         int64
	}{{"deepseek-account", "deepseek-flash", 136}, {"openai-codex", "gpt-6.1-sol", 2230}} {
		t.Run(tc.route, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			db, err := storesqlite.Open(ctx, storesqlite.Config{Path: filepath.Join(root, "facts.db")})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(context.Background())
			r := store.NewRepository(db)
			if err := r.EnsureApplicationSchema(ctx); err != nil {
				t.Fatal(err)
			}
			state, statePath := testState(t)
			prefs := &dshTestPreferences{snapshot: preferences.Snapshot{Providers: preferences.DefaultProviderPreferences()}}
			at := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC).UnixMilli()
			logs := filepath.Join(root, "sessions")
			sessionDir := filepath.Join(logs, "project", "session")
			if err := os.MkdirAll(sessionDir, 0700); err != nil {
				t.Fatal(err)
			}
			header := map[string]any{"type": "session", "version": 4, "id": "private-session", "cwd": "/synthetic/private-project", "createdAt": at - 1000}
			rows := []map[string]any{
				{"seq": 0, "time": at - 900, "type": "request/header", "data": map[string]any{"header": map[string]any{"config": map[string]any{"provider": tc.route, "model": tc.model}}}},
				{"seq": 1, "time": at - 800, "type": "turn/start", "data": map[string]int{"turn": 1}},
				{"seq": 2, "time": at - 700, "type": "step/start", "data": map[string]int{"turn": 1, "step": 1}},
				{"seq": 3, "time": at - 200, "type": "assistant/message", "data": map[string]any{"turn": 1, "step": 1, "usage": map[string]int{"inputTokens": 100, "cacheReadTokens": 300, "outputTokens": 200, "totalTokens": 600}, "message": map[string]any{"content": []map[string]string{{"type": "text", "text": "PRIVATE BODY"}}}}},
				{"seq": 4, "time": at - 100, "type": "turn/end", "data": map[string]int{"turn": 1}},
				{"seq": 5, "time": at - 50, "type": "session/title", "data": map[string]any{"title": "合成会话名称", "source": map[string]string{"kind": "user"}, "messageSeqs": []int{}}},
			}
			file, err := os.Create(filepath.Join(sessionDir, "session.v4.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			encoder := json.NewEncoder(file)
			if err := encoder.Encode(header); err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if err := encoder.Encode(row); err != nil {
					t.Fatal(err)
				}
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			collector, err := dshprovider.NewCollector(r, dshprovider.Config{SessionsRoot: logs, Now: func() time.Time { return time.UnixMilli(at) }})
			if err != nil {
				t.Fatal(err)
			}
			if err := collector.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			exporter := NewExporter(r, prefs, state)
			page, err := exporter.Page(ctx, "dsh", "", 0)
			if err != nil || len(page.Sessions) != 1 || !page.Authority {
				t.Fatal("DSH export unavailable", err)
			}
			snapshot := page.Sessions[0]
			if snapshot.Title != "合成会话名称" || snapshot.Provider != "dsh" || snapshot.Contributions[0].CostMicroUSD == nil || *snapshot.Contributions[0].CostMicroUSD != tc.cost {
				t.Fatal("USD facts lost")
			}
			query, err := dshprovider.NewQueryService(collector, r)
			if err != nil {
				t.Fatal(err)
			}
			usage, err := query.UsageCost(ctx, usagecost.UsageCostRequest{ExactRange: &basequery.UTCTimeRange{StartAtMS: at - 1000, EndAtMS: at + 1, TimeZone: "UTC"}, Granularity: usagecost.TrendDay})
			if err != nil || usage.Totals.EstimatedUSDMicros.Value == nil || *usage.Totals.EstimatedUSDMicros.Value != tc.cost || len(usage.PricingVersions) != 1 || usage.PricingVersions[0] != *snapshot.Contributions[0].PricingVersion {
				t.Fatal("Mac query and reporting price disagree", usage, err)
			}
			if err := state.Enqueue(ctx, "center", "sweep", snapshot); err != nil {
				t.Fatal(err)
			}
			first, err := state.next(ctx, "center")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(first.Body), "PRIVATE BODY") || strings.Contains(string(first.Body), "/synthetic/") || strings.Contains(string(first.Body), "private-session") {
				t.Fatal("private source content was exported")
			}
			if err := state.Close(ctx); err != nil {
				t.Fatal(err)
			}
			restarted, err := OpenState(ctx, statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close(context.Background())
			again, err := restarted.next(ctx, "center")
			if err != nil || string(again.Body) != string(first.Body) {
				t.Fatal("DSH queue changed on restart", err)
			}
			var batch reportingv1.Batch
			if err := json.Unmarshal(again.Body, &batch); err != nil {
				t.Fatal(err)
			}
			if err := batch.Validate(); err != nil {
				t.Fatal("queued DSH contract invalid", err)
			}
			// 未改日志、未补采；直接重读已有 Store 仍能导出相同费用证据。
			repeated, err := exporter.Page(ctx, "dsh", "", 0)
			if err != nil || len(repeated.Sessions) != 1 {
				t.Fatal("unchanged indexed history could not be re-exported", err)
			}
			if err := restarted.Enqueue(ctx, "center", "repeat", repeated.Sessions[0]); err != nil {
				t.Fatal(err)
			}
			prefs.snapshot.Providers.DSH.Intent = preferences.ProviderIntentDisabled
			if _, err := exporter.Page(ctx, "dsh", "", 0); !errors.Is(err, store.ErrReportingSource) {
				t.Fatal("disabled DSH exported new facts", err)
			}
			status, err := exporter.Status(ctx, "dsh", 0)
			if err != nil || status.Status != "disabled" {
				t.Fatal("disabled DSH status lost", err)
			}
			if err := restarted.Ack(ctx, again, reportingv1.Receipt{Version: 1, BatchID: again.BatchID, ReceivedAtMS: at + 1000}); err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.next(ctx, "center"); err == nil {
				t.Fatal("repeat scan duplicated queue")
			}
		})
	}
}

func TestDSHCostRevisionKeepsContributionAndQueuedHistory(t *testing.T) {
	state, _ := testState(t)
	ctx := t.Context()
	snap := testSnapshot()
	snap.Provider, snap.SourceKind = "dsh", "dsh_local"
	snap.Contributions[0].Model = new("gpt-6.1-sol")
	snap.Contributions[0].PricingMode = "event_cost"
	snap.Contributions[0].ID = reportingv1.ContributionID(snap.Provider, snap.SessionID, snap.Contributions[0], 0)
	if err := state.Enqueue(ctx, "center", "old", snap); err != nil {
		t.Fatal(err)
	}
	old, err := state.next(ctx, "center")
	if err != nil {
		t.Fatal(err)
	}
	snap.Contributions[0].CostStatus = "known"
	snap.Contributions[0].CostMicroUSD = new(int64(20))
	snap.Contributions[0].PricingVersion = new("openai-api-2026-09-29")
	if id := reportingv1.ContributionID(snap.Provider, snap.SessionID, snap.Contributions[0], 0); id != snap.Contributions[0].ID {
		t.Fatal("price altered contribution identity")
	}
	if err := state.Enqueue(ctx, "center", "new", snap); err != nil {
		t.Fatal(err)
	}
	unchanged, err := state.next(ctx, "center")
	if err != nil || string(unchanged.Body) != string(old.Body) {
		t.Fatal("pending old batch mutated", err)
	}
	if err := state.Ack(ctx, old, reportingv1.Receipt{Version: 1, BatchID: old.BatchID}); err != nil {
		t.Fatal(err)
	}
	next, err := state.next(ctx, "center")
	if err != nil {
		t.Fatal(err)
	}
	var batch reportingv1.Batch
	if err := json.Unmarshal(next.Body, &batch); err != nil {
		t.Fatal(err)
	}
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
	revision := batch.Sessions[0]
	if revision.Revision != 2 || len(revision.Contributions) != 1 || revision.Contributions[0].ID != snap.Contributions[0].ID || revision.Contributions[0].CostMicroUSD == nil || *revision.Contributions[0].CostMicroUSD != 20 {
		t.Fatal("historical price revision lost", revision)
	}
	if err := state.Enqueue(ctx, "center", "repeat", snap); err != nil {
		t.Fatal(err)
	}
	if err := state.Ack(ctx, next, reportingv1.Receipt{Version: 1, BatchID: next.BatchID}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.next(ctx, "center"); err == nil {
		t.Fatal("same price revision was repeated")
	}
}
