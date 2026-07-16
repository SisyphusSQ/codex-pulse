# 通用查询 DTO、分页、筛选与空值契约 Runbook

## 当前验证结果

- 记录时间：2026-07-16（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-267 `query-v1` request/response、分页/排序/筛选、时间、数值、空值和错误 contract
- 当前结论：`DONE`；TOO-267 已由 PR #32 self-merge 为 `d6976c9db107c25dc523bc4a2bd261611e942811`，post-merge verify 与 Linear Done 已完成；当前作为 TOO-236 Master closeout 的集成证据。
- M6 Master 集成验证（2026-07-17）：`query/... + store + app + scheduler` focused count10 全部通过（Store 102.690s、app 19.047s、scheduler 34.816s），race count3 全部通过（Store 218.458s、app 41.893s、scheduler 78.954s）；完整 `make verify`、全仓 race（Store 82.855s）、vet/tidy/diff、harness/project/version、frontend 5 files/16 tests、generated 319 packages/1 service/15 methods/32 enums/65 models/1 event、arm64/minOS 15 app/ZIP 全部通过。release classification=`issue-only`、version `findings=[]`，最终 diff 仅含 M6 五份 runbook。
- M6 Master final review：独立 subagent 首轮发现本地 ignored Master plan skeleton Medium，补齐后又发现并关闭 `internal/app/service.go` / `Service` 命名真相 Low；最终 Critical/High/Medium/Low 均为 none，`remaining_findings=0`、`blocking_findings=0`、`MASTER_FINAL_REVIEW_PASS:YES`。
- 自动化入口：`internal/query/*_test.go`
- 对应计划 / issue：`.agents/plans/2026-07-16-too-267-query-contracts.md` / TOO-267
- 结果说明：测试只构造 synthetic query request、日期、数值和错误，不读取 Store、Codex Home、auth、SQLite 或外部服务。三轮有效 RED 均由目标 contract 缺失或 response invariant 缺口触发；focused repeat/race、全仓 test/race/vet/tidy、control/version 与完整 build/package gate 均已通过。

### 本次执行结果

- 执行时间：2026-07-16
- 执行目录：仓库根目录
- 本次结论：`internal/query count20 / race10 + full repository + make verify PASS`
- 影响范围：Go build/test cache；完整 gate 临时安装 lockfile npm dependencies，并生成 frontend/build/task/package ignored 产物；没有临时数据库、server、端口、业务网络 request 或外部写入。
- 清理结果：先执行 Wails package clean，再 scoped clean `frontend/node_modules`、`frontend/dist`、`.task` 与 `bin`；仅保留 TOO-267 scope 变更。
- 敏感信息处理：错误测试只使用固定 synthetic marker；提交版不记录真实路径、token、Authorization/Cookie、SQL、数据库 row/internal ID、底层 cause 或机器临时目录。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| Gate / design truth / plan | PASS | blockers Done；真实 design 路径、scope、非泛型 query-v1 目标冻结 |
| request/specification TDD | PASS | effective RED -> GREEN；bounded limit/cursor、allowlist、arity、stable tie-breaker、copy/cancel/restart |
| date/numeric/null TDD | PASS | IANA/DST UTC 半开区间；JS-safe token/微美元；unknown 与真实零 |
| response/error TDD | PASS | complete/partial/unavailable；page invariant；content-free error envelope；self-review rework |
| focused repeat | PASS | rework 后 `go test ./internal/query -count=20`，0.526s |
| focused race | PASS | rework 后 `go test -race ./internal/query -count=10`，1.883s；无 race report |
| full Go | PASS | `go test ./... -count=1`、单独可追踪的 `go test -race ./... -count=1`、`go vet ./...`、`go mod tidy -diff` |
| control/version | PASS | harness、带锁定 Wails CLI PATH 的 project gates、project version/release check |
| frontend/generated/package | PASS | 完整 `make verify`；typecheck、5 tests、build、bindings stability、arm64 ad-hoc app/zip verify |
| implementation review | PASS | 首轮 3 个 Medium 均按有效 RED/GREEN 修复；原 reviewer 复核 `blocking_findings=0` |
| changelog/integration verify | PASS | 唯一 `[TOO-267]` Unreleased feature；随后完整 `make verify` PASS，ignored 产物 scoped clean |
| final scope review | PASS | 首轮 1 个 runbook Medium 已修复；不同 reviewer closure `blocking_findings=0` |
| closeout | PASS | PR #32 merged；`main@d6976c9` post-merge PASS；Linear TOO-267 Done |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

## 目标

- 证明每个业务 query endpoint 可冻结 immutable allowlist specification，并得到有界、稳定、不可注入任意字段的 `ValidatedRequest`。
- 证明本地日范围由 IANA timezone 正确换算 DST-aware UTC `[start,end)`，不假设每天固定 24 小时。
- 证明 token/count/微美元在 Wails/TypeScript number 精确范围内使用整数，unknown/null 与真实 0 不混淆。
- 证明 partial、unavailable、not-found、validation、cancel/deadline 和 internal cause 映射为固定 content-free contract。
- 证明 focused contract 测试不访问 Store/SQLite、Wails/frontend、真实用户数据或网络；full repository gate 的工具链副作用单独声明。

## 执行副作用

- 可能写入的本地文件：Go build/test cache；完整 `make verify` 会临时创建或更新 `frontend/node_modules`、`frontend/dist`、`.task` 与 `bin` 下的 ignored dependency/build/package 产物。
- 可能访问的服务 / 数据库 / 外部系统：缺少前端依赖时，`npm ci` 可能访问 npm registry；不会访问业务 API、Store/SQLite、Codex Home、auth 或 GitHub Actions。
- 可能创建的临时数据：Go test 进程内 synthetic DTO/error，以及 Wails/macOS package gate 的临时 bundle 校验目录；没有业务数据库或用户数据。
- 明确不会触达的范围：Codex Home、`auth.json`、SQLite、Wham、Wails runtime、GitHub Actions、release。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：待验证的 TOO-267 分支，或包含其变更的最终主分支。
3. 必需命令：Go 1.25 toolchain、Node/npm、`make`；完整 package gate 需要仓库锁定的 Wails CLI。前端依赖缺失时按 lockfile 执行 `npm --prefix frontend ci`。
4. 必需配置：focused contract 无配置；full repository gate 需要由执行环境注入 `WAILS_BIN_DIR`，其目录内 `wails3` 必须为 `v3.0.0-alpha2.117`。只记录变量名，不把机器实际路径写入提交版结果。
5. 必需测试环境：focused synthetic 单元测试不需要网络、账号或凭据；clean full gate 不需要业务账号/凭据，但 npm cache 未命中时可能需要访问 npm registry。

## 测试变量 / 初始化

```bash
set -euo pipefail

test -f go.mod
test -d internal/query
test -f docs/test/dto.md
```

预期结果：三个路径存在；不打印或读取任何敏感配置。

## 主路径

### 1. Focused contract

```bash
go test ./internal/query -count=20
go test -race ./internal/query -count=10
```

预期结果：全部退出 0；无 race report。测试覆盖 limit/cursor、sort/filter allowlist、stable tie-breaker、DST、JS-safe numeric、unknown/zero、response/error 和取消。

### 2. Source / privacy guards

```bash
gofmt -d internal/query
go list -deps ./internal/query
git diff --check
```

预期结果：无 gofmt/diff；dependency graph 只有标准库和当前 package，不包含 Store/GORM/SQLite/Wails；生产文件不含 raw SQL、timer/ticker/sleep、日志或 secret-shaped literal。

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
PATH="$WAILS_BIN_DIR:$PATH" wails3 task package:clean
git clean -ndX -- frontend/node_modules frontend/dist .task bin
git clean -fdX -- frontend/node_modules frontend/dist .task bin
```

预期结果：全部退出 0；允许仓库既有 macOS deployment linker warning，但不能掩盖新增 error。清理预览只包含上述 scoped ignored 目录，执行后这些产物不再出现。Actions 不是验证入口。

### 4. 清理与工作树

```bash
git status --short
```

预期结果：仅保留 TOO-267 的 code/test/design/runbook/CHANGELOG 变更；完整 `make verify` 生成的 ignored frontend/task/package 产物已按步骤 3 的 scoped clean 流程删除。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| request/specification | 未 allowlist 字段通过、limit 无界、tie-breaker 不稳定 | 记录固定测试名和 field code，不记录 filter value | 修复 validator 后重跑步骤 1 |
| date/numeric | DST 边界错误、unknown/0 混淆、unsafe integer 被接受 | 记录 timezone 场景和固定 field code | 修复 value contract 后重跑步骤 1 |
| error/privacy | cause、path、SQL 或 marker 进入 JSON/Error | 只记录固定 failure class | 修复 mapping 并重跑步骤 1～2 |
| full/control | 任一命令非零 | 记录 gate 名和脱敏摘要；后续未执行项保持未执行 | 修复后从失败 gate 重跑，最终再全量执行 |
| cleanup | ignored 产物残留 | 记录目录类别，不写机器绝对路径 | preview 后 scoped clean，再读回 status |

## 结果回写

执行完成后更新本文前部结论和步骤表：只写真实命令、退出结论和脱敏时长。不得写真实 request value、业务/用户路径、`WAILS_BIN_DIR` 实际值、token/header/cookie、SQL、internal row ID、完整错误 cause、机器绝对路径或临时目录。Actions 保持 `actions_disabled_by_user`；普通 Execution 不发布。
