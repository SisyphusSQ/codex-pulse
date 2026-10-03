-- Codex Pulse 中心结构 v2，MySQL 8.x（utf8mb4）或 SQLite 3.35+。
-- 仅通过显式 db init 初始化；CREATE IF NOT EXISTS 可重入，结构摘要最后提交。
-- 不自动迁移，不删除历史；MySQL DDL 不假设事务回滚，失败后检查实际结构再重入。
-- 时间保存UTC整数毫秒，金额保存整数微美元；NULL区别于零。

-- 中心结构版本与定义摘要
CREATE TABLE IF NOT EXISTS pulse_schema (
    id INTEGER NOT NULL,
    version INTEGER NOT NULL,
    checksum TEXT NOT NULL,
    initialized_at_ms INTEGER NOT NULL,
PRIMARY KEY (id)
);

-- 统一浏览器与采集设备授权
CREATE TABLE IF NOT EXISTS pulse_clients (
    id TEXT NOT NULL,
    purpose TEXT NOT NULL,
    name TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    origin TEXT NOT NULL,
    csrf_hash TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER,
    revoked_at_ms INTEGER,
    last_received_at_ms INTEGER,
PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS unq_clients_secret ON pulse_clients (secret_hash);

-- 短期一次性设备码
CREATE TABLE IF NOT EXISTS pulse_pairings (
    code_hash TEXT NOT NULL,
    purpose TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER NOT NULL,
    consumed_at_ms INTEGER,
PRIMARY KEY (code_hash)
);

-- 已原子提交批次与可重放确认
CREATE TABLE IF NOT EXISTS pulse_batches (
    client_id TEXT NOT NULL,
    batch_id TEXT NOT NULL,
    digest TEXT NOT NULL,
    received_at_ms INTEGER NOT NULL,
PRIMARY KEY (client_id, batch_id)
);

-- 设备项目与显式跨机关联
CREATE TABLE IF NOT EXISTS pulse_projects (
    id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    local_id TEXT NOT NULL,
    name TEXT NOT NULL,
    group_id TEXT NOT NULL,
    updated_at_ms INTEGER NOT NULL,
PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_projects_group ON pulse_projects (group_id);

-- 各设备会话完整结构化快照
CREATE TABLE IF NOT EXISTS pulse_session_sources (
    id TEXT NOT NULL,
    session_key TEXT NOT NULL,
    client_id TEXT NOT NULL,
    home_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    revision INTEGER NOT NULL,
    collected_at_ms INTEGER NOT NULL,
    digest TEXT NOT NULL,
    payload TEXT NOT NULL,
PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_session_sources_session ON pulse_session_sources (session_key);

-- 中心会话可查询元数据与合并状态
CREATE TABLE IF NOT EXISTS pulse_sessions (
    id TEXT NOT NULL,
    provider TEXT NOT NULL,
    session_id TEXT NOT NULL,
    title TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    session_kind TEXT NOT NULL,
    history_start_at_ms INTEGER NOT NULL,
    canonical_source_id TEXT NOT NULL,
    created_at_ms INTEGER,
    last_active_at_ms INTEGER,
    canonical_revision INTEGER NOT NULL,
    collected_at_ms INTEGER NOT NULL,
    complete INTEGER NOT NULL,
    conflict INTEGER NOT NULL,
    correction_fence INTEGER NOT NULL,
    deleted INTEGER NOT NULL,
PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_sessions_activity ON pulse_sessions (last_active_at_ms, id);

CREATE INDEX IF NOT EXISTS idx_sessions_project ON pulse_sessions (project_id, last_active_at_ms, id);

-- 中心去重结构化Token与历史成本贡献
CREATE TABLE IF NOT EXISTS pulse_usage (
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
PRIMARY KEY (session_key, contribution_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS unq_usage_position ON pulse_usage (session_key, position);

CREATE INDEX IF NOT EXISTS idx_usage_time ON pulse_usage (observed_at_ms, session_key);

-- 来源确认的账号资料与本机scope关联
CREATE TABLE IF NOT EXISTS pulse_accounts (
    id TEXT NOT NULL,
    provider TEXT NOT NULL,
    account_id TEXT NOT NULL,
    email TEXT,
    plan TEXT,
    collected_at_ms INTEGER NOT NULL,
PRIMARY KEY (id)
);

-- 账号配额原始可信观测及来源
CREATE TABLE IF NOT EXISTS pulse_quota_observations (
    id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    account_key TEXT,
    local_scope TEXT NOT NULL,
    observation_id TEXT NOT NULL,
    limit_id TEXT NOT NULL,
    window_kind TEXT NOT NULL,
    window_minutes INTEGER,
    window_start_at_ms INTEGER,
    association_scope TEXT,
    resets_at_ms INTEGER,
    observed_at_ms INTEGER NOT NULL,
    used_percent REAL,
    validity TEXT NOT NULL,
    source TEXT NOT NULL,
    history_origin TEXT NOT NULL,
    received_at_ms INTEGER NOT NULL,
PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_quota_account_window ON pulse_quota_observations (account_key, limit_id, window_kind, observed_at_ms);

CREATE INDEX IF NOT EXISTS idx_quota_pending ON pulse_quota_observations (client_id, local_scope, observed_at_ms);

-- 允许的Reset Credits库存状态快照
CREATE TABLE IF NOT EXISTS pulse_reset_credits (
    id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    account_key TEXT,
    local_scope TEXT NOT NULL,
    observed_at_ms INTEGER NOT NULL,
    inventory INTEGER,
    status TEXT NOT NULL,
    next_reset_at_ms INTEGER,
    next_expires_at_ms INTEGER,
    details_status TEXT NOT NULL,
    expiry_schedule TEXT NOT NULL,
PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_credits_account ON pulse_reset_credits (account_key, observed_at_ms);

-- 设备各Provider采集覆盖和同步状态
CREATE TABLE IF NOT EXISTS pulse_device_status (
    client_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    version TEXT NOT NULL,
    collected_at_ms INTEGER,
    coverage_start_ms INTEGER,
    coverage_end_ms INTEGER,
    pending_batches INTEGER NOT NULL,
    status TEXT NOT NULL,
    received_at_ms INTEGER NOT NULL,
PRIMARY KEY (client_id, provider)
);

-- 保留已接受的白名单快照，部分修订不能抹去可信历史
CREATE TABLE IF NOT EXISTS pulse_session_canonical (
    session_key TEXT NOT NULL,
    payload TEXT NOT NULL,
PRIMARY KEY (session_key)
);

-- 中心去重工具与技能统计，不含参数输出
CREATE TABLE IF NOT EXISTS pulse_invocations (
    session_key TEXT NOT NULL,
    invocation_id TEXT NOT NULL,
    observed_at_ms INTEGER NOT NULL,
    kind TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    outcome TEXT NOT NULL,
    duration_ms INTEGER,
PRIMARY KEY (session_key, invocation_id)
);
CREATE INDEX IF NOT EXISTS idx_invocations_time ON pulse_invocations (observed_at_ms, session_key);

-- 同一采集设备确认的本地scope与真实账号关联
CREATE TABLE IF NOT EXISTS pulse_account_bindings (
    id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    local_scope TEXT NOT NULL,
    account_key TEXT NOT NULL,
    confirmed_at_ms INTEGER NOT NULL,
PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_bindings_account ON pulse_account_bindings (account_key, client_id);

-- 中心手动账号订阅设置，不修改设备事实。
CREATE TABLE IF NOT EXISTS pulse_account_settings (
    account_key TEXT NOT NULL,
    revision INTEGER NOT NULL,
    alias TEXT,
    manual_plan TEXT,
    date_kind TEXT NOT NULL,
    renewal_day INTEGER,
    membership_date TEXT,
    time_zone TEXT NOT NULL,
    updated_at_ms INTEGER NOT NULL,
PRIMARY KEY (account_key),
CHECK (revision > 0),
CHECK ((date_kind = '' AND renewal_day IS NULL AND membership_date IS NULL) OR (date_kind = 'monthly_renewal' AND renewal_day IS NOT NULL AND renewal_day BETWEEN 1 AND 31 AND membership_date IS NULL) OR (date_kind = 'membership_expiry' AND renewal_day IS NULL AND membership_date IS NOT NULL))
);
