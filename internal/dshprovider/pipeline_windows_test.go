package dshprovider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestWindowsDSHPlainAndZstdFileStoreQuery(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "zstd"}[compressed], func(t *testing.T) {
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
			cwd, _ := json.Marshal(`C:\work\中文项目`)
			content := strings.Replace(fixture(true), `"/synthetic/demo"`, string(cwd), 1)
			path := filepath.Join(dir, "session.v4.jsonl")
			payload := []byte(content)
			if compressed {
				encoder, err := zstd.NewWriter(nil)
				if err != nil {
					t.Fatal(err)
				}
				payload = encoder.EncodeAll(payload, nil)
				encoder.Close()
				path += ".zstd"
			}
			if err := os.WriteFile(path, payload, 0600); err != nil {
				t.Fatal(err)
			}
			collector, err := NewCollector(repository, Config{SessionsRoot: sessions, Now: func() time.Time { return time.UnixMilli(2000) }})
			if err != nil {
				t.Fatal(err)
			}
			if err := collector.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			service, err := NewQueryService(collector, repository)
			if err != nil {
				t.Fatal(err)
			}
			service.localRefreshing = true
			value, err := service.UsageCost(ctx, usagecost.UsageCostRequest{ExactRange: &basequery.UTCTimeRange{StartAtMS: 0, EndAtMS: 3000, TimeZone: "UTC"}, Granularity: usagecost.TrendDay})
			if err != nil || value.Totals.TotalTokens.Value == nil || *value.Totals.TotalTokens.Value != 70 || value.Totals.InputTokens.Value == nil || *value.Totals.InputTokens.Value != 47 || value.Totals.CachedInputTokens.Value == nil || *value.Totals.CachedInputTokens.Value != 35 {
				t.Fatalf("closed loop: %+v %v", value, err)
			}
			if value.Totals.EstimatedUSDMicros.Value != nil {
				t.Fatal("1970 fixture acquired a current/historical price without evidence")
			}
			facts, err := repository.DSHSnapshot(ctx)
			if err != nil || len(facts.Sessions) != 1 || facts.Sessions[0].ProjectDisplayName != "中文项目" || len(facts.UsageEvents) != 2 {
				t.Fatalf("project or seeded/retry projection: %+v %v", facts.Sessions, err)
			}
			data, _ := json.Marshal(facts)
			if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), `C:\\work`) {
				t.Fatal("content or raw project path persisted")
			}
		})
	}
}
