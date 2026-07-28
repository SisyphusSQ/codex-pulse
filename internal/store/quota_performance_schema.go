package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

var quotaPerformanceSchemaObjects = []storeschema.Object{
	{
		ObjectType: "index",
		Name:       "idx_quota_observations_projection",
		Statement: `CREATE INDEX IF NOT EXISTS idx_quota_observations_projection
			ON quota_observations(account_scope, window_kind, limit_id, last_observed_at_ms, observation_id)`,
	},
}
