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

func TestCursorGrokBotWriterKeepsIndependentCycleAndCrossFailureIsolation(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	percent := 12.5
	if err := repository.CommitCursorDashboardSnapshot(ctx, CursorDashboardSnapshot{
		Generation: 1, CollectedAtMS: 2_000, WindowStartMS: 1_000, WindowEndMS: 2_000, BillingCycleEndMS: 30_000,
		QuotaWindows: []CursorDashboardQuotaWindow{{
			LimitID: "cursor.models", UsedPercent: 7, CycleStartAtMS: 1_000, CycleEndAtMS: 30_000,
		}},
		Events: []CursorDashboardUsageEvent{{
			EventFingerprint: strings.Repeat("d", 64), OccurrenceCount: 1,
			OccurredAtMS: 1_500, InputTokens: 10, UpdatedAtMS: 2_000,
		}},
	}); err != nil {
		t.Fatalf("CommitCursorDashboardSnapshot() error = %v", err)
	}
	if err := repository.CommitCursorGrokBotObservation(ctx, CursorGrokBotCommit{
		Generation: 2, CollectedAtMS: 3_000, Included: true, UsedPercent: &percent,
		CycleStartAtMS: 2_500, CycleEndAtMS: 9_000,
	}); err != nil {
		t.Fatalf("CommitCursorGrokBotObservation() error = %v", err)
	}
	if err := repository.RecordCursorGrokBotFailure(ctx, 4_000, "read_failed"); err != nil {
		t.Fatalf("RecordCursorGrokBotFailure() error = %v", err)
	}
	if err := repository.RecordCursorDashboardFailure(ctx, 4_100, "auth_expired"); err != nil {
		t.Fatalf("RecordCursorDashboardFailure() error = %v", err)
	}
	if err := repository.ReplaceCursorSnapshot(ctx, CursorSnapshot{
		Generation: 3, CollectedAtMS: 4_200,
		Sources: []CursorSourceStatus{{
			Provider: "cursor", SourceKey: "cursor.state", SourceType: "sqlite_snapshot",
			State: "available", CoverageState: "exact", CheckpointKind: "snapshot",
			LastAttemptAtMS: 4_200, LastSuccessAtMS: pointerInt64ForCursorTest(4_200), UpdatedAtMS: 4_200,
		}},
	}); err != nil {
		t.Fatalf("ReplaceCursorSnapshot() error = %v", err)
	}

	readback, err := repository.CursorSnapshot(ctx)
	if err != nil {
		t.Fatalf("CursorSnapshot() error = %v", err)
	}
	if len(readback.DashboardUsageEvents) != 1 {
		t.Fatalf("monthly usage events = %#v", readback.DashboardUsageEvents)
	}
	var grokBot *CursorDashboardQuotaObservation
	var models *CursorDashboardQuotaObservation
	for index := range readback.DashboardQuotaObservations {
		observation := &readback.DashboardQuotaObservations[index]
		switch observation.LimitID {
		case "cursor.grok_bot":
			grokBot = observation
		case "cursor.models":
			models = observation
		}
	}
	if models == nil || models.UsedPercent != 7 || models.CycleStartAtMS != 1_000 || models.CycleEndAtMS != 30_000 {
		t.Fatalf("monthly observation after grok bot writes = %#v", models)
	}
	if grokBot == nil || grokBot.UsedPercent != 12.5 || grokBot.CycleStartAtMS != 2_500 || grokBot.CycleEndAtMS != 9_000 {
		t.Fatalf("grok bot weekly observation = %#v", grokBot)
	}
	sources := map[string]CursorSourceStatus{}
	for _, source := range readback.Sources {
		sources[source.SourceKey] = source
	}
	dashboard := sources["cursor.dashboard"]
	grokSource := sources["cursor.dashboard.grok_bot"]
	if dashboard.State != "unavailable" || dashboard.FailureCode == nil || *dashboard.FailureCode != "auth_expired" ||
		dashboard.LastSuccessAtMS == nil || *dashboard.LastSuccessAtMS != 2_000 {
		t.Fatalf("dashboard source after independent failure = %#v", dashboard)
	}
	if grokSource.State != "unavailable" || grokSource.FailureCode == nil || *grokSource.FailureCode != "read_failed" ||
		grokSource.LastSuccessAtMS == nil || *grokSource.LastSuccessAtMS != 3_000 {
		t.Fatalf("grok bot source after independent failure = %#v", grokSource)
	}
	if _, ok := sources["cursor.state"]; !ok {
		t.Fatal("local source missing after replacement")
	}
}

func TestCursorGrokBotNotApplicableOmitsPercentAndMonthlySnapshotRejectsGrokBotWindow(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.CommitCursorGrokBotObservation(ctx, CursorGrokBotCommit{
		Generation: 8, CollectedAtMS: 4_000, Included: false,
		CycleStartAtMS: 1_000, CycleEndAtMS: 8_000,
	}); err != nil {
		t.Fatalf("CommitCursorGrokBotObservation(not_applicable) error = %v", err)
	}
	if err := repository.ReplaceCursorSnapshot(ctx, CursorSnapshot{Generation: 1, CollectedAtMS: 4_200}); err != nil {
		t.Fatalf("ReplaceCursorSnapshot() error = %v", err)
	}
	zero := 0.0
	if err := repository.CommitCursorGrokBotObservation(ctx, CursorGrokBotCommit{
		Generation: 9, CollectedAtMS: 4_100, Included: false, UsedPercent: &zero,
		CycleStartAtMS: 1_000, CycleEndAtMS: 8_000,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("not_applicable with percent error = %v, want invalid record", err)
	}
	if err := repository.CommitCursorDashboardSnapshot(ctx, CursorDashboardSnapshot{
		Generation: 10, CollectedAtMS: 5_000, WindowStartMS: 1_000, WindowEndMS: 5_000, BillingCycleEndMS: 30_000,
		QuotaWindows: []CursorDashboardQuotaWindow{{
			LimitID: "cursor.grok_bot", UsedPercent: 1, CycleStartAtMS: 1_000, CycleEndAtMS: 8_000,
		}},
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("monthly snapshot with grok_bot window error = %v, want invalid record", err)
	}
	readback, err := repository.CursorSnapshot(ctx)
	if err != nil {
		t.Fatalf("CursorSnapshot() error = %v", err)
	}
	for _, observation := range readback.DashboardQuotaObservations {
		if observation.LimitID == "cursor.grok_bot" {
			t.Fatalf("not_applicable wrote grok_bot observation = %#v", observation)
		}
	}
	var grokSource *CursorSourceStatus
	for index := range readback.Sources {
		if readback.Sources[index].SourceKey == "cursor.dashboard.grok_bot" {
			grokSource = &readback.Sources[index]
		}
	}
	if grokSource == nil || grokSource.State != "available" || grokSource.LastSuccessAtMS == nil ||
		*grokSource.LastSuccessAtMS != 4_000 {
		t.Fatalf("not_applicable grok bot source = %#v", grokSource)
	}
}

func pointerInt64ForCursorTest(value int64) *int64 { return &value }

func TestCursorDashboardQuotaHistoryRetainsZeroAndMultipleSnapshots(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	for _, snapshot := range []CursorDashboardSnapshot{
		{
			Generation: 1, CollectedAtMS: 2_000, WindowStartMS: 1_000,
			WindowEndMS: 2_000, BillingCycleEndMS: 4_000,
			QuotaWindows: []CursorDashboardQuotaWindow{
				{LimitID: "cursor.models", UsedPercent: 7, CycleStartAtMS: 1_000, CycleEndAtMS: 4_000},
				{LimitID: "cursor.other_models", UsedPercent: 0, CycleStartAtMS: 1_000, CycleEndAtMS: 4_000},
			},
		},
		{
			Generation: 2, CollectedAtMS: 3_000, WindowStartMS: 1_000,
			WindowEndMS: 3_000, BillingCycleEndMS: 4_000,
			QuotaWindows: []CursorDashboardQuotaWindow{
				{LimitID: "cursor.models", UsedPercent: 9, CycleStartAtMS: 1_000, CycleEndAtMS: 4_000},
				{LimitID: "cursor.other_models", UsedPercent: 0, CycleStartAtMS: 1_000, CycleEndAtMS: 4_000},
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
