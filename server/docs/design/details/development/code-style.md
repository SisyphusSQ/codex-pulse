# Go 代码规范

本规范适用于业务工程及生成后的新增代码；模型归属以 [模型约定](../../architecture/models.md) 为准，版本以 go.mod 为准。

## 格式、命名与类型

- 修改的 Go 文件通过 gofmt；import 按标准库、第三方、项目内包分组。
- 导出名称使用 Go 缩写习惯，如 ID、HTTP、URL；注释以标识符名称开头，中文说明契约和限制。
- 文件按职责命名，包名简短；不为“通用”创建含义模糊的 Manager、Data 或大杂烩包。
- DTO/DO/VO 放 models；service 仅保留实现、依赖容器和有理由的局部算法状态。
- 接口由真实替换边界决定，保持小而明确；不为每个 struct 或测试创建无业务意义的接口。

## 依赖注入与生命周期

- 继续使用 Uber Fx，在各层 module.go 组合 Provide/Invoke；新模块显式注册，不用 init 隐式建立业务连接。
- 构造器只保存依赖和验证本地参数；网络、监听、任务在 OnStart 启动，失败 hook 清理自己，OnStop 关闭成功启动的资源。
- context.Context 是操作参数，不保存在长期对象中；派生 context 及时 cancel，goroutine 明确结束条件和错误传播。
- SQL 事务由 service 发起，repository 使用传入的事务 context；禁止嵌套或跨库冒充原子事务。

## 错误、日志与 HTTP

- 错误保留 errors.Is/As 可识别语义，需要上下文时使用 %w；业务失败不返回伪成功。
- controller 使用统一响应和 BindAndValidate；HTTP 错误交给统一边界，不回显内部异常或完整连接串。
- 错误通常由负责处置的一层记录一次；使用 log.FromContext(ctx) 关联 request_id，结构化字段保持稳定。
- 日志消息使用简短英文，代码注释/文档使用中文；不记录密码、Token、Cookie、请求体或原始 SQL 参数。
- 所有业务路由默认经过鉴权；资源归属在 service/repository 校验，不能仅靠 DTO 的 userID 或前端隐藏。

## 版本、测试与交付

- Go 1.27.1 能力按实际契约使用；新增自有 JSON 编解码优先标准库 encoding/json/v2，UUID 使用标准库 uuid，第三方协议按其 SDK 契约处理。
- 外部输入、超时、资源释放和并发正确性需要防护；不堆无依据的 retry、recover、fallback 或兼容分支。
- 测试覆盖实际行为和失败语义；普通单测不依赖外部服务，集成证据单独记录。
- 修改代码同时维护受影响设计、配置和接口；提交/发版收尾不自动重复测试，不把交叉编译等同目标平台运行。
