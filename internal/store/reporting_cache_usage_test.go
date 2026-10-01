package store

import (
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestReportingCacheUsageSameNativeCountersAndHistoryPrivacy(t *testing.T) {
	r := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := r.StartLightTokenRebuild(t.Context(), "one", identity, "parser-cache-fixture", 300000000)
	if err != nil {
		t.Fatal(err)
	}
	batch := storelight.LightTokenBatch{SessionID: "one", Generation: generation, UpdatedAtMS: 300000001, Activate: true, Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 1000, CachedInputTokens: 900, OutputTokens: 5}, TimedDeltas: []storelight.LightTokenTimedDelta{{SourceOffset: 10, ObservedAtMS: 2000, InputTokens: 1000, CachedInputTokens: 900, OutputTokens: 5}}}
	if err := r.CommitLightTokenBatch(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	native, err := r.SessionAnalytics(t.Context(), SessionAnalyticsDetailFilter{SessionID: "one", TurnLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	source := ReportingSource{HomeID: "synthetic-home", Path: identity.Home.Path, DeviceID: identity.Home.DeviceID, Inode: identity.Home.Inode}
	page, err := r.coreTestRepository.Repository.ReportingPage(t.Context(), "codex", source, "")
	if err != nil {
		t.Fatal(err)
	}
	s := page.Sessions[0]
	s.Revision = 1
	if s.CacheUsage == nil || native.Record.Rollup == nil || *s.CacheUsage.InputTokens != *native.Record.Rollup.InputTokens || *s.CacheUsage.CachedInputTokens != *native.Record.Rollup.CachedInputTokens {
		t.Fatal("native counters diverged")
	}
	if err := (reportingv1.Batch{Version: 1, ID: "00000000-0000-0000-0000-000000000001", Sessions: []reportingv1.SessionSnapshot{s}}).Validate(); err != nil {
		t.Fatal(err)
	}
	source.StartAtMS = 2500
	page, err = r.coreTestRepository.Repository.ReportingPage(t.Context(), "codex", source, "")
	if err != nil {
		t.Fatal(err)
	}
	c := page.Sessions[0].CacheUsage
	if c.InputTokens != nil || c.CachedInputTokens != nil || c.Reason != "history_filtered" {
		t.Fatal("pre-history counters leaked")
	}
}
