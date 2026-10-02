# 架构与扩展

入口 `app/cmd/http.go` 加载并验证配置，初始化日志，再构造 Fx 应用。组件模块按配置装配；OnStart 依次建立资源，HTTP 最后开始监听；OnStop 先停止 HTTP，再释放依赖。启动失败由 Fx 回滚已成功启动的 hook，失败 hook 自己清理已打开资源。

| 层 | 责任 |
|---|---|
| controller | 解析和校验请求、调用 service、统一响应 |
| service | 业务规则、同库事务边界、模型转换 |
| repository | 绑定 context 的数据访问和存储错误转换 |
| models/do | 持久化模型 |
| models/dto | 跨边界输入和外部系统 payload |
| models/vo | 对外响应及既有请求校验模型 |
| lib | 按生命周期管理的基础组件 |

业务子包遵守 [目录规范](packages.md)，模型转换遵守 [模型约定](models.md)。新增业务按模型、repository、service、controller 顺序实现，并在相应 module.go 注册 Provide/Invoke。遵循现有边界，不为每个函数建立无调用需求的抽象。

SQL repository 必须使用 `engine.DB(ctx)`；`engine.Transaction(ctx, callback)` 内传给 callback 的 context 携带同一事务。回调返回错误或被取消即回滚。嵌套和跨数据库事务不支持。

cron 通过 Fx 的 `cron_jobs` value group 注入 `cron.Job`，包含 Name、Schedule、Run(context.Context)。调度器默认无任务，不自动计数或写 Redis。任务必须响应取消；同一进程内上次尚未完成时跳过重叠执行。多副本互斥需要业务显式采用锁或外部调度系统。

## 当前中心域映射

| 域 | Controller / Service | Repository / DO | 责任 |
| --- | --- | --- | --- |
| access | access_controller / access_srv | mysql/access_repo / mysql/access_do | 客户端、一次性码、认证、权限与撤销 |
| reporting | reporting_controller / reporting_srv | mysql/reporting_repo / mysql/reporting_do | 收到的白名单事实、幂等、仲裁与显式项目关联 |
| statistics | statistics_controller / statistics_srv | mysql/statistics_repo / 无自有 DO | 已接受事实的只读汇总、来源对账与检索 |
| quota | quota_controller / quota_srv | mysql/quota_repo / 无自有 DO | 复用 reporting 事实及纯 Go 规则的中心周期、可信额度与 Credits |
| catalog | catalog_controller / catalog_srv | mysql/catalog_repo / 无自有DO | 公开版本化价目与已观测模型投影 |
| subscription | subscription_controller / subscription_srv | mysql/subscription_repo / mysql/subscription_do | 独立中心手动订阅设置与修订控制 |
| schema | 无 HTTP / CLI 入口 | mysql/schema_repo / mysql/schema_do | 显式 DDL、版本检查与初始化标记 |

DTO/VO 同样使用 `<domain>_dto` / `<domain>_vo`。根 module.go 保留 Fx 装配；VO 的 Response、校验与通用 MutationView 留公共根。严格 JSON 解码复用 internal/http.DecodeJSON；statistics 的来源重建复用 reporting 的显式 DecideSnapshot 入口。测试夹具在 testsupport/center，仅依赖模型与存储，不反向依赖各业务 service。

以上组织手工依据 Go Starter v2.0.1 同步，保留最初 v2.0.0 生成溯源、HTTP/JSON/表名、权限和事务边界。MySQL 为目标存储，SQLite 仍为当前隔离开发环境。
