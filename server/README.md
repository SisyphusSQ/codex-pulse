# Codex Pulse 中心服务

由 Go Web Starter v2.0.0 生成，Go 1.27.1、Echo v5、Uber Fx。目标数据库为 MySQL；当前以独立 SQLite 开发验证，不装配本机 App 采集运行时。前端位于 `web/`。业务目录已手工同步 Starter v2.0.1 规范，最初生成来源保留为 v2.0.0。

```sh
make build-center
bin/codex-pulse-server db init
bin/codex-pulse-server db bootstrap
make run-center
```

默认配置使用私有 `.runtime/pulse.sqlite` 和环回 HTTP。`db init` 显式建表并读回，正常启动只检查结构；`db bootstrap` 在受信任终端显示 10 分钟有效、一次性管理员码，不在网络提供匿名管理员签发。Web 使用该码建立浏览器授权，管理员再签发采集设备码。

前端位于 [web/](web/README.md)，使用 React/TypeScript/Vite/AntD/ECharts。开发请求同源经 Vite 转发，明确允许 localhost/127.0.0.1:5173；Cookie/CSRF、撤销和错误语义沿用统一鉴权。

HTTP/HTTPS 共用统一配对、凭证摘要、用途和撤销体系，无 Basic、AK 或 JWT 第二套登录。浏览器使用入口绑定的 HttpOnly/SameSite Cookie 和 CSRF；HTTPS Cookie 额外 Secure。采集设备用独立 Bearer，仅允许自己的上报/同步状态。上报 DTO 不携带 Agent 凭据或原始内容。

- [总体设计与任务入口](../docs/design/details/multi-machine-reporting/README.md)
- [网络协议](api/README.md)
- [统计查询与覆盖口径](api/statistics.md)
- [业务子包与 DO 组织](docs/design/architecture/packages.md)
- [构建、常驻与备份恢复](docs/test/operations.md)
- [配置](docs/design/details/runtime/configuration.md)
- [SQL 结构与验证边界](docs/sqls/schema/README.md)

`.starter.json` 记录生成来源，不授权覆盖业务修改。生成后不再次运行生成器覆盖本目录。实际 MySQL、三机产品与生产部署验收单独记录；构建或 SQLite 测试不代表这些项目已通过。
