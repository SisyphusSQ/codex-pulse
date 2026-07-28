package store

import (
	"context"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

var coreSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "projects", Statement: `CREATE TABLE IF NOT EXISTS projects (
		project_id TEXT PRIMARY KEY CHECK (length(project_id) > 0),
		display_name TEXT NOT NULL CHECK (length(display_name) > 0),
		root_path TEXT NOT NULL CHECK (length(root_path) > 0),
		git_remote_sanitized TEXT CHECK (git_remote_sanitized IS NULL OR length(git_remote_sanitized) > 0),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= created_at_ms)
	) STRICT`},
	{ObjectType: "table", Name: "sessions", Statement: `CREATE TABLE IF NOT EXISTS sessions (
		session_id TEXT PRIMARY KEY CHECK (length(session_id) > 0),
		provider TEXT NOT NULL CHECK (length(provider) > 0),
		originator TEXT CHECK (originator IS NULL OR length(originator) > 0),
		source_kind TEXT NOT NULL CHECK (length(source_kind) > 0),
		model_provider TEXT CHECK (model_provider IS NULL OR length(model_provider) > 0),
		initial_cwd TEXT CHECK (initial_cwd IS NULL OR length(initial_cwd) > 0),
		project_id TEXT CHECK (project_id IS NULL OR length(project_id) > 0) REFERENCES projects(project_id) ON DELETE SET NULL,
		cli_version TEXT CHECK (cli_version IS NULL OR length(cli_version) > 0),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		first_seen_at_ms INTEGER NOT NULL CHECK (first_seen_at_ms >= 0),
		last_seen_at_ms INTEGER NOT NULL CHECK (last_seen_at_ms >= first_seen_at_ms)
	) STRICT`},
	{ObjectType: "table", Name: "turns", Statement: turnsSchemaCurrentStatement},
	{ObjectType: "table", Name: "session_current", Statement: `CREATE TABLE IF NOT EXISTS session_current (
		session_id TEXT PRIMARY KEY CHECK (length(session_id) > 0) REFERENCES sessions(session_id) ON DELETE CASCADE,
		thread_name TEXT CHECK (thread_name IS NULL OR length(thread_name) > 0),
		thread_name_updated_at_ms INTEGER,
		active_turn_id TEXT CHECK (active_turn_id IS NULL OR length(active_turn_id) > 0) REFERENCES turns(turn_id) ON DELETE SET NULL,
		current_model TEXT CHECK (current_model IS NULL OR length(current_model) > 0),
		current_cwd TEXT CHECK (current_cwd IS NULL OR length(current_cwd) > 0),
		last_activity_at_ms INTEGER CHECK (last_activity_at_ms IS NULL OR last_activity_at_ms >= 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		CHECK (
			(thread_name IS NULL AND thread_name_updated_at_ms IS NULL)
			OR (
				thread_name IS NOT NULL
				AND thread_name_updated_at_ms IS NOT NULL
				AND thread_name_updated_at_ms >= 0
				AND thread_name_updated_at_ms <= updated_at_ms
			)
		)
	) STRICT`},
	{ObjectType: "table", Name: "turn_usage", Statement: `CREATE TABLE IF NOT EXISTS turn_usage (
		turn_id TEXT PRIMARY KEY CHECK (length(turn_id) > 0) REFERENCES turns(turn_id) ON DELETE CASCADE,
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		is_final INTEGER NOT NULL CHECK (is_final IN (0, 1)),
		input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
		cached_input_tokens INTEGER CHECK (cached_input_tokens IS NULL OR cached_input_tokens >= 0),
		output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
		reasoning_tokens INTEGER CHECK (reasoning_tokens IS NULL OR reasoning_tokens >= 0),
		context_window INTEGER CHECK (context_window IS NULL OR context_window >= 0),
		source_generation INTEGER NOT NULL CHECK (source_generation >= 0),
		source_offset INTEGER NOT NULL CHECK (source_offset >= 0),
		confidence TEXT NOT NULL CHECK (length(confidence) > 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= observed_at_ms)
	) STRICT`},
	{ObjectType: "table", Name: "session_usage_current", Statement: `CREATE TABLE IF NOT EXISTS session_usage_current (
		session_id TEXT PRIMARY KEY CHECK (length(session_id) > 0) REFERENCES sessions(session_id) ON DELETE CASCADE,
		counter_epoch INTEGER NOT NULL CHECK (counter_epoch >= 0),
		total_input_tokens INTEGER CHECK (total_input_tokens IS NULL OR total_input_tokens >= 0),
		total_cached_tokens INTEGER CHECK (total_cached_tokens IS NULL OR total_cached_tokens >= 0),
		total_output_tokens INTEGER CHECK (total_output_tokens IS NULL OR total_output_tokens >= 0),
		total_reasoning_tokens INTEGER CHECK (total_reasoning_tokens IS NULL OR total_reasoning_tokens >= 0),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		source_generation INTEGER NOT NULL CHECK (source_generation >= 0),
		source_offset INTEGER NOT NULL CHECK (source_offset >= 0),
		counter_state TEXT NOT NULL CHECK (length(counter_state) > 0)
	) STRICT`},
	{ObjectType: "index", Name: "idx_turns_source_position", Statement: `CREATE UNIQUE INDEX IF NOT EXISTS idx_turns_source_position
		ON turns(session_id, source_generation, start_offset)`},
	{ObjectType: "index", Name: "idx_turns_session_lifecycle", Statement: `CREATE INDEX IF NOT EXISTS idx_turns_session_lifecycle
		ON turns(session_id, started_at_ms DESC, turn_id DESC, completed_at_ms)`},
	{ObjectType: "index", Name: "idx_turns_project_time", Statement: `CREATE INDEX IF NOT EXISTS idx_turns_project_time
		ON turns(project_id, started_at_ms DESC, turn_id DESC, completed_at_ms)`},
	{ObjectType: "index", Name: "idx_turns_model_time", Statement: `CREATE INDEX IF NOT EXISTS idx_turns_model_time
		ON turns(model, started_at_ms DESC, turn_id DESC, completed_at_ms)`},
	{ObjectType: "index", Name: "idx_session_current_activity", Statement: `CREATE INDEX IF NOT EXISTS idx_session_current_activity
		ON session_current(last_activity_at_ms)`},
	{ObjectType: "index", Name: "idx_turn_usage_observed_final", Statement: `CREATE INDEX IF NOT EXISTS idx_turn_usage_observed_final
		ON turn_usage(observed_at_ms, is_final)`},
}

// EnsureCoreSchema 在单一 writer transaction 中创建核心事实和投影表。
func (repository *Repository) EnsureCoreSchema(ctx context.Context) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		return storeschema.EnsureObjects(ctx, transaction, coreSchemaObjects)
	})
}

// EnsureApplicationSchema 在单一 writer transaction 中确保核心与运行事实 schema。
func (repository *Repository) EnsureApplicationSchema(ctx context.Context) error {
	_, err := repository.MigrateApplicationSchema(ctx)
	return err
}
