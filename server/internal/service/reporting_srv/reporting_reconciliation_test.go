package reporting_srv

import (
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestReportingReconcileCursorPreviewApplyIdempotenceAndRollback(t *testing.T) {
	s, db, clients := reportingFixture(t)
	old := cursorDashboardSnapshot(10)
	centerfixture.SendSnapshot(t, s, clients[0], old)
	// 模拟旧中心已收到新版来源，却仍将 canonical 冻结在 revision 1。
	updated := cursorDashboardSnapshot(100)
	updated.Revision = 2
	updated.CollectedAtMS++
	payload, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := sourceDigest(updated)
	if err != nil {
		t.Fatal(err)
	}
	id := reportingv1.Key(clients[0].ID, "cursor", old.HomeID, old.SessionID)
	if err := db.Model(&reporting_do.SessionSource{}).Where("id = ?", id).Updates(map[string]any{"payload": string(payload), "revision": 2, "collected_at_ms": updated.CollectedAtMS, "digest": digest}).Error; err != nil {
		t.Fatal(err)
	}
	var sourceBefore reporting_do.SessionSource
	if err := db.Where("id = ?", id).Take(&sourceBefore).Error; err != nil {
		t.Fatal(err)
	}
	var batchesBefore int64
	if err := db.Model(&reporting_do.Batch{}).Count(&batchesBefore).Error; err != nil {
		t.Fatal(err)
	}
	preview, err := s.ReconcileCursor(t.Context(), "", 1, false)
	if err != nil || preview.Processed != 1 || preview.Changed != 1 || preview.Applied {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	_, total := readSession(t, db)
	if total != 10 {
		t.Fatal("preview mutated canonical")
	}
	injected := errors.New("usage write failed")
	callback := "reconcile-injected-failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "pulse_usage" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := s.ReconcileCursor(t.Context(), "", 1, true)
	if !errors.Is(err, injected) || failed.Next != "" || failed.Processed != 0 || failed.Changed != 0 {
		t.Fatalf("failure advanced checkpoint: %+v %v", failed, err)
	}
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	meta, total := readSession(t, db)
	if total != 10 || meta.CanonicalRevision != 1 {
		t.Fatal("failed session was not rolled back")
	}
	applied, err := s.ReconcileCursor(t.Context(), "", 1, true)
	if err != nil || applied.Changed != 1 || applied.Processed != 1 || !applied.Applied {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	meta, total = readSession(t, db)
	if total != 100 || meta.CanonicalRevision != 2 {
		t.Fatal("historical projection not restored")
	}
	again, err := s.ReconcileCursor(t.Context(), "", 1, true)
	if err != nil || again.Changed != 0 {
		t.Fatalf("repeated reconciliation not idempotent: %+v %v", again, err)
	}
	end, err := s.ReconcileCursor(t.Context(), applied.Next, 1, true)
	if err != nil || end.Processed != 0 {
		t.Fatalf("pagination: %+v %v", end, err)
	}
	var sourceAfter reporting_do.SessionSource
	var batchesAfter int64
	if err := db.Where("id = ?", id).Take(&sourceAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&reporting_do.Batch{}).Count(&batchesAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sourceBefore, sourceAfter) || batchesBefore != batchesAfter {
		t.Fatal("reconciliation rewrote source or receipts")
	}
}

func TestReportingReconcileCursorDoesNotTouchOtherProviders(t *testing.T) {
	s, db, clients := reportingFixture(t)
	centerfixture.SendSnapshot(t, s, clients[0], centerfixture.Snapshot())
	result, err := s.ReconcileCursor(t.Context(), "", 32, true)
	if err != nil || result.Processed != 0 {
		t.Fatalf("non-Cursor session selected: %+v %v", result, err)
	}
	_, total := readSession(t, db)
	if total != 100 {
		t.Fatal("other provider changed")
	}
}
