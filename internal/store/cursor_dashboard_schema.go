package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

var cursorDashboardV23SchemaObjects = append([]storeschema.Object{{
	ObjectType: "table", Name: "cursor_dashboard_snapshots", Statement: `CREATE TABLE IF NOT EXISTS cursor_dashboard_snapshots (
		provider TEXT PRIMARY KEY CHECK (provider = 'cursor'),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		collected_at_ms INTEGER NOT NULL CHECK (collected_at_ms >= 0),
		window_start_ms INTEGER NOT NULL CHECK (window_start_ms >= 0),
		window_end_ms INTEGER NOT NULL CHECK (window_end_ms > window_start_ms),
		event_count INTEGER NOT NULL CHECK (event_count >= 0)
	) STRICT`,
}}, cursorDashboardUsageSchemaObjects...)

var cursorDashboardSchemaObjects = append([]storeschema.Object{{
	ObjectType: "table", Name: "cursor_dashboard_snapshots", Statement: `CREATE TABLE IF NOT EXISTS cursor_dashboard_snapshots (
		provider TEXT PRIMARY KEY CHECK (provider = 'cursor'),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		collected_at_ms INTEGER NOT NULL CHECK (collected_at_ms >= 0),
		window_start_ms INTEGER NOT NULL CHECK (window_start_ms >= 0),
		window_end_ms INTEGER NOT NULL CHECK (window_end_ms > window_start_ms),
		billing_cycle_end_ms INTEGER NOT NULL CHECK (billing_cycle_end_ms > window_start_ms),
		plan_total_spend_micros INTEGER CHECK (plan_total_spend_micros IS NULL OR plan_total_spend_micros >= 0),
		plan_included_spend_micros INTEGER CHECK (plan_included_spend_micros IS NULL OR plan_included_spend_micros >= 0),
		plan_bonus_spend_micros INTEGER CHECK (plan_bonus_spend_micros IS NULL OR plan_bonus_spend_micros >= 0),
		plan_remaining_micros INTEGER CHECK (plan_remaining_micros IS NULL OR plan_remaining_micros >= 0),
		plan_limit_micros INTEGER CHECK (plan_limit_micros IS NULL OR plan_limit_micros >= 0),
		event_count INTEGER NOT NULL CHECK (event_count >= 0),
		CHECK (
			(plan_total_spend_micros IS NULL AND plan_included_spend_micros IS NULL AND plan_bonus_spend_micros IS NULL AND plan_remaining_micros IS NULL AND plan_limit_micros IS NULL)
			OR (plan_total_spend_micros IS NOT NULL AND plan_included_spend_micros IS NOT NULL AND plan_bonus_spend_micros IS NOT NULL AND plan_remaining_micros IS NOT NULL AND plan_limit_micros IS NOT NULL)
		)
	) STRICT`,
}}, cursorDashboardUsageSchemaObjects...)

var cursorDashboardQuotaSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "cursor_dashboard_quota_observations", Statement: `CREATE TABLE IF NOT EXISTS cursor_dashboard_quota_observations (
		provider TEXT NOT NULL CHECK (provider = 'cursor'),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		limit_id TEXT NOT NULL CHECK (limit_id IN ('cursor.models', 'cursor.other_models')),
		used_percent REAL NOT NULL CHECK (used_percent >= 0.0 AND used_percent <= 100.0),
		cycle_start_at_ms INTEGER NOT NULL CHECK (cycle_start_at_ms >= 0),
		cycle_end_at_ms INTEGER NOT NULL CHECK (cycle_end_at_ms > cycle_start_at_ms),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= cycle_start_at_ms AND observed_at_ms <= cycle_end_at_ms),
		PRIMARY KEY (provider, generation, limit_id)
	) STRICT`},
	{ObjectType: "index", Name: "idx_cursor_dashboard_quota_history", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_dashboard_quota_history
		ON cursor_dashboard_quota_observations(provider, limit_id, cycle_start_at_ms, observed_at_ms, generation)`},
}

var cursorDashboardUsageSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "cursor_dashboard_usage_events", Statement: `CREATE TABLE IF NOT EXISTS cursor_dashboard_usage_events (
		event_fingerprint TEXT PRIMARY KEY CHECK (length(event_fingerprint) = 64 AND event_fingerprint NOT GLOB '*[^0-9a-f]*'),
		occurrence_count INTEGER NOT NULL CHECK (occurrence_count > 0),
		external_session_id TEXT CHECK (external_session_id IS NULL OR length(external_session_id) BETWEEN 1 AND 128),
		occurred_at_ms INTEGER NOT NULL CHECK (occurred_at_ms >= 0),
		model_key TEXT CHECK (model_key IS NULL OR length(model_key) BETWEEN 1 AND 128),
		kind INTEGER NOT NULL CHECK (kind >= 0),
		token_based INTEGER NOT NULL CHECK (token_based IN (0, 1)),
		input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
		output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
		cache_write_tokens INTEGER NOT NULL CHECK (cache_write_tokens >= 0),
		cache_read_tokens INTEGER NOT NULL CHECK (cache_read_tokens >= 0),
		reported_charge_micros INTEGER NOT NULL CHECK (reported_charge_micros >= 0),
		cursor_token_fee_micros INTEGER NOT NULL CHECK (cursor_token_fee_micros >= 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{ObjectType: "index", Name: "idx_cursor_dashboard_usage_time", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_dashboard_usage_time
		ON cursor_dashboard_usage_events(occurred_at_ms, event_fingerprint)`},
	{ObjectType: "index", Name: "idx_cursor_dashboard_usage_session", Statement: `CREATE INDEX IF NOT EXISTS idx_cursor_dashboard_usage_session
		ON cursor_dashboard_usage_events(external_session_id, occurred_at_ms)`},
}
