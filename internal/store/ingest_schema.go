package store

// turnsSchemaV1Statement 是 migration v1 的历史 DDL。它必须与已发布 checksum
// 保持一致；v3 只通过 append-only table rebuild 放宽 source offset 顺序。
const turnsSchemaV1Statement = `CREATE TABLE IF NOT EXISTS turns (
		turn_id TEXT PRIMARY KEY CHECK (length(turn_id) > 0),
		session_id TEXT NOT NULL CHECK (length(session_id) > 0) REFERENCES sessions(session_id) ON DELETE CASCADE,
		started_at_ms INTEGER NOT NULL CHECK (started_at_ms >= 0),
		completed_at_ms INTEGER,
		outcome TEXT,
		model TEXT CHECK (model IS NULL OR length(model) > 0),
		reasoning_effort TEXT CHECK (reasoning_effort IS NULL OR length(reasoning_effort) > 0),
		cwd TEXT CHECK (cwd IS NULL OR length(cwd) > 0),
		project_id TEXT CHECK (project_id IS NULL OR length(project_id) > 0) REFERENCES projects(project_id) ON DELETE SET NULL,
		source_generation INTEGER NOT NULL CHECK (source_generation >= 0),
		start_offset INTEGER NOT NULL CHECK (start_offset >= 0),
		complete_offset INTEGER,
		CHECK (
			(completed_at_ms IS NULL AND outcome IS NULL AND complete_offset IS NULL)
			OR (
				completed_at_ms >= started_at_ms
				AND outcome IS NOT NULL AND length(outcome) > 0
				AND complete_offset >= start_offset
			)
		)
	) STRICT`

// turnsSchemaCurrentStatement 接受 terminal-before-start 的原始 source position：
// complete_offset 只要求非负，不再错误地要求它晚于 start_offset。
const turnsSchemaCurrentStatement = `CREATE TABLE IF NOT EXISTS turns (
		turn_id TEXT PRIMARY KEY CHECK (length(turn_id) > 0),
		session_id TEXT NOT NULL CHECK (length(session_id) > 0) REFERENCES sessions(session_id) ON DELETE CASCADE,
		started_at_ms INTEGER NOT NULL CHECK (started_at_ms >= 0),
		completed_at_ms INTEGER,
		outcome TEXT,
		model TEXT CHECK (model IS NULL OR length(model) > 0),
		reasoning_effort TEXT CHECK (reasoning_effort IS NULL OR length(reasoning_effort) > 0),
		cwd TEXT CHECK (cwd IS NULL OR length(cwd) > 0),
		project_id TEXT CHECK (project_id IS NULL OR length(project_id) > 0) REFERENCES projects(project_id) ON DELETE SET NULL,
		source_generation INTEGER NOT NULL CHECK (source_generation >= 0),
		start_offset INTEGER NOT NULL CHECK (start_offset >= 0),
		complete_offset INTEGER CHECK (complete_offset IS NULL OR complete_offset >= 0),
		CHECK (
			(completed_at_ms IS NULL AND outcome IS NULL AND complete_offset IS NULL)
			OR (
				completed_at_ms >= started_at_ms
				AND outcome IS NOT NULL AND length(outcome) > 0
				AND complete_offset IS NOT NULL
			)
		)
	) STRICT`

var ingestSchemaObjects = []schemaObject{
	{objectType: "table", name: "source_generations", statement: `CREATE TABLE IF NOT EXISTS source_generations (
		source_file_id TEXT NOT NULL CHECK (length(source_file_id) > 0) REFERENCES source_files(source_file_id) ON DELETE CASCADE,
		generation INTEGER NOT NULL CHECK (generation >= 0),
		state TEXT NOT NULL CHECK (state IN ('building', 'active', 'superseded')),
		provider TEXT NOT NULL CHECK (provider = 'codex'),
		source_kind TEXT NOT NULL CHECK (source_kind IN ('session', 'archived_session')),
		current_path TEXT NOT NULL CHECK (length(current_path) > 0),
		device_id TEXT NOT NULL CHECK (length(device_id) > 0),
		inode INTEGER NOT NULL CHECK (inode >= 0),
		size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
		mtime_ns INTEGER NOT NULL CHECK (mtime_ns >= 0),
		prefix_bytes INTEGER NOT NULL CHECK (prefix_bytes >= 0 AND prefix_bytes <= size_bytes AND prefix_bytes <= 4096),
		prefix_sha256 TEXT NOT NULL CHECK (length(prefix_sha256) = 64 AND prefix_sha256 NOT GLOB '*[^0-9a-f]*'),
		fingerprint_sha256 TEXT NOT NULL CHECK (length(fingerprint_sha256) = 64 AND fingerprint_sha256 NOT GLOB '*[^0-9a-f]*'),
		parser_version TEXT NOT NULL CHECK (length(parser_version) > 0),
		committed_offset INTEGER NOT NULL CHECK (committed_offset >= 0 AND committed_offset <= size_bytes),
		session_id TEXT CHECK (session_id IS NULL OR length(session_id) > 0) REFERENCES sessions(session_id) ON DELETE SET NULL,
		replaces_source_file_id TEXT CHECK (
			replaces_source_file_id IS NULL OR length(replaces_source_file_id) > 0
		) REFERENCES source_files(source_file_id) ON DELETE SET NULL,
		base_source_file_id TEXT,
		base_generation INTEGER,
		base_fingerprint_sha256 TEXT,
		superseded_building_source_file_id TEXT,
		superseded_building_generation INTEGER,
		superseded_building_fingerprint_sha256 TEXT,
		superseded_building_parser_version TEXT,
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		PRIMARY KEY (source_file_id, generation),
		FOREIGN KEY (base_source_file_id, base_generation)
			REFERENCES source_generations(source_file_id, generation) ON DELETE RESTRICT,
		FOREIGN KEY (superseded_building_source_file_id, superseded_building_generation)
			REFERENCES source_generations(source_file_id, generation) ON DELETE RESTRICT,
		CHECK (replaces_source_file_id IS NULL OR replaces_source_file_id != source_file_id),
		CHECK (
			(base_source_file_id IS NULL AND base_generation IS NULL AND base_fingerprint_sha256 IS NULL)
			OR (
				base_source_file_id IS NOT NULL AND length(base_source_file_id) > 0
				AND base_generation IS NOT NULL AND base_generation >= 0
				AND base_fingerprint_sha256 IS NOT NULL
				AND length(base_fingerprint_sha256) = 64
				AND base_fingerprint_sha256 NOT GLOB '*[^0-9a-f]*'
				AND NOT (base_source_file_id = source_file_id AND base_generation = generation)
			)
		),
		CHECK (
			(superseded_building_source_file_id IS NULL
				AND superseded_building_generation IS NULL
				AND superseded_building_fingerprint_sha256 IS NULL
				AND superseded_building_parser_version IS NULL)
			OR (
				superseded_building_source_file_id IS NOT NULL
				AND length(superseded_building_source_file_id) > 0
				AND superseded_building_generation IS NOT NULL
				AND superseded_building_generation >= 0
				AND superseded_building_fingerprint_sha256 IS NOT NULL
				AND length(superseded_building_fingerprint_sha256) = 64
				AND superseded_building_fingerprint_sha256 NOT GLOB '*[^0-9a-f]*'
				AND superseded_building_parser_version IS NOT NULL
				AND length(superseded_building_parser_version) > 0
				AND NOT (
					superseded_building_source_file_id = source_file_id
					AND superseded_building_generation = generation
				)
			)
		)
	) STRICT`},
	{objectType: "table", name: "parser_checkpoints", statement: `CREATE TABLE IF NOT EXISTS parser_checkpoints (
		source_file_id TEXT NOT NULL CHECK (length(source_file_id) > 0),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		checkpoint_version INTEGER NOT NULL CHECK (checkpoint_version > 0),
		parser_seed BLOB NOT NULL,
		projector_state BLOB NOT NULL,
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		PRIMARY KEY (source_file_id, generation),
		FOREIGN KEY (source_file_id, generation)
			REFERENCES source_generations(source_file_id, generation) ON DELETE CASCADE
	) STRICT`},
	{objectType: "table", name: "source_generation_batches", statement: `CREATE TABLE IF NOT EXISTS source_generation_batches (
		source_file_id TEXT NOT NULL CHECK (length(source_file_id) > 0),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		from_offset INTEGER NOT NULL CHECK (from_offset >= 0),
		to_offset INTEGER NOT NULL CHECK (to_offset >= from_offset),
		batch_identity_sha256 TEXT NOT NULL CHECK (length(batch_identity_sha256) = 64 AND batch_identity_sha256 NOT GLOB '*[^0-9a-f]*'),
		facts BLOB NOT NULL,
		eof INTEGER NOT NULL CHECK (eof IN (0, 1)),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		PRIMARY KEY (source_file_id, generation, from_offset, to_offset, batch_identity_sha256),
		FOREIGN KEY (source_file_id, generation)
			REFERENCES source_generations(source_file_id, generation) ON DELETE CASCADE,
		CHECK (to_offset > from_offset OR eof = 1)
	) STRICT`},
	{objectType: "table", name: "parser_diagnostics", statement: `CREATE TABLE IF NOT EXISTS parser_diagnostics (
		source_file_id TEXT NOT NULL CHECK (length(source_file_id) > 0),
		generation INTEGER NOT NULL CHECK (generation >= 0),
		batch_end_offset INTEGER NOT NULL CHECK (batch_end_offset >= 0),
		batch_identity_sha256 TEXT NOT NULL CHECK (
			length(batch_identity_sha256) = 64 AND batch_identity_sha256 NOT GLOB '*[^0-9a-f]*'
		),
		ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
		class TEXT NOT NULL CHECK (class IN ('framing', 'syntax', 'compatibility', 'lifecycle')),
		code TEXT NOT NULL CHECK (length(code) > 0),
		start_offset INTEGER NOT NULL CHECK (start_offset >= 0),
		end_offset INTEGER NOT NULL CHECK (end_offset > start_offset),
		retryable INTEGER NOT NULL CHECK (retryable IN (0, 1)),
		PRIMARY KEY (source_file_id, generation, batch_end_offset, batch_identity_sha256, ordinal),
		FOREIGN KEY (source_file_id, generation)
			REFERENCES source_generations(source_file_id, generation) ON DELETE CASCADE
	) STRICT`},
	{objectType: "index", name: "idx_source_generations_active", statement: `CREATE UNIQUE INDEX IF NOT EXISTS idx_source_generations_active
		ON source_generations(source_file_id) WHERE state = 'active'`},
	{objectType: "index", name: "idx_source_generations_building", statement: `CREATE UNIQUE INDEX IF NOT EXISTS idx_source_generations_building
		ON source_generations(source_file_id) WHERE state = 'building'`},
	{objectType: "index", name: "idx_source_generations_active_session", statement: `CREATE UNIQUE INDEX IF NOT EXISTS idx_source_generations_active_session
		ON source_generations(session_id) WHERE state = 'active' AND session_id IS NOT NULL`},
	{objectType: "index", name: "idx_source_generations_snapshot", statement: `CREATE INDEX IF NOT EXISTS idx_source_generations_snapshot
		ON source_generations(state, current_path, source_file_id)`},
	{objectType: "index", name: "idx_generation_batches_replay", statement: `CREATE INDEX IF NOT EXISTS idx_generation_batches_replay
		ON source_generation_batches(source_file_id, generation, from_offset, to_offset, batch_identity_sha256)`},
	{objectType: "index", name: "idx_parser_diagnostics_source", statement: `CREATE INDEX IF NOT EXISTS idx_parser_diagnostics_source
		ON parser_diagnostics(source_file_id, generation, batch_end_offset, batch_identity_sha256, ordinal)`},
}
