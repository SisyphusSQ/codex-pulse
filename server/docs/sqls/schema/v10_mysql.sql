-- TOO-725 机器用量范围索引，适用于已登记的 MySQL / SeekDB 中心。
-- 停止旧中心写入并备份后，由 Schema.Migrate 创建结构并从来源快照补建。
-- DDL 可重入。回填与版本标记同一事务，失败后保留旧数据和旧标记。
CREATE TABLE IF NOT EXISTS pulse_machine_usage_sources (
    session_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '中心会话键',
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '采集机器身份',
    source_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '同机器内仲裁选中的来源',
    session_id VARCHAR(255) COLLATE utf8mb4_bin NOT NULL COMMENT '用于检索的原始会话 ID',
    title VARCHAR(512) NOT NULL COMMENT '选中来源允许上报的会话标题',
    project_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '选中来源的机器项目键',
    collected_at_ms BIGINT NOT NULL COMMENT '选中来源的采集时间 UTC 毫秒',
    complete BIGINT NOT NULL COMMENT '选中来源的快照是否完整',
    conflict BIGINT NOT NULL COMMENT '同机器内多个来源是否冲突',
    first_observed_at_ms BIGINT NULL COMMENT '入选计时事实最早发生时间，UTC 毫秒',
    last_observed_at_ms BIGINT NULL COMMENT '入选计时事实最晚发生时间，UTC 毫秒',
    untimed_models LONGTEXT NULL COMMENT '各模型的无时间事实计数，NULL 表示不存在',
PRIMARY KEY (session_key, client_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Codex 单机已选来源的时间范围与覆盖';

CREATE INDEX idx_machine_usage_time ON pulse_machine_usage_sources (last_observed_at_ms,first_observed_at_ms);
