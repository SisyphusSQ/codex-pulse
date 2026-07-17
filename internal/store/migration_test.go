package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationSchemaV1ChecksumIsFrozen(t *testing.T) {
	const want = "b48a4015a94f844f54aee2152589681406c20d90f016c2806ae5f6206ca0370a"
	if got := applicationSchemaV1Checksum(); got != want {
		t.Fatalf("applicationSchemaV1Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV2ChecksumIsFrozen(t *testing.T) {
	const want = "d546de752413306483a942e89f89234dfd694d12c65b5dd1894d457c13117399"
	if got := applicationSchemaV2Checksum(); got != want {
		t.Fatalf("applicationSchemaV2Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV3ChecksumIsFrozen(t *testing.T) {
	const want = "390fa8d40b543eb51ec72417462d3a6aa7d641ed9cb6b8ea7a5bd5a165274b92"
	if got := applicationSchemaV3Checksum(); got != want {
		t.Fatalf("applicationSchemaV3Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV4ChecksumIsFrozen(t *testing.T) {
	const want = "fc6c1d6dfc52b2220d80f515d4550fb01ecd4238efcd514098b4f89dfe11c33c"
	if got := applicationSchemaV4Checksum(); got != want {
		t.Fatalf("applicationSchemaV4Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV5ChecksumIsFrozen(t *testing.T) {
	const want = "a4fc5d2a78988b7df11d4540a0e005bf1c37bb6cd52291a153b70f4cf0eb034f"
	if got := applicationSchemaV5Checksum(); got != want {
		t.Fatalf("applicationSchemaV5Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV6ChecksumIsFrozen(t *testing.T) {
	const want = "718ce635056ffedad225e7e5918b1df49803d21b752c5cc91fa1cd2bd0c1e215"
	if got := applicationSchemaV6Checksum(); got != want {
		t.Fatalf("applicationSchemaV6Checksum() = %q, want frozen %q", got, want)
	}
}

func TestEnsureApplicationSchemaRecordsVersionedFreshMigration(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))

	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema(replay) error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
}

func TestApplicationMigrationAppendsIngestSchemaToFrozenV2(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV2(t, database)
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
		return "/tmp/application-v2-before-v5.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if report.FromVersion != 2 || report.TargetVersion != applicationSchemaVersion ||
		!equalInts(report.AppliedVersions, []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}) || report.BackupPath == "" {
		t.Fatalf("run() report = %#v, want v2 to v13 with backup", report)
	}
	if backupVersions != [2]int{2, 13} {
		t.Fatalf("backup versions = %v, want [2 13]", backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))

	err = database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for _, table := range []string{
			"source_generations", "parser_checkpoints", "source_generation_batches", "parser_diagnostics",
		} {
			if !connection.Migrator().HasTable(table) {
				t.Errorf("v3 table %q missing", table)
			}
		}
		var turnDDL string
		if err := connection.WithContext(ctx).Raw(
			`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'turns'`,
		).Scan(&turnDDL).Error; err != nil {
			return err
		}
		normalized := normalizeSchemaSQL(turnDDL)
		if strings.Contains(normalized, "complete_offset >= start_offset") {
			t.Errorf("turns DDL still orders terminal offset after start: %s", turnDDL)
		}
		if !strings.Contains(normalized, "complete_offset >= 0") {
			t.Errorf("turns DDL lost non-negative complete offset check: %s", turnDDL)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect v3 migration: %v", err)
	}
}

func TestApplicationMigrationAppendsRetentionIndexesToFrozenV1(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV1(t, database)
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
		return "/tmp/application-v1-before-v2.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if report.FromVersion != 1 || report.TargetVersion != applicationSchemaVersion ||
		!equalInts(report.AppliedVersions, []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}) || report.BackupPath == "" {
		t.Fatalf("run() report = %#v, want v1 to v13 with backup", report)
	}
	if backupVersions != [2]int{1, 13} {
		t.Fatalf("backup versions = %v, want [1 13]", backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))

	err = database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var checksum string
		if err := connection.WithContext(ctx).Model(&schemaMigrationModel{}).
			Where("version = ?", 1).Pluck("checksum", &checksum).Error; err != nil {
			return err
		}
		if checksum != applicationSchemaV1Checksum() {
			t.Errorf("v1 checksum = %q, want frozen %q", checksum, applicationSchemaV1Checksum())
		}
		for _, index := range []struct{ table, name string }{
			{table: "health_events", name: "idx_health_events_retention"},
			{table: "job_runs", name: "idx_job_runs_retention"},
			{table: "job_runs", name: "idx_job_runs_resume_lineage"},
			{table: "source_attempts", name: "idx_source_attempts_retention"},
		} {
			if !connection.Migrator().HasIndex(index.table, index.name) {
				t.Errorf("retention index %q missing after v2 migration", index.name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect v2 migration: %v", err)
	}
}

func TestMigrationRunnerAppliesAllPendingVersionsInOrder(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	runner := migrationRunner{
		repository: NewRepository(database),
		catalog: []migrationDefinition{
			{
				version: 1, name: "one", checksum: strings.Repeat("1", 64),
				apply: func(ctx context.Context, transaction storesqlite.WriteTx) error {
					return transaction.WithContext(ctx).Exec(`CREATE TABLE migration_one (id INTEGER) STRICT`).Error
				},
			},
			{
				version: 2, name: "two", checksum: strings.Repeat("2", 64),
				apply: func(ctx context.Context, transaction storesqlite.WriteTx) error {
					return transaction.WithContext(ctx).Exec(`CREATE TABLE migration_two (id INTEGER) STRICT`).Error
				},
			},
		},
		now: func() time.Time { return time.UnixMilli(123) },
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got, want := report.AppliedVersions, []int{1, 2}; !equalInts(got, want) {
		t.Fatalf("AppliedVersions = %v, want %v", got, want)
	}
	assertMigrationVersionAndHistory(t, database, 2, 2)

	replay, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run(replay) error = %v", err)
	}
	if len(replay.AppliedVersions) != 0 {
		t.Fatalf("run(replay) AppliedVersions = %v, want empty", replay.AppliedVersions)
	}
}

func TestMigrationRunnerRollsBackAllPendingVersionsOnLaterFailure(t *testing.T) {
	t.Parallel()

	errInjected := errors.New("injected migration failure")
	database := openTestDatabase(t)
	runner := migrationRunner{
		repository: NewRepository(database),
		catalog: []migrationDefinition{
			{
				version: 1, name: "one", checksum: strings.Repeat("1", 64),
				apply: func(ctx context.Context, transaction storesqlite.WriteTx) error {
					return transaction.WithContext(ctx).Exec(`CREATE TABLE migration_one (id INTEGER) STRICT`).Error
				},
			},
			{
				version: 2, name: "two", checksum: strings.Repeat("2", 64),
				apply: func(ctx context.Context, transaction storesqlite.WriteTx) error {
					if err := transaction.WithContext(ctx).Exec(`CREATE TABLE migration_two (id INTEGER) STRICT`).Error; err != nil {
						return err
					}
					return errInjected
				},
			},
		},
	}
	report, err := runner.run(context.Background())
	if !errors.Is(err, errInjected) {
		t.Fatalf("run() error = %v, want injected failure", err)
	}
	if len(report.AppliedVersions) != 0 {
		t.Fatalf("AppliedVersions = %v, want empty after rollback", report.AppliedVersions)
	}
	err = database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var version int
		if err := rawQueryRow(ctx, connection, `PRAGMA user_version`).Scan(&version); err != nil {
			return err
		}
		if version != 0 {
			t.Errorf("PRAGMA user_version = %d, want 0", version)
		}
		for _, table := range []string{"schema_migrations", "migration_one", "migration_two"} {
			if connection.Migrator().HasTable(table) {
				t.Errorf("table %q must roll back", table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect rollback: %v", err)
	}
}

func TestMigrationRunnerBacksUpLegacyDatabaseBeforeApplying(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedLegacyApplicationSchema(t, database)
	var events []string
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error {
		events = append(events, "space")
		return nil
	}
	runner.backup = func(
		context.Context,
		int,
		int,
		func(storesqlite.BackupProgress),
	) (string, error) {
		events = append(events, "backup")
		return "/tmp/legacy-before-v2.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got, want := strings.Join(events, ","), "space,backup"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
	if report.BackupPath != "/tmp/legacy-before-v2.db" {
		t.Fatalf("BackupPath = %q", report.BackupPath)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
}

func TestApplicationMigrationCreatesRestorablePreMigrationBackupForLegacyDatabase(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedLegacyApplicationSchema(t, database)
	report, err := NewRepository(database).MigrateApplicationSchema(context.Background())
	if err != nil {
		t.Fatalf("MigrateApplicationSchema() error = %v", err)
	}
	if report.BackupPath == "" {
		t.Fatal("MigrateApplicationSchema() returned no backup path for legacy database")
	}
	backupDatabase, err := sql.Open("sqlite", report.BackupPath)
	if err != nil {
		t.Fatalf("open migration backup: %v", err)
	}
	defer backupDatabase.Close()
	var version int
	if err := backupDatabase.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read backup user_version: %v", err)
	}
	if version != 0 {
		t.Fatalf("backup user_version = %d, want legacy 0", version)
	}
	var projectTableCount int
	if err := backupDatabase.QueryRow(
		`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'projects'`,
	).Scan(&projectTableCount); err != nil {
		t.Fatalf("read backup schema: %v", err)
	}
	if projectTableCount != 1 {
		t.Fatalf("backup projects table count = %d, want 1", projectTableCount)
	}
	var ledgerCount int
	if err := backupDatabase.QueryRow(
		`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'schema_migrations'`,
	).Scan(&ledgerCount); err != nil {
		t.Fatalf("read backup ledger state: %v", err)
	}
	if ledgerCount != 0 {
		t.Fatalf("backup ledger table count = %d, want 0", ledgerCount)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
}

func TestApplicationMigrationUpgradesCoreOnlyLegacyDatabaseAndPreservesDataInBackup(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	project := Project{
		ProjectID: "legacy-project", DisplayName: "Legacy", RootPath: "/synthetic/legacy",
		CreatedAtMS: 1, UpdatedAtMS: 1,
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Project: &project}); err != nil {
		t.Fatalf("UpsertFacts() error = %v", err)
	}
	report, err := repository.MigrateApplicationSchema(context.Background())
	if err != nil {
		t.Fatalf("MigrateApplicationSchema() error = %v", err)
	}
	if report.BackupPath == "" {
		t.Fatal("MigrateApplicationSchema() returned no backup path")
	}
	if _, err := repository.SourceFile(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SourceFile() error = %v, want ErrNotFound from created runtime schema", err)
	}
	backupDatabase, err := sql.Open("sqlite", report.BackupPath)
	if err != nil {
		t.Fatalf("open migration backup: %v", err)
	}
	defer backupDatabase.Close()
	var displayName string
	if err := backupDatabase.QueryRow(
		`SELECT display_name FROM projects WHERE project_id = ?`, project.ProjectID,
	).Scan(&displayName); err != nil {
		t.Fatalf("read legacy project from backup: %v", err)
	}
	if displayName != project.DisplayName {
		t.Fatalf("backup display_name = %q, want %q", displayName, project.DisplayName)
	}
	var runtimeTables int
	if err := backupDatabase.QueryRow(
		`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'source_files'`,
	).Scan(&runtimeTables); err != nil {
		t.Fatalf("read legacy runtime state: %v", err)
	}
	if runtimeTables != 0 {
		t.Fatalf("backup runtime table count = %d, want pre-migration 0", runtimeTables)
	}

	now := time.UnixMilli(200_000_000).UTC()
	cutoffMS := now.Add(-RetentionWindow).UnixMilli()
	state := SourceState{
		SourceInstanceID: "legacy-upgraded-source", SourceType: "quota", ScopeKey: "default",
		FreshnessState: SourceFreshnessCurrent, CursorVersion: 1, UpdatedAtMS: now.UnixMilli(),
	}
	if err := repository.UpsertSourceState(context.Background(), state); err != nil {
		t.Fatalf("UpsertSourceState(after upgrade) error = %v", err)
	}
	if err := repository.AppendSourceAttempt(context.Background(), SourceAttempt{
		RequestID: "legacy-upgraded-attempt", SourceInstanceID: state.SourceInstanceID,
		StartedAtMS: cutoffMS - 2, FinishedAtMS: cutoffMS - 1, Outcome: SourceAttemptSucceeded,
		AttemptCount: 1,
	}); err != nil {
		t.Fatalf("AppendSourceAttempt(after upgrade) error = %v", err)
	}
	cleanup, err := repository.CleanupRetention(context.Background(), RetentionCleanupOptions{Now: now})
	if err != nil {
		t.Fatalf("CleanupRetention(after upgrade) error = %v", err)
	}
	if cleanup.Deleted != (RetentionDeletedCounts{SourceAttempts: 1}) {
		t.Fatalf("CleanupRetention(after upgrade) = %#v, want one source attempt", cleanup)
	}
	if attempts, err := repository.ListSourceAttempts(context.Background(), state.SourceInstanceID, 10); err != nil || len(attempts) != 0 {
		t.Fatalf("ListSourceAttempts(after cleanup) = %#v, %v, want empty", attempts, err)
	}
	var projectCount int64
	err = database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Model(&projectModel{}).
			Where("project_id = ?", project.ProjectID).Count(&projectCount).Error
	})
	if err != nil {
		t.Fatalf("read legacy project after cleanup: %v", err)
	}
	if projectCount != 1 {
		t.Fatalf("legacy project count after cleanup = %d, want 1", projectCount)
	}
}

func TestMigrationRunnerSkipsBackupForFreshAndCurrentDatabase(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error {
		return errors.New("space check must be skipped")
	}
	runner.backup = func(
		context.Context,
		int,
		int,
		func(storesqlite.BackupProgress),
	) (string, error) {
		return "", errors.New("backup must be skipped")
	}
	if _, err := runner.run(context.Background()); err != nil {
		t.Fatalf("run(fresh) error = %v", err)
	}
	if _, err := runner.run(context.Background()); err != nil {
		t.Fatalf("run(current) error = %v", err)
	}
}

func TestMigrationRunnerStopsBeforeApplyWhenSpaceOrBackupFails(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		spaceError  error
		backupError error
	}{
		{name: "space", spaceError: storesqlite.ErrDiskFull},
		{name: "backup", backupError: storesqlite.ErrIO},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDatabase(t)
			seedLegacyApplicationSchema(t, database)
			backupCalled := false
			runner := applicationMigrationRunnerForTest(database)
			runner.spaceCheck = func(context.Context, string, int64) error { return testCase.spaceError }
			runner.backup = func(
				context.Context,
				int,
				int,
				func(storesqlite.BackupProgress),
			) (string, error) {
				backupCalled = true
				return "", testCase.backupError
			}
			_, err := runner.run(context.Background())
			want := testCase.spaceError
			if want == nil {
				want = testCase.backupError
			}
			if !errors.Is(err, want) {
				t.Fatalf("run() error = %v, want %v", err, want)
			}
			if testCase.spaceError != nil && backupCalled {
				t.Fatal("backup called after failed space check")
			}
			assertLegacyMigrationState(t, database)
		})
	}
}

func TestMigrationFailureExposesStableStageCodeAndVersionContext(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedLegacyApplicationSchema(t, database)
	errBackup := errors.New("driver detail must remain wrapped")
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		context.Context,
		int,
		int,
		func(storesqlite.BackupProgress),
	) (string, error) {
		return "/tmp/partial-before-v2.db", errBackup
	}
	_, err := runner.run(context.Background())
	var failure *MigrationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("run() error = %v, want MigrationFailure", err)
	}
	if failure.Stage != MigrationStageBackup || failure.Code != MigrationCodeBackupFailed ||
		failure.CurrentVersion != 0 || failure.TargetVersion != applicationSchemaVersion || failure.FailedVersion != 0 ||
		failure.BackupPath != "" || !errors.Is(failure, errBackup) {
		t.Fatalf("MigrationFailure = %#v", failure)
	}
}

func TestMigrationProgressReportsStableStagesAndBackupPages(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedLegacyApplicationSchema(t, database)
	var progress []MigrationProgress
	runner := applicationMigrationRunnerForTest(database)
	runner.observe = func(update MigrationProgress) { progress = append(progress, update) }
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context,
		_ int,
		_ int,
		observe func(storesqlite.BackupProgress),
	) (string, error) {
		observe(storesqlite.BackupProgress{CopiedPages: 3, RemainingPages: 2, TotalPages: 5})
		observe(storesqlite.BackupProgress{CopiedPages: 5, RemainingPages: 0, TotalPages: 5})
		return "/tmp/legacy-before-v2.db", nil
	}
	if _, err := runner.run(context.Background()); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	var stages []string
	var backupUpdates []MigrationProgress
	for _, update := range progress {
		stages = append(stages, string(update.Stage))
		if update.Stage == MigrationStageBackup && update.TotalPages > 0 {
			backupUpdates = append(backupUpdates, update)
		}
	}
	wantStages := []string{"inspect", "space", "backup", "backup", "backup"}
	for range applicationMigrations {
		wantStages = append(wantStages, "apply")
	}
	wantStages = append(wantStages, "verify", "complete")
	if got, want := strings.Join(stages, ","), strings.Join(wantStages, ","); got != want {
		t.Fatalf("progress stages = %q, want %q", got, want)
	}
	if len(backupUpdates) != 2 || backupUpdates[0].CopiedPages != 3 || backupUpdates[1].RemainingPages != 0 {
		t.Fatalf("backup progress = %#v", backupUpdates)
	}
}

func applicationMigrationRunnerForTest(database *storesqlite.Store) migrationRunner {
	return migrationRunner{
		repository: NewRepository(database), catalog: applicationMigrations,
		now: func() time.Time { return time.UnixMilli(123) },
		verifyCurrent: func(ctx context.Context, transaction storesqlite.WriteTx) error {
			return verifyApplicationSchema(ctx, transaction)
		},
	}
}

func seedLegacyApplicationSchema(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := ensureSchemaObjects(ctx, transaction, applicationSchemaV1CoreObjects()); err != nil {
			return err
		}
		return ensureSchemaObjects(ctx, transaction, runtimeSchemaObjects)
	})
	if err != nil {
		t.Fatalf("seed legacy application schema: %v", err)
	}
}

func seedApplicationSchemaV1(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := ensureSchemaObjects(ctx, transaction, migrationSchemaObjects); err != nil {
			return err
		}
		if err := applicationMigrations[0].apply(ctx, transaction); err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Create(&schemaMigrationModel{
			Version: 1, Name: "initial-application-schema",
			Checksum: applicationSchemaV1Checksum(), AppliedAtMS: 1,
		}).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Exec("PRAGMA user_version = 1").Error
	})
	if err != nil {
		t.Fatalf("seed application schema v1: %v", err)
	}
}

func seedApplicationSchemaV2(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := ensureSchemaObjects(ctx, transaction, migrationSchemaObjects); err != nil {
			return err
		}
		for _, migration := range applicationMigrations[:2] {
			if err := migration.apply(ctx, transaction); err != nil {
				return err
			}
			if err := transaction.WithContext(ctx).Create(&schemaMigrationModel{
				Version: migration.version, Name: migration.name,
				Checksum: migration.checksum, AppliedAtMS: int64(migration.version),
			}).Error; err != nil {
				return err
			}
		}
		return transaction.WithContext(ctx).Exec("PRAGMA user_version = 2").Error
	})
	if err != nil {
		t.Fatalf("seed application schema v2: %v", err)
	}
}

func assertLegacyMigrationState(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var version int
		if err := rawQueryRow(ctx, connection, `PRAGMA user_version`).Scan(&version); err != nil {
			return err
		}
		if version != 0 {
			t.Errorf("PRAGMA user_version = %d, want 0", version)
		}
		if connection.Migrator().HasTable(&schemaMigrationModel{}) {
			t.Error("schema_migrations must not exist")
		}
		if !connection.Migrator().HasTable(&projectModel{}) {
			t.Error("legacy projects table must remain")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect legacy migration state: %v", err)
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestMigrationRejectsChecksumDrift(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Model(&schemaMigrationModel{}).
			Where("version = ?", 1).Update("checksum", "0000000000000000000000000000000000000000000000000000000000000000").Error
	})
	if err != nil {
		t.Fatalf("corrupt checksum: %v", err)
	}
	if err := repository.EnsureApplicationSchema(context.Background()); !errors.Is(err, ErrMigrationContract) {
		t.Fatalf("EnsureApplicationSchema() error = %v, want ErrMigrationContract", err)
	}
}

func TestMigrationRejectsVersionHistoryDivergenceAndNewerSchema(t *testing.T) {
	t.Parallel()

	t.Run("version without ledger", func(t *testing.T) {
		database := openTestDatabase(t)
		if err := setMigrationUserVersion(database, 1); err != nil {
			t.Fatalf("set user_version: %v", err)
		}
		if err := NewRepository(database).EnsureApplicationSchema(context.Background()); !errors.Is(err, ErrMigrationContract) {
			t.Fatalf("EnsureApplicationSchema() error = %v, want ErrMigrationContract", err)
		}
	})

	t.Run("newer version", func(t *testing.T) {
		database := openTestDatabase(t)
		if err := setMigrationUserVersion(database, applicationSchemaVersion+1); err != nil {
			t.Fatalf("set user_version: %v", err)
		}
		if err := NewRepository(database).EnsureApplicationSchema(context.Background()); !errors.Is(err, ErrMigrationNewer) {
			t.Fatalf("EnsureApplicationSchema() error = %v, want ErrMigrationNewer", err)
		}
	})

	t.Run("newer history", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
			t.Fatalf("EnsureApplicationSchema() error = %v", err)
		}
		err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
			return transaction.WithContext(ctx).Create(&schemaMigrationModel{
				Version: applicationSchemaVersion + 1, Name: "future", Checksum: strings.Repeat("3", 64), AppliedAtMS: 2,
			}).Error
		})
		if err != nil {
			t.Fatalf("append future history: %v", err)
		}
		if err := repository.EnsureApplicationSchema(context.Background()); !errors.Is(err, ErrMigrationNewer) {
			t.Fatalf("EnsureApplicationSchema() error = %v, want ErrMigrationNewer", err)
		}
	})
}

func TestMigrationRunnerRejectsInvalidCatalogBeforeWriting(t *testing.T) {
	t.Parallel()

	validApply := func(context.Context, storesqlite.WriteTx) error { return nil }
	for _, testCase := range []struct {
		name    string
		catalog []migrationDefinition
	}{
		{name: "empty"},
		{
			name: "gap",
			catalog: []migrationDefinition{{
				version: 2, name: "two", checksum: strings.Repeat("2", 64), apply: validApply,
			}},
		},
		{
			name: "empty name",
			catalog: []migrationDefinition{{
				version: 1, checksum: strings.Repeat("1", 64), apply: validApply,
			}},
		},
		{
			name:    "invalid checksum",
			catalog: []migrationDefinition{{version: 1, name: "one", checksum: "not-sha256", apply: validApply}},
		},
		{
			name:    "nil apply",
			catalog: []migrationDefinition{{version: 1, name: "one", checksum: strings.Repeat("1", 64)}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDatabase(t)
			runner := migrationRunner{repository: NewRepository(database), catalog: testCase.catalog}
			_, err := runner.run(context.Background())
			if !errors.Is(err, ErrMigrationContract) {
				t.Fatalf("run() error = %v, want ErrMigrationContract", err)
			}
			var failure *MigrationFailure
			if !errors.As(err, &failure) || failure.Stage != MigrationStageCatalog ||
				failure.Code != MigrationCodeCatalogInvalid {
				t.Fatalf("run() failure = %#v", failure)
			}
			assertLegacyMigrationStateWithoutSchema(t, database)
		})
	}
}

func assertLegacyMigrationStateWithoutSchema(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var version int
		if err := rawQueryRow(ctx, connection, `PRAGMA user_version`).Scan(&version); err != nil {
			return err
		}
		if version != 0 || connection.Migrator().HasTable(&schemaMigrationModel{}) {
			t.Errorf("migration state changed: version=%d ledger=%v", version, connection.Migrator().HasTable(&schemaMigrationModel{}))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect untouched database: %v", err)
	}
}

func TestMigrationApplyFailureRollsBackVersionHistoryAndPendingObjects(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).
			Exec(`CREATE TABLE source_files (source_file_id TEXT PRIMARY KEY) STRICT`).Error
	})
	if err != nil {
		t.Fatalf("create incompatible source_files: %v", err)
	}

	err = NewRepository(database).EnsureApplicationSchema(context.Background())
	if !errors.Is(err, ErrSchemaContract) {
		t.Fatalf("EnsureApplicationSchema() error = %v, want ErrSchemaContract", err)
	}
	err = database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var version int
		if err := rawQueryRow(ctx, connection, `PRAGMA user_version`).Scan(&version); err != nil {
			return err
		}
		if version != 0 {
			t.Errorf("PRAGMA user_version = %d, want 0", version)
		}
		if connection.Migrator().HasTable(&schemaMigrationModel{}) {
			t.Error("schema_migrations must roll back")
		}
		if connection.Migrator().HasTable(&projectModel{}) {
			t.Error("projects must roll back")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect rollback: %v", err)
	}
}

func setMigrationUserVersion(database *storesqlite.Store, version int) error {
	return database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Exec("PRAGMA user_version = " + strconv.Itoa(version)).Error
	})
}

func assertMigrationVersionAndHistory(
	t *testing.T,
	database *storesqlite.Store,
	wantVersion int,
	wantHistory int64,
) {
	t.Helper()
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var version int
		if err := rawQueryRow(ctx, connection, `PRAGMA user_version`).Scan(&version); err != nil {
			return err
		}
		if version != wantVersion {
			t.Errorf("PRAGMA user_version = %d, want %d", version, wantVersion)
		}
		var history int64
		if err := connection.WithContext(ctx).Table("schema_migrations").Count(&history).Error; err != nil {
			return err
		}
		if history != wantHistory {
			t.Errorf("schema_migrations count = %d, want %d", history, wantHistory)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect migration state: %v", err)
	}
}
