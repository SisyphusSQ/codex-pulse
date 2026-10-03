-- TOO-523：中心结构 v3 新增分片暂存和有限同步状态。
-- 适用于 SQLite 3.35+，独立开发数据库。
-- 前置：空库初始化或已确认 v1/v2 摘要；启动迁移器执行并读回后写 v3 标记。
-- 副作用：仅新增两张表，保留旧表及历史；CREATE IF NOT EXISTS 可重入。
-- MySQL DDL 不保证事务回滚；失败检查结构后重入，不删除或重建表。
CREATE TABLE IF NOT EXISTS pulse_snapshot_chunks (
    client_id TEXT NOT NULL,
    source_key TEXT NOT NULL,
    revision INTEGER NOT NULL,
    chunk_index INTEGER NOT NULL,
    payload_bytes INTEGER NOT NULL,
    payload TEXT NOT NULL,
    received_at_ms INTEGER NOT NULL,
PRIMARY KEY (client_id, source_key, revision, chunk_index)
) STRICT;
CREATE TABLE IF NOT EXISTS pulse_device_sync (
    client_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    sync_state TEXT NOT NULL,
    sync_checked_at_ms INTEGER NOT NULL,
    full_sync_state TEXT NOT NULL,
PRIMARY KEY (client_id, provider)
) STRICT;
