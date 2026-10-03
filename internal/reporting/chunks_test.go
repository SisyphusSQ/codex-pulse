package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func largeSnapshot() reportingv1.SessionSnapshot {
	s := testSnapshot()
	s.Contributions = nil
	for index := range 20032 {
		c := reportingv1.Contribution{ObservedAtMS: new(int64(index)), TotalTokens: new(int64(1)), CostStatus: "unpriced"}
		c.ID = reportingv1.ContributionID(s.Provider, s.SessionID, c, 0)
		s.Contributions = append(s.Contributions, c)
	}
	for index := range 16691 {
		i := reportingv1.Invocation{ObservedAtMS: int64(index), Kind: "tool", Name: "exec_command", Outcome: "unknown"}
		i.ID = reportingv1.InvocationID(s.Provider, s.SessionID, i, 0)
		s.Invocations = append(s.Invocations, i)
	}
	return s
}
func TestLargeSnapshotDurablePartsAndAcknowledgement(t *testing.T) {
	s, path := testState(t)
	ctx := t.Context()
	snapshot := largeSnapshot()
	if err := s.Enqueue(ctx, "center", "sweep", snapshot); err != nil {
		t.Fatal(err)
	}
	first, err := s.next(ctx, "center")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	s, err = OpenState(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	retry, err := s.next(ctx, "center")
	if err != nil || retry.BatchID != first.BatchID || string(retry.Body) != string(first.Body) {
		t.Fatal("restart changed immutable part", err)
	}
	parts := []reportingv1.SessionSnapshot{}
	for {
		item, err := s.next(ctx, "center")
		if errors.Is(err, gorm.ErrRecordNotFound) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(item.Body) > reportingv1.MaxBodyBytes {
			t.Fatal("body exceeds budget")
		}
		var batch reportingv1.Batch
		if err := json.Unmarshal(item.Body, &batch); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, batch.Sessions[0])
		if err := s.Ack(ctx, item, reportingv1.Receipt{Version: 1, BatchID: item.BatchID, ReceivedAtMS: 3000}); err != nil {
			t.Fatal(err)
		}
		var checkpoint checkpoint
		if err := s.db.View(ctx, func(_ context.Context, db *gorm.DB) error { return db.First(&checkpoint).Error }); err != nil {
			t.Fatal(err)
		}
		if len(parts) < batch.Sessions[0].Chunk.Count && checkpoint.AcknowledgedRevision != 0 {
			t.Fatal("partial snapshot acknowledged")
		}
	}
	full, err := reportingv1.AssembleSnapshot(parts)
	if err != nil || len(full.Contributions) != 20032 || len(full.Invocations) != 16691 {
		t.Fatal("facts lost", err)
	}
	parts[0].Contributions[0].TotalTokens = new(int64(99))
	if _, err := reportingv1.AssembleSnapshot(parts); err == nil {
		t.Fatal("digest corruption accepted")
	}
}

func TestFullResendPreservesQueueRevisionAndRestartProgress(t *testing.T) {
	s, path := testState(t)
	ctx := t.Context()
	snap := testSnapshot()
	if err := s.Enqueue(ctx, "center", "old", snap); err != nil {
		t.Fatal(err)
	}
	first, _ := s.next(ctx, "center")
	if err := s.startFullSync(ctx, "center"); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(ctx, "center", "new", snap); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := OpenState(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	var rows []queued
	var task fullSync
	if err := s.db.View(ctx, func(_ context.Context, db *gorm.DB) error {
		if err := db.Order("seq").Find(&rows).Error; err != nil {
			return err
		}
		return db.First(&task).Error
	}); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].BatchID != first.BatchID || rows[1].Revision != rows[0].Revision+1 || task.State != "running" || task.ExportedSessions != 1 {
		t.Fatal("resend reset identity or discarded queue")
	}
	if err := s.finishFullSync(ctx, "center"); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err := s.Ack(ctx, row, reportingv1.Receipt{Version: 1, BatchID: row.BatchID, ReceivedAtMS: 4000}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.finishFullSync(ctx, "center"); err != nil {
		t.Fatal(err)
	}
	_ = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error { return db.First(&task).Error })
	if task.State != "completed" || task.AcknowledgedBatches != 2 {
		t.Fatal("task completion not persisted")
	}
}
