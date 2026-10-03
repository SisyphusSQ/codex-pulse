# Codex Pulse 二进制部署包

进入部署包目录后执行以下命令。Web 和表结构定义已内嵌；运行机器只需当前平台的二进制、私有配置与数据库，无需 Go、Node、前端目录或 Docker。

只有 DEV 与正式两套配置：DEV 默认监听环回 18089，正式默认监听环回 18090 并由同机代理提供 HTTPS。MySQL 连接端口在 `database.port` 配置，匹配实际数据库实例。二进制不创建数据库或账号，启动时自动管理其中的 Pulse 表。

```sh
umask 077
cp -n config/development.example.yml config/development.local.yml
cp -n config/production.example.yml config/production.local.yml
chmod 600 config/development.local.yml config/production.local.yml
```

在需要使用的本地文件填写实际 `database.host`、`port`、`database`、`username`、`password`。两个环境使用各自数据库；正式配置替换 `server.origins` 的 HTTPS 域名。私有配置只留部署机器，不提交或打入分发包。TLS 默认校验证书；仅已确认的同机/私有无 TLS 数据库可显式配置 `tls: "false"`，不自动降级。

```sh
./codex-pulse-server --config "$PWD/config/development.local.yml" http
# 正式启动将配置路径改为 production.local.yml。
```

HTTP 开始监听前自动初始化空库或执行已登记升级；失败时服务不开始监听，不删除历史或降级未知结构。日常升级替换完整二进制并重启，数据库迁移随二进制交付；升级前保留可恢复的常规备份。

服务启动成功后，在另一个受信任终端使用同一配置：

```sh
./codex-pulse-server --config "$PWD/config/development.local.yml" db bootstrap
```

打开中心网页，在“浏览器授权”输入终端显示的短期一次性管理员码。未授权用户可以加载页面，业务 API 仍受保护；码不放入 URL、日志或截图。管理员可签发采集设备码，原生 App 配对后明确设置补传范围、同步间隔并启用上报。失去全部浏览器授权时，用相同配置再次执行 bootstrap 恢复访问，已有统计保留。

Tailscale DEV 直连先实时执行 `tailscale status --json` 确认服务器地址，然后将 `server.address` 改为具体 Tailscale IP 的 18089 端口，`server.origins` 改为同一精确 HTTP 地址；`allowHTTP: true`。正式 HTTPS 代理与常驻配置见 `deploy/nginx.conf` 和 `deploy/codex-pulse-server.service`，示例 upstream 是 18090；安装前替换实际域名、证书、用户和路径，部署包不会自动安装服务。

`/health` 表示进程存活，`/ready` 表示数据库就绪，均不证明完整采集或生产验收。数据库备份恢复、回滚、权限和运行限制见 `OPERATIONS.md`。该文档中的仓库设计链接用于源码浏览，不是运行依赖。


## macOS 常驻

部署包包含 `scripts/macos-service.py`，使用 Python 3 与当前登录用户的 LaunchAgent。DEV 与正式各自有独立 label 和版本目录；默认根目录是 `~/Library/Application Support/Codex Pulse Center`，目录 0700、日志 0600。配置仍指向自己的 0600 私有文件，不复制进发行目录。

```sh
python3 scripts/macos-service.py install --environment production \
  --package "$PWD" --config /absolute/private/production.local.yml \
  --release-id v0.15.0-COMMIT
python3 scripts/macos-service.py status --environment production
python3 scripts/macos-service.py restart --environment production
python3 scripts/macos-service.py stop --environment production
python3 scripts/macos-service.py start --environment production
```

安装先复制到新的版本目录再切换 `current`；拒绝覆盖已有 release-id。更新仍保留数据库、配置和旧版本目录，结构兼容时可停服务后将 `current` 切回保留的旧目录再启动；结构不兼容须按恢复流程处理。`remove` 只卸载 LaunchAgent，保留发行文件和数据，不删除数据库。用户登录后自动启动、异常退出自动重启；没有登录的机器不会由用户 LaunchAgent 启动。`status` 的进程状态不等于 ready，启动后读回实际监听、`/ready`、版本和数据库。

生产 `log.output: file` 使用自身有界轮转；LaunchAgent stdout/stderr 用于启动诊断。私有日志不提交。数据库备份工具需要本机已有 MySQL 8.0+ 客户端及 GNU timeout 或 Python 3；数据恢复只允许新的空库，不能覆盖活动库。
