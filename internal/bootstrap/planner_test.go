package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestFreezeInitialPlanPrioritizesHintsWithinBudgetAndTiersBackfill(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	paths := map[string]string{
		"today": filepath.Join(sessions, "today.jsonl"),
		"week":  filepath.Join(sessions, "week.jsonl"),
		"month": filepath.Join(sessions, "month.jsonl"),
		"old":   filepath.Join(sessions, "old.jsonl"),
	}
	ages := map[string]time.Duration{
		"today": time.Hour, "week": 3 * 24 * time.Hour,
		"month": 20 * 24 * time.Hour, "old": 60 * 24 * time.Hour,
	}
	for name, path := range paths {
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
		mtime := now.Add(-ages[name])
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("Chtimes(%s) error = %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(session index) error = %v", err)
	}

	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	reconcile, err := logs.PlanReconcile(home, nil, discovery)
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	var weekSourceID string
	for _, snapshot := range discovery.Snapshots {
		if snapshot.Path == paths["week"] {
			weekSourceID = snapshot.SourceFileID
		}
	}
	items, err := FreezeInitialPlan(PlanRequest{
		JobID: "bootstrap-job", Reconcile: reconcile, NowMS: now.UnixMilli(),
		DayStartMS:   now.Truncate(24 * time.Hour).UnixMilli(),
		FastMaxFiles: 2, FastMaxBytes: 16,
		RecencyHints: map[string]int64{weekSourceID: now.Add(-30 * time.Minute).UnixMilli()},
		AtMS:         20,
	})
	if err != nil {
		t.Fatalf("FreezeInitialPlan() error = %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("len(items) = %d, want 4 rollout sources", len(items))
	}
	assertPlanItem(t, items[0], 0, store.BootstrapLaneFast, store.BootstrapTierToday, paths["week"])
	assertPlanItem(t, items[1], 1, store.BootstrapLaneFast, store.BootstrapTierToday, paths["today"])
	assertPlanItem(t, items[2], 2, store.BootstrapLaneBackfill, store.BootstrapTierRecent30Days, paths["month"])
	assertPlanItem(t, items[3], 3, store.BootstrapLaneBackfill, store.BootstrapTierOlder, paths["old"])
}

func TestFreezeInitialPlanHandlesEmptyPlanAndRejectsInvalidBudget(t *testing.T) {
	t.Parallel()

	items, err := FreezeInitialPlan(PlanRequest{
		JobID: "bootstrap-empty", NowMS: 100, DayStartMS: 0,
		FastMaxFiles: 1, FastMaxBytes: 1, AtMS: 100,
	})
	if err != nil || len(items) != 0 {
		t.Fatalf("FreezeInitialPlan(empty) = %#v, %v, want empty", items, err)
	}
	_, err = FreezeInitialPlan(PlanRequest{JobID: "bootstrap-invalid", FastMaxFiles: 0, FastMaxBytes: 1})
	if err == nil {
		t.Fatal("FreezeInitialPlan(invalid budget) error = nil")
	}
}

func TestFreezeInitialPlanFillsStaticFastBudgetFromOlderSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	for index, name := range []string{"old-a.jsonl", "old-b.jsonl", "old-c.jsonl"} {
		path := filepath.Join(home, "sessions", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
		modified := now.Add(-time.Duration(60+index) * 24 * time.Hour)
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatalf("Chtimes(%s) error = %v", name, err)
		}
	}
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	reconcile, err := logs.PlanReconcile(home, nil, discovery)
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	items, err := FreezeInitialPlan(PlanRequest{
		JobID: "bootstrap-old-budget", Reconcile: reconcile,
		NowMS: now.UnixMilli(), DayStartMS: now.Add(-12 * time.Hour).UnixMilli(),
		FastMaxFiles: 2, FastMaxBytes: 16, AtMS: 10,
	})
	if err != nil {
		t.Fatalf("FreezeInitialPlan() error = %v", err)
	}
	if len(items) != 3 || items[0].Lane != store.BootstrapLaneFast ||
		items[1].Lane != store.BootstrapLaneFast || items[2].Lane != store.BootstrapLaneBackfill {
		t.Fatalf("old-session plan = %#v, want two fast and one backfill", items)
	}
}

func TestFreezeReconcilePlanPreservesActionOrderAndStartsAtFixedOrdinal(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, name := range []string{"b.jsonl", "a.jsonl"} {
		if err := os.WriteFile(filepath.Join(home, "sessions", name), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	reconcile, err := logs.PlanReconcile(home, nil, discovery)
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	items, err := FreezeReconcilePlan(ReconcilePlanRequest{
		JobID: "bootstrap-reconcile", Reconcile: reconcile,
		StartOrdinal: 7, Pass: 2, AtMS: 30,
	})
	if err != nil {
		t.Fatalf("FreezeReconcilePlan() error = %v", err)
	}
	if len(items) != 2 || items[0].Ordinal != 7 || items[1].Ordinal != 8 {
		t.Fatalf("FreezeReconcilePlan() = %#v", items)
	}
	for index, item := range items {
		if item.Pass != 2 || item.Lane != store.BootstrapLaneReconcile ||
			item.Tier != store.BootstrapTierReconcile || item.ActionKind != store.BootstrapActionAdded ||
			item.State != store.BootstrapItemQueued || item.Current == nil ||
			item.Current.CurrentPath != reconcile.Actions[index].Current.Path {
			t.Fatalf("reconcile item[%d] = %#v", index, item)
		}
	}
	if _, err := FreezeReconcilePlan(ReconcilePlanRequest{
		JobID: "bootstrap-reconcile", StartOrdinal: -1, Pass: 1, AtMS: 30,
	}); err == nil {
		t.Fatal("FreezeReconcilePlan(invalid) error = nil")
	}
}

func assertPlanItem(
	t *testing.T,
	item store.BootstrapPlanItem,
	wantOrdinal int64,
	wantLane store.BootstrapLane,
	wantTier store.BootstrapTier,
	wantPath string,
) {
	t.Helper()
	if item.Ordinal != wantOrdinal || item.Lane != wantLane || item.Tier != wantTier ||
		item.Current == nil || item.Current.CurrentPath != wantPath ||
		item.ActionKind != store.BootstrapActionAdded || item.State != store.BootstrapItemQueued {
		t.Fatalf("plan item = %#v, want ordinal=%d lane=%s tier=%s path=%s", item, wantOrdinal, wantLane, wantTier, wantPath)
	}
}
