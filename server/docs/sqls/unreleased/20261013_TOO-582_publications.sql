-- TOO-582 / schema v6：持久化发布任务、不可见用量版本及分段快照。
-- 仅追加表，不更改或删除已有来源、分片、批次与配对。CREATE IF NOT EXISTS 支持失败重入。
CREATE TABLE IF NOT EXISTS pulse_publication_jobs (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    client_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    batch_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    digest VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    revision BIGINT NOT NULL,
    body LONGTEXT NOT NULL,
    state VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    failure_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at_ms BIGINT NOT NULL,
    next_attempt_at_ms BIGINT NOT NULL,
    lease_until_ms BIGINT NOT NULL,
    lease_token VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
PRIMARY KEY (id),
KEY idx_publication_jobs_ready (state,next_attempt_at_ms,lease_until_ms,created_at_ms,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS pulse_session_publications (
    session_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    generation VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
PRIMARY KEY (session_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS pulse_payload_parts (
    payload_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    part_index BIGINT NOT NULL,
    body MEDIUMBLOB NOT NULL,
    created_at_ms BIGINT NOT NULL,
PRIMARY KEY (payload_key, part_index),
KEY idx_payload_parts_age (created_at_ms,payload_key,part_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS pulse_usage_versions (
    generation CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    session_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '所属中心会话键',
    contribution_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '独立于设备与本地generation的贡献键',
    position BIGINT NOT NULL COMMENT '会话结构化贡献顺序',
    observed_at_ms BIGINT NULL COMMENT '来源贡献UTC毫秒',
    model VARCHAR(128) COLLATE utf8mb4_bin NULL COMMENT '规范模型标识，NULL为未知',
    input_tokens BIGINT NULL COMMENT '输入Token，NULL为未知，缓存可能为子集',
    cached_tokens BIGINT NULL COMMENT '缓存输入Token',
    cache_write_tokens BIGINT NULL COMMENT '独立缓存写入Token',
    output_tokens BIGINT NULL COMMENT '输出Token',
    reasoning_tokens BIGINT NULL COMMENT '推理Token，是否计入输出遵守Provider语义',
    total_tokens BIGINT NULL COMMENT '按Provider口径的总Token',
    cost_micro_usd BIGINT NULL COMMENT '历史API等价成本整数微美元',
    reported_charge_micro_usd BIGINT NULL COMMENT '来源明确的实际收费整数微美元',
    pricing_version VARCHAR(128) NULL COMMENT '不可变价格证据版本，NULL为未定价',
    pricing_mode VARCHAR(32) NOT NULL COMMENT '历史成本舍入口径',
    input_price BIGINT NULL COMMENT '输入微美元每百万Token',
    cached_price BIGINT NULL COMMENT '缓存读取微美元每百万Token',
    cache_write_price BIGINT NULL COMMENT '缓存写入微美元每百万Token',
    output_price BIGINT NULL COMMENT '输出微美元每百万Token',
    cost_status VARCHAR(32) NOT NULL COMMENT 'known、partial或unpriced',
PRIMARY KEY (generation, session_key, contribution_id),
UNIQUE KEY unq_usage_versions_position (generation, session_key, position),
KEY idx_usage_versions_time (observed_at_ms, session_key, generation)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='中心去重结构化Token与历史成本贡献';

CREATE TABLE IF NOT EXISTS pulse_payload_owners (
    id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    payload_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
PRIMARY KEY (id),
KEY idx_payload_owners_key (payload_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
