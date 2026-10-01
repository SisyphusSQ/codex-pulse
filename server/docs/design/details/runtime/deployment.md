# 中心部署入口

正式命令、SQLite/MySQL 备份恢复、HTTPS/私网 HTTP、保留与回滚统一见 [运行说明](../../../test/operations.md)。统一设备码认证，无 AK / Basic 第二套体系。

推荐直接使用 `make package-center` 的同源二进制/Web 产物，非 root 常驻示例在 `deploy/`。不自动安装系统服务、不自动初始化结构、不执行发布。

可选容器从 monorepo 根作为 build context，先在 `server/` 执行 `make web-build`，再在仓库根执行 `docker build -f server/Dockerfile -t codex-pulse-center:local .`；根 `.dockerignore` 排除运行数据、日志、私有 env 和 node_modules。多阶段固定 Go/Alpine 构建，非 root 运行，包含同源 Web；运行时挂载私有配置或提供实际数据库秘密/入口/可信代理。HTTPS 经受信任代理终止，示例不默认开放匿名 API。

`server/compose.yml` 仅是明确启用的可选 MySQL 开发服务，端口环回；密码必须来自私有环境。当前无 Docker/MySQL 实际运行证据，不能用这些模板声称容器或 MySQL 验收通过，不会安装或启动它们。

`/health` 仅存活，`/ready` 数据库就绪；`/metrics` 管理授权保护。日志和 SIGINT/SIGTERM 沿用 Fx 生命周期，详细边界见运行说明。
