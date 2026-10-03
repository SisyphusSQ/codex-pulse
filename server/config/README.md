# 开发与正式环境配置

当前采用内嵌 Web 的二进制部署，只保留开发环境与正式环境；不安装或运行 Docker。Dockerfile 可保留作为构建描述，部署机器无需 Go、Node 或前端目录。

| 环境 | 可提交模板 | 本地填写文件（Git 忽略） | 默认服务入口 | 默认 MySQL 数据库 |
| --- | --- | --- | --- | --- |
| 开发 | [development.example.yml](development.example.yml) | `development.local.yml` | `http://127.0.0.1:18089` | `pulse_center_development` |
| 正式 | [production.example.yml](production.example.yml) | `production.local.yml` | HTTPS 域名，Server 监听 `127.0.0.1:18090` | `pulse_center` |

两个环境的 Web/Server 监听端口不同，MySQL 的 `database.port` 仍匹配实际数据库实例，默认 3306。跨机器 DEV 访问先用 `tailscale status --json` 确认当前服务器 IP，再同时修改 `server.address` 与精确 `server.origins`，绑定该 Tailscale IP 的 18089 端口。

本机已准备两份 `*.local.yml` 空账密副本，权限为 0600。其他 checkout 可在 `server/` 目录创建；目标已存在时不要覆盖：

```sh
umask 077
cp -n config/development.example.yml config/development.local.yml
cp -n config/production.example.yml config/production.local.yml
chmod 600 config/development.local.yml config/production.local.yml
```

在本地副本填写 `database.host`、`port`、`database`、`username`、`password`，两个环境使用独立数据库。模板的用户名、库名和地址都是占位示例，MySQL 数据库本身须已建立；Server 自动管理其中的 Pulse 表，不创建数据库或账号。运行账号需要本库业务读写与所交付迁移所需的 DDL 权限，不需要全局权限。TLS 默认开启并校验证书；只有确认的私有无 TLS 数据库才显式填写 `tls: "false"`。

正式环境同时替换 `server.origins` 的 HTTPS 域名。模板通过同机代理终止 HTTPS，代理必须覆盖 forwarded headers，`trustedProxies` 只信任同机 IPv4 环回；直接私网 HTTP 参照[网络配置](../docs/test/operations.md#网络与常驻)。正式日志默认相对服务工作目录写入 `.runtime/production/logs/server.log`；服务用户必须有该私有目录的写权限。Linux systemd 示例仅放行 `/var/lib/codex-pulse`，使用时将日志配置改到该目录下。

`config.yml` 保留为原有环回 SQLite 验证入口，正式二进制部署始终显式选择本地配置。`APP_` 环境变量优先于文件；切换环境时确认没有沿用另一环境的数据库覆盖值。CORS 默认 `*`，内嵌页面使用同源 Cookie，Origin/CSRF 和权限校验保持。

## 二进制启动

构建机器在 `server/` 执行 `make web-install`、`make build-center`；部署机器只复制生成的二进制与自己的私有配置。以下示例在 `server/` 运行，正式环境把配置文件改为 `production.local.yml`：

```sh
bin/codex-pulse-server --config "$PWD/config/development.local.yml" http
```

启动自动初始化新库中的表，或将已确认的旧结构升级到二进制携带的版本；用户无需执行建表 SQL、`db init` 或 `db upgrade`。`database.migrationTimeout` 默认 2 分钟，缺省时也生效。未知版本、结构漂移、权限不足或迁移失败会阻止 HTTP 启动，保留已有数据；不会降级或删除历史。后续结构变化由开发者随二进制交付版本化迁移。

服务启动成功后，在另一个受信任终端用相同配置生成浏览器授权码：

```sh
bin/codex-pulse-server --config "$PWD/config/development.local.yml" db bootstrap
```

完整授权步骤见[首次浏览器授权](../README.md#首次浏览器授权与访问恢复)。日常升级只替换二进制并重启；保留常规数据库备份。`db check`、`db init` 与 `db upgrade` 保留为诊断和受控运维入口。

`server/.gitignore` 忽略 `config/*.local.*`，包括本地配置副本；镜像构建上下文也排除这些文件。真实地址、账密和环境配置不要写回 `*.example.yml` 或提交到 Git。DEV 数据库与跨机验证结果见[验证记录](../../docs/test/multi-machine-reporting.md)，正式环境是否上线须单独验收。

## Prometheus 采集

`server.metrics: true` 开启 `/metrics`。使用 `server.metricsTokenFile`（或 `APP_SERVER_METRICSTOKENFILE`）指定本机私有的普通文件，权限 0600，内容为 32–256 位高熵 Token；部署时从密钥管理配置，不写进 YAML、仓库或日志。无有效文件时不能使用 Bearer 采集，仅已有管理员浏览器 Cookie 可读；配置文件不可读、权限不符或长度不符会阻止服务启动。此专用凭据只能 GET/HEAD `/metrics`，不能读取管理数据、提交事实或使用采集设备 API。改 Token 文件后重启加载；Cookie 授权和设备凭证保持独立。

Prometheus 配置示例（目标替换为真实私网/HTTPS 入口，Token 文件位于采集器所在机器）：

```yaml
scrape_configs:
  - job_name: codex-pulse-center
    metrics_path: /metrics
    scrape_interval: 30s
    static_configs:
      - targets: ["pulse.internal:18090"]
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/secrets/codex-pulse-center-token
```

HTTPS 入口配置 `scheme: https` 并使用正确 CA，不关闭证书校验。暴露 HTTP 请求/错误/时延、Go/process、数据库连接池与等待、上传请求/失败/时延，以及每个设备 Provider 的最后接收/原采集时间、待传快照和同步健康。设备健康仅在最近 15 分钟收到 ready 同步检查时为 1；旧 App 缺少检查时间时保持 0。抓取只扫描最多 3,000 条设备状态，不读取用量历史；数据库查询失败用 `pulse_metrics_database_up 0` 显示。标签不含邮箱、标题、路径或凭据，HTTP 路径与方法归一化。计数是上传请求数，包含幂等重试，不是新增用量。Token/费用/账号额度业务指标未纳入本次运维指标范围。
