package store

var quotaProjectionSchemaObjects = []schemaObject{
	{objectType: "table", name: "quota_current", statement: `CREATE TABLE IF NOT EXISTS quota_current (
		account_scope TEXT NOT NULL CHECK (account_scope = 'default'),
		window_kind TEXT NOT NULL CHECK (
			window_kind IN ('primary', 'secondary')
			OR (window_kind GLOB 'additional:*' AND length(window_kind) > length('additional:'))
		),
		limit_id TEXT NOT NULL CHECK (length(limit_id) > 0 AND length(limit_id) <= 512),
		observation_id TEXT REFERENCES quota_observations(observation_id) ON DELETE RESTRICT,
		effective_used_percent REAL CHECK (
			effective_used_percent IS NULL OR (effective_used_percent >= 0.0 AND effective_used_percent <= 100.0)
		),
		window_minutes INTEGER CHECK (window_minutes IS NULL OR (window_minutes > 0 AND window_minutes <= 525600)),
		resets_at_ms INTEGER CHECK (resets_at_ms IS NULL OR resets_at_ms >= 0),
		window_generation INTEGER CHECK (window_generation IS NULL OR window_generation >= 0),
		selected_source TEXT CHECK (selected_source IS NULL OR selected_source IN ('local_jsonl', 'wham')),
		freshness_state TEXT NOT NULL CHECK (freshness_state IN (
			'never_loaded', 'fresh', 'stale', 'expired_unknown', 'suspicious'
		)),
		conflict_state TEXT NOT NULL CHECK (conflict_state IN ('none', 'conflict')),
		fresh_until_ms INTEGER CHECK (fresh_until_ms IS NULL OR fresh_until_ms >= 0),
		last_success_at_ms INTEGER CHECK (last_success_at_ms IS NULL OR last_success_at_ms >= 0),
		last_attempt_at_ms INTEGER CHECK (last_attempt_at_ms IS NULL OR last_attempt_at_ms >= 0),
		rule_version TEXT NOT NULL CHECK (length(rule_version) > 0 AND length(rule_version) <= 128),
		explanation_code TEXT NOT NULL CHECK (explanation_code IN (
			'trusted', 'stale', 'expired_unknown', 'suspicious_candidate', 'source_conflict', 'unavailable'
		)),
		evaluated_at_ms INTEGER NOT NULL CHECK (evaluated_at_ms >= 0),
		PRIMARY KEY (account_scope, window_kind, limit_id),
		CHECK (
			(freshness_state = 'never_loaded'
				AND observation_id IS NULL AND effective_used_percent IS NULL AND window_minutes IS NULL
				AND resets_at_ms IS NULL AND window_generation IS NULL AND selected_source IS NULL
				AND fresh_until_ms IS NULL AND last_success_at_ms IS NULL AND conflict_state = 'none')
			OR
			(freshness_state != 'never_loaded'
				AND observation_id IS NOT NULL AND effective_used_percent IS NOT NULL AND window_minutes IS NOT NULL
				AND resets_at_ms IS NOT NULL AND window_generation IS NOT NULL AND selected_source IS NOT NULL
				AND fresh_until_ms IS NOT NULL AND last_success_at_ms IS NOT NULL)
		),
		CHECK (fresh_until_ms IS NULL OR resets_at_ms IS NOT NULL AND fresh_until_ms <= resets_at_ms),
		CHECK (window_generation IS NULL OR window_generation = resets_at_ms),
		CHECK (last_success_at_ms IS NULL OR last_attempt_at_ms IS NOT NULL AND last_success_at_ms <= last_attempt_at_ms),
		CHECK (conflict_state != 'conflict' OR observation_id IS NOT NULL),
		CHECK (freshness_state != 'never_loaded' OR explanation_code = 'unavailable')
	) STRICT`},
	{objectType: "table", name: "quota_arbitration_evidence", statement: `CREATE TABLE IF NOT EXISTS quota_arbitration_evidence (
		account_scope TEXT NOT NULL CHECK (account_scope = 'default'),
		window_kind TEXT NOT NULL CHECK (
			window_kind IN ('primary', 'secondary')
			OR (window_kind GLOB 'additional:*' AND length(window_kind) > length('additional:'))
		),
		limit_id TEXT NOT NULL CHECK (length(limit_id) > 0 AND length(limit_id) <= 512),
		observation_id TEXT NOT NULL REFERENCES quota_observations(observation_id) ON DELETE RESTRICT,
		window_generation INTEGER NOT NULL CHECK (window_generation >= 0),
		disposition TEXT NOT NULL CHECK (disposition IN ('selected', 'eligible', 'superseded', 'suspicious', 'rejected')),
		reason TEXT CHECK (reason IS NULL OR reason IN (
			'missing_limit_id', 'missing_primary_window', 'reset_not_future', 'unknown_plan_type',
			'invalid_used_percent', 'invalid_window_minutes', 'invalid_resets_at', 'invalid_structure',
			'used_regression', 'reset_regression', 'observed_time_regression', 'source_conflict',
			'default_fallback'
		)),
		explanation_code TEXT NOT NULL CHECK (explanation_code IN (
			'trusted', 'stale', 'expired_unknown', 'suspicious_candidate', 'source_conflict', 'unavailable'
		)),
		PRIMARY KEY (account_scope, window_kind, limit_id, observation_id),
		FOREIGN KEY (account_scope, window_kind, limit_id)
			REFERENCES quota_current(account_scope, window_kind, limit_id) ON DELETE CASCADE,
		CHECK (
			(disposition IN ('suspicious', 'rejected') AND reason IS NOT NULL)
			OR (disposition IN ('selected', 'eligible') AND (reason IS NULL OR reason = 'source_conflict'))
			OR (disposition = 'superseded' AND reason IS NULL)
		)
	) STRICT`},
	{objectType: "index", name: "idx_quota_current_freshness", statement: `CREATE INDEX IF NOT EXISTS idx_quota_current_freshness
		ON quota_current(account_scope, freshness_state, fresh_until_ms, resets_at_ms, window_kind, limit_id)`},
	{objectType: "index", name: "idx_quota_arbitration_evidence_observation", statement: `CREATE INDEX IF NOT EXISTS idx_quota_arbitration_evidence_observation
		ON quota_arbitration_evidence(observation_id, account_scope, window_kind, limit_id)`},
}
