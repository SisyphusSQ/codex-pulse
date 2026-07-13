# Project Mechanical Constraints

## 文档定位

本文件登记当前项目的项目级机械约束：哪些工程边界已经变成可执行检查，哪些还只是文档约束，哪些计划后续接入。

它不定义通用 lint 规则，也不预设某个业务项目的架构边界。初始化后，项目维护者需要基于真实代码、架构文档、运行入口和协作规则补齐本文件。

固定原则：

- 没有可执行命令或 gate 时，不得假装 `enforced`
- `enforced` 必须能对应到本地命令、CI、linter、script、test、contract diff、E2E 或 review gate
- `documented` 只表示已有文档规则，不表示机器会拦截
- `partial` 必须说明哪些部分已机械化，哪些仍需人工 review
- 项目专属规则不要写进 base harness 模板本身，先登记到本文件，再按项目选择 linter / script / test / E2E 载体

## 状态枚举

| Status | 含义 |
| --- | --- |
| `enforced` | 已有可执行命令或 gate 会在违反时失败 |
| `partial` | 部分已机械化，仍有人工 review 或后续补齐项 |
| `documented` | 只有文档约束，尚无可执行检查 |
| `planned` | 已决定后续接入，但当前没有规则或命令 |
| `not_applicable` | 当前项目明确不适用 |

## 分类枚举

| Category | 典型内容 |
| --- | --- |
| `architecture` | 分层、依赖方向、目录职责、模块边界 |
| `contract` | API / schema / DTO / OpenAPI / provider-consumer contract |
| `runtime` | 配置、环境变量、日志、指标、trace、启动方式 |
| `verification` | 测试矩阵、E2E、live self-test、构建和验证入口 |
| `docs` | 设计文档、runbook、计划、结果摘要和链接同步 |
| `security` | 权限、副作用、危险命令 |
| `cross-repo` | provider / consumer / shared truth 分层与同步 |

## 维护循环关联

Maintenance loop 默认扫描本文件，用来判断项目规则是否仍停留在文档层、是否需要建 issue，或是否已具备升级为机械检查的条件。

| Maintenance Tag | 含义 |
| --- | --- |
| `maintenance_candidate` | 维护循环应定期扫描该规则是否漂移，但当前不一定适合机械化 |
| `rule_promotion_candidate` | 重复 review finding 或已有稳定命令，适合评估升级为机械检查 |
| `human_decision_required` | 涉及产品、API、安全、数据或跨团队取舍，需要人类确认后才能修改 |

固定规则：

- maintenance loop 发现 `documented` 规则长期未机械化时，只能报告或建议建 issue，不得自动把它改成 `enforced`。
- repeated review finding 可以升级为 `project-check`、linter、contract diff、E2E 或 harness check，但必须先写清 evidence、目标 `Rule ID`、执行命令、回归验证和回滚方式。
- `rule_promotion_candidate` 只是候选标签，不代表已经允许自动新增检查脚本或 CI。

## 约束登记表

| Rule ID | Category | Rule | Source | Enforcement | Command | Status | Maintenance Tag | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `ARCH-001` | `architecture` | 后端使用 Go + Wails3，前端使用 Vue 3 + TypeScript + Vite，本地 SQLite 与 Go 查询层是业务事实和聚合口径的权威来源 | `README.md` | review | - | `documented` | `human_decision_required` | M1-E2 已生成 `internal/app`、Vue app assembly 与 generated bindings；M2-E1 已接入 SQLite 连接/生命周期基线，业务 schema、repository 与查询层仍由后续卡实现 |
| `ARCH-002` | `architecture` | Wails3 负责基础 tray、窗口和事件；macOS 双行状态项与原生 Sparkle 2 集成必须留在平台 adapter，不得把 AppKit 或 Sparkle 细节泄漏到 Vue / 业务层 | `docs/design/details/architecture/README.md` / `docs/test/engineering-baseline/wails3-toolchain-capability-probe.md` | review + capability probe | `go test ./pkg/application ./pkg/updater/...`（在锁定的 Wails module 目录执行） | `partial` | `rule_promotion_candidate` | 基础能力已有同 tag 测试和运行证据；Wails digest-signature 不等于 Sparkle 2 `sign_update` 互操作，custom `NSStatusItem`、signature interoperability 与 native shim 仍由 M9/M10 人工 review 和 live E2E 验收 |
| `DATA-001` | `contract` | 只读增量索引 Codex 本地 JSONL；不得复制原始 JSONL，也不得持久化完整 prompt、response 或工具输出 | `README.md` | review | - | `documented` | `rule_promotion_candidate` | 后续应通过 parser/store 测试和 schema 检查机械化 |
| `DATA-002` | `runtime` | 桌面进程应只通过 `internal/store/sqlite` 的单 writer queue 写 SQLite；必须读回 WAL、foreign keys、synchronous、busy timeout 和 reader query-only，并按 reject -> drain -> wait reads -> close 顺序退出 | `docs/design/details/data-model/README.md` / `docs/design/details/architecture/README.md` | Go integration + race tests + review | `go test -race ./internal/store/sqlite ./internal/app` | `partial` | `rule_promotion_candidate` | 测试已机械覆盖应用路径/权限、真实 pragma、FIFO/rollback、authoritative cancellation、WAL 并发、队列满、busy/readonly 分类、drain、幂等 Close 与 app lifecycle；当前仍需 review 阻止其他包直接打开第二条 SQLite writer，后续可评估 architecture project-check；业务 schema 不在本规则内 |
| `SEC-001` | `security` | access token、refresh token、Authorization header、Cookie 及其他凭证只能临时进入内存，不得写入数据库、日志或仓库 | `README.md` | review | - | `documented` | `rule_promotion_candidate` | 在线 quota 与 reset credits 必须保持可关闭 |
| `RUNTIME-001` | `runtime` | v0.1 仅支持 macOS，并保持 local-first；SQLite 数据位于应用本地数据目录，不写入 Codex home | `README.md` | review | - | `documented` | `human_decision_required` | 跨平台或远程访问属于后续范围，需单独决策 |
| `RUNTIME-002` | `runtime` | v0.1 工程与 package 只支持 macOS 15+ arm64；不得沿用 Wails template 的 macOS 12 deployment target，也不顺带构建 Intel / Universal | `docs/test/engineering-baseline/wails3-toolchain-capability-probe.md` / `docs/test/packaging/macos-arm64-bundle-signing.md` / `docs/test/engineering-baseline/basic-ci-and-verification.md` | project check + build/package task + bundle verification + CI | `make verify` | `enforced` | `maintenance_candidate` | project-check 拒绝非 Darwin/arm64 并检查 deployment/target contract；package gate 从 plist/Mach-O 读回 macOS 15 与 thin arm64；PR CI 固定 `macos-15` arm64 |
| `TOOLCHAIN-001` | `verification` | Wails CLI 与 Go module 精确固定为 `v3.0.0-alpha2.117`，对应 `@wailsio/runtime` 精确固定为 `3.0.0-alpha.97`；Node 支持范围为 `^22.13.0 || >=24.0.0`，明确排除锁定 jsdom 不支持的 Node 23；禁止 `latest`、master、nightly 和普通功能卡顺带升级 | `docs/test/engineering-baseline/wails3-toolchain-capability-probe.md` / `docs/test/engineering-baseline/basic-ci-and-verification.md` | project check + negative contract tests + CI | `make verify-project` | `enforced` | `maintenance_candidate` | 同时读回 Wails CLI、Go module、package manifest/lockfile、Node/npm root engine 与锁定 jsdom engine；负向 fixture 覆盖 runtime/CLI/Node 22.12 回退与错误放宽 Node 23，CI 精确安装锁定 CLI 与 Node 22.13.0 |
| `VERIFY-001` | `verification` | base harness 的受管文件、占位符、脚本和入口必须保持完整 | `AGENTS.md` / `Makefile` | harness check | `make harness-verify` | `enforced` | `maintenance_candidate` | 当前命令会在结构或关键 contract 缺失时失败 |
| `VERIFY-002` | `verification` | macOS package 必须包含正确 plist、正式 icns、thin arm64/minOS 15 Mach-O、可严格验证的 ad-hoc 签名，以及只有一个顶层 `.app` 且解压后签名仍有效的 ZIP | `docs/test/packaging/macos-arm64-bundle-signing.md` | package verification script | `wails3 package GOOS=darwin` + `wails3 task package:verify` | `enforced` | `maintenance_candidate` | 该 gate 只证明 ad-hoc 自用 bundle 的结构与完整性；不证明 Developer ID、notarization、Gatekeeper trusted distribution 或 Sparkle 更新互操作 |
| `VERIFY-003` | `verification` | 当前工程基线必须通过统一入口串联 base harness、project checks、Go、Vue、generated bindings/Go module 漂移检查和 macOS package | `docs/test/engineering-baseline/basic-ci-and-verification.md` | Make targets + generated snapshot/negative gate + CI | `make verify` | `enforced` | `maintenance_candidate` | generated 先于 package；bindings 强制重生成并比较执行前后的 tracked 与非忽略 untracked 文件，fake generator 新建文件有负向 fixture |
| `CI-001` | `security` | PR/main CI 必须运行在固定 macOS 15 arm64 runner，只读 checkout、关闭 dependency cache，不读取发布 secrets、不显式引用 workflow token、不申请写权限且不执行 release | `docs/test/engineering-baseline/basic-ci-and-verification.md` | safe YAML/AST workflow contract check + negative fixtures + GitHub Actions | `make verify-project` + GitHub `CI / macOS 15 arm64 verification` | `enforced` | `maintenance_candidate` | 官方 actions固定commit SHA；结构化gate只允许唯一顶层`contents: read`，拒绝safe-loaded job override，并在decoded object graph上allowlist `github.workflow/ref`；base harness只用runner自带`grep -R`，project gate保守禁止harness源码出现独立`rg` token（含命令、路径与说明文本）；负向fixture覆盖inline/merge/write-all/duplicate permissions、点号/index/dynamic/decoded token、release、cache、runner、ripgrep与untracked clean gate；真实workflow check是GitHub语义的权威证据 |

## `project-check` 挂载协议

base harness 不默认生成 `project-check`，也不生成永远 pass 的占位脚本。

当项目已有稳定的项目级机械约束后，可以按需补充：

```text
scripts/project-checks/
  check.sh
  check-architecture.sh
  check-contracts.sh
  check-runtime.sh
  check-docs.sh
```

推荐 Makefile 入口：

```makefile
project-check:
	bash scripts/project-checks/check.sh
```

固定要求：

- 一旦某条规则标记为 `enforced`，`Command` 必须指向真实可执行入口
- `project-check` 可以汇总项目专属检查，但不替代 `make harness-check`
- `make harness-check` 只校验本文件作为登记入口存在且结构完整，不替项目臆造项目规则
- 违反规则时，失败信息应说明违反了哪条 `Rule ID`、参考哪个 `Source`、应运行或修复哪个 `Command`

## 初始化后补齐步骤

1. 从 `AGENTS.md`、目录级 `AGENTS.md`、README、架构文档和现有 Makefile 里提取项目不可违反的规则。
2. 先把规则登记到上方表格，并诚实标注 `Status`。
3. 已有命令或 gate 的规则，补齐 `Enforcement` 和 `Command`。
4. 只有文档约束的规则，保持 `documented`，不要写成 `enforced`。
5. 后续把稳定规则逐步接入 linter、script、test、contract diff、E2E 或 CI。
6. 为每条规则补齐 `Maintenance Tag`，让 maintenance loop 能区分扫描、升级和人工决策边界。
