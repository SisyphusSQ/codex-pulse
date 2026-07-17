package store

// metricsSchemaObjects 是 v13 app runtime metrics schema。
//
// STRICT、跨字段 CHECK 与显式 retention index 无法由 GORM Migrator 完整表达，
// 因此 DDL 集中保留为 raw SQL 例外；普通 sample CRUD 与 snapshot 查询使用 GORM。
var metricsSchemaObjects = []schemaObject{
	{objectType: "table", name: "app_runtime_samples", statement: `CREATE TABLE IF NOT EXISTS app_runtime_samples (
		captured_at_ms INTEGER PRIMARY KEY CHECK (captured_at_ms >= 0),
		cpu_percent REAL NOT NULL CHECK (cpu_percent >= 0 AND cpu_percent <= 102400),
		cpu_user_ms INTEGER NOT NULL CHECK (cpu_user_ms >= 0),
		cpu_system_ms INTEGER NOT NULL CHECK (cpu_system_ms >= 0),
		rss_bytes INTEGER NOT NULL CHECK (rss_bytes >= 0),
		peak_rss_bytes INTEGER NOT NULL CHECK (peak_rss_bytes >= rss_bytes),
		goroutine_count INTEGER NOT NULL CHECK (goroutine_count > 0),
		db_bytes INTEGER NOT NULL CHECK (db_bytes >= 0),
		wal_bytes INTEGER NOT NULL CHECK (wal_bytes >= 0),
		disk_free_bytes INTEGER NOT NULL CHECK (disk_free_bytes >= 0),
		live_queue_depth INTEGER NOT NULL CHECK (live_queue_depth >= 0),
		backfill_queue_depth INTEGER NOT NULL CHECK (backfill_queue_depth >= 0),
		oldest_live_wait_ms INTEGER NOT NULL CHECK (oldest_live_wait_ms >= 0),
		oldest_backfill_wait_ms INTEGER NOT NULL CHECK (oldest_backfill_wait_ms >= 0),
		query_count INTEGER NOT NULL CHECK (query_count >= 0),
		query_total_micros INTEGER NOT NULL CHECK (query_total_micros >= 0),
		query_max_micros INTEGER NOT NULL CHECK (query_max_micros >= 0),
		collector_duration_micros INTEGER NOT NULL CHECK (collector_duration_micros >= 0),
		dropped_samples INTEGER NOT NULL CHECK (dropped_samples >= 0),
		CHECK (
			(query_count = 0 AND query_total_micros = 0 AND query_max_micros = 0)
			OR (query_count > 0 AND query_total_micros >= query_max_micros)
		)
	) STRICT`},
	{objectType: "index", name: "idx_app_runtime_samples_retention", statement: `CREATE INDEX IF NOT EXISTS idx_app_runtime_samples_retention
		ON app_runtime_samples(captured_at_ms)`},
}
