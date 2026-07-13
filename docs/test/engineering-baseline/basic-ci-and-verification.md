# 基础 CI、统一验证入口与项目约束 Runbook

## 1. 目标与边界

本 runbook 记录 TOO-245 建立的工程基线验证面：

- 本地与 GitHub CI 共用 `make verify`。
- base harness 与 project-specific checks 分层。
- 当前真实实现的 Go、Vue、Wails bindings、macOS 15+ arm64 bundle/ZIP 会阻断回归。
- GitHub PR/main CI 使用无发布凭证、只读权限、关闭 dependency cache 的 clean runner。

不包含 Developer ID、notarization、Sparkle/appcast、发布 secret、tag、GitHub Release、Intel/Universal、外部部署或 Linear 写入。业务模块尚未实现的规则继续保持 `documented`/`partial`，不伪造空测试或 enforced 状态。

## 2. 前置条件

- macOS 15+、Apple Silicon arm64。
- Go module 声明的 Go `1.25.0`。
- Node.js `^22.13.0 || >=24.0.0`、npm `>=10.0.0`；该范围与锁定 `jsdom@29.1.1` 的可用分支一致，并明确排除其不支持的 Node 23。
- Wails CLI 精确为 `v3.0.0-alpha2.117`。
- macOS 自带 Ruby/Psych（本次验证为 Ruby `2.6.10`）；只用于 safe YAML/AST 权限 contract，不安装额外 gem。

首次或完全清理后：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
wails3 version
npm ci --prefix frontend
```

Expected：`wails3 version` 有且只有版本行 `v3.0.0-alpha2.117`；`npm ci` 按提交的 lockfile 安装且不得修改 `package-lock.json`。

## 3. 统一入口

从仓库根执行：

```bash
make verify
```

固定顺序：

1. `make harness-verify`：base harness 结构和受管 contract。
2. `make verify-project`：项目规则读回与负向 contract tests。
3. `make verify-go`：`go test ./...`、`go vet ./...`。
4. `make verify-frontend`：Vue typecheck、全部 Vitest、production build。
5. `make verify-generated`：执行 `go mod tidy` 和强制 Wails bindings 重生成，比较执行前后的 tracked 与非忽略 untracked 文件快照，再执行 `git diff --check`。
6. `make verify-package`：clean macOS arm64 package、bundle/ZIP 完整复验。

生成一致性 gate 必须先于 package：这样 package 内部即使再次执行 tidy，也只能消费已经证明稳定的 module/bindings 输入，不能先改写输入再让后续快照把变化误认成基线。

Make 不吞掉子命令错误。某层失败时，直接重放日志中对应的 `make verify-*` 或原始命令；不要跳过失败层继续把总体结果写成 PASS。

## 4. Project checks

`scripts/project-checks/check.sh` 当前机械保护：

| Rule ID | 检查 |
| --- | --- |
| `RUNTIME-002` | 执行环境为 Darwin/arm64；darwin Taskfile 保持 macOS 15 deployment 和 arm64 输入 contract |
| `TOOLCHAIN-001` | Go directive；Wails CLI/module；前端 runtime manifest/lockfile；Node/npm manifest/lock root engines；锁定 jsdom engine 精确读回 |
| `VERIFY-003` | Makefile 存在统一入口和全部可重放分层 target；generated 必须先于 package；新生成的非忽略 untracked binding 必须失败 |
| `CI-001` | workflow runner、action SHA、safe YAML/AST 唯一只读权限块、safe-loaded jobs、GitHub expression allowlist、checkout credential、cache、精确 Wails、`npm ci`、`make verify`、含 untracked 的 clean status，以及 base harness 不依赖 runner 缺失的 ripgrep contract |

失败格式包含 Rule ID、source 和重放命令，例如：

```text
[TOOLCHAIN-001] Wails module must be v3.0.0-alpha2.117, got ...
source: go.mod
command: make project-check
```

契约测试：

```bash
make project-check-test
make project-generated-check-test
```

它在临时 fixture 中验证以下漂移会非零失败并输出正确 Rule ID：

- Go module 的 Wails 版本漂移。
- `@wailsio/runtime` manifest 版本漂移。
- manifest Node engine 从 `^22.13.0 || >=24.0.0` 回退到 `>=22.12.0`。
- manifest Node engine 放宽为会错误声明 Node 23 可用的 `>=22.13.0`。
- lockfile root Node engine 从 `^22.13.0 || >=24.0.0` 回退到 `>=22.12.0`。
- workflow 从 `macos-15` 漂移到 `macos-latest`。
- workflow Node pin 从 `22.13.0` 回退到 `22.12.0`。
- base harness 重新引入 clean runner 未预装的 `rg` 命令。
- `contents: read` 被提升为 write。
- job 级新增 `permissions: write-all`。
- job 级使用合法 inline YAML `permissions : {contents: write}`。
- job 级使用 YAML merge key `<<: {permissions: write-all}`。
- permissions map 重复声明 `contents` key。
- setup-go dependency cache 被打开。
- workflow 引用隐式 `github.token`。
- workflow 使用 index expression `github["token"]`。
- workflow 使用动态 index expression `github[format(...)]`。
- workflow 用 YAML Unicode escape 在 safe-load 后还原动态 token expression。
- workflow 新增 `gh release` 发布步骤。
- clean gate 丢失 `--untracked-files=all`。
- Wails CLI 从锁定版本漂移。
- Wails generator 新建非忽略 untracked binding。

fixture 位于系统临时目录并通过 trap 清理，不修改仓库真相。

## 5. GitHub CI contract

workflow：`.github/workflows/ci.yml`。

- 触发：pull request 和 `main` push。
- runner：`macos-15`；GitHub 官方当前将 public repository 的该 label 定义为标准 M1 arm64 runner。
- permissions：Ruby/Psych 用 AST 检查顶层 `permissions` 与内部 `contents` key 唯一性，再 safe-load YAML 并遍历实际 job hash；只允许顶层 `{contents: read}`，direct/inline/merge 后的 job permissions、`write-all`、其它 scope 和 write permission 都会被拒绝。
- expressions：递归遍历 safe-loaded object graph 的全部 String key/value，再从 YAML 解码后的内容提取 `${{ ... }}`；只允许 `github.workflow` 与 `github.ref` 各一次。点号、index、动态 index、Unicode-escaped dynamic expression、完整 context 序列化等额外 GitHub expression 都失败。
- checkout：`persist-credentials: false`。
- actions：checkout/setup-go/setup-node 固定当前 v6 commit SHA，不使用 moving major tag。
- Go cache：`cache: false`。
- npm cache：`package-manager-cache: false`。
- 依赖：每个 ephemeral runner 执行 `npm ci` 和精确 Wails CLI 安装。
- 主命令：`make verify`。
- clean checkout gate：主命令后要求 staged/unstaged tracked diff 以及全部非忽略 untracked 状态都为零。

官方真相入口：

- https://docs.github.com/en/actions/reference/runners/github-hosted-runners
- https://github.com/actions/runner-images
- https://github.com/actions/checkout
- https://github.com/actions/setup-go
- https://github.com/actions/setup-node

workflow 不显式引用 `secrets.*`/index expression、`github.token`/index/dynamic expression 或 `GITHUB_TOKEN`，不申请 `id-token` 或 write permission，不执行 codesign identity、notary、release、deploy 或外部 Issue Tracker 写入。checkout 使用官方 action 的 ephemeral read-only repository access，但 `persist-credentials: false` 保证凭证不写入 checkout 配置。ad-hoc bundle 签名仍由现有 package task 完成，不需要 Keychain/Apple 发布凭证。

## 6. 生成物、缓存与清理

| 路径 | 来源 | Git 状态 | 清理 |
| --- | --- | --- | --- |
| `frontend/node_modules/` | `npm ci` | ignored | `rm -rf frontend/node_modules` |
| `frontend/dist/`（保留 `.gitkeep`） | Vue build | ignored 派生文件 | 删除 `assets/` 和 `index.html` |
| `.task/` | go-task 状态 | ignored | `rm -rf .task` |
| `bin/codex-pulse`、`bin/Codex Pulse.app`、ZIP、`.packaging/` | Wails build/package | ignored | `wails3 task package:clean` 后删除剩余 `bin/` |

CI 不持久化 dependency cache，runner 销毁即清理。关闭 cache 是 correctness 基线；未来若为了性能启用 cache，key、restore 边界和 cache-off 回归必须另行 review，且 cache miss 不能改变结果。

`verify-generated` 会实际执行 `go mod tidy` 和 clean bindings generation。若它失败，先用 `git status --short` 与 `git diff -- go.mod go.sum frontend/bindings` 区分原有工作和本次生成结果；不要用 destructive reset 覆盖用户改动。修复源输入后重新执行同一命令，或只在确认内容为可丢弃派生结果时按文件恢复。

## 7. 当前验证结果（2026-07-14）

### RED

- 实现前 `make verify`：退出 2。
- 输出：`No rule to make target 'verify'`。
- 未产生 `bin/` 或 `frontend/node_modules/`。

### GREEN

- Bash syntax：PASS。
- project-check contract tests：PASS；Go module、runtime、Node manifest/lock/workflow、runner、顶层/direct/inline/merge/duplicate 权限、cache、点号/index/dynamic/decoded token、release、untracked clean gate、base harness ripgrep 依赖、CLI 二十一类负向 fixture 均被拒绝。
- workflow permissions parser：Ruby 2.6.10 syntax/safe-load/AST readback PASS；safe-loaded job merge override、duplicate contents、动态 expression 与 YAML-decoded expression 四类复审绕过均被拒绝。
- generated-check contract test：PASS；fake generator 新建的非忽略 untracked binding 被 `VERIFY-003` 拒绝并报告具体路径。
- `make verify`：PASS。
- base harness：PASS。
- project checks：`RUNTIME-002`、`TOOLCHAIN-001`、`VERIFY-003`、`CI-001` PASS。
- Go：`go test ./...`、`go vet ./...` PASS。
- Vue：typecheck PASS；2 test files / 5 tests PASS；production build PASS。
- package：thin arm64、minOS 15.0.0、ad-hoc strict signature、单顶层 ZIP 与解压复验 PASS。
- generated：强制 bindings 重生成前后的 tracked 与非忽略 untracked 快照无变化；`git diff --check` PASS。
- npm install：200 packages audited，0 vulnerabilities；现有间接 `glob@10.5.0` 输出 deprecation warning。
- Node floor CI rework：精确 Node `22.13.0` / npm `10.9.2` 下 clean `npm ci` 与完整 `make verify` PASS；package 内再次 clean install 同样 PASS，manifest/lock/workflow 的 22.12 回退和 manifest 错误放宽 Node 23 的 fixtures 均按 Rule ID 拒绝。另以 Node `23.11.1` / npm `10.9.2` 实际执行 `npm ci`，由 `engine-strict=true` 非零返回 `EBADENGINE`，证明排除 Node 23 不是纯文档假设。
- clean runner 工具 rework：base harness 的 29 处文本搜索统一改用系统 `grep -R`；在刻意排除 Homebrew、无法解析 `rg` 的 PATH 中 `scripts/harness/check.sh` PASS；`CI-001` 对 harness 源码执行保守的独立 `rg` token 禁令，命令替换、括号、反引号、绝对路径、直接调用、注释与字符串七种 fixture 均非零拒绝；精确 Node `22.13.0` / npm `10.9.2` 下完整 `make verify` 再次 PASS。

Go test 在当前 macOS 26 SDK 环境仍输出“SDK object built for 26.0, linked 11.0”的已知 linker warning；测试为 PASS，且 package 的独立 plist/Mach-O gate 已证明交付物 minimum macOS 15.0。该 warning 不应被误写成 bundle target 11。

真实 GitHub workflow check 只能在 PR 推送后形成；其 URL、status、attempt 和结论回写 Linear/PR，本 runbook 不预写未发生的 CI PASS。

首次本地完整验证使用 Node `26.0.0` / npm `11.12.1`，落在依赖支持的 `>=24.0.0` 分支，因此只能证明高版本路径，未覆盖 Node 22 最低分支。PR #5 首次 CI run 29275829555 随后在 `npm ci` 真实复现 `EBADENGINE`：Node 22.12.0 不满足 `jsdom@29.1.1` 的 `^22.13.0`。本卡已据此把 manifest、lock root 与机械检查统一为 `^22.13.0 || >=24.0.0`，workflow 固定为用于证明最低分支的 Node 22.13.0，并完成精确低版本本地复验；修复后的真实 PR CI 结果仍必须另行读回，不能用本段本地 PASS 替代 runner PASS。

PR #5 第二次 CI run 29277295135 已通过 checkout、Go/Node setup、Node 22.13 `npm ci` 与精确 Wails CLI 安装，随后在 `make verify -> harness-verify` 因 runner 没有 `rg` 而失败。该失败证明首次 Node rework 有效，也暴露 base harness 的隐式本机工具依赖；本卡已改用 runner 自带的 `grep -R` 并加入机械回归。再次推送后的真实 CI 仍须另行读回。

## 8. Stop When 与发布边界

当本地完整 `make verify`、负向 contract tests、独立 review、Final Scope Review 和真实 PR CI 全部通过，即满足本卡基础 CI/机械约束目标。后续只在已有业务实现出现时追加相应规则，不在本卡伪造未来检查。

普通 Execution closeout 只写 `CHANGELOG.md -> Unreleased`。本 runbook 不授权版本归档、tag、GitHub Release 或任何正式发布动作。
