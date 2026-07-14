package store

var runtimeSchemaObjects = []schemaObject{
	{objectType: "table", name: "source_files", statement: `CREATE TABLE IF NOT EXISTS source_files (
		source_file_id TEXT PRIMARY KEY CHECK (length(source_file_id) > 0),
		provider TEXT NOT NULL CHECK (length(provider) > 0),
		session_id TEXT CHECK (session_id IS NULL OR length(session_id) > 0) REFERENCES sessions(session_id) ON DELETE SET NULL,
		current_path TEXT NOT NULL CHECK (length(current_path) > 0),
		device_id TEXT NOT NULL CHECK (length(device_id) > 0),
		inode INTEGER NOT NULL CHECK (inode >= 0),
		size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
		mtime_ns INTEGER NOT NULL CHECK (mtime_ns >= 0),
		parsed_offset INTEGER NOT NULL CHECK (parsed_offset >= 0 AND parsed_offset <= size_bytes),
		parser_version TEXT NOT NULL CHECK (length(parser_version) > 0),
		active_generation INTEGER NOT NULL CHECK (active_generation >= 0),
		state TEXT NOT NULL CHECK (state IN ('discovered', 'active', 'completed', 'unavailable', 'failed')),
		last_scanned_at_ms INTEGER CHECK (last_scanned_at_ms IS NULL OR last_scanned_at_ms >= 0),
		last_error_class TEXT CHECK (last_error_class IS NULL OR last_error_class IN (
			'canceled', 'busy', 'disk_full', 'read_only', 'permission', 'io',
			'corrupt', 'timeout', 'unavailable', 'invalid_input', 'unknown'
		)),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		UNIQUE (provider, device_id, inode)
	) STRICT`},
	{objectType: "table", name: "source_state", statement: `CREATE TABLE IF NOT EXISTS source_state (
		source_instance_id TEXT PRIMARY KEY CHECK (length(source_instance_id) > 0),
		source_type TEXT NOT NULL CHECK (length(source_type) > 0),
		scope_key TEXT NOT NULL CHECK (length(scope_key) > 0),
		last_attempt_at_ms INTEGER CHECK (last_attempt_at_ms IS NULL OR last_attempt_at_ms >= 0),
		last_success_at_ms INTEGER CHECK (last_success_at_ms IS NULL OR last_success_at_ms >= 0),
		next_due_at_ms INTEGER CHECK (next_due_at_ms IS NULL OR next_due_at_ms >= 0),
		consecutive_failures INTEGER NOT NULL CHECK (consecutive_failures >= 0),
		last_error_class TEXT CHECK (last_error_class IS NULL OR last_error_class IN (
			'canceled', 'busy', 'disk_full', 'read_only', 'permission', 'io',
			'corrupt', 'timeout', 'unavailable', 'invalid_input', 'unknown'
		)),
		freshness_state TEXT NOT NULL CHECK (freshness_state IN ('unknown', 'current', 'stale', 'unavailable')),
		cursor_version INTEGER NOT NULL CHECK (cursor_version >= 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		CHECK (last_success_at_ms IS NULL OR last_attempt_at_ms IS NULL OR last_success_at_ms <= last_attempt_at_ms),
		UNIQUE (source_type, scope_key)
	) STRICT`},
	{objectType: "table", name: "source_attempts", statement: `CREATE TABLE IF NOT EXISTS source_attempts (
		request_id TEXT PRIMARY KEY CHECK (length(request_id) > 0),
		source_instance_id TEXT NOT NULL CHECK (length(source_instance_id) > 0) REFERENCES source_state(source_instance_id) ON DELETE CASCADE,
		started_at_ms INTEGER NOT NULL CHECK (started_at_ms >= 0),
		finished_at_ms INTEGER NOT NULL CHECK (finished_at_ms >= started_at_ms),
		outcome TEXT NOT NULL CHECK (outcome IN ('succeeded', 'failed', 'cancelled')),
		http_status INTEGER CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
		error_class TEXT CHECK (error_class IS NULL OR error_class IN (
			'canceled', 'busy', 'disk_full', 'read_only', 'permission', 'io',
			'corrupt', 'timeout', 'unavailable', 'invalid_input', 'unknown'
		)),
		payload_sha256 TEXT CHECK (
			payload_sha256 IS NULL
			OR (length(payload_sha256) = 64 AND payload_sha256 NOT GLOB '*[^0-9a-f]*')
		),
		CHECK ((outcome = 'succeeded' AND error_class IS NULL) OR (outcome != 'succeeded'))
	) STRICT`},
	{objectType: "table", name: "job_runs", statement: `CREATE TABLE IF NOT EXISTS job_runs (
		job_id TEXT PRIMARY KEY CHECK (length(job_id) > 0),
		job_type TEXT NOT NULL CHECK (length(job_type) > 0),
		requested_by TEXT NOT NULL CHECK (length(requested_by) > 0),
		priority INTEGER NOT NULL CHECK (priority >= 0),
		state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'interrupted')),
		phase TEXT NOT NULL CHECK (phase IN ('discover', 'fast_bootstrap', 'history_backfill', 'reconcile', 'live', 'maintenance')),
		source_file_id TEXT CHECK (source_file_id IS NULL OR length(source_file_id) > 0) REFERENCES source_files(source_file_id) ON DELETE SET NULL,
		resume_of_job_id TEXT CHECK (resume_of_job_id IS NULL OR length(resume_of_job_id) > 0) REFERENCES job_runs(job_id) ON DELETE SET NULL,
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		started_at_ms INTEGER CHECK (started_at_ms IS NULL OR started_at_ms >= created_at_ms),
		finished_at_ms INTEGER CHECK (
			finished_at_ms IS NULL
			OR (
				finished_at_ms >= created_at_ms
				AND (started_at_ms IS NULL OR finished_at_ms >= started_at_ms)
			)
		),
		progress_current INTEGER CHECK (progress_current IS NULL OR progress_current >= 0),
		progress_total INTEGER CHECK (progress_total IS NULL OR progress_total >= 0),
		resume_generation INTEGER CHECK (resume_generation IS NULL OR resume_generation >= 0),
		resume_offset INTEGER CHECK (resume_offset IS NULL OR resume_offset >= 0),
		error_class TEXT CHECK (error_class IS NULL OR error_class IN (
			'canceled', 'busy', 'disk_full', 'read_only', 'permission', 'io',
			'corrupt', 'timeout', 'unavailable', 'invalid_input', 'unknown'
		)),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= created_at_ms),
		CHECK (progress_current IS NULL OR progress_total IS NULL OR progress_current <= progress_total),
		CHECK ((resume_generation IS NULL) = (resume_offset IS NULL)),
		CHECK (
			(state = 'queued' AND started_at_ms IS NULL AND finished_at_ms IS NULL AND error_class IS NULL)
			OR (state = 'running' AND started_at_ms IS NOT NULL AND finished_at_ms IS NULL AND error_class IS NULL)
			OR (state = 'succeeded' AND started_at_ms IS NOT NULL AND finished_at_ms IS NOT NULL AND error_class IS NULL)
			OR (state = 'failed' AND started_at_ms IS NOT NULL AND finished_at_ms IS NOT NULL AND error_class IS NOT NULL)
			OR (state IN ('cancelled', 'interrupted') AND finished_at_ms IS NOT NULL)
		),
		CHECK (resume_of_job_id IS NULL OR resume_of_job_id != job_id)
	) STRICT`},
	{objectType: "table", name: "health_events", statement: `CREATE TABLE IF NOT EXISTS health_events (
		event_id TEXT PRIMARY KEY CHECK (length(event_id) > 0),
		fingerprint TEXT NOT NULL UNIQUE CHECK (
			length(fingerprint) = 64 AND fingerprint NOT GLOB '*[^0-9a-f]*'
		),
		domain TEXT NOT NULL CHECK (domain IN ('source', 'job', 'store', 'pricing', 'runtime')),
		severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'error', 'critical')),
		code TEXT NOT NULL,
		source_file_id TEXT CHECK (source_file_id IS NULL OR length(source_file_id) > 0) REFERENCES source_files(source_file_id) ON DELETE SET NULL,
		job_id TEXT CHECK (job_id IS NULL OR length(job_id) > 0) REFERENCES job_runs(job_id) ON DELETE SET NULL,
		error_class TEXT CHECK (error_class IS NULL OR error_class IN (
			'canceled', 'busy', 'disk_full', 'read_only', 'permission', 'io',
			'corrupt', 'timeout', 'unavailable', 'invalid_input', 'unknown'
		)),
		first_seen_at_ms INTEGER NOT NULL CHECK (first_seen_at_ms >= 0),
		last_seen_at_ms INTEGER NOT NULL CHECK (last_seen_at_ms >= first_seen_at_ms),
		resolved_at_ms INTEGER CHECK (resolved_at_ms IS NULL OR resolved_at_ms >= last_seen_at_ms),
		occurrence_count INTEGER NOT NULL CHECK (occurrence_count > 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= last_seen_at_ms),
		CHECK (
			(domain = 'source' AND code IN (
				'source.timeout', 'source.unavailable', 'source.permission', 'source.corrupt', 'source.stale'
			))
			OR (domain = 'job' AND code IN ('job.interrupted', 'job.failed', 'job.cancelled'))
			OR (domain = 'store' AND code IN (
				'store.busy', 'store.disk_full', 'store.read_only', 'store.permission',
				'store.io', 'store.corrupt', 'store.unavailable', 'store.unknown'
			))
			OR (domain = 'pricing' AND code IN ('pricing.unavailable', 'pricing.invalid'))
			OR (domain = 'runtime' AND code = 'runtime.unknown')
		)
	) STRICT`},
	{objectType: "table", name: "pricing_versions", statement: `CREATE TABLE IF NOT EXISTS pricing_versions (
		pricing_version TEXT PRIMARY KEY CHECK (length(pricing_version) > 0),
		source TEXT NOT NULL CHECK (length(source) > 0),
		currency TEXT NOT NULL CHECK (length(currency) > 0),
		effective_from_ms INTEGER NOT NULL CHECK (effective_from_ms >= 0),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		UNIQUE (source, currency, effective_from_ms)
	) STRICT`},
	{objectType: "table", name: "model_prices", statement: `CREATE TABLE IF NOT EXISTS model_prices (
		pricing_version TEXT NOT NULL CHECK (length(pricing_version) > 0) REFERENCES pricing_versions(pricing_version) ON DELETE CASCADE,
		match_kind TEXT NOT NULL CHECK (match_kind IN ('exact', 'prefix', 'default')),
		model_pattern TEXT NOT NULL CHECK (length(model_pattern) > 0),
		priority INTEGER NOT NULL CHECK (priority >= 0),
		input_micros_per_million INTEGER CHECK (input_micros_per_million IS NULL OR input_micros_per_million >= 0),
		cached_input_micros_per_million INTEGER CHECK (cached_input_micros_per_million IS NULL OR cached_input_micros_per_million >= 0),
		output_micros_per_million INTEGER CHECK (output_micros_per_million IS NULL OR output_micros_per_million >= 0),
		PRIMARY KEY (pricing_version, match_kind, model_pattern),
		CHECK (match_kind != 'default' OR model_pattern = '*'),
		CHECK (
			input_micros_per_million IS NOT NULL
			OR cached_input_micros_per_million IS NOT NULL
			OR output_micros_per_million IS NOT NULL
		)
	) STRICT`},
	{objectType: "index", name: "idx_source_files_session_state", statement: `CREATE INDEX IF NOT EXISTS idx_source_files_session_state
		ON source_files(session_id, state, last_scanned_at_ms, source_file_id)`},
	{objectType: "index", name: "idx_source_state_due", statement: `CREATE INDEX IF NOT EXISTS idx_source_state_due
		ON source_state(next_due_at_ms, source_instance_id)`},
	{objectType: "index", name: "idx_source_attempts_history", statement: `CREATE INDEX IF NOT EXISTS idx_source_attempts_history
		ON source_attempts(source_instance_id, started_at_ms DESC, request_id DESC)`},
	{objectType: "index", name: "idx_job_runs_state_queue", statement: `CREATE INDEX IF NOT EXISTS idx_job_runs_state_queue
		ON job_runs(state, updated_at_ms, priority DESC, job_id)`},
	{objectType: "index", name: "idx_job_runs_source_history", statement: `CREATE INDEX IF NOT EXISTS idx_job_runs_source_history
		ON job_runs(source_file_id, created_at_ms DESC, job_id DESC)`},
	{objectType: "index", name: "idx_job_runs_all", statement: `CREATE INDEX IF NOT EXISTS idx_job_runs_all
		ON job_runs(updated_at_ms, priority DESC, job_id)`},
	{objectType: "index", name: "idx_health_events_active", statement: `CREATE INDEX IF NOT EXISTS idx_health_events_active
		ON health_events(resolved_at_ms, last_seen_at_ms DESC, event_id, severity)`},
	{objectType: "index", name: "idx_health_events_history", statement: `CREATE INDEX IF NOT EXISTS idx_health_events_history
		ON health_events(last_seen_at_ms DESC, event_id)`},
	{objectType: "index", name: "idx_health_events_severity", statement: `CREATE INDEX IF NOT EXISTS idx_health_events_severity
		ON health_events(severity, last_seen_at_ms DESC, event_id)`},
	{objectType: "index", name: "idx_health_events_relation", statement: `CREATE INDEX IF NOT EXISTS idx_health_events_relation
		ON health_events(source_file_id, last_seen_at_ms DESC, event_id)`},
	{objectType: "index", name: "idx_health_events_job", statement: `CREATE INDEX IF NOT EXISTS idx_health_events_job
		ON health_events(job_id, last_seen_at_ms DESC, event_id)`},
	{objectType: "index", name: "idx_pricing_versions_effective", statement: `CREATE INDEX IF NOT EXISTS idx_pricing_versions_effective
		ON pricing_versions(source, currency, effective_from_ms DESC)`},
	{objectType: "index", name: "idx_model_prices_match", statement: `CREATE INDEX IF NOT EXISTS idx_model_prices_match
		ON model_prices(pricing_version, priority DESC, match_kind, model_pattern)`},
}
