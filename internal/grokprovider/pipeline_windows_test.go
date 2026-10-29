package grokprovider

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestWindowsGrokFileStoreQueryAndLockedLastGood(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := platformfs.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := storesqlite.Open(ctx, storesqlite.Config{Path: filepath.Join(private, "facts.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	repository := store.NewRepository(db)
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(root, strings.Repeat("long-", 30), "中文 空格#%&", "sessions")
	dir := filepath.Join(sessions, "project", "session")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "summary.json"), map[string]any{"info": map[string]string{"id": "session", "cwd": `C:\work\中文项目`}, "created_at": "2026-10-28T00:00:00Z", "current_model_id": "grok-4.6"})
	path := filepath.Join(dir, "updates.jsonl")
	line := `{"method":"_x.ai/session/update","timestamp":1793145600,"params":{"sessionId":"session","update":{"sessionUpdate":"turn_completed","prompt_id":"prompt-1","usage":{"inputTokens":100,"outputTokens":20,"totalTokens":120,"cachedReadTokens":40,"costUsdTicks":1500000},"text":"PRIVATE BODY"}}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	clock := time.UnixMilli(1793145700000)
	collector, err := NewCollector(repository, Config{SessionsRoot: sessions, MinimumRefresh: time.Second, Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	assertQuery := func(partial bool) {
		t.Helper()
		service, err := NewQueryService(collector, repository)
		if err != nil {
			t.Fatal(err)
		}
		// This test drives collection explicitly; no background scan may race teardown.
		service.localRefreshing = true
		value, err := service.UsageCost(ctx, usagecost.UsageCostRequest{ExactRange: &basequery.UTCTimeRange{StartAtMS: 1793059200000, EndAtMS: 1793232000000, TimeZone: "Asia/Shanghai"}, Granularity: usagecost.TrendDay})
		if err != nil || value.Totals.TotalTokens.Value == nil || *value.Totals.TotalTokens.Value != 120 || value.ReportedUSDMicros == nil || value.ReportedUSDMicros.Value == nil || *value.ReportedUSDMicros.Value != 150 {
			t.Fatalf("closed loop: %+v %v", value, err)
		}
		if (value.Meta.Status == basequery.ResponsePartial) != partial {
			facts, _ := repository.GrokSnapshot(ctx)
			t.Fatalf("source completeness: want partial=%v, got %s; sources=%+v sessions=%+v", partial, value.Meta.Status, facts.Sources, facts.Sessions)
		}
		facts, err := repository.GrokSnapshot(ctx)
		if err != nil || len(facts.Sessions) != 1 || facts.Sessions[0].ProjectDisplayName != "中文项目" {
			t.Fatalf("Windows project: %+v %v", facts.Sessions, err)
		}
		payload, _ := json.Marshal(facts)
		if strings.Contains(string(payload), "PRIVATE BODY") || strings.Contains(string(payload), `C:\\work`) {
			t.Fatal("content or raw project path persisted")
		}
	}
	assertQuery(false)
	name, _ := windows.UTF16PtrFromString(path)
	lock, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	err = collector.Refresh(ctx)
	_ = windows.CloseHandle(lock)
	if !errors.Is(err, store.ErrGrokSourceUnavailable) {
		t.Fatalf("locked file replaced last good: %v", err)
	}
	assertQuery(true)
	clock = clock.Add(time.Second)
	if err := collector.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	assertQuery(false)
}
