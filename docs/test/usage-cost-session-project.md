# Usage、Cost、Session 与 Project 查询 Runbook

## 当前验证结果

- 记录时间：2026-07-16（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-268 usage/cost trend、Session/Project bounded query、opaque cursor、safe attribution 与 reconciliation
- 当前结论：`DONE`；TOO-268 已由 PR #33 self-merge 为 `c8b6daabd2e9e31e03bf8764e7502a461b633671`，post-merge verify 与 Linear Done 已完成；当前作为 TOO-236 Master closeout 的集成证据。
- M6 Master 集成验证（2026-07-17）：`query/... + store + app + scheduler` focused count10 全部通过（Store 102.690s、app 19.047s、scheduler 34.816s），race count3 全部通过（Store 218.458s、app 41.893s、scheduler 78.954s）；完整 `make verify`、全仓 race（Store 82.855s）、vet/tidy/diff、harness/project/version、frontend 5 files/16 tests、generated 319 packages/1 service/15 methods/32 enums/65 models/1 event、arm64/minOS 15 app/ZIP 全部通过。release classification=`issue-only`、version `findings=[]`，最终 diff 仅含 M6 五份 runbook。
- M6 Master final review：独立 subagent 首轮发现本地 ignored Master plan skeleton Medium，补齐后又发现并关闭 `internal/app/service.go` / `Service` 命名真相 Low；最终 Critical/High/Medium/Low 均为 none，`remaining_findings=0`、`blocking_findings=0`、`MASTER_FINAL_REVIEW_PASS:YES`。
- 自动化入口：`internal/store/analytics_query_*_test.go`、`internal/query/usagecost/*_test.go`
- 对应计划 / issue：`.agents/plans/2026-07-16-too-268-usage-cost-session-project.md` / TOO-268
- 结果说明：全部业务 fixture 使用 `testing.T.TempDir()` 中的 Pure-Go SQLite 或进程内 typed reader；不读取真实 Codex Home、JSONL、auth、prompt/response/tool content，也不访问业务网络。Actions 按用户策略保持停用。

### 本次执行结果

- 执行时间：2026-07-16
- 执行目录：仓库根目录
- 本次结论：`initial 与 post-integration 两轮 full test/race/vet/tidy + harness/project/version + make verify/package PASS；review rework focused repeat/race PASS`
- 影响范围：Go build/test cache；Store tests 在测试临时目录创建 SQLite、WAL/SHM 并执行 application migrations；full gate 临时安装 lockfile npm dependencies、锁定 Wails CLI，并生成 frontend/build/package ignored 产物。
- 清理结果：测试临时目录由 `testing` 清理；package clean 后先预览，再 scoped clean `frontend/node_modules`、`frontend/dist`、`.task` 与 `bin`；临时 Wails CLI 目录已删除。没有 server、端口、后台进程或外部业务写入。
- 敏感信息处理：只使用 synthetic ID、pricing marker 和 content-free error marker；提交版不记录机器临时目录、真实路径、SQL 参数、generation ID、driver cause 或凭据。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| Store usage/session/project TDD | PASS | single View、GORM-first、fallback/unavailable、keyset、reconciliation、restart |
| Query service TDD | PASS | day/week/month、safe DTO、JS-safe、opaque cursor、partial/known-empty、error privacy |
| Focused repeat/race | PASS | 初始/rework 后 Store analytics count20/race10、usagecost 全包 count20/race10、CGO=0；final scope Low 修复后含 calendar-range test 的 Store count20/race10 再次 PASS |
| GORM / privacy guards | PASS | production analytics query 无 `.Raw` / `.Exec`；不输出 cwd/raw model/path/content |
| full repository / integration | PASS | initial 与 CHANGELOG 集成后均通过 full test/race/vet/tidy、harness/project/version、frontend/generated、arm64 ad-hoc app/zip |
| implementation review | PASS | 独立 subagent 首轮 5 项 findings 均闭环；`blocking_findings=0`、`IMPLEMENTATION_REVIEW_PASS:YES` |
| final scope review | PASS | 不同 subagent 首轮唯一 Low 已修复并 closure；`remaining_findings=0`、`blocking_findings=0`、`FINAL_SCOPE_REVIEW_PASS:YES` |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

## 目标

- 证明概览可按 IANA 本地日范围返回 day/week/month token 与 API 等价成本趋势，并保留 pricing version、unpriced、unknown 与真实零。
- 证明 Session list/detail 只使用安全 attribution，支持 active/idle、project/model/time filter、三种 null-last sort 和无重复/缺口 keyset。
- 证明 Project list/detail 保留 unknown/conflict/invalid dimension，按 range-level confidence 筛选，并使 global/matched/page/detail daily totals 可对账。
- 证明缺 usage rollup 时 Overview/Session 采用显式 partial，Project 采用 fatal unavailable；查询路径不重建 ledger、不写数据库。
- 证明未指定 timezone 且存在多个 active generation 时 Session 返回 `rollup_ambiguous` 安全降级；混合 priced/unpriced 始终 partial，pricing version 与 unpriced reason counts 必须和 totals 严格一致。
- 证明跨端 DTO 不包含 generation、cwd/root path、raw model、SQL、driver cause 或对话/工具内容。

## 执行副作用

- 可能写入的本地文件：Go build/test cache；Store tests 在 `testing.T.TempDir()` 创建临时 SQLite、WAL/SHM 并执行 schema migration。完整 `make verify` 还可能创建 ignored `frontend/node_modules`、`frontend/dist`、`.task` 与 `bin` 产物。
- 可能访问的服务 / 数据库 / 外部系统：focused 路径只访问测试临时 SQLite，不访问网络或外部系统；clean full gate 在 npm cache 缺失时可能访问 npm registry。
- 可能创建的临时数据：synthetic Session/Turn/usage/cost/project attribution、active cost generation 与查询 cursor；全部位于测试进程或临时 SQLite。
- 明确不会触达的范围：真实 Codex Home/JSONL/auth、用户数据库、Wham、云账单、GitHub Actions、release、系统 cron 或长期后台进程。
- 执行前必须先说明上述副作用；若命令范围改变，先更新本 runbook。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：待验证的 TOO-268 分支，或包含其变更的最终主分支。
3. 必需命令：Go 1.25 toolchain、`make`、`rg`；full gate 另需 Node/npm 与仓库锁定的 Wails CLI。
4. 必需配置：focused 无配置；full gate 由执行环境注入 `WAILS_BIN_DIR`，提交版不记录实际值。
5. 必需测试环境：不需要账号、凭据、业务网络或真实用户数据；`CGO_ENABLED=0` 必须可运行 focused tests。

## 测试变量 / 初始化

```bash
set -euo pipefail

test -f go.mod
test -d internal/query/usagecost
test -f internal/store/analytics_query_repository.go
test -f docs/test/usage-cost-session-project.md
```

预期结果：四个路径存在；命令不读取或打印敏感配置。

## 主路径

### 1. Focused Store / Service

```bash
go test ./internal/store \
  -run 'Test(UsageCostRange|ListSessionAnalytics|SessionAnalytics|ListProjectAnalytics|ProjectAnalytics|ValidateAnalyticsRange)' \
  -count=20
go test -race ./internal/store \
  -run 'Test(UsageCostRange|ListSessionAnalytics|SessionAnalytics|ListProjectAnalytics|ProjectAnalytics|ValidateAnalyticsRange)' \
  -count=10
go test ./internal/query/usagecost -count=20
go test -race ./internal/query/usagecost -count=10
CGO_ENABLED=0 go test ./internal/store ./internal/query/usagecost -count=1
```

预期结果：全部退出 0，无 race report；CGO 关闭后仍使用 GORM + Pure-Go SQLite 完成真实 migration/read/restart 测试。

### 2. GORM / privacy / dependency guards

```bash
if rg -n '\.Raw\(|\.Exec\(' internal/store/analytics_query*.go internal/query/usagecost; then
  exit 1
fi
if rg -n 'initial_cwd|current_cwd|root_path|git_remote|prompt|response_body|tool_output' \
  internal/query/usagecost; then
  exit 1
fi
go list -m all | rg 'gorm.io/gorm v1.31.2|github.com/libtnb/sqlite v1.2.0|modernc.org/sqlite v1.53.0'
go vet ./internal/store ./internal/query/usagecost
gofmt -d internal/store/analytics_query*.go internal/query/usagecost
git diff --check
```

预期结果：两个 deny-list 搜索均无输出；依赖版本精确命中；vet/gofmt/diff 退出 0。固定 GORM `Select/Joins/Where/Order` 片段允许存在，但任何 client field/operator 不得进入 SQL 拼接。

### 3. Full repository

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
make harness-verify
: "${WAILS_BIN_DIR:?set WAILS_BIN_DIR to the directory containing wails3}"
test -x "$WAILS_BIN_DIR/wails3"
test "$("$WAILS_BIN_DIR/wails3" version 2>&1)" = "v3.0.0-alpha2.117"
PATH="$WAILS_BIN_DIR:$PATH" make verify-project
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
if [ ! -x frontend/node_modules/.bin/vue-tsc ]; then
  npm --prefix frontend ci
fi
PATH="$WAILS_BIN_DIR:$PATH" make verify
```

预期结果：全部退出 0；Actions 不是验证入口。允许仓库既有 macOS deployment linker warning，但不得掩盖新增 error。

### 4. 清理与读回

```bash
PATH="$WAILS_BIN_DIR:$PATH" wails3 task package:clean
git clean -ndX -- frontend/node_modules frontend/dist .task bin
git clean -fdX -- frontend/node_modules frontend/dist .task bin
git status --short
```

预期结果：先预览且只清理列出的 ignored build/dependency/package 目录；最终只保留 TOO-268 code/test/design/runbook/CHANGELOG 变更。测试临时 SQLite 已由测试框架清理。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| Store snapshot/reconciliation | generation 混用、totals 漂移、unknown dimension 丢失、query 写入 | 记录固定测试名和脱敏 invariant，不记录 SQL/row | 修复 Store projection 后重跑步骤 1 |
| cursor/filter | tamper/cross-sort 通过、分页重复/缺口、任意字段到达 Store | 记录固定 field/endpoint，不记录用户值 | 修复 specification/codec 后重跑步骤 1 |
| null/cost/privacy | unknown/0 混淆、JS unsafe 被舍入、私密字段进入 JSON/error | 只记录固定 error class | 修复 mapper 后重跑步骤 1～2 |
| full/control | 任一命令非零 | 记录 gate 名和脱敏摘要；后续项保持未执行 | 修复后从失败 gate 重跑，最终再完整执行 |
| cleanup | ignored 产物超出允许目录或残留 | 记录目录类别，不写机器绝对路径 | 停止，预览后 scoped clean 并读回 status |

## 结果回写

完成后更新本文前部结论和步骤表，仅写真实命令、退出结论与脱敏时长。不得记录真实路径、generation/row ID、cursor payload、token/header/cookie、SQL 参数、driver cause、机器临时目录或原始响应。Actions 保持 `actions_disabled_by_user`；普通 Execution 不发布。
