-- TOO-529 / schema v5：新增当前周期账号 Token 事实、来源及能力标记。
-- MySQL 8 / SQLite STRICT；前置 schema v4（旧版本由受控迁移链升级）。
-- 只新增表和索引，不回填、不删除旧数据；IF NOT EXISTS 可重入，漂移由启动读回拒绝。
CREATE TABLE IF NOT EXISTS pulse_account_token_facts (
    id TEXT NOT NULL,
    account_key TEXT NOT NULL,
    provider TEXT NOT NULL,
    observed_at_ms INTEGER NOT NULL,
    total_tokens INTEGER NOT NULL,
    PRIMARY KEY (id)
) STRICT;
CREATE INDEX IF NOT EXISTS idx_account_tokens_time ON pulse_account_token_facts(account_key,observed_at_ms);
CREATE TABLE IF NOT EXISTS pulse_account_token_sources (
    id TEXT NOT NULL,
    fact_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    PRIMARY KEY (id)
) STRICT;
CREATE INDEX IF NOT EXISTS idx_account_tokens_source ON pulse_account_token_sources(client_id,fact_id);
CREATE TABLE IF NOT EXISTS pulse_account_token_periods (
    id TEXT NOT NULL,
    account_key TEXT NOT NULL,
    provider TEXT NOT NULL,
    client_id TEXT NOT NULL,
    window_start_at_ms INTEGER NOT NULL,
    resets_at_ms INTEGER NOT NULL,
    collected_at_ms INTEGER NOT NULL,
    PRIMARY KEY (id)
) STRICT;
CREATE INDEX IF NOT EXISTS idx_account_tokens_period ON pulse_account_token_periods(account_key,resets_at_ms);
