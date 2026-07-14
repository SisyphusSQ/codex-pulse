package store

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// Session 返回稳定 Session 事实及其可选当前投影。
func (repository *Repository) Session(ctx context.Context, sessionID string) (SessionSnapshot, error) {
	if repository == nil || repository.database == nil {
		return SessionSnapshot{}, ErrInvalidRepository
	}

	var snapshot SessionSnapshot
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		database := connection.WithContext(ctx)
		var session sessionModel
		if err := database.Take(&session, "session_id = ?", sessionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		snapshot.Session = sessionFromModel(session)

		var current sessionCurrentModel
		err := database.Take(&current, "session_id = ?", sessionID).Error
		switch {
		case err == nil:
			value := sessionCurrentFromModel(current)
			snapshot.Current = &value
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}

		var usage sessionUsageCurrentModel
		err = database.Take(&usage, "session_id = ?", sessionID).Error
		switch {
		case err == nil:
			value := sessionUsageCurrentFromModel(usage)
			snapshot.Usage = &value
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		return nil
	})
	return snapshot, err
}

// Turn 返回稳定 Turn 事实及其可选 usage fact。
func (repository *Repository) Turn(ctx context.Context, turnID string) (TurnSnapshot, error) {
	if repository == nil || repository.database == nil {
		return TurnSnapshot{}, ErrInvalidRepository
	}

	var snapshot TurnSnapshot
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		database := connection.WithContext(ctx)
		var turn turnModel
		if err := database.Take(&turn, "turn_id = ?", turnID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		snapshot = turnSnapshotFromModel(turn, nil)

		var usage turnUsageModel
		query := database.Where("turn_id = ? AND source_generation = ?", turnID, turn.SourceGeneration)
		if turn.CompletedAtMS != nil {
			query = query.Where("is_final = ?", true)
		}
		err := query.Take(&usage).Error
		switch {
		case err == nil:
			snapshot = turnSnapshotFromModel(turn, &usage)
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		return nil
	})
	return snapshot, err
}

// ListTurns 使用固定字段白名单组合查询条件并返回稳定倒序结果。
func (repository *Repository) ListTurns(ctx context.Context, filter TurnFilter) ([]TurnSnapshot, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	limit, err := validateTurnFilter(filter)
	if err != nil {
		return nil, err
	}

	var snapshots []TurnSnapshot
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		query := connection.WithContext(ctx).Model(&turnModel{})
		if filter.SessionID != nil {
			query = query.Where("session_id = ?", *filter.SessionID)
		}
		if filter.ProjectID != nil {
			query = query.Where("project_id = ?", *filter.ProjectID)
		}
		if filter.Model != nil {
			query = query.Where("model = ?", *filter.Model)
		}
		if filter.SourceGeneration != nil {
			query = query.Where("source_generation = ?", *filter.SourceGeneration)
		}
		if filter.StartOffsetAtOrAfter != nil {
			query = query.Where("start_offset >= ?", *filter.StartOffsetAtOrAfter)
		}
		if filter.StartedAtOrAfterMS != nil {
			query = query.Where("started_at_ms >= ?", *filter.StartedAtOrAfterMS)
		}
		if filter.StartedBeforeMS != nil {
			query = query.Where("started_at_ms < ?", *filter.StartedBeforeMS)
		}

		var turns []turnModel
		if err := query.Order("started_at_ms DESC").Order("turn_id DESC").Limit(limit).Find(&turns).Error; err != nil {
			return err
		}
		if len(turns) == 0 {
			return nil
		}

		turnIDs := make([]string, 0, len(turns))
		for _, turn := range turns {
			turnIDs = append(turnIDs, turn.TurnID)
		}
		var usages []turnUsageModel
		if err := connection.WithContext(ctx).Where("turn_id IN ?", turnIDs).Find(&usages).Error; err != nil {
			return err
		}
		usageByTurn := make(map[string]turnUsageModel, len(usages))
		for _, usage := range usages {
			usageByTurn[usage.TurnID] = usage
		}

		snapshots = make([]TurnSnapshot, 0, len(turns))
		for _, turn := range turns {
			usage, found := usageByTurn[turn.TurnID]
			if !found || usage.SourceGeneration != turn.SourceGeneration ||
				(turn.CompletedAtMS != nil && !usage.IsFinal) {
				snapshots = append(snapshots, turnSnapshotFromModel(turn, nil))
				continue
			}
			snapshots = append(snapshots, turnSnapshotFromModel(turn, &usage))
		}
		return nil
	})
	return snapshots, err
}

func sessionFromModel(model sessionModel) Session {
	return Session{
		SessionID: model.SessionID, Provider: model.Provider, Originator: model.Originator,
		SourceKind: model.SourceKind, ModelProvider: model.ModelProvider, InitialCWD: model.InitialCWD,
		ProjectID: model.ProjectID, CLIVersion: model.CLIVersion, CreatedAtMS: model.CreatedAtMS,
		FirstSeenAtMS: model.FirstSeenAtMS, LastSeenAtMS: model.LastSeenAtMS,
	}
}

func sessionCurrentFromModel(model sessionCurrentModel) SessionCurrent {
	return SessionCurrent{
		SessionID: model.SessionID, ThreadName: model.ThreadName,
		ThreadNameUpdatedAtMS: model.ThreadNameUpdatedAtMS, ActiveTurnID: model.ActiveTurnID,
		CurrentModel: model.CurrentModel, CurrentCWD: model.CurrentCWD,
		LastActivityAtMS: model.LastActivityAtMS, UpdatedAtMS: model.UpdatedAtMS,
	}
}

func sessionUsageCurrentFromModel(model sessionUsageCurrentModel) SessionUsageCurrent {
	return SessionUsageCurrent{
		SessionID: model.SessionID, CounterEpoch: model.CounterEpoch,
		TotalInputTokens: model.TotalInputTokens, TotalCachedTokens: model.TotalCachedTokens,
		TotalOutputTokens: model.TotalOutputTokens, TotalReasoningTokens: model.TotalReasoningTokens,
		ObservedAtMS: model.ObservedAtMS, SourceGeneration: model.SourceGeneration,
		SourceOffset: model.SourceOffset, CounterState: model.CounterState,
	}
}

func turnSnapshotFromModel(turn turnModel, usage *turnUsageModel) TurnSnapshot {
	snapshot := TurnSnapshot{Turn: Turn{
		TurnID: turn.TurnID, SessionID: turn.SessionID, StartedAtMS: turn.StartedAtMS,
		CompletedAtMS: turn.CompletedAtMS, Outcome: turn.Outcome, Model: turn.Model,
		ReasoningEffort: turn.ReasoningEffort, CWD: turn.CWD, ProjectID: turn.ProjectID,
		SourceGeneration: turn.SourceGeneration, StartOffset: turn.StartOffset,
		CompleteOffset: turn.CompleteOffset,
	}}
	if usage != nil {
		snapshot.Usage = &TurnUsage{
			TurnID: usage.TurnID, ObservedAtMS: usage.ObservedAtMS, IsFinal: usage.IsFinal,
			InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
			OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens,
			ContextWindow: usage.ContextWindow, SourceGeneration: usage.SourceGeneration,
			SourceOffset: usage.SourceOffset, Confidence: usage.Confidence, UpdatedAtMS: usage.UpdatedAtMS,
		}
	}
	return snapshot
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func int64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}
