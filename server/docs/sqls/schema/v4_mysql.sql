-- v4：中心查询摘要与额度精简幂等记录；只新增表，不删除既有历史。
CREATE TABLE IF NOT EXISTS pulse_retired_observations (
    id CHAR(64) NOT NULL,
    digest CHAR(64) NOT NULL,
    retired_at_ms BIGINT NOT NULL,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS pulse_session_capsules (
    kind VARCHAR(16) NOT NULL,
    id CHAR(64) NOT NULL,
    session_key CHAR(64) NOT NULL,
    client_id CHAR(36) NOT NULL,
    complete BOOLEAN NOT NULL,
    deleted BOOLEAN NOT NULL,
    facts_digest CHAR(64) NOT NULL,
    throughput LONGTEXT,
    cache_usage LONGTEXT,
    PRIMARY KEY (kind,id),
    KEY idx_capsules_session (session_key,kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
