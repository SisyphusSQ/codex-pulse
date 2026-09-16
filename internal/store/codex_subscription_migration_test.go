package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestCodexSubscriptionMigrationFreshDatabaseIsV33(t *testing.T) {
	t.Parallel()
	if applicationSchemaVersion != applicationSchemaV33Version {
		t.Fatalf("applicationSchemaVersion = %d, want 33", applicationSchemaVersion)
	}
	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 33, 33)
	assertCodexSubscriptionSchemaContract(t, database)
	if count := scalarCount(t, database, `SELECT COUNT(*) FROM codex_subscription_detected_accounts`); count != 0 {
		t.Fatalf("fresh detected rows = %d, want 0", count)
	}
}

func TestCodexSubscriptionMigrationUpgradesV32AndBackfillsScopes(t *testing.T) {
	t.Parallel()
	database := openTestDatabase(t)
	seedApplicationSchemaV32(t, database)
	scopeA := strings.Repeat("a", 64)
	scopeB := strings.Repeat("b", 64)
	if err := database.Write(t.Context(), func(ctx context.Context, transaction *gorm.DB) error {
		if err := transaction.WithContext(ctx).Create(&codexAccountScopeModel{
			AccountScope: scopeA, FirstSeenAtMS: 1, LastSeenAtMS: 1,
		}).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Create(&codexAccountScopeModel{
			AccountScope: scopeB, FirstSeenAtMS: 2, LastSeenAtMS: 2,
		}).Error
	}); err != nil {
		t.Fatalf("seed scopes: %v", err)
	}

	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context, fromVersion int, targetVersion int, _ func(storesqlite.BackupProgress),
	) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v32-before-v33.db", nil
	}
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("run(v32->v33) error = %v", err)
	}
	if report.FromVersion != 32 || report.TargetVersion != 33 ||
		!equalInts(report.AppliedVersions, []int{33}) || backupVersions != [2]int{32, 33} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 33, 33)
	assertCodexSubscriptionSchemaContract(t, database)
	if count := scalarCount(t, database, `SELECT COUNT(*) FROM codex_subscription_detected_accounts`); count != 2 {
		t.Fatalf("backfilled detected rows = %d, want 2", count)
	}
	if count := scalarCount(t, database, `SELECT COUNT(*) FROM codex_subscription_detected_accounts WHERE automatic_plan_state != 'unavailable' OR automatic_plan IS NOT NULL OR detected_email IS NOT NULL`); count != 0 {
		t.Fatalf("backfill wrote profile facts: %d", count)
	}
	if count := scalarCount(t, database, `SELECT COUNT(*) FROM codex_subscription_detected_accounts WHERE account_scope = 'default'`); count != 0 {
		t.Fatalf("legacy default was backfilled")
	}
	idA := scalarText(t, database, `SELECT detected_account_id FROM codex_subscription_detected_accounts WHERE account_scope = '`+scopeA+`'`)
	idB := scalarText(t, database, `SELECT detected_account_id FROM codex_subscription_detected_accounts WHERE account_scope = '`+scopeB+`'`)
	if len(idA) != 36 || len(idB) != 36 || idA == idB {
		t.Fatalf("public ids = %q %q", idA, idB)
	}
}

func TestCodexSubscriptionMigrationRollsBackAtomically(t *testing.T) {
	t.Parallel()
	database := openTestDatabase(t)
	seedApplicationSchemaV32(t, database)
	want := errors.New("injected v33 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	original := catalog[applicationSchemaV33Version-1].apply
	catalog[applicationSchemaV33Version-1].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := original(ctx, transaction); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v32-before-failed-v33.db", nil
	}
	if _, err := runner.run(t.Context()); !errors.Is(err, want) {
		t.Fatalf("run(failed v33) error = %v, want injected", err)
	}
	assertMigrationVersionAndHistory(t, database, 32, 32)
	if err := database.View(t.Context(), func(_ context.Context, connection *gorm.DB) error {
		for _, table := range []string{
			"codex_subscription_detected_accounts",
			"codex_subscription_manual_entries",
			"codex_subscription_links",
		} {
			if connection.Migrator().HasTable(table) {
				t.Errorf("failed v33 migration left table %q behind", table)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v33 rollback: %v", err)
	}
}

func TestCodexSubscriptionMigrationReopenKeepsSchemaVersion33(t *testing.T) {
	t.Parallel()
	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 33, 33)
	if err := NewRepository(database).EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema(reopen) error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 33, 33)
	assertCodexSubscriptionSchemaContract(t, database)
}

func TestApplicationSchemaV32ChecksumRemainsFrozenAfterV33(t *testing.T) {
	t.Parallel()
	const want = "0aedee7a707f37b27a1249886b260cad7cbe1da35ea4ad302b45f27c4d430f52"
	if got := applicationSchemaV32Checksum(); got != want {
		t.Fatalf("applicationSchemaV32Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV33ChecksumIsFrozen(t *testing.T) {
	t.Parallel()
	const want = "2cb5ad03c0a51a480712df402bb962cbc0990c2826501f8abfcf6f47b8409518"
	if got := applicationSchemaV33Checksum(); got != want {
		t.Fatalf("applicationSchemaV33Checksum() = %q, want frozen %q", got, want)
	}
}

func seedApplicationSchemaV32(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:applicationSchemaV32Version]
	runner.verifyCurrent = func(context.Context, *gorm.DB) error { return nil }
	if report, err := runner.run(t.Context()); err != nil || report.TargetVersion != 32 {
		t.Fatalf("seed application schema v32 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 32, 32)
}

func assertCodexSubscriptionSchemaContract(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	if err := database.View(t.Context(), func(ctx context.Context, connection *gorm.DB) error {
		for _, object := range codexSubscriptionSchemaObjects {
			exists, err := storeschema.VerifyObject(ctx, connection, object)
			if err != nil {
				return err
			}
			if !exists {
				t.Errorf("v33 %s %q missing or mismatched", object.ObjectType, object.Name)
			}
		}
		var indexes []string
		if err := connection.WithContext(ctx).Raw(`
			SELECT name FROM sqlite_schema
			WHERE type = 'index' AND sql LIKE '%codex_subscription_detected_accounts(email_match_key)%UNIQUE%'
		`).Scan(&indexes).Error; err != nil {
			return err
		}
		if len(indexes) != 0 {
			t.Fatalf("detected email unique index present: %v", indexes)
		}
		var violations []struct{ Table string }
		if err := connection.WithContext(ctx).Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
			return err
		}
		if len(violations) != 0 {
			t.Fatalf("PRAGMA foreign_key_check violations = %#v", violations)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v33 schema: %v", err)
	}
}
