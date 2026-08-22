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
	if report.FromVersion != 25 || report.TargetVersion != 30 ||
		!equalInts(report.AppliedVersions, []int{26, 27, 28, 29, 30}) || backupVersions != [2]int{25, 30} {
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

func TestApplicationMigrationV30AllowsGrokBotQuotaWithoutRewritingMonthlyHistory(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	v29Runner := migrationRunner{
		repository: NewRepository(database), catalog: applicationMigrations[:applicationSchemaV29Version],
		now:           func() time.Time { return time.UnixMilli(123) },
		verifyCurrent: func(context.Context, *gorm.DB) error { return nil },
	}
	if _, err := v29Runner.run(context.Background()); err != nil {
		t.Fatalf("run(v29) error = %v", err)
	}
	if err := database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).Create(&cursorDashboardQuotaObservationModel{
			Provider: "cursor", Generation: 11, LimitID: "cursor.models",
			UsedPercent: 7, CycleStartAtMS: 100, CycleEndAtMS: 400, ObservedAtMS: 150,
		}).Error
	}); err != nil {
		t.Fatalf("seed v29 monthly quota observation: %v", err)
	}
	if err := database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).Create(&cursorDashboardQuotaObservationModel{
			Provider: "cursor", Generation: 12, LimitID: "cursor.grok_bot",
			UsedPercent: 1, CycleStartAtMS: 200, CycleEndAtMS: 300, ObservedAtMS: 250,
		}).Error
	}); err == nil {
		t.Fatal("v29 quota CHECK must reject cursor.grok_bot")
	}

	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context, fromVersion int, targetVersion int, _ func(storesqlite.BackupProgress),
	) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v29-before-v30.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run(v30) error = %v", err)
	}
	if report.FromVersion != 29 || report.TargetVersion != 30 ||
		!equalInts(report.AppliedVersions, []int{30}) || backupVersions != [2]int{29, 30} {
		t.Fatalf("run(v30) report = %#v backup=%v", report, backupVersions)
	}

	if err := database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).Create(&cursorDashboardQuotaObservationModel{
			Provider: "cursor", Generation: 13, LimitID: "cursor.grok_bot",
			UsedPercent: 12.5, CycleStartAtMS: 200, CycleEndAtMS: 300, ObservedAtMS: 250,
		}).Error
	}); err != nil {
		t.Fatalf("write grok bot weekly observation after v30: %v", err)
	}
	var monthly cursorDashboardQuotaObservationModel
	var weekly cursorDashboardQuotaObservationModel
	if err := database.View(context.Background(), func(ctx context.Context, connection *gorm.DB) error {
		if err := connection.WithContext(ctx).Where("limit_id = ?", "cursor.models").First(&monthly).Error; err != nil {
			return err
		}
		return connection.WithContext(ctx).Where("limit_id = ?", "cursor.grok_bot").First(&weekly).Error
	}); err != nil {
		t.Fatalf("read migrated quota observations: %v", err)
	}
	if monthly.Generation != 11 || monthly.UsedPercent != 7 || monthly.CycleStartAtMS != 100 ||
		monthly.CycleEndAtMS != 400 {
		t.Fatalf("monthly history rewritten = %#v", monthly)
	}
	if weekly.Generation != 13 || weekly.UsedPercent != 12.5 || weekly.CycleStartAtMS != 200 ||
		weekly.CycleEndAtMS != 300 {
		t.Fatalf("weekly grok bot observation = %#v", weekly)
	}
}
