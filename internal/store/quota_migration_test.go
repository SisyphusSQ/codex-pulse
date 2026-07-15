package store

import (
	"context"
	"errors"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

func TestApplicationSchemaV9ChecksumIsFrozen(t *testing.T) {
	const want = "b578ed511ccb376738a5638a8fe37afb637c38d9d0621ae2fd2a4c32d84642b4"
	if got := applicationSchemaV9Checksum(); got != want {
		t.Fatalf("applicationSchemaV9Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV9CreatesQuotaObservationFacts(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != 10 {
		t.Fatalf("applicationSchemaVersion = %d, want 10", applicationSchemaVersion)
	}
	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 10, 10)
	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, object := range quotaSchemaObjects {
			if object.objectType == "table" && !connection.Migrator().HasTable(object.name) {
				t.Errorf("schema v9 table %q missing", object.name)
			}
			if object.objectType == "index" {
				model := any(&quotaObservationModel{})
				if object.name == "idx_quota_observation_receipts_segment" {
					model = &quotaObservationReceiptModel{}
				}
				if !connection.Migrator().HasIndex(model, object.name) {
					t.Errorf("schema v9 index %q missing", object.name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect schema v9: %v", err)
	}
}

func TestApplicationMigrationUpgradesV8ToV9WithoutLosingFacts(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV8(t, database)
	repository := NewRepository(database)
	session := quotaTestSession("v8-preserved-session")
	if err := repository.UpsertFacts(context.Background(), FactBatch{Session: &session}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}

	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:9]
	runner.verifyCurrent = verifyApplicationSchemaV9
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(_ context.Context, from, target int, _ func(storesqlite.BackupProgress)) (string, error) {
		backupVersions = [2]int{from, target}
		return "/tmp/application-v8-before-v9.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if report.FromVersion != 8 || report.TargetVersion != 9 || !equalInts(report.AppliedVersions, []int{9}) ||
		backupVersions != [2]int{8, 9} || report.BackupPath == "" {
		t.Fatalf("run() report = %#v", report)
	}
	assertMigrationVersionAndHistory(t, database, 9, 9)
	if got, err := repository.Session(context.Background(), session.SessionID); err != nil || got.SessionID != session.SessionID {
		t.Fatalf("Session(preserved) = %#v, %v", got, err)
	}
}

func TestApplicationMigrationRollsBackFailedV9Atomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV8(t, database)
	want := errors.New("injected v9 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	catalog[8].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := ensureSchemaObjects(ctx, transaction, quotaSchemaObjects[:1]); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v8-before-failed-v9.db", nil
	}
	if _, err := runner.run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("run() error = %v, want injected failure", err)
	}
	assertMigrationVersionAndHistory(t, database, 8, 8)
	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, table := range []string{"quota_observations", "quota_observation_receipts"} {
			if connection.Migrator().HasTable(table) {
				t.Fatalf("failed v9 migration left %s behind", table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect rollback: %v", err)
	}
}

func seedApplicationSchemaV8(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:8]
	runner.verifyCurrent = verifyApplicationSchemaV8
	if report, err := runner.run(context.Background()); err != nil || report.TargetVersion != 8 {
		t.Fatalf("seed application schema v8 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 8, 8)
}

func verifyApplicationSchemaV8(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjects, retentionSchemaObjects,
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects,
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
	return nil
}
