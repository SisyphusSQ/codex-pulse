-- TOO-724 / v9，适用已登记的 MySQL 8.x、SeekDB 与 SQLite 中心库。
-- 先停止旧中心写入并备份。启动迁移器在同一事务复制有效旧投影、建立发布指针并更新版本。
-- 已有发布指针的会话继续采用该发布版本。迁移可重入，失败回滚整个数据切换。
INSERT INTO pulse_usage_versions (
    generation, session_key, contribution_id, position, observed_at_ms, model,
    input_tokens, cached_tokens, cache_write_tokens, output_tokens, reasoning_tokens, total_tokens,
    cost_micro_usd, reported_charge_micro_usd, pricing_version, pricing_mode,
    input_price, cached_price, cache_write_price, output_price, cost_status
)
SELECT
    legacy_usage.session_key, legacy_usage.session_key, legacy_usage.contribution_id, legacy_usage.position, legacy_usage.observed_at_ms, legacy_usage.model,
    legacy_usage.input_tokens, legacy_usage.cached_tokens, legacy_usage.cache_write_tokens, legacy_usage.output_tokens, legacy_usage.reasoning_tokens, legacy_usage.total_tokens,
    legacy_usage.cost_micro_usd, legacy_usage.reported_charge_micro_usd, legacy_usage.pricing_version, legacy_usage.pricing_mode,
    legacy_usage.input_price, legacy_usage.cached_price, legacy_usage.cache_write_price, legacy_usage.output_price, legacy_usage.cost_status
FROM pulse_usage AS legacy_usage
WHERE NOT EXISTS (SELECT 1 FROM pulse_session_publications AS published WHERE published.session_key = legacy_usage.session_key);

INSERT INTO pulse_session_publications (session_key, generation)
SELECT DISTINCT legacy_usage.session_key, legacy_usage.session_key
FROM pulse_usage AS legacy_usage
WHERE NOT EXISTS (SELECT 1 FROM pulse_session_publications AS published WHERE published.session_key = legacy_usage.session_key);
