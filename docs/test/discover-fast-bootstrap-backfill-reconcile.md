# Discover、Fast Bootstrap、Backfill 与 Reconcile Runbook

## 当前验证结果

- 记录时间：2026-07-14（Asia/Shanghai）
- 对应 Issue：TOO-259
- 本轮任务性质：生成 + 执行 + 回写
- 当前结论：`PASS（pre-commit verified）`；implementation review 的 6 个 findings 和 final scope review 的并发 exact replay finding 均已 RED→GREEN并复审到 `ZERO_FINDINGS`；修复后的全仓 race、module/Pure Go/GORM/version/diff与完整 `make verify` post-integration门禁全绿，产物已清理，等待 pre-commit final freeze。
- Actions：`actions_disabled_by_user`；本 runbook 不启用、查询、触发或等待 GitHub Actions。
- Release：不适用；普通 Execution Issue 不创建 tag、release 或正式发布产物。

### 本次执行结果

- 执行时间：2026-07-14
- 本次结论：`PASS（pre-commit verified）`
- 影响范围：创建 Go test 临时 Home、临时 Pure Go SQLite、symlink/文件漂移 fixture、Go/Node工具缓存和本地 ad-hoc package验证产物。
- 清理结果：临时 Home 和数据库由 `testing.T.TempDir()` 自动清理；`.task/`、`bin/`、`frontend/dist/assets/` 与生成的 `frontend/dist/index.html` 已删除；无常驻进程、外部数据或发布产物残留。
- 敏感信息处理：fixture 只含固定 synthetic Session/Turn；未读取真实 Codex Home，未记录原始用户 JSONL、prompt、response、tool output、凭据、连接串或机器临时路径。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| schema v6 / repository | PASS | typed facts/plan、持久 `reconcile_pass`、freeze/advance/reconcile append/source CAS/resume/latest readback |
| planner / confirmed readers | PASS | incomplete unchanged active append、fast/backfill tier、confirmed root no-follow、drift/symlink/cancel |
| four-stage runtime | PASS | normal/empty/exact replay、partial checkpoint、fresh reconcile pass、root replacement、Drain operation admission |
| focused Store/indexer/logs/index integration | PASS | 5 个相关包 `CGO_ENABLED=0` 全包通过 |
| focused repeated / race / coverage | PASS | final rework 后 5 包 count=20/race；bootstrap 81.5%、Store 79.0%、indexer 85.2%，三包合计 79.9% |
| Pure Go / GORM / module | PASS | 禁用 driver 无命中、bootstrap production 无 Raw/Exec、tidy diff 为空 |
| full local gates | PASS | final rework后全仓 test/race/vet、focused count=20、harness/project/version/diff与完整 `make verify` 全绿；frontend/typecheck/test/build、generated bindings、macOS 15 arm64 ad-hoc app/ZIP验证通过后已清理 |
| implementation review | PASS | 同一独立 reviewer 第二次复审 `ZERO_FINDINGS`、`blocking_findings: 0`、`READY_FOR_CHANGELOG: YES`；首轮 5项与新增 initial unreadable项均关闭 |
| final scope review | PASS | 不同 subagent复审到 `ZERO_FINDINGS`、`blocking_findings: 0`、`FINAL_SCOPE_PASS: YES`；其要求的完整 post-integration rerun已通过 |

### Implementation review 闭环

| finding | 修复与证据 |
| --- | --- |
| confirmed Home root 未绑定后续读取 | Discoverer、snapshot reader、session-index reader 强制核对持久 device/inode；hardlink 保持 source fingerprint 的 root replacement 三阶段测试均 fail closed |
| partial active append 被 fingerprint 误判完成 | 只有 authoritative offset=size 才追平 item；incomplete unchanged source 进入 `active_append` fast plan并从 offset 续读 |
| final unreadable/drift 无可闭合恢复 | schema v6 持久 `reconcile_pass`；Resume 保留旧 pass 审计并冻结新 pass，最新 pass 全 succeeded + issue=0 才 full-ready |
| Drain 未覆盖 Start/Resume admission | Start/Run/Resume 统一 generation operation registry；Drain 关闭、取消、等待后再持久 interrupt |
| Resume 后 exact Start 冲突 | stable ID 只约束 initial pending attempt；合法 Resume lineage 按 immutable facts exact replay no-op |
| initial unreadable 永久重放 pass 0 | pass 0 unreadable 标记 drifted并进入强制 final discovery；reconcile pass仍不可读时阻止 full-ready，修复后 Resume冻结 fresh pass闭合 |
| 并发 exact Start/Resume 被 active guard 误报冲突 | ready/terminal exact Start 先做只读 durable readback；pending 同 identity Start/Resume loser 等待 owner 后再核对，不同 operation 与 Drain 仍保持 generation 互斥 |

## 目标

- 证明 `discover -> fast_bootstrap -> history_backfill -> reconcile` 是 Store 可查询、可恢复的真实状态机，不是仅打印阶段日志。
- 证明 initial plan 固定 lane/tier/action snapshot；Session index 只提供 hint，不进入 rollout parser 或事实表。
- 证明 source generation/checkpoint 是 authoritative cursor，bootstrap progress 只在 source commit 后追平。
- 证明 first-screen ready 与 full-history ready 分离；空 Home 可完成，blocking issue 不伪装 full-ready。
- 证明 final reconcile 捕获扫描期间 added/grown/moved/replaced/deleted，unreadable 保留旧事实，空 reconcile 也持久化固定边界。
- 证明 cancel、Drain、failed/cancelled/interrupted attempt、进程重启与 exact replay 都不清库、不重复已提交事实。
- 证明生产依赖保持 GORM-first Pure Go SQLite，不引入禁用 driver 或 bootstrap CRUD raw SQL。

## 执行副作用

- DB：测试在独立临时 SQLite 中执行 schema v1-v6 migration、synthetic source generation、facts、bootstrap job/plan 与自动清理。
- 文件：测试创建 synthetic `sessions/`、`archived_sessions/`、可选 `session_index.jsonl`、symlink 和读中漂移 fixture；不会访问真实 Codex Home。
- 服务与外部系统：不启动 server，不访问网络、Linear/GitHub API、GitHub Actions 或 release 服务。
- 缓存：允许写入 Go build/test cache；race/coverage 产生正常临时编译文件。
- 全仓 gate：`make verify` 可能生成 ignored frontend/package 产物；closeout 必须核对并清理本轮生成物，保留 tracked 文件。

## 前置条件

1. 位于仓库根目录并处于 TOO-259 独立分支。
2. Go toolchain、Node、npm、Wails CLI 满足仓库锁定版本。
3. 不需要网络、凭据、真实 Codex 数据或 CGO SQLite driver。
4. 工作树中的无关用户修改必须保留；普通卡不得执行 release。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(pwd)}"
cd "$REPO_ROOT"

test -f go.mod
test -d internal/bootstrap
test -d internal/store
test -d internal/indexer
```

预期结果：前置路径存在；命令成功退出；不打印敏感配置。

## 主路径

### 1. Focused、重复、race 与 coverage

```bash
CGO_ENABLED=0 go test ./internal/bootstrap ./internal/codex/logs ./internal/codex/index \
  ./internal/indexer ./internal/store -count=20
CGO_ENABLED=0 go test -race ./internal/bootstrap ./internal/codex/logs ./internal/codex/index \
  ./internal/indexer ./internal/store -count=1

COVER_FILE="${TMPDIR:-/tmp}/codex-pulse-too259.cover"
CGO_ENABLED=0 go test ./internal/bootstrap ./internal/store ./internal/indexer \
  -coverprofile="$COVER_FILE" -count=1
go tool cover -func="$COVER_FILE"
rm -f "$COVER_FILE"
```

预期结果：20 次无 flaky failure；race 无报告；coverage 输出 statement 结果且临时 profile 被删除。

### 2. Pure Go、GORM 与 module 边界

```bash
if CGO_ENABLED=0 go list -deps \
  ./internal/bootstrap ./internal/store ./internal/indexer | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi

if rg -n '\.(Raw|Exec)\(' internal/bootstrap \
  --glob '*.go' --glob '!**/*_test.go'; then
  exit 1
fi

go mod tidy -diff
```

预期结果：production 编译闭包不含禁用 SQLite driver；bootstrap production 路径没有 raw SQL；module 无漂移。canonical DDL/PRAGMA/schema readback/query-plan 的仓储例外不属于本检查。

### 3. 全仓本地 gate

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make verify-architecture
git diff --check
make verify
```

预期结果：全部退出码为 0。若 `make verify` 生成 ignored app/ZIP/frontend 产物，验证后按仓库规则清理；Actions 不属于本轮 gate。

## 必测行为矩阵

| 行为 | 自动化证据 | 预期结果 |
| --- | --- | --- |
| typed schema / checksum | `TestApplicationSchemaV6ChecksumIsFrozen`、runtime schema tests | v1-v5 不变，v6 表/列/FK/index/strict checks 可读 |
| create/freeze/replay | `TestBootstrapRepositoryCreatesFreezesAndReadsTypedPlan` | stable identity exact replay no-op，冲突拒绝 |
| atomic advance | `TestBootstrapRepositoryAdvancesFactsAndItemAtomically` | public job、bootstrap facts、单 item 同事务推进，倒退拒绝 |
| reconcile pass | `TestBootstrapRepositoryAppendsFixedReconcilePassAtomically` | persisted pass（含空 pass）、contiguous ordinal、aggregate denominator、change/issue/timestamp 原子持久化 |
| deleted CAS | `TestBootstrapRepositoryMarksDeletedSourceUnavailableWithFingerprintCAS` | 仅 expected active fingerprint 可标 unavailable，事实不删除 |
| resume lineage | repository resume + runtime restart tests | recoverable terminal clone新 attempt；plan/facts复制，source checkpoint不复制 |
| deterministic initial plan | `TestFreezeInitialPlan*` + incomplete runtime test | incomplete unchanged active append/today/hint 优先，7d/30d/older 稳定，Session index 排除 |
| deterministic final plan | `TestFreezeReconcilePlan*` | action 顺序、pass、ordinal 固定；issue-only 由 summary 计数 |
| confirmed no-follow readers | `TestSnapshotReader*` + root replacement runtime test | persisted Home root、file identity、hint、prefix/size/mtime、chunk/cancel/symlink fail closed |
| normal + repeat | `TestRuntimeRunsFourStagesAndReplaysCompletedBootstrap` | first/full ready 分离；两份 Session 可查；重复 start/run 不重复写 |
| empty Home | `TestRuntimeCompletesEmptyHomeWithDistinctReadinessFacts` | 空 plan 仍写 first/reconcile-plan/full ready 并 succeeded |
| cancel + restart | `TestRuntimeInterruptsAndResumesFromDurableSourceCheckpointAfterRestart` | interrupted status needs-resume；新 Runtime 克隆 attempt 并继续 checkpoint；Resume 后 exact Start no-op |
| source commit / item lag | `TestRuntimeCatchesUpItemAfterSourceCommitWinsCancellationRace` | source 已提交但 item 未推进时，exact active cursor 追平且不重放 facts |
| partial active append | incomplete Resume/cold bootstrap runtime tests | fingerprint 已推进但 offset<size 时继续读到 EOF，Turn 与 cursor 不缺尾部 |
| directory drift | `TestRuntimeFinalReconcileCapturesDirectoryDrift` | late source 进入 reconcile pass 并产生 facts |
| final action matrix | grown/deleted/unreadable + moved/replaced + stale CAS tests | 所有 action 有真实闭环；unreadable/drift/stale CAS 阻止 full-ready 并可由递增新 pass 恢复 |
| initial unreadable recovery | `TestRuntimeInitialUnreadableRecoversThroughFreshReconcilePass` | pass 0 转 drifted并进入 final discovery；修复同一物理 source后 Resume递增 fresh pass并 full-ready |
| empty/fresh reconcile pass | issue-only、final drift recovery tests | 空 pass 仍递增持久 pass；旧 pass 不改写，只有最新 pass 决定 full-ready |
| dependency failure | `TestRuntimeFailsAfterPlanWhenSourceDependencyBecomesUnsafe` | plan-ready job failed + source pause；可克隆恢复，不伪装 full-ready |
| Drain admission | existing Drain test + `TestRuntimeDrainLinearizes*` | Start/Run/Resume 统一 admission；Drain 返回前 operation 已退出且无 queued/running writer |
| concurrent exact replay | concurrent Start/Resume + active Run exact Start tests | loser 等待同 owner 或读取 ready durable facts；不创建第二份副作用，不把 exact replay 误报 active conflict |

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| schema/checksum | v1-v5 漂移、v6 readback 不一致 | 记录版本和稳定错误摘要 | 只修当前 v6；不得改历史 checksum |
| source checkpoint | item/job progress 领先 Store cursor | 记录 source/action/test 名 | 修复 commit 后追平顺序，重跑 Store/indexer/bootstrap |
| readiness | issue/failure 仍写 full-ready，或 first/full 同义 | 记录阶段和 typed facts | 修复状态机，重跑 runtime matrix |
| reconcile | drift 丢失、deleted 删除事实、unreadable 当 deleted | 记录 action kind 与 fixture | 修复 plan/apply CAS，重跑 final reconcile tests |
| recovery | terminal attempt 复活、plan 重新排序、空 pass 重新取边界 | 记录 job lineage/pass | 修复 clone/marker/idempotency，重跑 restart/Drain |
| Pure Go/GORM | 命中禁用 driver 或 bootstrap raw SQL | 记录包/文件 | 替换依赖或 typed GORM 操作，CGO0 重跑 |
| full gate | test/race/vet/harness/project/version/verify 任一失败 | 只回写真实失败项 | 同分支修复并完整重跑，不用 Actions 替代 |

## 清理

- Go tests 的 Home、SQLite、WAL/SHM、symlink 和漂移 fixture 随临时目录删除。
- coverage profile 必须在读取后删除。
- 检查 `git status --short`，清理仅由本轮 gate 生成的 ignored build/package 产物；不删除用户文件或无关修改。
- 不需要 token revoke、服务停止、外部数据 cleanup 或 release rollback。

## 结果回写

- 每轮执行后更新本文前部的当前结论、步骤状态和真实 gate 摘要。
- 已执行写 PASS/FAIL；未执行保持“未执行”，不得用计划替代证据。
- 原始长输出与机器临时路径不提交；提交版只保留命令、稳定测试名、计数、coverage、清理结果和脱敏失败摘要。
- implementation review、post-integration verify 和不同 subagent final scope review 都通过后，才进入 commit/PR/merge。
