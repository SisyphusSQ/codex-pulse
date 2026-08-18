package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

const grokProviderCheck = "('codex','cursor','grok')"

var grokProviderSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "grok_sessions", Statement: `CREATE TABLE IF NOT EXISTS grok_sessions (
		id INTEGER PRIMARY KEY,
		provider TEXT NOT NULL DEFAULT 'grok' CHECK (provider = 'grok'),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		display_title TEXT NOT NULL DEFAULT '未命名会话' CHECK (length(display_title) BETWEEN 1 AND 128),
		title_source TEXT NOT NULL DEFAULT 'fallback' CHECK (title_source IN ('grok_summary','fallback')),
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
	{ObjectType: "table", Name: "grok_session_lineage", Statement: `CREATE TABLE IF NOT EXISTS grok_session_lineage (
		session_id INTEGER NOT NULL REFERENCES grok_sessions(id) ON DELETE CASCADE,
		source_key TEXT NOT NULL CHECK (length(source_key) BETWEEN 1 AND 128),
		lineage_key TEXT NOT NULL CHECK (length(lineage_key) = 64),
		content_digest TEXT NOT NULL CHECK (length(content_digest) = 64),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		PRIMARY KEY (session_id, source_key, lineage_key)
	) STRICT`},
	{ObjectType: "table", Name: "grok_usage_events", Statement: `CREATE TABLE IF NOT EXISTS grok_usage_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) BETWEEN 1 AND 128),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
		input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
		output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
		cached_read_tokens INTEGER NOT NULL CHECK (cached_read_tokens >= 0),
		cache_creation_tokens INTEGER NOT NULL CHECK (cache_creation_tokens >= 0),
		reasoning_tokens INTEGER NOT NULL CHECK (reasoning_tokens >= 0),
		total_tokens INTEGER NOT NULL CHECK (total_tokens >= 0),
		reported_cost_micros INTEGER CHECK (reported_cost_micros IS NULL OR reported_cost_micros >= 0),
		provenance TEXT NOT NULL CHECK (provenance = 'grok_turn_completed'),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "grok_tool_events", Statement: `CREATE TABLE IF NOT EXISTS grok_tool_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) = 64),
		external_session_id TEXT NOT NULL CHECK (length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 128),
		outcome TEXT NOT NULL CHECK (outcome IN ('succeeded','failed','unknown')),
		provenance TEXT NOT NULL CHECK (provenance = 'grok_updates'),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "grok_billing_snapshots", Statement: `CREATE TABLE IF NOT EXISTS grok_billing_snapshots (
		provider TEXT PRIMARY KEY CHECK (provider = 'grok'),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		collected_at_ms INTEGER NOT NULL CHECK (collected_at_ms >= 0),
		period_type TEXT NOT NULL CHECK (period_type IN ('weekly','monthly')),
		period_start_ms INTEGER NOT NULL CHECK (period_start_ms >= 0),
		period_end_ms INTEGER NOT NULL CHECK (period_end_ms > period_start_ms),
		used_percent REAL NOT NULL CHECK (used_percent >= 0 AND used_percent <= 100),
		on_demand_used REAL CHECK (on_demand_used IS NULL OR on_demand_used >= 0),
		on_demand_cap REAL CHECK (on_demand_cap IS NULL OR on_demand_cap >= 0),
		prepaid_balance REAL CHECK (prepaid_balance IS NULL OR prepaid_balance >= 0),
		subscription_tier TEXT CHECK (subscription_tier IS NULL OR length(subscription_tier) BETWEEN 1 AND 128),
		is_unified_billing INTEGER NOT NULL CHECK (is_unified_billing IN (0,1))
	) STRICT`},
	{ObjectType: "table", Name: "grok_billing_quota_observations", Statement: `CREATE TABLE IF NOT EXISTS grok_billing_quota_observations (
		provider TEXT NOT NULL CHECK (provider = 'grok'),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		limit_id TEXT NOT NULL CHECK (length(limit_id) BETWEEN 1 AND 128),
		used_percent REAL NOT NULL CHECK (used_percent >= 0 AND used_percent <= 100),
		cycle_start_at_ms INTEGER NOT NULL CHECK (cycle_start_at_ms >= 0),
		cycle_end_at_ms INTEGER NOT NULL CHECK (cycle_end_at_ms > cycle_start_at_ms),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		PRIMARY KEY (provider, generation, limit_id)
	) STRICT`},
	{ObjectType: "index", Name: "idx_grok_sessions_activity", Statement: `CREATE INDEX IF NOT EXISTS idx_grok_sessions_activity ON grok_sessions(last_activity_at_ms DESC, external_session_id DESC)`},
	{ObjectType: "index", Name: "idx_grok_sessions_project", Statement: `CREATE INDEX IF NOT EXISTS idx_grok_sessions_project ON grok_sessions(project_key, last_activity_at_ms DESC)`},
	{ObjectType: "index", Name: "idx_grok_usage_time", Statement: `CREATE INDEX IF NOT EXISTS idx_grok_usage_time ON grok_usage_events(occurred_at_ms, event_id)`},
	{ObjectType: "index", Name: "idx_grok_tools_time", Statement: `CREATE INDEX IF NOT EXISTS idx_grok_tools_time ON grok_tool_events(occurred_at_ms, event_id)`},
}

func currentAgentProviderSnapshotStatement() string {
	return `CREATE TABLE IF NOT EXISTS agent_provider_snapshots (
		provider TEXT PRIMARY KEY CHECK (provider IN ` + grokProviderCheck + `),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		collected_at_ms INTEGER NOT NULL CHECK (collected_at_ms >= 0)
	) STRICT`
}

func currentAgentProviderSourceStatement() string {
	return `CREATE TABLE IF NOT EXISTS agent_provider_sources (
			provider TEXT NOT NULL CHECK (provider IN ` + grokProviderCheck + `),
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

func currentProviderSchemaObjects() []storeschema.Object {
	objects := currentCursorProviderSchemaObjects()
	for index := range objects {
		switch objects[index].Name {
		case "agent_provider_snapshots":
			objects[index].Statement = currentAgentProviderSnapshotStatement()
		case "agent_provider_sources":
			objects[index].Statement = currentAgentProviderSourceStatement()
		}
	}
	return append(objects, grokProviderSchemaObjects...)
}
