# Codex Pulse AGENTS

## 项目边界

Codex Pulse 是 local-first 的 Codex 使用量、额度、Session、项目归因和数据健康工具。当前运行时由 Go Helper 与原生 Swift macOS App 组成：

- `api/codexpulse/core/v1/core.proto` 是唯一跨进程 contract。
- Go Helper 负责数据、索引、调度、SQLite 和业务口径。
- Swift App 负责窗口、菜单栏和 UI，通过 generated CoreService client 访问 Helper。
- Helper 只监听 Unix Domain Socket，不监听 TCP；鉴权 token 只通过继承 pipe 传递。
- Swift App 不得直接读取 SQLite、JSONL 或复制 Go 业务真相。

## 工作方式

- 开始修改前先检查 `git status`，保留并避开无关工作树改动。
- 普通改动不要求 repo-local plan、state、run、write lease 或重复 review 文档。
- Linear 可作为轻量任务列表；仓库不维护另一套 Issue 状态机镜像。
- 复杂或高风险改动仍应先明确目标、范围、失败语义和验证入口，但不要求固定模板。
- 修改目录前读取就近的 `AGENTS.md`；更细目录规则优先。
- PR 标题和正文默认使用中文，代码标识、命令和必要错误原文可保留英文。

## 真实 Codex Home 默认环境

- 本地 development App 的启动、手工调试、菜单栏/UI 检查、E2E 与产品验收一律同时满足两个条件：显式绑定按 `${CODEX_HOME:-$HOME/.codex}` 解析的真实 Home；通过 `CODEX_PULSE_APP_RUNTIME` 或 `--runtime-directory` 使用 mode `0700` 的私有 runtime。首次缺少 `preferences.json` 时 Helper 会在 metadata-only 安全探测后自动持久化默认 Home 的 canonical path/device/inode；已有配置时不得覆盖。启动后必须读回 preferences、App/Helper 环境和 Helper 参数，确认最终 Home 物理身份与预期一致。
- 用户已明确授权本仓库后续本地 App 启动与人工验收使用真实 Codex Home，无需每次重复询问；但执行脚本前仍须说明会读取 Session/JSONL，并可能写入应用 runtime、SQLite、偏好及 App Server 标准 housekeeping。
- 本地产品验收默认使用 `make verify-live` 或等价的显式真实 Home 启动，并通过进程环境或产品读回确认实际使用的 `CODEX_HOME`。不得用 isolated / synthetic / empty Home 的结果代替真实产品结论。
- CI、单元测试、contract test 和确定性 smoke 是唯一例外：为防止测试污染个人数据，仍必须使用 synthetic / empty Home；这些结果只能作为测试证据，不能冒充真实 Home 验收。

## 验证入口

日常开发优先运行受影响的包或 Swift executable tests，不在每次迭代都跑完整验证。

| 目标 | 用途 |
| --- | --- |
| `make test-go` | Go 全仓非 race 测试 |
| `make test-swift` | Swift client 与 App 测试 |
| `make check` | 提交前产品检查：架构、Proto、Go、Swift |
| `make verify` | PR / CI 完整验证：架构、Proto、Go race/vet、Swift transport 与隔离 App smoke |
| `make verify-live` | 显式复用本机私有 runtime 的真实 Home development App 验收 |

- `make verify-live` 不属于 PR / CI 默认验证，但属于本地 App 启动与人工产品验收的默认入口；真实 Home 使用授权见上文，运行前仍须说明副作用边界。
- CI、单元测试、contract test 与确定性 smoke 使用 synthetic / empty Home，不能冒充真实 Home 产品验收。
- 正式签名、公证和发布只在对应任务明确授权后执行。

## 文档与本地证据

- `README.md` 是仓库和开发入口。
- `docs/design/` 保存产品、架构和契约设计。
- `docs/test/` 保存可复用 runbook 与提交版脱敏结果摘要。
- `.artifacts/` 保存本机验证原始证据，默认忽略且不得提交。
- 不提交凭据、真实路径、原始 JSONL、用户内容、完整日志或临时下载地址。

## Git 与交付

- 分支名仅使用英文、数字、`-`、`_` 和 `/`。
- 不重置、覆盖或删除无关改动。
- 完成实现后运行与风险匹配的验证；PR/CI 收口运行 `make verify`。
- 未实际执行的 CI、live E2E、签名、公证、发布或外部回写不得描述为已完成。
