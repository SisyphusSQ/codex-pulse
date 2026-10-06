package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

var dshProviderSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "dsh_sessions", Statement: `CREATE TABLE IF NOT EXISTS dsh_sessions (
		id INTEGER PRIMARY KEY,
		provider TEXT NOT NULL DEFAULT 'dsh' CHECK (provider = 'dsh'),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		throughput TEXT CHECK (throughput IS NULL OR json_valid(throughput)),
 display_title TEXT NOT NULL DEFAULT '未命名会话' CHECK (length(display_title) BETWEEN 1 AND 128),
		title_source TEXT NOT NULL DEFAULT 'fallback' CHECK (title_source IN ('dsh_header','fallback')),
		project_key TEXT NOT NULL CHECK (length(project_key) BETWEEN 1 AND 128),
		project_display_name TEXT NOT NULL CHECK (length(project_display_name) BETWEEN 1 AND 128),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		last_activity_at_ms INTEGER NOT NULL CHECK (last_activity_at_ms >= created_at_ms),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
		request_count INTEGER NOT NULL CHECK (request_count >= 0),
		tool_call_count INTEGER NOT NULL CHECK (tool_call_count >= 0),
		lineage_conflict INTEGER NOT NULL CHECK (lineage_conflict IN (0,1)),
		coverage_state TEXT NOT NULL CHECK (coverage_state IN ('exact','partial','unknown')),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		UNIQUE (provider, external_session_id)
	) STRICT`},
	{ObjectType: "table", Name: "dsh_session_lineage", Statement: `CREATE TABLE IF NOT EXISTS dsh_session_lineage (
		session_id INTEGER NOT NULL REFERENCES dsh_sessions(id) ON DELETE CASCADE,
		source_key TEXT NOT NULL CHECK (length(source_key) BETWEEN 1 AND 128),
		lineage_key TEXT NOT NULL CHECK (length(lineage_key) = 64),
		content_digest TEXT NOT NULL CHECK (length(content_digest) = 64),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		PRIMARY KEY (session_id, source_key, lineage_key)
	) STRICT`},
	{ObjectType: "table", Name: "dsh_usage_events", Statement: `CREATE TABLE IF NOT EXISTS dsh_usage_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) BETWEEN 1 AND 128),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
        model_provider TEXT NOT NULL CHECK (length(model_provider) <= 128),
        cache_read_known INTEGER NOT NULL CHECK (cache_read_known IN (0,1)),
        cache_write_known INTEGER NOT NULL CHECK (cache_write_known IN (0,1)),
        reasoning_known INTEGER NOT NULL CHECK (reasoning_known IN (0,1)),
        total_known INTEGER NOT NULL CHECK (total_known IN (0,1)),
        started_at_ms INTEGER CHECK (started_at_ms IS NULL OR started_at_ms >= 0),
        ended_at_ms INTEGER CHECK (ended_at_ms IS NULL OR ended_at_ms >= started_at_ms),
        input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
		output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
		cached_read_tokens INTEGER NOT NULL CHECK (cached_read_tokens >= 0),
		cache_creation_tokens INTEGER NOT NULL CHECK (cache_creation_tokens >= 0),
		reasoning_tokens INTEGER NOT NULL CHECK (reasoning_tokens >= 0),
		total_tokens INTEGER NOT NULL CHECK (total_tokens >= 0),
		reported_cost_micros INTEGER CHECK (reported_cost_micros IS NULL OR reported_cost_micros >= 0),
		provenance TEXT NOT NULL CHECK (provenance = 'dsh_assistant_message'),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "dsh_tool_events", Statement: `CREATE TABLE IF NOT EXISTS dsh_tool_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) = 64),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 128),
		outcome TEXT NOT NULL CHECK (outcome IN ('succeeded','failed','unknown')),
		provenance TEXT NOT NULL CHECK (provenance = 'dsh_updates'),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "index", Name: "idx_dsh_sessions_activity", Statement: `CREATE INDEX IF NOT EXISTS idx_dsh_sessions_activity ON dsh_sessions(last_activity_at_ms DESC, external_session_id DESC)`},
	{ObjectType: "index", Name: "idx_dsh_sessions_project", Statement: `CREATE INDEX IF NOT EXISTS idx_dsh_sessions_project ON dsh_sessions(project_key, last_activity_at_ms DESC)`},
	{ObjectType: "index", Name: "idx_dsh_usage_time", Statement: `CREATE INDEX IF NOT EXISTS idx_dsh_usage_time ON dsh_usage_events(occurred_at_ms, event_id)`},
	{ObjectType: "index", Name: "idx_dsh_tools_time", Statement: `CREATE INDEX IF NOT EXISTS idx_dsh_tools_time ON dsh_tool_events(occurred_at_ms, event_id)`},
}
