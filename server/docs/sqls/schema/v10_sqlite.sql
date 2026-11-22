-- TOO-725 机器用量范围索引。启动迁移器事务内创建并回填。
CREATE TABLE IF NOT EXISTS pulse_machine_usage_sources (
    session_key TEXT NOT NULL,
    client_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    title TEXT NOT NULL,
    project_id TEXT NOT NULL,
    collected_at_ms INTEGER NOT NULL,
    complete INTEGER NOT NULL,
    conflict INTEGER NOT NULL,
    first_observed_at_ms INTEGER,
    last_observed_at_ms INTEGER,
    untimed_models TEXT,
PRIMARY KEY (session_key, client_id)
) STRICT;

CREATE INDEX idx_machine_usage_time ON pulse_machine_usage_sources (last_observed_at_ms,first_observed_at_ms);
