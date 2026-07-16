package store

// quotaScheduleSchemaObjects is the v12 Reset Credits and refresh scheduling
// schema. STRICT/CHECK/composite FK definitions are isolated here because GORM
// Migrator cannot express their full SQLite contract.
var quotaScheduleSchemaObjects = []schemaObject{
	{objectType: "table", name: "reset_credit_snapshots", statement: `CREATE TABLE IF NOT EXISTS reset_credit_snapshots (
		snapshot_id TEXT PRIMARY KEY CHECK (length(snapshot_id) > 0 AND length(snapshot_id) <= 512),
		request_id TEXT NOT NULL UNIQUE CHECK (length(request_id) > 0 AND length(request_id) <= 512)
			REFERENCES source_attempts(request_id) ON DELETE RESTRICT,
		account_scope TEXT NOT NULL CHECK (account_scope = 'default'),
		available_count INTEGER NOT NULL CHECK (available_count BETWEEN 0 AND 100),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0)
	) STRICT`},
	{objectType: "table", name: "reset_credits", statement: `CREATE TABLE IF NOT EXISTS reset_credits (
		snapshot_id TEXT NOT NULL REFERENCES reset_credit_snapshots(snapshot_id) ON DELETE CASCADE,
		credit_id_hash TEXT NOT NULL CHECK (
			length(credit_id_hash) = 64 AND credit_id_hash NOT GLOB '*[^0-9a-f]*'
		),
		status TEXT NOT NULL CHECK (status IN ('available', 'redeemed', 'expired', 'used')),
		reset_type TEXT NOT NULL CHECK (reset_type IN ('codex_rate_limits', 'unknown')),
		granted_at_ms INTEGER NOT NULL CHECK (granted_at_ms >= 0),
		expires_at_ms INTEGER NOT NULL CHECK (expires_at_ms >= granted_at_ms),
		redeemed_at_ms INTEGER CHECK (
			redeemed_at_ms IS NULL OR (redeemed_at_ms >= granted_at_ms AND redeemed_at_ms <= expires_at_ms)
		),
		PRIMARY KEY (snapshot_id, credit_id_hash),
		CHECK (
			(status = 'available' AND redeemed_at_ms IS NULL)
			OR (status IN ('redeemed', 'used') AND redeemed_at_ms IS NOT NULL)
			OR status = 'expired'
		)
	) STRICT`},
	{objectType: "index", name: "idx_reset_credit_snapshots_current", statement: `CREATE INDEX IF NOT EXISTS idx_reset_credit_snapshots_current
		ON reset_credit_snapshots(account_scope, observed_at_ms DESC, snapshot_id DESC)`},
	{objectType: "index", name: "idx_reset_credits_expiry", statement: `CREATE INDEX IF NOT EXISTS idx_reset_credits_expiry
		ON reset_credits(snapshot_id, status, expires_at_ms, credit_id_hash)`},
	{objectType: "table", name: "source_refresh_schedules", statement: `CREATE TABLE IF NOT EXISTS source_refresh_schedules (
		source_instance_id TEXT PRIMARY KEY CHECK (length(source_instance_id) > 0 AND length(source_instance_id) <= 512),
		source_type TEXT NOT NULL CHECK (length(source_type) > 0 AND length(source_type) <= 128),
		scope_key TEXT NOT NULL CHECK (length(scope_key) > 0 AND length(scope_key) <= 128),
		next_due_at_ms INTEGER CHECK (next_due_at_ms IS NULL OR next_due_at_ms >= 0),
		reason TEXT NOT NULL CHECK (reason IN (
			'startup', 'normal_interval', 'low_remaining', 'near_reset', 'reset_grace',
			'foreground', 'wake_stale', 'manual', 'network_backoff', 'retry_after',
			'auth_required', 'schema_incompatible', 'cancelled', 'disabled', 'recovery'
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
		CHECK (reason NOT IN ('auth_required', 'schema_incompatible', 'disabled') OR next_due_at_ms IS NULL)
	) STRICT`},
	{objectType: "index", name: "idx_source_refresh_schedules_due", statement: `CREATE INDEX IF NOT EXISTS idx_source_refresh_schedules_due
		ON source_refresh_schedules(next_due_at_ms, source_instance_id, revision)`},
	{objectType: "index", name: "idx_source_refresh_schedules_claim", statement: `CREATE INDEX IF NOT EXISTS idx_source_refresh_schedules_claim
		ON source_refresh_schedules(claim_expires_at_ms, source_instance_id)`},
	{objectType: "table", name: "source_refresh_claims", statement: `CREATE TABLE IF NOT EXISTS source_refresh_claims (
		claim_id TEXT PRIMARY KEY CHECK (length(claim_id) > 0 AND length(claim_id) <= 512),
		source_instance_id TEXT NOT NULL REFERENCES source_refresh_schedules(source_instance_id) ON DELETE CASCADE,
		schedule_revision INTEGER NOT NULL CHECK (schedule_revision > 0),
		trigger TEXT NOT NULL CHECK (trigger IN ('scheduled', 'startup', 'foreground', 'wake', 'manual', 'recovery')),
		started_at_ms INTEGER NOT NULL CHECK (started_at_ms >= 0),
		expires_at_ms INTEGER NOT NULL CHECK (expires_at_ms >= started_at_ms),
		state TEXT NOT NULL CHECK (state IN ('active', 'completed', 'abandoned')),
		finalized_at_ms INTEGER CHECK (finalized_at_ms IS NULL OR finalized_at_ms >= started_at_ms),
		CHECK ((state = 'active') = (finalized_at_ms IS NULL))
	) STRICT`},
	{objectType: "index", name: "idx_source_refresh_claims_source", statement: `CREATE INDEX IF NOT EXISTS idx_source_refresh_claims_source
		ON source_refresh_claims(source_instance_id, state, expires_at_ms, claim_id)`},
}
