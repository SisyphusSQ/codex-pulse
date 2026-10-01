-- Codex Pulse 中心结构 v1，MySQL 8.x（utf8mb4）或 SQLite 3.35+。
-- 仅通过显式 db init 初始化；CREATE IF NOT EXISTS 可重入，结构摘要最后提交。
-- 不自动迁移，不删除历史；MySQL DDL 不假设事务回滚，失败后检查实际结构再重入。
-- 时间保存UTC整数毫秒，金额保存整数微美元；NULL区别于零。

-- 中心结构版本与定义摘要
CREATE TABLE IF NOT EXISTS pulse_schema (
    id BIGINT NOT NULL COMMENT '结构记录唯一ID',
    version BIGINT NOT NULL COMMENT '当前结构版本',
    checksum CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '当前dialect DDL的SHA256',
    initialized_at_ms BIGINT NOT NULL COMMENT '初始化UTC毫秒',
PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='中心结构版本与定义摘要';

-- 统一浏览器与采集设备授权
CREATE TABLE IF NOT EXISTS pulse_clients (
    id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '服务端签发客户端ID',
    purpose VARCHAR(16) NOT NULL COMMENT 'admin或collector权限用途',
    name VARCHAR(128) NOT NULL COMMENT '客户端显示名称',
    secret_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '高熵服务凭证SHA256摘要',
    origin VARCHAR(255) NOT NULL COMMENT '浏览器会话绑定入口，采集设备为空',
    csrf_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '浏览器CSRF随机值摘要',
    created_at_ms BIGINT NOT NULL COMMENT '签发UTC毫秒',
    expires_at_ms BIGINT NULL COMMENT '有效期UTC毫秒，NULL为持续设备授权',
    revoked_at_ms BIGINT NULL COMMENT '撤销UTC毫秒，NULL为有效',
    last_received_at_ms BIGINT NULL COMMENT '最后成功上报UTC毫秒',
PRIMARY KEY (id),
UNIQUE KEY unq_clients_secret (secret_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='统一浏览器与采集设备授权';

-- 短期一次性设备码
CREATE TABLE IF NOT EXISTS pulse_pairings (
    code_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '一次性配对码SHA256摘要',
    purpose VARCHAR(16) NOT NULL COMMENT '服务端固定的授权用途',
    name VARCHAR(128) NOT NULL COMMENT '签发时客户端名称',
    created_at_ms BIGINT NOT NULL COMMENT '创建UTC毫秒',
    expires_at_ms BIGINT NOT NULL COMMENT '失效UTC毫秒',
    consumed_at_ms BIGINT NULL COMMENT '成功消费UTC毫秒',
PRIMARY KEY (code_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='短期一次性设备码';

-- 已原子提交批次与可重放确认
CREATE TABLE IF NOT EXISTS pulse_batches (
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '已鉴权采集客户端ID',
    batch_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '稳定批次ID',
    digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '允许payload的SHA256',
    received_at_ms BIGINT NOT NULL COMMENT '首次提交UTC毫秒',
PRIMARY KEY (client_id, batch_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='已原子提交批次与可重放确认';

-- 设备项目与显式跨机关联
CREATE TABLE IF NOT EXISTS pulse_projects (
    id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '设备与本地项目标识组合摘要',
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '采集设备ID',
    provider VARCHAR(32) COLLATE utf8mb4_bin NOT NULL COMMENT 'Agent Provider',
    local_id VARCHAR(255) COLLATE utf8mb4_bin NOT NULL COMMENT '本地安全项目标识，不含原始路径',
    name VARCHAR(255) NOT NULL COMMENT '允许的项目名称',
    group_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '中心项目组ID，默认仅自身',
    updated_at_ms BIGINT NOT NULL COMMENT '最后事实更新时间UTC毫秒',
PRIMARY KEY (id),
KEY idx_projects_group (group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='设备项目与显式跨机关联';

-- 各设备会话完整结构化快照
CREATE TABLE IF NOT EXISTS pulse_session_sources (
    id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '设备来源会话键摘要',
    session_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'Provider与原始会话ID组合摘要',
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '已鉴权来源设备ID',
    revision BIGINT NOT NULL COMMENT '来源单调快照修订',
    collected_at_ms BIGINT NOT NULL COMMENT '原始采集UTC毫秒',
    digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '快照允许字段SHA256',
    payload LONGTEXT NOT NULL COMMENT '白名单typed快照，不含正文或原始事件',
PRIMARY KEY (id),
KEY idx_session_sources_session (session_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='各设备会话完整结构化快照';

-- 中心会话可查询元数据与合并状态
CREATE TABLE IF NOT EXISTS pulse_sessions (
    id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'Provider与原始会话ID组合摘要',
    provider VARCHAR(32) COLLATE utf8mb4_bin NOT NULL COMMENT 'Agent Provider',
    session_id VARCHAR(255) COLLATE utf8mb4_bin NOT NULL COMMENT '原始Session ID，区分大小写',
    title VARCHAR(512) NOT NULL COMMENT '允许的会话标题',
    project_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '选定来源项目键',
    canonical_source_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '选定的可比较结构化快照来源',
    created_at_ms BIGINT NOT NULL COMMENT '来源会话创建UTC毫秒',
    last_active_at_ms BIGINT NOT NULL COMMENT '来源会话最后活动UTC毫秒',
    collected_at_ms BIGINT NOT NULL COMMENT '选定快照采集UTC毫秒',
    complete BIGINT NOT NULL COMMENT '完整覆盖为1，部分为0',
    conflict BIGINT NOT NULL COMMENT '不可比较来源冲突为1',
    deleted BIGINT NOT NULL COMMENT '所有来源已删除为1',
PRIMARY KEY (id),
KEY idx_sessions_activity (last_active_at_ms, id),
KEY idx_sessions_project (project_id, last_active_at_ms, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='中心会话可查询元数据与合并状态';

-- 中心去重结构化Token与历史成本贡献
CREATE TABLE IF NOT EXISTS pulse_usage (
    session_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '所属中心会话键',
    contribution_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '独立于设备与本地generation的贡献键',
    position BIGINT NOT NULL COMMENT '会话结构化贡献顺序',
    observed_at_ms BIGINT NOT NULL COMMENT '来源贡献UTC毫秒',
    model VARCHAR(128) COLLATE utf8mb4_bin NULL COMMENT '规范模型标识，NULL为未知',
    input_tokens BIGINT NULL COMMENT '输入Token，NULL为未知，缓存可能为子集',
    cached_tokens BIGINT NULL COMMENT '缓存输入Token',
    output_tokens BIGINT NULL COMMENT '输出Token',
    reasoning_tokens BIGINT NULL COMMENT '推理Token，是否计入输出遵守Provider语义',
    total_tokens BIGINT NULL COMMENT '按Provider口径的总Token',
    cost_micro_usd BIGINT NULL COMMENT '历史API等价成本整数微美元',
    reported_charge_micro_usd BIGINT NULL COMMENT '来源明确的实际收费整数微美元',
    pricing_version VARCHAR(128) NULL COMMENT '不可变价格证据版本，NULL为未定价',
    cost_status VARCHAR(32) NOT NULL COMMENT 'known、partial或unpriced',
PRIMARY KEY (session_key, contribution_id),
UNIQUE KEY unq_usage_position (session_key, position),
KEY idx_usage_time (observed_at_ms, session_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='中心去重结构化Token与历史成本贡献';

-- 来源确认的账号资料与本机scope关联
CREATE TABLE IF NOT EXISTS pulse_accounts (
    id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'Provider与原始账号ID组合摘要',
    provider VARCHAR(32) COLLATE utf8mb4_bin NOT NULL COMMENT 'Agent Provider',
    account_id VARCHAR(255) COLLATE utf8mb4_bin NOT NULL COMMENT '来源确认的原始账号ID，区分大小写',
    email VARCHAR(255) NULL COMMENT '允许的邮箱资料，NULL为未知',
    plan VARCHAR(128) NULL COMMENT '已确认套餐，NULL为未知',
    collected_at_ms BIGINT NOT NULL COMMENT '资料采集UTC毫秒',
PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='来源确认的账号资料与本机scope关联';

-- 账号配额原始可信观测及来源
CREATE TABLE IF NOT EXISTS pulse_quota_observations (
    id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '设备与稳定观测身份摘要',
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '来源采集设备ID',
    provider VARCHAR(32) COLLATE utf8mb4_bin NOT NULL COMMENT 'Agent Provider',
    account_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT '已确认中心账号键，NULL为待关联历史',
    local_scope VARCHAR(128) NOT NULL COMMENT '本机不可逆账号scope，用于受验证关联',
    observation_id VARCHAR(128) NOT NULL COMMENT '来源稳定观测ID',
    limit_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL COMMENT '原始安全limit标识',
    window_kind VARCHAR(64) COLLATE utf8mb4_bin NOT NULL COMMENT '来源窗口种类',
    window_minutes BIGINT NULL COMMENT '实际窗口分钟，NULL为未知',
    resets_at_ms BIGINT NULL COMMENT '真实reset UTC毫秒',
    observed_at_ms BIGINT NOT NULL COMMENT '原始观测UTC毫秒',
    used_percent DECIMAL(9,6) NULL COMMENT '已用比例0到100，NULL为未知',
    validity VARCHAR(32) NOT NULL COMMENT 'accepted、suspicious、rejected或unknown',
    source VARCHAR(64) NOT NULL COMMENT '有限来源类型',
    history_origin VARCHAR(64) NOT NULL COMMENT 'confirmed、legacy_unassigned等历史来源',
    received_at_ms BIGINT NOT NULL COMMENT '中心接收UTC毫秒',
PRIMARY KEY (id),
KEY idx_quota_account_window (account_key, limit_id, window_kind, observed_at_ms),
KEY idx_quota_pending (client_id, local_scope, observed_at_ms)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='账号配额原始可信观测及来源';

-- 允许的Reset Credits库存状态快照
CREATE TABLE IF NOT EXISTS pulse_reset_credits (
    id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '设备与库存观测身份摘要',
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '来源采集设备ID',
    account_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT '已确认中心账号键',
    local_scope VARCHAR(128) NOT NULL COMMENT '本地历史归属scope',
    observed_at_ms BIGINT NOT NULL COMMENT '原始采集UTC毫秒',
    inventory BIGINT NULL COMMENT '可验证库存，NULL为未知',
    status VARCHAR(32) NOT NULL COMMENT '有限可信状态码',
    next_reset_at_ms BIGINT NULL COMMENT '来源明确的下次重置UTC毫秒',
PRIMARY KEY (id),
KEY idx_credits_account (account_key, observed_at_ms)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='允许的Reset Credits库存状态快照';

-- 设备各Provider采集覆盖和同步状态
CREATE TABLE IF NOT EXISTS pulse_device_status (
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '来源设备ID',
    provider VARCHAR(32) COLLATE utf8mb4_bin NOT NULL COMMENT 'Agent Provider',
    version VARCHAR(64) NOT NULL COMMENT '采集应用版本',
    collected_at_ms BIGINT NULL COMMENT '最后成功采集UTC毫秒',
    coverage_start_ms BIGINT NULL COMMENT '已确认覆盖起点UTC毫秒',
    coverage_end_ms BIGINT NULL COMMENT '已确认覆盖终点UTC毫秒',
    pending_batches BIGINT NOT NULL COMMENT '本机待发送批次',
    status VARCHAR(32) NOT NULL COMMENT '有限同步状态码',
    received_at_ms BIGINT NOT NULL COMMENT '中心接收UTC毫秒',
PRIMARY KEY (client_id, provider)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='设备各Provider采集覆盖和同步状态';
