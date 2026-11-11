-- TOO-697：查询索引，适用于 MySQL 8.x 与已验证的 SeekDB MySQL 协议。
-- 前置为已登记结构；唯一入口为启动迁移器，先按名称/列序检查再创建，失败不写版本。
-- 只增加索引，不修改事实；DDL 可能持锁并消耗磁盘，不依赖事务回滚。重入复用已匹配索引。
CREATE INDEX idx_publication_jobs_lease ON pulse_publication_jobs (lease_until_ms);
CREATE INDEX idx_publication_jobs_cleanup ON pulse_publication_jobs (state,created_at_ms,id);
CREATE INDEX idx_publication_jobs_source ON pulse_publication_jobs (client_id,source_key,state,revision);
CREATE INDEX idx_publications_generation ON pulse_session_publications (generation);
