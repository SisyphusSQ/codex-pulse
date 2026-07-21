# Project Mechanical Constraints

本文件登记 Codex Pulse 当前可执行的项目级机械约束。`enforced` 必须对应真实命令；Swift transport 已有 SwiftPM、生成漂移和隔离 Helper E2E，`.app`、AppKit UI、签名、公证仍未实现，不能标记为已验证。

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
| `DATA-001` | `contract` | 不复制原始 JSONL，不持久化完整 prompt/response/tool output 或 credential | `README.md` | schema/privacy tests + review | `make verify-go` | `partial` | `human_decision_required` | 真实 Home 只读 live E2E 本轮未执行 |
| `DATA-002` | `runtime` | SQLite 保持单 writer queue，并按 admission → worker → SQLite 逆序退出 | architecture | integration/race tests | `make verify-go` | `enforced` | `rule_promotion_candidate` | Helper runtime 复用现有 repository/runtime |
| `TOOLCHAIN-001` | `verification` | Go 1.25、grpc-go v1.82.1、protobuf-go v1.36.11 与生成器版本固定 | `go.mod` / proto script | project check + Proto gate | `make verify-project` / `make verify-proto` | `enforced` | `maintenance_candidate` | 禁止浮动 generator |
| `VERIFY-001` | `verification` | base harness 受管文件和入口保持完整 | `AGENTS.md` | harness check | `make harness-verify` | `enforced` | `maintenance_candidate` | 不替代项目 gate |
| `VERIFY-003` | `verification` | 统一入口必须串联 harness、project、Go/Swift Proto、race、vet、Helper build 与隔离 Swift transport E2E | `Makefile` | Make targets + CI | `make verify` | `enforced` | `maintenance_candidate` | `.app`、签名、公证仍不在本轮 |
| `CI-001` | `security` | CI 固定 macOS 15、只读权限，不读取 secret、不发布，只运行验证 | CI workflow | project check + GitHub Actions | `make verify-project` | `enforced` | `maintenance_candidate` | 官方 action固定 commit SHA |
| `SWIFT-001` | `contract` | Swift client 必须消费同一 Proto、固定官方依赖、经认证 UDS 监督真实 Helper，并保留 unknown/partial/recovery 语义 | native macOS client design | source + dependency + generation drift + deterministic/live gate | `make verify-swift-transport` / `make verify-project` | `enforced` | `maintenance_candidate` | 当前为 transport spike；AppKit UI、bundle、签名、公证另行承接 |

## `project-check` 挂载协议

- `scripts/project-checks/check.sh` 检查当前架构、工具链、入口和 CI contract。
- `scripts/project-checks/check_test.sh` 使用临时 fixture 证明关键负向规则会失败。
- 任何新增 `enforced` 规则都必须补可重放命令和失败 fixture；只有 review 约束时保持 `documented` 或 `partial`。
