package store

var bootstrapSchemaObjects = []schemaObject{
	{objectType: "table", name: "bootstrap_jobs", statement: `CREATE TABLE IF NOT EXISTS bootstrap_jobs (
		job_id TEXT PRIMARY KEY CHECK (length(job_id) > 0) REFERENCES job_runs(job_id) ON DELETE CASCADE,
		switch_id TEXT NOT NULL CHECK (length(switch_id) > 0),
		home_generation INTEGER NOT NULL CHECK (home_generation >= 0),
		home_path TEXT NOT NULL CHECK (length(home_path) > 0),
		home_device_id TEXT NOT NULL CHECK (length(home_device_id) > 0),
		home_inode INTEGER NOT NULL CHECK (home_inode >= 0),
		data_store_key TEXT NOT NULL CHECK (length(data_store_key) > 0),
		strategy TEXT NOT NULL CHECK (strategy IN ('independent_database', 'clear_and_rebuild')),
		plan_state TEXT NOT NULL CHECK (plan_state IN ('pending', 'ready')),
		plan_sha256 TEXT CHECK (plan_sha256 IS NULL OR (length(plan_sha256) = 64 AND plan_sha256 NOT GLOB '*[^0-9a-f]*')),
		phase_progress_current INTEGER NOT NULL CHECK (phase_progress_current >= 0),
		phase_progress_total INTEGER NOT NULL CHECK (phase_progress_total >= phase_progress_current),
		eta_state TEXT NOT NULL CHECK (eta_state IN ('unknown', 'known', 'complete')),
		eta_remaining_ms INTEGER CHECK (eta_remaining_ms IS NULL OR eta_remaining_ms >= 0),
		pause_reason TEXT CHECK (pause_reason IS NULL OR pause_reason IN (
			'source_unavailable', 'storage_backpressure', 'application_draining', 'user_paused'
		)),
		first_screen_ready_at_ms INTEGER CHECK (first_screen_ready_at_ms IS NULL OR first_screen_ready_at_ms >= 0),
		reconcile_pass INTEGER NOT NULL CHECK (reconcile_pass >= 0),
		reconcile_plan_at_ms INTEGER CHECK (reconcile_plan_at_ms IS NULL OR reconcile_plan_at_ms >= 0),
		full_history_ready_at_ms INTEGER CHECK (full_history_ready_at_ms IS NULL OR full_history_ready_at_ms >= 0),
		reconciled_at_ms INTEGER CHECK (reconciled_at_ms IS NULL OR reconciled_at_ms >= 0),
		reconcile_change_count INTEGER NOT NULL CHECK (reconcile_change_count >= 0),
		reconcile_issue_count INTEGER NOT NULL CHECK (reconcile_issue_count >= 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		CHECK ((plan_state = 'pending' AND plan_sha256 IS NULL) OR (plan_state = 'ready' AND plan_sha256 IS NOT NULL)),
		CHECK ((eta_state = 'known' AND eta_remaining_ms IS NOT NULL) OR (eta_state != 'known' AND eta_remaining_ms IS NULL)),
		CHECK (reconcile_plan_at_ms IS NULL OR reconcile_pass > 0),
		CHECK (reconcile_plan_at_ms IS NULL OR first_screen_ready_at_ms IS NOT NULL),
		CHECK (full_history_ready_at_ms IS NULL OR reconcile_plan_at_ms IS NOT NULL),
		CHECK (reconciled_at_ms IS NULL OR full_history_ready_at_ms IS NOT NULL)
	) STRICT`},
	{objectType: "table", name: "bootstrap_plan_items", statement: `CREATE TABLE IF NOT EXISTS bootstrap_plan_items (
		job_id TEXT NOT NULL CHECK (length(job_id) > 0) REFERENCES bootstrap_jobs(job_id) ON DELETE CASCADE,
		ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
		pass INTEGER NOT NULL CHECK (pass >= 0),
		lane TEXT NOT NULL CHECK (lane IN ('fast', 'backfill', 'reconcile')),
		tier TEXT NOT NULL CHECK (tier IN ('active_append', 'today', 'recent_7d', 'recent_30d', 'older', 'reconcile')),
		action_kind TEXT NOT NULL CHECK (action_kind IN ('added', 'unchanged', 'grown', 'truncated', 'moved', 'replaced', 'deleted', 'unreadable')),
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
		current_source_file_id TEXT,
		current_source_kind TEXT,
		current_path TEXT,
		current_device_id TEXT,
		current_inode INTEGER,
		current_size_bytes INTEGER,
		current_mtime_ns INTEGER,
		current_prefix_bytes INTEGER,
		current_prefix_sha256 TEXT,
		current_fingerprint_sha256 TEXT,
		state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'drifted', 'failed')),
		source_generation INTEGER CHECK (source_generation IS NULL OR source_generation >= 0),
		progress_current INTEGER NOT NULL CHECK (progress_current >= 0),
		progress_total INTEGER NOT NULL CHECK (progress_total >= progress_current),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		PRIMARY KEY (job_id, ordinal),
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
			(current_source_file_id IS NULL AND current_source_kind IS NULL AND current_path IS NULL
				AND current_device_id IS NULL AND current_inode IS NULL AND current_size_bytes IS NULL
				AND current_mtime_ns IS NULL AND current_prefix_bytes IS NULL
				AND current_prefix_sha256 IS NULL AND current_fingerprint_sha256 IS NULL)
			OR
			(current_source_file_id IS NOT NULL AND length(current_source_file_id) > 0
				AND current_source_kind IN ('session', 'archived_session') AND length(current_path) > 0
				AND length(current_device_id) > 0 AND current_inode >= 0 AND current_size_bytes >= 0
				AND current_mtime_ns >= 0 AND current_prefix_bytes >= 0
				AND current_prefix_bytes <= current_size_bytes AND current_prefix_bytes <= 4096
				AND length(current_prefix_sha256) = 64 AND current_prefix_sha256 NOT GLOB '*[^0-9a-f]*'
				AND length(current_fingerprint_sha256) = 64 AND current_fingerprint_sha256 NOT GLOB '*[^0-9a-f]*')
		),
		CHECK (
			(action_kind = 'added' AND previous_source_file_id IS NULL AND current_source_file_id IS NOT NULL)
			OR (action_kind IN ('deleted', 'unreadable') AND previous_source_file_id IS NOT NULL AND current_source_file_id IS NULL)
			OR (action_kind IN ('unchanged', 'grown', 'truncated', 'moved', 'replaced')
				AND previous_source_file_id IS NOT NULL AND current_source_file_id IS NOT NULL)
		),
		CHECK ((lane = 'reconcile') = (tier = 'reconcile'))
	) STRICT`},
	{objectType: "index", name: "idx_bootstrap_jobs_generation_status", statement: `CREATE INDEX IF NOT EXISTS idx_bootstrap_jobs_generation_status
		ON bootstrap_jobs(switch_id, home_generation, updated_at_ms DESC, job_id DESC)`},
	{objectType: "index", name: "idx_bootstrap_plan_items_pending", statement: `CREATE INDEX IF NOT EXISTS idx_bootstrap_plan_items_pending
		ON bootstrap_plan_items(job_id, lane, state, pass, ordinal)`},
}
