package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestReplaceLightMetadataPublishesOneGenerationAndRemovesStaleSessions(t *testing.T) {
	t.Parallel()

	repository := openLightRuntimeRepository(t)
	home := storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	name := "真实标题"
	path := "/confirmed-home/sessions/one.jsonl"
	if err := repository.ReplaceLightMetadata(context.Background(), storelight.LightMetadataSnapshot{
		Home: home, Generation: 1, ReadyAtMS: 1_000,
		Sessions: []storelight.LightSessionMetadata{
			{SessionID: "one", ThreadName: &name, CWD: "/workspace/one", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 200},
			{SessionID: "stale", CWD: "/workspace/stale", CreatedAtMS: 300, UpdatedAtMS: 400},
		},
	}); err != nil {
		t.Fatalf("ReplaceLightMetadata(first) error = %v", err)
	}
	if err := repository.ReplaceLightMetadata(context.Background(), storelight.LightMetadataSnapshot{
		Home: home, Generation: 2, ReadyAtMS: 2_000,
		Sessions: []storelight.LightSessionMetadata{
			{SessionID: "one", ThreadName: &name, CWD: "/workspace/renamed", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 500},
		},
	}); err != nil {
		t.Fatalf("ReplaceLightMetadata(second) error = %v", err)
	}

	state, err := repository.LightIndexState(context.Background())
	if err != nil || state.MetadataGeneration != 2 || state.MetadataReadyAtMS == nil || *state.MetadataReadyAtMS != 2_000 {
		t.Fatalf("storelight.LightIndexState() = %#v, %v", state, err)
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

	repository := openLightRuntimeRepository(t)
	first := storelight.LightMetadataSnapshot{
		Home:       storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		Generation: 1, ReadyAtMS: 1_000,
		Sessions: []storelight.LightSessionMetadata{{SessionID: "one", CWD: "/workspace", CreatedAtMS: 100, UpdatedAtMS: 200}},
	}
	if err := repository.ReplaceLightMetadata(context.Background(), first); err != nil {
		t.Fatalf("ReplaceLightMetadata(first) error = %v", err)
	}
	conflict := storelight.LightMetadataSnapshot{
		Home:       storelight.LightHomeIdentity{Path: "/other-home", DeviceID: "9", Inode: 10},
		Generation: 2, ReadyAtMS: 2_000,
		Sessions: []storelight.LightSessionMetadata{{SessionID: "other", CWD: "/other", CreatedAtMS: 300, UpdatedAtMS: 400}},
	}
	if err := repository.ReplaceLightMetadata(context.Background(), conflict); !errors.Is(err, storelight.ErrLightHomeFence) {
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
	oldHome := storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	newHome := storelight.LightHomeIdentity{Path: "/next-home", DeviceID: "9", Inode: 10}
	if err := repository.ReplaceLightMetadataForHomeSwitch(context.Background(), oldHome, storelight.LightMetadataSnapshot{
		Home: newHome, Generation: 1, ReadyAtMS: 3_000,
		Sessions: []storelight.LightSessionMetadata{{SessionID: "next", CWD: "/workspace/next", CreatedAtMS: 300, UpdatedAtMS: 400}},
	}); err != nil {
		t.Fatalf("ReplaceLightMetadataForHomeSwitch() error = %v", err)
	}
	state, err := repository.LightIndexState(context.Background())
	if err != nil || state.Home != newHome || state.MetadataGeneration != 1 {
		t.Fatalf("storelight.LightIndexState() = %#v, %v", state, err)
	}
	sessions, err := repository.ListLightSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != "next" {
		t.Fatalf("ListLightSessions() = %#v, %v", sessions, err)
	}
	if _, err := repository.LightSessionTokenUsage(context.Background(), "one"); !errors.Is(err, storelight.ErrNotFound) {
		t.Fatalf("old Home token facts survived switch: %v", err)
	}
	if err := repository.ReplaceLightMetadataForHomeSwitch(context.Background(), oldHome, storelight.LightMetadataSnapshot{
		Home:       storelight.LightHomeIdentity{Path: "/third-home", DeviceID: "11", Inode: 12},
		Generation: 1, ReadyAtMS: 4_000,
	}); !errors.Is(err, storelight.ErrLightHomeFence) {
		t.Fatalf("stale expected Home error = %v", err)
	}
}

func TestMigrateLightHomeIdentityUpdatesStateAndTokenScansAtomically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(ctx, "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatalf("StartLightTokenRebuild() error = %v", err)
	}
	if err := repository.CommitLightTokenBatch(ctx, storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
		},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch() error = %v", err)
	}
	legacy := identity.Home
	stable := legacy
	stable.DeviceID = "volume:00112233445566778899aabbccddeeff"
	migrated, err := repository.MigrateLightHomeIdentity(ctx, legacy, stable)
	if err != nil || !migrated {
		t.Fatalf("MigrateLightHomeIdentity() = %t, %v", migrated, err)
	}
	state, err := repository.LightIndexState(ctx)
	if err != nil || state.Home != stable {
		t.Fatalf("LightIndexState() = %#v, %v, want Home %#v", state, err, stable)
	}
	active, err := repository.ActiveLightTokenScan(ctx, "one")
	if err != nil || active.Identity.Home != stable {
		t.Fatalf("ActiveLightTokenScan() = %#v, %v, want Home %#v", active, err, stable)
	}
	migrated, err = repository.MigrateLightHomeIdentity(ctx, legacy, stable)
	if !errors.Is(err, storelight.ErrLightHomeFence) || migrated {
		t.Fatalf("stale migration = %t, %v, want ErrLightHomeFence", migrated, err)
	}
}

func TestReplaceLightMetadataRejectsDuplicateSessionIDs(t *testing.T) {
	t.Parallel()

	repository := openLightRuntimeRepository(t)
	err := repository.ReplaceLightMetadata(context.Background(), storelight.LightMetadataSnapshot{
		Home:       storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		Generation: 1, ReadyAtMS: 1_000,
		Sessions: []storelight.LightSessionMetadata{
			{SessionID: "same", CWD: "/one", CreatedAtMS: 100, UpdatedAtMS: 200},
			{SessionID: "same", CWD: "/two", CreatedAtMS: 300, UpdatedAtMS: 400},
		},
	})
	if !errors.Is(err, storelight.ErrInvalidRecord) {
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
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: 4_000, Complete: false,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
			PhysicalBytesRead: 4_096, LinesSeen: 20, CandidateLines: 2, JSONDecoded: 2,
		},
		DailyDeltas: []storelight.LightTokenDailyDelta{{
			DayStartMS: 1_721_347_200_000, InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		}},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
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
	if _, err := repository.LightSessionTokenUsage(context.Background(), "one"); !errors.Is(err, storelight.ErrNotFound) {
		t.Fatalf("partial generation became visible: %v", err)
	}

	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_200, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 130, CachedInputTokens: 25, OutputTokens: 15, ReasoningTokens: 4,
			PhysicalBytesRead: identity.SizeBytes, LinesSeen: 30, CandidateLines: 3, JSONDecoded: 3,
		},
		DailyDeltas: []storelight.LightTokenDailyDelta{{
			DayStartMS: 1_721_347_200_000, InputTokens: 30, CachedInputTokens: 5, OutputTokens: 5, ReasoningTokens: 2,
		}},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 5_000, ObservedAtMS: 1_721_347_200_000,
			InputTokens: 30, CachedInputTokens: 5, OutputTokens: 5, ReasoningTokens: 2,
		}},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch(final) error = %v", err)
	}
	usage, err := repository.LightSessionTokenUsage(context.Background(), "one")
	if err != nil || usage.Generation != generation || usage.InputTokens != 130 || usage.CachedInputTokens != 25 ||
		usage.OutputTokens != 15 || usage.ReasoningTokens != 4 || !usage.Complete {
		t.Fatalf("storelight.LightSessionTokenUsage() = %#v, %v", usage, err)
	}
	daily, err := repository.LightSessionTokenDaily(context.Background(), "one")
	if err != nil || len(daily) != 1 || daily[0].InputTokens != 130 || daily[0].CachedInputTokens != 25 ||
		daily[0].OutputTokens != 15 || daily[0].ReasoningTokens != 4 {
		t.Fatalf("storelight.LightSessionTokenDaily() = %#v, %v", daily, err)
	}
	timed, err := repository.LightSessionTokenTimed(context.Background(), "one", 0, 2_000_000_000_000)
	if err != nil || len(timed) != 2 ||
		timed[0].SourceOffset != 3_900 || timed[0].InputTokens != 100 ||
		timed[1].SourceOffset != 5_000 || timed[1].InputTokens != 30 {
		t.Fatalf("storelight.LightSessionTokenTimed() = %#v, %v", timed, err)
	}
}

func TestLightTokenBatchPersistsOnlySafeInvocationFactsForActiveGeneration(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatalf("StartLightTokenRebuild() error = %v", err)
	}
	duration := int64(42)
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true},
		InvocationDeltas: []storelight.LightInvocationDelta{
			{
				SourceOffset: 3_900, Ordinal: 0, ObservedAtMS: 1_721_347_199_000,
				Kind: "tool", Name: "linear.list_issues", Source: "mcp", Outcome: "succeeded", DurationMS: &duration,
			},
			{
				SourceOffset: 3_900, Ordinal: 1, ObservedAtMS: 1_721_347_199_000,
				Kind: "skill", Name: "go-code-style", Source: "skill_file_loaded", Outcome: "unknown",
			},
		},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch() error = %v", err)
	}

	events, err := repository.ListLightInvocations(
		context.Background(), 1_721_347_198_000, 1_721_347_200_000,
	)
	if err != nil || len(events) != 2 {
		t.Fatalf("ListLightInvocations() = %#v, %v", events, err)
	}
	if events[0].SessionID != "one" || events[0].Generation != generation ||
		events[0].Name != "linear.list_issues" || events[0].DurationMS == nil || *events[0].DurationMS != duration ||
		events[1].Name != "go-code-style" {
		t.Fatalf("events = %#v", events)
	}
}

func TestLightTokenBatchRejectsDuplicateInvocationIdentityAtomically(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	delta := storelight.LightInvocationDelta{
		SourceOffset: 3_900, Ordinal: 0, ObservedAtMS: 1_721_347_199_000,
		Kind: "tool", Name: "exec", Source: "response_custom", Outcome: "unknown",
	}
	err = repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100,
		Checkpoint:       storelight.LightTokenCheckpoint{DurableOffset: 4_000},
		InvocationDeltas: []storelight.LightInvocationDelta{delta, delta},
	})
	if !errors.Is(err, storelight.ErrLightTokenConflict) {
		t.Fatalf("CommitLightTokenBatch(duplicate invocation) error = %v", err)
	}
	pending, pendingErr := repository.PendingLightTokenScan(context.Background(), "one")
	if pendingErr != nil || pending.Checkpoint.DurableOffset != 0 {
		t.Fatalf("duplicate invocation advanced checkpoint: %#v, %v", pending, pendingErr)
	}
}

func TestLightTokenBatchRejectsUnsafeInvocationDuration(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	unsafeDuration := int64(9_007_199_254_740_992)
	err = repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100,
		Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true},
		InvocationDeltas: []storelight.LightInvocationDelta{{
			SourceOffset: 3_900, ObservedAtMS: 1_721_347_199_000,
			Kind: "tool", Name: "web_search", Source: "web_search", Outcome: "unknown", DurationMS: &unsafeDuration,
		}},
	})
	if !errors.Is(err, storelight.ErrInvalidRecord) {
		t.Fatalf("CommitLightTokenBatch(unsafe duration) error = %v", err)
	}
}

func TestCommitLightTokenBatchRejectsMissingTimedDeltaWithoutAdvancingCheckpoint(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	generation, err := repository.StartLightTokenRebuild(
		context.Background(),
		"one",
		lightRolloutFixture(),
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: 4_000,
			InputTokens:   10,
		},
	})
	if !errors.Is(err, storelight.ErrLightTokenConflict) {
		t.Fatalf("CommitLightTokenBatch(missing timed delta) error = %v", err)
	}
	pending, readErr := repository.PendingLightTokenScan(context.Background(), "one")
	if readErr != nil {
		t.Fatalf("PendingLightTokenScan() error = %v", readErr)
	}
	if pending.Checkpoint.DurableOffset != 0 || pending.Checkpoint.InputTokens != 0 {
		t.Fatalf("rejected batch advanced checkpoint: %#v", pending.Checkpoint)
	}
}

func TestCommitLightTokenBatchRejectsDuplicateSourceOffsetAtomically(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	generation, err := repository.StartLightTokenRebuild(
		context.Background(),
		"one",
		lightRolloutFixture(),
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: 4_000,
			InputTokens:   20,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{
			{SourceOffset: 3_900, ObservedAtMS: 100, InputTokens: 10},
			{SourceOffset: 3_900, ObservedAtMS: 200, InputTokens: 10},
		},
	})
	if !errors.Is(err, storelight.ErrLightTokenConflict) {
		t.Fatalf("CommitLightTokenBatch(duplicate source offset) error = %v", err)
	}
	pending, readErr := repository.PendingLightTokenScan(context.Background(), "one")
	if readErr != nil {
		t.Fatalf("PendingLightTokenScan() error = %v", readErr)
	}
	if pending.Checkpoint.DurableOffset != 0 || pending.Checkpoint.InputTokens != 0 {
		t.Fatalf("rejected duplicate batch advanced checkpoint: %#v", pending.Checkpoint)
	}
}

func TestCommitLightTokenBatchRejectsPreviouslyStoredSourceOffsetAtomically(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	generation, err := repository.StartLightTokenRebuild(
		context.Background(),
		"one",
		lightRolloutFixture(),
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: 4_000,
			InputTokens:   10,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 3_900, ObservedAtMS: 100, InputTokens: 10,
		}},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch(first) error = %v", err)
	}
	err = repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_200,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: 5_000,
			InputTokens:   20,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 3_900, ObservedAtMS: 200, InputTokens: 10,
		}},
	})
	if !errors.Is(err, storelight.ErrLightTokenConflict) {
		t.Fatalf("CommitLightTokenBatch(reused source offset) error = %v", err)
	}
	pending, readErr := repository.PendingLightTokenScan(context.Background(), "one")
	if readErr != nil {
		t.Fatalf("PendingLightTokenScan() error = %v", readErr)
	}
	if pending.Checkpoint.DurableOffset != 4_000 || pending.Checkpoint.InputTokens != 10 {
		t.Fatalf("rejected reused offset advanced checkpoint: %#v", pending.Checkpoint)
	}
}

func TestSessionAnalyticsFailsClosedWhenActiveTimedRowsDriftFromCheckpoint(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(
		context.Background(),
		"one",
		identity,
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes,
			Complete:      true,
			InputTokens:   10,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: 200, InputTokens: 10,
		}},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch() error = %v", err)
	}
	if err := repository.database.Write(context.Background(), func(
		ctx context.Context,
		transaction *gorm.DB,
	) error {
		return transaction.WithContext(ctx).
			Where("session_id = ? AND generation = ?", "one", generation).
			Delete(&lightTokenTimedTestModel{}).Error
	}); err != nil {
		t.Fatalf("delete active timed rows: %v", err)
	}

	_, err = repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "one",
		TurnLimit: 20,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("SessionAnalytics(drifted timed rows) error = %v", err)
	}
}

func TestSessionAnalyticsFailsClosedForEveryTimedTokenDimensionDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		column string
	}{
		{name: "input and derived total", column: "input_tokens"},
		{name: "cached input", column: "cached_input_tokens"},
		{name: "output and derived total", column: "output_tokens"},
		{name: "reasoning and derived total", column: "reasoning_tokens"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repository := lightIndexRepositoryFixture(t)
			identity := lightRolloutFixture()
			generation, err := repository.StartLightTokenRebuild(
				context.Background(),
				"one",
				identity,
				"parser-v1",
				2_000,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
				SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
				Checkpoint: storelight.LightTokenCheckpoint{
					DurableOffset: identity.SizeBytes, Complete: true,
					InputTokens: 100, CachedInputTokens: 20,
					OutputTokens: 30, ReasoningTokens: 10,
				},
				TimedDeltas: []storelight.LightTokenTimedDelta{{
					SourceOffset: 4_000, ObservedAtMS: 200,
					InputTokens: 100, CachedInputTokens: 20,
					OutputTokens: 30, ReasoningTokens: 10,
				}},
			}); err != nil {
				t.Fatalf("CommitLightTokenBatch() error = %v", err)
			}
			if err := repository.database.Write(context.Background(), func(
				ctx context.Context,
				transaction *gorm.DB,
			) error {
				return transaction.WithContext(ctx).
					Model(&lightTokenTimedTestModel{}).
					Where("session_id = ? AND generation = ?", "one", generation).
					UpdateColumn(test.column, gorm.Expr(test.column+" + 1")).
					Error
			}); err != nil {
				t.Fatalf("drift active timed %s: %v", test.column, err)
			}

			_, err = repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
				SessionID: "one",
				TurnLimit: 20,
			})
			if !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("SessionAnalytics(%s drift) error = %v", test.column, err)
			}
		})
	}
}

func TestSessionAnalyticsAllowsKnownZeroCheckpointWithEmptyTimedTrend(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(
		context.Background(),
		"one",
		identity,
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes,
			Complete:      true,
		},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch() error = %v", err)
	}

	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "one",
		TurnLimit: 20,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics(known zero) error = %v", err)
	}
	if detail.Record.Rollup == nil || detail.Record.Rollup.TotalTokens == nil ||
		*detail.Record.Rollup.TotalTokens != 0 || len(detail.Daily) != 0 {
		t.Fatalf("SessionAnalytics(known zero) = %#v", detail)
	}
}

func TestSessionAnalyticsPreservesRepeatedDSTHourFromLightTimedDeltas(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(
		context.Background(),
		"one",
		identity,
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstObserved := mustParseCostTime(t, "2026-11-01T05:30:00Z")
	secondObserved := mustParseCostTime(t, "2026-11-01T06:45:00Z")
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 10, OutputTokens: 20,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{
			{SourceOffset: 4_000, ObservedAtMS: firstObserved, InputTokens: 10},
			{SourceOffset: 5_000, ObservedAtMS: secondObserved, OutputTokens: 20},
		},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch() error = %v", err)
	}

	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID:         "one",
		ReportingTimezone: pointerTo("America/New_York"),
		TurnLimit:         20,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics(DST repeated hour) error = %v", err)
	}
	wantStarts := []int64{
		mustParseCostTime(t, "2026-11-01T05:00:00Z"),
		mustParseCostTime(t, "2026-11-01T06:00:00Z"),
	}
	gotStarts := make([]int64, 0, len(detail.Daily))
	for _, bucket := range detail.Daily {
		gotStarts = append(gotStarts, bucket.BucketStartMS)
	}
	if !reflect.DeepEqual(gotStarts, wantStarts) ||
		detail.Daily[0].InputTokens == nil || *detail.Daily[0].InputTokens != 10 ||
		detail.Daily[1].OutputTokens == nil || *detail.Daily[1].OutputTokens != 20 {
		t.Fatalf("SessionAnalytics(DST repeated hour) = %#v", detail)
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
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: first, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 100},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: 100, InputTokens: 100,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	second, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v2", 3_000)
	if err != nil || second != 2 {
		t.Fatalf("StartLightTokenRebuild(second) = %d, %v", second, err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: second, UpdatedAtMS: 3_100,
		Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: 4_000, InputTokens: 50},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 3_900, ObservedAtMS: 100, InputTokens: 50,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if usage, err := repository.LightSessionTokenUsage(context.Background(), "one"); err != nil || usage.Generation != first || usage.InputTokens != 100 {
		t.Fatalf("old active usage not preserved: %#v, %v", usage, err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: second, UpdatedAtMS: 3_200, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 150},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 5_000, ObservedAtMS: 200, InputTokens: 100,
		}},
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
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 100},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: 100, InputTokens: 100,
		}},
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
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 3_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: grown.SizeBytes, Complete: true, InputTokens: 130},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: grown.SizeBytes, ObservedAtMS: 200, InputTokens: 30,
		}},
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
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		},
		DailyDeltas: []storelight.LightTokenDailyDelta{{
			DayStartMS:  0,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		}},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: 200,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		}},
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
		record.Project.Source != attribution.SourceCWDPathDigest || record.TitleSource != attribution.SourceAppServerName {
		t.Fatalf("record = %#v", record)
	}
	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "one", TurnLimit: 50,
	})
	if err != nil || detail.Mode != AnalyticsReadLightIndex || detail.Record.SessionID != "one" ||
		detail.ReportingTimezone != "UTC" || len(detail.Daily) != 1 ||
		detail.Daily[0].BucketStartMS != 0 || detail.Daily[0].TotalTokens == nil ||
		*detail.Daily[0].TotalTokens != 112 ||
		len(detail.Turns) != 0 || detail.PricingVersions == nil || detail.UnpricedReasons == nil {
		t.Fatalf("SessionAnalytics(light) = %#v, %v", detail, err)
	}
}

// 测试 Session detail 按请求时区把同一自然日内的轻量 timed delta 聚合为小时趋势。
func TestSessionAnalyticsBucketsLightTimedUsageByRequestedLocalHour(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(
		context.Background(),
		"one",
		identity,
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	first := mustParseCostTime(t, "2026-07-26T16:30:00Z")
	second := mustParseCostTime(t, "2026-07-26T17:45:00Z")
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 10, OutputTokens: 20,
		},
		DailyDeltas: []storelight.LightTokenDailyDelta{{
			DayStartMS:  mustParseCostTime(t, "2026-07-26T00:00:00Z"),
			InputTokens: 10, OutputTokens: 20,
		}},
		TimedDeltas: []storelight.LightTokenTimedDelta{
			{SourceOffset: 4_000, ObservedAtMS: first, InputTokens: 10},
			{SourceOffset: 5_000, ObservedAtMS: second, OutputTokens: 20},
		},
	}); err != nil {
		t.Fatal(err)
	}

	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "one", ReportingTimezone: pointerTo("Asia/Shanghai"), TurnLimit: 50,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics(hourly light trend) error = %v", err)
	}
	wantBuckets := []int64{
		mustParseCostTime(t, "2026-07-26T16:00:00Z"),
		mustParseCostTime(t, "2026-07-26T17:00:00Z"),
	}
	gotBuckets := make([]int64, 0, len(detail.Daily))
	for _, bucket := range detail.Daily {
		gotBuckets = append(gotBuckets, bucket.BucketStartMS)
	}
	if detail.ReportingTimezone != "Asia/Shanghai" ||
		!reflect.DeepEqual(gotBuckets, wantBuckets) {
		t.Fatalf(
			"SessionAnalytics(hourly light trend) timezone=%q buckets=%#v, want %#v",
			detail.ReportingTimezone,
			gotBuckets,
			wantBuckets,
		)
	}
}

// 测试 Session detail 的轻量 timed delta 一旦跨越请求时区的自然日就切换为每日趋势。
func TestSessionAnalyticsBucketsLightTimedUsageByLocalDayAcrossDates(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(
		context.Background(),
		"one",
		identity,
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 10, OutputTokens: 20,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{
			{
				SourceOffset: 4_000,
				ObservedAtMS: mustParseCostTime(t, "2026-07-26T15:30:00Z"),
				InputTokens:  10,
			},
			{
				SourceOffset: 5_000,
				ObservedAtMS: mustParseCostTime(t, "2026-07-26T16:30:00Z"),
				OutputTokens: 20,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "one", ReportingTimezone: pointerTo("Asia/Shanghai"), TurnLimit: 50,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics(daily light trend) error = %v", err)
	}
	wantBuckets := []int64{
		mustParseCostTime(t, "2026-07-25T16:00:00Z"),
		mustParseCostTime(t, "2026-07-26T16:00:00Z"),
	}
	gotBuckets := make([]int64, 0, len(detail.Daily))
	for _, bucket := range detail.Daily {
		gotBuckets = append(gotBuckets, bucket.BucketStartMS)
	}
	if !reflect.DeepEqual(gotBuckets, wantBuckets) {
		t.Fatalf("SessionAnalytics(daily light trend) buckets=%#v, want %#v", gotBuckets, wantBuckets)
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
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: observedAtMS,
			InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, ReasoningTokens: 2,
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
		IncludeActivityDistribution: true,
	})
	if err != nil || snapshot.Mode != AnalyticsReadLightIndex || snapshot.Generation != nil || len(snapshot.Daily) != 1 {
		t.Fatalf("UsageCostRange(light) = %#v, %v", snapshot, err)
	}
	row := snapshot.Daily[0]
	if row.BucketStartMS != start || row.ReportingTimezone != "Asia/Shanghai" ||
		row.InputTokens == nil || *row.InputTokens != 100 || row.TotalTokens == nil || *row.TotalTokens != 112 {
		t.Fatalf("daily row = %#v", row)
	}
	if snapshot.ActivityDistribution == nil ||
		len(snapshot.ActivityDistribution.Timeline) != 1 ||
		len(snapshot.ActivityDistribution.WeekdayHours) != 1 ||
		snapshot.ActivityDistribution.Timeline[0].TotalTokens == nil ||
		*snapshot.ActivityDistribution.Timeline[0].TotalTokens != *row.TotalTokens ||
		snapshot.ActivityDistribution.WeekdayHours[0].TotalTokens == nil ||
		*snapshot.ActivityDistribution.WeekdayHours[0].TotalTokens != *row.TotalTokens ||
		snapshot.ActivityDistribution.Timeline[0].SessionCount != 1 ||
		snapshot.ActivityDistribution.WeekdayHours[0].Weekday != 5 ||
		snapshot.ActivityDistribution.WeekdayHours[0].Hour != 7 {
		t.Fatalf("light activity distribution = %#v", snapshot.ActivityDistribution)
	}
}

// 测试 UsageCostRange 在精确范围内排除同一自然日中窗口起点之前的增量。
func TestUsageCostRangeUsesExactPartialDayBoundaries(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v2", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC).UnixMilli()
	inside := time.Date(2026, 7, 22, 2, 0, 0, 0, time.UTC).UnixMilli()
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: inside + 1, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 300,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{
			{SourceOffset: 4_000, ObservedAtMS: before, InputTokens: 100},
			{SourceOffset: 5_000, ObservedAtMS: inside, InputTokens: 200},
		},
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 22, 1, 30, 0, 0, time.UTC).UnixMilli()
	end := time.Date(2026, 7, 22, 2, 30, 0, 0, time.UTC).UnixMilli()
	snapshot, err := repository.UsageCostRange(context.Background(), AnalyticsRange{
		ReportingTimezone: "UTC", StartAtMS: start, EndAtMS: end, Exact: true,
	})
	if err != nil || snapshot.Mode != AnalyticsReadLightIndex || len(snapshot.Daily) != 1 {
		t.Fatalf("UsageCostRange(exact) = %#v, %v", snapshot, err)
	}
	row := snapshot.Daily[0]
	if row.BucketStartMS != start || row.InputTokens == nil || *row.InputTokens != 200 ||
		row.TotalTokens == nil || *row.TotalTokens != 200 {
		t.Fatalf("exact daily row = %#v", row)
	}
	page, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		ReportingTimezone:       pointerTo("UTC"),
		LastActivityAtOrAfterMS: &start, LastActivityBeforeMS: &end, RangeExact: true,
		Limit: 10, SortField: SessionAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil || len(page.Records) != 1 || page.Records[0].Rollup == nil ||
		page.Records[0].Rollup.TotalTokens == nil || *page.Records[0].Rollup.TotalTokens != 200 {
		t.Fatalf("ListSessionAnalytics(exact light) = %#v, %v", page, err)
	}
}

func TestUsageCostRangePricesAndGroupsLightTimedDeltasByModel(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	for _, catalog := range pricing.BuiltinOpenAICatalog() {
		if err := repository.AddPricingVersion(context.Background(), catalog); err != nil {
			t.Fatalf("AddPricingVersion(%s) error = %v", catalog.PricingVersion, err)
		}
	}
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v2", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	model := "gpt-5.4-mini"
	observedAtMS := time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC).UnixMilli()
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: observedAtMS + 1, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 1_000_000, CachedInputTokens: 200_000,
			OutputTokens: 100_000, ReasoningTokens: 50_000,
			CurrentModelKey: &model, CurrentModelSource: attribution.SourceModelCanonical,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: observedAtMS,
			ModelKey: &model, ModelSource: attribution.SourceModelCanonical,
			InputTokens: 1_000_000, CachedInputTokens: 200_000,
			OutputTokens: 100_000, ReasoningTokens: 50_000,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	active, err := repository.ActiveLightTokenScan(context.Background(), "one")
	if err != nil || active.Checkpoint.CurrentModelKey == nil ||
		*active.Checkpoint.CurrentModelKey != model || active.Checkpoint.CurrentModelSource != attribution.SourceModelCanonical {
		t.Fatalf("active model checkpoint = %#v, %v", active.Checkpoint, err)
	}
	snapshot, err := repository.UsageCostRange(context.Background(), AnalyticsRange{
		ReportingTimezone: "UTC",
		StartAtMS:         time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC).UnixMilli(),
		EndAtMS:           time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC).UnixMilli(),
	})
	if err != nil {
		t.Fatalf("UsageCostRange(light priced) error = %v", err)
	}
	if snapshot.PricingSource != "openai-api" || snapshot.Currency != "USD" ||
		len(snapshot.PricingVersions) != 1 || snapshot.PricingVersions[0] != "openai-api-2026-07-22" ||
		len(snapshot.Daily) != 1 || snapshot.Daily[0].EstimatedUSDMicros == nil ||
		*snapshot.Daily[0].EstimatedUSDMicros != 1_290_000 || len(snapshot.Models) != 1 {
		t.Fatalf("priced light snapshot = %#v", snapshot)
	}
	modelRow := snapshot.Models[0]
	if modelRow.DimensionKey != model || modelRow.ModelKey == nil || *modelRow.ModelKey != model ||
		modelRow.ModelDisplayName == nil || *modelRow.ModelDisplayName != "GPT-5.4 Mini" ||
		modelRow.TotalTokens == nil || *modelRow.TotalTokens != 1_150_000 ||
		modelRow.EstimatedUSDMicros == nil || *modelRow.EstimatedUSDMicros != 1_290_000 {
		t.Fatalf("light model row = %#v", modelRow)
	}
	rangeStart := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC).UnixMilli()
	rangeEnd := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC).UnixMilli()
	sessionPage, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), LastActivityAtOrAfterMS: &rangeStart,
		LastActivityBeforeMS: &rangeEnd, RangeExact: true, Limit: 10,
		SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || sessionPage.PricingSource != "openai-api" || sessionPage.Currency != "USD" ||
		len(sessionPage.Records) != 1 || sessionPage.Records[0].Rollup == nil ||
		sessionPage.Records[0].Rollup.EstimatedUSDMicros == nil ||
		*sessionPage.Records[0].Rollup.EstimatedUSDMicros != 1_290_000 {
		t.Fatalf("priced light session page = %#v, %v", sessionPage, err)
	}
	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "one", TurnLimit: 50,
	})
	if err != nil || detail.PricingSource != "openai-api" || detail.Currency != "USD" ||
		len(detail.PricingVersions) != 1 || detail.PricingVersions[0] != "openai-api-2026-07-22" ||
		detail.Record.Rollup == nil || detail.Record.Rollup.EstimatedUSDMicros == nil ||
		*detail.Record.Rollup.EstimatedUSDMicros != 1_290_000 {
		t.Fatalf("priced light session detail = %#v, %v", detail, err)
	}
}

func TestProjectAnalyticsGroupsLightTokenDeltasByCWDProject(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	for _, catalog := range pricing.BuiltinOpenAICatalog() {
		if err := repository.AddPricingVersion(context.Background(), catalog); err != nil {
			t.Fatal(err)
		}
	}
	identity := lightRolloutFixture()
	model := "gpt-5.4-mini"
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v1", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	observedAtMS := time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC).UnixMilli()
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 1_000_000, CachedInputTokens: 200_000,
			OutputTokens: 100_000, ReasoningTokens: 50_000,
			CurrentModelKey: &model, CurrentModelSource: attribution.SourceModelCanonical,
		},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: observedAtMS,
			ModelKey: &model, ModelSource: attribution.SourceModelCanonical,
			InputTokens: 1_000_000, CachedInputTokens: 200_000,
			OutputTokens: 100_000, ReasoningTokens: 50_000,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC",
			StartAtMS:         time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC).UnixMilli(),
			EndAtMS:           time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
		Limit: 20, SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || page.Mode != AnalyticsReadLightIndex || len(page.Records) != 1 || page.MatchedCount != 1 {
		t.Fatalf("ListProjectAnalytics(light) = %#v, %v", page, err)
	}
	record := page.Records[0]
	if record.ProjectID == nil || record.ProjectDisplayName == nil || *record.ProjectDisplayName != "workspace" ||
		record.Totals.TotalTokens == nil || *record.Totals.TotalTokens != 1_150_000 ||
		record.Totals.EstimatedUSDMicros == nil || *record.Totals.EstimatedUSDMicros != 1_290_000 ||
		record.SessionCount != 1 || len(record.Trend) != 1 ||
		len(page.PricingVersions) != 1 || page.PricingVersions[0] != "openai-api-2026-07-22" {
		t.Fatalf("record/page = %#v / %#v", record, page)
	}
	detail, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: ProjectAnalyticsFilter{
			Range: AnalyticsRange{
				ReportingTimezone: "UTC",
				StartAtMS:         time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC).UnixMilli(),
				EndAtMS:           time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC).UnixMilli(),
			},
		}.Range,
		DimensionKey: record.DimensionKey, SessionLimit: 20, ModelLimit: 20,
	})
	if err != nil || detail.Mode != AnalyticsReadLightIndex || detail.Record.DimensionKey != record.DimensionKey ||
		len(detail.Sessions) != 1 || detail.Sessions[0].SessionID != "one" || len(detail.Models) != 1 ||
		detail.Models[0].DimensionKey != model || detail.Models[0].Model.ModelKey == nil ||
		*detail.Models[0].Model.ModelKey != model || detail.Models[0].Totals.TotalTokens == nil ||
		*detail.Models[0].Totals.TotalTokens != 1_150_000 ||
		detail.Models[0].Totals.EstimatedUSDMicros == nil ||
		*detail.Models[0].Totals.EstimatedUSDMicros != 1_290_000 {
		t.Fatalf("ProjectAnalytics(light) = %#v, %v", detail, err)
	}
}

func TestProjectAnalyticsUsesExactPartialDayBoundaries(t *testing.T) {
	t.Parallel()

	repository := lightIndexRepositoryFixture(t)
	identity := lightRolloutFixture()
	generation, err := repository.StartLightTokenRebuild(context.Background(), "one", identity, "parser-v2", 2_000)
	if err != nil {
		t.Fatal(err)
	}
	observedAtMS := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC).UnixMilli()
	if err := repository.CommitLightTokenBatch(context.Background(), storelight.LightTokenBatch{
		SessionID: "one", Generation: generation, UpdatedAtMS: observedAtMS + 1, Activate: true,
		Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 100},
		TimedDeltas: []storelight.LightTokenTimedDelta{{
			SourceOffset: 4_000, ObservedAtMS: observedAtMS, InputTokens: 100,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 23, 0, 30, 0, 0, time.UTC).UnixMilli()
	end := time.Date(2026, 7, 23, 2, 0, 0, 0, time.UTC).UnixMilli()
	page, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC", StartAtMS: start, EndAtMS: end, Exact: true,
		},
		Limit: 5, SortField: ProjectAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil || len(page.Records) != 1 || len(page.Records[0].Trend) != 1 {
		t.Fatalf("ListProjectAnalytics(exact light) = %#v, %v", page, err)
	}
	if page.Records[0].Trend[0].BucketStartMS != start {
		t.Fatalf(
			"exact project bucket = %d, want range start %d",
			page.Records[0].Trend[0].BucketStartMS, start,
		)
	}
}

func TestLightAnalyticsGroupsCodexWorktreesAndKeepsScratchUsageAsOther(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openLightRuntimeRepository(t)
	home := storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	root := t.TempDir()
	worktreeOne := filepath.Join(root, ".codex", "worktrees", "aaaa", "codex-pulse")
	worktreeTwo := filepath.Join(root, ".codex", "worktrees", "bbbb", "codex-pulse", "internal")
	scratch := filepath.Join(root, "Codex", "2026-07-23", "quota-overview")
	sessions := []storelight.LightSessionMetadata{
		{SessionID: "worktree-one", CWD: worktreeOne, CreatedAtMS: 100, UpdatedAtMS: 200},
		{SessionID: "worktree-two", CWD: worktreeTwo, CreatedAtMS: 100, UpdatedAtMS: 200},
		{SessionID: "scratch", CWD: scratch, CreatedAtMS: 100, UpdatedAtMS: 200},
	}
	for index := range sessions {
		path := filepath.Join(home.Path, "sessions", sessions[index].SessionID+".jsonl")
		sessions[index].RolloutPath = &path
	}
	if err := repository.ReplaceLightMetadata(ctx, storelight.LightMetadataSnapshot{
		Home: home, Generation: 1, ReadyAtMS: 1_000, Sessions: sessions,
	}); err != nil {
		t.Fatal(err)
	}
	observedAtMS := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC).UnixMilli()
	for index, session := range sessions {
		identity := storelight.LightRolloutIdentity{
			Path: *session.RolloutPath, SourceFileID: "codex:" + session.SessionID,
			Home: home, DeviceID: "3", Inode: int64(index + 10), SizeBytes: 8_192,
			MTimeNS: 100, PrefixBytes: 4_096,
			PrefixSHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			FingerprintSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}
		generation, err := repository.StartLightTokenRebuild(ctx, session.SessionID, identity, "parser-v2", 2_000)
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.CommitLightTokenBatch(ctx, storelight.LightTokenBatch{
			SessionID: session.SessionID, Generation: generation, UpdatedAtMS: 2_100, Activate: true,
			Checkpoint: storelight.LightTokenCheckpoint{DurableOffset: identity.SizeBytes, Complete: true, InputTokens: 100},
			TimedDeltas: []storelight.LightTokenTimedDelta{{
				SourceOffset: 4_000, ObservedAtMS: observedAtMS + int64(index), InputTokens: 100,
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := repository.ListProjectAnalytics(ctx, ProjectAnalyticsFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC",
			StartAtMS:         time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC).UnixMilli(),
			EndAtMS:           time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
		Limit: 20, SortField: ProjectAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil || len(page.Records) != 2 {
		t.Fatalf("ListProjectAnalytics(grouped) = %#v, %v", page, err)
	}
	grouped := page.Records[0]
	if grouped.ProjectDisplayName == nil || *grouped.ProjectDisplayName != "codex-pulse" ||
		grouped.SessionCount != 2 || grouped.Totals.TotalTokens == nil || *grouped.Totals.TotalTokens != 200 {
		t.Fatalf("grouped worktree project = %#v", grouped)
	}
	groupedDetail, err := repository.ProjectAnalytics(ctx, ProjectAnalyticsDetailFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC",
			StartAtMS:         time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC).UnixMilli(),
			EndAtMS:           time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
		DimensionKey: grouped.DimensionKey, SessionLimit: 1, ModelLimit: 20,
	})
	if err != nil || len(groupedDetail.Sessions) != 1 || groupedDetail.NextSessionCursor == nil ||
		groupedDetail.NextSessionCursor.GenerationID == "" {
		t.Fatalf("ProjectAnalytics(grouped first page) = %#v, %v", groupedDetail, err)
	}
	groupedSecondPage, err := repository.ProjectAnalytics(ctx, ProjectAnalyticsDetailFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC",
			StartAtMS:         time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC).UnixMilli(),
			EndAtMS:           time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
		DimensionKey: grouped.DimensionKey, SessionLimit: 1,
		SessionCursor: groupedDetail.NextSessionCursor, ModelLimit: 20,
	})
	if err != nil || len(groupedSecondPage.Sessions) != 1 || groupedSecondPage.NextSessionCursor != nil ||
		groupedSecondPage.Sessions[0].SessionID == groupedDetail.Sessions[0].SessionID {
		t.Fatalf("ProjectAnalytics(grouped second page) = %#v, %v", groupedSecondPage, err)
	}
	other := page.Records[1]
	if other.ProjectID != nil || other.ProjectDisplayName != nil || other.SessionCount != 1 ||
		other.Totals.TotalTokens == nil || *other.Totals.TotalTokens != 100 {
		t.Fatalf("scratch other project = %#v", other)
	}
	otherDetail, err := repository.ProjectAnalytics(ctx, ProjectAnalyticsDetailFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC",
			StartAtMS:         time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC).UnixMilli(),
			EndAtMS:           time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
		DimensionKey: other.DimensionKey, SessionLimit: 20, ModelLimit: 20,
	})
	if err != nil || otherDetail.Record.DimensionKey != other.DimensionKey ||
		otherDetail.Record.ProjectID != nil || otherDetail.Record.Totals.TotalTokens == nil ||
		*otherDetail.Record.Totals.TotalTokens != 100 || len(otherDetail.Sessions) != 1 {
		t.Fatalf("ProjectAnalytics(other) = %#v, %v", otherDetail, err)
	}

	sessionPage, err := repository.ListSessionAnalytics(ctx, SessionAnalyticsFilter{
		Limit: 10, SortField: SessionAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil || len(sessionPage.Records) != 3 {
		t.Fatalf("ListSessionAnalytics(grouped) = %#v, %v", sessionPage, err)
	}
	projectIDs := make(map[string]*string)
	for _, record := range sessionPage.Records {
		projectIDs[record.SessionID] = record.Project.ProjectID
	}
	if projectIDs["worktree-one"] == nil || projectIDs["worktree-two"] == nil ||
		*projectIDs["worktree-one"] != *projectIDs["worktree-two"] || projectIDs["scratch"] != nil {
		t.Fatalf("session project identities = %#v", projectIDs)
	}
}

func lightIndexRepositoryFixture(t *testing.T) *lightTestRepository {
	t.Helper()
	repository := openLightRuntimeRepository(t)
	path := "/confirmed-home/sessions/one.jsonl"
	name := "真实标题"
	if err := repository.ReplaceLightMetadata(context.Background(), storelight.LightMetadataSnapshot{
		Home:       storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		Generation: 1, ReadyAtMS: 1_000,
		Sessions: []storelight.LightSessionMetadata{{
			SessionID: "one", ThreadName: &name, CWD: "/workspace", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 200,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	return repository
}

func lightRolloutFixture() storelight.LightRolloutIdentity {
	return storelight.LightRolloutIdentity{
		Path: "/confirmed-home/sessions/one.jsonl", SourceFileID: "codex:test",
		Home:     storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		DeviceID: "3", Inode: 4, SizeBytes: 8_192, MTimeNS: 100,
		PrefixBytes: 4_096, PrefixSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		FingerprintSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
}
