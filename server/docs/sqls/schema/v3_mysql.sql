-- TOO-523：中心结构 v3 新增分片暂存和有限同步状态。
-- 适用于 MySQL 8.x，InnoDB / utf8mb4。
-- 前置：空库初始化或已确认 v1/v2 摘要；启动迁移器执行并读回后写 v3 标记。
-- 副作用：仅新增两张表，保留旧表及历史；CREATE IF NOT EXISTS 可重入。
-- MySQL DDL 不保证事务回滚；失败检查结构后重入，不删除或重建表。
CREATE TABLE IF NOT EXISTS pulse_snapshot_chunks (
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '已鉴权来源设备',
    source_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '设备来源会话键',
    revision BIGINT NOT NULL COMMENT '完整快照单调版本',
    chunk_index BIGINT NOT NULL COMMENT '片段序号从零开始',
    payload_bytes BIGINT NOT NULL COMMENT '服务端计算正文UTF8字节数',
    payload LONGTEXT NOT NULL COMMENT '白名单片段JSON',
    received_at_ms BIGINT NOT NULL COMMENT '片段接收UTC毫秒',
PRIMARY KEY (client_id, source_key, revision, chunk_index)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='完整快照的有界分片暂存';
CREATE TABLE IF NOT EXISTS pulse_device_sync (
    client_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '设备ID',
    provider VARCHAR(32) NOT NULL COMMENT 'Provider',
    sync_state VARCHAR(32) NOT NULL COMMENT '有限同步错误或就绪状态',
    sync_checked_at_ms BIGINT NOT NULL COMMENT '同步检查UTC毫秒',
    full_sync_state VARCHAR(32) NOT NULL COMMENT '全量任务状态',
PRIMARY KEY (client_id, provider)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='设备同步任务的有限状态';
