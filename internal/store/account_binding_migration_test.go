package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestAccountBindingMigrationPreservesLegacyDefaultAndSealsOldClaims(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != applicationSchemaV32Version {
		t.Fatalf("applicationSchemaVersion = %d, want 32", applicationSchemaVersion)
	}
	database := openTestDatabase(t)
	seedApplicationSchemaV31(t, database)
	before := seedLegacyDefaultQuotaFixture(t, database)

	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context, fromVersion int, targetVersion int, _ func(storesqlite.BackupProgress),
	) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v31-before-v32.db", nil
	}
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("run(v31->v32) error = %v", err)
	}
	if report.FromVersion != 31 || report.TargetVersion != 32 ||
		!equalInts(report.AppliedVersions, []int{32}) || backupVersions != [2]int{31, 32} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 32, 32)
	assertLegacyDefaultQuotaPreserved(t, database, before)
	assertAccountBindingSchemaContract(t, database)
	if count := scalarCount(t, database, `SELECT COUNT(*) FROM codex_account_binding`); count != 0 {
		t.Fatalf("legacy default was assigned a binding row: %d", count)
	}
	if count := scalarCount(t, database, `SELECT COUNT(*) FROM source_refresh_claims WHERE state = 'active'`); count != 0 {
		t.Fatalf("active claims remained after migration: %d", count)
	}
	if count := scalarCount(t, database, `SELECT COUNT(*) FROM source_refresh_schedules WHERE scope_key = 'default' AND (reason != 'disabled' OR next_due_at_ms IS NOT NULL)`); count != 0 {
		t.Fatalf("default schedules were not disabled: %d", count)
	}
	if count := scalarCount(t, database, `SELECT COUNT(*) FROM source_attempts WHERE binding_generation != 0`); count != 0 {
		t.Fatalf("legacy attempts were not backfilled with generation 0")
	}
}

func TestAccountBindingMigrationRollsBackAtomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV31(t, database)
	before := seedLegacyDefaultQuotaFixture(t, database)
	want := errors.New("injected v32 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	original := catalog[applicationSchemaV32Version-1].apply
	catalog[applicationSchemaV32Version-1].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := original(ctx, transaction); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v31-before-failed-v32.db", nil
	}
	if _, err := runner.run(t.Context()); !errors.Is(err, want) {
		t.Fatalf("run(failed v32) error = %v, want injected", err)
	}
	assertMigrationVersionAndHistory(t, database, 31, 31)
	assertLegacyDefaultQuotaPreserved(t, database, before)
	if err := database.View(t.Context(), func(_ context.Context, connection *gorm.DB) error {
		for _, table := range []string{
			"codex_account_scope_key", "codex_account_scopes", "codex_account_binding",
			"source_refresh_global_fences",
		} {
			if connection.Migrator().HasTable(table) {
				t.Errorf("failed v32 migration left table %q behind", table)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v32 rollback: %v", err)
	}
}

func TestAccountBindingMigrationReopenKeepsSchemaVersion32(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 32, 32)
	if err := NewRepository(database).EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema(reopen) error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 32, 32)
	assertAccountBindingSchemaContract(t, database)
}

func TestApplicationSchemaV32ChecksumIsFrozen(t *testing.T) {
	t.Parallel()
	const want = "0aedee7a707f37b27a1249886b260cad7cbe1da35ea4ad302b45f27c4d430f52"
	if got := applicationSchemaV32Checksum(); got != want {
		t.Fatalf("applicationSchemaV32Checksum() = %q, want frozen %q", got, want)
	}
}

func seedApplicationSchemaV31(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:applicationSchemaV31Version]
	runner.verifyCurrent = func(context.Context, *gorm.DB) error { return nil }
	if report, err := runner.run(t.Context()); err != nil || report.TargetVersion != 31 {
		t.Fatalf("seed application schema v31 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 31, 31)
}

type legacyDefaultQuotaFixture struct {
	observationCount int
	currentCount     int
	evidenceCount    int
	resetCount       int
	observationHash  string
	currentHash      string
	evidenceHash     string
	resetHash        string
}

func seedLegacyDefaultQuotaFixture(t *testing.T, database *storesqlite.Store) legacyDefaultQuotaFixture {
	t.Helper()
	if err := database.Write(t.Context(), func(ctx context.Context, transaction *gorm.DB) error {
		if err := transaction.WithContext(ctx).Exec(`
			INSERT INTO source_state (
				source_instance_id, source_type, scope_key, consecutive_failures,
				freshness_state, cursor_version, updated_at_ms
			) VALUES ('quota:wham:default', 'wham_quota', 'default', 0, 'current', 1, 1_784_000_000_000)
		`).Error; err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Exec(`
			INSERT INTO source_attempts (
				request_id, source_instance_id, started_at_ms, finished_at_ms, outcome,
				http_status, attempt_count, response_bytes
			) VALUES (
				'legacy-wham-request', 'quota:wham:default', 1_784_000_000_000, 1_784_000_000_100,
				'succeeded', 200, 1, 128
			)
		`).Error; err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Exec(`
			INSERT INTO quota_observations (
				observation_id, account_scope, source, limit_id, window_kind, used_percent,
				window_minutes, resets_at_ms, plan_type, validity, first_observed_at_ms,
				last_observed_at_ms, sample_count, request_id, first_source_generation,
				first_source_offset, source_generation, source_offset
			) VALUES (
				'legacy-wham-primary', 'default', 'wham', 'codex', 'primary', 41,
				300, 1_784_360_000_000, 'pro', 'accepted', 1_784_000_000_000,
				1_784_000_000_000, 1, 'legacy-wham-request', 0, 0, 0, 0
			)
		`).Error; err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Exec(`
			INSERT INTO quota_current (
				account_scope, window_kind, limit_id, observation_id, effective_used_percent,
				window_minutes, resets_at_ms, window_generation, selected_source, freshness_state,
				conflict_state, fresh_until_ms, last_success_at_ms, last_attempt_at_ms,
				rule_version, explanation_code, evaluated_at_ms
			) VALUES (
				'default', 'primary', 'codex', 'legacy-wham-primary', 41,
				300, 1_784_360_000_000, 1_784_360_000_000, 'wham', 'fresh',
				'none', 1_784_000_600_000, 1_784_000_000_100, 1_784_000_000_100,
				'v1', 'trusted', 1_784_000_000_100
			)
		`).Error; err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Exec(`
			INSERT INTO quota_arbitration_evidence (
				account_scope, window_kind, limit_id, observation_id, window_generation,
				disposition, explanation_code
			) VALUES (
				'default', 'primary', 'codex', 'legacy-wham-primary', 1_784_360_000_000,
				'selected', 'trusted'
			)
		`).Error; err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Exec(`
			INSERT INTO reset_credit_snapshots (
				snapshot_id, request_id, account_scope, available_count, observed_at_ms
			) VALUES (
				'legacy-reset-snapshot', 'legacy-wham-request', 'default', 1, 1_784_000_000_100
			)
		`).Error; err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Exec(`
			INSERT INTO reset_credits (
				snapshot_id, credit_id_hash, status, reset_type, granted_at_ms, expires_at_ms
			) VALUES (
				'legacy-reset-snapshot',
				'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
				'available', 'codex_rate_limits', 1_783_000_000_000, 1_785_000_000_000
			)
		`).Error; err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Exec(`
			INSERT INTO source_refresh_schedules (
				source_instance_id, source_type, scope_key, next_due_at_ms, reason,
				active_claim_id, active_trigger, claim_started_at_ms, claim_expires_at_ms,
				revision, updated_at_ms
			) VALUES (
				'quota:wham:default', 'wham_quota', 'default', 1_784_000_060_000, 'normal_interval',
				'legacy-active-claim', 'scheduled', 1_784_000_000_000, 1_784_000_030_000,
				1, 1_784_000_000_000
			)
		`).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Exec(`
			INSERT INTO source_refresh_claims (
				claim_id, source_instance_id, schedule_revision, trigger, started_at_ms,
				expires_at_ms, state
			) VALUES (
				'legacy-active-claim', 'quota:wham:default', 1, 'scheduled', 1_784_000_000_000,
				1_784_000_030_000, 'active'
			)
		`).Error
	}); err != nil {
		t.Fatalf("seed legacy default fixture: %v", err)
	}
	return readLegacyDefaultQuotaFixture(t, database)
}

func readLegacyDefaultQuotaFixture(t *testing.T, database *storesqlite.Store) legacyDefaultQuotaFixture {
	t.Helper()
	return legacyDefaultQuotaFixture{
		observationCount: scalarCount(t, database, `SELECT COUNT(*) FROM quota_observations WHERE account_scope = 'default'`),
		currentCount:     scalarCount(t, database, `SELECT COUNT(*) FROM quota_current WHERE account_scope = 'default'`),
		evidenceCount:    scalarCount(t, database, `SELECT COUNT(*) FROM quota_arbitration_evidence WHERE account_scope = 'default'`),
		resetCount:       scalarCount(t, database, `SELECT COUNT(*) FROM reset_credit_snapshots WHERE account_scope = 'default'`),
		observationHash:  scalarText(t, database, `SELECT group_concat(observation_id || ':' || used_percent || ':' || resets_at_ms, ',') FROM quota_observations WHERE account_scope = 'default'`),
		currentHash:      scalarText(t, database, `SELECT group_concat(window_kind || ':' || limit_id || ':' || observation_id, ',') FROM quota_current WHERE account_scope = 'default'`),
		evidenceHash:     scalarText(t, database, `SELECT group_concat(observation_id || ':' || disposition, ',') FROM quota_arbitration_evidence WHERE account_scope = 'default'`),
		resetHash:        scalarText(t, database, `SELECT group_concat(snapshot_id || ':' || available_count, ',') FROM reset_credit_snapshots WHERE account_scope = 'default'`),
	}
}

func assertLegacyDefaultQuotaPreserved(t *testing.T, database *storesqlite.Store, before legacyDefaultQuotaFixture) {
	t.Helper()
	after := readLegacyDefaultQuotaFixture(t, database)
	if after != before {
		t.Fatalf("legacy default facts changed: before=%#v after=%#v", before, after)
	}
}

func assertAccountBindingSchemaContract(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	if err := database.View(t.Context(), func(ctx context.Context, connection *gorm.DB) error {
		for _, objects := range [][]storeschema.Object{
			accountBindingSchemaObjects,
			quotaSchemaObjectsV32,
			quotaProjectionSchemaObjectsV32,
			quotaScheduleSchemaObjectsV32,
			{sourceAttemptsSchemaObjectV32()},
		} {
			for _, object := range objects {
				exists, err := storeschema.VerifyObject(ctx, connection, object)
				if err != nil {
					return err
				}
				if !exists {
					t.Errorf("v32 %s %q missing or mismatched", object.ObjectType, object.Name)
				}
			}
		}
		var violations []struct {
			Table string
		}
		if err := connection.WithContext(ctx).Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
			return err
		}
		if len(violations) != 0 {
			t.Fatalf("PRAGMA foreign_key_check violations = %#v", violations)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v32 schema: %v", err)
	}
}

func scalarCount(t *testing.T, database *storesqlite.Store, query string) int {
	t.Helper()
	var value int
	if err := database.View(t.Context(), func(ctx context.Context, connection *gorm.DB) error {
		return connection.WithContext(ctx).Raw(query).Scan(&value).Error
	}); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return value
}

func scalarText(t *testing.T, database *storesqlite.Store, query string) string {
	t.Helper()
	var value *string
	if err := database.View(t.Context(), func(ctx context.Context, connection *gorm.DB) error {
		return connection.WithContext(ctx).Raw(query).Scan(&value).Error
	}); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if value == nil {
		return ""
	}
	return *value
}
