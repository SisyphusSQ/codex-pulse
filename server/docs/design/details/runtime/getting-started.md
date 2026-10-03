# 快速开始

中心通过单个 Go 二进制同时提供 Web 与 API，运行环境无需 Go、Node 或 Docker。构建环境使用 Go 1.27.1 与 Web 构建依赖；不要用 Starter 的 AK 或 debug 匿名模式替代中心配对认证。

在 `server/` 目录首次构建：

```sh
make web-install
make build-center
```

按[开发与正式环境配置](../../../../config/README.md)准备 Git 忽略、权限 0600 的 `development.local.yml` 或 `production.local.yml`。DEV 默认 Server 端口 18089，正式默认 18090；MySQL 连接端口另由实际数据库实例决定。数据库须已建立，表由二进制启动自动初始化或执行已登记升级。

```sh
bin/codex-pulse-server --config "$PWD/config/development.local.yml" http
```

启动成功后，在另一个受信任终端使用同一配置执行 `db bootstrap`，在浏览器输入短期管理员配对码；然后为原生 App 签发采集设备码。配对后上报保持关闭，设置历史范围、同步间隔并明确启用。未授权浏览器可以加载网页，业务 API 仍需鉴权；首次访问及恢复步骤见 [Server README](../../../../README.md#首次浏览器授权与访问恢复)。

跨机器访问先通过 `tailscale status --json` 确认目标，再将 DEV 的 `server.address` 绑定目标的具体 Tailscale IP，`server.origins` 填写同一 HTTP 地址与端口，并显式允许 HTTP。正式 HTTPS 使用同机代理，示例 upstream 为环回 18090；配置与常驻说明见[运行说明](../../../test/operations.md)。

`config/config.yml` 的环回 SQLite 是确定性验证入口。它不能代替实际数据库、跨机与生产验收；本地日志、配置、runtime 和凭据均不提交。
