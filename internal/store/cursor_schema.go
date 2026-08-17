package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

var cursorProviderSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "agent_provider_snapshots", Statement: `CREATE TABLE IF NOT EXISTS agent_provider_snapshots (
		provider TEXT PRIMARY KEY CHECK (provider IN ('codex','cursor')),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		collected_at_ms INTEGER NOT NULL CHECK (collected_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "agent_provider_sources", Statement: `CREATE TABLE IF NOT EXISTS agent_provider_sources (
		provider TEXT NOT NULL CHECK (provider IN ('codex','cursor')),
		source_key TEXT NOT NULL CHECK (length(source_key) BETWEEN 1 AND 128),
		source_type TEXT NOT NULL CHECK (length(source_type) BETWEEN 1 AND 64),
		state TEXT NOT NULL CHECK (state IN ('available','partial','unavailable','not_configured')),
		coverage_state TEXT NOT NULL CHECK (coverage_state IN ('exact','partial','unknown')),
		schema_version INTEGER CHECK (schema_version IS NULL OR schema_version >= 0),
		checkpoint_kind TEXT NOT NULL CHECK (checkpoint_kind IN ('snapshot','filesystem_scan','not_configured')),
		checkpoint_value TEXT CHECK (checkpoint_value IS NULL OR length(checkpoint_value) BETWEEN 1 AND 128),
		row_count INTEGER NOT NULL CHECK (row_count >= 0),
		last_attempt_at_ms INTEGER NOT NULL CHECK (last_attempt_at_ms >= 0),
		last_success_at_ms INTEGER CHECK (last_success_at_ms IS NULL OR last_success_at_ms >= 0),
		failure_code TEXT CHECK (failure_code IS NULL OR failure_code IN ('missing','permission','schema_incompatible','busy','corrupt','read_failed','not_configured')),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		PRIMARY KEY (provider, source_key)
	) STRICT`},
	{ObjectType: "table", Name: "cursor_sessions", Statement: `CREATE TABLE IF NOT EXISTS cursor_sessions (
		id INTEGER PRIMARY KEY,
		provider TEXT NOT NULL DEFAULT 'cursor' CHECK (provider = 'cursor'),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		project_key TEXT NOT NULL CHECK (length(project_key) BETWEEN 1 AND 128),
		project_display_name TEXT NOT NULL CHECK (length(project_display_name) BETWEEN 1 AND 128),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		last_activity_at_ms INTEGER NOT NULL CHECK (last_activity_at_ms >= created_at_ms),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
		request_count INTEGER NOT NULL CHECK (request_count >= 0),
		tool_call_count INTEGER NOT NULL CHECK (tool_call_count >= 0),
		ai_edit_count INTEGER NOT NULL CHECK (ai_edit_count >= 0),
		ai_lines_added INTEGER CHECK (ai_lines_added IS NULL OR ai_lines_added >= 0),
		ai_lines_removed INTEGER CHECK (ai_lines_removed IS NULL OR ai_lines_removed >= 0),
		lineage_conflict INTEGER NOT NULL CHECK (lineage_conflict IN (0,1)),
		coverage_state TEXT NOT NULL CHECK (coverage_state IN ('exact','partial','unknown')),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		UNIQUE (provider, external_session_id)
	) STRICT`},
	{ObjectType: "table", Name: "cursor_session_lineage", Statement: `CREATE TABLE IF NOT EXISTS cursor_session_lineage (
		session_id INTEGER NOT NULL REFERENCES cursor_sessions(id) ON DELETE CASCADE,
		source_key TEXT NOT NULL CHECK (length(source_key) BETWEEN 1 AND 128),
		lineage_key TEXT NOT NULL CHECK (length(lineage_key) = 64),
		content_digest TEXT NOT NULL CHECK (length(content_digest) = 64),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		PRIMARY KEY (session_id, source_key, lineage_key)
	) STRICT`},
	{ObjectType: "table", Name: "cursor_usage_events", Statement: `CREATE TABLE IF NOT EXISTS cursor_usage_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) BETWEEN 1 AND 128),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
		input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
		output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
		provenance TEXT NOT NULL CHECK (provenance = 'cursor_state_usage'),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "cursor_request_events", Statement: `CREATE TABLE IF NOT EXISTS cursor_request_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) BETWEEN 1 AND 128),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		provenance TEXT NOT NULL CHECK (provenance = 'cursor_generation_id'),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "cursor_tool_events", Statement: `CREATE TABLE IF NOT EXISTS cursor_tool_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) = 64),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 128),
		outcome TEXT NOT NULL CHECK (outcome IN ('succeeded','failed','unknown')),
		provenance TEXT NOT NULL CHECK (provenance IN ('cursor_transcript','cursor_state')),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "cursor_ai_edit_events", Statement: `CREATE TABLE IF NOT EXISTS cursor_ai_edit_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) = 64),
		external_session_id TEXT CHECK (external_session_id IS NULL OR length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
		edit_count INTEGER NOT NULL CHECK (edit_count > 0),
		provenance TEXT NOT NULL CHECK (provenance = 'cursor_ai_tracking'),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "index", Name: "idx_cursor_sessions_activity", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_sessions_activity ON cursor_sessions(last_activity_at_ms DESC, external_session_id DESC)`},
	{ObjectType: "index", Name: "idx_cursor_sessions_project", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_sessions_project ON cursor_sessions(project_key, last_activity_at_ms DESC)`},
	{ObjectType: "index", Name: "idx_cursor_usage_time", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_usage_time ON cursor_usage_events(occurred_at_ms, event_id)`},
	{ObjectType: "index", Name: "idx_cursor_requests_time", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_requests_time ON cursor_request_events(occurred_at_ms, event_id)`},
	{ObjectType: "index", Name: "idx_cursor_tools_time", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_tools_time ON cursor_tool_events(occurred_at_ms, event_id)`},
	{ObjectType: "index", Name: "idx_cursor_ai_edits_time", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_ai_edits_time ON cursor_ai_edit_events(occurred_at_ms, event_id)`},
}

func currentCursorProviderSchemaObjects() []storeschema.Object {
	objects := append([]storeschema.Object(nil), cursorProviderSchemaObjects...)
	for index := range objects {
		switch objects[index].Name {
		case "cursor_sessions":
			objects[index].Statement = appendSQLiteMigratedColumns(
				objects[index].Statement,
				"\n\t\tUNIQUE (provider",
				"`display_title` TEXT NOT NULL DEFAULT '未命名会话' CHECK (length(display_title) BETWEEN 1 AND 128)",
				"`title_source` TEXT NOT NULL DEFAULT 'fallback' CHECK (title_source IN ('cursor_composer_header','cursor_conversation_search','fallback'))",
			)
		case "agent_provider_sources":
			objects[index].Statement = `CREATE TABLE IF NOT EXISTS agent_provider_sources (
			provider TEXT NOT NULL CHECK (provider IN ('codex','cursor')),
			source_key TEXT NOT NULL CHECK (length(source_key) BETWEEN 1 AND 128),
			source_type TEXT NOT NULL CHECK (length(source_type) BETWEEN 1 AND 64),
			state TEXT NOT NULL CHECK (state IN ('available','partial','unavailable','not_configured')),
			coverage_state TEXT NOT NULL CHECK (coverage_state IN ('exact','partial','unknown')),
			schema_version INTEGER CHECK (schema_version IS NULL OR schema_version >= 0),
			checkpoint_kind TEXT NOT NULL CHECK (checkpoint_kind IN ('snapshot','filesystem_scan','not_configured')),
			checkpoint_value TEXT CHECK (checkpoint_value IS NULL OR length(checkpoint_value) BETWEEN 1 AND 128),
			row_count INTEGER NOT NULL CHECK (row_count >= 0),
			last_attempt_at_ms INTEGER NOT NULL CHECK (last_attempt_at_ms >= 0),
			last_success_at_ms INTEGER CHECK (last_success_at_ms IS NULL OR last_success_at_ms >= 0),
			failure_code TEXT CHECK (failure_code IS NULL OR failure_code IN ('missing','permission','schema_incompatible','busy','corrupt','read_failed','not_configured','auth_expired')),
			updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
			PRIMARY KEY (provider, source_key)
		) STRICT`
		}
	}
	return objects
}

var cursorSessionMetadataMigrationColumns = []struct {
	column     string
	definition string
}{
	{
		column:     "display_title",
		definition: `TEXT NOT NULL DEFAULT '未命名会话' CHECK (length(display_title) BETWEEN 1 AND 128)`,
	},
	{
		column:     "title_source",
		definition: `TEXT NOT NULL DEFAULT 'fallback' CHECK (title_source IN ('cursor_composer_header','cursor_conversation_search','fallback'))`,
	},
}
