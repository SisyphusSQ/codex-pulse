package store

import (
	"context"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func upsertProject(ctx context.Context, transaction storesqlite.WriteTx, project Project) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO projects (
			project_id, display_name, root_path, git_remote_sanitized,
			created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			display_name = CASE
				WHEN excluded.updated_at_ms > projects.updated_at_ms THEN excluded.display_name
				ELSE projects.display_name
			END,
			root_path = CASE
				WHEN excluded.updated_at_ms > projects.updated_at_ms THEN excluded.root_path
				ELSE projects.root_path
			END,
			git_remote_sanitized = CASE
				WHEN excluded.updated_at_ms > projects.updated_at_ms
					THEN COALESCE(excluded.git_remote_sanitized, projects.git_remote_sanitized)
				ELSE projects.git_remote_sanitized
			END,
			created_at_ms = MIN(projects.created_at_ms, excluded.created_at_ms),
			updated_at_ms = MAX(projects.updated_at_ms, excluded.updated_at_ms)
	`,
		project.ProjectID,
		project.DisplayName,
		project.RootPath,
		nullableString(project.GitRemoteSanitized),
		project.CreatedAtMS,
		project.UpdatedAtMS,
	)
	return err
}

func upsertSession(ctx context.Context, transaction storesqlite.WriteTx, session Session) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO sessions (
			session_id, provider, originator, source_kind, model_provider,
			initial_cwd, project_id, cli_version, created_at_ms,
			first_seen_at_ms, last_seen_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			originator = COALESCE(sessions.originator, excluded.originator),
			model_provider = COALESCE(sessions.model_provider, excluded.model_provider),
			initial_cwd = COALESCE(sessions.initial_cwd, excluded.initial_cwd),
			project_id = COALESCE(sessions.project_id, excluded.project_id),
			cli_version = COALESCE(sessions.cli_version, excluded.cli_version),
			created_at_ms = MIN(sessions.created_at_ms, excluded.created_at_ms),
			first_seen_at_ms = MIN(sessions.first_seen_at_ms, excluded.first_seen_at_ms),
			last_seen_at_ms = MAX(sessions.last_seen_at_ms, excluded.last_seen_at_ms)
	`,
		session.SessionID,
		session.Provider,
		nullableString(session.Originator),
		session.SourceKind,
		nullableString(session.ModelProvider),
		nullableString(session.InitialCWD),
		nullableString(session.ProjectID),
		nullableString(session.CLIVersion),
		session.CreatedAtMS,
		session.FirstSeenAtMS,
		session.LastSeenAtMS,
	)
	return err
}

func upsertTurn(ctx context.Context, transaction storesqlite.WriteTx, turn Turn) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO turns (
			turn_id, session_id, started_at_ms, completed_at_ms, outcome,
			model, reasoning_effort, cwd, project_id, source_generation,
			start_offset, complete_offset
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(turn_id) DO UPDATE SET
			started_at_ms = MIN(turns.started_at_ms, excluded.started_at_ms),
			completed_at_ms = COALESCE(excluded.completed_at_ms, turns.completed_at_ms),
			outcome = COALESCE(excluded.outcome, turns.outcome),
			model = COALESCE(excluded.model, turns.model),
			reasoning_effort = COALESCE(excluded.reasoning_effort, turns.reasoning_effort),
			cwd = COALESCE(excluded.cwd, turns.cwd),
			project_id = COALESCE(excluded.project_id, turns.project_id),
			source_generation = excluded.source_generation,
			start_offset = excluded.start_offset,
			complete_offset = COALESCE(excluded.complete_offset, turns.complete_offset)
		WHERE (
				excluded.source_generation > turns.source_generation
				AND (turns.completed_at_ms IS NULL OR excluded.completed_at_ms IS NOT NULL)
			)
			OR (
				excluded.source_generation = turns.source_generation
				AND excluded.start_offset = turns.start_offset
				AND (turns.completed_at_ms IS NULL OR excluded.completed_at_ms IS NOT NULL)
			)
	`,
		turn.TurnID,
		turn.SessionID,
		turn.StartedAtMS,
		nullableInt64(turn.CompletedAtMS),
		nullableString(turn.Outcome),
		nullableString(turn.Model),
		nullableString(turn.ReasoningEffort),
		nullableString(turn.CWD),
		nullableString(turn.ProjectID),
		turn.SourceGeneration,
		turn.StartOffset,
		nullableInt64(turn.CompleteOffset),
	)
	return err
}

func upsertTurnUsage(ctx context.Context, transaction storesqlite.WriteTx, usage TurnUsage) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO turn_usage (
			turn_id, observed_at_ms, is_final, input_tokens, cached_input_tokens,
			output_tokens, reasoning_tokens, context_window, source_generation, source_offset,
			confidence, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(turn_id) DO UPDATE SET
			observed_at_ms = excluded.observed_at_ms,
			is_final = excluded.is_final,
			input_tokens = excluded.input_tokens,
			cached_input_tokens = excluded.cached_input_tokens,
			output_tokens = excluded.output_tokens,
			reasoning_tokens = excluded.reasoning_tokens,
			context_window = excluded.context_window,
			source_generation = excluded.source_generation,
			source_offset = excluded.source_offset,
			confidence = excluded.confidence,
			updated_at_ms = excluded.updated_at_ms
		WHERE (
				excluded.source_generation > turn_usage.source_generation
				OR (
					excluded.source_generation = turn_usage.source_generation
					AND excluded.source_offset > turn_usage.source_offset
				)
			)
			AND (turn_usage.is_final = 0 OR excluded.is_final = 1)
	`,
		usage.TurnID,
		usage.ObservedAtMS,
		boolInteger(usage.IsFinal),
		nullableInt64(usage.InputTokens),
		nullableInt64(usage.CachedInputTokens),
		nullableInt64(usage.OutputTokens),
		nullableInt64(usage.ReasoningTokens),
		nullableInt64(usage.ContextWindow),
		usage.SourceGeneration,
		usage.SourceOffset,
		usage.Confidence,
		usage.UpdatedAtMS,
	)
	return err
}

func removeInvalidTurnUsage(ctx context.Context, transaction storesqlite.WriteTx, turnID string) error {
	_, err := transaction.ExecContext(ctx, `
		DELETE FROM turn_usage
		WHERE turn_id = ?
		  AND (
			source_generation <> (
				SELECT source_generation FROM turns WHERE turn_id = ?
			)
			OR (
				is_final = 0
				AND EXISTS (
					SELECT 1 FROM turns
					WHERE turn_id = ? AND completed_at_ms IS NOT NULL
				)
			)
		  )
	`, turnID, turnID, turnID)
	return err
}

func upsertSessionCurrent(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	current SessionCurrent,
) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO session_current (
			session_id, thread_name, thread_name_updated_at_ms, active_turn_id,
			current_model, current_cwd, last_activity_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			thread_name = CASE
				WHEN excluded.thread_name_updated_at_ms IS NOT NULL
					AND (
						session_current.thread_name_updated_at_ms IS NULL
						OR excluded.thread_name_updated_at_ms > session_current.thread_name_updated_at_ms
					) THEN excluded.thread_name
				ELSE session_current.thread_name
			END,
			thread_name_updated_at_ms = CASE
				WHEN excluded.thread_name_updated_at_ms IS NOT NULL
					AND (
						session_current.thread_name_updated_at_ms IS NULL
						OR excluded.thread_name_updated_at_ms > session_current.thread_name_updated_at_ms
					) THEN excluded.thread_name_updated_at_ms
				ELSE session_current.thread_name_updated_at_ms
			END,
			active_turn_id = CASE
				WHEN excluded.updated_at_ms > session_current.updated_at_ms THEN excluded.active_turn_id
				ELSE session_current.active_turn_id
			END,
			current_model = CASE
				WHEN excluded.updated_at_ms > session_current.updated_at_ms THEN excluded.current_model
				ELSE session_current.current_model
			END,
			current_cwd = CASE
				WHEN excluded.updated_at_ms > session_current.updated_at_ms THEN excluded.current_cwd
				ELSE session_current.current_cwd
			END,
			last_activity_at_ms = CASE
				WHEN excluded.updated_at_ms > session_current.updated_at_ms THEN excluded.last_activity_at_ms
				ELSE session_current.last_activity_at_ms
			END,
			updated_at_ms = MAX(session_current.updated_at_ms, excluded.updated_at_ms)
	`,
		current.SessionID,
		nullableString(current.ThreadName),
		nullableInt64(current.ThreadNameUpdatedAtMS),
		nullableString(current.ActiveTurnID),
		nullableString(current.CurrentModel),
		nullableString(current.CurrentCWD),
		nullableInt64(current.LastActivityAtMS),
		current.UpdatedAtMS,
	)
	return err
}

func upsertSessionUsageCurrent(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	usage SessionUsageCurrent,
) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO session_usage_current (
			session_id, counter_epoch, total_input_tokens, total_cached_tokens,
			total_output_tokens, total_reasoning_tokens, observed_at_ms,
			source_generation, source_offset, counter_state
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			counter_epoch = excluded.counter_epoch,
			total_input_tokens = excluded.total_input_tokens,
			total_cached_tokens = excluded.total_cached_tokens,
			total_output_tokens = excluded.total_output_tokens,
			total_reasoning_tokens = excluded.total_reasoning_tokens,
			observed_at_ms = excluded.observed_at_ms,
			source_generation = excluded.source_generation,
			source_offset = excluded.source_offset,
			counter_state = excluded.counter_state
		WHERE excluded.source_generation > session_usage_current.source_generation
			OR (
				excluded.source_generation = session_usage_current.source_generation
				AND (
					excluded.counter_epoch > session_usage_current.counter_epoch
					OR (
						excluded.counter_epoch = session_usage_current.counter_epoch
						AND excluded.source_offset > session_usage_current.source_offset
					)
				)
			)
	`,
		usage.SessionID,
		usage.CounterEpoch,
		nullableInt64(usage.TotalInputTokens),
		nullableInt64(usage.TotalCachedTokens),
		nullableInt64(usage.TotalOutputTokens),
		nullableInt64(usage.TotalReasoningTokens),
		usage.ObservedAtMS,
		usage.SourceGeneration,
		usage.SourceOffset,
		usage.CounterState,
	)
	return err
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
