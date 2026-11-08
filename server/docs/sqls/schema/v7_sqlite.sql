-- TOO-697：SQLite 开发查询索引。由启动迁移器检查名称/列序后创建，可重入；事务内升级。
-- 仅新增索引，不删除事实；未知结构拒绝升级，失败不更新版本。
CREATE INDEX idx_publication_jobs_lease ON pulse_publication_jobs (lease_until_ms);
CREATE INDEX idx_publication_jobs_cleanup ON pulse_publication_jobs (state,created_at_ms,id);
CREATE INDEX idx_publication_jobs_source ON pulse_publication_jobs (client_id,source_key,state,revision);
CREATE INDEX idx_publications_generation ON pulse_session_publications (generation);
