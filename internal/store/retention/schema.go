package retention

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

// retentionSchemaObjects is a separate v2 migration contract. Keeping these
// indexes outside runtimeSchemaObjects preserves the released v1 checksum.
var retentionSchemaObjects = []storeschema.Object{
	{ObjectType: "index", Name: "idx_health_events_retention", Statement: `CREATE INDEX IF NOT EXISTS idx_health_events_retention
		ON health_events(resolved_at_ms, event_id)`},
	{ObjectType: "index", Name: "idx_job_runs_retention", Statement: `CREATE INDEX IF NOT EXISTS idx_job_runs_retention
		ON job_runs(state, finished_at_ms, job_id)`},
	{ObjectType: "index", Name: "idx_job_runs_resume_lineage", Statement: `CREATE INDEX IF NOT EXISTS idx_job_runs_resume_lineage
		ON job_runs(resume_of_job_id, job_id)`},
	{ObjectType: "index", Name: "idx_source_attempts_retention", Statement: `CREATE INDEX IF NOT EXISTS idx_source_attempts_retention
		ON source_attempts(finished_at_ms, request_id)`},
}

// SchemaObjects 返回 v2 冻结的 retention 索引描述副本。
func SchemaObjects() []storeschema.Object {
	return append([]storeschema.Object(nil), retentionSchemaObjects...)
}
