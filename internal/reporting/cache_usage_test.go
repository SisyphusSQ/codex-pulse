package reporting

import (
	"encoding/json"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func TestCacheUsageUpgradeRevisionDurableRetryAndCleanTombstone(t *testing.T) {
	s, path := testState(t)
	snapshot := testSnapshot()
	snapshot.SourceKind = "light_index"
	snapshot.Contributions[0].CachedTokens = new(int64(9))
	if err := s.Enqueue(t.Context(), "center", "old", snapshot); err != nil {
		t.Fatal(err)
	}
	old, err := s.next(t.Context(), "center")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ack(t.Context(), old, reportingv1.Receipt{Version: 1, BatchID: old.BatchID, ReceivedAtMS: 3000}); err != nil {
		t.Fatal(err)
	}
	snapshot.CacheUsage = &reportingv1.CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", InputTokens: snapshot.Contributions[0].InputTokens, CachedInputTokens: new(int64(9))}
	if err := s.Enqueue(t.Context(), "center", "new", snapshot); err != nil {
		t.Fatal(err)
	}
	queued, err := s.next(t.Context(), "center")
	if err != nil || queued.Revision != old.Revision+1 {
		t.Fatal("new capsule not queued", err)
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenState(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(t.Context())
	retry, err := reopened.next(t.Context(), "center")
	if err != nil || string(retry.Body) != string(queued.Body) {
		t.Fatal("retry changed", err)
	}
	var body reportingv1.Batch
	if err := json.Unmarshal(retry.Body, &body); err != nil || body.Sessions[0].CacheUsage == nil {
		t.Fatal("capsule missing", err)
	}
	removed, err := reopened.removed(t.Context(), "center", "codex", snapshot.HomeID, "later")
	if err != nil || len(removed) != 1 || removed[0].CacheUsage != nil {
		t.Fatal("tombstone retained counters", err)
	}
}
