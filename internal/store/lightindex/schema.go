package lightindex

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

var lightIndexSchemaObjects = []storeschema.Object{
	{
		ObjectType: "table",
		Name:       "light_index_state",
		Statement: `CREATE TABLE IF NOT EXISTS light_index_state (
			state_id INTEGER PRIMARY KEY CHECK (state_id = 1),
			home_path TEXT NOT NULL CHECK (length(home_path) > 0),
			home_device_id TEXT NOT NULL CHECK (length(home_device_id) > 0),
			home_inode INTEGER NOT NULL CHECK (home_inode > 0),
			metadata_generation INTEGER NOT NULL DEFAULT 0 CHECK (metadata_generation >= 0),
			metadata_ready_at_ms INTEGER CHECK (metadata_ready_at_ms IS NULL OR metadata_ready_at_ms >= 0),
			token_scan_generation INTEGER NOT NULL DEFAULT 0 CHECK (token_scan_generation >= 0),
			token_scan_state TEXT NOT NULL DEFAULT 'idle'
				CHECK (token_scan_state IN ('idle','running','partial','complete','cancelled','failed')),
			token_scan_started_at_ms INTEGER CHECK (token_scan_started_at_ms IS NULL OR token_scan_started_at_ms >= 0),
			token_scan_finished_at_ms INTEGER CHECK (token_scan_finished_at_ms IS NULL OR token_scan_finished_at_ms >= 0),
			updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
		) STRICT`,
	},
	{
		ObjectType: "table",
		Name:       "light_sessions",
		Statement: `CREATE TABLE IF NOT EXISTS light_sessions (
			session_id TEXT PRIMARY KEY CHECK (length(session_id) > 0),
			thread_name TEXT CHECK (thread_name IS NULL OR length(thread_name) > 0),
			cwd TEXT NOT NULL,
			rollout_path TEXT,
			created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
			updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0),
			recency_at_ms INTEGER CHECK (recency_at_ms IS NULL OR recency_at_ms > 0),
			metadata_generation INTEGER NOT NULL CHECK (metadata_generation > 0),
			active_token_generation INTEGER NOT NULL DEFAULT 0 CHECK (active_token_generation >= 0),
			pending_token_generation INTEGER CHECK (pending_token_generation IS NULL OR pending_token_generation > 0),
			scan_state TEXT NOT NULL DEFAULT 'pending'
				CHECK (scan_state IN ('pending','scanning','partial','complete','deferred','failed','cancelled')),
			row_updated_at_ms INTEGER NOT NULL CHECK (row_updated_at_ms >= 0)
		) STRICT`,
	},
	{
		ObjectType: "table",
		Name:       "light_token_scans",
		Statement: `CREATE TABLE IF NOT EXISTS light_token_scans (
			session_id TEXT NOT NULL,
			generation INTEGER NOT NULL CHECK (generation > 0),
			rollout_path TEXT NOT NULL CHECK (length(rollout_path) > 0),
			source_file_id TEXT NOT NULL CHECK (length(source_file_id) > 0),
			home_path TEXT NOT NULL CHECK (length(home_path) > 0),
			home_device_id TEXT NOT NULL CHECK (length(home_device_id) > 0),
			home_inode INTEGER NOT NULL CHECK (home_inode > 0),
			file_device_id TEXT NOT NULL CHECK (length(file_device_id) > 0),
			file_inode INTEGER NOT NULL CHECK (file_inode > 0),
			file_size_bytes INTEGER NOT NULL CHECK (file_size_bytes >= 0),
			file_mtime_ns INTEGER NOT NULL,
			prefix_bytes INTEGER NOT NULL CHECK (prefix_bytes >= 0 AND prefix_bytes <= file_size_bytes),
			prefix_sha256 TEXT NOT NULL CHECK (length(prefix_sha256) = 64 AND prefix_sha256 NOT GLOB '*[^0-9a-f]*'),
			fingerprint_sha256 TEXT NOT NULL CHECK (length(fingerprint_sha256) = 64 AND fingerprint_sha256 NOT GLOB '*[^0-9a-f]*'),
			parser_version TEXT NOT NULL CHECK (length(parser_version) > 0),
			durable_offset INTEGER NOT NULL CHECK (durable_offset >= 0 AND durable_offset <= file_size_bytes),
			complete INTEGER NOT NULL CHECK (complete IN (0, 1)),
			input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
			cached_input_tokens INTEGER NOT NULL CHECK (cached_input_tokens >= 0),
			output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
			reasoning_tokens INTEGER NOT NULL CHECK (reasoning_tokens >= 0),
			latest_event_at_ms INTEGER CHECK (latest_event_at_ms IS NULL OR latest_event_at_ms >= 0),
			physical_bytes_read INTEGER NOT NULL DEFAULT 0 CHECK (physical_bytes_read >= 0),
			lines_seen INTEGER NOT NULL DEFAULT 0 CHECK (lines_seen >= 0),
			candidate_lines INTEGER NOT NULL DEFAULT 0 CHECK (candidate_lines >= 0),
			json_decoded INTEGER NOT NULL DEFAULT 0 CHECK (json_decoded >= 0),
			state TEXT NOT NULL CHECK (state IN ('building','active','failed','cancelled')),
			updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
			PRIMARY KEY (session_id, generation),
			FOREIGN KEY (session_id) REFERENCES light_sessions(session_id) ON DELETE CASCADE
		) STRICT`,
	},
	{
		ObjectType: "table",
		Name:       "light_token_daily",
		Statement: `CREATE TABLE IF NOT EXISTS light_token_daily (
			session_id TEXT NOT NULL,
			generation INTEGER NOT NULL CHECK (generation > 0),
			day_start_ms INTEGER NOT NULL CHECK (day_start_ms >= 0),
			input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
			cached_input_tokens INTEGER NOT NULL CHECK (cached_input_tokens >= 0),
			output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
			reasoning_tokens INTEGER NOT NULL CHECK (reasoning_tokens >= 0),
			PRIMARY KEY (session_id, generation, day_start_ms),
			FOREIGN KEY (session_id, generation)
				REFERENCES light_token_scans(session_id, generation) ON DELETE CASCADE
		) STRICT`,
	},
	{
		ObjectType: "table",
		Name:       "light_token_timed",
		Statement: `CREATE TABLE IF NOT EXISTS light_token_timed (
			session_id TEXT NOT NULL,
			generation INTEGER NOT NULL CHECK (generation > 0),
			source_offset INTEGER NOT NULL CHECK (source_offset > 0),
			observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
			input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
			cached_input_tokens INTEGER NOT NULL CHECK (cached_input_tokens >= 0),
			output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
			reasoning_tokens INTEGER NOT NULL CHECK (reasoning_tokens >= 0),
			PRIMARY KEY (session_id, generation, source_offset),
			FOREIGN KEY (session_id, generation)
				REFERENCES light_token_scans(session_id, generation) ON DELETE CASCADE
		) STRICT`,
	},
	{
		ObjectType: "index",
		Name:       "idx_light_sessions_updated",
		Statement: `CREATE INDEX IF NOT EXISTS idx_light_sessions_updated
			ON light_sessions(updated_at_ms DESC, session_id)`,
	},
	{
		ObjectType: "index",
		Name:       "idx_light_sessions_cwd",
		Statement: `CREATE INDEX IF NOT EXISTS idx_light_sessions_cwd
			ON light_sessions(cwd, session_id)`,
	},
	{
		ObjectType: "index",
		Name:       "idx_light_token_daily_day",
		Statement: `CREATE INDEX IF NOT EXISTS idx_light_token_daily_day
			ON light_token_daily(day_start_ms, session_id, generation)`,
	},
	{
		ObjectType: "index",
		Name:       "idx_light_token_timed_observed",
		Statement: `CREATE INDEX IF NOT EXISTS idx_light_token_timed_observed
			ON light_token_timed(observed_at_ms, session_id, generation)`,
	},
}

// SchemaObjects 返回 v16 冻结的轻量索引 schema 描述副本。
func SchemaObjects() []storeschema.Object {
	return append([]storeschema.Object(nil), lightIndexSchemaObjects...)
}

// CurrentSchemaObjects 返回应用当前版本所需的轻量索引 schema 描述。
func CurrentSchemaObjects() []storeschema.Object {
	objects := SchemaObjects()
	for index := range objects {
		switch objects[index].Name {
		case "light_token_scans":
			objects[index].Statement = appendMigratedColumns(
				objects[index].Statement,
				"\n\t\t\tPRIMARY KEY",
				"`current_model_key` TEXT CHECK (current_model_key IS NULL OR (length(current_model_key) BETWEEN 1 AND 128))",
				"`current_model_source` TEXT NOT NULL DEFAULT 'missing' CHECK (current_model_source IN ('model_canonical','model_alias','missing','invalid_model'))",
			)
		case "light_token_timed":
			objects[index].Statement = appendMigratedColumns(
				objects[index].Statement,
				"\n\t\t\tPRIMARY KEY",
				"`model_key` TEXT CHECK (model_key IS NULL OR (length(model_key) BETWEEN 1 AND 128))",
				"`model_source` TEXT NOT NULL DEFAULT 'missing' CHECK (model_source IN ('model_canonical','model_alias','missing','invalid_model'))",
			)
		}
	}
	return objects
}
