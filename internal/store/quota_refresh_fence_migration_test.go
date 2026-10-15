package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestQuotaRefreshFenceMigrationDropsOrdinaryBackoffAndKeepsServerLimit(t *testing.T) {
	for _, scenario := range []struct{ name, oldReason, failure, wantReason string }{
		{name: "ordinary backoff", oldReason: "network_backoff", failure: "server_error"},
		{name: "explicit rate limit", oldReason: "network_backoff", failure: "http_429", wantReason: "retry_after"},
		{name: "server deadline without global fence", failure: "http_429", wantReason: "retry_after"},
		{name: "manual throttle", oldReason: "manual_interval", failure: "server_error", wantReason: "manual_interval"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			database := openTestDatabase(t)
			runner := applicationMigrationRunnerForTest(database)
			runner.catalog = applicationMigrations[:37]
			runner.verifyCurrent = nil
			if _, err := runner.run(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := database.Write(t.Context(), func(ctx context.Context, db *gorm.DB) error {
				if scenario.oldReason != "" {
					if err := db.WithContext(ctx).Exec(`INSERT INTO source_refresh_global_fences VALUES ('codex_app_server_rate_limits', 900000, ?, 1000)`, scenario.oldReason).Error; err != nil {
						return err
					}
				}
				return db.WithContext(ctx).Exec(`INSERT INTO source_state (source_instance_id, source_type, scope_key, last_attempt_at_ms, next_due_at_ms, consecutive_failures, last_error_class, freshness_state, cursor_version, updated_at_ms, last_failure_code) VALUES (?, ?, ?, 1000, 900000, 1, 'unavailable', 'unavailable', 1, 1000, ?)`, ResetCreditsSourceInstanceAppServer(testAccountScopeA), ResetCreditsSourceTypeAppServer, testAccountScopeA, scenario.failure).Error
			}); err != nil {
				t.Fatal(err)
			}
			runner = applicationMigrationRunnerForTest(database)
			runner.spaceCheck = func(context.Context, string, int64) error { return nil }
			runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
				return "/tmp/quota-fence-v37.db", nil
			}
			report, err := runner.run(t.Context())
			if err != nil || report.TargetVersion != 38 {
				t.Fatalf("migration=%#v, error=%v", report, err)
			}
			if err := database.View(t.Context(), func(ctx context.Context, db *gorm.DB) error {
				var fences []sourceRefreshGlobalFenceModel
				if err := db.WithContext(ctx).Find(&fences).Error; err != nil {
					return err
				}
				if scenario.wantReason == "" {
					if len(fences) != 0 {
						t.Fatalf("ordinary backoff survived: %#v", fences)
					}
				} else if len(fences) != 1 || fences[0].Reason != scenario.wantReason || fences[0].NotBeforeMS != 900000 {
					t.Fatalf("fences=%#v", fences)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQuotaRefreshFenceMigrationFailureRestoresV37Table(t *testing.T) {
	database := openTestDatabase(t)
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:37]
	runner.verifyCurrent = nil
	if _, err := runner.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := database.Write(t.Context(), func(ctx context.Context, db *gorm.DB) error {
		return db.WithContext(ctx).Exec(`INSERT INTO source_refresh_global_fences VALUES ('codex_app_server_rate_limits', 900000, 'network_backoff', 1000)`).Error
	}); err != nil {
		t.Fatal(err)
	}
	want := errors.New("injected fence migration failure")
	runner = applicationMigrationRunnerForTest(database)
	runner.catalog = append([]migrationDefinition(nil), applicationMigrations...)
	original := runner.catalog[37].apply
	runner.catalog[37].apply = func(ctx context.Context, db *gorm.DB) error {
		if err := original(ctx, db); err != nil {
			return err
		}
		return want
	}
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/quota-fence-v37-rollback.db", nil
	}
	if _, err := runner.run(t.Context()); !errors.Is(err, want) {
		t.Fatalf("migration failure=%v", err)
	}
	assertMigrationVersionAndHistory(t, database, 37, 37)
	if err := database.View(t.Context(), func(ctx context.Context, db *gorm.DB) error {
		var fence sourceRefreshGlobalFenceModel
		if err := db.WithContext(ctx).Take(&fence).Error; err != nil {
			return err
		}
		if fence.Reason != "network_backoff" || fence.NotBeforeMS != 900000 || db.Migrator().HasTable("source_refresh_global_fences_v37") {
			t.Fatalf("rollback fence=%#v", fence)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
