package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func seedAGYPreV40(t *testing.T) (*storesqlite.Store, *Repository) {
	t.Helper()
	database := openTestDatabase(t)
	runner := applicationMigrationRunnerForTest(database)
	runner.catalog = applicationMigrations[:39]
	runner.verifyCurrent = func(context.Context, *gorm.DB) error { return nil }
	if _, err := runner.run(t.Context()); err != nil {
		t.Fatalf("seed v39 database error = %v", err)
	}
	repo := NewRepository(database)
	if err := repo.ReplaceDSHSnapshot(t.Context(), dshTitleSnapshot()); err != nil {
		t.Fatalf("seed DSH snapshot in v39 error = %v", err)
	}
	return database, repo
}

func agyUpgradeRunner(database *storesqlite.Store) migrationRunner {
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error) {
		return "/tmp/agy-v40-test-backup.db", nil
	}
	return runner
}

func TestAGYFreshSchemaAndNoToolEventsTable(t *testing.T) {
	database := openTestDatabase(t)
	repo := NewRepository(database)
	if err := repo.EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaV40Version, applicationSchemaV40Version)

	err := database.ViewSnapshot(t.Context(), func(_ context.Context, tx *gorm.DB) error {
		for _, table := range []string{
			"agy_sessions",
			"agy_session_lineage",
			"agy_usage_events",
			"agy_quota_snapshots",
			"agy_quota_observations",
		} {
			if !tx.Migrator().HasTable(table) {
				t.Fatalf("expected table %s to exist", table)
			}
		}
		if tx.Migrator().HasTable("agy_tool_events") {
			t.Fatal("agy_tool_events must not exist (tools/skills are out of scope)")
		}
		if tx.Migrator().HasColumn(&agySessionModel{}, "tool_call_count") {
			t.Fatal("agy_sessions must not contain tool_call_count column")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect schema error = %v", err)
	}
}

func TestAGYMigrationV39ToV40PreservesExistingFactsAndChecksums(t *testing.T) {
	const frozenV36 = "0e83c4b1708adc579ccc2997ce2ba095d2096dcdc362c64ce8c81841898aedf1"
	if applicationSchemaV36Checksum() != frozenV36 {
		t.Fatalf("v36 checksum drifted: %s", applicationSchemaV36Checksum())
	}
	database, repo := seedAGYPreV40(t)
	dshBefore, err := repo.DSHSnapshot(t.Context())
	if err != nil {
		t.Fatalf("DSHSnapshot(before) error = %v", err)
	}

	runner := agyUpgradeRunner(database)
	report, err := runner.run(t.Context())
	if err != nil {
		t.Fatalf("v39->v40 run() error = %v", err)
	}
	if report.FromVersion != 39 || report.TargetVersion != 40 || !equalInts(report.AppliedVersions, []int{40}) {
		t.Fatalf("report = %#v, want v39->v40 applying [40]", report)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaV40Version, applicationSchemaV40Version)

	dshAfter, err := repo.DSHSnapshot(t.Context())
	if err != nil || !reflect.DeepEqual(dshBefore, dshAfter) {
		t.Fatalf("DSH facts changed across v40 migration: err=%v", err)
	}
	if err := repo.EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema(reopen) error = %v", err)
	}

	// 验证 provider 白名单接受 'agy' 且拒绝未知客户端
	err = database.Write(t.Context(), func(_ context.Context, tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO agent_provider_snapshots (provider, generation, collected_at_ms) VALUES ('unknown_client', 1, 1000)`).Error
	})
	if err == nil {
		t.Fatal("agent_provider_snapshots accepted unknown provider")
	}
}

func TestAGYMigrationV40RollsBackAtomicallyOnFailure(t *testing.T) {
	database, repo := seedAGYPreV40(t)
	dshBefore, err := repo.DSHSnapshot(t.Context())
	if err != nil {
		t.Fatalf("DSHSnapshot(before) error = %v", err)
	}
	runner := agyUpgradeRunner(database)
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	injected := errors.New("injected v40 failure")
	original := catalog[39].apply
	catalog[39].apply = func(ctx context.Context, tx *gorm.DB) error {
		if err := original(ctx, tx); err != nil {
			return err
		}
		return injected
	}
	runner.catalog = catalog
	if _, err := runner.run(t.Context()); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 39, 39)
	dshAfter, err := repo.DSHSnapshot(t.Context())
	if err != nil || !reflect.DeepEqual(dshBefore, dshAfter) {
		t.Fatalf("rollback lost DSH facts: err=%v", err)
	}
	retry := agyUpgradeRunner(database)
	if _, err := retry.run(t.Context()); err != nil {
		t.Fatalf("retry v40 migration failed: %v", err)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaV40Version, applicationSchemaV40Version)
}

func TestAGYRepositoryCRUDConstraintsAndProviderIsolation(t *testing.T) {
	repo := openRuntimeRepository(t)
	ctx := t.Context()

	// 先写入一份 DSH 快照，验证 AGY 读写与 DSH 严格隔离
	dshSeed := dshTitleSnapshot()
	if err := repo.ReplaceDSHSnapshot(ctx, dshSeed); err != nil {
		t.Fatalf("ReplaceDSHSnapshot() error = %v", err)
	}

	at := int64(1_760_000_000_000)
	model := "gemini-3-pro"
	parentID := "cascade-main-1"
	started := at - 2000
	ended := at - 500
	outTokens := int64(300)
	activeDuration := int64(1500)
	snapshot := AGYSnapshot{
		Generation:    1,
		CollectedAtMS: at,
		Sources: []CursorSourceStatus{{
			Provider:        "agy",
			SourceKey:       "agy.antigravity.summaries",
			SourceType:      "sqlite",
			State:           "available",
			CoverageState:   "exact",
			CheckpointKind:  "snapshot",
			RowCount:        2,
			LastAttemptAtMS: at,
			LastSuccessAtMS: &at,
			UpdatedAtMS:     at,
		}},
		Sessions: []AGYSession{
			{
				ExternalSessionID:  "cascade-main-1",
				DisplayTitle:       "AGY 主会话",
				TitleSource:        "agy_annotation",
				SessionRole:        "main",
				ProjectKey:         strings.Repeat("a", 64),
				ProjectDisplayName: "codex-pulse",
				ProjectSource:      "workspace",
				CreatedAtMS:        at - 5000,
				LastActivityAtMS:   at,
				ModelKey:           &model,
				RequestCount:       1,
				CoverageState:      "exact",
				Throughput: &reportingv1.ThroughputCapsule{
					Version: 1,
					Basis:   "session_lifecycle",
					Measures: reportingv1.ThroughputMeasures{
						OutputTokens:     &outTokens,
						ActiveDurationMS: &activeDuration,
						IncludedTurns:    1,
						CoverageKnown:    true,
						Status:           "complete",
						DurationSource:   "log_timestamp",
					},
					TurnsTotal: 1,
				},
				UpdatedAtMS: at,
			},
			{
				ExternalSessionID:       "cascade-sub-1",
				DisplayTitle:            "子代理调研",
				TitleSource:             "agy_summary",
				ParentExternalSessionID: &parentID,
				SessionRole:             "subagent",
				ProjectKey:              strings.Repeat("a", 64),
				ProjectDisplayName:      "codex-pulse",
				ProjectSource:           "inherited_parent",
				CreatedAtMS:             at - 4000,
				LastActivityAtMS:        at - 1000,
				ModelKey:                &model,
				RequestCount:            1,
				CoverageState:           "partial",
				UpdatedAtMS:             at,
			},
		},
		Lineage: []AGYSessionLineage{
			{
				ExternalSessionID: "cascade-main-1",
				SourceKey:         "agy.antigravity",
				LineageKey:        strings.Repeat("1", 64),
				ContentDigest:     strings.Repeat("2", 64),
				ObservedAtMS:      at,
			},
			{
				ExternalSessionID: "cascade-sub-1",
				SourceKey:         "agy.antigravity-ide",
				LineageKey:        strings.Repeat("3", 64),
				ContentDigest:     strings.Repeat("4", 64),
				ObservedAtMS:      at,
			},
		},
		UsageEvents: []AGYUsageEvent{
			{
				EventID:             strings.Repeat("b", 64),
				ExternalSessionID:   "cascade-main-1",
				StepIndex:           0,
				OccurredAtMS:        at,
				ModelKey:            &model,
				UncachedInputTokens: 120,
				CacheReadTokens:     80,
				OutputTokens:        300,
				ReasoningTokens:     50,
				TotalTokens:         500,
				CacheReadKnown:      true,
				ReasoningKnown:      true,
				TotalKnown:          true,
				StartedAtMS:         &started,
				EndedAtMS:           &ended,
				Provenance:          "agy_step_metadata",
				UpdatedAtMS:         at,
			},
			{
				EventID:             strings.Repeat("c", 64),
				ExternalSessionID:   "cascade-sub-1",
				StepIndex:           1,
				OccurredAtMS:        at - 1000,
				ModelKey:            &model,
				UncachedInputTokens: 50,
				CacheReadTokens:     0,
				OutputTokens:        100,
				ReasoningTokens:     0,
				TotalTokens:         150,
				CacheReadKnown:      false,
				ReasoningKnown:      false,
				TotalKnown:          true,
				Provenance:          "agy_transcript_jsonl",
				UpdatedAtMS:         at,
			},
		},
	}
	if err := repo.ReplaceAGYSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("ReplaceAGYSnapshot() error = %v", err)
	}
	got, err := repo.AGYSnapshot(ctx)
	if err != nil {
		t.Fatalf("AGYSnapshot() error = %v", err)
	}
	if got.Generation != 1 || len(got.Sessions) != 2 || len(got.Lineage) != 2 || len(got.UsageEvents) != 2 || len(got.Sources) != 1 {
		t.Fatalf("AGYSnapshot() = %#v", got)
	}
	if got.Sessions[0].Throughput == nil || got.Sessions[0].Throughput.Measures.OutputTokens == nil || *got.Sessions[0].Throughput.Measures.OutputTokens != 300 {
		t.Fatalf("throughput capsule lost: %#v", got.Sessions[0])
	}

	// 来源暂时不可用时保留历史会话和用量事件
	degraded := AGYSnapshot{
		Generation:    2,
		CollectedAtMS: at + 1000,
		Sources: []CursorSourceStatus{{
			Provider:        "agy",
			SourceKey:       "agy.antigravity.summaries",
			SourceType:      "sqlite",
			State:           "unavailable",
			CoverageState:   "unknown",
			CheckpointKind:  "snapshot",
			RowCount:        0,
			LastAttemptAtMS: at + 1000,
			UpdatedAtMS:     at + 1000,
		}},
	}
	if err := repo.ReplaceAGYSnapshot(ctx, degraded); err != nil {
		t.Fatalf("ReplaceAGYSnapshot(degraded) error = %v", err)
	}
	afterDegraded, err := repo.AGYSnapshot(ctx)
	if err != nil || len(afterDegraded.Sessions) != 2 || len(afterDegraded.UsageEvents) != 2 {
		t.Fatalf("degraded snapshot lost history: %#v, err=%v", afterDegraded, err)
	}

	// 多配额池快照读写 + 拒绝邮箱原文作为 account_scope
	scope := strings.Repeat("d", 64)
	plan := "Google AI Ultra 20x"
	usedPrimary := int64(35)
	usedWeekly := int64(60)
	resetPrimary := at + 3600_000
	resetWeekly := at + 7*24*3600_000
	quotas := []AGYQuotaSnapshot{
		{
			AccountScope: scope, Generation: 1, PlanType: &plan,
			LimitID: "agy.gemini", LimitName: "Gemini", WindowKind: "primary",
			WindowMinutes: 300, UsedPercent: &usedPrimary, ResetsAtMS: &resetPrimary,
			DataAsOfMS: at, UpdatedAtMS: at,
		},
		{
			AccountScope: scope, Generation: 1, PlanType: &plan,
			LimitID: "agy.claude_gpt", LimitName: "Claude & GPT", WindowKind: "secondary",
			WindowMinutes: 10080, UsedPercent: &usedWeekly, ResetsAtMS: &resetWeekly,
			DataAsOfMS: at, UpdatedAtMS: at,
		},
	}
	if err := repo.ReplaceAGYQuotaSnapshots(ctx, scope, quotas); err != nil {
		t.Fatalf("ReplaceAGYQuotaSnapshots() error = %v", err)
	}
	gotQuotas, err := repo.AGYQuotaSnapshots(ctx, scope)
	if err != nil || len(gotQuotas) != 2 {
		t.Fatalf("AGYQuotaSnapshots() = %#v, err=%v", gotQuotas, err)
	}
	if err := repo.ReplaceAGYQuotaSnapshots(ctx, "raw-user@example.com", nil); err == nil {
		t.Fatal("ReplaceAGYQuotaSnapshots() accepted raw email as account_scope")
	}

	// 配额观测点有界保留
	for i := int64(1); i <= 5; i++ {
		if err := repo.AppendAGYQuotaObservation(ctx, AGYQuotaObservation{
			AccountScope: scope,
			LimitID:      "agy.gemini",
			WindowKind:   "primary",
			ObservedAtMS: at + i*60_000,
			UsedPercent:  i * 10,
			ResetsAtMS:   &resetPrimary,
		}, 3); err != nil {
			t.Fatalf("AppendAGYQuotaObservation(%d) error = %v", i, err)
		}
	}
	observations, err := repo.AGYQuotaObservations(ctx, scope, "agy.gemini", "primary")
	if err != nil || len(observations) != 3 {
		t.Fatalf("AGYQuotaObservations() len=%d, err=%v", len(observations), err)
	}
	if observations[0].UsedPercent != 30 || observations[2].UsedPercent != 50 {
		t.Fatalf("retention kept wrong observations: %#v", observations)
	}

	// 确认 DSH 表与快照未受 AGY 写入影响
	dshAfter, err := repo.DSHSnapshot(ctx)
	if err != nil || len(dshAfter.Sessions) != 1 || dshAfter.Sessions[0].ExternalSessionID != "dsh-title-test" {
		t.Fatalf("DSH snapshot affected by AGY writes: %#v, err=%v", dshAfter, err)
	}
}
