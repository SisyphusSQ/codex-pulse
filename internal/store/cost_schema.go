package store

var costSchemaObjects = []schemaObject{
	{objectType: "table", name: "pricing_catalog_metadata", statement: `CREATE TABLE IF NOT EXISTS pricing_catalog_metadata (
		pricing_version TEXT PRIMARY KEY CHECK (length(pricing_version) > 0) REFERENCES pricing_versions(pricing_version) ON DELETE CASCADE,
		source_url TEXT NOT NULL CHECK (length(source_url) > 0 AND length(source_url) <= 2048 AND source_url LIKE 'https://%'),
		verified_at_ms INTEGER NOT NULL CHECK (verified_at_ms > 0)
	) STRICT`},
	{objectType: "table", name: "cost_rollup_generations", statement: `CREATE TABLE IF NOT EXISTS cost_rollup_generations (
		generation_id TEXT PRIMARY KEY CHECK (length(generation_id) > 0),
		reporting_timezone TEXT NOT NULL CHECK (length(reporting_timezone) > 0),
		pricing_source TEXT NOT NULL CHECK (length(pricing_source) > 0),
		currency TEXT NOT NULL CHECK (length(currency) > 0),
		rollup_version INTEGER NOT NULL CHECK (rollup_version > 0),
		state TEXT NOT NULL CHECK (state IN ('building', 'active', 'superseded')),
		created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
		completed_at_ms INTEGER,
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= created_at_ms),
		UNIQUE (generation_id, reporting_timezone),
		CHECK (
			(state = 'building' AND completed_at_ms IS NULL)
			OR (
				state IN ('active', 'superseded')
				AND completed_at_ms IS NOT NULL
				AND completed_at_ms >= created_at_ms
				AND updated_at_ms >= completed_at_ms
			)
		)
	) STRICT`},
	{objectType: "table", name: "turn_costs", statement: `CREATE TABLE IF NOT EXISTS turn_costs (
		generation_id TEXT NOT NULL CHECK (length(generation_id) > 0) REFERENCES cost_rollup_generations(generation_id) ON DELETE CASCADE,
		turn_id TEXT NOT NULL CHECK (length(turn_id) > 0) REFERENCES turns(turn_id) ON DELETE CASCADE,
		pricing_version TEXT CHECK (pricing_version IS NULL OR length(pricing_version) > 0) REFERENCES pricing_versions(pricing_version),
		estimated_usd_micros INTEGER CHECK (estimated_usd_micros IS NULL OR estimated_usd_micros >= 0),
		pricing_status TEXT NOT NULL CHECK (pricing_status IN ('priced', 'unpriced')),
		pricing_reason TEXT NOT NULL CHECK (pricing_reason IN (
			'priced', 'missing_attribution', 'missing_model', 'conflict_model', 'invalid_model',
			'catalog_not_effective', 'model_not_listed', 'missing_token', 'missing_price_component'
		)),
		calculated_at_ms INTEGER NOT NULL CHECK (calculated_at_ms >= 0),
		PRIMARY KEY (generation_id, turn_id),
		CHECK (
			(pricing_status = 'priced' AND pricing_reason = 'priced' AND pricing_version IS NOT NULL AND estimated_usd_micros IS NOT NULL)
			OR (pricing_status = 'unpriced' AND pricing_reason != 'priced' AND estimated_usd_micros IS NULL)
		)
	) STRICT`},
	{objectType: "table", name: "session_usage_rollups", statement: `CREATE TABLE IF NOT EXISTS session_usage_rollups (
		generation_id TEXT NOT NULL CHECK (length(generation_id) > 0) REFERENCES cost_rollup_generations(generation_id) ON DELETE CASCADE,
		session_id TEXT NOT NULL CHECK (length(session_id) > 0) REFERENCES sessions(session_id) ON DELETE CASCADE,
		turn_count INTEGER NOT NULL CHECK (turn_count > 0),
		input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
		cached_input_tokens INTEGER CHECK (cached_input_tokens IS NULL OR cached_input_tokens >= 0),
		output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
		reasoning_tokens INTEGER CHECK (reasoning_tokens IS NULL OR reasoning_tokens >= 0),
		total_tokens INTEGER CHECK (total_tokens IS NULL OR total_tokens >= 0),
		estimated_usd_micros INTEGER CHECK (estimated_usd_micros IS NULL OR estimated_usd_micros >= 0),
		priced_turn_count INTEGER NOT NULL CHECK (priced_turn_count >= 0),
		unpriced_turn_count INTEGER NOT NULL CHECK (unpriced_turn_count >= 0),
		first_activity_at_ms INTEGER NOT NULL CHECK (first_activity_at_ms >= 0),
		last_activity_at_ms INTEGER NOT NULL CHECK (last_activity_at_ms >= first_activity_at_ms),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= last_activity_at_ms),
		PRIMARY KEY (generation_id, session_id),
		CHECK (priced_turn_count + unpriced_turn_count = turn_count),
		CHECK ((priced_turn_count = 0 AND estimated_usd_micros IS NULL) OR (priced_turn_count > 0 AND estimated_usd_micros IS NOT NULL)),
		CHECK (
			((input_tokens IS NULL OR cached_input_tokens IS NULL OR output_tokens IS NULL OR reasoning_tokens IS NULL) AND total_tokens IS NULL)
			OR (
				input_tokens IS NOT NULL AND cached_input_tokens IS NOT NULL
				AND output_tokens IS NOT NULL AND reasoning_tokens IS NOT NULL
				AND total_tokens IS NOT NULL
				AND total_tokens = input_tokens + cached_input_tokens + output_tokens + reasoning_tokens
			)
		)
	) STRICT`},
	{objectType: "table", name: "usage_daily", statement: `CREATE TABLE IF NOT EXISTS usage_daily (
		generation_id TEXT NOT NULL CHECK (length(generation_id) > 0),
		bucket_start_ms INTEGER NOT NULL CHECK (bucket_start_ms >= 0),
		reporting_timezone TEXT NOT NULL CHECK (length(reporting_timezone) > 0),
		turn_count INTEGER NOT NULL CHECK (turn_count > 0),
		input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
		cached_input_tokens INTEGER CHECK (cached_input_tokens IS NULL OR cached_input_tokens >= 0),
		output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
		reasoning_tokens INTEGER CHECK (reasoning_tokens IS NULL OR reasoning_tokens >= 0),
		total_tokens INTEGER CHECK (total_tokens IS NULL OR total_tokens >= 0),
		estimated_usd_micros INTEGER CHECK (estimated_usd_micros IS NULL OR estimated_usd_micros >= 0),
		priced_turn_count INTEGER NOT NULL CHECK (priced_turn_count >= 0),
		unpriced_turn_count INTEGER NOT NULL CHECK (unpriced_turn_count >= 0),
		first_activity_at_ms INTEGER NOT NULL CHECK (first_activity_at_ms >= 0),
		last_activity_at_ms INTEGER NOT NULL CHECK (last_activity_at_ms >= first_activity_at_ms),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= last_activity_at_ms),
		PRIMARY KEY (generation_id, bucket_start_ms),
		FOREIGN KEY (generation_id, reporting_timezone) REFERENCES cost_rollup_generations(generation_id, reporting_timezone) ON DELETE CASCADE,
		CHECK (priced_turn_count + unpriced_turn_count = turn_count),
		CHECK ((priced_turn_count = 0 AND estimated_usd_micros IS NULL) OR (priced_turn_count > 0 AND estimated_usd_micros IS NOT NULL)),
		CHECK (
			((input_tokens IS NULL OR cached_input_tokens IS NULL OR output_tokens IS NULL OR reasoning_tokens IS NULL) AND total_tokens IS NULL)
			OR (
				input_tokens IS NOT NULL AND cached_input_tokens IS NOT NULL
				AND output_tokens IS NOT NULL AND reasoning_tokens IS NOT NULL
				AND total_tokens IS NOT NULL
				AND total_tokens = input_tokens + cached_input_tokens + output_tokens + reasoning_tokens
			)
		)
	) STRICT`},
	{objectType: "table", name: "project_usage_daily", statement: `CREATE TABLE IF NOT EXISTS project_usage_daily (
		generation_id TEXT NOT NULL CHECK (length(generation_id) > 0),
		bucket_start_ms INTEGER NOT NULL CHECK (bucket_start_ms >= 0),
		reporting_timezone TEXT NOT NULL CHECK (length(reporting_timezone) > 0),
		dimension_key TEXT NOT NULL CHECK (length(dimension_key) > 0),
		project_id TEXT,
		project_display_name TEXT,
		attribution_confidence TEXT NOT NULL CHECK (length(attribution_confidence) > 0),
		attribution_source TEXT NOT NULL CHECK (length(attribution_source) > 0),
		attribution_reason TEXT NOT NULL CHECK (length(attribution_reason) > 0),
		turn_count INTEGER NOT NULL CHECK (turn_count > 0),
		input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
		cached_input_tokens INTEGER CHECK (cached_input_tokens IS NULL OR cached_input_tokens >= 0),
		output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
		reasoning_tokens INTEGER CHECK (reasoning_tokens IS NULL OR reasoning_tokens >= 0),
		total_tokens INTEGER CHECK (total_tokens IS NULL OR total_tokens >= 0),
		estimated_usd_micros INTEGER CHECK (estimated_usd_micros IS NULL OR estimated_usd_micros >= 0),
		priced_turn_count INTEGER NOT NULL CHECK (priced_turn_count >= 0),
		unpriced_turn_count INTEGER NOT NULL CHECK (unpriced_turn_count >= 0),
		first_activity_at_ms INTEGER NOT NULL CHECK (first_activity_at_ms >= 0),
		last_activity_at_ms INTEGER NOT NULL CHECK (last_activity_at_ms >= first_activity_at_ms),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= last_activity_at_ms),
		PRIMARY KEY (generation_id, bucket_start_ms, dimension_key),
		FOREIGN KEY (generation_id, reporting_timezone) REFERENCES cost_rollup_generations(generation_id, reporting_timezone) ON DELETE CASCADE,
		CHECK ((project_id IS NULL AND project_display_name IS NULL) OR (project_id IS NOT NULL AND length(project_id) > 0 AND project_display_name IS NOT NULL AND length(project_display_name) > 0 AND dimension_key = project_id)),
		CHECK (priced_turn_count + unpriced_turn_count = turn_count),
		CHECK ((priced_turn_count = 0 AND estimated_usd_micros IS NULL) OR (priced_turn_count > 0 AND estimated_usd_micros IS NOT NULL)),
		CHECK (
			((input_tokens IS NULL OR cached_input_tokens IS NULL OR output_tokens IS NULL OR reasoning_tokens IS NULL) AND total_tokens IS NULL)
			OR (
				input_tokens IS NOT NULL AND cached_input_tokens IS NOT NULL
				AND output_tokens IS NOT NULL AND reasoning_tokens IS NOT NULL
				AND total_tokens IS NOT NULL
				AND total_tokens = input_tokens + cached_input_tokens + output_tokens + reasoning_tokens
			)
		)
	) STRICT`},
	{objectType: "table", name: "model_usage_daily", statement: `CREATE TABLE IF NOT EXISTS model_usage_daily (
		generation_id TEXT NOT NULL CHECK (length(generation_id) > 0),
		bucket_start_ms INTEGER NOT NULL CHECK (bucket_start_ms >= 0),
		reporting_timezone TEXT NOT NULL CHECK (length(reporting_timezone) > 0),
		dimension_key TEXT NOT NULL CHECK (length(dimension_key) > 0),
		model_key TEXT,
		model_display_name TEXT,
		attribution_confidence TEXT NOT NULL CHECK (length(attribution_confidence) > 0),
		attribution_source TEXT NOT NULL CHECK (length(attribution_source) > 0),
		attribution_reason TEXT NOT NULL CHECK (length(attribution_reason) > 0),
		turn_count INTEGER NOT NULL CHECK (turn_count > 0),
		input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
		cached_input_tokens INTEGER CHECK (cached_input_tokens IS NULL OR cached_input_tokens >= 0),
		output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
		reasoning_tokens INTEGER CHECK (reasoning_tokens IS NULL OR reasoning_tokens >= 0),
		total_tokens INTEGER CHECK (total_tokens IS NULL OR total_tokens >= 0),
		estimated_usd_micros INTEGER CHECK (estimated_usd_micros IS NULL OR estimated_usd_micros >= 0),
		priced_turn_count INTEGER NOT NULL CHECK (priced_turn_count >= 0),
		unpriced_turn_count INTEGER NOT NULL CHECK (unpriced_turn_count >= 0),
		first_activity_at_ms INTEGER NOT NULL CHECK (first_activity_at_ms >= 0),
		last_activity_at_ms INTEGER NOT NULL CHECK (last_activity_at_ms >= first_activity_at_ms),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= last_activity_at_ms),
		PRIMARY KEY (generation_id, bucket_start_ms, dimension_key),
		FOREIGN KEY (generation_id, reporting_timezone) REFERENCES cost_rollup_generations(generation_id, reporting_timezone) ON DELETE CASCADE,
		CHECK ((model_key IS NULL AND model_display_name IS NULL) OR (model_key IS NOT NULL AND length(model_key) > 0 AND model_display_name IS NOT NULL AND length(model_display_name) > 0 AND dimension_key = model_key)),
		CHECK (priced_turn_count + unpriced_turn_count = turn_count),
		CHECK ((priced_turn_count = 0 AND estimated_usd_micros IS NULL) OR (priced_turn_count > 0 AND estimated_usd_micros IS NOT NULL)),
		CHECK (
			((input_tokens IS NULL OR cached_input_tokens IS NULL OR output_tokens IS NULL OR reasoning_tokens IS NULL) AND total_tokens IS NULL)
			OR (
				input_tokens IS NOT NULL AND cached_input_tokens IS NOT NULL
				AND output_tokens IS NOT NULL AND reasoning_tokens IS NOT NULL
				AND total_tokens IS NOT NULL
				AND total_tokens = input_tokens + cached_input_tokens + output_tokens + reasoning_tokens
			)
		)
	) STRICT`},
	{objectType: "index", name: "idx_cost_rollup_generations_active", statement: `CREATE UNIQUE INDEX IF NOT EXISTS idx_cost_rollup_generations_active
		ON cost_rollup_generations(reporting_timezone) WHERE state = 'active'`},
	{objectType: "index", name: "idx_turn_costs_turn", statement: `CREATE INDEX IF NOT EXISTS idx_turn_costs_turn
		ON turn_costs(turn_id, generation_id)`},
	{objectType: "index", name: "idx_session_usage_rollups_session", statement: `CREATE INDEX IF NOT EXISTS idx_session_usage_rollups_session
		ON session_usage_rollups(session_id, generation_id)`},
	{objectType: "index", name: "idx_usage_daily_bucket", statement: `CREATE INDEX IF NOT EXISTS idx_usage_daily_bucket
		ON usage_daily(bucket_start_ms, generation_id)`},
	{objectType: "index", name: "idx_project_usage_daily_dimension", statement: `CREATE INDEX IF NOT EXISTS idx_project_usage_daily_dimension
		ON project_usage_daily(dimension_key, bucket_start_ms, generation_id)`},
	{objectType: "index", name: "idx_model_usage_daily_dimension", statement: `CREATE INDEX IF NOT EXISTS idx_model_usage_daily_dimension
		ON model_usage_daily(dimension_key, bucket_start_ms, generation_id)`},
}
