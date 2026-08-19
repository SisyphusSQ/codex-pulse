package store

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationMigrationV26AddsCursorQuotaHistoryWithoutLosingDashboardSnapshot(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	v25Runner := migrationRunner{
		repository: NewRepository(database), catalog: applicationMigrations[:applicationSchemaV25Version],
		now:           func() time.Time { return time.UnixMilli(123) },
		verifyCurrent: func(context.Context, *gorm.DB) error { return nil },
	}
	if _, err := v25Runner.run(context.Background()); err != nil {
		t.Fatalf("run(v25) error = %v", err)
	}
	if err := database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).Create(&cursorDashboardSnapshotModel{
			Provider: "cursor", Generation: 7, CollectedAtMS: 150,
			WindowStartMS: 100, WindowEndMS: 200, BillingCycleEndMS: 300,
		}).Error
	}); err != nil {
		t.Fatalf("seed v25 dashboard snapshot: %v", err)
	}

	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context, fromVersion int, targetVersion int, _ func(storesqlite.BackupProgress),
	) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v25-before-v26.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run(v26) error = %v", err)
	}
	if report.FromVersion != 25 || report.TargetVersion != 29 ||
		!equalInts(report.AppliedVersions, []int{26, 27, 28, 29}) || backupVersions != [2]int{25, 29} {
		t.Fatalf("run(v27) report = %#v backup=%v", report, backupVersions)
	}

	if err := database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).Create(&cursorDashboardQuotaObservationModel{
			Provider: "cursor", Generation: 7, LimitID: "cursor.other_models",
			UsedPercent: 0, CycleStartAtMS: 100, CycleEndAtMS: 300, ObservedAtMS: 150,
		}).Error
	}); err != nil {
		t.Fatalf("write migrated zero quota observation: %v", err)
	}
	var dashboard cursorDashboardSnapshotModel
	var observation cursorDashboardQuotaObservationModel
	if err := database.View(context.Background(), func(ctx context.Context, connection *gorm.DB) error {
		if err := connection.WithContext(ctx).First(&dashboard, "provider = ?", "cursor").Error; err != nil {
			return err
		}
		return connection.WithContext(ctx).First(&observation, "provider = ?", "cursor").Error
	}); err != nil {
		t.Fatalf("read migrated facts: %v", err)
	}
	if dashboard.Generation != 7 || observation.LimitID != "cursor.other_models" ||
		observation.UsedPercent != 0 {
		t.Fatalf("dashboard = %#v, quota = %#v", dashboard, observation)
	}
}
