package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

const (
	codexAccountBindingMigrationName = "codex-account-binding"
	sourceRefreshGlobalFenceGroup    = "codex_app_server_rate_limits"
)

var accountBindingSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "codex_account_scope_key", Statement: `CREATE TABLE IF NOT EXISTS codex_account_scope_key (
		singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
		key_bytes BLOB NOT NULL CHECK (length(key_bytes) = 32),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "codex_account_scopes", Statement: `CREATE TABLE IF NOT EXISTS codex_account_scopes (
		account_scope TEXT PRIMARY KEY CHECK (
			length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*'
		),
		first_seen_at_ms INTEGER NOT NULL CHECK (first_seen_at_ms >= 0),
		last_seen_at_ms INTEGER NOT NULL CHECK (last_seen_at_ms >= first_seen_at_ms)
	) STRICT`},
	{ObjectType: "table", Name: "codex_account_binding", Statement: `CREATE TABLE IF NOT EXISTS codex_account_binding (
		singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
		state TEXT NOT NULL CHECK (state IN (
			'unknown', 'pending', 'confirmed', 'signed_out', 'identity_unavailable'
		)),
		account_scope TEXT REFERENCES codex_account_scopes(account_scope) ON DELETE RESTRICT,
		last_confirmed_scope TEXT REFERENCES codex_account_scopes(account_scope) ON DELETE RESTRICT,
		binding_generation INTEGER NOT NULL CHECK (binding_generation >= 0),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		reason TEXT NOT NULL CHECK (reason IN (
			'startup', 'stable', 'account_changed', 'signed_out', 'missing_account_id',
			'confirmation_failed', 'unsupported_app_server'
		)),
		CHECK ((state = 'confirmed') = (account_scope IS NOT NULL)),
		CHECK (state != 'confirmed' OR last_confirmed_scope IS NOT NULL),
		CHECK (state != 'confirmed' OR account_scope = last_confirmed_scope),
		CHECK (state != 'confirmed' OR binding_generation > 0)
	) STRICT`},
	{ObjectType: "table", Name: "source_refresh_global_fences", Statement: `CREATE TABLE IF NOT EXISTS source_refresh_global_fences (
		source_group TEXT PRIMARY KEY CHECK (source_group = 'codex_app_server_rate_limits'),
		not_before_ms INTEGER NOT NULL CHECK (not_before_ms >= 0),
		reason TEXT NOT NULL CHECK (reason IN ('manual_interval', 'network_backoff')),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
}

var quotaSchemaObjectsV32 = []storeschema.Object{
	{ObjectType: "table", Name: "quota_observations", Statement: `CREATE TABLE IF NOT EXISTS quota_observations (
		observation_id TEXT PRIMARY KEY CHECK (length(observation_id) > 0 AND length(observation_id) <= 512),
		account_scope TEXT NOT NULL CHECK (account_scope = 'default' OR (length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*')),
		source TEXT NOT NULL CHECK (source IN ('local_jsonl', 'wham', 'app_server')),
		limit_id TEXT CHECK (limit_id IS NULL OR (length(limit_id) > 0 AND length(limit_id) <= 512)),
		limit_name TEXT CHECK (limit_name IS NULL OR (length(limit_name) BETWEEN 1 AND 512)),
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
			OR (source IN ('wham', 'app_server') AND source_file_id IS NULL AND request_id IS NOT NULL)),
		CHECK (source_generation > first_source_generation OR source_offset >= first_source_offset)
	) STRICT`},
	{ObjectType: "table", Name: "quota_observation_receipts", Statement: `CREATE TABLE IF NOT EXISTS quota_observation_receipts (
		observation_id TEXT PRIMARY KEY CHECK (length(observation_id) > 0 AND length(observation_id) <= 512),
		segment_observation_id TEXT NOT NULL CHECK (length(segment_observation_id) > 0 AND length(segment_observation_id) <= 512)
			REFERENCES quota_observations(observation_id) ON DELETE CASCADE,
		sample_sha256 TEXT NOT NULL CHECK (
			length(sample_sha256) = 64 AND sample_sha256 NOT GLOB '*[^0-9a-f]*'
		)
	) STRICT`},
	{ObjectType: "index", Name: "idx_quota_observations_current", Statement: `CREATE INDEX IF NOT EXISTS idx_quota_observations_current
		ON quota_observations(account_scope, source, source_file_id, window_kind, limit_id, last_observed_at_ms DESC, observation_id DESC)`},
	{ObjectType: "index", Name: "idx_quota_observations_source_position", Statement: `CREATE UNIQUE INDEX IF NOT EXISTS idx_quota_observations_source_position
		ON quota_observations(source_file_id, source_generation, source_offset, window_kind)
		WHERE source_file_id IS NOT NULL`},
	{ObjectType: "index", Name: "idx_quota_observation_receipts_segment", Statement: `CREATE INDEX IF NOT EXISTS idx_quota_observation_receipts_segment
		ON quota_observation_receipts(segment_observation_id, observation_id)`},
}

var quotaProjectionSchemaObjectsV32 = []storeschema.Object{
	{ObjectType: "table", Name: "quota_current", Statement: `CREATE TABLE IF NOT EXISTS quota_current (
		account_scope TEXT NOT NULL CHECK (account_scope = 'default' OR (length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*')),
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
		selected_source TEXT CHECK (selected_source IS NULL OR selected_source IN ('local_jsonl', 'wham', 'app_server')),
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
	{ObjectType: "table", Name: "quota_arbitration_evidence", Statement: `CREATE TABLE IF NOT EXISTS quota_arbitration_evidence (
		account_scope TEXT NOT NULL CHECK (account_scope = 'default' OR (length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*')),
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
	{ObjectType: "index", Name: "idx_quota_current_freshness", Statement: `CREATE INDEX IF NOT EXISTS idx_quota_current_freshness
		ON quota_current(account_scope, freshness_state, fresh_until_ms, resets_at_ms, window_kind, limit_id)`},
	{ObjectType: "index", Name: "idx_quota_arbitration_evidence_observation", Statement: `CREATE INDEX IF NOT EXISTS idx_quota_arbitration_evidence_observation
		ON quota_arbitration_evidence(observation_id, account_scope, window_kind, limit_id)`},
}

var quotaScheduleSchemaObjectsV32 = []storeschema.Object{
	{ObjectType: "table", Name: "reset_credit_snapshots", Statement: `CREATE TABLE IF NOT EXISTS reset_credit_snapshots (
		snapshot_id TEXT PRIMARY KEY CHECK (length(snapshot_id) > 0 AND length(snapshot_id) <= 512),
		request_id TEXT NOT NULL UNIQUE CHECK (length(request_id) > 0 AND length(request_id) <= 512)
			REFERENCES source_attempts(request_id) ON DELETE RESTRICT,
		account_scope TEXT NOT NULL CHECK (account_scope = 'default' OR (length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*')),
		available_count INTEGER NOT NULL CHECK (available_count BETWEEN 0 AND 1000000),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0)
	) STRICT`},
	{ObjectType: "table", Name: "reset_credits", Statement: `CREATE TABLE IF NOT EXISTS reset_credits (
		snapshot_id TEXT NOT NULL REFERENCES reset_credit_snapshots(snapshot_id) ON DELETE CASCADE,
		credit_id_hash TEXT NOT NULL CHECK (
			length(credit_id_hash) = 64 AND credit_id_hash NOT GLOB '*[^0-9a-f]*'
		),
		status TEXT NOT NULL CHECK (status IN ('available', 'redeeming', 'redeemed', 'expired', 'used', 'unknown')),
		reset_type TEXT NOT NULL CHECK (reset_type IN ('codex_rate_limits', 'unknown')),
		granted_at_ms INTEGER NOT NULL CHECK (granted_at_ms >= 0),
		expires_at_ms INTEGER CHECK (expires_at_ms IS NULL OR expires_at_ms >= granted_at_ms),
		redeemed_at_ms INTEGER CHECK (
			redeemed_at_ms IS NULL OR (
				redeemed_at_ms >= granted_at_ms
				AND (expires_at_ms IS NULL OR redeemed_at_ms <= expires_at_ms)
			)
		),
		PRIMARY KEY (snapshot_id, credit_id_hash),
		CHECK (
			(status = 'available' AND redeemed_at_ms IS NULL)
			OR status IN ('redeeming', 'redeemed', 'expired', 'used', 'unknown')
		)
	) STRICT`},
	{ObjectType: "index", Name: "idx_reset_credit_snapshots_current", Statement: `CREATE INDEX IF NOT EXISTS idx_reset_credit_snapshots_current
		ON reset_credit_snapshots(account_scope, observed_at_ms DESC, snapshot_id DESC)`},
	{ObjectType: "index", Name: "idx_reset_credits_expiry", Statement: `CREATE INDEX IF NOT EXISTS idx_reset_credits_expiry
		ON reset_credits(snapshot_id, status, expires_at_ms, credit_id_hash)`},
	{ObjectType: "table", Name: "source_refresh_schedules", Statement: `CREATE TABLE IF NOT EXISTS source_refresh_schedules (
		source_instance_id TEXT PRIMARY KEY CHECK (length(source_instance_id) > 0 AND length(source_instance_id) <= 512),
		source_type TEXT NOT NULL CHECK (length(source_type) > 0 AND length(source_type) <= 128),
		scope_key TEXT NOT NULL CHECK (length(scope_key) > 0 AND length(scope_key) <= 128),
		binding_generation INTEGER NOT NULL CHECK (binding_generation >= 0),
		next_due_at_ms INTEGER CHECK (next_due_at_ms IS NULL OR next_due_at_ms >= 0),
		reason TEXT NOT NULL CHECK (reason IN (
			'startup', 'normal_interval', 'low_remaining', 'near_reset', 'reset_grace',
			'foreground', 'wake_stale', 'manual', 'network_backoff', 'retry_after',
			'auth_required', 'schema_incompatible', 'cancelled', 'disabled', 'recovery',
			'inactive_account'
		)),
		last_manual_at_ms INTEGER CHECK (last_manual_at_ms IS NULL OR last_manual_at_ms >= 0),
		active_claim_id TEXT CHECK (active_claim_id IS NULL OR (length(active_claim_id) > 0 AND length(active_claim_id) <= 512)),
		active_trigger TEXT CHECK (active_trigger IS NULL OR active_trigger IN (
			'scheduled', 'startup', 'foreground', 'wake', 'manual', 'recovery'
		)),
		claim_started_at_ms INTEGER CHECK (claim_started_at_ms IS NULL OR claim_started_at_ms >= 0),
		claim_expires_at_ms INTEGER CHECK (claim_expires_at_ms IS NULL OR claim_expires_at_ms >= claim_started_at_ms),
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		UNIQUE (source_type, scope_key),
		CHECK ((active_claim_id IS NULL) = (active_trigger IS NULL)),
		CHECK ((active_claim_id IS NULL) = (claim_started_at_ms IS NULL)),
		CHECK ((active_claim_id IS NULL) = (claim_expires_at_ms IS NULL)),
		CHECK (reason NOT IN ('auth_required', 'schema_incompatible', 'disabled', 'inactive_account') OR next_due_at_ms IS NULL)
	) STRICT`},
	{ObjectType: "index", Name: "idx_source_refresh_schedules_due", Statement: `CREATE INDEX IF NOT EXISTS idx_source_refresh_schedules_due
		ON source_refresh_schedules(next_due_at_ms, source_instance_id, revision)`},
	{ObjectType: "index", Name: "idx_source_refresh_schedules_claim", Statement: `CREATE INDEX IF NOT EXISTS idx_source_refresh_schedules_claim
		ON source_refresh_schedules(claim_expires_at_ms, source_instance_id)`},
	{ObjectType: "table", Name: "source_refresh_claims", Statement: `CREATE TABLE IF NOT EXISTS source_refresh_claims (
		claim_id TEXT PRIMARY KEY CHECK (length(claim_id) > 0 AND length(claim_id) <= 512),
		source_instance_id TEXT NOT NULL REFERENCES source_refresh_schedules(source_instance_id) ON DELETE CASCADE,
		schedule_revision INTEGER NOT NULL CHECK (schedule_revision > 0),
		binding_generation INTEGER NOT NULL CHECK (binding_generation >= 0),
		trigger TEXT NOT NULL CHECK (trigger IN ('scheduled', 'startup', 'foreground', 'wake', 'manual', 'recovery')),
		started_at_ms INTEGER NOT NULL CHECK (started_at_ms >= 0),
		expires_at_ms INTEGER NOT NULL CHECK (expires_at_ms >= started_at_ms),
		state TEXT NOT NULL CHECK (state IN ('active', 'completed', 'abandoned')),
		finalized_at_ms INTEGER CHECK (finalized_at_ms IS NULL OR finalized_at_ms >= started_at_ms),
		CHECK ((state = 'active') = (finalized_at_ms IS NULL))
	) STRICT`},
	{ObjectType: "index", Name: "idx_source_refresh_claims_source", Statement: `CREATE INDEX IF NOT EXISTS idx_source_refresh_claims_source
		ON source_refresh_claims(source_instance_id, state, expires_at_ms, claim_id)`},
}

func sourceAttemptsSchemaObjectV32() storeschema.Object {
	for _, object := range currentRuntimeSchemaObjects() {
		if object.Name == "source_attempts" {
			return object
		}
	}
	return storeschema.Object{}
}
