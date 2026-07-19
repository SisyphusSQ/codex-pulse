package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReplaceLightMetadataPublishesOneGenerationAndRemovesStaleSessions(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	home := LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	name := "真实标题"
	path := "/confirmed-home/sessions/one.jsonl"
	if err := repository.ReplaceLightMetadata(context.Background(), LightMetadataSnapshot{
		Home: home, Generation: 1, ReadyAtMS: 1_000,
		Sessions: []LightSessionMetadata{
			{SessionID: "one", ThreadName: &name, CWD: "/workspace/one", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 200},
			{SessionID: "stale", CWD: "/workspace/stale", CreatedAtMS: 300, UpdatedAtMS: 400},
		},
	}); err != nil {
		t.Fatalf("ReplaceLightMetadata(first) error = %v", err)
	}
	if err := repository.ReplaceLightMetadata(context.Background(), LightMetadataSnapshot{
		Home: home, Generation: 2, ReadyAtMS: 2_000,
		Sessions: []LightSessionMetadata{
			{SessionID: "one", ThreadName: &name, CWD: "/workspace/renamed", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 500},
		},
	}); err != nil {
		t.Fatalf("ReplaceLightMetadata(second) error = %v", err)
	}

	state, err := repository.LightIndexState(context.Background())
	if err != nil || state.MetadataGeneration != 2 || state.MetadataReadyAtMS == nil || *state.MetadataReadyAtMS != 2_000 {
		t.Fatalf("LightIndexState() = %#v, %v", state, err)
	}
	sessions, err := repository.ListLightSessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListLightSessions() = %#v, %v", sessions, err)
	}
	if sessions[0].SessionID != "one" || sessions[0].MetadataGeneration != 2 ||
		sessions[0].CWD != "/workspace/renamed" || sessions[0].ThreadName == nil || *sessions[0].ThreadName != name {
		t.Fatalf("session = %#v", sessions[0])
	}
}

func TestReplaceLightMetadataRejectsHomeGenerationConflictAtomically(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	first := LightMetadataSnapshot{
		Home:       LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		Generation: 1, ReadyAtMS: 1_000,
		Sessions: []LightSessionMetadata{{SessionID: "one", CWD: "/workspace", CreatedAtMS: 100, UpdatedAtMS: 200}},
	}
	if err := repository.ReplaceLightMetadata(context.Background(), first); err != nil {
		t.Fatalf("ReplaceLightMetadata(first) error = %v", err)
	}
	conflict := LightMetadataSnapshot{
		Home:       LightHomeIdentity{Path: "/other-home", DeviceID: "9", Inode: 10},
		Generation: 2, ReadyAtMS: 2_000,
		Sessions: []LightSessionMetadata{{SessionID: "other", CWD: "/other", CreatedAtMS: 300, UpdatedAtMS: 400}},
	}
	if err := repository.ReplaceLightMetadata(context.Background(), conflict); !errors.Is(err, ErrLightHomeFence) {
		t.Fatalf("ReplaceLightMetadata(conflict) error = %v", err)
	}
	sessions, err := repository.ListLightSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != "one" {
		t.Fatalf("conflict changed sessions: %#v, %v", sessions, err)
	}
}

func TestReplaceLightMetadataForConfirmedHomeSwitchDropsOldDerivedIndex(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	oldHome := LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	newHome := LightHomeIdentity{Path: "/next-home", DeviceID: "9", Inode: 10}
	if err := repository.ReplaceLightMetadataForHomeSwitch(context.Background(), oldHome, LightMetadataSnapshot{
		Home: newHome, Generation: 1, ReadyAtMS: 3_000,
		Sessions: []LightSessionMetadata{{SessionID: "next", CWD: "/workspace/next", CreatedAtMS: 300, UpdatedAtMS: 400}},
	}); err != nil {
		t.Fatalf("ReplaceLightMetadataForHomeSwitch() error = %v", err)
	}
	state, err := repository.LightIndexState(context.Background())
	if err != nil || state.Home != newHome || state.MetadataGeneration != 1 {
		t.Fatalf("LightIndexState() = %#v, %v", state, err)
	}
	sessions, err := repository.ListLightSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != "next" {
		t.Fatalf("ListLightSessions() = %#v, %v", sessions, err)
	}
	if _, err := repository.LightSessionTokenUsage(context.Background(), "one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old Home token facts survived switch: %v", err)
	}
	if err := repository.ReplaceLightMetadataForHomeSwitch(context.Background(), oldHome, LightMetadataSnapshot{
		Home:       LightHomeIdentity{Path: "/third-home", DeviceID: "11", Inode: 12},
		Generation: 1, ReadyAtMS: 4_000,
	}); !errors.Is(err, ErrLightHomeFence) {
		t.Fatalf("stale expected Home error = %v", err)
	}
}

func TestReplaceLightMetadataRejectsDuplicateSessionIDs(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	err := repository.ReplaceLightMetadata(context.Background(), LightMetadataSnapshot{
		Home:       LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		Generation: 1, ReadyAtMS: 1_000,
		Sessions: []LightSessionMetadata{
			{SessionID: "same", CWD: "/one", CreatedAtMS: 100, UpdatedAtMS: 200},
			{SessionID: "same", CWD: "/two", CreatedAtMS: 300, UpdatedAtMS: 400},
		},
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ReplaceLightMetadata(duplicate) error = %v", err)
	}
	if sessions, listErr := repository.ListLightSessions(context.Background()); listErr != nil || len(sessions) != 0 {
		t.Fatalf("invalid snapshot changed sessions: %#v, %v", sessions, listErr)
	}
}

func TestLightTokenRebuildPublishesOnlyAfterCompleteAndResumesCheckpoint(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil || generation != 1 {
		t.Fatalf("StartLightTokenRebuild() = %d, %v", generation, err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100,
		Checkpoint: LightTokenCheckpoint{
			DurableOffset: 4_000, Complete: false,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
			PhysicalBytesRead: 4_096, LinesSeen: 20, CandidateLines: 2, JSONDecoded: 2,
		},
		DailyDeltas: []LightTokenDailyDelta{{
			DayStartMS: 1_721_347_200_000, InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		}},
		TimedDeltas: []LightTokenTimedDelta{{
			SourceOffset: 3_900, ObservedAtMS: 1_721_347_199_000,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		}},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch(partial) error = %v", err)
	}
	pending, err := repository.PendingLightTokenScan(context.Background(), "one")
	if err != nil || pending.Generation != generation || pending.Checkpoint.DurableOffset != 4_000 || pending.Checkpoint.Complete {
		t.Fatalf("PendingLightTokenScan() = %#v, %v", pending, err)
	}
	if _, err := repository.LightSessionTokenUsage(context.Background(), "one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial generation became visible: %v", err)
	}

	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_200, Activate: true,
		Checkpoint: LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 130, CachedInputTokens: 25, OutputTokens: 15, ReasoningTokens: 4,
			PhysicalBytesRead: identity.SizeBytes, LinesSeen: 30, CandidateLines: 3, JSONDecoded: 3,
		},
		DailyDeltas: []LightTokenDailyDelta{{
			DayStartMS: 1_721_347_200_000, InputTokens: 30, CachedInputTokens: 5, OutputTokens: 5, ReasoningTokens: 2,
		}},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch(final) error = %v", err)
	}
	usage, err := repository.LightSessionTokenUsage(context.Background(), "one")
	if err != nil || usage.Generation != generation || usage.InputTokens != 130 || usage.CachedInputTokens != 25 ||
		usage.OutputTokens != 15 || usage.ReasoningTokens != 4 || !usage.Complete {
		t.Fatalf("LightSessionTokenUsage() = %#v, %v", usage, err)
	}
	daily, err := repository.LightSessionTokenDaily(context.Background(), "one")
	if err != nil || len(daily) != 1 || daily[0].InputTokens != 130 || daily[0].CachedInputTokens != 25 ||
		daily[0].OutputTokens != 15 || daily[0].ReasoningTokens != 4 {
		t.Fatalf("LightSessionTokenDaily() = %#v, %v", daily, err)
	}
	timed, err := repository.LightSessionTokenTimed(context.Background(), "one", 0, 2_000_000_000_000)
	if err != nil || len(timed) != 1 || timed[0].SourceOffset != 3_900 || timed[0].InputTokens != 100 {
		t.Fatalf("LightSessionTokenTimed() = %#v, %v", timed, err)
	}
}

func TestLightTokenRebuildKeepsOldActiveGenerationUntilReplacementActivates(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	first, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: first, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 100},
	}); err != nil {
		t.Fatal(err)
	}
	second, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v2", 3_000)
	if err != nil || second != 2 {
		t.Fatalf("StartLightTokenRebuild(second) = %d, %v", second, err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: second, UpdatedAtMS: 3_100,
		Checkpoint: LightTokenCheckpoint{DurableOffset: 4_000, InputTokens: 50},
	}); err != nil {
		t.Fatal(err)
	}
	if usage, err := repository.LightSessionTokenUsage(context.Background(), "one"); err != nil || usage.Generation != first || usage.InputTokens != 100 {
		t.Fatalf("old active usage not preserved: %#v, %v", usage, err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: second, UpdatedAtMS: 3_200, Activate: true,
		Checkpoint: LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 150},
	}); err != nil {
		t.Fatal(err)
	}
	if usage, err := repository.LightSessionTokenUsage(context.Background(), "one"); err != nil || usage.Generation != second || usage.InputTokens != 150 {
		t.Fatalf("replacement usage not activated: %#v, %v", usage, err)
	}
}

func TestLightTokenAppendKeepsActiveCountersAndAdvancesSameGeneration(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 100},
	}); err != nil {
		t.Fatal(err)
	}
	grown := identity
	grown.SizeBytes += 1_024
	grown.MTimeNS++
	grown.FingerprintSHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	appendGeneration, err := repository.StartLightTokenAppend(context.Background(), "one", grown, "parser-v1", 3_000)
	if err != nil || appendGeneration != generation {
		t.Fatalf("StartLightTokenAppend() = %d, %v", appendGeneration, err)
	}
	if usage, err := repository.LightSessionTokenUsage(context.Background(), "one"); err != nil || usage.InputTokens != 100 {
		t.Fatalf("active counters disappeared during append: %#v, %v", usage, err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 3_100, Activate: true,
		Checkpoint: LightTokenCheckpoint{DurableOffset: grown.SizeBytes, Complete: true, InputTokens: 130},
	}); err != nil {
		t.Fatal(err)
	}
	usage, err := repository.LightSessionTokenUsage(context.Background(), "one")
	if err != nil || usage.Generation != generation || usage.InputTokens != 130 {
		t.Fatalf("append usage = %#v, %v", usage, err)
	}
}

func TestSessionAnalyticsPrefersLightMetadataAndTokenTotals(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		},
	}); err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		Limit: 50, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || page.Mode != AnalyticsReadLightIndex || page.Generation != nil ||
		page.MatchedCount != 1 || len(page.Records) != 1 || page.MatchedTotals == nil || page.PageTotals == nil {
		t.Fatalf("ListSessionAnalytics(light) = %#v, %v", page, err)
	}
	record := page.Records[0]
	if record.SessionID != "one" || record.DisplayTitle != "真实标题" || record.Rollup == nil ||
		record.Rollup.TotalTokens == nil || *record.Rollup.TotalTokens != 112 ||
		record.LastActivityAtMS == nil || *record.LastActivityAtMS != 200 ||
		record.Project.ProjectID == nil || record.Project.DisplayName == nil ||
		*record.Project.DisplayName != "workspace" ||
		record.Project.Source != AttributionSourceCWDPathDigest || record.TitleSource != AttributionSourceAppServerName {
		t.Fatalf("record = %#v", record)
	}
	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "one", TurnLimit: 50,
	})
	if err != nil || detail.Mode != AnalyticsReadLightIndex || detail.Record.SessionID != "one" ||
		len(detail.Turns) != 0 || detail.PricingVersions == nil || detail.UnpricedReasons == nil {
		t.Fatalf("SessionAnalytics(light) = %#v, %v", detail, err)
	}
}

func TestUsageCostRangeBucketsLightTimedDeltasInRequestedTimezone(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	observedAtMS := time.Date(2024, 7, 18, 23, 30, 0, 0, time.UTC).UnixMilli()
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 100},
		TimedDeltas: []LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: observedAtMS, InputTokens: 100,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 7, 19, 0, 0, 0, 0, location).UTC().UnixMilli()
	end := time.Date(2024, 7, 20, 0, 0, 0, 0, location).UTC().UnixMilli()
	snapshot, err := repository.UsageCostRange(context.Background(), AnalyticsRange{
		ReportingTimezone: "Asia/Shanghai", StartAtMS: start, EndAtMS: end,
	})
	if err != nil || snapshot.Mode != AnalyticsReadLightIndex || snapshot.Generation != nil || len(snapshot.Daily) != 1 {
		t.Fatalf("UsageCostRange(light) = %#v, %v", snapshot, err)
	}
	row := snapshot.Daily[0]
	if row.BucketStartMS != start || row.ReportingTimezone != "Asia/Shanghai" ||
		row.InputTokens == nil || *row.InputTokens != 100 || row.TotalTokens == nil || *row.TotalTokens != 100 {
		t.Fatalf("daily row = %#v", row)
	}
}

func TestProjectAnalyticsGroupsLightTokenDeltasByCWDProject(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	observedAtMS := time.Date(2024, 7, 19, 1, 0, 0, 0, time.UTC).UnixMilli()
	if err := repository.CommitLightTokenBatch(context.Background(), LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2},
		TimedDeltas: []LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: observedAtMS,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC",
			StartAtMS:         time.Date(2024, 7, 19, 0, 0, 0, 0, time.UTC).UnixMilli(),
			EndAtMS:           time.Date(2024, 7, 20, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
		Limit: 20, SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || page.Mode != AnalyticsReadLightIndex || len(page.Records) != 1 || page.MatchedCount != 1 {
		t.Fatalf("ListProjectAnalytics(light) = %#v, %v", page, err)
	}
	record := page.Records[0]
	if record.ProjectID == nil || record.ProjectDisplayName == nil || *record.ProjectDisplayName != "workspace" ||
		record.Totals.TotalTokens == nil || *record.Totals.TotalTokens != 112 || record.SessionCount != 1 ||
		len(record.Trend) != 1 {
		t.Fatalf("record = %#v", record)
	}
	detail, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: ProjectAnalyticsFilter{
			Range: AnalyticsRange{
				ReportingTimezone: "UTC",
				StartAtMS:         time.Date(2024, 7, 19, 0, 0, 0, 0, time.UTC).UnixMilli(),
				EndAtMS:           time.Date(2024, 7, 20, 0, 0, 0, 0, time.UTC).UnixMilli(),
			},
		}.Range,
		DimensionKey: record.DimensionKey, SessionLimit: 20, ModelLimit: 20,
	})
	if err != nil || detail.Mode != AnalyticsReadLightIndex || detail.Record.DimensionKey != record.DimensionKey ||
		len(detail.Sessions) != 1 || detail.Sessions[0].SessionID != "one" || len(detail.Models) != 0 {
		t.Fatalf("ProjectAnalytics(light) = %#v, %v", detail, err)
	}
}

func lightIndexRepositoryFixture(t *testing.T) *Repository {
	t.Helper()
	repository := openRuntimeRepository(t)
	path := "/confirmed-home/sessions/one.jsonl"
	name := "真实标题"
	if err := repository.ReplaceLightMetadata(context.Background(), LightMetadataSnapshot{
		Home:       LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		Generation: 1, ReadyAtMS: 1_000,
		Sessions: []LightSessionMetadata{{
			SessionID: "one", ThreadName: &name, CWD: "/workspace", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 200,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	return repository
}

func lightRolloutFixture() LightRolloutIdentity {
	return LightRolloutIdentity{
		Path: "/confirmed-home/sessions/one.jsonl", SourceFileID: "codex:test",
		Home:     LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		DeviceID: "3", Inode: 4, SizeBytes: 8_192, MTimeNS: 100,
		PrefixBytes: 4_096, PrefixSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		FingerprintSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
}
