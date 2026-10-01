# 业务子包与目录约定

本工程保留 controller、service、repository、models/do、dto、vo 分层；每层的业务代码必须按业务域建立独立子目录和 Go 子包，即使当前只有一个文件。禁止将不同业务域平铺在层根包，也不能只改文件名前缀而继续共用一个根包。

## 目录与拆分粒度

沿用现有目录后缀，业务域名称在各层保持一致：

| 层 | 业务子包位置 | 示例 |
|---|---|---|
| controller | `internal/controller/<domain>_controller/` | payment_controller |
| service | `internal/service/<domain>_srv/` | payment_srv |
| MySQL repository | `internal/repository/mysql/<domain>_repo/` | mysql/payment_repo |
| MongoDB repository | `internal/repository/mongo/<domain>_repo/` | mongo/payment_repo |
| MySQL DO | `internal/models/do/mysql/<domain>_do/` | mysql/payment_do |
| MongoDB DO | `internal/models/do/mongo/<domain>_do/` | mongo/payment_do |
| DTO | `internal/models/dto/<domain>_dto/` | payment_dto、lark_dto |
| VO | `internal/models/vo/<domain>_vo/` | payment_vo |

业务域由业务责任和数据归属决定，不按文件数量、接口数量或每个 struct 拆包。例如记账域内的账户、交易和预算可以放在同一组业务子包中。已有工程遵循其既定子包后缀；新增业务不得借命名差异改回平铺。

只建立实际需要的层：无存储业务不创建 DO/repository，没有跨服务或外部系统数据时不创建 DTO。无 examples 时保留目录说明和规则，不生成占位业务或空业务子包。

## 层根包与公共能力

- controller、service、repository 根包只承担 module.go 的 Fx 装配；具体 handler、业务规则和数据访问放进业务子包。
- models 的根目录保留 README、AGENTS 和子目录；DO 按存储类型再按业务域分包，DTO 的具体业务或外部系统对象进入对应子包。
- VO 根包允许统一 Response、响应 helper、BindAndValidate、通用校验及通用分页对象；具体业务请求/响应进入 `<domain>_vo`。公共 VO 根包不得导入业务 VO 子包或汇总它们的类型别名。
- 共享代码必须有明确职责和实际调用方。已有 common_srv 中的公共服务可以保留；新增具体业务不得因多个调用方就并入 common_srv，外部系统专属 DTO 仍单独分包。

## 装配与依赖

各层 module.go 显式注册子包的构造器或 Module。子包不依赖注册自身的上层装配包；确需共享的能力放在职责明确且不反向依赖业务的包中，避免导入循环。子包不必为了目录对称再创建一个 module.go，也不使用 init 隐式装配。

controller 子包调用 service 子包；service 子包调用 repository 子包及模型；repository 子包访问所属存储模型。models 不导入 controller、service、repository 或运行时连接组件。跨业务域引用先确认类型归属和依赖方向，不复制相同语义的模型，也不建立互相导入的业务包。

同名的 MySQL/MongoDB 子包同时导入时使用明确 alias，例如 mysql_payment_repo、mongo_payment_repo。包名随现有业务子目录保持一致。

## MySQL DO：一个表一个文件

在 `internal/models/do/mysql/<domain>_do/` 内，一个数据库表对应一个独立 .go 文件；文件包含该表的 DO、TableName()、列映射及直接相关的存储定义。文件名按对应模型使用 snake_case，例如 user.go 对应 users，user_token.go 对应 user_tokens。

禁止把同一业务域的多张表合并进 models.go、schema.go 或单个大文件；一个表的主 DO 和映射方法也不拆散到多个生产文件。共享的枚举或基础字段类型可放职责明确的公共文件，但该文件不汇集多张表的 DO。关联表同样按一表一文件处理；查询投影、跨表结果和 HTTP 对象不冒充表 DO。

新增或调整表时同步 docs/sqls/schema 的完整结构、对应 DO 文件和 repository；发布 SQL 遵守 [SQL 文件规范](../../sqls/README.md)。这项文件组织约定不改变 SQL 归档和数据库执行证据的边界。

## 已有工程迁移

先确定业务域与模型归属，再移动文件并修改 package/import，最后调整 Fx 装配和相邻测试。保留 HTTP 路由、JSON 字段、表名、认证、授权及事务语义；目录迁移不自动变更这些契约。

v2.0.1 将示例用户 VO 移到 models/vo/example_vo，将 Lark DTO 移到 models/dto/lark_dto。使用这些生成包的已有工程需要同步 import；更新生成器不会自动迁移已有业务工程。

模型语义见 [模型约定](models.md)，实施顺序见 [新增模块步骤](../details/development/add-module.md)。
