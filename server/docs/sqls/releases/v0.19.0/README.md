# v0.19.0 中心结构 v9 与 v10

本次随正式版本交付此前主线纳入的两次结构变更。运行时使用以下原始迁移文件。

- TOO-724 用量版本表切换见 [v9 迁移](../../schema/v9_usage_cutover.sql)。
- TOO-725 日期范围索引见 [MySQL / SeekDB v10](../../schema/v10_mysql.sql) 和 [SQLite v10](../../schema/v10_sqlite.sql)。

升级前保留一致数据库备份，沿用原配置，由启动迁移器核对结构与版本。已在 v10 的中心沿用现有结构。AGY 支持沿用这套结构。
