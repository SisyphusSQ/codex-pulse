-- TOO-529 / schema v5：新增当前周期账号 Token 事实、来源及能力标记。
-- MySQL 8 / SQLite STRICT；前置 schema v4（旧版本由受控迁移链升级）。
-- 只新增表和索引，不回填、不删除旧数据；IF NOT EXISTS 可重入，漂移由启动读回拒绝。
CREATE TABLE IF NOT EXISTS pulse_account_token_facts (
    id CHAR(64) NOT NULL,
    account_key CHAR(64) NOT NULL,
    provider VARCHAR(16) NOT NULL,
    observed_at_ms BIGINT NOT NULL,
    total_tokens BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_account_tokens_time (account_key,observed_at_ms)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS pulse_account_token_sources (
    id CHAR(64) NOT NULL,
    fact_id CHAR(64) NOT NULL,
    client_id CHAR(36) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_account_tokens_source (client_id,fact_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS pulse_account_token_periods (
    id CHAR(64) NOT NULL,
    account_key CHAR(64) NOT NULL,
    provider VARCHAR(16) NOT NULL,
    client_id CHAR(36) NOT NULL,
    window_start_at_ms BIGINT NOT NULL,
    resets_at_ms BIGINT NOT NULL,
    collected_at_ms BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_account_tokens_period (account_key,resets_at_ms)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
