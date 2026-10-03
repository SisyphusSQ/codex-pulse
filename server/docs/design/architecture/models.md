# DO / DTO / VO 模型约定

本仓固定采用 internal/models/do、dto、vo。删除示例业务不等于删除分层规范：三个目录及说明始终生成；数据库专属 DO 子目录按所选数据库生成。

| 类型 | 位置与命名 | 责任 | 禁止 |
|---|---|---|---|
| DO | `do/mysql/<domain>_do/`、`do/mongo/<domain>_do/`；如 User | 表、集合、字段映射及持久化元数据 | 直接作为 HTTP 响应或携带连接对象 |
| DTO | `dto/<domain>_dto/`；如 CreatePaymentCommand、ProviderResult | 跨服务、命令、事件及外部系统请求/结果 | 依赖 Echo、service 或数据库标签 |
| VO | `vo/<domain>_vo/`；如 CreateUserReq、UserView、UserListResp | HTTP 请求、格式校验和稳定对外响应 | 泄露 DO、密码哈希、内部 SDK 结果或存储标签 |

业务模型必须进入业务子包，不能以一个业务一个根目录文件代替分包；VO 根包只保留明确的 HTTP 公共能力。MySQL DO 在业务子包内一表一文件，TableName() 和列映射与对应表 DO 同文件，禁止多表合并进 models.go/schema.go。完整目录、共享类型例外与迁移规则见 [业务子包约定](packages.md)。

VO 包含 HTTP 请求和响应，这是本工程及既有示例的约定。不要把 HTTP 请求迁到 DTO 只为套用另一套术语。请求复杂、需要多个入口调用同一应用服务时，由 controller 将 VO 转成语义清晰的 DTO。

## 转换和依赖

- controller：VO 请求绑定与 Validate，取得可信身份，调用 service，统一返回 VO；不直接连接数据库。
- service：业务规则、资源授权、事务边界和 DO/DTO → VO 转换；不接收 Echo context。
- repository：DO 与持久化；SQL 使用 Engine.DB(ctx)，参数化查询，返回稳定存储错误。
- models 不导入 service、controller、repository 或连接组件。依赖方向是 service → models。
- 相同字段不构成复用理由；禁止用 DO 的类型别名冒充 VO，或用嵌入 DO 省略敏感字段筛选。
- 接口返回命名 VO；动态键值才使用 map。不能用匿名 struct 绕过 DO/DTO/VO 分层。

## service 内允许保留的类型

service 实现、依赖容器与 Fx 参数结构体可以保留。只在单个实现使用、不导出、不跨包且不参与序列化/持久化的算法状态可保留并说明原因；其他业务数据结构移入 models。

## 字段和演进

- ID、时间、可选值和空值语义在接口边界固定；HTTP 更新使用显式字段白名单，不能全量持久化请求。
- 密码仅在请求边界接收；哈希仅留 DO；VO 永不返回密码或哈希。
- Validate 做格式与范围检查；账户归属、权限、库存等规则由 service 和有授权条件的查询保证。
- schema 变更按 [SQL 规范](../../sqls/README.md) 同步完整结构、DO、repository 与相关 DTO/VO；API 变更同步调用方、文档和行为测试。

新增模块流程见 [开发步骤](../details/development/add-module.md)。
