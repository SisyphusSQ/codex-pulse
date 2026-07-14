package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// ErrSchemaContract 表示既有数据库结构与当前核心 schema 不兼容。
var ErrSchemaContract = errors.New("core schema contract mismatch")

type schemaObject struct {
	objectType string
	name       string
	statement  string
}

var coreSchemaObjects = []schemaObject{
	{objectType: "table", name: "projects", statement: `CREATE TABLE IF NOT EXISTS projects (
		project_id TEXT PRIMARY KEY CHECK (length(project_id) > 0),
		display_name TEXT NOT NULL CHECK (length(display_name) > 0),
		root_path TEXT NOT NULL CHECK (length(root_path) > 0),
		git_remote_sanitized TEXT CHECK (git_remote_sanitized IS NULL OR length(git_remote_sanitized) > 0),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= created_at_ms)
	) STRICT`},
	{objectType: "table", name: "sessions", statement: `CREATE TABLE IF NOT EXISTS sessions (
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
	{objectType: "table", name: "turns", statement: turnsSchemaCurrentStatement},
	{objectType: "table", name: "session_current", statement: `CREATE TABLE IF NOT EXISTS session_current (
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
	{objectType: "table", name: "turn_usage", statement: `CREATE TABLE IF NOT EXISTS turn_usage (
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
	{objectType: "table", name: "session_usage_current", statement: `CREATE TABLE IF NOT EXISTS session_usage_current (
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
	{objectType: "index", name: "idx_turns_source_position", statement: `CREATE UNIQUE INDEX IF NOT EXISTS idx_turns_source_position
		ON turns(session_id, source_generation, start_offset)`},
	{objectType: "index", name: "idx_turns_session_lifecycle", statement: `CREATE INDEX IF NOT EXISTS idx_turns_session_lifecycle
		ON turns(session_id, started_at_ms DESC, turn_id DESC, completed_at_ms)`},
	{objectType: "index", name: "idx_turns_project_time", statement: `CREATE INDEX IF NOT EXISTS idx_turns_project_time
		ON turns(project_id, started_at_ms DESC, turn_id DESC, completed_at_ms)`},
	{objectType: "index", name: "idx_turns_model_time", statement: `CREATE INDEX IF NOT EXISTS idx_turns_model_time
		ON turns(model, started_at_ms DESC, turn_id DESC, completed_at_ms)`},
	{objectType: "index", name: "idx_session_current_activity", statement: `CREATE INDEX IF NOT EXISTS idx_session_current_activity
		ON session_current(last_activity_at_ms)`},
	{objectType: "index", name: "idx_turn_usage_observed_final", statement: `CREATE INDEX IF NOT EXISTS idx_turn_usage_observed_final
		ON turn_usage(observed_at_ms, is_final)`},
}

// EnsureCoreSchema 在单一 writer transaction 中创建核心事实和投影表。
func (repository *Repository) EnsureCoreSchema(ctx context.Context) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return ensureSchemaObjects(ctx, transaction, coreSchemaObjects)
	})
}

// EnsureApplicationSchema 在单一 writer transaction 中确保核心与运行事实 schema。
func (repository *Repository) EnsureApplicationSchema(ctx context.Context) error {
	_, err := repository.MigrateApplicationSchema(ctx)
	return err
}

func ensureSchemaObjects(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	objects []schemaObject,
) error {
	for _, object := range objects {
		if err := ensureSchemaObject(ctx, transaction, object); err != nil {
			return err
		}
	}
	return nil
}

func ensureSchemaObject(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	object schemaObject,
) error {
	exists, err := verifySchemaObject(ctx, transaction, object)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := transaction.WithContext(ctx).Exec(object.statement).Error; err != nil {
		return err
	}
	exists, err = verifySchemaObject(ctx, transaction, object)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %s %q was not created", ErrSchemaContract, object.objectType, object.name)
	}
	return nil
}

func verifySchemaObject(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	object schemaObject,
) (bool, error) {
	var actualType string
	var actualSQL sql.NullString
	// STRICT、CHECK 与特殊索引的 canonical DDL 无法由 GORM Migrator 完整表达，
	// 因此这里只读取 sqlite_schema 做精确契约校验。
	err := transaction.WithContext(ctx).
		Raw(`SELECT type, sql FROM sqlite_schema WHERE name = ?`, object.name).
		Row().Scan(&actualType, &actualSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: read %s %q: %v", ErrSchemaContract, object.objectType, object.name, err)
	}
	if actualType != object.objectType || !actualSQL.Valid ||
		normalizeSchemaSQL(actualSQL.String) != normalizeSchemaSQL(canonicalSchemaSQL(object.statement)) {
		return false, fmt.Errorf("%w: %s %q differs from canonical definition", ErrSchemaContract, object.objectType, object.name)
	}
	return true, nil
}

func canonicalSchemaSQL(statement string) string {
	return strings.Replace(statement, " IF NOT EXISTS", "", 1)
}

func normalizeSchemaSQL(statement string) string {
	return strings.ToLower(strings.Join(strings.Fields(statement), " "))
}
