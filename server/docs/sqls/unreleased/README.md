# 未发布结构

TOO-478：中心初始结构 v1，完整初始化 SQL 位于 ../schema/center_mysql.sql 和 center_sqlite.sql，由运行时嵌入；不复制成第二份初始化 SQL。没有已发布中心版本，不建立虚构 releases/vX.Y.Z。

SQLite 开发场景覆盖初始化、重入、重启读回、不兼容拒绝、事务回滚和唯一约束。MySQL 实际执行 Not Run，待用户提供环境；正式版本归档时记录真实 tag、摘要及执行证据。
