package store

import (
	"context"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationMigrationV19AddsQuotaLimitNameWithoutChangingExistingFacts(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV18ForQuotaLimitName(t, database)
	repository := NewRepository(database)
	requestID := "v18-general-request"
	limitID := "codex"
	plan := "pro"
	existing := QuotaObservationSample{
		ObservationID: "v18-general", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceWham, LimitID: &limitID, WindowKind: QuotaWindowPrimary,
		UsedPercent: 20, WindowMinutes: 10_080, ResetsAtMS: 700_000_000,
		PlanType: &plan, ObservedAtMS: 100_000_000, Validity: QuotaValidityAccepted,
		RequestID: &requestID,
	}
	if err := database.Write(t.Context(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Omit("limit_name").Create(
			quotaObservationModelFromSample(existing),
		).Error
	}); err != nil {
		t.Fatalf("seed v18 quota observation: %v", err)
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
		return "/tmp/application-v18-before-v19.db", nil
	}
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("run(v18->v19) error = %v", err)
	}
	if report.FromVersion != 18 || report.TargetVersion != 19 ||
		!equalInts(report.AppliedVersions, []int{19}) || backupVersions != [2]int{18, 19} {
		t.Fatalf("migration report = %#v backup=%v", report, backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 19, 19)

	preserved, err := repository.QuotaObservation(t.Context(), existing.ObservationID)
	if err != nil || preserved.LimitName != nil || preserved.UsedPercent != existing.UsedPercent {
		t.Fatalf("preserved observation = %#v, %v", preserved, err)
	}

	additionalRequestID := "v19-spark-request"
	additionalLimitID := "codex_spark"
	additionalLimitName := "GPT-5.3-Codex-Spark"
	additional := QuotaObservationSample{
		ObservationID: "v19-spark", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceWham, LimitID: &additionalLimitID, LimitName: &additionalLimitName,
		WindowKind: QuotaWindowPrimary, UsedPercent: 0, WindowMinutes: 10_080,
		ResetsAtMS: 700_000_001, PlanType: &plan, ObservedAtMS: 100_000_001,
		Validity: QuotaValidityAccepted, RequestID: &additionalRequestID,
	}
	if err := repository.UpsertFacts(t.Context(), FactBatch{QuotaObservation: &additional}); err != nil {
		t.Fatalf("write named v19 quota observation: %v", err)
	}
	stored, err := repository.QuotaObservation(t.Context(), additional.ObservationID)
	if err != nil || stored.LimitName == nil || *stored.LimitName != additionalLimitName {
		t.Fatalf("named observation = %#v, %v", stored, err)
	}
}

func seedApplicationSchemaV18ForQuotaLimitName(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:18]
	runner.verifyCurrent = verifyApplicationSchemaV18
	if report, err := runner.run(t.Context()); err != nil || report.TargetVersion != 18 {
		t.Fatalf("seed application schema v18 = %#v, %v", report, err)
	}
	assertMigrationVersionAndHistory(t, database, 18, 18)
}
