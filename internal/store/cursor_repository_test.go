package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCursorLocalReplacementPreservesLastDashboardSnapshot(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	model := "cursor-model"
	conversationID := "conversation-a"
	dashboard := CursorDashboardSnapshot{
		Generation: 10, CollectedAtMS: 2_000, WindowStartMS: 1_000, WindowEndMS: 2_000, BillingCycleEndMS: 3_000,
		Events: []CursorDashboardUsageEvent{{
			EventFingerprint: strings.Repeat("b", 64), OccurrenceCount: 1,
			ExternalSessionID: &conversationID, OccurredAtMS: 1_500, ModelKey: &model,
			InputTokens: 10, OutputTokens: 20, CacheWriteTokens: 30, CacheReadTokens: 40,
			ReportedChargeMicros: 15_000, CursorTokenFeeMicros: 2_500, UpdatedAtMS: 2_000,
		}},
	}
	if err := repository.CommitCursorDashboardSnapshot(ctx, dashboard); err != nil {
		t.Fatalf("CommitCursorDashboardSnapshot() error = %v", err)
	}

	local := CursorSnapshot{
		Generation: 11, CollectedAtMS: 2_100,
		Sources: []CursorSourceStatus{{
			Provider: "cursor", SourceKey: "cursor.state", SourceType: "sqlite_snapshot",
			State: "available", CoverageState: "exact", CheckpointKind: "snapshot",
			RowCount: 0, LastAttemptAtMS: 2_100, LastSuccessAtMS: pointerInt64ForCursorTest(2_100), UpdatedAtMS: 2_100,
		}},
	}
	if err := repository.ReplaceCursorSnapshot(ctx, local); err != nil {
		t.Fatalf("ReplaceCursorSnapshot() error = %v", err)
	}

	readback, err := repository.CursorSnapshot(ctx)
	if err != nil {
		t.Fatalf("CursorSnapshot() error = %v", err)
	}
	if len(readback.DashboardUsageEvents) != 1 || readback.DashboardUsageEvents[0].ReportedChargeMicros != 15_000 {
		t.Fatalf("dashboard usage after local replacement = %#v", readback.DashboardUsageEvents)
	}
	var dashboardSource *CursorSourceStatus
	for index := range readback.Sources {
		if readback.Sources[index].SourceKey == "cursor.dashboard" {
			dashboardSource = &readback.Sources[index]
		}
	}
	if dashboardSource == nil || dashboardSource.State != "available" || dashboardSource.LastSuccessAtMS == nil || *dashboardSource.LastSuccessAtMS != 2_000 {
		t.Fatalf("dashboard source after local replacement = %#v", dashboardSource)
	}
}

func TestCursorDashboardFailurePreservesLastSuccessAndEvents(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.CommitCursorDashboardSnapshot(ctx, CursorDashboardSnapshot{
		Generation: 1, CollectedAtMS: 2_000, WindowStartMS: 1_000, WindowEndMS: 2_000, BillingCycleEndMS: 3_000,
		Events: []CursorDashboardUsageEvent{{
			EventFingerprint: strings.Repeat("c", 64), OccurrenceCount: 1,
			OccurredAtMS: 1_500, InputTokens: 10, UpdatedAtMS: 2_000,
		}},
	}); err != nil {
		t.Fatalf("CommitCursorDashboardSnapshot() error = %v", err)
	}
	if err := repository.RecordCursorDashboardFailure(ctx, 3_000, "auth_expired"); err != nil {
		t.Fatalf("RecordCursorDashboardFailure() error = %v", err)
	}
	if err := repository.ReplaceCursorSnapshot(ctx, CursorSnapshot{Generation: 2, CollectedAtMS: 3_100}); err != nil {
		t.Fatalf("ReplaceCursorSnapshot() error = %v", err)
	}
	readback, err := repository.CursorSnapshot(ctx)
	if err != nil {
		t.Fatalf("CursorSnapshot() error = %v", err)
	}
	if len(readback.DashboardUsageEvents) != 1 {
		t.Fatalf("dashboard events after failure = %#v", readback.DashboardUsageEvents)
	}
	for _, source := range readback.Sources {
		if source.SourceKey != "cursor.dashboard" {
			continue
		}
		if source.State != "unavailable" || source.FailureCode == nil || *source.FailureCode != "auth_expired" ||
			source.LastSuccessAtMS == nil || *source.LastSuccessAtMS != 2_000 || source.LastAttemptAtMS != 3_000 {
			t.Fatalf("dashboard source after failure = %#v", source)
		}
		return
	}
	t.Fatal("dashboard source is missing")
}

func TestCursorDashboardQuotaHistoryRetainsZeroAndMultipleSnapshots(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	for _, snapshot := range []CursorDashboardSnapshot{
		{
			Generation: 1, CollectedAtMS: 2_000, WindowStartMS: 1_000,
			WindowEndMS: 2_000, BillingCycleEndMS: 4_000,
			QuotaWindows: []CursorDashboardQuotaWindow{
				{LimitID: "cursor.models", UsedPercent: 7},
				{LimitID: "cursor.other_models", UsedPercent: 0},
			},
		},
		{
			Generation: 2, CollectedAtMS: 3_000, WindowStartMS: 1_000,
			WindowEndMS: 3_000, BillingCycleEndMS: 4_000,
			QuotaWindows: []CursorDashboardQuotaWindow{
				{LimitID: "cursor.models", UsedPercent: 9},
				{LimitID: "cursor.other_models", UsedPercent: 0},
			},
		},
	} {
		if err := repository.CommitCursorDashboardSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("CommitCursorDashboardSnapshot() error = %v", err)
		}
	}
	if err := repository.ReplaceCursorSnapshot(ctx, CursorSnapshot{
		Generation: 3, CollectedAtMS: 3_100,
	}); err != nil {
		t.Fatalf("ReplaceCursorSnapshot() error = %v", err)
	}

	readback, err := repository.CursorSnapshot(ctx)
	if err != nil {
		t.Fatalf("CursorSnapshot() error = %v", err)
	}
	if len(readback.DashboardQuotaObservations) != 4 {
		t.Fatalf("quota observations = %#v", readback.DashboardQuotaObservations)
	}
	latest := readback.DashboardQuotaObservations[3]
	if latest.Generation != 2 || latest.LimitID != "cursor.other_models" || latest.UsedPercent != 0 {
		t.Fatalf("latest zero-percent quota observation = %#v", latest)
	}
}

func pointerInt64ForCursorTest(value int64) *int64 { return &value }

func TestCursorSnapshotRejectsRawPathBeforeAtomicReplacement(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	projectKey := strings.Repeat("a", 64)
	snapshot := CursorSnapshot{
		Generation: 1, CollectedAtMS: 2_000,
		Sources: []CursorSourceStatus{{
			Provider: "cursor", SourceKey: "cursor.state", SourceType: "sqlite_snapshot",
			State: "available", CoverageState: "exact", CheckpointKind: "snapshot",
			RowCount: 1, LastAttemptAtMS: 2_000, UpdatedAtMS: 2_000,
		}},
		Sessions: []CursorSession{{
			ExternalSessionID: "session-a", ProjectKey: projectKey,
			ProjectDisplayName: "Cursor 项目 aaaaaaaa", CreatedAtMS: 1_000,
			LastActivityAtMS: 1_500, RequestCount: 1, CoverageState: "exact", UpdatedAtMS: 2_000,
		}},
		RequestEvents: []CursorRequestEvent{{
			EventID: "generation-a", ExternalSessionID: "session-a",
			OccurredAtMS: 1_500, UpdatedAtMS: 2_000,
		}},
	}
	if err := repository.ReplaceCursorSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("ReplaceCursorSnapshot(valid) error = %v", err)
	}
	invalid := snapshot
	invalid.Generation = 2
	invalid.Sessions = append([]CursorSession(nil), snapshot.Sessions...)
	invalid.Sessions[0].ProjectDisplayName = "/Users/private/secret-project"
	if err := repository.ReplaceCursorSnapshot(ctx, invalid); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ReplaceCursorSnapshot(raw path) error = %v, want invalid record", err)
	}
	readback, err := repository.CursorSnapshot(ctx)
	if err != nil {
		t.Fatalf("CursorSnapshot() error = %v", err)
	}
	if readback.Generation != 1 || len(readback.Sessions) != 1 ||
		readback.Sessions[0].ProjectDisplayName != snapshot.Sessions[0].ProjectDisplayName {
		t.Fatalf("readback = %#v, invalid replacement must be atomic", readback)
	}
}
