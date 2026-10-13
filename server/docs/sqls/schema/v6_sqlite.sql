-- TOO-582 / schema v6：持久化发布任务、不可见用量版本及分段快照。
-- 仅追加表，不更改或删除已有来源、分片、批次与配对。CREATE IF NOT EXISTS 支持失败重入。
CREATE TABLE IF NOT EXISTS pulse_publication_jobs (
    id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    batch_id TEXT NOT NULL,
    digest TEXT NOT NULL,
    source_key TEXT NOT NULL,
    revision INTEGER NOT NULL,
    body TEXT NOT NULL,
    state TEXT NOT NULL,
    failure_code TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    next_attempt_at_ms INTEGER NOT NULL,
    lease_until_ms INTEGER NOT NULL,
    lease_token TEXT NOT NULL,
PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS pulse_session_publications (
    session_key TEXT NOT NULL,
    generation TEXT NOT NULL,
PRIMARY KEY (session_key)
);

CREATE TABLE IF NOT EXISTS pulse_payload_parts (
    payload_key TEXT NOT NULL,
    part_index INTEGER NOT NULL,
    body BLOB NOT NULL,
    created_at_ms INTEGER NOT NULL,
PRIMARY KEY (payload_key, part_index)
);

CREATE TABLE IF NOT EXISTS pulse_usage_versions (
    generation TEXT NOT NULL,
    session_key TEXT NOT NULL,
    contribution_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    observed_at_ms INTEGER,
    model TEXT,
    input_tokens INTEGER,
    cached_tokens INTEGER,
    cache_write_tokens INTEGER,
    output_tokens INTEGER,
    reasoning_tokens INTEGER,
    total_tokens INTEGER,
    cost_micro_usd INTEGER,
    reported_charge_micro_usd INTEGER,
    pricing_version TEXT,
    pricing_mode TEXT NOT NULL,
    input_price INTEGER,
    cached_price INTEGER,
    cache_write_price INTEGER,
    output_price INTEGER,
    cost_status TEXT NOT NULL,
PRIMARY KEY (generation, session_key, contribution_id)
);
CREATE INDEX IF NOT EXISTS idx_usage_versions_time ON pulse_usage_versions (observed_at_ms, session_key, generation);

CREATE TABLE IF NOT EXISTS pulse_payload_owners (
    id TEXT NOT NULL,
    payload_key TEXT NOT NULL,
PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_payload_owners_key ON pulse_payload_owners (payload_key);

CREATE INDEX IF NOT EXISTS idx_publication_jobs_ready ON pulse_publication_jobs (state,next_attempt_at_ms,lease_until_ms,created_at_ms,id);
CREATE INDEX IF NOT EXISTS idx_payload_parts_age ON pulse_payload_parts (created_at_ms,payload_key,part_index);
