# 中心数据库结构

结构版本为 2，SQL 为唯一事实源，运行时直接嵌入这些文件。

- [MySQL 8.x 结构](center_mysql.sql)：InnoDB、utf8mb4，身份字段区分大小写；目标实例版本、SQL mode 和排序规则在真实联调前读回。
- [SQLite 开发结构](center_sqlite.sql)：与 MySQL 相同业务字段和唯一性契约，独立于本机 App 的 SQLite。

时间使用 UTC 整数毫秒以保留既有观测语义；金额用整数微美元，配额比例用 MySQL DECIMAL，未知保留 NULL。凭证只存摘要，快照 payload 仅包含 typed 白名单业务事实，不含原始事件或内容。

HTTP 启动在监听之前自动初始化空库或将已确认 v1 升级到 v2，再校验版本、DDL 摘要与字段；当前版本重复启动不会改写标记或历史。用户日常只更新并重启二进制，`db init`/`db upgrade`/`db check` 保留为运维工具。MySQL 迁移以连接级命名锁串行化，同一连接执行 DDL 与释放锁；失败丢弃连接，避免残留锁返回池中。[MySQL 锁语义](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html)。MySQL DDL 不保证整体回滚，全部 DDL 与字段读回成功才写版本，故障修复后可重启续行。SQLite 初始化与升级保留事务。未知版本/摘要与字段漂移不会被自动降级或覆盖。

现有 SQL 字节内容及头部历史说明保留，避免仅因执行方式变化就改写既有数据库使用的 checksum；当前调度规则以本页和启动迁移器为准。本次没有结构变更或版本号递增。后续新增结构由开发者交付独立版本 SQL 与验证证据，用户无需关心执行步骤。

SQLite 只证明开发测试结果，真实 MySQL 连接、DDL、锁、排序和备份恢复尚未验证；后续按整体联调 runbook 在 Master 记录。

结构 v1 尚未正式发布。当前开发迭代新增来源仲裁/调用统计/账号关联，并保留 nullable 的会话与贡献时间、缓存写入、历史费率和舍入口径。每次结构定义变化后 checksum 改变，旧的开发中心库会拒绝启动；使用新的隔离测试库，不覆盖本机应用库。已交付结构通过已登记升级演进并在启动时自动执行，不重写发布版本。

配额事实保存原始 local_scope、显式 association_scope 与实际 window_start_at_ms。Credits 保存有限库存、详情状态、按到期时间合并的 JSON 分布及 next_expires_at_ms；不含 raw credit ID，未知 next_reset_at_ms 保持 NULL。到期不被解释成 reset。
