package reporting_srv

import (
	"context"
	"fmt"
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
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for range 5 {
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
