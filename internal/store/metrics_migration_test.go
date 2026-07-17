package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// 测试 application schema v13 创建 app runtime metrics 且保持历史 migration 冻结。
func TestApplicationSchemaV13CreatesAppRuntimeMetrics(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != 13 {
		t.Fatalf("applicationSchemaVersion = %d, want 13", applicationSchemaVersion)
	}
	const wantChecksum = "08e62ab635873e100445bafd527a9f13ee55626cdb9c839eab476797cb540a5d"
	if got := applicationSchemaV13Checksum(); got != wantChecksum {
		t.Fatalf("applicationSchemaV13Checksum() = %q, want frozen %q", got, wantChecksum)
	}
	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 13, 13)
	if err := database.View(t.Context(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		if !connection.Migrator().HasColumn(&jobRunModel{}, "resume_consumed_by_job_id") {
			t.Error("schema v13 job_runs.resume_consumed_by_job_id missing")
		}
		for _, object := range metricsSchemaObjects {
			exists, err := verifySchemaObject(ctx, connection, object)
			if err != nil {
				return err
			}
			if !exists {
				t.Errorf("schema v13 %s %q missing", object.objectType, object.name)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect schema v13: %v", err)
	}
}

// 测试 v12 升级到 v13 追加 metrics 对象、回填 resume consumption 并保留既有 quota scheduling schema。
func TestApplicationMigrationUpgradesV12ToV13WithoutChangingExistingSchema(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV12(t, database)
	if err := database.Write(t.Context(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := transaction.WithContext(ctx).Table("job_runs").Create(map[string]any{
			"job_id": "v12-interrupted-parent", "job_type": "scan", "requested_by": "test",
			"priority": 1, "state": string(JobInterrupted), "phase": string(JobPhaseLive),
			"created_at_ms": 10, "started_at_ms": 11, "finished_at_ms": 12, "updated_at_ms": 12,
		}).Error; err != nil {
			return err
		}
		if err := transaction.WithContext(ctx).Table("job_runs").Create(map[string]any{
			"job_id": "v12-recoverable-orphan", "job_type": "scan", "requested_by": "test",
			"priority": 1, "state": string(JobInterrupted), "phase": string(JobPhaseLive),
			"created_at_ms": 10, "started_at_ms": 11, "finished_at_ms": 12, "updated_at_ms": 12,
		}).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Table("job_runs").Create(map[string]any{
			"job_id": "v12-resume-child", "job_type": "scan", "requested_by": "test",
			"priority": 1, "state": string(JobQueued), "phase": string(JobPhaseLive),
			"resume_of_job_id": "v12-interrupted-parent", "created_at_ms": 13, "updated_at_ms": 13,
		}).Error
	}); err != nil {
		t.Fatalf("seed v12 resume lineage: %v", err)
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
		return "/tmp/application-v12-before-v13.db", nil
	}
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("run(v12->v13) error = %v", err)
	}
	if report.FromVersion != 12 || report.TargetVersion != 13 ||
		!equalInts(report.AppliedVersions, []int{13}) || backupVersions != [2]int{12, 13} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 13, 13)
	if err := database.View(t.Context(), func(_ context.Context, connection storesqlite.ReadConn) error {
		var parent jobRunModel
		if err := connection.Where("job_id = ?", "v12-interrupted-parent").Take(&parent).Error; err != nil {
			return err
		}
		if parent.ResumeConsumedByJobID == nil || *parent.ResumeConsumedByJobID != "v12-resume-child" {
			t.Errorf("v13 resume consumption backfill = %#v", parent.ResumeConsumedByJobID)
		}
		var orphan jobRunModel
		if err := connection.Where("job_id = ?", "v12-recoverable-orphan").Take(&orphan).Error; err != nil {
			return err
		}
		if orphan.ResumeConsumedByJobID != nil {
			t.Errorf("v13 orphan resume consumption = %#v, want nil", orphan.ResumeConsumedByJobID)
		}
		for _, object := range quotaScheduleSchemaObjects {
			if object.objectType == "table" && !connection.Migrator().HasTable(object.name) {
				t.Errorf("v13 migration removed v12 table %q", object.name)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect preserved v12 schema: %v", err)
	}
}

// 测试 v13 任一步失败会回滚全部 metrics 对象并保持 v12 ledger/user_version。
func TestApplicationMigrationRollsBackFailedV13Atomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV12(t, database)
	want := errors.New("injected v13 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	catalog[12].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := addMetricsMigrationColumns(ctx, transaction); err != nil {
			return err
		}
		if err := ensureSchemaObjects(ctx, transaction, metricsSchemaObjects[:1]); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v12-before-failed-v13.db", nil
	}
	if _, err := runner.run(t.Context()); !errors.Is(err, want) {
		t.Fatalf("run() error = %v, want injected failure", err)
	}
	assertMigrationVersionAndHistory(t, database, 12, 12)
	if err := database.View(t.Context(), func(_ context.Context, connection storesqlite.ReadConn) error {
		if connection.Migrator().HasColumn(&jobRunModel{}, "resume_consumed_by_job_id") {
			t.Error("failed v13 migration left resume_consumed_by_job_id")
		}
		for _, object := range metricsSchemaObjects {
			if object.objectType == "table" && connection.Migrator().HasTable(object.name) {
				t.Errorf("failed v13 migration left table %q", object.name)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v13 rollback: %v", err)
	}
}

func seedApplicationSchemaV12(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:12]
	runner.verifyCurrent = verifyApplicationSchemaV12
	if report, err := runner.run(t.Context()); err != nil || report.TargetVersion != 12 {
		t.Fatalf("seed application schema v12 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 12, 12)
}

func verifyApplicationSchemaV12(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjectsThroughV12(), retentionSchemaObjects,
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects, quotaSchemaObjects, quotaProjectionSchemaObjects,
		quotaScheduleSchemaObjects,
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
