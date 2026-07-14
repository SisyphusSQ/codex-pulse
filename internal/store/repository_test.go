package store

import (
	"context"
	"errors"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// 测试 UpsertFacts 在完整事实批次重复回放场景下保持行级幂等。
func TestRepositoryUpsertFactsIsIdempotent(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	projectID := "project-1"
	model := "gpt-5"
	outcome := "done"
	completedAt := int64(1_700_000_001_000)
	completeOffset := int64(240)
	inputTokens := int64(12)
	cachedTokens := int64(0)
	batch := FactBatch{
		Project: &Project{
			ProjectID:   projectID,
			DisplayName: "Codex Pulse",
			RootPath:    "/workspace/codex-pulse",
			CreatedAtMS: 1_700_000_000_000,
			UpdatedAtMS: 1_700_000_000_000,
		},
		Session: &Session{
			SessionID:     "session-1",
			Provider:      "codex",
			SourceKind:    "session_jsonl",
			ProjectID:     &projectID,
			CreatedAtMS:   1_700_000_000_000,
			FirstSeenAtMS: 1_700_000_000_100,
			LastSeenAtMS:  1_700_000_001_100,
		},
		Turn: &Turn{
			TurnID:           "turn-1",
			SessionID:        "session-1",
			StartedAtMS:      1_700_000_000_500,
			CompletedAtMS:    &completedAt,
			Outcome:          &outcome,
			Model:            &model,
			ProjectID:        &projectID,
			SourceGeneration: 0,
			StartOffset:      120,
			CompleteOffset:   &completeOffset,
		},
		Usage: &TurnUsage{
			TurnID:            "turn-1",
			ObservedAtMS:      completedAt,
			IsFinal:           true,
			InputTokens:       &inputTokens,
			CachedInputTokens: &cachedTokens,
			SourceOffset:      completeOffset,
			Confidence:        "exact",
			UpdatedAtMS:       completedAt,
		},
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.UpsertFacts(context.Background(), batch); err != nil {
			t.Fatalf("UpsertFacts() attempt %d error = %v", attempt+1, err)
		}
	}

	session, err := repository.Session(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if session.SessionID != "session-1" || session.ProjectID == nil || *session.ProjectID != projectID {
		t.Fatalf("Session() = %#v", session)
	}

	turn, err := repository.Turn(context.Background(), "turn-1")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if turn.TurnID != "turn-1" || turn.Usage == nil || !turn.Usage.IsFinal {
		t.Fatalf("Turn() = %#v", turn)
	}

	wantCounts := map[string]int{
		"projects":   1,
		"sessions":   1,
		"turns":      1,
		"turn_usage": 1,
	}
	gotCounts, err := tableCounts(context.Background(), database, []string{"projects", "sessions", "turns", "turn_usage"})
	if err != nil {
		t.Fatalf("count facts: %v", err)
	}
	for table, want := range wantCounts {
		if got := gotCounts[table]; got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
}

// 测试 UpsertFacts 在事务尾部引用无效场景下回滚此前已写入的全部事实。
func TestRepositoryUpsertFactsRollsBackEntireBatch(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	projectID := "project-rollback"
	missingTurnID := "turn-missing"
	err := repository.UpsertFacts(context.Background(), FactBatch{
		Project: &Project{
			ProjectID:   projectID,
			DisplayName: "Rollback Project",
			RootPath:    "/workspace/rollback",
			CreatedAtMS: 1_700_000_000_000,
			UpdatedAtMS: 1_700_000_000_000,
		},
		Session: &Session{
			SessionID:     "session-rollback",
			Provider:      "codex",
			SourceKind:    "session_jsonl",
			ProjectID:     &projectID,
			CreatedAtMS:   1_700_000_000_000,
			FirstSeenAtMS: 1_700_000_000_000,
			LastSeenAtMS:  1_700_000_000_000,
		},
		SessionCurrent: &SessionCurrent{
			SessionID:    "session-rollback",
			ActiveTurnID: &missingTurnID,
			UpdatedAtMS:  1_700_000_000_000,
		},
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts() error = %v, want ErrInvalidRecord", err)
	}

	gotCounts, countErr := tableCounts(context.Background(), database, []string{"projects", "sessions", "session_current"})
	if countErr != nil {
		t.Fatalf("count rows after rollback: %v", countErr)
	}
	for table, count := range gotCounts {
		if count != 0 {
			t.Errorf("%s rows after rollback = %d, want 0", table, count)
		}
	}
}

// 测试 UpsertFacts 在乱序重放场景下不降级 final usage 和较新的 current projection。
func TestRepositoryUpsertFactsPreservesNullZeroAndMonotonicProjections(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	zero := int64(0)
	activeTurnID := "turn-1"
	model := "gpt-5"
	lastActivity := int64(1_700_000_000_900)
	initial := FactBatch{
		Session: &Session{
			SessionID:     "session-1",
			Provider:      "codex",
			SourceKind:    "session_jsonl",
			CreatedAtMS:   1_700_000_000_000,
			FirstSeenAtMS: 1_700_000_000_000,
			LastSeenAtMS:  1_700_000_000_900,
		},
		Turn: &Turn{
			TurnID:           activeTurnID,
			SessionID:        "session-1",
			StartedAtMS:      1_700_000_000_500,
			Model:            &model,
			SourceGeneration: 0,
			StartOffset:      100,
		},
		Usage: &TurnUsage{
			TurnID:            activeTurnID,
			ObservedAtMS:      1_700_000_000_800,
			CachedInputTokens: &zero,
			SourceOffset:      180,
			Confidence:        "exact",
			UpdatedAtMS:       1_700_000_000_800,
		},
		SessionCurrent: &SessionCurrent{
			SessionID:        "session-1",
			ActiveTurnID:     &activeTurnID,
			CurrentModel:     &model,
			LastActivityAtMS: &lastActivity,
			UpdatedAtMS:      1_700_000_000_900,
		},
		SessionUsageCurrent: &SessionUsageCurrent{
			SessionID:         "session-1",
			CounterEpoch:      0,
			TotalInputTokens:  nil,
			TotalCachedTokens: &zero,
			ObservedAtMS:      1_700_000_000_800,
			SourceOffset:      180,
			CounterState:      "unknown",
		},
	}
	if err := repository.UpsertFacts(context.Background(), initial); err != nil {
		t.Fatalf("UpsertFacts(initial) error = %v", err)
	}

	outcome := "done"
	completedAt := int64(1_700_000_001_000)
	completeOffset := int64(240)
	final := FactBatch{
		Turn: &Turn{
			TurnID:           activeTurnID,
			SessionID:        "session-1",
			StartedAtMS:      1_700_000_000_500,
			CompletedAtMS:    &completedAt,
			Outcome:          &outcome,
			Model:            &model,
			SourceGeneration: 0,
			StartOffset:      100,
			CompleteOffset:   &completeOffset,
		},
		Usage: &TurnUsage{
			TurnID:            activeTurnID,
			ObservedAtMS:      completedAt,
			IsFinal:           true,
			CachedInputTokens: &zero,
			SourceOffset:      completeOffset,
			Confidence:        "exact",
			UpdatedAtMS:       completedAt,
		},
		SessionCurrent: &SessionCurrent{
			SessionID:        "session-1",
			CurrentModel:     &model,
			LastActivityAtMS: &completedAt,
			UpdatedAtMS:      completedAt,
		},
		SessionUsageCurrent: &SessionUsageCurrent{
			SessionID:         "session-1",
			CounterEpoch:      1,
			TotalInputTokens:  nil,
			TotalCachedTokens: &zero,
			ObservedAtMS:      completedAt,
			SourceOffset:      completeOffset,
			CounterState:      "reset",
		},
	}
	if err := repository.UpsertFacts(context.Background(), final); err != nil {
		t.Fatalf("UpsertFacts(final) error = %v", err)
	}
	err := repository.UpsertFacts(context.Background(), initial)
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(stale provisional replay) error = %v, want ErrInvalidRecord", err)
	}

	turn, err := repository.Turn(context.Background(), activeTurnID)
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if turn.CompletedAtMS == nil || *turn.CompletedAtMS != completedAt || turn.Usage == nil || !turn.Usage.IsFinal {
		t.Fatalf("Turn() after stale replay = %#v", turn)
	}
	if turn.Usage.InputTokens != nil {
		t.Fatalf("InputTokens = %v, want nil unknown", *turn.Usage.InputTokens)
	}
	if turn.Usage.CachedInputTokens == nil || *turn.Usage.CachedInputTokens != 0 {
		t.Fatalf("CachedInputTokens = %v, want pointer to real zero", turn.Usage.CachedInputTokens)
	}

	session, err := repository.Session(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if session.Current == nil || session.Current.ActiveTurnID != nil || session.Current.UpdatedAtMS != completedAt {
		t.Fatalf("Session.Current after stale replay = %#v", session.Current)
	}
	if session.Usage == nil || session.Usage.CounterEpoch != 1 || session.Usage.CounterState != "reset" {
		t.Fatalf("Session.Usage after stale replay = %#v", session.Usage)
	}
	if session.Usage.TotalInputTokens != nil {
		t.Fatalf("TotalInputTokens = %v, want nil unknown", *session.Usage.TotalInputTokens)
	}
	if session.Usage.TotalCachedTokens == nil || *session.Usage.TotalCachedTokens != 0 {
		t.Fatalf("TotalCachedTokens = %v, want pointer to real zero", session.Usage.TotalCachedTokens)
	}
}

// 测试 ListTurns 按来源位置、项目、模型和时间范围返回稳定倒序结果。
func TestRepositoryListTurnsFiltersSourceProjectModelAndTime(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	upsertListTurn(t, repository, "project-a", "session-a", "turn-a1", "gpt-5", 1_000, 100)
	upsertListTurn(t, repository, "project-a", "session-a", "turn-a2", "gpt-4", 2_000, 250)
	upsertListTurn(t, repository, "project-b", "session-b", "turn-b1", "gpt-5", 1_500, 120)
	zero := int64(0)
	if err := repository.UpsertFacts(context.Background(), FactBatch{Usage: &TurnUsage{
		TurnID:            "turn-a2",
		ObservedAtMS:      2_100,
		CachedInputTokens: &zero,
		SourceOffset:      280,
		Confidence:        "exact",
		UpdatedAtMS:       2_100,
	}}); err != nil {
		t.Fatalf("UpsertFacts(turn-a2 usage) error = %v", err)
	}

	sessionID := "session-a"
	generation := int64(0)
	minimumOffset := int64(200)
	bySource, err := repository.ListTurns(context.Background(), TurnFilter{
		SessionID:            &sessionID,
		SourceGeneration:     &generation,
		StartOffsetAtOrAfter: &minimumOffset,
		Limit:                10,
	})
	if err != nil {
		t.Fatalf("ListTurns(by source) error = %v", err)
	}
	assertTurnIDs(t, bySource, "turn-a2")
	if bySource[0].Usage == nil || bySource[0].Usage.CachedInputTokens == nil || *bySource[0].Usage.CachedInputTokens != 0 {
		t.Fatalf("ListTurns(by source) usage = %#v, want cached real zero", bySource[0].Usage)
	}

	projectID := "project-a"
	startedBefore := int64(2_500)
	byProject, err := repository.ListTurns(context.Background(), TurnFilter{
		ProjectID:       &projectID,
		StartedBeforeMS: &startedBefore,
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("ListTurns(by project) error = %v", err)
	}
	assertTurnIDs(t, byProject, "turn-a2", "turn-a1")

	model := "gpt-5"
	startedAtOrAfter := int64(1_200)
	byModel, err := repository.ListTurns(context.Background(), TurnFilter{
		Model:              &model,
		StartedAtOrAfterMS: &startedAtOrAfter,
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ListTurns(by model) error = %v", err)
	}
	assertTurnIDs(t, byModel, "turn-b1")

	_, err = repository.ListTurns(context.Background(), TurnFilter{Limit: 501})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListTurns(limit 501) error = %v, want ErrInvalidRecord", err)
	}
}

// 测试 UpsertFacts 在空批次、负数、跨对象引用和空 optional string 场景下稳定拒绝。
func TestRepositoryRejectsInvalidRecordMatrix(t *testing.T) {
	t.Parallel()

	empty := ""
	negative := int64(-1)
	cases := []struct {
		name  string
		batch FactBatch
	}{
		{name: "empty batch", batch: FactBatch{}},
		{
			name: "negative token",
			batch: FactBatch{Usage: &TurnUsage{
				TurnID:       "turn-invalid",
				ObservedAtMS: 1,
				InputTokens:  &negative,
				SourceOffset: 1,
				Confidence:   "exact",
				UpdatedAtMS:  1,
			}},
		},
		{
			name: "mismatched session and turn",
			batch: FactBatch{
				Session: &Session{
					SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
				},
				Turn: &Turn{
					TurnID: "turn-a", SessionID: "session-b",
				},
			},
		},
		{
			name: "empty optional model",
			batch: FactBatch{
				Session: &Session{
					SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
				},
				Turn: &Turn{
					TurnID: "turn-a", SessionID: "session-a", Model: &empty,
				},
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDatabase(t)
			repository := NewRepository(database)
			if err := repository.EnsureCoreSchema(context.Background()); err != nil {
				t.Fatalf("EnsureCoreSchema() error = %v", err)
			}
			err := repository.UpsertFacts(context.Background(), testCase.batch)
			if !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("UpsertFacts() error = %v, want ErrInvalidRecord", err)
			}
		})
	}
}

// 测试 UpsertFacts 拒绝复用 stable ID 或来源位置表达不同事实。
func TestRepositoryRejectsStableIdentityConflicts(t *testing.T) {
	t.Parallel()

	t.Run("session provider conflict", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		if err := repository.EnsureCoreSchema(context.Background()); err != nil {
			t.Fatalf("EnsureCoreSchema() error = %v", err)
		}
		upsertListTurn(t, repository, "project-a", "session-a", "turn-a", "gpt-5", 1_000, 100)

		err := repository.UpsertFacts(context.Background(), FactBatch{Session: &Session{
			SessionID:     "session-a",
			Provider:      "other-provider",
			SourceKind:    "session_jsonl",
			CreatedAtMS:   900,
			FirstSeenAtMS: 900,
			LastSeenAtMS:  1_100,
		}})
		if !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("UpsertFacts(provider conflict) error = %v, want ErrInvalidRecord", err)
		}
	})

	t.Run("source position conflict", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		if err := repository.EnsureCoreSchema(context.Background()); err != nil {
			t.Fatalf("EnsureCoreSchema() error = %v", err)
		}
		upsertListTurn(t, repository, "project-a", "session-a", "turn-a", "gpt-5", 1_000, 100)

		err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
			TurnID:           "turn-b",
			SessionID:        "session-a",
			StartedAtMS:      1_100,
			SourceGeneration: 0,
			StartOffset:      100,
		}})
		if !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("UpsertFacts(source conflict) error = %v, want ErrInvalidRecord", err)
		}
	})
}

// 测试 Turn 来源 generation 只能前进，旧 generation 重放不能回退来源位置。
func TestRepositoryAdvancesTurnSourceGenerationMonotonically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	upsertListTurn(t, repository, "project-a", "session-a", "turn-a", "gpt-5", 1_000, 100)

	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID:           "turn-a",
		SessionID:        "session-a",
		StartedAtMS:      1_000,
		SourceGeneration: 1,
		StartOffset:      20,
	}}); err != nil {
		t.Fatalf("UpsertFacts(new generation) error = %v", err)
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID:           "turn-a",
		SessionID:        "session-a",
		StartedAtMS:      1_000,
		SourceGeneration: 0,
		StartOffset:      100,
	}}); err != nil {
		t.Fatalf("UpsertFacts(stale generation) error = %v", err)
	}

	turn, err := repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if turn.SourceGeneration != 1 || turn.StartOffset != 20 {
		t.Fatalf("turn source position = (%d, %d), want (1, 20)", turn.SourceGeneration, turn.StartOffset)
	}

	err = repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID:           "turn-a",
		SessionID:        "session-a",
		StartedAtMS:      1_000,
		SourceGeneration: 1,
		StartOffset:      21,
	}})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(conflicting same generation) error = %v, want ErrInvalidRecord", err)
	}
}

// 测试 completed turn 只有在 higher generation 提供完整 completion tuple 后才切换来源位置。
func TestRepositoryCompletedTurnAdvancesOnlyWithFullHigherGeneration(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	completedAt := int64(1_100)
	completeOffset := int64(200)
	outcome := "done"
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_100,
		},
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: 0,
			StartOffset: 100, CompleteOffset: &completeOffset,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(initial completed turn) error = %v", err)
	}

	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
		SourceGeneration: 1, StartOffset: 300,
	}}); err != nil {
		t.Fatalf("UpsertFacts(higher generation provisional replay) error = %v", err)
	}

	turn, err := repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn(after provisional replay) error = %v", err)
	}
	if turn.SourceGeneration != 0 || turn.StartOffset != 100 ||
		turn.CompleteOffset == nil || *turn.CompleteOffset != 200 {
		t.Fatalf("turn after provisional replay = %#v, want original complete source tuple", turn)
	}

	newCompletedAt := int64(1_200)
	newCompleteOffset := int64(350)
	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
		CompletedAtMS: &newCompletedAt, Outcome: &outcome, SourceGeneration: 1,
		StartOffset: 300, CompleteOffset: &newCompleteOffset,
	}}); err != nil {
		t.Fatalf("UpsertFacts(higher generation complete replay) error = %v", err)
	}
	turn, err = repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn(after complete replay) error = %v", err)
	}
	if turn.SourceGeneration != 1 || turn.StartOffset != 300 ||
		turn.CompleteOffset == nil || *turn.CompleteOffset != 350 {
		t.Fatalf("turn after complete replay = %#v, want generation 1 complete tuple", turn)
	}
}

// 测试 usage 来源位置按 (generation, offset) 排序，generation 切换允许 offset 重置。
func TestRepositoryUsageSourceGenerationSupersedesLowerOffsets(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	outcome := "done"
	completedAt := int64(1_100)
	completeOffset := int64(900)
	oneHundred := int64(100)
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_100,
		},
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: 0,
			StartOffset: 500, CompleteOffset: &completeOffset,
		},
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_100, IsFinal: true,
			InputTokens: &oneHundred, SourceGeneration: 0, SourceOffset: 1_000,
			Confidence: "exact", UpdatedAtMS: 1_100,
		},
		SessionUsageCurrent: &SessionUsageCurrent{
			SessionID: "session-a", CounterEpoch: 3, TotalInputTokens: &oneHundred,
			ObservedAtMS: 1_100, SourceGeneration: 0, SourceOffset: 1_000,
			CounterState: "live",
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(generation 0) error = %v", err)
	}

	newCompletedAt := int64(1_200)
	newCompleteOffset := int64(200)
	oneHundredTwenty := int64(120)
	ten := int64(10)
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &newCompletedAt, Outcome: &outcome, SourceGeneration: 1,
			StartOffset: 100, CompleteOffset: &newCompleteOffset,
		},
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_200, IsFinal: true,
			InputTokens: &oneHundredTwenty, SourceGeneration: 1, SourceOffset: 250,
			Confidence: "exact", UpdatedAtMS: 1_200,
		},
		SessionUsageCurrent: &SessionUsageCurrent{
			SessionID: "session-a", CounterEpoch: 0, TotalInputTokens: &ten,
			ObservedAtMS: 1_200, SourceGeneration: 1, SourceOffset: 250,
			CounterState: "rebuilt",
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(generation 1) error = %v", err)
	}

	one := int64(1)
	err := repository.UpsertFacts(context.Background(), FactBatch{
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_300, IsFinal: true,
			InputTokens: &one, SourceGeneration: 0, SourceOffset: 2_000,
			Confidence: "exact", UpdatedAtMS: 1_300,
		},
		SessionUsageCurrent: &SessionUsageCurrent{
			SessionID: "session-a", CounterEpoch: 99, TotalInputTokens: &one,
			ObservedAtMS: 1_300, SourceGeneration: 0, SourceOffset: 2_000,
			CounterState: "stale",
		},
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(stale generation) error = %v, want ErrInvalidRecord", err)
	}

	turn, err := repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if turn.Usage == nil || turn.Usage.SourceGeneration != 1 || turn.Usage.SourceOffset != 250 ||
		turn.Usage.InputTokens == nil || *turn.Usage.InputTokens != 120 {
		t.Fatalf("Turn.Usage = %#v, want generation 1 usage", turn.Usage)
	}
	session, err := repository.Session(context.Background(), "session-a")
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if session.Usage == nil || session.Usage.SourceGeneration != 1 ||
		session.Usage.CounterEpoch != 0 || session.Usage.SourceOffset != 250 ||
		session.Usage.TotalInputTokens == nil || *session.Usage.TotalInputTokens != 10 {
		t.Fatalf("Session.Usage = %#v, want generation 1 counter", session.Usage)
	}
}

// 测试 usage 不能领先事务内实际采用的 turn generation，失败批次必须原子回滚。
func TestRepositoryRejectsUsageAheadOfStoredTurnGeneration(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	completedAt := int64(1_100)
	completeOffset := int64(200)
	outcome := "done"
	oneHundred := int64(100)
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_100,
		},
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: 0,
			StartOffset: 100, CompleteOffset: &completeOffset,
		},
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_100, IsFinal: true,
			InputTokens: &oneHundred, SourceGeneration: 0, SourceOffset: 220,
			Confidence: "exact", UpdatedAtMS: 1_100,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(initial facts) error = %v", err)
	}

	oneHundredTwenty := int64(120)
	err := repository.UpsertFacts(context.Background(), FactBatch{
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			SourceGeneration: 1, StartOffset: 300,
		},
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_200, IsFinal: true,
			InputTokens: &oneHundredTwenty, SourceGeneration: 1, SourceOffset: 350,
			Confidence: "exact", UpdatedAtMS: 1_200,
		},
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(partial parent with advanced usage) error = %v, want ErrInvalidRecord", err)
	}
	assertTurnAndUsageGeneration(t, repository, 0, 100)

	err = repository.UpsertFacts(context.Background(), FactBatch{Usage: &TurnUsage{
		TurnID: "turn-a", ObservedAtMS: 1_300, IsFinal: true,
		InputTokens: &oneHundredTwenty, SourceGeneration: 2, SourceOffset: 50,
		Confidence: "exact", UpdatedAtMS: 1_300,
	}})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(usage-only generation ahead) error = %v, want ErrInvalidRecord", err)
	}
	assertTurnAndUsageGeneration(t, repository, 0, 100)
}

// 测试 batch 中所有事实必须归属同一 Session，失败时先写对象也必须回滚。
func TestRepositoryRejectsCrossSessionBatchAtomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	for _, sessionID := range []string{"session-a", "session-b"} {
		if err := repository.UpsertFacts(context.Background(), FactBatch{Session: &Session{
			SessionID: sessionID, Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 200,
		}}); err != nil {
			t.Fatalf("UpsertFacts(%s) error = %v", sessionID, err)
		}
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID: "turn-b", SessionID: "session-b", StartedAtMS: 150,
		SourceGeneration: 0, StartOffset: 10,
	}}); err != nil {
		t.Fatalf("UpsertFacts(turn-b) error = %v", err)
	}

	oneHundred := int64(100)
	err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 300,
		},
		Usage: &TurnUsage{
			TurnID: "turn-b", ObservedAtMS: 200, InputTokens: &oneHundred,
			SourceGeneration: 0, SourceOffset: 20, Confidence: "exact", UpdatedAtMS: 200,
		},
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(cross-session batch) error = %v, want ErrInvalidRecord", err)
	}
	session, err := repository.Session(context.Background(), "session-a")
	if err != nil {
		t.Fatalf("Session(session-a) error = %v", err)
	}
	if session.LastSeenAtMS != 200 {
		t.Fatalf("session-a LastSeenAtMS = %d, want rollback value 200", session.LastSeenAtMS)
	}
	turn, err := repository.Turn(context.Background(), "turn-b")
	if err != nil {
		t.Fatalf("Turn(turn-b) error = %v", err)
	}
	if turn.Usage != nil {
		t.Fatalf("turn-b Usage = %#v, want nil after rollback", turn.Usage)
	}

	err = repository.UpsertFacts(context.Background(), FactBatch{
		SessionCurrent:      &SessionCurrent{SessionID: "session-a", UpdatedAtMS: 200},
		SessionUsageCurrent: &SessionUsageCurrent{SessionID: "session-b", CounterState: "live"},
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(conflicting direct session IDs) error = %v, want ErrInvalidRecord", err)
	}
}

// 测试 turn 换代会移除旧 current usage，且之后只接受与实际 turn 相同的 generation。
func TestRepositoryTurnGenerationReplacesCurrentUsage(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	completedAt := int64(1_100)
	completeOffset := int64(200)
	outcome := "done"
	oneHundred := int64(100)
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_100,
		},
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: 0,
			StartOffset: 100, CompleteOffset: &completeOffset,
		},
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_100, IsFinal: true,
			InputTokens: &oneHundred, SourceGeneration: 0, SourceOffset: 220,
			Confidence: "exact", UpdatedAtMS: 1_100,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(generation 0) error = %v", err)
	}

	newCompletedAt := int64(1_200)
	newCompleteOffset := int64(350)
	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
		CompletedAtMS: &newCompletedAt, Outcome: &outcome, SourceGeneration: 1,
		StartOffset: 300, CompleteOffset: &newCompleteOffset,
	}}); err != nil {
		t.Fatalf("UpsertFacts(turn generation 1) error = %v", err)
	}
	turn, err := repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn(after generation change) error = %v", err)
	}
	if turn.SourceGeneration != 1 || turn.Usage != nil {
		t.Fatalf("turn after generation change = %#v, want generation 1 without stale usage", turn)
	}

	err = repository.UpsertFacts(context.Background(), FactBatch{Usage: &TurnUsage{
		TurnID: "turn-a", ObservedAtMS: 1_300, IsFinal: true,
		InputTokens: &oneHundred, SourceGeneration: 0, SourceOffset: 500,
		Confidence: "exact", UpdatedAtMS: 1_300,
	}})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(stale usage) error = %v, want ErrInvalidRecord", err)
	}
}

// 测试 completed Turn 跨 generation 始终只接受 final usage，拒绝时保留旧完整事实。
func TestRepositoryCompletedTurnRequiresFinalUsageAcrossGenerations(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	completedAt := int64(1_100)
	completeOffset := int64(200)
	outcome := "done"
	oneHundred := int64(100)
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_100,
		},
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: 0,
			StartOffset: 100, CompleteOffset: &completeOffset,
		},
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_100, IsFinal: true,
			InputTokens: &oneHundred, SourceGeneration: 0, SourceOffset: 220,
			Confidence: "exact", UpdatedAtMS: 1_100,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(generation 0 final) error = %v", err)
	}

	newCompletedAt := int64(1_200)
	newCompleteOffset := int64(350)
	oneHundredTwenty := int64(120)
	err := repository.UpsertFacts(context.Background(), FactBatch{
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &newCompletedAt, Outcome: &outcome, SourceGeneration: 1,
			StartOffset: 300, CompleteOffset: &newCompleteOffset,
		},
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_200, IsFinal: false,
			InputTokens: &oneHundredTwenty, SourceGeneration: 1, SourceOffset: 370,
			Confidence: "exact", UpdatedAtMS: 1_200,
		},
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(generation change with provisional usage) error = %v, want ErrInvalidRecord", err)
	}
	turn, err := repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn(after rejected generation change) error = %v", err)
	}
	if turn.SourceGeneration != 0 || turn.Usage == nil || !turn.Usage.IsFinal ||
		turn.Usage.SourceGeneration != 0 || turn.Usage.InputTokens == nil || *turn.Usage.InputTokens != 100 {
		t.Fatalf("turn after rejected generation change = %#v, want original generation 0 final usage", turn)
	}

	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
		CompletedAtMS: &newCompletedAt, Outcome: &outcome, SourceGeneration: 1,
		StartOffset: 300, CompleteOffset: &newCompleteOffset,
	}}); err != nil {
		t.Fatalf("UpsertFacts(turn-only generation change) error = %v", err)
	}
	err = repository.UpsertFacts(context.Background(), FactBatch{Usage: &TurnUsage{
		TurnID: "turn-a", ObservedAtMS: 1_300, IsFinal: false,
		InputTokens: &oneHundredTwenty, SourceGeneration: 1, SourceOffset: 390,
		Confidence: "exact", UpdatedAtMS: 1_300,
	}})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(provisional usage after completed generation change) error = %v, want ErrInvalidRecord", err)
	}
	turn, err = repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn(after rejected provisional usage) error = %v", err)
	}
	if turn.SourceGeneration != 1 || turn.Usage != nil {
		t.Fatalf("turn after rejected provisional usage = %#v, want generation 1 with unknown usage", turn)
	}
}

// 测试同 generation 的 Turn-only completion 会移除既有 provisional current usage。
func TestRepositoryTurnOnlyCompletionRemovesProvisionalUsage(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	oneHundred := int64(100)
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_000,
		},
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			SourceGeneration: 0, StartOffset: 100,
		},
		Usage: &TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_050, IsFinal: false,
			InputTokens: &oneHundred, SourceGeneration: 0, SourceOffset: 150,
			Confidence: "exact", UpdatedAtMS: 1_050,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(provisional turn and usage) error = %v", err)
	}

	completedAt := int64(1_100)
	completeOffset := int64(200)
	outcome := "done"
	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
		CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: 0,
		StartOffset: 100, CompleteOffset: &completeOffset,
	}}); err != nil {
		t.Fatalf("UpsertFacts(turn-only completion) error = %v", err)
	}

	turn, err := repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if turn.CompletedAtMS == nil || *turn.CompletedAtMS != completedAt || turn.Usage != nil {
		t.Fatalf("turn after completion = %#v, want completed with unknown usage", turn)
	}
	counts, err := tableCounts(context.Background(), database, []string{"turn_usage"})
	if err != nil {
		t.Fatalf("count turn_usage: %v", err)
	}
	if counts["turn_usage"] != 0 {
		t.Fatalf("turn_usage rows = %d, want provisional row removed", counts["turn_usage"])
	}
}

// 测试 Turn completion 必须在保留旧 usage ordering 证据的前提下合并 final usage。
func TestRepositoryCompletionUsageOrderingIsAtomic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		newOffset  int64
		wantError  bool
		wantOffset int64
	}{
		{name: "greater advances", newOffset: 400, wantOffset: 400},
		{name: "equal conflict rolls back", newOffset: 300, wantError: true, wantOffset: 300},
		{name: "lower rolls back", newOffset: 200, wantError: true, wantOffset: 300},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDatabase(t)
			repository := NewRepository(database)
			if err := repository.EnsureCoreSchema(context.Background()); err != nil {
				t.Fatalf("EnsureCoreSchema() error = %v", err)
			}

			oneHundred := int64(100)
			if err := repository.UpsertFacts(context.Background(), FactBatch{
				Session: &Session{
					SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
					CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_000,
				},
				Turn: &Turn{
					TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
					SourceGeneration: 0, StartOffset: 100,
				},
				Usage: &TurnUsage{
					TurnID: "turn-a", ObservedAtMS: 1_050, IsFinal: false,
					InputTokens: &oneHundred, SourceGeneration: 0, SourceOffset: 300,
					Confidence: "exact", UpdatedAtMS: 1_050,
				},
			}); err != nil {
				t.Fatalf("UpsertFacts(provisional facts) error = %v", err)
			}

			completedAt := int64(1_100)
			completeOffset := int64(450)
			outcome := "done"
			oneHundredTwenty := int64(120)
			err := repository.UpsertFacts(context.Background(), FactBatch{
				Turn: &Turn{
					TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
					CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: 0,
					StartOffset: 100, CompleteOffset: &completeOffset,
				},
				Usage: &TurnUsage{
					TurnID: "turn-a", ObservedAtMS: 1_100, IsFinal: true,
					InputTokens: &oneHundredTwenty, SourceGeneration: 0,
					SourceOffset: testCase.newOffset, Confidence: "exact", UpdatedAtMS: 1_100,
				},
			})
			if testCase.wantError && !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("UpsertFacts(completion) error = %v, want ErrInvalidRecord", err)
			}
			if !testCase.wantError && err != nil {
				t.Fatalf("UpsertFacts(completion) error = %v", err)
			}

			turn, err := repository.Turn(context.Background(), "turn-a")
			if err != nil {
				t.Fatalf("Turn() error = %v", err)
			}
			if testCase.wantError {
				if turn.CompletedAtMS != nil || turn.Usage == nil || turn.Usage.IsFinal ||
					turn.Usage.SourceOffset != testCase.wantOffset {
					t.Fatalf("turn after rejected completion = %#v, want provisional offset %d", turn, testCase.wantOffset)
				}
				return
			}
			if turn.CompletedAtMS == nil || turn.Usage == nil || !turn.Usage.IsFinal ||
				turn.Usage.SourceOffset != testCase.wantOffset || turn.Usage.InputTokens == nil ||
				*turn.Usage.InputTokens != 120 {
				t.Fatalf("turn after completion = %#v, want final offset %d", turn, testCase.wantOffset)
			}
		})
	}
}

// 测试 completed turn 的同来源 provisional 重放既不能冲突改写，也不能补写已冻结字段。
func TestRepositoryCompletedTurnRejectsOrIgnoresProvisionalFieldReplay(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	completedAt := int64(1_100)
	completeOffset := int64(200)
	outcome := "done"
	model := "gpt-5"
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_100,
		},
		Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &completedAt, Outcome: &outcome, Model: &model,
			SourceGeneration: 0, StartOffset: 100, CompleteOffset: &completeOffset,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(initial completed turn) error = %v", err)
	}

	conflictingModel := "gpt-4"
	err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
		Model: &conflictingModel, SourceGeneration: 0, StartOffset: 100,
	}})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(conflicting provisional replay) error = %v, want ErrInvalidRecord", err)
	}

	reasoning := "high"
	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
		TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
		Model: &model, ReasoningEffort: &reasoning, SourceGeneration: 0, StartOffset: 100,
	}}); err != nil {
		t.Fatalf("UpsertFacts(non-conflicting provisional replay) error = %v", err)
	}
	turn, err := repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if turn.Model == nil || *turn.Model != model || turn.ReasoningEffort != nil {
		t.Fatalf("turn after provisional replay = %#v, want completed fields unchanged", turn)
	}
}

func assertTurnAndUsageGeneration(t *testing.T, repository *Repository, generation, inputTokens int64) {
	t.Helper()
	turn, err := repository.Turn(context.Background(), "turn-a")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if turn.SourceGeneration != generation || turn.Usage == nil ||
		turn.Usage.SourceGeneration != generation || turn.Usage.InputTokens == nil ||
		*turn.Usage.InputTokens != inputTokens {
		t.Fatalf("turn after rejected replay = %#v, want turn/usage generation %d and input %d", turn, generation, inputTokens)
	}
}

// 测试 thread name 使用自己的更新时间合并，普通活动投影不能回退或清空它。
func TestRepositorySessionCurrentPreservesThreadNameByFieldTimestamp(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	latestName := "最新标题"
	latestNameAt := int64(200)
	oldModel := "gpt-5"
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 200,
		},
		SessionCurrent: &SessionCurrent{
			SessionID: "session-a", ThreadName: &latestName,
			ThreadNameUpdatedAtMS: &latestNameAt, CurrentModel: &oldModel, UpdatedAtMS: 200,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(initial current) error = %v", err)
	}

	staleName := "旧标题"
	staleNameAt := int64(100)
	newModel := "gpt-6"
	if err := repository.UpsertFacts(context.Background(), FactBatch{SessionCurrent: &SessionCurrent{
		SessionID: "session-a", ThreadName: &staleName,
		ThreadNameUpdatedAtMS: &staleNameAt, CurrentModel: &newModel, UpdatedAtMS: 300,
	}}); err != nil {
		t.Fatalf("UpsertFacts(new activity with stale thread name) error = %v", err)
	}
	assertSessionCurrent(t, repository, "session-a", latestName, latestNameAt, newModel, 300)

	newestModel := "gpt-7"
	if err := repository.UpsertFacts(context.Background(), FactBatch{SessionCurrent: &SessionCurrent{
		SessionID: "session-a", CurrentModel: &newestModel, UpdatedAtMS: 400,
	}}); err != nil {
		t.Fatalf("UpsertFacts(new activity without thread name) error = %v", err)
	}
	assertSessionCurrent(t, repository, "session-a", latestName, latestNameAt, newestModel, 400)

	conflictingName := "冲突标题"
	err := repository.UpsertFacts(context.Background(), FactBatch{SessionCurrent: &SessionCurrent{
		SessionID: "session-a", ThreadName: &conflictingName,
		ThreadNameUpdatedAtMS: &latestNameAt, CurrentModel: &newestModel, UpdatedAtMS: 500,
	}})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(conflicting thread timestamp) error = %v, want ErrInvalidRecord", err)
	}
	assertSessionCurrent(t, repository, "session-a", latestName, latestNameAt, newestModel, 400)
}

// 测试相同 stable identity 或排序键只允许完全一致重放，冲突 payload 必须拒绝。
func TestRepositoryRejectsConflictingPayloadAtSameOrderingKey(t *testing.T) {
	t.Parallel()

	t.Run("turn completion", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		if err := repository.EnsureCoreSchema(context.Background()); err != nil {
			t.Fatalf("EnsureCoreSchema() error = %v", err)
		}
		completedAt := int64(1_100)
		completeOffset := int64(200)
		outcome := "done"
		turn := Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &completedAt, Outcome: &outcome,
			SourceGeneration: 0, StartOffset: 100, CompleteOffset: &completeOffset,
		}
		if err := repository.UpsertFacts(context.Background(), FactBatch{
			Session: &Session{
				SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
				CreatedAtMS: 900, FirstSeenAtMS: 900, LastSeenAtMS: 1_100,
			},
			Turn: &turn,
		}); err != nil {
			t.Fatalf("UpsertFacts(initial turn) error = %v", err)
		}
		if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &turn}); err != nil {
			t.Fatalf("UpsertFacts(exact turn replay) error = %v", err)
		}

		conflictingCompletedAt := int64(1_200)
		conflictingOffset := int64(220)
		conflictingOutcome := "failed"
		err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &Turn{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1_000,
			CompletedAtMS: &conflictingCompletedAt, Outcome: &conflictingOutcome,
			SourceGeneration: 0, StartOffset: 100, CompleteOffset: &conflictingOffset,
		}})
		if !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("UpsertFacts(conflicting completion) error = %v, want ErrInvalidRecord", err)
		}
	})

	t.Run("turn usage", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		if err := repository.EnsureCoreSchema(context.Background()); err != nil {
			t.Fatalf("EnsureCoreSchema() error = %v", err)
		}
		upsertListTurn(t, repository, "project-a", "session-a", "turn-a", "gpt-5", 1_000, 100)
		oneHundred := int64(100)
		usage := TurnUsage{
			TurnID: "turn-a", ObservedAtMS: 1_100, IsFinal: true,
			InputTokens: &oneHundred, SourceOffset: 200, Confidence: "exact", UpdatedAtMS: 1_100,
		}
		if err := repository.UpsertFacts(context.Background(), FactBatch{Usage: &usage}); err != nil {
			t.Fatalf("UpsertFacts(initial usage) error = %v", err)
		}
		if err := repository.UpsertFacts(context.Background(), FactBatch{Usage: &usage}); err != nil {
			t.Fatalf("UpsertFacts(exact usage replay) error = %v", err)
		}
		one := int64(1)
		conflict := usage
		conflict.InputTokens = &one
		err := repository.UpsertFacts(context.Background(), FactBatch{Usage: &conflict})
		if !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("UpsertFacts(conflicting usage) error = %v, want ErrInvalidRecord", err)
		}
	})

	t.Run("session current", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		if err := repository.EnsureCoreSchema(context.Background()); err != nil {
			t.Fatalf("EnsureCoreSchema() error = %v", err)
		}
		model := "gpt-5"
		current := SessionCurrent{SessionID: "session-a", CurrentModel: &model, UpdatedAtMS: 500}
		if err := repository.UpsertFacts(context.Background(), FactBatch{
			Session: &Session{
				SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
				CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 500,
			},
			SessionCurrent: &current,
		}); err != nil {
			t.Fatalf("UpsertFacts(initial current) error = %v", err)
		}
		if err := repository.UpsertFacts(context.Background(), FactBatch{SessionCurrent: &current}); err != nil {
			t.Fatalf("UpsertFacts(exact current replay) error = %v", err)
		}
		conflictingModel := "gpt-4"
		conflict := current
		conflict.CurrentModel = &conflictingModel
		err := repository.UpsertFacts(context.Background(), FactBatch{SessionCurrent: &conflict})
		if !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("UpsertFacts(conflicting current) error = %v, want ErrInvalidRecord", err)
		}
	})

	t.Run("session usage current", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		if err := repository.EnsureCoreSchema(context.Background()); err != nil {
			t.Fatalf("EnsureCoreSchema() error = %v", err)
		}
		if err := repository.UpsertFacts(context.Background(), FactBatch{Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl",
			CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 500,
		}}); err != nil {
			t.Fatalf("UpsertFacts(session) error = %v", err)
		}
		oneHundred := int64(100)
		usage := SessionUsageCurrent{
			SessionID: "session-a", CounterEpoch: 1, TotalInputTokens: &oneHundred,
			ObservedAtMS: 500, SourceOffset: 200, CounterState: "live",
		}
		if err := repository.UpsertFacts(context.Background(), FactBatch{SessionUsageCurrent: &usage}); err != nil {
			t.Fatalf("UpsertFacts(initial session usage) error = %v", err)
		}
		if err := repository.UpsertFacts(context.Background(), FactBatch{SessionUsageCurrent: &usage}); err != nil {
			t.Fatalf("UpsertFacts(exact session usage replay) error = %v", err)
		}
		one := int64(1)
		conflict := usage
		conflict.TotalInputTokens = &one
		err := repository.UpsertFacts(context.Background(), FactBatch{SessionUsageCurrent: &conflict})
		if !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("UpsertFacts(conflicting session usage) error = %v, want ErrInvalidRecord", err)
		}
	})
}

// 测试 stable Project/Session metadata 不受旧扫描或冲突重放的到达顺序影响。
func TestRepositoryStableMetadataRejectsRegressionAndConflicts(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	remote := "example.invalid/repo"
	project := Project{
		ProjectID: "project-a", DisplayName: "Current", RootPath: "/current",
		GitRemoteSanitized: &remote, CreatedAtMS: 100, UpdatedAtMS: 200,
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Project: &project}); err != nil {
		t.Fatalf("UpsertFacts(initial project) error = %v", err)
	}
	staleRemote := "example.invalid/stale"
	if err := repository.UpsertFacts(context.Background(), FactBatch{Project: &Project{
		ProjectID: "project-a", DisplayName: "Stale", RootPath: "/stale",
		GitRemoteSanitized: &staleRemote, CreatedAtMS: 90, UpdatedAtMS: 150,
	}}); err != nil {
		t.Fatalf("UpsertFacts(stale project) error = %v", err)
	}
	assertStoredProject(t, database, "Current", "/current", remote, 90, 200)

	if err := repository.UpsertFacts(context.Background(), FactBatch{Project: &project}); err != nil {
		t.Fatalf("UpsertFacts(exact project replay) error = %v", err)
	}
	equalTimeConflict := project
	equalTimeConflict.DisplayName = "Conflict"
	err := repository.UpsertFacts(context.Background(), FactBatch{Project: &equalTimeConflict})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(equal-time project conflict) error = %v, want ErrInvalidRecord", err)
	}

	newerProject := project
	newerProject.DisplayName = "Newer"
	newerProject.RootPath = "/newer"
	newerProject.GitRemoteSanitized = nil
	newerProject.UpdatedAtMS = 300
	if err := repository.UpsertFacts(context.Background(), FactBatch{Project: &newerProject}); err != nil {
		t.Fatalf("UpsertFacts(newer project) error = %v", err)
	}
	assertStoredProject(t, database, "Newer", "/newer", remote, 90, 300)

	originator := "codex-cli"
	modelProvider := "openai"
	initialCWD := "/stable"
	cliVersion := "v1"
	session := Session{
		SessionID: "session-a", Provider: "codex", Originator: &originator,
		SourceKind: "session_jsonl", ModelProvider: &modelProvider, InitialCWD: &initialCWD,
		CLIVersion: &cliVersion, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 200,
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Session: &session}); err != nil {
		t.Fatalf("UpsertFacts(initial session) error = %v", err)
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Session: &session}); err != nil {
		t.Fatalf("UpsertFacts(exact session replay) error = %v", err)
	}
	conflictingOriginator := "other-cli"
	conflict := session
	conflict.Originator = &conflictingOriginator
	err = repository.UpsertFacts(context.Background(), FactBatch{Session: &conflict})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(session metadata conflict) error = %v, want ErrInvalidRecord", err)
	}
	storedSession, err := repository.Session(context.Background(), "session-a")
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if storedSession.Originator == nil || *storedSession.Originator != originator {
		t.Fatalf("Session.Originator = %v, want %q", storedSession.Originator, originator)
	}
}

// 测试 Session 的 created/first-seen 只向更早收敛，last-seen 只向更晚推进。
func TestRepositorySessionTimestampsConvergeMonotonically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	upsert := func(createdAt, firstSeenAt, lastSeenAt int64) {
		t.Helper()
		err := repository.UpsertFacts(context.Background(), FactBatch{Session: &Session{
			SessionID:     "session-a",
			Provider:      "codex",
			SourceKind:    "session_jsonl",
			CreatedAtMS:   createdAt,
			FirstSeenAtMS: firstSeenAt,
			LastSeenAtMS:  lastSeenAt,
		}})
		if err != nil {
			t.Fatalf("UpsertFacts(%d, %d, %d) error = %v", createdAt, firstSeenAt, lastSeenAt, err)
		}
	}

	upsert(100, 100, 200)
	upsert(90, 95, 300)
	upsert(110, 110, 150)

	session, err := repository.Session(context.Background(), "session-a")
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if session.CreatedAtMS != 90 || session.FirstSeenAtMS != 95 || session.LastSeenAtMS != 300 {
		t.Fatalf(
			"session timestamps = (%d, %d, %d), want (90, 95, 300)",
			session.CreatedAtMS,
			session.FirstSeenAtMS,
			session.LastSeenAtMS,
		)
	}
}

// 测试 UpsertFacts 对缺失的 stored references 返回稳定 ErrInvalidRecord。
func TestRepositoryRejectsMissingStoredReferences(t *testing.T) {
	t.Parallel()

	missingProjectID := "project-missing"
	cases := []struct {
		name  string
		batch FactBatch
	}{
		{
			name: "session project missing",
			batch: FactBatch{Session: &Session{
				SessionID: "session-a", Provider: "codex", SourceKind: "session_jsonl", ProjectID: &missingProjectID,
			}},
		},
		{
			name: "turn session missing",
			batch: FactBatch{Turn: &Turn{
				TurnID: "turn-a", SessionID: "session-missing",
			}},
		},
		{
			name: "usage turn missing",
			batch: FactBatch{Usage: &TurnUsage{
				TurnID: "turn-missing", Confidence: "exact",
			}},
		},
		{
			name: "session current missing",
			batch: FactBatch{SessionCurrent: &SessionCurrent{
				SessionID: "session-missing",
			}},
		},
		{
			name: "session usage current missing",
			batch: FactBatch{SessionUsageCurrent: &SessionUsageCurrent{
				SessionID: "session-missing", CounterState: "unknown",
			}},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDatabase(t)
			repository := NewRepository(database)
			if err := repository.EnsureCoreSchema(context.Background()); err != nil {
				t.Fatalf("EnsureCoreSchema() error = %v", err)
			}
			err := repository.UpsertFacts(context.Background(), testCase.batch)
			if !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("UpsertFacts() error = %v, want ErrInvalidRecord", err)
			}
		})
	}
}

// 测试 Repository 在未绑定 Store 和单项事实不存在场景下返回稳定错误。
func TestRepositoryInvalidAndNotFoundErrors(t *testing.T) {
	t.Parallel()

	invalid := NewRepository(nil)
	if err := invalid.EnsureCoreSchema(context.Background()); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("EnsureCoreSchema(nil) error = %v, want ErrInvalidRepository", err)
	}
	if err := invalid.UpsertFacts(context.Background(), FactBatch{}); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("UpsertFacts(nil) error = %v, want ErrInvalidRepository", err)
	}
	if _, err := invalid.Session(context.Background(), "session-a"); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("Session(nil) error = %v, want ErrInvalidRepository", err)
	}
	if _, err := invalid.Turn(context.Background(), "turn-a"); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("Turn(nil) error = %v, want ErrInvalidRepository", err)
	}
	if _, err := invalid.ListTurns(context.Background(), TurnFilter{}); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("ListTurns(nil) error = %v, want ErrInvalidRepository", err)
	}

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	if _, err := repository.Session(context.Background(), "session-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := repository.Turn(context.Background(), "turn-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Turn(missing) error = %v, want ErrNotFound", err)
	}
}

func upsertListTurn(
	t *testing.T,
	repository *Repository,
	projectID, sessionID, turnID, model string,
	startedAt, startOffset int64,
) {
	t.Helper()

	err := repository.UpsertFacts(context.Background(), FactBatch{
		Project: &Project{
			ProjectID:   projectID,
			DisplayName: projectID,
			RootPath:    "/workspace/" + projectID,
			CreatedAtMS: 900,
			UpdatedAtMS: startedAt,
		},
		Session: &Session{
			SessionID:     sessionID,
			Provider:      "codex",
			SourceKind:    "session_jsonl",
			ProjectID:     &projectID,
			CreatedAtMS:   900,
			FirstSeenAtMS: 900,
			LastSeenAtMS:  startedAt,
		},
		Turn: &Turn{
			TurnID:           turnID,
			SessionID:        sessionID,
			StartedAtMS:      startedAt,
			Model:            &model,
			ProjectID:        &projectID,
			SourceGeneration: 0,
			StartOffset:      startOffset,
		},
	})
	if err != nil {
		t.Fatalf("UpsertFacts(%s) error = %v", turnID, err)
	}
}

func assertTurnIDs(t *testing.T, turns []TurnSnapshot, want ...string) {
	t.Helper()

	got := make([]string, len(turns))
	for index, turn := range turns {
		got[index] = turn.TurnID
	}
	if !equalStrings(got, want) {
		t.Fatalf("turn IDs = %v, want %v", got, want)
	}
}

func assertSessionCurrent(
	t *testing.T,
	repository *Repository,
	sessionID, threadName string,
	threadNameUpdatedAt int64,
	model string,
	updatedAt int64,
) {
	t.Helper()

	session, err := repository.Session(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("Session(%s) error = %v", sessionID, err)
	}
	if session.Current == nil || session.Current.ThreadName == nil ||
		*session.Current.ThreadName != threadName || session.Current.ThreadNameUpdatedAtMS == nil ||
		*session.Current.ThreadNameUpdatedAtMS != threadNameUpdatedAt ||
		session.Current.CurrentModel == nil || *session.Current.CurrentModel != model ||
		session.Current.UpdatedAtMS != updatedAt {
		t.Fatalf("Session(%s).Current = %#v", sessionID, session.Current)
	}
}

func assertStoredProject(
	t *testing.T,
	database *storesqlite.Store,
	displayName, rootPath, remote string,
	createdAt, updatedAt int64,
) {
	t.Helper()

	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var gotDisplayName, gotRootPath, gotRemote string
		var gotCreatedAt, gotUpdatedAt int64
		if err := rawQueryRow(ctx, connection, `
			SELECT display_name, root_path, git_remote_sanitized, created_at_ms, updated_at_ms
			FROM projects WHERE project_id = 'project-a'
		`).Scan(&gotDisplayName, &gotRootPath, &gotRemote, &gotCreatedAt, &gotUpdatedAt); err != nil {
			return err
		}
		if gotDisplayName != displayName || gotRootPath != rootPath || gotRemote != remote ||
			gotCreatedAt != createdAt || gotUpdatedAt != updatedAt {
			t.Errorf(
				"stored project = (%q, %q, %q, %d, %d), want (%q, %q, %q, %d, %d)",
				gotDisplayName, gotRootPath, gotRemote, gotCreatedAt, gotUpdatedAt,
				displayName, rootPath, remote, createdAt, updatedAt,
			)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect stored project: %v", err)
	}
}

func tableCounts(ctx context.Context, database *storesqlite.Store, tables []string) (map[string]int, error) {
	counts := make(map[string]int, len(tables))
	err := database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		for _, table := range tables {
			var count int
			query := "SELECT COUNT(*) FROM " + table
			if err := rawQueryRow(ctx, connection, query).Scan(&count); err != nil {
				return err
			}
			counts[table] = count
		}
		return nil
	})
	return counts, err
}
