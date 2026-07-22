package store

import (
	"context"
	"errors"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

func TestApplicationSchemaV12CreatesResetCreditsAndRefreshScheduling(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != applicationSchemaV17Version {
		t.Fatalf("applicationSchemaVersion = %d, want 17", applicationSchemaVersion)
	}
	const wantChecksum = "9ab44dccdb1467d2ad8bdca4cf3703158e09c80b23506247e66735c099912bd0"
	if got := applicationSchemaV12Checksum(); got != wantChecksum {
		t.Fatalf("applicationSchemaV12Checksum() = %q, want frozen %q", got, wantChecksum)
	}
	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for _, object := range quotaScheduleSchemaObjects {
			exists, err := verifySchemaObject(ctx, connection, object)
			if err != nil {
				return err
			}
			if !exists {
				t.Errorf("schema v12 %s %q missing", object.objectType, object.name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect schema v12: %v", err)
	}
}

func TestApplicationMigrationUpgradesV11ThroughCurrentWithoutChangingQuotaFacts(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV11(t, database)
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
		return "/tmp/application-v11-before-v12.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run(v11->v12) error = %v", err)
	}
	if report.FromVersion != 11 || report.TargetVersion != applicationSchemaVersion ||
		!equalInts(report.AppliedVersions, []int{12, 13, 14, 15, 16, 17}) || backupVersions != [2]int{11, 17} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
	if values, err := NewRepository(database).ListQuotaCurrent(
		context.Background(), QuotaAccountScopeDefault, 1,
	); err != nil || len(values) != 0 {
		t.Fatalf("existing empty quota projection = %#v, %v", values, err)
	}
}

func TestApplicationMigrationRollsBackFailedV12Atomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV11(t, database)
	want := errors.New("injected v12 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	catalog[11].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := ensureSchemaObjects(ctx, transaction, quotaScheduleSchemaObjects[:1]); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v11-before-failed-v12.db", nil
	}
	if _, err := runner.run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("run() error = %v, want injected failure", err)
	}
	assertMigrationVersionAndHistory(t, database, 11, 11)
	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, object := range quotaScheduleSchemaObjects {
			if object.objectType == "table" && connection.Migrator().HasTable(object.name) {
				t.Errorf("failed v12 migration left table %q", object.name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect v12 rollback: %v", err)
	}
}

func seedApplicationSchemaV11(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:11]
	runner.verifyCurrent = verifyApplicationSchemaV11
	if report, err := runner.run(context.Background()); err != nil || report.TargetVersion != 11 {
		t.Fatalf("seed application schema v11 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 11, 11)
}

func verifyApplicationSchemaV11(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjectsThroughV12(), retentionSchemaObjects,
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects, quotaSchemaObjects, quotaProjectionSchemaObjects,
	} {
		for _, object := range objects {
			exists, err := verifySchemaObject(ctx, transaction, object)
			if err != nil {
				return err
			}
			if !exists {
				return ErrSchemaContract
			}
		}
	}
	return verifySourceFailureColumns(transaction)
}
