package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func dshTitleSnapshot() DSHSnapshot {
	return DSHSnapshot{Generation: 1, CollectedAtMS: 2000,
		Sessions:    []DSHSession{{ExternalSessionID: "dsh-title-test", DisplayTitle: "未命名会话", TitleSource: "fallback", ProjectKey: strings.Repeat("a", 64), ProjectDisplayName: "demo", CreatedAtMS: 1000, LastActivityAtMS: 1500, RequestCount: 1, ToolCallCount: 1, CoverageState: "exact", UpdatedAtMS: 2000}},
		Lineage:     []DSHSessionLineage{{ExternalSessionID: "dsh-title-test", SourceKey: "dsh.logs", LineageKey: strings.Repeat("b", 64), ContentDigest: strings.Repeat("c", 64), ObservedAtMS: 2000}},
		UsageEvents: []DSHUsageEvent{{EventID: strings.Repeat("d", 64), ExternalSessionID: "dsh-title-test", OccurredAtMS: 1500, InputTokens: 10, OutputTokens: 20, TotalTokens: 30, TotalKnown: true, UpdatedAtMS: 2000}},
		ToolEvents:  []DSHToolEvent{{EventID: strings.Repeat("e", 64), ExternalSessionID: "dsh-title-test", OccurredAtMS: 1500, ToolName: "read_file", Outcome: "succeeded", UpdatedAtMS: 2000}},
	}
}

func TestDSHTitleBackfillWithUnchangedDigestAndReportingIdentity(t *testing.T) {
	r := openRuntimeRepository(t)
	snapshot := dshTitleSnapshot()
	if err := r.ReplaceDSHSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	before, err := r.ReportingPage(t.Context(), "dsh", ReportingSource{HomeID: "opaque-home", Partition: "local"}, "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Generation++
	snapshot.Sessions[0].DisplayTitle = strings.Repeat("会话", 200)
	snapshot.Sessions[0].TitleSource = "dsh_title_event"
	// The log file already contained the title; upgrading the parser changes no digest.
	if err := r.ReplaceDSHSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	read, err := r.DSHSnapshot(t.Context())
	if err != nil || len(read.Sessions) != 1 || read.Sessions[0].DisplayTitle != snapshot.Sessions[0].DisplayTitle || read.Sessions[0].TitleSource != "dsh_title_event" {
		t.Fatal("title backfill lost", err)
	}
	after, err := r.ReportingPage(t.Context(), "dsh", ReportingSource{HomeID: "opaque-home", Partition: "local"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if after.Sessions[0].Title != snapshot.Sessions[0].DisplayTitle || before.Sessions[0].SessionID != after.Sessions[0].SessionID || !reflect.DeepEqual(before.Sessions[0].Contributions, after.Sessions[0].Contributions) {
		t.Fatal("title change altered reporting usage identity")
	}
	snapshot.Generation++
	if err := r.ReplaceDSHSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	read, err = r.DSHSnapshot(t.Context())
	if err != nil || len(read.Lineage) != 1 || len(read.UsageEvents) != 1 || len(read.ToolEvents) != 1 {
		t.Fatal("backfill duplicated facts", err)
	}
}

func TestDSHTitleMigrationPreservesV36FactsAndChecksum(t *testing.T) {
	const frozenV36 = "0e83c4b1708adc579ccc2997ce2ba095d2096dcdc362c64ce8c81841898aedf1"
	if applicationSchemaV36Checksum() != frozenV36 {
		t.Fatal("v36 checksum changed")
	}
	database, r := seedDSHTitleV36(t)
	before, err := r.DSHSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	runner := dshTitleUpgradeRunner(database)
	report, err := runner.run(t.Context())
	if err != nil || !equalInts(report.AppliedVersions, []int{37}) {
		t.Fatal("title migration failed", report, err)
	}
	after, err := r.DSHSnapshot(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration lost indexed facts", err)
	}
	if err := r.EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	before.Sessions[0].DisplayTitle = strings.Repeat("名", 512)
	before.Sessions[0].TitleSource = "dsh_title_event"
	if err := r.ReplaceDSHSnapshot(t.Context(), before); err != nil {
		t.Fatal("widened title contract unavailable", err)
	}
	assertMigrationVersionAndHistory(t, database, 37, 37)
}

func TestDSHTitleMigrationRollsBackLineageAndFacts(t *testing.T) {
	database, r := seedDSHTitleV36(t)
	before, err := r.DSHSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	runner := dshTitleUpgradeRunner(database)
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	want := errors.New("injected title migration failure")
	original := catalog[36].apply
	catalog[36].apply = func(ctx context.Context, tx *gorm.DB) error {
		if err := original(ctx, tx); err != nil {
			return err
		}
		return want
	}
	runner.catalog = catalog
	if _, err := runner.run(t.Context()); !errors.Is(err, want) {
		t.Fatal("expected rollback", err)
	}
	after, err := r.DSHSnapshot(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rollback lost lineage or facts", err)
	}
	assertMigrationVersionAndHistory(t, database, 36, 36)
	if _, err := dshTitleUpgradeRunner(database).run(t.Context()); err != nil {
		t.Fatal("retry after rollback failed", err)
	}
}

func seedDSHTitleV36(t *testing.T) (*storesqlite.Store, *Repository) {
	t.Helper()
	database := openTestDatabase(t)
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:36]
	runner.verifyCurrent = func(context.Context, *gorm.DB) error { return nil }
	if _, err := runner.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := NewRepository(database)
	if err := r.ReplaceDSHSnapshot(t.Context(), dshTitleSnapshot()); err != nil {
		t.Fatal(err)
	}
	return database, r
}

func dshTitleUpgradeRunner(database *storesqlite.Store) migrationRunner {
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/dsh-title-test-backup.db", nil
	}
	return runner
}
