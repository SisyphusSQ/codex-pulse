package store

import (
	"context"
	"errors"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

func TestApplicationSchemaV10ChecksumIsFrozen(t *testing.T) {
	const want = "70ec74aa0bf38fe3e9a6256bdbc47f6cad7a1029872ba7e5f065a60289985f2c"
	if got := applicationSchemaV10Checksum(); got != want {
		t.Fatalf("applicationSchemaV10Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV10AddsTypedSourceFailureMetrics(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != 12 {
		t.Fatalf("applicationSchemaVersion = %d, want 12", applicationSchemaVersion)
	}
	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 12, 12)
	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, field := range []struct {
			model any
			name  string
		}{
			{&sourceStateModel{}, "last_failure_code"},
			{&sourceAttemptModel{}, "failure_code"},
			{&sourceAttemptModel{}, "attempt_count"},
			{&sourceAttemptModel{}, "response_bytes"},
			{&sourceAttemptModel{}, "retry_at_ms"},
		} {
			if !connection.Migrator().HasColumn(field.model, field.name) {
				t.Errorf("schema v10 column %s missing", field.name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect schema v10: %v", err)
	}
}

func TestApplicationMigrationUpgradesV9ToV10AndEnforcesFailureAllowlist(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV9(t, database)
	repository := NewRepository(database)
	stateID := "v9-preserved-source"
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Table("source_state").Create(map[string]any{
			"source_instance_id": stateID, "source_type": "wham_quota", "scope_key": "default",
			"consecutive_failures": 0, "freshness_state": string(SourceFreshnessUnknown),
			"cursor_version": 0, "updated_at_ms": 10,
		}).Error
	})
	if err != nil {
		t.Fatalf("seed source state v9: %v", err)
	}
	legacyAttempt := SourceAttempt{
		RequestID: "v9-preserved-attempt", SourceInstanceID: stateID,
		StartedAtMS: 10, FinishedAtMS: 11, Outcome: SourceAttemptSucceeded,
	}
	err = database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Table("source_attempts").Create(map[string]any{
			"request_id": legacyAttempt.RequestID, "source_instance_id": stateID,
			"started_at_ms": 10, "finished_at_ms": 11, "outcome": string(SourceAttemptSucceeded),
		}).Error
	})
	if err != nil {
		t.Fatalf("seed source attempt v9: %v", err)
	}

	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:10]
	runner.verifyCurrent = verifyApplicationSchemaV10
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v9-before-v10.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run(v9->v10) error = %v", err)
	}
	if report.FromVersion != 9 || report.TargetVersion != 10 || !equalInts(report.AppliedVersions, []int{10}) {
		t.Fatalf("migration report = %#v", report)
	}
	assertMigrationVersionAndHistory(t, database, 10, 10)
	attempts, err := repository.ListSourceAttempts(context.Background(), stateID, 10)
	if err != nil || len(attempts) != 1 || attempts[0].AttemptCount != 1 || attempts[0].ResponseBytes != 0 {
		t.Fatalf("legacy attempt after v10 = %#v, %v", attempts, err)
	}

	err = database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Model(&sourceAttemptModel{}).
			Where("request_id = ?", legacyAttempt.RequestID).
			UpdateColumn("failure_code", "raw-secret-or-unknown-code").Error
	})
	if err == nil {
		t.Fatal("database accepted non-allowlisted failure_code")
	}
}

func TestApplicationMigrationRollsBackFailedV10Atomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV9(t, database)
	want := errors.New("injected v10 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	catalog[9].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := addSourceFailureColumns(ctx, transaction, 1); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v9-before-failed-v10.db", nil
	}
	if _, err := runner.run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("run(failed v10) error = %v, want injected failure", err)
	}
	assertMigrationVersionAndHistory(t, database, 9, 9)
	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		if connection.Migrator().HasColumn(&sourceStateModel{}, "last_failure_code") ||
			connection.Migrator().HasColumn(&sourceAttemptModel{}, "failure_code") {
			t.Fatal("failed v10 migration left columns behind")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect v10 rollback: %v", err)
	}
}

func seedApplicationSchemaV9(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:9]
	runner.verifyCurrent = verifyApplicationSchemaV9
	if report, err := runner.run(context.Background()); err != nil || report.TargetVersion != 9 {
		t.Fatalf("seed application schema v9 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 9, 9)
}

func verifyApplicationSchemaV9(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjects, retentionSchemaObjects,
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects, quotaSchemaObjects,
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
