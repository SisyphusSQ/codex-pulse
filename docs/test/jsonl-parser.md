# JSONL Parser 与 Turn Lifecycle Runbook

## 当前验证结果

- 记录时间：2026-07-14 23:28 CST
- 记录目录：仓库根目录
- 本轮任务性质：TOO-252 rollout JSONL incremental parser / safe decoder / explicit turn lifecycle
- 当前结论：`PASS`；PR #13 已合并为 `24f0aeb`，TOO-252 已完成 main post-merge verify 与 Linear Done。
- 自动化入口：`internal/codex/logs` 的 framer、decoder、lifecycle、stream parser 与 fuzz tests
- 对应计划 / issue：`.agents/plans/2026-07-14-too-252-jsonl-parser-turn-lifecycle.md` / TOO-252
- 结果说明：使用 synthetic upstream-shaped fixture 验证 CRLF/UTF-8/chunk、坏行恢复、重复 key、未知类型、隐私 marker、turn lifecycle、nullable usage 和 deterministic fuzz；未读取真实 Codex rollout。

### 本次执行结果

- 执行时间：2026-07-14 15:25 CST
- 执行目录：仓库根目录
- 本次结论：`PASS（含 post-merge verify）`
- 影响范围：编译并测试 Go 全仓、frontend、generated bindings 与 macOS 15 arm64 package；coverage profile 写入系统临时目录；Go/npm/Wails 使用本地缓存并生成仓库已忽略的构建产物。
- 清理结果：coverage profile 已删除；synthetic fixture 全在测试进程内；post-merge 复验后的 ignored Wails/frontend/package 产物已清理并保留 tracked `.gitkeep`；无数据库、server、外部资源或用户文件需要清理。
- 敏感信息处理：不读取真实 `~/.codex`、不写 prompt、response、tool output、base/user instructions、凭据、token、cookie、真实用户路径、原始 JSONL 或机器本地临时路径。fixture 中的 marker 只用于否定断言。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| protocol truth / synthetic fixture | PASS | 对齐 `codex-cli 0.142.3`、official `rust-v0.142.3` / `e2b60462a7321517895dd94920661599303a7539`；未读取真实 rollout |
| focused 20 次 | PASS | `CGO_ENABLED=0` |
| race | PASS | `internal/codex/logs` |
| coverage | PASS | post-checkpoint 85.5% statements |
| fuzz | PASS | post-integration 10 秒，3,374,129 次执行，无 panic/不变量失败 |
| `go vet` / `git diff --check` | PASS | affected package 与当前 diff |
| implementation review remediation | PASS | parser 生成完整 Session/open/context/usage/pending/closed `NextSeed`；pending 不再钉 offset；duplicate session-meta 与 usage restart deterministic；原 reviewer 第三次复审 `ZERO_FINDINGS / blocking_findings: 0` |
| Pure Go Store guard | PASS | `CGO_ENABLED=0` Store tests；实际依赖含 libtnb/modernc，不含 official sqlite/mattn driver |
| 全仓 / harness / exact Wails | PASS | 全仓 test/race/vet、harness/project/version/diff、frontend/generated、macOS 15 arm64 bundle/ZIP 与 ad-hoc codesign 均通过 |
| CHANGELOG | PASS | 唯一 `[TOO-252]` 条目位于 `Unreleased -> feature`；classification 为 `changelog-only`，未创建版本或发布 |
| 独立 review / final scope review | PASS / PASS | implementation reviewer 最终 `READY_FOR_CHANGELOG`；different final scope reviewer 对全部 13 个计划内文件返回 `ZERO_FINDINGS / blocking_findings: 0 / READY_TO_COMMIT` |
| PR / merge / post-merge | PASS | PR #13 合并为 `24f0aeb`；main 必要门禁通过，Linear 已读回 Done |

## 目标

- 证明任意连续 chunk 能确定性恢复完整 JSONL 行，半行不推进可提交 offset；unresolved pending 进入有界 checkpoint，不阻塞 offset 或后续内容。
- 证明只输出 session、turn、nullable usage 和 terminal outcome 的 allowlisted 结构；内容型 payload、raw JSON/type/error 不进入结果或 diagnostic。
- 证明 lifecycle 只根据 explicit start/context/usage/terminal 归一化，非零 offset 从持久 seed 恢复，乱序和缺失 ID 在无歧义时恢复，在歧义、冲突或有界状态超限时 fail closed。
- 证明本卡不写 SQLite；TOO-253 必须新增 typed GORM checkpoint persistence，把全部返回事实/diagnostic、`NextSeed` 与 `CommittableOffset` 原子写入，禁止从 mutable current facts 反推 seed。
- 固化 terminal-before-start 的 source truth：causal output 是 start→end，但 end source offset 可小于 start source offset；TOO-253 必须放宽 Store 的 offset 顺序约束并做真实 GORM transaction/restart 集成验收。

成功标准：focused/repeat/race/coverage/fuzz 全绿；同一输入的单块与任意分块结果一致；所有 offset 有界；synthetic private marker 在内容型记录、unknown type 和 terminal content 中不可见；全仓 closeout gates 与两轮独立 review 最终通过。

## 执行副作用

- 可能写入的本地文件：可选 coverage profile 写入 `${TMPDIR:-/tmp}`；Wails closeout gate 可能生成 ignored frontend/build 产物，按仓库规则清理。
- 可能访问的服务 / 数据库 / 外部系统：无。测试不打开 Store、不连接 SQLite、不启动 server、不访问网络。
- 可能创建的临时数据：Go testing/fuzz cache 与进程内 synthetic byte slices。
- 明确不会触达的范围：真实 `~/.codex`、默认应用数据库、用户 session/archived files、GitHub Actions、release/tag、外部 API。
- 执行前应先向协作者说明上述副作用和输出位置。

## 前置条件

1. 当前工作目录为仓库根目录。
2. 当前分支或版本包含待验证的 TOO-252 变更。
3. Go toolchain 满足 `go.mod`；focused parser 命令使用 `CGO_ENABLED=0`。
4. 完整 closeout 使用仓库精确锁定的 `wails3 v3.0.0-alpha2.117` 和 frontend lockfile dependencies。
5. GitHub Actions 保持 `actions_disabled_by_user`；本 runbook 不启用、不查询、不 dispatch、不等待 CI。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$PWD}"
cd "$REPO_ROOT"

test -f go.mod
test -f internal/codex/logs/parser.go
git status --short --branch
```

预期结果：命令成功；工作目录是目标仓库；状态输出不包含意外生成文件或真实 fixture。

## 主路径

### 1. Focused、race 与 coverage

```bash
CGO_ENABLED=0 go test ./internal/codex/logs -count=20
CGO_ENABLED=0 go test -race ./internal/codex/logs -count=1
CGO_ENABLED=0 go test -coverprofile="${TMPDIR:-/tmp}/codex-pulse-jsonl-parser.cover" \
  ./internal/codex/logs -count=1
go tool cover -func="${TMPDIR:-/tmp}/codex-pulse-jsonl-parser.cover" | tail -1
```

预期结果：全部退出码为 0；coverage 不低于当前受影响包基线；framer、decoder、lifecycle、integration 和 fuzz seed tests 全部通过。

### 2. Deterministic fuzz

```bash
CGO_ENABLED=0 go test ./internal/codex/logs \
  -run=^$ -fuzz=FuzzStreamParser -fuzztime=10s
```

预期结果：无 panic、hang 或 invariant failure；单块与不同 chunk 切分得到相同事件、diagnostic emission order、统计和最终 offset。

### 3. Privacy 与 protocol contract

```bash
CGO_ENABLED=0 go test ./internal/codex/logs \
  -run 'Privacy|KnownContent|Diagnostics|SupportedRecords|Chunking|Pending|Ambiguous|Conflict' \
  -count=20
rg -n 'RawMessage|json\.RawMessage|map\[string\]any' internal/codex/logs
```

预期结果：测试通过；`json.RawMessage` 只存在于 decoder 内部 transient DTO，不出现在 `parser_types.go` 的公开事实；生产代码不存在 `map[string]any` 通用 payload 下传。

### 4. 全仓与 Pure Go Store 回归

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
CGO_ENABLED=0 go test ./internal/store/... -count=1
if CGO_ENABLED=0 go list -deps ./internal/store | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
make harness-verify
make project-check
git diff --check
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
make verify
```

预期结果：全部退出码为 0；Store 继续在 `CGO_ENABLED=0` 下构建，实际编译依赖不含禁用 driver；version check findings 为空；exact Wails build/bindings/macOS arm64 gate 稳定。

### 5. 清理

```bash
rm -f "${TMPDIR:-/tmp}/codex-pulse-jsonl-parser.cover"
git status --short
```

预期结果：coverage profile 删除；只保留计划内 tracked 修改和仓库既有的 ignored/local state，不留下 synthetic rollout、数据库或构建产物。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| non-contiguous / offset | chunk 结果不同、commit 越过半行 | 记录固定 test 名和 safe offset，不记录输入内容 | 修复 framer，先重跑 focused matrix |
| syntax / privacy | marker 泄露或 raw error/type 出现在输出 | 只记录 diagnostic code 与 test 名 | 收窄 DTO/输出后重跑 privacy 20 次与 fuzz |
| lifecycle | 错误归属、时间倒退被接受、final usage 重复 | 记录 synthetic turn id 与 transition | 修复 state machine，重跑 lifecycle/race |
| restart/checkpoint | 非零 offset 缺 seed、pending/usage/session/closed state 缺失、seed 引用 caller memory | 记录 safe offset/turn id 与 sentinel，不记录内容 | 丢弃 parser，从旧 committed offset + 同事务原样持久化的旧 `NextSeed` 重建 |
| checkpoint persistence | 试图从 `turn_usage` / current projection 反推 seed | 记录 checkpoint version 与 safe state count | 停止集成；TOO-253 增加 typed GORM checkpoint schema/repository 与事务失败重启测试 |
| parser/store offset order | Store 因 `complete_offset < start_offset` 拒绝 terminal-before-start | 记录 synthetic turn id 与两个 safe offset | 停止集成；TOO-253 迁移 DDL/typed validation，保留时间顺序，并重跑 parser→FactBatch→GORM Store→restart/replay |
| fuzz | panic、hang、chunk 不确定 | 保留 Go fuzz corpus，不复制可能含内容的 raw input到提交文档 | 用生成的 corpus focused 重现后修复 |
| full gate | Store/Pure Go/harness/Wails 回归 | 记录 gate 和脱敏摘要，后续 gate 标未执行 | 修复后从完整 gate 重新运行 |

## 结果回写

执行完成后更新本文前部的时间、结论、步骤表、清理和敏感信息处理。只写真实执行结果；未执行项保持 `未执行`。原始长输出只留在本地运行记录，不进入提交版。

普通 Execution 只更新 `CHANGELOG.md -> Unreleased`，不创建版本、tag、GitHub Release 或正式发布。Actions 保持 `actions_disabled_by_user`。
