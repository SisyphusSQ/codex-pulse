-- v4：中心查询摘要与额度精简幂等记录；只新增表，不删除既有历史。
CREATE TABLE IF NOT EXISTS pulse_retired_observations (
    id TEXT NOT NULL,
    digest TEXT NOT NULL,
    retired_at_ms INTEGER NOT NULL,
    PRIMARY KEY (id)
) STRICT;
CREATE TABLE IF NOT EXISTS pulse_session_capsules (
    kind TEXT NOT NULL,
    id TEXT NOT NULL,
    session_key TEXT NOT NULL,
    client_id TEXT NOT NULL,
    complete INTEGER NOT NULL,
    deleted INTEGER NOT NULL,
    facts_digest TEXT NOT NULL,
    throughput TEXT,
    cache_usage TEXT,
    PRIMARY KEY (kind,id)
) STRICT;
CREATE INDEX IF NOT EXISTS idx_capsules_session ON pulse_session_capsules(session_key,kind);
