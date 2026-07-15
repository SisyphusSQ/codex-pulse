package store

var quotaSchemaObjects = []schemaObject{
	{objectType: "table", name: "quota_observations", statement: `CREATE TABLE IF NOT EXISTS quota_observations (
		observation_id TEXT PRIMARY KEY CHECK (length(observation_id) > 0 AND length(observation_id) <= 512),
		account_scope TEXT NOT NULL CHECK (account_scope = 'default'),
		source TEXT NOT NULL CHECK (source IN ('local_jsonl', 'wham')),
		limit_id TEXT CHECK (limit_id IS NULL OR (length(limit_id) > 0 AND length(limit_id) <= 512)),
		window_kind TEXT NOT NULL CHECK (
			window_kind IN ('primary', 'secondary')
			OR (window_kind GLOB 'additional:*' AND length(window_kind) > length('additional:'))
		),
		used_percent REAL NOT NULL CHECK (used_percent >= 0.0 AND used_percent <= 100.0),
		window_minutes INTEGER NOT NULL CHECK (window_minutes > 0 AND window_minutes <= 525600),
		resets_at_ms INTEGER NOT NULL CHECK (resets_at_ms >= 0),
		plan_type TEXT CHECK (plan_type IS NULL OR plan_type IN (
			'free', 'go', 'plus', 'pro', 'prolite', 'team', 'self_serve_business_usage_based',
			'business', 'enterprise_cbp_usage_based', 'enterprise', 'edu', 'unknown'
		)),
		validity TEXT NOT NULL CHECK (validity IN ('accepted', 'suspicious', 'rejected')),
		rejection_reason TEXT CHECK (rejection_reason IS NULL OR rejection_reason IN (
			'missing_limit_id', 'missing_primary_window', 'reset_not_future', 'unknown_plan_type',
			'invalid_used_percent', 'invalid_window_minutes', 'invalid_resets_at', 'invalid_structure',
			'used_regression', 'reset_regression', 'observed_time_regression', 'source_conflict',
			'default_fallback'
		)),
		first_observed_at_ms INTEGER NOT NULL CHECK (first_observed_at_ms >= 0),
		last_observed_at_ms INTEGER NOT NULL CHECK (last_observed_at_ms >= first_observed_at_ms),
		sample_count INTEGER NOT NULL CHECK (sample_count > 0),
		request_id TEXT CHECK (request_id IS NULL OR length(request_id) > 0),
		session_id TEXT CHECK (session_id IS NULL OR length(session_id) > 0)
			REFERENCES sessions(session_id) ON DELETE SET NULL,
		source_file_id TEXT CHECK (source_file_id IS NULL OR length(source_file_id) > 0)
			REFERENCES source_files(source_file_id) ON DELETE RESTRICT,
		first_source_generation INTEGER NOT NULL CHECK (first_source_generation >= 0),
		first_source_offset INTEGER NOT NULL CHECK (first_source_offset >= 0),
		source_generation INTEGER NOT NULL CHECK (source_generation >= first_source_generation),
		source_offset INTEGER NOT NULL CHECK (source_offset >= 0),
		CHECK ((validity = 'accepted' AND rejection_reason IS NULL)
			OR (validity != 'accepted' AND rejection_reason IS NOT NULL)),
		CHECK (validity != 'accepted' OR (
			limit_id IS NOT NULL
			AND (plan_type IS NULL OR plan_type != 'unknown')
			AND resets_at_ms > last_observed_at_ms
		)),
		CHECK ((source = 'local_jsonl' AND source_file_id IS NOT NULL AND request_id IS NULL)
			OR (source = 'wham' AND source_file_id IS NULL AND request_id IS NOT NULL)),
		CHECK (source_generation > first_source_generation OR source_offset >= first_source_offset)
	) STRICT`},
	{objectType: "table", name: "quota_observation_receipts", statement: `CREATE TABLE IF NOT EXISTS quota_observation_receipts (
		observation_id TEXT PRIMARY KEY CHECK (length(observation_id) > 0 AND length(observation_id) <= 512),
		segment_observation_id TEXT NOT NULL CHECK (length(segment_observation_id) > 0 AND length(segment_observation_id) <= 512)
			REFERENCES quota_observations(observation_id) ON DELETE CASCADE,
		sample_sha256 TEXT NOT NULL CHECK (
			length(sample_sha256) = 64 AND sample_sha256 NOT GLOB '*[^0-9a-f]*'
		)
	) STRICT`},
	{objectType: "index", name: "idx_quota_observations_current", statement: `CREATE INDEX IF NOT EXISTS idx_quota_observations_current
		ON quota_observations(account_scope, source, source_file_id, window_kind, limit_id, last_observed_at_ms DESC, observation_id DESC)`},
	{objectType: "index", name: "idx_quota_observations_source_position", statement: `CREATE UNIQUE INDEX IF NOT EXISTS idx_quota_observations_source_position
		ON quota_observations(source_file_id, source_generation, source_offset, window_kind)
		WHERE source_file_id IS NOT NULL`},
	{objectType: "index", name: "idx_quota_observation_receipts_segment", statement: `CREATE INDEX IF NOT EXISTS idx_quota_observation_receipts_segment
		ON quota_observation_receipts(segment_observation_id, observation_id)`},
}
