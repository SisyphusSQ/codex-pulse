package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationSchemaVersionIncludesLightUsageSummaryIndex(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != applicationSchemaV21Version {
		t.Fatalf("applicationSchemaVersion = %d, want 21", applicationSchemaVersion)
	}
	database := openTestDatabase(t)
	seedApplicationSchemaV16(t, database)
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
		return "/tmp/application-v16-before-v21.db", nil
	}
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("run(v16->v21) error = %v", err)
	}
	if report.FromVersion != 16 || report.TargetVersion != 21 ||
		!equalInts(report.AppliedVersions, []int{17, 18, 19, 20, 21}) || backupVersions != [2]int{16, 21} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 21, 21)
	if err := database.View(t.Context(), func(_ context.Context, connection *gorm.DB) error {
		for _, column := range lightModelMigrationColumns {
			if !connection.Migrator().HasColumn(column.model, column.column) {
				t.Errorf("v17 column %s.%s is missing", column.table, column.column)
			}
		}
		if !connection.Migrator().HasTable("light_invocation_events") {
			t.Error("v20 table light_invocation_events is missing")
		}
		for _, index := range []string{"idx_light_invocation_observed", "idx_light_invocation_kind_name"} {
			if !connection.Migrator().HasIndex("light_invocation_events", index) {
				t.Errorf("v20 index %q is missing", index)
			}
		}
		if !connection.Migrator().HasIndex(
			"light_token_timed",
			"idx_light_token_timed_usage_summary",
		) {
			t.Error("v21 index idx_light_token_timed_usage_summary is missing")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v17 schema: %v", err)
	}
}

func TestApplicationSchemaV16AddsLightIndexTables(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV15(t, database)
	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:16]
	runner.verifyCurrent = verifyApplicationSchemaV16
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context,
		fromVersion int,
		targetVersion int,
		_ func(storesqlite.BackupProgress),
	) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v15-before-v16.db", nil
	}
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("run(v15->v16) error = %v", err)
	}
	if report.FromVersion != 15 || report.TargetVersion != 16 ||
		!equalInts(report.AppliedVersions, []int{16}) || backupVersions != [2]int{15, 16} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 16, 16)
	if err := database.View(t.Context(), func(_ context.Context, connection *gorm.DB) error {
		for _, table := range []string{"light_index_state", "light_sessions", "light_token_scans", "light_token_daily", "light_token_timed"} {
			if !connection.Migrator().HasTable(table) {
				t.Errorf("v16 table %q is missing", table)
			}
		}
		for _, index := range []string{"idx_light_sessions_updated", "idx_light_sessions_cwd", "idx_light_token_daily_day", "idx_light_token_timed_observed"} {
			if !connection.Migrator().HasIndex("light_sessions", index) &&
				!connection.Migrator().HasIndex("light_token_daily", index) &&
				!connection.Migrator().HasIndex("light_token_timed", index) {
				t.Errorf("v16 index %q is missing", index)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v16 schema: %v", err)
	}
}

func TestApplicationSchemaV16MigrationRollsBackAtomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV15(t, database)
	want := errors.New("injected v16 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	catalog = catalog[:16]
	catalog[15].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := storeschema.EnsureObjects(ctx, transaction, storelight.SchemaObjects()); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.verifyCurrent = verifyApplicationSchemaV16
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v15-before-failed-v16.db", nil
	}
	if _, err := runner.run(t.Context()); !errors.Is(err, want) {
		t.Fatalf("run(failed v16) error = %v, want injected", err)
	}
	assertMigrationVersionAndHistory(t, database, 15, 15)
	if err := database.View(t.Context(), func(_ context.Context, connection *gorm.DB) error {
		for _, table := range []string{"light_index_state", "light_sessions", "light_token_scans", "light_token_daily", "light_token_timed"} {
			if connection.Migrator().HasTable(table) {
				t.Errorf("failed v16 migration left table %q behind", table)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v16 rollback: %v", err)
	}
}

func seedApplicationSchemaV15(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:15]
	runner.verifyCurrent = verifyApplicationSchemaV15
	if report, err := runner.run(t.Context()); err != nil || report.TargetVersion != 15 {
		t.Fatalf("seed application schema v15 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 15, 15)
}

func seedApplicationSchemaV16(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:16]
	runner.verifyCurrent = verifyApplicationSchemaV16
	if report, err := runner.run(t.Context()); err != nil || report.TargetVersion != 16 {
		t.Fatalf("seed application schema v16 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 16, 16)
}
