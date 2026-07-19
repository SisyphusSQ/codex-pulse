package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationSchemaV15AddsQuotaProjectionPerformanceIndex(t *testing.T) {
	t.Parallel()
	if applicationSchemaVersion != applicationSchemaV15Version {
		t.Fatalf("applicationSchemaVersion = %d, want 15", applicationSchemaVersion)
	}
	const wantChecksum = "e0d74e9fea57fd72ee4a96e45f60ebb60db2d1dd291168fd7b2fcf74021e10f2"
	if got := applicationSchemaV15Checksum(); got != wantChecksum {
		t.Fatalf("applicationSchemaV15Checksum() = %q, want frozen %q", got, wantChecksum)
	}

	database := openTestDatabase(t)
	seedApplicationSchemaV14(t, database)
	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context,
		fromVersion int,
		targetVersion int,
		_ func(storesqlite.BackupProgress),
	) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v14-before-v15.db", nil
	}
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("run(v14->v15) error = %v", err)
	}
	if report.FromVersion != 14 || report.TargetVersion != 15 ||
		!equalInts(report.AppliedVersions, []int{15}) || backupVersions != [2]int{14, 15} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 15, 15)
	if err := database.View(t.Context(), func(_ context.Context, connection *gorm.DB) error {
		if !connection.Migrator().HasIndex(&quotaObservationModel{}, "idx_quota_observations_projection") {
			t.Fatal("v15 quota projection performance index is missing")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v15 index: %v", err)
	}
}

func TestApplicationSchemaV15IndexMigrationRollsBackAtomically(t *testing.T) {
	t.Parallel()
	database := openTestDatabase(t)
	seedApplicationSchemaV14(t, database)
	want := errors.New("injected v15 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	catalog[14].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := ensureSchemaObjects(ctx, transaction, quotaPerformanceSchemaObjects); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v14-before-failed-v15.db", nil
	}
	if _, err := runner.run(t.Context()); !errors.Is(err, want) {
		t.Fatalf("run(failed v15) error = %v, want injected", err)
	}
	assertMigrationVersionAndHistory(t, database, 14, 14)
	if err := database.View(t.Context(), func(_ context.Context, connection *gorm.DB) error {
		if connection.Migrator().HasIndex(&quotaObservationModel{}, "idx_quota_observations_projection") {
			t.Fatal("failed v15 migration left performance index behind")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v15 rollback: %v", err)
	}
}

func seedApplicationSchemaV14(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:14]
	runner.verifyCurrent = verifyApplicationSchemaV14
	if report, err := runner.run(t.Context()); err != nil || report.TargetVersion != 14 {
		t.Fatalf("seed application schema v14 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 14, 14)
}
