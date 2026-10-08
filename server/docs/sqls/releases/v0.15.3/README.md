# v0.15.3 / schema v5

代码 tag：`v0.15.3`；发布日期：2026-10-05；归属 TOO-529。此目录记录该版本 SQL 交付，当前发布/部署结果见根仓验证记录；不会仅因归档宣称生产已执行。

- `20261005_TOO-529_account_tokens_mysql.sql`：SHA-256 `1c1fcecb58c94d6ea6f5deb9a3a915921769ea8ecb57ea0ba31d869965394e86`；精确复用 schema/v5_mysql.sql。
- `20261005_TOO-529_account_tokens_sqlite.sql`：SHA-256 `e4d70ab1c1a0f6ed13606d995a670d4608eb689a03cae917329659bd6e335ae2`；精确复用 schema/v5_sqlite.sql。

前置：空库或摘要匹配的已发布 v1/v2/v3/v4；先做一致备份。迁移顺序：既有完整结构 → v5 新增事实、来源、周期表/索引 → 字段读回 → marker v5。运行时嵌入 schema 自动执行，用户不需另行手工运行归档 SQL。只追加表、不回填、不删旧事实，IF NOT EXISTS 重入；MySQL DDL 不保证整体回滚，未完成字段读回不推进标记，失败后修复再重启，未知摘要拒绝。旧服务降级需旧备份库。

开发证据：SQLite v4→v5、既有数据保留、重入与不兼容拒绝通过。2026-10-05 SQMC04 生产 schema v4→v5 的读回见 [TOO-529 验证及交付记录](../../../../../docs/test/account-cycle-tokens.md)。本归档文件本身不是执行证明。该记录保留 Gatekeeper 拒绝，且不包含独立 MySQL 8.4 验收。
