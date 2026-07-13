package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const turnSnapshotColumns = `
	t.turn_id, t.session_id, t.started_at_ms, t.completed_at_ms, t.outcome,
	t.model, t.reasoning_effort, t.cwd, t.project_id, t.source_generation,
	t.start_offset, t.complete_offset,
	u.turn_id, u.observed_at_ms, u.is_final, u.input_tokens,
	u.cached_input_tokens, u.output_tokens, u.reasoning_tokens,
	u.context_window, u.source_generation, u.source_offset, u.confidence, u.updated_at_ms
`

type rowScanner interface {
	Scan(destinations ...any) error
}

// Session 返回稳定 Session 事实及其可选当前投影。
func (repository *Repository) Session(ctx context.Context, sessionID string) (SessionSnapshot, error) {
	if repository == nil || repository.database == nil {
		return SessionSnapshot{}, ErrInvalidRepository
	}

	var snapshot SessionSnapshot
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var originator, modelProvider, initialCWD, projectID, cliVersion sql.NullString
		var currentSessionID, threadName, activeTurnID, currentModel, currentCWD sql.NullString
		var threadNameUpdatedAt, lastActivityAt, currentUpdatedAt sql.NullInt64
		var usageSessionID, counterState sql.NullString
		var counterEpoch, totalInput, totalCached, totalOutput, totalReasoning sql.NullInt64
		var usageObservedAt, usageSourceGeneration, usageSourceOffset sql.NullInt64

		err := connection.QueryRowContext(ctx, `
			SELECT
				s.session_id, s.provider, s.originator, s.source_kind,
				s.model_provider, s.initial_cwd, s.project_id, s.cli_version,
				s.created_at_ms, s.first_seen_at_ms, s.last_seen_at_ms,
				c.session_id, c.thread_name, c.thread_name_updated_at_ms,
				c.active_turn_id, c.current_model, c.current_cwd,
				c.last_activity_at_ms, c.updated_at_ms,
				u.session_id, u.counter_epoch, u.total_input_tokens,
				u.total_cached_tokens, u.total_output_tokens,
				u.total_reasoning_tokens, u.observed_at_ms, u.source_generation, u.source_offset,
				u.counter_state
			FROM sessions AS s
			LEFT JOIN session_current AS c ON c.session_id = s.session_id
			LEFT JOIN session_usage_current AS u ON u.session_id = s.session_id
			WHERE s.session_id = ?
		`, sessionID).Scan(
			&snapshot.SessionID,
			&snapshot.Provider,
			&originator,
			&snapshot.SourceKind,
			&modelProvider,
			&initialCWD,
			&projectID,
			&cliVersion,
			&snapshot.CreatedAtMS,
			&snapshot.FirstSeenAtMS,
			&snapshot.LastSeenAtMS,
			&currentSessionID,
			&threadName,
			&threadNameUpdatedAt,
			&activeTurnID,
			&currentModel,
			&currentCWD,
			&lastActivityAt,
			&currentUpdatedAt,
			&usageSessionID,
			&counterEpoch,
			&totalInput,
			&totalCached,
			&totalOutput,
			&totalReasoning,
			&usageObservedAt,
			&usageSourceGeneration,
			&usageSourceOffset,
			&counterState,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		snapshot.Originator = stringPointer(originator)
		snapshot.ModelProvider = stringPointer(modelProvider)
		snapshot.InitialCWD = stringPointer(initialCWD)
		snapshot.ProjectID = stringPointer(projectID)
		snapshot.CLIVersion = stringPointer(cliVersion)
		if currentSessionID.Valid {
			snapshot.Current = &SessionCurrent{
				SessionID:             currentSessionID.String,
				ThreadName:            stringPointer(threadName),
				ThreadNameUpdatedAtMS: int64Pointer(threadNameUpdatedAt),
				ActiveTurnID:          stringPointer(activeTurnID),
				CurrentModel:          stringPointer(currentModel),
				CurrentCWD:            stringPointer(currentCWD),
				LastActivityAtMS:      int64Pointer(lastActivityAt),
				UpdatedAtMS:           currentUpdatedAt.Int64,
			}
		}
		if usageSessionID.Valid {
			snapshot.Usage = &SessionUsageCurrent{
				SessionID:            usageSessionID.String,
				CounterEpoch:         counterEpoch.Int64,
				TotalInputTokens:     int64Pointer(totalInput),
				TotalCachedTokens:    int64Pointer(totalCached),
				TotalOutputTokens:    int64Pointer(totalOutput),
				TotalReasoningTokens: int64Pointer(totalReasoning),
				ObservedAtMS:         usageObservedAt.Int64,
				SourceGeneration:     usageSourceGeneration.Int64,
				SourceOffset:         usageSourceOffset.Int64,
				CounterState:         counterState.String,
			}
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
		err := scanTurnSnapshot(connection.QueryRowContext(ctx, `
			SELECT `+turnSnapshotColumns+`
			FROM turns AS t
			LEFT JOIN turn_usage AS u
				ON u.turn_id = t.turn_id
				AND u.source_generation = t.source_generation
				AND (t.completed_at_ms IS NULL OR u.is_final = 1)
			WHERE t.turn_id = ?
		`, turnID), &snapshot)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
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

	conditions := []string{"1 = 1"}
	arguments := make([]any, 0, 8)
	if filter.SessionID != nil {
		conditions = append(conditions, "t.session_id = ?")
		arguments = append(arguments, *filter.SessionID)
	}
	if filter.ProjectID != nil {
		conditions = append(conditions, "t.project_id = ?")
		arguments = append(arguments, *filter.ProjectID)
	}
	if filter.Model != nil {
		conditions = append(conditions, "t.model = ?")
		arguments = append(arguments, *filter.Model)
	}
	if filter.SourceGeneration != nil {
		conditions = append(conditions, "t.source_generation = ?")
		arguments = append(arguments, *filter.SourceGeneration)
	}
	if filter.StartOffsetAtOrAfter != nil {
		conditions = append(conditions, "t.start_offset >= ?")
		arguments = append(arguments, *filter.StartOffsetAtOrAfter)
	}
	if filter.StartedAtOrAfterMS != nil {
		conditions = append(conditions, "t.started_at_ms >= ?")
		arguments = append(arguments, *filter.StartedAtOrAfterMS)
	}
	if filter.StartedBeforeMS != nil {
		conditions = append(conditions, "t.started_at_ms < ?")
		arguments = append(arguments, *filter.StartedBeforeMS)
	}
	arguments = append(arguments, limit)

	query := `
		SELECT ` + turnSnapshotColumns + `
		FROM turns AS t
		LEFT JOIN turn_usage AS u
			ON u.turn_id = t.turn_id
			AND u.source_generation = t.source_generation
			AND (t.completed_at_ms IS NULL OR u.is_final = 1)
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY t.started_at_ms DESC, t.turn_id DESC
		LIMIT ?
	`

	var snapshots []TurnSnapshot
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := connection.QueryContext(ctx, query, arguments...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var snapshot TurnSnapshot
			if err := scanTurnSnapshot(rows, &snapshot); err != nil {
				return err
			}
			snapshots = append(snapshots, snapshot)
		}
		return rows.Err()
	})
	return snapshots, err
}

func scanTurnSnapshot(scanner rowScanner, snapshot *TurnSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("scan turn snapshot: nil destination")
	}

	var completedAt, completeOffset sql.NullInt64
	var outcome, model, reasoningEffort, cwd, projectID sql.NullString
	var usageTurnID, confidence sql.NullString
	var observedAt, isFinal, inputTokens, cachedInputTokens sql.NullInt64
	var outputTokens, reasoningTokens, contextWindow, sourceGeneration, sourceOffset, updatedAt sql.NullInt64

	if err := scanner.Scan(
		&snapshot.TurnID,
		&snapshot.SessionID,
		&snapshot.StartedAtMS,
		&completedAt,
		&outcome,
		&model,
		&reasoningEffort,
		&cwd,
		&projectID,
		&snapshot.SourceGeneration,
		&snapshot.StartOffset,
		&completeOffset,
		&usageTurnID,
		&observedAt,
		&isFinal,
		&inputTokens,
		&cachedInputTokens,
		&outputTokens,
		&reasoningTokens,
		&contextWindow,
		&sourceGeneration,
		&sourceOffset,
		&confidence,
		&updatedAt,
	); err != nil {
		return err
	}

	snapshot.CompletedAtMS = int64Pointer(completedAt)
	snapshot.Outcome = stringPointer(outcome)
	snapshot.Model = stringPointer(model)
	snapshot.ReasoningEffort = stringPointer(reasoningEffort)
	snapshot.CWD = stringPointer(cwd)
	snapshot.ProjectID = stringPointer(projectID)
	snapshot.CompleteOffset = int64Pointer(completeOffset)
	if usageTurnID.Valid {
		snapshot.Usage = &TurnUsage{
			TurnID:            usageTurnID.String,
			ObservedAtMS:      observedAt.Int64,
			IsFinal:           isFinal.Int64 == 1,
			InputTokens:       int64Pointer(inputTokens),
			CachedInputTokens: int64Pointer(cachedInputTokens),
			OutputTokens:      int64Pointer(outputTokens),
			ReasoningTokens:   int64Pointer(reasoningTokens),
			ContextWindow:     int64Pointer(contextWindow),
			SourceGeneration:  sourceGeneration.Int64,
			SourceOffset:      sourceOffset.Int64,
			Confidence:        confidence.String,
			UpdatedAtMS:       updatedAt.Int64,
		}
	}
	return nil
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
