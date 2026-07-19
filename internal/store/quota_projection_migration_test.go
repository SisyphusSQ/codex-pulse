package store

import (
	"context"
	"errors"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

func TestApplicationSchemaV11CreatesQuotaProjection(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != applicationSchemaV16Version {
		t.Fatalf("applicationSchemaVersion = %d, want 16", applicationSchemaVersion)
	}
	const wantChecksum = "838ab8173f637ae8f702b3f4e2139bf1d6810941b0a83d1c258743183d914475"
	if got := applicationSchemaV11Checksum(); got != wantChecksum {
		t.Fatalf("applicationSchemaV11Checksum() = %q, want frozen %q", got, wantChecksum)
	}
	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, object := range quotaProjectionSchemaObjects {
			if object.objectType == "table" && !connection.Migrator().HasTable(object.name) {
				t.Errorf("schema v11 table %q missing", object.name)
			}
			if object.objectType == "index" {
				model := any(&quotaCurrentModel{})
				if object.name == "idx_quota_arbitration_evidence_observation" {
					model = &quotaArbitrationEvidenceModel{}
				}
				if !connection.Migrator().HasIndex(model, object.name) {
					t.Errorf("schema v11 index %q missing", object.name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect schema v11: %v", err)
	}
}

func TestApplicationMigrationUpgradesV10ThroughCurrentWithoutChangingRawObservations(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV10(t, database)
	observation := quotaObservationModelFromSample(quotaProjectionWhamSample(
		"v10-wham", 38, 1_000_000, 1_000_000+5*quotaTestHourMS,
	))
	if err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Create(observation).Error
	}); err != nil {
		t.Fatalf("seed v10 observation: %v", err)
	}

	runner := applicationMigrationRunnerForTest(database)
	runner.now = func() time.Time { return time.UnixMilli(1_000_000 + quotaTestMinuteMS) }
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v10-before-v11.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run(v10->v11) error = %v", err)
	}
	if report.FromVersion != 10 || report.TargetVersion != applicationSchemaVersion ||
		!equalInts(report.AppliedVersions, []int{11, 12, 13, 14, 15, 16}) {
		t.Fatalf("migration report = %#v", report)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
	repository := NewRepository(database)
	current, err := repository.QuotaCurrent(
		context.Background(), QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", 1_000_000+quotaTestMinuteMS,
	)
	if err != nil || current.ObservationID == nil || *current.ObservationID != observation.ObservationID {
		t.Fatalf("QuotaCurrent(after migration backfill) = %#v, %v", current, err)
	}
	readback, err := repository.QuotaObservation(context.Background(), observation.ObservationID)
	if err != nil || readback.UsedPercent != 38 || readback.Validity != QuotaValidityAccepted {
		t.Fatalf("raw observation = %#v, %v", readback, err)
	}
}

func TestApplicationMigrationV11UsesTrustedClockForFutureObservation(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV10(t, database)
	now := int64(20 * quotaTestHourMS)
	observed := now + defaultQuotaArbitrationRule().MaxClockSkewMS + 1
	observation := quotaObservationModelFromSample(quotaProjectionWhamSample(
		"v10-future-wham", 38, observed, observed+5*quotaTestHourMS,
	))
	if err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Create(observation).Error
	}); err != nil {
		t.Fatalf("seed future v10 observation: %v", err)
	}

	runner := applicationMigrationRunnerForTest(database)
	runner.now = func() time.Time { return time.UnixMilli(now) }
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v10-future-before-v11.db", nil
	}
	if _, err := runner.run(context.Background()); err != nil {
		t.Fatalf("run(v10->v11 future observation) error = %v", err)
	}
	repository := NewRepository(database)
	assertQuotaCurrentNeverLoaded(t, repository, now)
	evidence, err := repository.ListQuotaArbitrationEvidence(
		context.Background(), QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	)
	if err != nil {
		t.Fatalf("ListQuotaArbitrationEvidence() error = %v", err)
	}
	assertQuotaEvidence(t, evidence, observation.ObservationID, QuotaEvidenceSuspicious, QuotaReasonObservedRegression)
}

func TestApplicationMigrationV11BatchesEvidenceBeyondSQLiteVariableLimit(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV10(t, database)
	const historySize = 4096
	base := int64(220 * quotaTestHourMS)
	reset := base + 5*quotaTestHourMS
	if err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		models := make([]*quotaObservationModel, 0, historySize)
		for index := 0; index < historySize; index++ {
			sample := quotaProjectionWhamSample(
				"migration-history-"+formatQuotaTestIndex(index), 40, base+int64(index), reset,
			)
			models = append(models, quotaObservationModelFromSample(sample))
		}
		return transaction.WithContext(ctx).CreateInBatches(&models, 256).Error
	}); err != nil {
		t.Fatalf("seed %d v10 observations: %v", historySize, err)
	}

	runner := applicationMigrationRunnerForTest(database)
	runner.now = func() time.Time { return time.UnixMilli(base + historySize) }
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v10-history-before-v11.db", nil
	}
	if _, err := runner.run(context.Background()); err != nil {
		t.Fatalf("run(v10->v11 %d observations) error = %v", historySize, err)
	}
	evidence, err := NewRepository(database).ListQuotaArbitrationEvidence(
		context.Background(), QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	)
	if err != nil || len(evidence) != historySize {
		t.Fatalf("migration evidence count = %d, %v; want %d", len(evidence), err, historySize)
	}
}

func TestApplicationMigrationRollsBackFailedV11Atomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV10(t, database)
	want := errors.New("injected v11 failure")
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	catalog[10].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := ensureSchemaObjects(ctx, transaction, quotaProjectionSchemaObjects[:1]); err != nil {
			return err
		}
		return want
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = catalog
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/application-v10-before-failed-v11.db", nil
	}
	if _, err := runner.run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("run(failed v11) error = %v, want injected failure", err)
	}
	assertMigrationVersionAndHistory(t, database, 10, 10)
	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, table := range []string{"quota_current", "quota_arbitration_evidence"} {
			if connection.Migrator().HasTable(table) {
				t.Fatalf("failed v11 migration left %s behind", table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect v11 rollback: %v", err)
	}
}

func seedApplicationSchemaV10(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:10]
	runner.verifyCurrent = verifyApplicationSchemaV10
	if report, err := runner.run(context.Background()); err != nil || report.TargetVersion != 10 {
		t.Fatalf("seed application schema v10 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 10, 10)
}

func verifyApplicationSchemaV10(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjectsThroughV12(), retentionSchemaObjects,
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
	return verifySourceFailureColumns(transaction)
}

func quotaProjectionWhamSample(id string, used float64, observedAt, reset int64) QuotaObservationSample {
	limitID, requestID := "codex", "request-"+id
	return QuotaObservationSample{
		ObservationID: id, AccountScope: QuotaAccountScopeDefault, Source: QuotaSourceWham,
		LimitID: &limitID, WindowKind: QuotaWindowPrimary, UsedPercent: used,
		WindowMinutes: 300, ResetsAtMS: reset, ObservedAtMS: observedAt,
		Validity: QuotaValidityAccepted, RequestID: &requestID,
	}
}
