package store

import (
	"context"
	"strings"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func TestDSHStorageHistoryReportingAndNullableFacts(t *testing.T) {
	r := openRuntimeRepository(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC).UnixMilli()
	model := "deepseek-flash"
	snapshot := DSHSnapshot{Generation: 1, CollectedAtMS: at, Sources: []CursorSourceStatus{{Provider: "dsh", SourceKey: "dsh.logs", SourceType: "jsonl", State: "available", CoverageState: "exact", CheckpointKind: "filesystem_scan", RowCount: 1, LastAttemptAtMS: at, LastSuccessAtMS: &at, UpdatedAtMS: at}}, Sessions: []DSHSession{{ExternalSessionID: "dsh-1", DisplayTitle: "未命名会话", TitleSource: "fallback", ProjectKey: strings.Repeat("a", 64), ProjectDisplayName: "demo", CreatedAtMS: at - 2000, LastActivityAtMS: at, RequestCount: 1, CoverageState: "exact", UpdatedAtMS: at}}, UsageEvents: []DSHUsageEvent{{EventID: strings.Repeat("b", 64), ExternalSessionID: "dsh-1", OccurredAtMS: at, StartedAtMS: new(at - 1000), EndedAtMS: &at, ModelProvider: "deepseek-account", ModelKey: &model, CacheReadKnown: true, CacheWriteKnown: true, TotalKnown: true, InputTokens: 100, OutputTokens: 200, CachedReadTokens: 300, TotalTokens: 600, UpdatedAtMS: at}}}
	if err := r.ReplaceDSHSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	page, err := r.ReportingPage(ctx, "dsh", ReportingSource{HomeID: "opaque-home", Partition: "local"}, "")
	if err != nil || len(page.Sessions) != 1 {
		t.Fatalf("export: %+v %v", page, err)
	}
	s := page.Sessions[0]
	s.Revision = 1
	if err := reportingv1.ValidateSnapshot(s); err != nil {
		t.Fatalf("contract invalid: %v %+v", err, s)
	}
	c := s.Contributions[0]
	if c.CostMicroUSD == nil || *c.CostMicroUSD != 136 || *c.InputTokens != 400 || c.ReasoningTokens != nil || s.CacheUsage == nil {
		t.Fatalf("facts lost: %+v", c)
	}
	filtered, err := r.ReportingPage(ctx, "dsh", ReportingSource{HomeID: "opaque-home", Partition: "local", StartAtMS: at - 500}, "")
	if err != nil {
		t.Fatal(err)
	}
	f := filtered.Sessions[0]
	f.Revision = 1
	if err := reportingv1.ValidateSnapshot(f); err != nil || f.CacheUsage.Reason != "history_filtered" {
		t.Fatalf("filtered: %v", err)
	}
	// Unavailable source is not evidence that previously indexed history was deleted.
	snapshot.Sessions = nil
	snapshot.UsageEvents = nil
	snapshot.Generation = 2
	snapshot.Sources[0].State = "unavailable"
	snapshot.Sources[0].CoverageState = "unknown"
	if err := r.ReplaceDSHSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	read, err := r.DSHSnapshot(ctx)
	if err != nil || len(read.Sessions) != 1 || len(read.UsageEvents) != 1 {
		t.Fatalf("history lost: %v", err)
	}
}
