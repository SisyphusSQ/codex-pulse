# 部署

Docker 使用多阶段构建、非 root 用户和固定 Go 版本。构建前执行 go mod tidy 生成 go.sum；镜像内不包含本机配置、运行日志或 .agents 的真实状态文件。

容器配置默认 AK 认证，必须提供自己的凭据。Compose 仅供本机联调，业务部署需要明确网络、秘密注入、资源限制和镜像发布目标。

- liveness：GET /health。
- readiness：GET /ready；仅返回通用 503，不暴露依赖地址或凭据。
- metrics：GET /metrics，沿用业务鉴权；Prometheus 抓取端需配置凭据，或在受控网关独立管理访问。
- 日志：默认 stdout JSON；request_id、method、route、status、duration 支持关联。
- SIGINT/SIGTERM：先停止接流量并等待请求结束，再按 Fx 逆序停止任务和连接；超过关闭期限返回失败并强制关闭 HTTP 连接。

新建环境先准备数据库及当前业务所需的表结构，再启用对应组件并启动服务。
