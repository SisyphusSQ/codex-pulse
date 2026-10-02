# 中心构建、运行与备份恢复

适用独立 Go Server + `server/web`。本机 App/Helper 的 UDS、pipe 鉴权和数据库保持原架构。只有 Server 常驻；退出 App 后采集与上报停止，下次启动增量补采。以下路径均为部署示例，不是已部署环境。

## 构建与首次启动

在仓库 `server/` 运行：

```sh
make web-install                 # npm ci，写 web/node_modules；首次或锁文件变更时
make build                      # 独立 Go 二进制，不运行测试
make web-build                  # 独立 Web 构建，写 web/dist
make package-center             # 写 bin/center；含二进制、web、deploy 和本文
bin/codex-pulse-server db init
bin/codex-pulse-server db check
bin/codex-pulse-server db bootstrap
make run-center
```

`package-center` 拒绝覆盖已有打包目录；重打包时用 `PACKAGE_DIR=/新目录`，保留旧产物并避免残留旧资源。

默认 SQLite 写 `.runtime/pulse.sqlite`，父目录 0700、文件 0600；不读 Agent Home。`db bootstrap` 只在受信任终端显示一次性短期管理员码，不将码放入日志、URL、截图或文档。浏览器访问 `http://127.0.0.1:8080/`，输入码后签发 collector 设备码，原生设置中配对并明确启用。关闭报告时本机统计继续运行。配对和 HTTP 许可不会自动打开上报。

`server.webDirectory` 为空时只启用 API；启用时必须指向完整构建目录（index.html + assets），缺失使启动失败。HashRouter 深链接形如 `/#/quota`。静态入口只开放 GET/HEAD `/` 与 `/assets/*`，不提供目录列表、源码 map、隐藏文件或任意 SPA API 兜底。业务接口仍要求授权。更新采用完整产物目录，重启前后端同一版本；Web 不含秘密。二进制默认版本为 `dev-<commit>`（有工作树改动附加 `-dirty`），正式中心版本需另行发布；原生 App 的 tag 不等于中心已发布。管理端页脚及 GET `/api/v1/version` 读回构建、上报/TPS 协议及 schema；不返回环境或仓库 URL。

## 网络与常驻

* HTTPS：复制 `deploy/https-sqlite.yml` 或 `https-mysql.yml` 到仓库外私有配置；替换实际域名。Nginx 示例只信任同机环回代理，覆盖 forwarded headers，TLS 证书由现有部署方式管理。Server 本身监听环回 HTTP，`allowHTTP:false`，仅 HTTPS Origin 对外有效。
* 私网 HTTP：从 `private-http-sqlite.yml` 起步；Tailscale 使用 `tailscale status --json` 按 HostName 确认当前目标 IP，再同时填写具体监听 IP 和精确 HTTP Origin，不猜历史 IP，不监听 `0.0.0.0`。显式 `allowHTTP:true`，同样设备码/用途/撤销/CSRF。浏览器会话绑定入口，HTTP 和 HTTPS 分别配对；不会自动降级。未经 HTTPS 或 Tailscale 加密的 LAN 链路使用者须明确承担明文传输属性。
* `deploy/codex-pulse-server.service` 仅为 Linux 非 root 服务示例；安装前准备用户、0700 数据目录、0600 配置/env 文件和实际路径。MySQL 密码用私有 env `APP_DATABASE_PASSWORD` 或私有配置，禁止 shell trace。未安装/启动任何系统服务。macOS 可由现有服务管理方式启动相同命令，不向原生 App 添加 LaunchAgent。
* SIGINT/SIGTERM 停止接收并在 `server.shutdownTimeout` 内关闭请求和依赖；超时显式失败。`/health` 仅存活，`/ready` 含数据库检查，不代表数据完整、采集在线或配额新鲜。
* 请求 8 MiB、配对/管理 4 KiB；队列、查询、分页及 TPS 预算见 API 文档。日志仅 method/route/status/request_id/duration，示例按 20 MiB、30 份、7 天轮转。默认不自动删除统计、配额、收据或撤销记录。备份保留期由操作者按磁盘预算管理，确认可恢复的新副本后再明确删除指定旧备份；不是自动清库。

## SQLite 备份与恢复

数据库和备份都含允许的账号/项目/标题元数据与 Pulse 凭证摘要，必须私有保存；备份不含原始 JSONL、Agent 密钥或 Home。备份命令有默认 2 分钟整体截止，可用 `--timeout 5m`，允许 5 秒至 10 分钟。在线备份使用 SQLite `VACUUM INTO` 一致快照，不能直接复制活跃主文件而漏掉 WAL。

```sh
umask 077
mkdir -p /private/center-backups       # 示例：操作者改为自己的私有目录
chmod 700 /private/center-backups
bin/codex-pulse-server --config /private/server.yml db backup /private/center-backups/snapshot-001
```

目标目录必须不存在，成功包含 `center.sqlite`（0600）和 `manifest.json`（schema、构建标识、时间、SHA-256）。失败不产生完成副本。不要把备份或私有日志提交。备份可在线运行，但禁止同时变更结构。

恢复前正常停止待切换的中心，保留原库、配置和二进制。将私有恢复配置的 `database.path` 指向新目录中的**不存在文件**，其他网络配置保持实际入口：

```sh
bin/codex-pulse-server --config /private/restored-server.yml db restore /private/center-backups/snapshot-001
bin/codex-pulse-server --config /private/restored-server.yml db check
bin/codex-pulse-server --config /private/restored-server.yml db bootstrap
bin/codex-pulse-server --config /private/restored-server.yml http
```

恢复校验源与复制后摘要、当前结构及 SQLite integrity；在同目录临时库完成撤销与关闭后原子发布，拒绝覆盖任何已有目标。失败不切换入口。旧管理员/采集授权及未消费设备码全部失效，防止备份复活后来撤销的凭证；原统计、reset/observedAt、收据 digest/receivedAt 不改为当前值。使用新管理员码和新设备码重新配对；App 的旧授权报告 reconnect_required 并保留待确认队列。既有收据继续提供幂等依据，不把重配当作历史事实丢弃。

恢复只能回到备份截至的事实；备份之后已确认并被客户端移出队列的批次可能需要重新导出，不能只靠“队列为空”声明数据齐全。用设备来源、本地范围和中心总量核对；必要时在明确历史范围下重新导出，事实去重保持幂等。未观测的配额历史不重造。

## MySQL 运行与备份入口

目标 MySQL 8.4 / InnoDB / utf8mb4，建专用数据库；SQL 唯一事实源 `docs/sqls/schema/center_mysql.sql`，由 `db init` 显式执行并读回。运行用户与初始化/备份用户按用途分开授权。`https-mysql.yml` 默认 TLS true，系统信任库需信任实际数据库 CA 且证书主机名匹配；不关闭校验或自动降级。现有 Engine 对 UTC 时间、ClientFoundRows 和有界连接/读写等待统一配置。

```sh
bin/codex-pulse-server --config /private/mysql-server.yml db init
bin/codex-pulse-server --config /private/mysql-server.yml db check
bin/codex-pulse-server --config /private/mysql-server.yml db bootstrap
bash scripts/mysql-backup.sh backup /private/mysql-client.cnf pulse_center /private/center-backups/mysql-001
# 先准备新的专用空库 pulse_center_restored，禁止对活动库执行。
bash scripts/mysql-backup.sh restore /private/mysql-client.cnf pulse_center_restored /private/center-backups/mysql-001
bin/codex-pulse-server --config /private/mysql-restored-server.yml db check
```

使用 MySQL 8.4 `mysql`/`mysqldump`、`shasum` 和 GNU `timeout`（macOS 为 `gtimeout`）；不会安装工具。客户端配置复制自 `deploy/mysql-client.example.cnf` 到 0600 私有文件，密码不进入 argv。仅处理自己的受信任备份，不接受上传 SQL；恢复凭据限制为指定新库，禁止全局权限。备份单事务/quick/hex-blob/no-tablespaces/GTID OFF，不与 DDL 并行；输出与错误日志私有，整体 10 分钟截止。恢复先校验摘要并检查目标空库，不使用 `--force`，禁 LOCAL 文件导入和非交互客户端命令，恢复后撤销旧授权。MySQL DDL/导入失败可能留下部分新库：保留诊断，不启动、不切换、不假装事务回滚；修复后另建新空库重试。

恢复后读回 schema/checksum、表结构、业务数量/来源/NULL/零、quota/reset/TPS、收据幂等和授权撤销；再 bootstrap、浏览器及三设备重配、核对真实数据，才可切换。**当前没有真实 MySQL 环境，这些操作未运行。** Shell 语法和模板检查不构成数据库验收。

官方选项依据：[mysqldump](https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html)、[mysql 客户端](https://dev.mysql.com/doc/refman/8.4/en/mysql-command-options.html)。

## 升级、回滚和数据操作区别

正常启动只检查 schema/version/checksum，不 AutoMigrate。当前未发布 schema 1 若与早期开发库不符，会拒绝启动；不删库解决。未来结构升级须独立的版本 SQL、备份、执行与读回记录，不能对已初始化库用新全量 SQL 假装增量迁移。先升级支持 TPS 的中心，再升级 App；旧中心拒绝新字段时队列保留，未来 capsule 版本返回 426。

回滚先停服务，核对旧二进制对应结构。结构未变可切回完整旧产物；结构不兼容则恢复对应旧快照到新位置，和匹配二进制/配置成对切换，仍撤销恢复授权并重配；不盲降 schema、不删除原库、不隐瞒备份后数据缺口。

| 操作 | 后果 |
| --- | --- |
| 关闭 App 上报 | 本机统计继续；不发新批次，历史队列保留 |
| 退出 App | Helper 退出，无持续采集或上报；下次增量补采 |
| 撤销客户端 | 拒绝后续访问；中心已接收事实保留，不等于删除数据 |
| 清理待确认队列 | App 明确确认后允许修改历史范围；不是清理中心历史 |
| 删除中心历史/备份 | 当前无自动入口；必须独立说明范围与恢复后果 |
| 恢复/回滚 | 保留快照内事实，旧授权失效；备份后事实须对账 |

## Master 整体联调入口

Execution 只记录开发证据，Master TOO-475/476/477 承接正式验收。SQLite 确定性检查使用 synthetic/empty Home，Web 证据使用合成账号与记录。按三个实际主机分别绑定真实 Home + 0700 runtime，读回物理身份；验证配对开关、上传/重试/幂等、重复来源、网络断开/撤销/超预算、退出停止/重启补采、账号隔离、NULL/零/reset/节奏和 TPS 生命周期对账。真实 Home 运行不得上传超白名单内容。

记录逐项 Pass / Fail / Blocked / Not Run，不用构建替代三机、真实 MySQL、HTTPS 代理部署或 CI。生产部署、签名、公证与发布在本阶段未执行，不启用已关闭 CI。开发证据见根仓 `docs/test/multi-machine-reporting.md` 及本目录运行交付摘要。

## Web 静态资源目录更新

Server 通过受限目录句柄托管同源构建资源，运行中不要直接在该目录执行 Vite build 或替换 assets 子目录。构建到新的版本目录后，用新的 server.webDirectory 启动或重启 Server；保留上一份二进制与完整 Web 目录用于回滚。开发时若重建了当前预览目录，也需要重启隔离预览 Server，再刷新页面。单独刷新浏览器不能重新绑定被构建替换的目录。

## 结构v1至v2订阅设置升级

升级前备份中心数据库并停写，使用当前新二进制及原配置显式执行db upgrade；HTTP不会自动迁移。升级器只接受已确认v1摘要，增加pulse_account_settings，校验全部字段后提交v2标记。SQLite升级在事务内，MySQL DDL不可假设回滚。失败保留实际结构，检查错误后重入；不要手改标记或删除旧事实。升级后通过db check、保留表数量/记录读回及订阅保存验证。旧备份回退须匹配旧二进制与结构，不盲降schema。MySQL实机验收仍待环境。
