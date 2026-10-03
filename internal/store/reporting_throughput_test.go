package store

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

func TestReportingThroughputUsesNativeLifetimeAndBoundsSafeRecentTurns(t *testing.T) {
	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(t.Context(), "one", identity, throughput.ParserVersion, 300000000)
	if err != nil {
		t.Fatal(err)
	}
	batch := storelight.LightTokenBatch{SessionID: "one", Generation: generation, UpdatedAtMS: 300000001, Activate: true, Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, OutputTokens: 610}}
	for i := range 61 {
		id := "private-turn-do-not-upload-" + strings.Repeat("x", i+1)
		start := int64(1000 + i*3600000)
		duration := int64(1000 + (i%2)*9000)
		end, at, delta := start+duration, start+100, int64(10)
		offset := int64(10 + i*30)
		batch.TurnEvents = append(batch.TurnEvents, throughput.Event{Offset: offset, Kind: "start", TurnID: &id, AtMS: &start, TimeSource: "log_timestamp"}, throughput.Event{Offset: offset + 10, Kind: "usage", AtMS: &at, OutputDelta: &delta, OutputObserved: true}, throughput.Event{Offset: offset + 20, Kind: "complete", TurnID: &id, AtMS: &end, DurationMS: &duration, TimeSource: "log_timestamp"})
		batch.TimedDeltas = append(batch.TimedDeltas, storelight.LightTokenTimedDelta{SourceOffset: offset + 10, ObservedAtMS: at, OutputTokens: delta})
	}
	if err := repository.CommitLightTokenBatch(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	native, err := repository.SessionAnalytics(t.Context(), SessionAnalyticsDetailFilter{SessionID: "one", TurnLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	source := ReportingSource{HomeID: "synthetic-home", Path: identity.Home.Path, DeviceID: identity.Home.DeviceID, Inode: identity.Home.Inode}
	page, err := repository.coreTestRepository.Repository.ReportingPage(t.Context(), "codex", source, "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := page.Sessions[0]
	capsule := snapshot.Throughput
	if capsule == nil || capsule.TurnsTotal != 61 || len(capsule.RecentTurns) != 50 || !reflect.DeepEqual(capsule.Measures, reportingThroughputMeasures(*native.Record.Throughput)) {
		t.Fatal("native lifetime metrics or bounded recent evidence changed")
	}
	snapshot.Revision = 1
	if err := (reportingv1.Batch{Version: 1, ID: "00000000-0000-0000-0000-000000000001", Sessions: []reportingv1.SessionSnapshot{snapshot}}).Validate(); err != nil {
		t.Fatal(err)
	}
	bytes, _ := json.Marshal(snapshot)
	for _, forbidden := range []string{"private-turn-do-not-upload", "source_offset", "generation", "turn_id", "auth.json", identity.Home.Path} {
		if strings.Contains(string(bytes), forbidden) {
			t.Fatal("unsafe throughput metadata exported")
		}
	}
	source.StartAtMS = 1000
	page, err = repository.coreTestRepository.Repository.ReportingPage(t.Context(), "codex", source, "")
	if err != nil || page.Sessions[0].Throughput.Measures.Reason != "history_filtered" || page.Sessions[0].Throughput.Measures.CoverageKnown || len(page.Sessions[0].Throughput.RecentTurns) != 0 {
		t.Fatal("restricted history fabricated a lifetime metric", err)
	}
	if _, err = repository.StartLightTokenRebuild(t.Context(), "one", identity, throughput.ParserVersion, 300000010); err != nil {
		t.Fatal(err)
	}
	source.StartAtMS = 0
	page, err = repository.coreTestRepository.Repository.ReportingPage(t.Context(), "codex", source, "")
	if err != nil || page.Sessions[0].Throughput.Measures.Reason != "index_incomplete" || page.Sessions[0].Throughput.Measures.Status != "partial" {
		t.Fatal("pending rebuild claimed complete throughput", err)
	}
}
