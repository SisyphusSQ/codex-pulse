package store

var quotaPerformanceSchemaObjects = []schemaObject{
	{
		objectType: "index",
		name:       "idx_quota_observations_projection",
		statement: `CREATE INDEX IF NOT EXISTS idx_quota_observations_projection
			ON quota_observations(account_scope, window_kind, limit_id, last_observed_at_ms, observation_id)`,
	},
}
