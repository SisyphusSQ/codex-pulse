# server Agent Guide

## 项目事实

- Go 1.27.1；Echo v5；依赖注入及生命周期统一用 Uber Fx。
- 入口：app/main.go、app/cmd；HTTP 与中间件在 internal/http。
- controller、service、repository 和 DO/DTO/VO 按业务域分子包；各层根包只装配，VO 根包仅保留明确公共能力。MySQL DO 一表一文件，现有标签和 TableName() 与 DO 同文件。
- service 不直接处理 Echo 或持久化连接。DO 不作为业务响应；事务在 service 边界发起，repository 使用 Engine.DB(ctx)。
- build：make build；release：make release（只构建，不测试、不发布）。
- test：make test；格式检查/静态检查：make verify；格式改写：make fmt。
- lint：make lint；漏洞检查：make vuln，均使用固定版本工具。
- integration：make integration；仅当所选组件需要且隔离环境已准备时运行，详见 docs/test/integration.md。
- 发布与真实服务：本基底无线上发布目标。不得把本地验证当成线上验收。
- issue provider：linear；issue prefix：TOO。

## 开发前必读

1. docs/design/architecture/README.md 和 packages.md：分层、业务子包与 Fx 生命周期。
2. docs/design/architecture/models.md：DO / DTO / VO 和转换责任。
3. docs/design/details/development/code-style.md：命名、类型、错误、日志、context、测试。
4. docs/design/details/development/add-module.md：新增业务步骤；受影响主题的 details 文档。

无 examples 时同样保留 internal/models/do、dto、vo 及 README。DO 不作为 HTTP 响应；DTO 放跨层与外部系统数据；VO 沿用本仓 HTTP 请求/响应约定。service 中不能新建应归入 models 的业务结构体或用匿名 struct 绕过规则。

build-all / release-all 覆盖 windows、darwin、linux × amd64、arm64，产物放 bin/<os>-<arch>/。构建细节见 docs/design/details/build/README.md。

## 协作入口

简单任务直接处理。总体设计以 `../docs/design/details/multi-machine-reporting/README.md` 为事实源；Linear 维护任务列表，本目录不另建 Issue 状态机或强制 plan/state/run 镜像。根仓库 AGENTS.md 与用户授权优先。
README 负责使用说明，docs 负责可复用说明；测试 runbook 放 docs/test。
用户当前请求、范围及已有授权优先，不重复询问已授权编辑；提交/发版收尾复用已有测试证据。

## 实现边界

- 组件只有显式 enabled 才装配。provider 构造器不访问网络；连接在 OnStart 打开，在失败路径和 OnStop 关闭。
- 构造器只保存依赖句柄，不能在 OnStart 之前获取 Mongo collection 或执行 SQL。
- 参数、分页、资源权限在责任层验证；不得关闭鉴权、TLS 校验或吞掉错误。
- 日志使用 FromContext(ctx) 关联 request_id；不记录 Token、Cookie、密码、完整 DSN、请求体或响应体。
- 原始运行记录、日志、缓存和凭据不提交；模板和脱敏验证摘要可以提交。
- `.agents/state/TEMPLATE.md` 和 `.agents/runs/TEMPLATE.md` 保持提交；真实运行文件保持本地。
- Harness 来源为 v0.7.0 / b20e5e8ece6a529c7d74aa0a8b1de77bd06c374c；通用工作流技能使用已安装的共享插件，不重复复制。

## SQL 文件规范

新增持久化模型或交付 SQL 前阅读 docs/sqls/README.md。当前完整结构放 docs/sqls/schema，待发布 SQL 放 docs/sqls/unreleased，真实发布时归档到 docs/sqls/releases/vX.Y.Z。无实际业务 SQL 时只保留说明，不生成 examples DDL、种子数据、空 SQL 或虚构版本目录；已发布文件不可改写。归档不等于数据库已经执行，执行证据单独记录。

## Starter v2.0.1 目录规范

手工同步 v2.0.1 的 packages/models/code-style/add-module 与各层 AGENTS；不重新生成已存在的 server。`.starter.json` 仍记录最初 v2.0.0 的生成来源，不能改写为重新生成的假记录。

- access：客户端、配对、凭证与权限；reporting：接收事实、仲裁及项目关联；statistics：只读汇总与检索；schema：受控结构初始化/检查。
- 业务实现进入 `<domain>_controller`、`<domain>_srv`、`repository/mysql/<domain>_repo`、`models/do/mysql/<domain>_do`、`models/dto/<domain>_dto`、`models/vo/<domain>_vo`。
- statistics 没有自有持久表，不创建空 DO 子包。MySQL 目录表示目标数据库；现有 SQLite 开发验证仍使用同一 Engine 和 DO，不复制业务或改换存储策略。
- HTTP 严格 JSON 解码在 internal/http；跨域只复用有真实调用方的访问权限和事实仲裁入口，不让子包反向导入装配根包。
