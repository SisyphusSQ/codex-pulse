package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

// agyProviderSchemaObjects 定义 Antigravity (AGY) 独立事实表与索引；
// 不建 agy_tool_events，且严禁在表结构中保存邮箱原文、提示词正文或本地完整路径。
var agyProviderSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "agy_sessions", Statement: `CREATE TABLE IF NOT EXISTS agy_sessions (
		id INTEGER PRIMARY KEY,
		provider TEXT NOT NULL DEFAULT 'agy' CHECK (provider = 'agy'),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		display_title TEXT NOT NULL DEFAULT '未命名会话' CHECK (length(display_title) BETWEEN 1 AND 512),
		title_source TEXT NOT NULL DEFAULT 'fallback' CHECK (title_source IN ('agy_annotation','agy_summary','fallback')),
		parent_external_session_id TEXT CHECK (parent_external_session_id IS NULL OR length(parent_external_session_id) BETWEEN 1 AND 128),
		session_role TEXT NOT NULL DEFAULT 'unknown' CHECK (session_role IN ('main','subagent','swarm_worker','side_question','unknown')),
		project_key TEXT NOT NULL CHECK (length(project_key) BETWEEN 1 AND 128),
		project_display_name TEXT NOT NULL CHECK (length(project_display_name) BETWEEN 1 AND 128),
		project_source TEXT NOT NULL DEFAULT 'unassigned' CHECK (project_source IN ('workspace','inherited_parent','unassigned')),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		last_activity_at_ms INTEGER NOT NULL CHECK (last_activity_at_ms >= created_at_ms),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
		request_count INTEGER NOT NULL CHECK (request_count >= 0),
		lineage_conflict INTEGER NOT NULL CHECK (lineage_conflict IN (0,1)),
		coverage_state TEXT NOT NULL CHECK (coverage_state IN ('exact','partial','unknown')),
		throughput TEXT CHECK (throughput IS NULL OR json_valid(throughput)),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		UNIQUE (provider, external_session_id)
	) STRICT`},
	{ObjectType: "table", Name: "agy_session_lineage", Statement: `CREATE TABLE IF NOT EXISTS agy_session_lineage (
		session_id INTEGER NOT NULL REFERENCES agy_sessions(id) ON DELETE CASCADE,
		source_key TEXT NOT NULL CHECK (length(source_key) BETWEEN 1 AND 128),
		lineage_key TEXT NOT NULL CHECK (length(lineage_key) = 64),
		content_digest TEXT NOT NULL CHECK (length(content_digest) = 64),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		PRIMARY KEY (session_id, source_key, lineage_key)
	) STRICT`},
	{ObjectType: "table", Name: "agy_usage_events", Statement: `CREATE TABLE IF NOT EXISTS agy_usage_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) = 64 AND event_id NOT GLOB '*[^0-9a-f]*'),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		step_index INTEGER NOT NULL CHECK (step_index >= 0),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
		uncached_input_tokens INTEGER NOT NULL CHECK (uncached_input_tokens >= 0),
		cache_read_tokens INTEGER NOT NULL CHECK (cache_read_tokens >= 0),
		output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
		reasoning_tokens INTEGER NOT NULL CHECK (reasoning_tokens >= 0),
		total_tokens INTEGER NOT NULL CHECK (total_tokens >= 0),
		cache_read_known INTEGER NOT NULL CHECK (cache_read_known IN (0,1)),
		reasoning_known INTEGER NOT NULL CHECK (reasoning_known IN (0,1)),
		total_known INTEGER NOT NULL CHECK (total_known IN (0,1)),
		started_at_ms INTEGER CHECK (started_at_ms IS NULL OR started_at_ms >= 0),
		ended_at_ms INTEGER CHECK (ended_at_ms IS NULL OR (started_at_ms IS NOT NULL AND ended_at_ms >= started_at_ms)),
		provenance TEXT NOT NULL CHECK (provenance IN ('agy_step_metadata','agy_transcript_jsonl')),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "agy_quota_snapshots", Statement: `CREATE TABLE IF NOT EXISTS agy_quota_snapshots (
		account_scope TEXT NOT NULL CHECK (length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*'),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		plan_type TEXT CHECK (plan_type IS NULL OR length(plan_type) BETWEEN 1 AND 128),
		limit_id TEXT NOT NULL CHECK (length(limit_id) BETWEEN 1 AND 128),
		limit_name TEXT NOT NULL CHECK (length(limit_name) BETWEEN 1 AND 128),
		window_kind TEXT NOT NULL CHECK (window_kind IN ('primary','secondary')),
		window_minutes INTEGER NOT NULL CHECK (window_minutes > 0),
		used_percent INTEGER CHECK (used_percent IS NULL OR used_percent BETWEEN 0 AND 100),
		resets_at_ms INTEGER CHECK (resets_at_ms IS NULL OR resets_at_ms >= 0),
		data_as_of_ms INTEGER NOT NULL CHECK (data_as_of_ms >= 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		PRIMARY KEY (account_scope, limit_id, window_kind)
	) STRICT`},
	{ObjectType: "table", Name: "agy_quota_observations", Statement: `CREATE TABLE IF NOT EXISTS agy_quota_observations (
		account_scope TEXT NOT NULL CHECK (length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*'),
		limit_id TEXT NOT NULL CHECK (length(limit_id) BETWEEN 1 AND 128),
		window_kind TEXT NOT NULL CHECK (window_kind IN ('primary','secondary')),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		used_percent INTEGER NOT NULL CHECK (used_percent BETWEEN 0 AND 100),
		resets_at_ms INTEGER CHECK (resets_at_ms IS NULL OR resets_at_ms >= 0),
		PRIMARY KEY (account_scope, limit_id, window_kind, observed_at_ms)
	) STRICT`},
	{ObjectType: "index", Name: "idx_agy_sessions_activity", Statement: `CREATE INDEX IF NOT EXISTS idx_agy_sessions_activity ON agy_sessions(last_activity_at_ms DESC, external_session_id DESC)`},
	{ObjectType: "index", Name: "idx_agy_sessions_project", Statement: `CREATE INDEX IF NOT EXISTS idx_agy_sessions_project ON agy_sessions(project_key, last_activity_at_ms DESC)`},
	{ObjectType: "index", Name: "idx_agy_usage_time", Statement: `CREATE INDEX IF NOT EXISTS idx_agy_usage_time ON agy_usage_events(occurred_at_ms, event_id)`},
	{ObjectType: "index", Name: "idx_agy_quota_observations_window", Statement: `CREATE INDEX IF NOT EXISTS idx_agy_quota_observations_window ON agy_quota_observations(account_scope, limit_id, window_kind, observed_at_ms DESC)`},
}
