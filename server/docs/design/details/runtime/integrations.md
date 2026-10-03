# 组件集成

## 数据库与 Redis

默认 disabled。启用后在 Fx OnStart 连接并验证连通性；就绪检查只检查这些已启用的必需资源。启动错误不会被降级为正常运行。关闭使用有期限的 context。

MySQL 目标为 8.x，数据库按新环境初始化。MongoDB 使用官方 Driver v2，不再依赖 qmgo。

## 鉴权

Basic 使用标准 Authorization；AK 使用 access_key / secret_key 头。所有业务路由和 `/metrics` 默认受保护，只有 `/health` 和 `/ready` 对外返回不含依赖详情的状态。

JWT 必须生成时选择 `jwt,redis`，运行时启用 Redis。校验 HS256、有效期、issuer 及 Redis 中当前有效令牌；键为 `<namespace>:jwt:user:<userID>`。签发方必须采用相同约定，默认没有账号系统。namespace 应按应用和环境隔离。

只有显式生成 MySQL User 示例、启用 MySQL 且选择 JWT 认证时才注册 `/login`、`/logout`。创建初始示例用户可在隔离环境先使用 Basic/AK 访问 CRUD，随后切到 JWT。示例登录使用进程内固定上限限速（5 次/秒、突发 10）；正式系统需按真实账户和部署拓扑设计限速。示例权限粒度只到认证，不代替正式业务的资源授权。

## 飞书和 Prometheus 查询

两者独立按 enabled 装配，使用有超时的客户端。配置目标属于运维可信配置，不得由 HTTP 请求直接透传 URL。HTTP 重定向默认不跟随，避免凭据被转发到其他目标。

PrometheusService.Query 接受调用方构造的 PromQL，返回结果和上游 warnings；没有内置 Java 堆内存业务查询。
Lark webhook URL 只允许在启动装配阶段由可信配置设定；不把 webhook URL 或消息正文写入日志。SDK 调用失败必须返回调用方，禁止用空结果冒充成功。

## 定时任务

cron 通过 Fx value group 注册业务 Job，默认没有任务。调度器仅保证本进程同一任务不重入，并在退出时取消任务 context、等待任务结束。多副本场景需要业务按实际语义配置单独调度实例或实现分布式协调；移除没有对应实现的 cron.lockTTL 配置，不能把本地防重入当作跨副本互斥。
