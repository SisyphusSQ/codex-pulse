package store

import (
	"context"
	"errors"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

func TestApplicationSchemaV8ChecksumIsFrozen(t *testing.T) {
	const want = "2ef5f8215da2c73e984645f30085e6ef9fa4df68d30b6f03b7ee7f634d51c6c9"
	if got := applicationSchemaV8Checksum(); got != want {
		t.Fatalf("applicationSchemaV8Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationMigrationUpgradesV7ThroughCurrentWithoutLosingSchedulerFacts(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV7(t, database)
	repository := NewRepository(database)
	job := schedulerTargetJob("v7-preserved-job", JobPhaseLive, 10)
	if err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Table("job_runs").Create(map[string]any{
			"job_id": job.JobID, "job_type": job.JobType, "requested_by": job.RequestedBy,
			"priority": job.Priority, "state": string(job.State), "phase": string(job.Phase),
			"created_at_ms": job.CreatedAtMS, "updated_at_ms": job.UpdatedAtMS,
		}).Error
	}); err != nil {
		t.Fatalf("seed v7 job run error = %v", err)
	}
	task := SchedulerTask{
		TaskID: "v7-preserved-task", DedupeKey: "v7:preserved",
		TargetKind: SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 7,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 11, EnqueuedAtMS: 11, UpdatedAtMS: 11,
	}
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		model := schedulerTaskModelFromDomain(task)
		return transaction.WithContext(ctx).Create(&model).Error
	})
	if err != nil {
		t.Fatalf("seed scheduler task: %v", err)
	}

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
		return "/tmp/application-v7-before-v8.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if report.FromVersion != 7 || report.TargetVersion != applicationSchemaVersion ||
		!equalInts(report.AppliedVersions, []int{8, 9, 10, 11, 12, 13}) || backupVersions != [2]int{7, 13} {
		t.Fatalf("run() report = %#v backup=%v, want v7 to v13", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
	stored, err := repository.SchedulerTask(context.Background(), task.TaskID)
	if err != nil || stored != task {
		t.Fatalf("SchedulerTask(preserved) = %#v, %v", stored, err)
	}
}

func TestApplicationMigrationRollsBackFailedV8Atomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV7(t, database)
	want := errors.New("injected v8 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	catalog[7].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := ensureSchemaObjects(ctx, transaction, lifecycleSchemaObjects[:1]); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v7-before-failed-v8.db", nil
	}
	if _, err := runner.run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("run() error = %v, want injected failure", err)
	}
	assertMigrationVersionAndHistory(t, database, 7, 7)
	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		if connection.Migrator().HasTable("scheduler_lifecycle") ||
			connection.Migrator().HasTable("scheduler_retry_states") {
			t.Fatal("failed v8 migration left lifecycle tables behind")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect rollback: %v", err)
	}
}

func seedApplicationSchemaV7(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:7]
	runner.verifyCurrent = verifyApplicationSchemaV7
	if report, err := runner.run(context.Background()); err != nil || report.TargetVersion != 7 {
		t.Fatalf("seed application schema v7 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 7, 7)
}

func verifyApplicationSchemaV7(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjects, retentionSchemaObjects,
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects,
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

func TestCurrentApplicationSchemaIncludesV8LifecycleAndRetryFacts(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != applicationSchemaV13Version {
		t.Fatalf("applicationSchemaVersion = %d, want 13", applicationSchemaVersion)
	}
	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))

	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, table := range []string{"scheduler_lifecycle", "scheduler_retry_states"} {
			if !connection.Migrator().HasTable(table) {
				t.Errorf("schema v8 table %q missing", table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect schema v8: %v", err)
	}
}
