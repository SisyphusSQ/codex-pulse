package store

const attributionEnumCheck = `('high', 'medium', 'low', 'unknown')`
const attributionSourceCheck = `(
	'session_id_fallback', 'registered_root', 'cwd_path_digest',
	'model_canonical', 'model_alias', 'conflict', 'missing',
	'invalid_path', 'invalid_model'
)`
const attributionReasonCheck = `(
	'stable_identity', 'root_matched', 'path_derived', 'observed',
	'conflict', 'missing', 'invalid'
)`

var attributionSchemaObjects = []schemaObject{
	{objectType: "table", name: "session_attributions", statement: `CREATE TABLE IF NOT EXISTS session_attributions (
		session_id TEXT PRIMARY KEY CHECK (length(session_id) > 0)
			REFERENCES sessions(session_id) ON DELETE CASCADE,
		display_title TEXT NOT NULL CHECK (length(display_title) > 0),
		title_confidence TEXT NOT NULL CHECK (title_confidence IN ` + attributionEnumCheck + `),
		title_source TEXT NOT NULL CHECK (title_source IN ` + attributionSourceCheck + `),
		title_reason TEXT NOT NULL CHECK (title_reason IN ` + attributionReasonCheck + `),
		project_id TEXT,
		project_display_name TEXT,
		project_confidence TEXT NOT NULL CHECK (project_confidence IN ` + attributionEnumCheck + `),
		project_source TEXT NOT NULL CHECK (project_source IN ` + attributionSourceCheck + `),
		project_reason TEXT NOT NULL CHECK (project_reason IN ` + attributionReasonCheck + `),
		model_key TEXT,
		model_display_name TEXT,
		model_confidence TEXT NOT NULL CHECK (model_confidence IN ` + attributionEnumCheck + `),
		model_source TEXT NOT NULL CHECK (model_source IN ` + attributionSourceCheck + `),
		model_reason TEXT NOT NULL CHECK (model_reason IN ` + attributionReasonCheck + `),
		rule_version INTEGER NOT NULL CHECK (rule_version > 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		CHECK ((project_id IS NULL AND project_display_name IS NULL)
			OR (project_id IS NOT NULL AND project_display_name IS NOT NULL
				AND length(project_id) > 0 AND length(project_display_name) > 0)),
		CHECK ((model_key IS NULL AND model_display_name IS NULL)
			OR (model_key IS NOT NULL AND model_display_name IS NOT NULL
				AND length(model_key) > 0 AND length(model_display_name) > 0))
	) STRICT`},
	{objectType: "table", name: "turn_attributions", statement: `CREATE TABLE IF NOT EXISTS turn_attributions (
		turn_id TEXT PRIMARY KEY CHECK (length(turn_id) > 0)
			REFERENCES turns(turn_id) ON DELETE CASCADE,
		project_id TEXT,
		project_display_name TEXT,
		project_confidence TEXT NOT NULL CHECK (project_confidence IN ` + attributionEnumCheck + `),
		project_source TEXT NOT NULL CHECK (project_source IN ` + attributionSourceCheck + `),
		project_reason TEXT NOT NULL CHECK (project_reason IN ` + attributionReasonCheck + `),
		model_key TEXT,
		model_display_name TEXT,
		model_confidence TEXT NOT NULL CHECK (model_confidence IN ` + attributionEnumCheck + `),
		model_source TEXT NOT NULL CHECK (model_source IN ` + attributionSourceCheck + `),
		model_reason TEXT NOT NULL CHECK (model_reason IN ` + attributionReasonCheck + `),
		rule_version INTEGER NOT NULL CHECK (rule_version > 0),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0),
		CHECK ((project_id IS NULL AND project_display_name IS NULL)
			OR (project_id IS NOT NULL AND project_display_name IS NOT NULL
				AND length(project_id) > 0 AND length(project_display_name) > 0)),
		CHECK ((model_key IS NULL AND model_display_name IS NULL)
			OR (model_key IS NOT NULL AND model_display_name IS NOT NULL
				AND length(model_key) > 0 AND length(model_display_name) > 0))
	) STRICT`},
	{objectType: "index", name: "idx_session_attributions_project", statement: `CREATE INDEX IF NOT EXISTS idx_session_attributions_project
		ON session_attributions(project_id, session_id)`},
	{objectType: "index", name: "idx_session_attributions_model", statement: `CREATE INDEX IF NOT EXISTS idx_session_attributions_model
		ON session_attributions(model_key, session_id)`},
	{objectType: "index", name: "idx_turn_attributions_project", statement: `CREATE INDEX IF NOT EXISTS idx_turn_attributions_project
		ON turn_attributions(project_id, turn_id)`},
	{objectType: "index", name: "idx_turn_attributions_model", statement: `CREATE INDEX IF NOT EXISTS idx_turn_attributions_model
		ON turn_attributions(model_key, turn_id)`},
}
