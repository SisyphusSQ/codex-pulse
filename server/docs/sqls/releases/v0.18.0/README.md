# v0.18.0 / schema v8

目标 tag `v0.18.0`，日期 2026-11-11。精确代码提交由该 signed tag 确定；公开发布与实际部署结果以 Release 和私有执行证据分别读回，不从文件存在推断完成。

- `20261111_TOO-702_paid_credits_mysql.sql`：SHA-256 `e9ecc017a437e1b89771bc08b7f7db681f0357ca58841e27d787b3643aaf8dea`。
- `20261111_TOO-702_paid_credits_sqlite.sql`：SHA-256 `e437610b006e8dd38ed4b44f4925afe3a5d4c400ceeb3f8f6b7fb1d76a54bf76`。

TOO-702 在已发布 v7 索引之上追加计费 Credits 表及索引，不改变既有 SQL 字节。运行时事实源仍为 schema/v8_*.sql，启动迁移器先校验已登记旧版摘要，创建新增结构并读回后推进版本；用户不手工重复执行 SQL。MySQL DDL 不能假设事务回滚，升级前备份一致数据库；失败不监听HTTP，不覆盖已有事实。

此前 SQLite Credits 验证使用旧基线；合并远端后顺延为 v8，提交和发布收尾按约定未重复测试。生产执行需单独记录。Windows 本版不构建、不替换；客户端数据结构 v39，中心 v8。根 CHANGELOG 与 docs/test/credits-rollout.md 说明能力与回滚边界。
