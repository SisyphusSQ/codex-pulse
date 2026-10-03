# 未发布结构

以下 TOO-478/483/513 段为初始设计与开发历史；中心 v2 已随 v0.15.0 交付，段内当时的“未发布/未执行”不代表当前环境状态。TOO-523 v3 已转入 [v0.15.1 发布档案](../releases/v0.15.1/README.md)，运行时仍只使用 schema；没有其他待交付结构。

TOO-478：中心初始结构 v1，完整初始化 SQL 位于 ../schema/center_mysql.sql 和 center_sqlite.sql，由运行时嵌入；不复制成第二份初始化 SQL。没有已发布中心版本，不建立虚构 releases/vX.Y.Z。

SQLite 开发场景覆盖初始化、重入、重启读回、不兼容拒绝、事务回滚和唯一约束。MySQL 实际执行 Not Run，待用户提供环境；正式版本归档时记录真实 tag、摘要及执行证据。

TOO-483：在同一个未发布初始结构中增加原始 default 历史的 association_scope、实际 window_start_at_ms、Credits 详情状态/到期汇总/next_expires_at_ms；与 reset 分开。两种 dialect 完整 SQL 和 DO 同步，SQLite 新隔离库验证通过。旧开发库摘要不匹配时拒绝启动，不自动 ALTER 或覆盖；没有已发布的升级前提，因此不提供无适用对象的重复升级 SQL。真实 MySQL 执行仍为 Not Run。

TOO-513：中心结构v2新增独立订阅设置。新环境使用完整schema；v1 环境更新二进制后由启动迁移器自动升级；`db upgrade` 保留为运维入口，常规备份仍保留。对应20261002_TOO-513_subscriptions_mysql.sql和sqlite.sql仅新增表，由升级程序校验v1摘要、读回所有结构并提交版本标记。SQLite隔离升级验证通过；MySQL实际执行Not Run，不以SQLite替代。

TOO-523：结构 v3 增加分片暂存和设备有限同步状态。SQL 唯一事实源为 [MySQL v3](../schema/v3_mysql.sql) / [SQLite v3](../schema/v3_sqlite.sql)，不复制第二份 DDL；先保留 v1/v2 定义，再执行新增表与字段读回、更新标记。计划版本 v0.15.1。SQLite 初始化、v1/v2 升级与历史保留已验证；DEV SeekDB 的 MySQL 协议入口完成 v2 备份、恢复演练、v3 升级及既有表指纹保留。独立 MySQL 8.4 与生产执行尚未完成，详见根仓多机 runbook。
