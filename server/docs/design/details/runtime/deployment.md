# 中心部署入口

正式命令、SQLite/MySQL 备份恢复、HTTPS/私网 HTTP、保留与回滚统一见 [运行说明](../../../test/operations.md)。统一设备码认证，无 AK / Basic 第二套体系。

当前使用 `make package-center` 构建的单体二进制，Web 已内嵌；非 root 常驻示例在 `deploy/`。开发与正式配置见[环境配置](../../../../config/README.md)，真实账密只填 Git 忽略的本地副本。启动自动初始化或升级已支持的结构，成功后才开始 HTTP 监听；不自动安装系统服务或执行发布。

Dockerfile 保留为可选构建描述，本阶段不安装或运行 Docker。若以后另行使用，构建上下文为仓库根，多阶段生成 Web 后嵌入 Go，最终非 root 运行；不把本地私有配置带入镜像。`server/compose.yml` 仅为可选 MySQL 描述，不代表当前部署方式。DEV SeekDB/MySQL 协议与 Tailscale 证据见[联调记录](../../../../../docs/test/multi-machine-reporting.md)，正式环境另行验收。

部署包包含二进制、两套公开 `config/*.example.yml`、启动 README、运维说明和 deploy/scripts 示例；不包含 `*.local.yml`、外置 Web 或本机证据。运行配置由部署机器自行填写，`package-center` 拒绝覆盖已有包。

`/health` 仅存活，`/ready` 数据库就绪；`/metrics` 由管理员会话或独立只读 Bearer Token 保护。采集器使用私有 `server.metricsTokenFile`，该 Token 无业务读取或写入权限。日志和 SIGINT/SIGTERM 沿用 Fx 生命周期，详细边界见运行说明。

macOS 使用 `scripts/macos-service.py` 管理两套独立用户 LaunchAgent，安装/启动/停止/重启/status/remove 说明见[部署 README](../../../../deploy/README.md#macos-常驻)。该服务只托管中心，不改变本机 App/Helper 生命周期。
