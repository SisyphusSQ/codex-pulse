package store

import (
	"reflect"
	"testing"

	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

func TestLightSessionThroughputAtomicPublicationAndGenerationFence(t *testing.T) {
	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(t.Context(), "one", identity, throughput.ParserVersion, 2000)
	if err != nil {
		t.Fatal(err)
	}
	start, end, delta, duration, turn := int64(1000), int64(11000), int64(100), int64(10000), "turn-one"
	batch := storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2100, Activate: true,
		Checkpoint:  storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, OutputTokens: 100},
		TimedDeltas: []storelight.LightTokenTimedDelta{{SourceOffset: 20, ObservedAtMS: 2000, OutputTokens: 100}},
		TurnEvents: []throughput.Event{
			{Offset: 10, Kind: "start", TurnID: &turn, AtMS: &start, TimeSource: "log_timestamp"},
			{Offset: 20, Kind: "usage", AtMS: pointerTo(int64(2000)), OutputDelta: &delta, OutputObserved: true},
			{Offset: 30, Kind: "complete", TurnID: &turn, AtMS: &end, DurationMS: &duration, TimeSource: "log_timestamp"},
		},
	}
	// A duplicate source position rejects the entire transaction, including tokens.
	bad := batch
	bad.TurnEvents = append(append([]throughput.Event(nil), batch.TurnEvents...), batch.TurnEvents[0])
	if err := repository.CommitLightTokenBatch(t.Context(), bad); err == nil {
		t.Fatal("duplicate event accepted")
	}
	drift := batch
	drift.TurnEvents = append([]throughput.Event(nil), batch.TurnEvents...)
	drift.TurnEvents[1].OutputDelta = pointerTo(int64(101))
	if err := repository.CommitLightTokenBatch(t.Context(), drift); err == nil {
		t.Fatal("throughput/token drift accepted")
	}
	scan, err := repository.PendingLightTokenScan(t.Context(), "one")
	if err != nil || scan.Checkpoint.DurableOffset != 0 {
		t.Fatalf("rollback scan=%+v err=%v", scan, err)
	}
	if err := repository.CommitLightTokenBatch(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	filter := SessionAnalyticsDetailFilter{SessionID: "one", TurnLimit: 20}
	detail, err := repository.SessionAnalytics(t.Context(), filter)
	if err != nil {
		t.Fatal(err)
	}
	stats := detail.Record.Throughput
	if stats == nil || stats.Status != "complete" || *stats.AverageMilliTPS != 10000 || *stats.OutputTokens != 100 || len(detail.ThroughputTurns) != 1 {
		t.Fatalf("throughput=%+v turns=%+v", stats, detail.ThroughputTurns)
	}
	page, err := repository.ListSessionAnalytics(t.Context(), SessionAnalyticsFilter{Limit: 20, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || !reflect.DeepEqual(page.Records[0].Throughput, stats) {
		t.Fatalf("list/detail mismatch: %+v", page.Records)
	}
	// Pending parser rebuild must not make the prior result look current/complete.
	if _, err := repository.StartLightTokenRebuild(t.Context(), "one", identity, throughput.ParserVersion, 3000); err != nil {
		t.Fatal(err)
	}
	detail, err = repository.SessionAnalytics(t.Context(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Record.Throughput.Status != "partial" || *detail.Record.Throughput.AverageMilliTPS != 10000 {
		t.Fatalf("pending result=%+v", detail.Record.Throughput)
	}
}
