# Codex Pulse 中心服务

由 Go Web Starter v2.0.0 生成，Go 1.27.1、Echo v5、Uber Fx。目标数据库为 MySQL；当前以独立 SQLite 开发验证，不装配本机 App 采集运行时。前端源码位于 `web/`，正式构建通过 Go `embed` 编入 Server，单个二进制同时提供 Web 和 API。业务目录已手工同步 Starter v2.0.1 规范，最初生成来源保留为 v2.0.0。

填写 Git 忽略的开发 MySQL 配置后，在 `server/` 运行：

```sh
make web-install                 # 首次或 package-lock 变化时安装构建依赖
make build-center                # 先构建 Web，再编入 Go Server
make run-center CONFIG="$PWD/config/development.local.yml" # 启动自动准备表结构
```

原有 `config.yml` 保留为私有 `.runtime/pulse.sqlite` 的环回 SQLite 验证入口；开发与正式 MySQL 使用下述本地配置。启动时自动初始化新库中的表或执行已登记的版本升级，再校验版本、摘要和字段；失败时 HTTP 不开始监听，已有数据保留。普通部署不再要求手工运行 `db init`、`db upgrade` 或 SQL。`db bootstrap` 在受信任终端显示 10 分钟有效、一次性管理员码，不在网络提供匿名管理员签发。Web 使用该码建立浏览器授权，管理员再签发采集设备码。

开发与正式两套 MySQL 模板、本地账密文件及二进制启动命令见[环境配置](config/README.md)。本机已准备 `config/development.local.yml` 与 `config/production.local.yml`，均被 Git 忽略；实际账密由使用者填写，不写回 `*.example.yml`。运行环境不安装或使用 Docker。

## 首次浏览器授权与访问恢复

Server 启动后，直接打开中心地址（开发模板为 `http://127.0.0.1:18089/`，正式模板由 HTTPS 代理转发到 `127.0.0.1:18090`）。没有浏览器授权时会显示“浏览器授权”页；页面及其静态资源允许匿名加载，统计数据和管理 API 仍要求授权。首次访问不需要已有的管理浏览器。

1. 在 **Server 所在机器的受信任终端**生成管理员配对码。在 `server/` 目录使用默认配置时运行 `bin/codex-pulse-server db bootstrap`；自定义部署必须使用与运行中 Server **相同的配置文件和数据库**：

   ```sh
   /opt/codex-pulse/bin/codex-pulse-server --config /etc/codex-pulse/config.yml db bootstrap
   ```

   上述绝对路径是部署示例，请替换为实际路径。首次部署先启动 Server，服务会自动准备表结构；随后在另一个终端执行 `db bootstrap`。该命令只访问已准备好的中心数据库，不启动 HTTP 服务或本机采集。
2. 终端会显示一次性管理员配对码及到期时间。码有效期为 **10 分钟**，不要放入 URL、日志、截图、仓库或公开文档。
3. 在目标浏览器的“浏览器授权”页输入该码，点击“配对并进入”。服务端设置 HttpOnly Cookie，页面进入中心看板。配对码消费后不能用于第二个浏览器。
4. 浏览器授权当前有效期为 **14 天**，刷新页面可恢复已有授权；过期、撤销、退出授权或 Cookie 被清理后需要重新配对。协议、域名或端口不同的入口须分别授权，HTTP 与 HTTPS 不共享授权。

已有管理浏览器可在“设备与授权”中签发新的浏览器配对码或采集设备码。**采集设备码只用于原生 App 上报，不能登录 Web 管理页面。**

如果所有浏览器都无法进入管理页面，部署者仍可在 Server 终端再次执行同一配置下的 `db bootstrap`，生成新的管理员码恢复访问；不需要重建数据库或删除已有统计。该命令也不会撤销现有客户端，确需撤销时在恢复授权后单独处理。

访问不到页面时先检查服务是否启动、是否使用已构建的中心二进制、监听地址与网络是否可达。默认只监听环回；跨机器访问须按[运行说明](docs/test/operations.md)配置明确的私网 HTTP 或 HTTPS 入口。页面能打开但配对失败时，检查码是否过期/已使用、用途是否为浏览器，以及命令是否指向运行中服务的同一数据库。

Web 使用 React/TypeScript/Vite/AntD/ECharts，源码说明见 [web/](web/README.md)。部署只运行 `codex-pulse-server`，无需 Node、Vite 或外置 Web 目录；启动后 `/` 与 `/assets/*` 直接提供二进制内的同版本页面，API 同源。`make build`、`release` 和各平台构建均先生成 Web 资源；直接使用 Go 命令前须先执行 `make web-build`。

CORS 默认 `server.corsOrigins: ["*"]`，通配模式不设置 `Access-Control-Allow-Credentials`。内嵌页面通过同源 Cookie 使用 API，入口仍由精确的 `server.origins` 校验，状态变更仍要求 Origin 与 CSRF。需要限制跨域时可以改为精确 Origin 列表，或显式 `[]` 关闭 CORS。从旧开发配置升级时删除 `server.webDirectory` 与 `APP_SERVER_WEBDIRECTORY`；运行时不再读取外置 Web。

Dockerfile 保留为可选构建描述，当前采用二进制部署，不安装或运行 Docker。二进制交付只需要可执行文件与私有配置；启动自动处理受支持的表结构升级。详细步骤见[运行说明](docs/test/operations.md)。

中心 Prometheus 采集与 Grafana 运行大盘见[运行监控](docs/monitoring/README.md)，覆盖资源、HTTP、数据库与各机器同步状态；专用只读 Token 与业务授权独立。

HTTP/HTTPS 共用统一配对、凭证摘要、用途和撤销体系，无 Basic、AK 或 JWT 第二套登录。浏览器使用入口绑定的 HttpOnly/SameSite Cookie 和 CSRF；HTTPS Cookie 额外 Secure。采集设备用独立 Bearer，仅允许自己的上报/同步状态。上报 DTO 不携带 Agent 凭据或原始内容。

- [总体设计与任务入口](../docs/design/details/multi-machine-reporting/README.md)
- [网络协议](api/README.md)
- [统计查询与覆盖口径](api/statistics.md)
- [业务子包与 DO 组织](docs/design/architecture/packages.md)
- [构建、常驻与备份恢复](docs/test/operations.md)
- [配置](docs/design/details/runtime/configuration.md)
- [SQL 结构与验证边界](docs/sqls/schema/README.md)

`.starter.json` 记录生成来源，不授权覆盖业务修改。生成后不再次运行生成器覆盖本目录。实际 MySQL、三机产品与生产部署验收单独记录；构建或 SQLite 测试不代表这些项目已通过。

中心查询与精简：`server.quotaMaintenance` 默认关闭；完成备份后显式启用，中心启动及每小时保留四个已观测额度周期，精简结束周期的重复状态。Session 查询投影独立分批补建，新上报在同一事务内维护。Web 使用账号/额度摘要、选中窗口节奏及分页证据，详见[设计与回滚边界](../docs/design/details/multi-machine-reporting/center-query-performance.md)。
