package store

var schedulerSchemaObjects = []schemaObject{
	{objectType: "table", name: "live_scan_jobs", statement: `CREATE TABLE IF NOT EXISTS live_scan_jobs (
		job_id TEXT PRIMARY KEY CHECK (length(job_id) > 0) REFERENCES job_runs(job_id) ON DELETE CASCADE,
		request_id TEXT NOT NULL UNIQUE CHECK (length(request_id) > 0),
		home_generation INTEGER NOT NULL CHECK (home_generation >= 0),
		home_path TEXT NOT NULL CHECK (length(home_path) > 0),
		home_device_id TEXT NOT NULL CHECK (length(home_device_id) > 0),
		home_inode INTEGER NOT NULL CHECK (home_inode > 0),
		action_kind TEXT NOT NULL CHECK (action_kind IN ('added', 'unchanged', 'grown', 'truncated', 'moved', 'replaced')),
		previous_source_file_id TEXT,
		previous_source_kind TEXT,
		previous_path TEXT,
		previous_device_id TEXT,
		previous_inode INTEGER,
		previous_size_bytes INTEGER,
		previous_mtime_ns INTEGER,
		previous_prefix_bytes INTEGER,
		previous_prefix_sha256 TEXT,
		previous_fingerprint_sha256 TEXT,
		current_source_file_id TEXT NOT NULL CHECK (length(current_source_file_id) > 0),
		current_source_kind TEXT NOT NULL CHECK (current_source_kind IN ('session', 'archived_session')),
		current_path TEXT NOT NULL CHECK (length(current_path) > 0),
		current_device_id TEXT NOT NULL CHECK (length(current_device_id) > 0),
		current_inode INTEGER NOT NULL CHECK (current_inode >= 0),
		current_size_bytes INTEGER NOT NULL CHECK (current_size_bytes >= 0),
		current_mtime_ns INTEGER NOT NULL CHECK (current_mtime_ns >= 0),
		current_prefix_bytes INTEGER NOT NULL CHECK (current_prefix_bytes >= 0 AND current_prefix_bytes <= current_size_bytes AND current_prefix_bytes <= 4096),
		current_prefix_sha256 TEXT NOT NULL CHECK (length(current_prefix_sha256) = 64 AND current_prefix_sha256 NOT GLOB '*[^0-9a-f]*'),
		current_fingerprint_sha256 TEXT NOT NULL CHECK (length(current_fingerprint_sha256) = 64 AND current_fingerprint_sha256 NOT GLOB '*[^0-9a-f]*'),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		CHECK (
			(previous_source_file_id IS NULL AND previous_source_kind IS NULL AND previous_path IS NULL
				AND previous_device_id IS NULL AND previous_inode IS NULL AND previous_size_bytes IS NULL
				AND previous_mtime_ns IS NULL AND previous_prefix_bytes IS NULL
				AND previous_prefix_sha256 IS NULL AND previous_fingerprint_sha256 IS NULL)
			OR
			(previous_source_file_id IS NOT NULL AND length(previous_source_file_id) > 0
				AND previous_source_kind IN ('session', 'archived_session') AND length(previous_path) > 0
				AND length(previous_device_id) > 0 AND previous_inode >= 0 AND previous_size_bytes >= 0
				AND previous_mtime_ns >= 0 AND previous_prefix_bytes >= 0
				AND previous_prefix_bytes <= previous_size_bytes AND previous_prefix_bytes <= 4096
				AND length(previous_prefix_sha256) = 64 AND previous_prefix_sha256 NOT GLOB '*[^0-9a-f]*'
				AND length(previous_fingerprint_sha256) = 64 AND previous_fingerprint_sha256 NOT GLOB '*[^0-9a-f]*')
		),
		CHECK (
			(action_kind = 'added' AND previous_source_file_id IS NULL)
			OR (action_kind != 'added' AND previous_source_file_id IS NOT NULL)
		)
	) STRICT`},
	{objectType: "table", name: "scheduler_tasks", statement: `CREATE TABLE IF NOT EXISTS scheduler_tasks (
		task_id TEXT PRIMARY KEY CHECK (length(task_id) > 0),
		dedupe_key TEXT NOT NULL UNIQUE CHECK (length(dedupe_key) > 0),
		target_kind TEXT NOT NULL CHECK (target_kind IN ('bootstrap', 'live_scan')),
		admission_target_id TEXT NOT NULL CHECK (length(admission_target_id) > 0),
		target_id TEXT NOT NULL UNIQUE CHECK (length(target_id) > 0) REFERENCES job_runs(job_id) ON DELETE CASCADE,
		home_generation INTEGER NOT NULL CHECK (home_generation >= 0),
		lane TEXT NOT NULL CHECK (lane IN ('live', 'backfill')),
		admission_service_class TEXT NOT NULL CHECK (admission_service_class IN ('background', 'interactive')),
		service_class TEXT NOT NULL CHECK (service_class IN ('background', 'interactive')),
		state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'interrupted')),
		queue_order_ms INTEGER NOT NULL CHECK (queue_order_ms >= 0),
		enqueued_at_ms INTEGER NOT NULL CHECK (enqueued_at_ms >= 0),
		first_started_at_ms INTEGER CHECK (first_started_at_ms IS NULL OR first_started_at_ms >= enqueued_at_ms),
		last_started_at_ms INTEGER CHECK (last_started_at_ms IS NULL OR last_started_at_ms >= enqueued_at_ms),
		finished_at_ms INTEGER CHECK (finished_at_ms IS NULL OR finished_at_ms >= enqueued_at_ms),
		files_processed INTEGER NOT NULL CHECK (files_processed >= 0),
		bytes_processed INTEGER NOT NULL CHECK (bytes_processed >= 0),
		slice_count INTEGER NOT NULL CHECK (slice_count >= 0),
		last_error_class TEXT CHECK (last_error_class IS NULL OR last_error_class IN (
			'canceled', 'busy', 'disk_full', 'read_only', 'permission', 'io',
			'corrupt', 'timeout', 'unavailable', 'invalid_input', 'unknown'
		)),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= enqueued_at_ms),
		CHECK ((first_started_at_ms IS NULL) = (last_started_at_ms IS NULL)),
		CHECK (last_started_at_ms IS NULL OR first_started_at_ms <= last_started_at_ms),
		CHECK (finished_at_ms IS NULL OR last_started_at_ms IS NULL OR finished_at_ms >= last_started_at_ms),
		CHECK (
			(state = 'queued' AND finished_at_ms IS NULL AND last_error_class IS NULL)
			OR (state = 'running' AND last_started_at_ms IS NOT NULL AND finished_at_ms IS NULL AND last_error_class IS NULL)
			OR (state = 'succeeded' AND last_started_at_ms IS NOT NULL AND finished_at_ms IS NOT NULL AND last_error_class IS NULL)
			OR (state = 'failed' AND last_started_at_ms IS NOT NULL AND finished_at_ms IS NOT NULL AND last_error_class IS NOT NULL)
			OR (state = 'interrupted' AND last_started_at_ms IS NOT NULL AND finished_at_ms IS NOT NULL)
		)
	) STRICT`},
	{objectType: "table", name: "scheduler_cycles", statement: `CREATE TABLE IF NOT EXISTS scheduler_cycles (
		commit_order INTEGER PRIMARY KEY AUTOINCREMENT,
		cycle_id TEXT NOT NULL UNIQUE CHECK (length(cycle_id) > 0),
		task_id TEXT NOT NULL CHECK (length(task_id) > 0) REFERENCES scheduler_tasks(task_id) ON DELETE CASCADE,
		lane TEXT NOT NULL CHECK (lane IN ('live', 'backfill')),
		selection_reason TEXT NOT NULL CHECK (selection_reason IN ('live_priority', 'live_only', 'backfill_only', 'backfill_fairness')),
		stop_reason TEXT NOT NULL CHECK (stop_reason IN (
			'completed', 'file_budget', 'byte_budget', 'time_budget', 'system_pressure',
			'live_preempted', 'cancelled', 'dependency_error', 'worker_panic'
		)),
		outcome TEXT NOT NULL CHECK (outcome IN ('completed', 'yielded', 'failed', 'interrupted')),
		budget_files INTEGER NOT NULL CHECK (budget_files >= 0),
		budget_bytes INTEGER NOT NULL CHECK (budget_bytes >= 0),
		budget_active_ms INTEGER NOT NULL CHECK (budget_active_ms >= 0),
		consumed_files INTEGER NOT NULL CHECK (consumed_files >= 0),
		consumed_bytes INTEGER NOT NULL CHECK (consumed_bytes >= 0),
		active_ms INTEGER NOT NULL CHECK (active_ms >= 0),
		live_depth INTEGER NOT NULL CHECK (live_depth >= 0),
		backfill_depth INTEGER NOT NULL CHECK (backfill_depth >= 0),
		oldest_live_wait_ms INTEGER NOT NULL CHECK (oldest_live_wait_ms >= 0),
		oldest_backfill_wait_ms INTEGER NOT NULL CHECK (oldest_backfill_wait_ms >= 0),
		started_at_ms INTEGER NOT NULL CHECK (started_at_ms >= 0),
		finished_at_ms INTEGER NOT NULL CHECK (finished_at_ms >= started_at_ms),
		CHECK (consumed_files <= budget_files),
		CHECK (consumed_bytes <= budget_bytes),
		CHECK (
			(outcome = 'completed' AND stop_reason = 'completed')
			OR (outcome = 'yielded' AND stop_reason IN ('file_budget', 'byte_budget', 'time_budget', 'system_pressure', 'live_preempted'))
			OR (outcome = 'failed' AND stop_reason = 'dependency_error')
			OR (outcome = 'interrupted' AND stop_reason IN ('cancelled', 'worker_panic'))
		)
	) STRICT`},
	{objectType: "index", name: "idx_live_scan_jobs_generation", statement: `CREATE INDEX IF NOT EXISTS idx_live_scan_jobs_generation
		ON live_scan_jobs(home_generation, updated_at_ms DESC, job_id DESC)`},
	{objectType: "index", name: "idx_scheduler_tasks_active", statement: `CREATE INDEX IF NOT EXISTS idx_scheduler_tasks_active
		ON scheduler_tasks(state, lane, queue_order_ms, task_id)`},
	{objectType: "index", name: "idx_scheduler_tasks_generation", statement: `CREATE INDEX IF NOT EXISTS idx_scheduler_tasks_generation
		ON scheduler_tasks(home_generation, state, updated_at_ms DESC, task_id DESC)`},
	{objectType: "index", name: "idx_scheduler_cycles_recent", statement: `CREATE INDEX IF NOT EXISTS idx_scheduler_cycles_recent
		ON scheduler_cycles(commit_order DESC)`},
	{objectType: "index", name: "idx_scheduler_cycles_task", statement: `CREATE INDEX IF NOT EXISTS idx_scheduler_cycles_task
		ON scheduler_cycles(task_id, commit_order DESC)`},
}
