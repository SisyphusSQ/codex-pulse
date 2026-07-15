package store

var lifecycleSchemaObjects = []schemaObject{
	{objectType: "table", name: "scheduler_lifecycle", statement: `CREATE TABLE IF NOT EXISTS scheduler_lifecycle (
		control_key INTEGER PRIMARY KEY CHECK (control_key = 1),
		home_generation INTEGER NOT NULL CHECK (home_generation >= 0),
		user_pause_scope TEXT NOT NULL CHECK (user_pause_scope IN ('none', 'backfill', 'all')),
		system_state TEXT NOT NULL CHECK (system_state IN ('awake', 'sleeping')),
		transition TEXT NOT NULL CHECK (transition IN ('steady', 'draining', 'reconciling', 'blocked')),
		source_state TEXT NOT NULL CHECK (source_state IN ('unknown', 'available', 'unavailable')),
		last_event_id TEXT NOT NULL CHECK (length(last_event_id) > 0 AND length(last_event_id) <= 256),
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`},
	{objectType: "table", name: "scheduler_retry_states", statement: `CREATE TABLE IF NOT EXISTS scheduler_retry_states (
		task_id TEXT PRIMARY KEY CHECK (length(task_id) > 0) REFERENCES scheduler_tasks(task_id) ON DELETE CASCADE,
		disposition TEXT NOT NULL CHECK (disposition IN ('waiting', 'blocked', 'resolved')),
		failure_count INTEGER NOT NULL CHECK (failure_count > 0),
		last_error_class TEXT NOT NULL CHECK (last_error_class IN (
			'canceled', 'busy', 'disk_full', 'read_only', 'permission', 'io',
			'corrupt', 'timeout', 'unavailable', 'invalid_input', 'unknown'
		)),
		next_retry_at_ms INTEGER CHECK (next_retry_at_ms IS NULL OR next_retry_at_ms >= 0),
		recovery_action TEXT NOT NULL CHECK (recovery_action IN (
			'none', 'retry', 'check_source', 'grant_permission', 'free_space', 'choose_home', 'repair_store'
		)),
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		CHECK (
			(disposition = 'waiting' AND next_retry_at_ms IS NOT NULL AND recovery_action = 'none')
			OR (disposition = 'blocked' AND next_retry_at_ms IS NULL AND recovery_action != 'none')
			OR (disposition = 'resolved' AND next_retry_at_ms IS NULL AND recovery_action = 'none')
		)
	) STRICT`},
	{objectType: "index", name: "idx_scheduler_retry_due", statement: `CREATE INDEX IF NOT EXISTS idx_scheduler_retry_due
		ON scheduler_retry_states(disposition, next_retry_at_ms, task_id)`},
}
