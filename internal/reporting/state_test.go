package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func testState(t *testing.T) (*State, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "reporting.db")
	state, err := OpenState(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close(context.Background()) })
	return state, path
}
func testSnapshot() reportingv1.SessionSnapshot {
	c := reportingv1.Contribution{ObservedAtMS: new(int64(1000)), InputTokens: new(int64(10)), TotalTokens: new(int64(10)), CostStatus: "unpriced"}
	c.ID = reportingv1.ContributionID("codex", "session", c, 0)
	return reportingv1.SessionSnapshot{Provider: "codex", HomeID: "private-home-key", SessionID: "session", ProjectID: "project", Revision: 1, CollectedAtMS: 2000, Contributions: []reportingv1.Contribution{c}}
}
func TestDurableQueueOnlyAdvancesAfterMatchingReceipt(t *testing.T) {
	ctx := context.Background()
	s, path := testState(t)
	snap := testSnapshot()
	if err := s.Enqueue(ctx, "center-one", "sweep", snap); err != nil {
		t.Fatal(err)
	}
	first, err := s.next(ctx, "center-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ack(ctx, first, reportingv1.Receipt{Version: 1, BatchID: "wrong"}); !errors.Is(err, ErrProtocol) {
		t.Fatal("wrong receipt acknowledged")
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenState(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(ctx)
	again, err := restarted.next(ctx, "center-one")
	if err != nil {
		t.Fatal(err)
	}
	if string(again.Body) != string(first.Body) || again.BatchID != first.BatchID {
		t.Fatal("retry body changed after restart")
	}
	snap.CollectedAtMS++
	if err := restarted.Enqueue(ctx, "center-one", "new-sweep", snap); err != nil {
		t.Fatal(err)
	}
	var rows []queued
	_ = restarted.db.View(ctx, func(_ context.Context, db *gorm.DB) error { return db.Find(&rows).Error })
	if len(rows) != 1 {
		t.Fatal("same facts enqueued twice")
	}
	if err := restarted.Ack(ctx, again, reportingv1.Receipt{Version: 1, BatchID: again.BatchID, ReceivedAtMS: 3000}); err != nil {
		t.Fatal(err)
	}
	snap.Title = "renamed"
	if err := restarted.Enqueue(ctx, "center-one", "sweep-3", snap); err != nil {
		t.Fatal(err)
	}
	changed, err := restarted.next(ctx, "center-one")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision != 2 {
		t.Fatal("revision did not survive restart")
	}
	var cp checkpoint
	_ = restarted.db.View(ctx, func(_ context.Context, db *gorm.DB) error { return db.First(&cp).Error })
	if cp.AcknowledgedRevision != 1 || cp.Revision != 2 {
		t.Fatal("export advanced unacknowledged progress")
	}
	var batch reportingv1.Batch
	if err := json.Unmarshal(changed.Body, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.Sessions[0].Contributions[0].ID != snap.Contributions[0].ID {
		t.Fatal("metadata revision changed contribution identity")
	}
}
func TestQueuePartitionCapacityAndExplicitDiscard(t *testing.T) {
	ctx := context.Background()
	s, _ := testState(t)
	snap := testSnapshot()
	if err := s.Enqueue(ctx, "old", "one", snap); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(ctx, "new", "one", snap); err != nil {
		t.Fatal(err)
	}
	if err := s.clearPending(ctx, "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.next(ctx, "old"); err != nil {
		t.Fatal("discard touched old center")
	}
	if _, err := s.next(ctx, "new"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("discard left queue")
	}
	if err := s.Enqueue(ctx, "new", "two", snap); err != nil {
		t.Fatal(err)
	}
	item, err := s.next(ctx, "new")
	if err != nil || item.Revision != 2 {
		t.Fatal("discard reset revision")
	}
	err = s.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		var q []queued
		for i := 0; i < QueueBatchBudget-2; i++ {
			q = append(q, queued{Partition: "full", BatchID: string(rune(0x1000 + i)), SourceKey: "key", Body: []byte("x")})
		}
		return db.CreateInBatches(q, 100).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	snap.Title = "capacity-change"
	if err := s.Enqueue(ctx, "new", "three", snap); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("capacity error: %v", err)
	}
	var cp checkpoint
	_ = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error { return db.Where("partition = ?", "new").First(&cp).Error })
	if cp.Revision != 2 {
		t.Fatal("failed enqueue advanced checkpoint")
	}
}
func TestPrivateStateStableHomeKeysAndFutureSchema(t *testing.T) {
	ctx := context.Background()
	s, path := testState(t)
	first := s.HomeID("/synthetic/home", "device", "1")
	if first == s.HomeID("/synthetic/other", "device", "1") {
		t.Fatal("Home collision")
	}
	status, err := s.Status(ctx)
	if err != nil || status.Enabled || status.State != "disabled" {
		t.Fatal("default enabled")
	}
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().Perm() != 0600 {
			t.Fatal("unsafe database permissions")
		}
	}
	if err := s.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		return db.Exec("UPDATE reporting_schema SET version=99").Error
	}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close(ctx)
	if _, err := OpenState(ctx, path); !errors.Is(err, ErrUnavailable) {
		t.Fatal("future schema opened")
	}
}

func TestCorruptQueueNeverSendsUnknownFieldsAndRemovalNeedsSweep(t *testing.T) {
	ctx := t.Context()
	state, _ := testState(t)
	snap := testSnapshot()
	if err := state.Enqueue(ctx, "center", "old-sweep", snap); err != nil {
		t.Fatal(err)
	}
	removed, err := state.removed(ctx, "center", "codex", snap.HomeID, "new-sweep")
	if err != nil || len(removed) != 1 || !removed[0].Deleted {
		t.Fatal("removal not identified")
	}
	if err := state.Enqueue(ctx, "center", "new-sweep", removed[0]); err != nil {
		t.Fatal(err)
	}
	removed, err = state.removed(ctx, "center", "codex", snap.HomeID, "later-sweep")
	if err != nil || len(removed) != 0 {
		t.Fatal("tombstone repeated indefinitely")
	}
	item, err := state.next(ctx, "center")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		return db.Model(&queued{}).Where("seq = ?", item.Seq).Update("body", []byte(`{"raw_jsonl":"forbidden"}`)).Error
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.next(ctx, "center"); !errors.Is(err, ErrProtocol) {
		t.Fatal("corrupt queue accepted")
	}
}
