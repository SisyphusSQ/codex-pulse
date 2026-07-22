# Project Mechanical Constraints

本文件登记 Codex Pulse 当前可执行的项目级机械约束。`enforced` 必须对应真实命令；Swift transport、原生 App 壳与主要页面已有 SwiftPM、生成漂移、确定性状态测试、隔离 CI smoke 和复用已配置 runtime 的真实 Home development App/Helper UI smoke。完整 Xcode/XCTest/XCUITest、真实 system lifecycle、签名、公证仍未验证，不能标记为通过。

固定原则：没有可执行命令或 gate 时，不得假装 `enforced`；repeated review finding 只有在规则稳定且有负向验证时才升级为 project-check。

## 状态枚举

| Status | 含义 |
| --- | --- |
| `enforced` | 已有本地/CI gate 会在违反时失败 |
| `partial` | 只机械覆盖其中一部分 |
| `documented` | 只有文档边界 |
| `planned` | 后续任务实现 |
| `not_applicable` | 当前项目明确不适用 |

## 分类枚举

`architecture`、`contract`、`runtime`、`verification`、`docs`、`security`、`cross-repo`。

## 维护循环关联

| Maintenance Tag | 含义 |
| --- | --- |
| `maintenance_candidate` | 维护循环定期扫描漂移 |
| `rule_promotion_candidate` | 有稳定 evidence 后可评估升级 gate |
| `human_decision_required` | 涉及产品、安全、数据或跨团队决策，必须人工确认 |

## 约束登记表

| Rule ID | Category | Rule | Source | Enforcement | Command | Status | Maintenance Tag | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `ARCH-001` | `architecture` | Go 运行时必须是无 Wails/Vue/Go AppKit/Sparkle 的 Helper；桌面 UI 与更新属于后续 Swift client | `README.md` / architecture | path + dependency negative gate | `make verify-project` | `enforced` | `maintenance_candidate` | 拒绝旧目录和依赖回流 |
| `RPC-001` | `contract` | `api/codexpulse/core/v1/core.proto` 是唯一跨进程 contract，生成代码必须无漂移 | `core.proto` | contract tests + temp regeneration diff | `make verify-proto` | `enforced` | `maintenance_candidate` | 固定 protoc 34.1、Go generator 版本 |
| `RPC-002` | `security` | Helper 只监听安全 UDS，token 只从继承 pipe 读取，unary/stream 必须鉴权 | architecture | unit/contract/race + source gate | `make verify-go` / `make verify-project` | `enforced` | `maintenance_candidate` | 无 TCP、argv/env token |
| `DATA-001` | `contract` | 不复制原始 JSONL，不持久化完整 prompt/response/tool output 或 credential；真实 Home live E2E 只公开脱敏计数和稳定状态 | `README.md` | schema/privacy tests + review + live output gate | `make verify-go` / `make verify-swift-app-live` | `partial` | `human_decision_required` | App Server 可在真实 Home 写正常状态库、WAL、锁和日志；Codex Pulse 派生事实仍写私有 runtime SQLite |
| `DATA-002` | `runtime` | SQLite 保持单 writer queue，并按 admission → worker → SQLite 逆序退出 | architecture | integration/race tests | `make verify-go` | `enforced` | `rule_promotion_candidate` | Helper runtime 复用现有 repository/runtime |
| `TOOLCHAIN-001` | `verification` | Go 1.25、grpc-go v1.82.1、protobuf-go v1.36.11 与生成器版本固定 | `go.mod` / proto script | project check + Proto gate | `make verify-project` / `make verify-proto` | `enforced` | `maintenance_candidate` | 禁止浮动 generator |
| `VERIFY-001` | `verification` | base harness 受管文件和入口保持完整 | `AGENTS.md` | harness check | `make harness-verify` | `enforced` | `maintenance_candidate` | 不替代项目 gate |
| `VERIFY-003` | `verification` | CI 统一入口必须串联 harness、project、Go/Swift Proto、race、vet、Helper build、隔离 Swift transport E2E 与确定性原生页面 smoke，不隐式读取用户数据 | `Makefile` | Make targets + CI | `make verify` | `enforced` | `maintenance_candidate` | 真实 Home 产品验收由显式本机 live gate 承担；正式签名、公证仍不在本轮 |
| `CI-001` | `security` | CI 固定 macOS 15、只读权限，不读取 secret、不发布，只运行验证 | CI workflow | project check + GitHub Actions | `make verify-project` | `enforced` | `maintenance_candidate` | 官方 action固定 commit SHA |
| `SWIFT-001` | `contract` | Swift client 必须消费同一 Proto、固定官方依赖、经认证 UDS 监督真实 Helper，并保留 unknown/partial/recovery 语义 | native macOS client design | source + dependency + generation drift + deterministic/live gate | `make verify-swift-transport` / `make verify-project` | `enforced` | `maintenance_candidate` | transport contract 已被原生 App 复用；签名、公证仍另行承接 |
| `SWIFT-002` | `architecture` | 原生 App 必须使用 AppKit/SwiftUI 和 generated CoreService client；Swift App 不得直读 SQLite/JSONL、复制 Go 业务真相或监听 TCP | native macOS client design / TOO-313 / TOO-314 | source negative gate + deterministic state tests + real development App/Helper UI smoke | `make verify-swift-app` / `make verify-swift-primary-pages` / `make verify-project` | `enforced` | `maintenance_candidate` | smoke 真实读回 window/status item/popover、主要页面数据链与 Shutdown；不代表正式分发 |
| `SWIFT-003` | `contract` | Sessions、Projects、Quota/Usage、Health、Sources/Jobs、Settings 必须共用 runtime generation，使用 bounded opaque pagination，并保留 partial/unknown/stale/cancelled；Settings 采用 revision CAS + receipt/readback；provider recovery command 与 `RunRuntimeAction` allowlist 必须分离 | native macOS client design / TOO-314 | required source surface + deterministic invalidation/generation/pagination/CAS/single-flight tests + isolated multi-page smoke + real Home live smoke + negative fixture | `make verify-swift-primary-pages` / `make verify-project` | `enforced` | `maintenance_candidate` | provider recovery command 当前只读；高风险 repair、migration、release 入口保持不可执行 |
| `SWIFT-004` | `verification` | 本机 development App / 主要页面 live E2E 必须复用已配置私有 runtime，confirmed Home 必须等于当前真实 Codex Home，允许标准 housekeeping，并要求非零 Sessions/Projects/Usage、详情读取、七页 render 与 clean Shutdown | native macOS client design / TOO-313 / TOO-314 | preflight + stable output assertions + no-new-runtime source gate | `make verify-swift-app-live` / `make verify-swift-app-smoke` / `make verify-swift-primary-pages` | `enforced` | `human_decision_required` | 不写真实路径、名称、payload 或原始日志；live gate 不属于 CI `make verify` |
| `LIFECYCLE-001` | `runtime` | App active、sleep/wake、terminate/restart 必须接入 Swift runtime/stream 状态机，未知 lifecycle enum fail closed | native macOS client design / TOO-313 | deterministic Swift state tests + existing Go tests；system notification live 尚缺 | `make verify-swift-app` / `make verify-go` | `partial` | `rule_promotion_candidate` | App/Helper 真实 Home live smoke 已执行，但仍显式不发送真实 OS lifecycle RPC |
| `BUNDLE-001` | `verification` | development App 将 Swift executable 与 Helper 放入 `Contents/MacOS` / `Contents/Helpers` 并可由 Bundle locator 启动 | native macOS client design / TOO-313 | unsigned layout assembly + isolated smoke + real Home App/Helper live smoke | `make verify-swift-app-smoke-isolated` / `make verify-swift-app-live` | `enforced` | `maintenance_candidate` | 仅 development-only；nested signing、archive、notarization 仍未实现 |

## `project-check` 挂载协议

- `scripts/project-checks/check.sh` 检查当前架构、工具链、入口和 CI contract。
- `scripts/project-checks/check_test.sh` 使用临时 fixture 证明关键负向规则会失败。
- 任何新增 `enforced` 规则都必须补可重放命令和失败 fixture；只有 review 约束时保持 `documented` 或 `partial`。
