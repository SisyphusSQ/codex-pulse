# 中心数据库结构

结构版本为 4，SQL 为唯一事实源，运行时直接嵌入这些文件。

- [MySQL 8.x 结构](center_mysql.sql)：InnoDB、utf8mb4，身份字段区分大小写；目标实例版本、SQL mode 和排序规则在真实联调前读回。
- [SQLite 开发结构](center_sqlite.sql)：与 MySQL 相同业务字段和唯一性契约，独立于本机 App 的 SQLite。

时间使用 UTC 整数毫秒以保留既有观测语义；金额用整数微美元，配额比例用 MySQL DECIMAL，未知保留 NULL。凭证只存摘要，快照 payload 仅包含 typed 白名单业务事实，不含原始事件或内容。

HTTP 启动在监听之前自动初始化空库或将已确认 v1/v2/v3 升级到 v4，再校验版本、DDL 摘要与字段；当前版本重复启动不会改写标记或历史。用户日常只更新并重启二进制，`db init`/`db upgrade`/`db check` 保留为运维工具。MySQL 迁移以连接级命名锁串行化，同一连接执行 DDL 与释放锁；失败丢弃连接，避免残留锁返回池中。[MySQL 锁语义](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html)。MySQL DDL 不保证整体回滚，全部 DDL 与字段读回成功才写版本，故障修复后可重启续行。SQLite 初始化与升级保留事务。未知版本/摘要与字段漂移不会被自动降级或覆盖。

现有 SQL 字节内容及头部历史说明保留，避免仅因执行方式变化就改写既有数据库使用的 checksum；当前调度规则以本页和启动迁移器为准。本次 TOO-523 以独立 [MySQL v3](v3_mysql.sql) / [SQLite v3](v3_sqlite.sql) 新增分片暂存和设备同步状态表；v3 完整结构为原 center_*.sql 加相应 v3 文件；当前 v4 继续追加 v4 文件，旧 SQL 字节不变。版本标记只在新增表与全部字段读回成功后更新。后续新增结构由开发者交付独立版本 SQL 与验证证据，用户无需关心执行步骤。

SQLite 只证明开发测试结果；SeekDB 的 MySQL 协议连接、DDL 和备份恢复有独立记录，不替代独立 MySQL 8.4 整体联调；后续按整体 runbook 在 Master 记录。

以下为 v1 最初开发阶段的历史说明。该阶段新增来源仲裁/调用统计/账号关联，并保留 nullable 的会话与贡献时间、缓存写入、历史费率和舍入口径。每次结构定义变化后 checksum 改变，旧的开发中心库会拒绝启动；使用新的隔离测试库，不覆盖本机应用库。已交付结构通过已登记升级演进并在启动时自动执行，不重写发布版本。

配额事实保存原始 local_scope、显式 association_scope 与实际 window_start_at_ms。Credits 保存有限库存、详情状态、按到期时间合并的 JSON 分布及 next_expires_at_ms；不含 raw credit ID，未知 next_reset_at_ms 保持 NULL。到期不被解释成 reset。

结构 v4 新增 [MySQL v4](v4_mysql.sql) / [SQLite v4](v4_sqlite.sql)：`pulse_session_capsules` 保存可重建的 TPS/缓存/完成状态投影，按 Session 索引；`pulse_retired_observations` 保存已精简额度事实的 ID 与摘要。迁移只新增表，后台精简由 `server.quotaMaintenance` 显式启用。v3 → v4 启动会读回新表字段后更新标记，SQLite 同时创建查询索引。四周期删除与重复观测精简须先备份，回滚到 v3 需要匹配旧二进制与备份库，不能只替换二进制。执行证据见[查询与保留验证](../../../../docs/test/center-query-retention-20261003.md)。
