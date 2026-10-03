# v0.15.1 SQL 发布档案

TOO-523，计划发布日期 2026-10-03，代码身份为 Git tag `v0.15.1` 的 peeled commit；对应根 [CHANGELOG](../../../../../CHANGELOG.md)。本目录保留实际交付 SQL 的精确字节副本，供发行审计，不独立修改或作为第二套运行时来源。启动唯一来源仍为 [MySQL v3](../../schema/v3_mysql.sql) 与 [SQLite v3](../../schema/v3_sqlite.sql)。

| 文件 | SHA-256 |
|---|---|
| [v3_mysql.sql](v3_mysql.sql) | `af52d252a5f90c2ccdcb831d69738bd2c9c1bded82d661c56dfbea6ab071c0de` |
| [v3_sqlite.sql](v3_sqlite.sql) | `5f8429a7fee8bcdebba1559c99cc0ee73b6a054cdf9ab9bf7278c1c9660bd122` |

前置为已确认 v1/v2 摘要或新建空库。先准备已登记的基础结构，再新增分片暂存及设备同步表；读回全部字段后提交 v3 标记。两种 dialect 分别执行，不在同一库混用。SQLite 使用事务，MySQL DDL 可能部分提交；失败保留原库，检查实际结构后重入，不手改版本、不删除旧事实。

已有证据：SQLite 新建与 v1/v2 升级场景通过；DEV SeekDB 的 MySQL 协议入口已完成备份、独立空库恢复演练、v2→v3 和 15 张既有表指纹保留。独立 MySQL 8.4 未执行；生产执行在部署阶段另行回写 [多机 runbook](../../../../../docs/test/multi-machine-reporting.md)，归档本身不代表已上线。

v3 不支持只切回 v0.15.0 二进制。恢复旧快照须使用新库和匹配旧程序，恢复授权失效、备份后已确认事实需另行对账。
