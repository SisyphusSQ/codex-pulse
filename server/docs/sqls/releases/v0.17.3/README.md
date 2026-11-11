# v0.17.3 / schema v7

代码 tag：`v0.17.3`；日期：2026-11-08；归属 TOO-697；变更见根 CHANGELOG。本次交付 Server 部署包及代码标签，不发布原生客户端更新。

- `20261108_TOO-697_query_indexes_mysql.sql`：SHA-256 `638ea59ca974d9cd87e3298ef638024d6d85cdc4973006ce7da0fb391120bfe8`；与 schema/v7_mysql.sql 字节一致。
- `20261108_TOO-697_query_indexes_sqlite.sql`：SHA-256 `f721a047d183b68edbd4b86d5a0f681e3d20da71e64a898fc33c61390cc42b04`；与 schema/v7_sqlite.sql 字节一致。

运行事实源为 [schema](../../schema/README.md)，此目录只归档。前置为空库或已登记摘要的 v1..v6；v6 仅额外接受已确认的 MySQL CRLF 摘要。顺序：一致备份 → 既有结构 → 四个查询索引 → 字段及索引名称/列序读回 → v7 标记。迁移器识别已有索引以支持中断后重入；同名异构索引与未知摘要拒绝，不覆盖数据。

MySQL DDL 可能持锁且不保证事务回滚，迁移失败不推进标记。升级不删除既有事实；旧版本二进制不接受 v7，回退须停写并按备份恢复旧库，再启动旧包，不能直接把软链接切回旧包。

测试环境与隔离 SeekDB 恢复演练已完成，见 [验收记录](../../../test/too-697-server-sql-scheduling.md)。生产在合并、标签与卡片关闭后部署，实际结果回写 TOO-697；本档案本身不证明生产已执行。未进行独立 MySQL 8.4 验收。
