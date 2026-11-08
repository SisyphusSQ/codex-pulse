package reporting_srv

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/fx"
	"gorm.io/gorm"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func TestVersionCollectionDrainsBacklogAndPreservesLiveLeasedAndRecentData(t *testing.T) {
	s, db, _ := reportingFixture(t)
	rows := make([]reporting_do.UsageVersion, 503)
	for i := range rows {
		generation := "orphan"
		if i >= 501 {
			generation = "live"
		}
		rows[i] = reporting_do.UsageVersion{Generation: generation, Usage: reporting_do.Usage{SessionKey: "session", ContributionID: fmt.Sprint(i)}}
	}
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&reporting_do.SessionPublication{SessionKey: "session", Generation: "live"}).Error; err != nil {
		t.Fatal(err)
	}
	job := reporting_do.PublicationJob{ID: "lease", LeaseUntilMS: time.Now().Add(time.Minute).UnixMilli()}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	for i := range 35 {
		part := reporting_do.PayloadPart{PayloadKey: fmt.Sprint(i), Body: []byte("part"), CreatedAtMS: time.Now().Add(-2 * time.Hour).UnixMilli()}
		if i == 34 {
			part.CreatedAtMS = time.Now().UnixMilli()
		}
		if err := db.Create(&part).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&reporting_do.PayloadOwner{ID: "owner", PayloadKey: "33"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repository.CollectVersions(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCount := func(model any, expected int64) {
		t.Helper()
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != expected {
			t.Fatalf("count %T: %d, want %d, err %v", model, count, expected, err)
		}
	}
	assertCount(&reporting_do.UsageVersion{}, 503)
	assertCount(&reporting_do.PayloadPart{}, 35)
	if err := db.Model(&job).Update("lease_until_ms", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repository.CollectVersions(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCount(&reporting_do.UsageVersion{}, 2)
	assertCount(&reporting_do.PayloadPart{}, 2)
}

func TestVersionCollectionRechecksPublicationBeforeDelete(t *testing.T) {
	s, db, _ := reportingFixture(t)
	row := reporting_do.UsageVersion{Generation: "pending", Usage: reporting_do.Usage{SessionKey: "session", ContributionID: "fact"}}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("test-publish-before-delete", func(tx *gorm.DB) {
		if tx.Statement.Table == "pulse_usage_versions" {
			if err := db.Create(&reporting_do.SessionPublication{SessionKey: "session", Generation: "pending"}).Error; err != nil {
				tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.repository.CollectVersions(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Remove("test-publish-before-delete"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&row).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("published version was removed: %d %v", count, err)
	}
}

func TestPublicationPollingDoesNotRepeatIdleVersionScan(t *testing.T) {
	s, db, _ := reportingFixture(t)
	var scans atomic.Int64
	polls := make(chan struct{}, 8)
	if err := db.Callback().Query().After("gorm:query").Register("test-count-background-queries", func(tx *gorm.DB) {
		switch tx.Statement.Table {
		case "pulse_usage_versions":
			scans.Add(1)
		case "pulse_publication_jobs":
			select {
			case polls <- struct{}{}:
			default:
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	app := fx.New(fx.NopLogger, fx.Supply(s), fx.Invoke(StartPublications))
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	deadline := time.NewTimer(9 * time.Second)
	defer deadline.Stop()
	for range 4 {
		select {
		case <-polls:
		case <-deadline.C:
			t.Fatal("publication polling stopped")
		}
	}
	if got := scans.Load(); got != 1 {
		t.Fatalf("idle version scans during second-by-second polling: %d", got)
	}
}

func TestPublicationCleanupBatchesAndPreservesPendingAndFailed(t *testing.T) {
	s, db, _ := reportingFixture(t)
	for _, state := range []string{"published", "pending", "failed"} {
		job := reporting_do.PublicationJob{ID: state, ClientID: "client", SourceKey: state, Revision: 1, State: state, Body: "retained until cleaned"}
		if err := db.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
		for i := range 256 {
			part := reporting_do.SnapshotChunk{ClientID: "client", SourceKey: state, Revision: 1, ChunkIndex: i, Payload: "{}", PayloadBytes: 2}
			if err := db.Create(&part).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for range 2 {
		if err := s.repository.CleanupPublication(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []string{"published", "pending", "failed"} {
		var count int64
		if err := db.Model(&reporting_do.SnapshotChunk{}).Where("source_key = ?", state).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		expected := int64(256)
		if state == "published" {
			expected = 0
		}
		if count != expected {
			t.Fatalf("%s chunks=%d", state, count)
		}
	}
	var job reporting_do.PublicationJob
	if err := db.Where("id = ?", "published").Take(&job).Error; err != nil || job.State != "completed" || job.Body != "" {
		t.Fatal("completion", job.State, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.repository.CleanupPublication(ctx); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestPublicationWakeInterruptsIdleWait(t *testing.T) {
	s, db, _ := reportingFixture(t)
	polls := make(chan struct{}, 8)
	if err := db.Callback().Query().After("gorm:query").Register("test-wake-poll", func(tx *gorm.DB) {
		if tx.Statement.Table == "pulse_publication_jobs" && strings.Contains(tx.Statement.SQL.String(), "next_attempt_at_ms <=") {
			polls <- struct{}{}
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.runPublications(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	for range 2 {
		select {
		case <-polls:
		case <-time.After(4 * time.Second):
			t.Fatal("idle poll missing")
		}
	}
	s.publicationWake <- struct{}{}
	select {
	case <-polls:
	case <-time.After(time.Second):
		t.Fatal("commit wake waited for idle timer")
	}
}
