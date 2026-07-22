# Quota、Source、Job、Health 与 Settings 查询 Runbook

## 当前验证结果

- 记录时间：2026-07-16（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-269 五类只读 query contract、GORM-first runtime snapshot、敏感字段裁剪与 typed recovery action
- 当前结论：`DONE`；TOO-269 已由 PR #34 self-merge 为 `0df96508386979015dc6363d490f5e89dca0f350`，post-merge verify 与 Linear Done 已完成。implementation closure 2 与不同 subagent final scope review 均无 finding，`CHANGELOG.md -> Unreleased` 已写入；当前作为 TOO-236 Master closeout 的集成证据。
- M6 Master 集成验证（2026-07-17）：`query/... + store + app + scheduler` focused count10 全部通过（Store 102.690s、app 19.047s、scheduler 34.816s），race count3 全部通过（Store 218.458s、app 41.893s、scheduler 78.954s）；完整 `make verify`、全仓 race（Store 82.855s）、vet/tidy/diff、harness/project/version、frontend 5 files/16 tests、generated 319 packages/1 service/15 methods/32 enums/65 models/1 event、arm64/minOS 15 app/ZIP 全部通过。release classification=`issue-only`、version `findings=[]`，最终 diff 仅含 M6 五份 runbook。
- M6 Master final review：独立 subagent 首轮发现本地 ignored Master plan skeleton Medium，补齐后又发现并关闭 `internal/app/service.go` / `Service` 命名真相 Low；最终 Critical/High/Medium/Low 均为 none，`remaining_findings=0`、`blocking_findings=0`、`MASTER_FINAL_REVIEW_PASS:YES`。
- 自动化入口：`internal/query/runtimeinfo/*_test.go`、`internal/store/runtime_query_test.go`、`internal/query/value_test.go`
- 对应 issue：TOO-269
- 结果说明：当前全部使用 synthetic typed facts 和 `testing.T.TempDir()` Pure-Go SQLite；未读取真实 Codex Home/Preferences、用户数据库或 credential，未请求 Wham，未注册 Wails binding，未触发 Actions 或 release。

### 本次执行结果

- 执行时间：2026-07-16
- 当前结论：production pair Rework 本地门禁 PASS；以下最新一轮结果作为当前 reviewer closure 与 merge 证据。
- Rework RED：新增并发 writer、单侧来源失败、完整 action enum 与 health aggregate row 上限测试，确认原实现分别存在跨提交混合快照、成功侧丢失、动作缺口与无界聚合。
- Rework focused20：`internal/query/runtimeinfo 0.894s`、`internal/query 1.284s`、`internal/store 6.896s`。
- Rework focused race10：`internal/query/runtimeinfo 1.555s`；`internal/store` 轮询会话自然结束，`exit 0`。
- Affected full：`go test ./internal/query/runtimeinfo ./internal/query ./internal/store -count=1` 退出 0；Store 10.921s。
- Rework full：`go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`、`go mod tidy -diff`、`git diff --check`、harness、version 与 guards 均退出 0；full race Store 84.243s，version `findings=[]`。
- full race 命令校正：首次误把 `CGO_ENABLED=0` 套到整个 Wails app，因 Wails mac package build constraints 正确失败；随后按计划使用 `go test -race ./... -count=1` 通过。Pure-Go 证明仍由 `CGO_ENABLED=0` 的 Store/query focused tests 与依赖 guard 提供。
- Rework full verify：`npm ci` 恢复 199 packages（audit 0）后，frontend typecheck、2 files/5 tests、Vite build、generated bindings/module stability、arm64/minOS 15 ad-hoc app/ZIP 与完整 `make verify` 全部退出 0。
- Closure RED：`schema_incompatible + invalid_input` 原先返回 `none`，定向测试以期望 `check_source` 失败。
- Closure GREEN：让更具体的 `SourceFailureCode` 优先于通用 `RuntimeErrorClass`，并覆盖 network/timeout/auth/rate-limit/server/schema/cancelled 全部生产配对；count20 与 race10 均退出 0。
- Closure full：affected CGO0、全仓 test/race/vet、tidy、harness、version `findings=[]`、diff/guards 均退出 0；full race Store 81.741s。
- Closure full verify：frontend typecheck、2 files/5 tests、Vite build、generated stability、arm64/minOS 15 ad-hoc app/ZIP 与完整 `make verify` 均退出 0。
- Post-integration：focused20（runtimeinfo 0.871s、query 0.453s、Store 10.913s）、focused race10（runtimeinfo 1.641s、Store 40.519s）、full test/race（Store 82.602s）、vet/tidy/harness/version、CHANGELOG guards、frontend/generated/package 与完整 `make verify` 均退出 0。
- 副作用：Go build/test cache，以及临时目录中的 synthetic SQLite/WAL/SHM。
- 清理结果：临时数据库由 Go test 自动清理；`.task`、`frontend/node_modules`、dist assets/index 与 package `bin` 已移入系统废纸篓，tracked `frontend/dist/.gitkeep` 保留；没有外部进程或用户文件需要清理。
- 敏感信息处理：JSON canary 已检查 path、device/inode、scope、data store key、switch/attempt/task/fingerprint identity 不进入 DTO；底层 cause 只保留内部 error chain，跨端 error text 为固定分类。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| Store runtime snapshot | PASS（Rework focused） | Source/Job/Health page 与 Job detail 单 read transaction；并发 writer 测试证明 summary/items/relations/lifecycle 同一快照 |
| query DTO / redaction | PASS（focused） | Quota wrapper、Source/Job/Health list/detail、Settings metadata 与 JSON canary |
| null / zero / JS-safe | PASS（focused） | byte unit、unknown reason、真实零、revision/generation string、unsafe timestamp fail closed |
| recovery / health level | PASS（Rework focused） | 全部 Source error/failure 与 Health code 枚举映射到固定 command allowlist；resolved critical 不污染 current |
| failure matrix | PASS（Rework focused） | Source 单侧失败保留成功侧并返回 `partial + unavailableKinds`；所选种类全失败才 unavailable |
| bounded health summary | PASS（Rework focused） | active/resolved × 4 severity，最多 8 个 aggregate row |
| focused repeat / race | PASS | `CGO_ENABLED=0` count20 与 race10 均退出 0 |
| full Go / harness / version | PASS（closure full） | affected CGO0、full test/race/vet、tidy、harness、`findings=[]`、diff 与 guards |
| frontend / generated / package | PASS（closure full） | typecheck、5 tests、build、bindings/module stable、arm64/minOS 15 app/ZIP |
| implementation / final scope review | PASS | implementation closure 2 与 final scope review 均 `blocking_findings=0`；`IMPLEMENTATION_REVIEW_PASS:YES`、`FINAL_SCOPE_REVIEW_PASS:YES` |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

## 目标

- 证明 Quota Current、Source、Job、Health 与 Settings response 来自真实 typed domain/persistence facts，而不是静态占位或日志文本。
- 证明 summary/list/detail、稳定 cursor、empty/partial/unavailable/not-found/cancel 和 reopen 语义可重复。
- 证明 Source/Settings/Job/Health 服务端裁剪敏感字段，未知 enum/error/action 与 JS-unsafe integer fail closed。
- 证明 recovery action 只返回固定 command reference，query 不执行修复、不写设置、不触发 migration。
- 本 runbook 不验证 M6 Wails/TypeScript、M7 页面、M8 evaluator/metrics，也不使用真实用户数据或在线 credential。

## 执行副作用

- 可能写入：Go build/test cache；`make verify` 可能生成仓库已忽略的 frontend/build/package 产物。
- 临时数据：`testing.T.TempDir()` 内 synthetic Pure-Go SQLite、WAL/SHM、source/job/health/preferences/quota fixture。
- 外部系统：focused/full Go tests 不访问网络、用户数据库或 Linear/GitHub；完整 `make verify` 在依赖缺失时可能访问 npm registry 恢复 lockfile 依赖。
- 明确不触达：真实 `~/.codex`、auth/token、用户 Preferences/SQLite、Wham、Wails runtime、GitHub Actions、tag/release。
- 若命令或 fixture 被改为真实路径、credential、endpoint 或用户数据库，立即停止并重新确认副作用。

## 前置条件

1. 当前目录为仓库根目录，分支包含 TOO-269 改动。
2. 使用项目锁定 Go toolchain；SQLite 测试必须在 `CGO_ENABLED=0` 下通过。
3. 不注入真实 Codex Home、credential、应用数据库或 HTTP response。
4. GitHub Actions 保持用户停用；本 runbook 不查询、触发或等待 CI。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

git status --short --branch
go version
```

预期结果：目录与分支正确；命令不读取或打印用户路径内容、credential、token、SQLite 行或原始错误。

## 主路径

### 1. Contract、分页、隐私与 reopen 重复验证

```bash
CGO_ENABLED=0 go test ./internal/query/runtimeinfo ./internal/query ./internal/store \
  -run 'Test(QuotaCurrentAndSettings|ListSources|JobAndHealthQueries|RuntimeInfo|HealthLevel|RuntimeFilters|SettingsRejects|RuntimeCursor|NumericValue|RuntimeSourceQuery|RuntimeJobAndHealthQueries|RuntimeQueries|RuntimeQueriesRemain|RuntimeHealthSummary|SourcePartial|RecoveryActionMatrices)' \
  -count=20
```

预期结果：

- Source 本机文件/在线来源按 `(updatedAtMS, sourceKey)` asc/desc 稳定分页，replay 和同一路径 reopen 结果一致。
- Quota wrapper 保留 `quota-current-v1`，公共 meta 为 `query-v1`；known-empty 是 complete empty。
- source/job/health summary/list/detail 与 Settings snapshot/metadata shape 稳定；unknown、真实 0 和不适用可区分。
- resolved critical 不污染 current health；pause/sleep 只能来自 durable lifecycle。
- JSON 不包含 path、device/inode、scope、request/payload、data store、switch/attempt/task/fingerprint/raw error/token。

### 2. Race 与取消/失败恢复

```bash
CGO_ENABLED=0 go test -race ./internal/query/runtimeinfo ./internal/store \
  -run 'Test(QuotaCurrentAndSettings|ListSources|JobAndHealthQueries|RuntimeInfo|HealthLevel|RuntimeFilters|SettingsRejects|RuntimeCursor|RuntimeSourceQuery|RuntimeJobAndHealthQueries|RuntimeQueries|RuntimeQueriesRemain|RuntimeHealthSummary|SourcePartial|RecoveryActionMatrices)' \
  -count=10
```

预期结果：race 退出 0；显式只读 transaction 不产生 writer 竞态或混合快照；cancel/deadline 原样传播，依赖失败/非法存量事实返回 content-free unavailable，detail missing 返回 not-found。

### 3. Pure-Go、GORM-first、依赖与隐私 guard

```bash
if CGO_ENABLED=0 go list -deps ./internal/store ./internal/query/runtimeinfo \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi

if rg -n '\.(Raw|Exec)\(' \
  internal/store/runtime_query.go \
  internal/query/runtimeinfo; then
  exit 1
fi

if rg -n 'Authorization:|Bearer [A-Za-z0-9+/=_-]{12,}|/Users/' \
  docs/test/quota-source-job-health-settings.md \
  docs/design/front/README.md \
  docs/design/details/observability/README.md | rg -v ':(if )?rg -n '; then
  exit 1
fi
```

预期结果：编译链不含禁用 SQLite driver；本卡生产查询不使用 raw SQL；提交版文档不含真实敏感值或机器绝对路径。

### 4. 全仓本地门禁

```bash
go test ./...
go test -race ./...
go vet ./...
go mod tidy -diff
git diff --check
make verify-architecture
make verify
```

预期结果：全部退出 0。GitHub Actions 继续记录 `actions_disabled_by_user`，不是本 runbook 的验证入口。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| cursor / summary | 重复/重启后缺行、重复行、count 与 items contract 不一致 | 记录 endpoint、sort 与稳定 synthetic ID | 修复 GORM predicate/merge 后重跑步骤 1～2 |
| contract | unknown 变 0、bytes 冒充 count、resolved event 污染 current level | 只记录字段与固定 enum | 修复 mapper/summary 后重跑步骤 1 |
| privacy | 路径、内部 identity、raw error/token 进入 DTO/error/docs | 只记录 marker 类别 | 删除泄漏面并重跑步骤 1、3 |
| action | 未注册 command、query 执行修复或设置写入 | 记录 action kind/command key | 收敛 allowlist/只读边界后重跑步骤 1～4 |
| race/Pure-Go | race 或禁用 driver 进入编译链 | 记录 package/依赖名 | 修复后重跑步骤 2～4 |
| harness/build | 任一本地 gate 非零 | 记录 gate 名与脱敏摘要 | 修复后从步骤 4 完整重跑 |

## 清理

```bash
git status --short
```

预期结果：测试临时数据库由 Go 自动清理；若 `make verify` 生成 ignored build/frontend/package 产物，按仓库既有清理流程删除并保留 tracked `.gitkeep`。无需 revoke credential、停止 server 或清理外部数据。

## 结果回写

每次执行后更新本文前部的结论、步骤表和清理结果，只写脱敏摘要。不得写真实 token、路径内容、内部 Store/Preferences identity、原始 error 或临时数据库位置。Actions 保持 `actions_disabled_by_user`，不查询、不触发、不等待；普通 Execution 不发布。
