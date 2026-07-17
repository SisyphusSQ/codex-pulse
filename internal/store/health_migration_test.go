package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationSchemaV14CreatesHealthEvaluatorEventAllowlist(t *testing.T) {
	t.Parallel()
	if applicationSchemaVersion != applicationSchemaV14Version {
		t.Fatalf("applicationSchemaVersion = %d, want 14", applicationSchemaVersion)
	}
	const wantChecksum = "684650b2128c1aeb7db65433d6f6e3349111fff714804e694dfa98c097ed11af"
	if got := applicationSchemaV14Checksum(); got != wantChecksum {
		t.Fatalf("applicationSchemaV14Checksum() = %q, want frozen %q", got, wantChecksum)
	}
	repository := openRuntimeRepository(t)
	allowed := []struct {
		domain HealthDomain
		code   HealthCode
	}{
		{HealthDomainSource, HealthCodeSourceAuthRequired},
		{HealthDomainSource, HealthCodeSourceFailureStreak},
		{HealthDomainJob, HealthCodeJobLiveQueueStalled},
		{HealthDomainJob, HealthCodeJobBackfillStalled},
		{HealthDomainStore, HealthCodeStoreDiskLow},
		{HealthDomainStore, HealthCodeStoreWALPressure},
		{HealthDomainRuntime, HealthCodeRuntimeCPUPressure},
		{HealthDomainRuntime, HealthCodeRuntimeMemoryPressure},
		{HealthDomainRuntime, HealthCodeRuntimeMetricsStale},
		{HealthDomainRuntime, HealthCodeRuntimeUpdaterUnavailable},
		{HealthDomainRuntime, HealthCodeRuntimeUpdaterUnknown},
	}
	for index, value := range allowed {
		observation := healthEvaluationObservation("v14-allowed-"+string(rune('a'+index)), value.domain, value.code, int64(index))
		if _, err := repository.ObserveHealthEvent(t.Context(), observation); err != nil {
			t.Fatalf("ObserveHealthEvent(%s) error = %v", value.code, err)
		}
	}
}

func TestApplicationMigrationV13ToV14PreservesHealthLifecycle(t *testing.T) {
	t.Parallel()
	database := openTestDatabase(t)
	seedApplicationSchemaV13(t, database)
	repository := NewRepository(database)
	observation := healthEvaluationObservation("v13-preserved", HealthDomainRuntime, HealthCodeRuntimeUnknown, 10)
	if _, err := repository.ObserveHealthEvent(t.Context(), observation); err != nil {
		t.Fatalf("ObserveHealthEvent(v13) error = %v", err)
	}
	if err := repository.ResolveHealthEvent(t.Context(), observation.EventID, 11); err != nil {
		t.Fatalf("ResolveHealthEvent(v13) error = %v", err)
	}

	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(_ context.Context, fromVersion, targetVersion int, _ func(storesqlite.BackupProgress)) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v13-before-v14.db", nil
	}
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("run(v13->v14) error = %v", err)
	}
	if report.FromVersion != 13 || report.TargetVersion != 14 ||
		!equalInts(report.AppliedVersions, []int{14}) || backupVersions != [2]int{13, 14} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 14, 14)
	preserved, err := repository.HealthEvent(t.Context(), observation.EventID)
	if err != nil || preserved.ResolvedAtMS == nil || *preserved.ResolvedAtMS != 11 ||
		preserved.OccurrenceCount != 1 || preserved.Fingerprint.String() != observation.Fingerprint.String() {
		t.Fatalf("preserved health event = %#v, %v", preserved, err)
	}
}

func TestApplicationMigrationV13ToV14PreservesLargeHealthHistoryInBatches(t *testing.T) {
	t.Parallel()
	database := openTestDatabase(t)
	seedApplicationSchemaV13(t, database)
	const rowCount = 5_000
	models := make([]healthEventModel, rowCount)
	for index := range models {
		eventID := fmt.Sprintf("v13-large-%04d", index)
		models[index] = healthEventModelFromDomain(HealthEvent{
			EventID: eventID, Fingerprint: SHA256DigestOf([]byte(eventID)),
			Domain: HealthDomainRuntime, Severity: HealthWarning, Code: HealthCodeRuntimeUnknown,
			FirstSeenAtMS: int64(index), LastSeenAtMS: int64(index), OccurrenceCount: 1,
			UpdatedAtMS: int64(index),
		})
	}
	if err := database.WriteMaintenance(t.Context(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).CreateInBatches(&models, 100).Error
	}); err != nil {
		t.Fatalf("seed large v13 health history: %v", err)
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v13-before-large-v14.db", nil
	}
	if _, err := runner.run(t.Context()); err != nil {
		t.Fatalf("run(v13->v14 large history) error = %v", err)
	}
	if err := database.View(t.Context(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var count int64
		if err := connection.WithContext(ctx).Model(&healthEventModel{}).Count(&count).Error; err != nil {
			return err
		}
		if count != rowCount {
			t.Fatalf("migrated health row count = %d, want %d", count, rowCount)
		}
		return nil
	}); err != nil {
		t.Fatalf("read large v14 health history: %v", err)
	}
}

func TestApplicationMigrationRollsBackFailedV14Atomically(t *testing.T) {
	t.Parallel()
	database := openTestDatabase(t)
	seedApplicationSchemaV13(t, database)
	repository := NewRepository(database)
	observation := healthEvaluationObservation("v13-rollback", HealthDomainRuntime, HealthCodeRuntimeUnknown, 10)
	if _, err := repository.ObserveHealthEvent(t.Context(), observation); err != nil {
		t.Fatalf("ObserveHealthEvent(v13) error = %v", err)
	}
	want := errors.New("injected v14 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	original := catalog[13].apply
	catalog[13].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := original(ctx, transaction); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v13-before-failed-v14.db", nil
	}
	if _, err := runner.run(t.Context()); !errors.Is(err, want) {
		t.Fatalf("run() error = %v, want injected failure", err)
	}
	assertMigrationVersionAndHistory(t, database, 13, 13)
	if err := database.View(t.Context(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		if connection.Migrator().HasTable("health_events_v13") {
			t.Error("failed v14 migration left temporary table")
		}
		for _, object := range runtimeSchemaObjectsThroughV13() {
			if object.name != "health_events" {
				continue
			}
			valid, err := verifySchemaObject(ctx, connection, object)
			if err != nil || !valid {
				t.Errorf("v13 health event schema was not restored: valid=%v err=%v", valid, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect v14 rollback: %v", err)
	}
	if _, err := repository.HealthEvent(t.Context(), observation.EventID); err != nil {
		t.Fatalf("v13 event missing after rollback: %v", err)
	}
}

func seedApplicationSchemaV13(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:13]
	runner.verifyCurrent = verifyApplicationSchemaV13
	if report, err := runner.run(t.Context()); err != nil || report.TargetVersion != 13 {
		t.Fatalf("seed application schema v13 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 13, 13)
}

func verifyApplicationSchemaV13(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjectsThroughV13(), retentionSchemaObjects,
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects, quotaSchemaObjects, quotaProjectionSchemaObjects,
		quotaScheduleSchemaObjects, metricsSchemaObjects,
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
	if err := verifySourceFailureColumns(transaction); err != nil {
		return err
	}
	return verifyMetricsMigrationColumns(transaction)
}
