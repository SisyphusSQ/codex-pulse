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

新增业务按模型、repository、service、controller 顺序实现，并在相应 module.go 注册 Provide/Invoke。遵循现有边界，不为每个函数建立无调用需求的抽象。

SQL repository 必须使用 `engine.DB(ctx)`；`engine.Transaction(ctx, callback)` 内传给 callback 的 context 携带同一事务。回调返回错误或被取消即回滚。嵌套和跨数据库事务不支持。

cron 通过 Fx 的 `cron_jobs` value group 注入 `cron.Job`，包含 Name、Schedule、Run(context.Context)。调度器默认无任务，不自动计数或写 Redis。任务必须响应取消；同一进程内上次尚未完成时跳过重叠执行。多副本互斥需要业务显式采用锁或外部调度系统。
