package reporting_srv

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestPublicationCancellationRestartAndImmutableFinalReceipt(t *testing.T) {
	s, db, clients := reportingFixture(t)
	full := centerfixture.Snapshot()
	centerfixture.SendSnapshot(t, s, clients[0], full)
	full.Revision = 2
	full.CollectedAtMS = 4000
	for i := range 21624 {
		full.Contributions = append(full.Contributions, centerfixture.Contribution(1, int64(2000+i)))
	}
	parts, err := reportingv1.SplitSnapshot(full)
	if err != nil {
		t.Fatal(err)
	}
	var final reportingv1.Batch
	for i, part := range parts {
		batch := reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{part}}
		_, err = s.Accept(t.Context(), clients[0], batch)
		if i < len(parts)-1 && err != nil {
			t.Fatal(err)
		}
		if i == len(parts)-1 {
			final = batch
			if !errors.Is(err, ErrPublicationPending) {
				t.Fatal(err)
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := db.Callback().Create().After("gorm:create").Register("test-cancel-staging", func(tx *gorm.DB) {
		if tx.Statement.Table == "pulse_usage_versions" {
			cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessPublication(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was swallowed", err)
	}
	_ = db.Callback().Create().Remove("test-cancel-staging")
	if _, total := readSession(t, db); total != 100 {
		t.Fatal("incomplete version visible", total)
	}
	var receiptCount int64
	if err := db.Model(&reporting_do.Batch{}).Where("batch_id = ?", final.ID).Count(&receiptCount).Error; err != nil || receiptCount != 0 {
		t.Fatal("premature receipt", err, receiptCount)
	}
	if err := db.Model(&reporting_do.PublicationJob{}).Where("state = ?", "pending").Update("next_attempt_at_ms", 0).Error; err != nil {
		t.Fatal(err)
	}
	restarted := NewReporting(s.repository)
	if _, err := restarted.Accept(t.Context(), clients[0], final); !errors.Is(err, ErrPublicationPending) {
		t.Fatal("restart lost pending batch", err)
	}
	if processed, err := restarted.ProcessPublication(t.Context()); !processed || err != nil {
		t.Fatal("resume failed", processed, err)
	}
	first, err := restarted.Accept(t.Context(), clients[0], final)
	if err != nil {
		t.Fatal(err)
	}
	again, err := restarted.Accept(t.Context(), clients[0], final)
	if err != nil || first != again {
		t.Fatal("receipt not idempotent", err)
	}
	meta, total := readSession(t, db)
	if meta.CanonicalRevision != 2 || total != 21724 {
		t.Fatal("wrong publication", meta.CanonicalRevision, total)
	}
	canonical, err := s.repository.Canonical(t.Context(), meta.ID)
	if err != nil || len(canonical.Payload) < 100000 {
		t.Fatal("manifest not resolved", err)
	}
	if err := s.repository.CollectVersions(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repository.Canonical(t.Context(), meta.ID); err != nil {
		t.Fatal("cleanup removed live payload", err)
	}
	// 小型完整纠正也必须通过后台发布，不能同步重写已有大投影。
	full.Revision = 3
	full.CollectedAtMS = 5000
	full.Contributions = full.Contributions[:1]
	correction := reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{full}}
	if _, err := s.Accept(t.Context(), clients[0], correction); !errors.Is(err, ErrPublicationPending) {
		t.Fatal("large correction bypassed publication", err)
	}
	if _, total := readSession(t, db); total != 21724 {
		t.Fatal("correction leaked before commit", total)
	}
	if _, err := s.ProcessPublication(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Accept(t.Context(), clients[0], correction); err != nil {
		t.Fatal(err)
	}
	if _, total := readSession(t, db); total != 100 {
		t.Fatal("complete correction failed", total)
	}
}
