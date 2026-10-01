package reporting

import (
	"context"
	"encoding/json"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func TestThroughputOnlyChangeQueuesRevisionAndTombstoneKeepsNoMetrics(t *testing.T) {
	ctx := context.Background()
	s, path := testState(t)
	snapshot := testSnapshot()
	snapshot.SourceKind = "light_index"
	snapshot.Contributions[0].OutputTokens = new(int64(100))
	snapshot.Contributions[0].TotalTokens = new(int64(110))
	snapshot.Contributions[0].ID = reportingv1.ContributionID("codex", snapshot.SessionID, snapshot.Contributions[0], 0)
	snapshot.Throughput = &reportingv1.ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", TurnsTotal: 1, Measures: reportingv1.ThroughputMeasures{OutputTokens: new(int64(100)), ActiveDurationMS: new(int64(10000)), IncludedTurns: 1, CoverageKnown: true, Status: "complete", DurationSource: "duration_ms"}}
	if err := s.Enqueue(ctx, "center", "first", snapshot); err != nil {
		t.Fatal(err)
	}
	first, err := s.next(ctx, "center")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenState(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(ctx)
	retry, err := reopened.next(ctx, "center")
	if err != nil || string(retry.Body) != string(first.Body) {
		t.Fatal("restart changed throughput retry body", err)
	}
	if err := reopened.Ack(ctx, retry, reportingv1.Receipt{Version: 1, BatchID: retry.BatchID, ReceivedAtMS: 3000}); err != nil {
		t.Fatal(err)
	}
	snapshot.Throughput.Measures.ActiveDurationMS = new(int64(11000))
	if err := reopened.Enqueue(ctx, "center", "second", snapshot); err != nil {
		t.Fatal(err)
	}
	changed, err := reopened.next(ctx, "center")
	if err != nil || changed.Revision != first.Revision+1 {
		t.Fatal("throughput-only revision not reserved", err)
	}
	var body reportingv1.Batch
	if err := json.Unmarshal(changed.Body, &body); err != nil || *body.Sessions[0].Throughput.Measures.ActiveDurationMS != 11000 {
		t.Fatal("throughput update absent", err)
	}
	removed, err := reopened.removed(ctx, "center", "codex", snapshot.HomeID, "third")
	if err != nil || len(removed) != 1 || removed[0].Throughput != nil || !removed[0].Deleted {
		t.Fatal("tombstone retained throughput", err)
	}
}
