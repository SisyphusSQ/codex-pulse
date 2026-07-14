package store

// retentionSchemaObjects is a separate v2 migration contract. Keeping these
// indexes outside runtimeSchemaObjects preserves the released v1 checksum.
var retentionSchemaObjects = []schemaObject{
	{objectType: "index", name: "idx_health_events_retention", statement: `CREATE INDEX IF NOT EXISTS idx_health_events_retention
		ON health_events(resolved_at_ms, event_id)`},
	{objectType: "index", name: "idx_job_runs_retention", statement: `CREATE INDEX IF NOT EXISTS idx_job_runs_retention
		ON job_runs(state, finished_at_ms, job_id)`},
	{objectType: "index", name: "idx_job_runs_resume_lineage", statement: `CREATE INDEX IF NOT EXISTS idx_job_runs_resume_lineage
		ON job_runs(resume_of_job_id, job_id)`},
	{objectType: "index", name: "idx_source_attempts_retention", statement: `CREATE INDEX IF NOT EXISTS idx_source_attempts_retention
		ON source_attempts(finished_at_ms, request_id)`},
}
