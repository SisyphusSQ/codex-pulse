# 未发布结构

TOO-478：中心初始结构 v1，完整初始化 SQL 位于 ../schema/center_mysql.sql 和 center_sqlite.sql，由运行时嵌入；不复制成第二份初始化 SQL。没有已发布中心版本，不建立虚构 releases/vX.Y.Z。

SQLite 开发场景覆盖初始化、重入、重启读回、不兼容拒绝、事务回滚和唯一约束。MySQL 实际执行 Not Run，待用户提供环境；正式版本归档时记录真实 tag、摘要及执行证据。

TOO-483：在同一个未发布初始结构中增加原始 default 历史的 association_scope、实际 window_start_at_ms、Credits 详情状态/到期汇总/next_expires_at_ms；与 reset 分开。两种 dialect 完整 SQL 和 DO 同步，SQLite 新隔离库验证通过。旧开发库摘要不匹配时拒绝启动，不自动 ALTER 或覆盖；没有已发布的升级前提，因此不提供无适用对象的重复升级 SQL。真实 MySQL 执行仍为 Not Run。
