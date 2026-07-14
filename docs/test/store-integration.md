# Store 保留策略与集成测试 Runbook

## 当前验证结果

- 记录时间：2026-07-14 11:49 CST
- 对应 Issue：TOO-250
- 当前结论：`原 implementation reviewer 已确认 3 个 Medium 全部关闭；CHANGELOG 集成后完整本地验证通过，待 final scope review`
- 已验证：append-only v1→v2 retention index migration 与冻结 v1 checksum；实际 GORM candidate SQL 命中专用索引且无临时排序；terminal resume lineage 逐层释放时 `More` 不提前结束；legacy upgrade 后真实 CRUD/cleanup；subprocess 绕过 `Store.Close` 后重开、interruption 与 cleanup。
- 待验证：不同 subagent 的最终 scope review、PR 合并与 post-merge verify。

## 目标

- 证明 v0.1 长期事实不受 cleanup 影响，短期运行行只在严格早于 24 小时 cutoff 且满足状态/引用条件时删除。
- 证明 maintenance 不抢占已排队普通写，队列有界，close 不丢已接受工作。
- 证明每批独立 commit，取消保留已提交 report，失败整批 rollback，重试不依赖隐式游标。
- 用真实 SQLite 覆盖 fresh/legacy/current、duplicate replay、FK/WAL、concurrency、cancel、failure、close/reopen 与启动中断恢复。
- 证明 Store 编译链保持 GORM + Pure Go，不引入 official CGO driver。

## 副作用与输出位置

- focused/integration tests 只在 `testing.T.TempDir()` 下创建 synthetic SQLite、WAL/SHM、migration backup 和 trigger；正常结束由 Go 自动清理。
- 故障测试只在临时库创建 `fail_retention_job_delete` trigger，测试内删除；不连接默认应用数据库。
- Go test/vet/race 使用本机默认 build/test cache。
- `make verify` 可能生成 ignored 的 `frontend/node_modules/`、`frontend/dist/`、`.task/`、`bin/`；closeout 前按仓库规则精确清理，并保留 tracked `.gitkeep`。
- 不读取真实 Codex JSONL、真实应用数据库、凭据或用户目录，不访问外部服务，不创建 release/tag，不触发 GitHub Actions。

## 前置条件

1. 工作目录是 Codex Pulse 仓库根目录，Go toolchain 满足 `go.mod`。
2. SQLite/store focused gate 使用 `CGO_ENABLED=0`；Wails macOS adapter 的全仓验证使用默认 CGO。
3. 固定依赖为 `gorm.io/gorm v1.31.2`、`github.com/libtnb/sqlite v1.2.0`、`modernc.org/sqlite v1.53.0`。
4. GitHub Actions 为 `actions_disabled_by_user`；本 runbook 只接受本地命令和 review 证据，不等待或触发 CI。
5. 仓库固定版本的 `wails3` 必须已在当前 shell 的 `PATH`；提交版命令不记录机器本地安装目录。

## 验证矩阵

| 维度 | 自动化入口 | 成功判据 |
| --- | --- | --- |
| fresh/current | `TestEnsureApplicationSchemaRecordsVersionedFreshMigration`、`TestStoreIntegrationFreshReplayCleanupInterruptedAndReopen` | 首次建库到 v2/history 2；重复 bootstrap no-op |
| v1/legacy upgrade | `TestApplicationMigrationAppendsRetentionIndexesToFrozenV1`、`TestApplicationMigrationUpgradesCoreOnlyLegacyDatabaseAndPreservesDataInBackup` | v1 checksum 不变且只追加 v2；upgrade 前 backup 可恢复，升级后真实 runtime CRUD/retention 可用 |
| duplicate replay | `TestRepositoryUpsertFactsIsIdempotent`、integration lifecycle | core fact/source attempt exact replay 不增行、不改语义 |
| FK/schema | `TestRuntimeSchemaColumnsForeignKeysAndIndexes`、retention reference test | FK 开启；active/recent health 与 resume lineage 不因 cleanup 丢失 |
| WAL/concurrency | `TestStoreSupportsConcurrentWALReadsAndQueuedWrites` | 并发读写成功，reader 保持 query-only |
| maintenance priority | `TestStorePrioritizesNormalWritesOverQueuedMaintenance` | maintenance 先排队时，后续已排队普通写仍先执行 |
| bounded/cancel | `TestStoreMaintenanceQueueIsBoundedAndSkipsCanceledWork`、`TestCleanupRetentionCancelsBetweenCommittedBatchesAndRetriesWithoutCursor` | 队列满返回 `ErrQueueFull`；批间取消返回 stable cancel + 已提交 report，retry 完成 |
| failure rollback | `TestCleanupRetentionRollsBackFailedBatchAndCanRetry` | 中段 DELETE 失败时 health/job/attempt 同批全保留，移除故障后可重试 |
| query plan | `TestRetentionCandidateQueriesUseDedicatedIndexes` | 实际 GORM DryRun SQL 命中 retention/health/resume 索引，无 `USE TEMP B-TREE` |
| resume lineage | `TestCleanupRetentionRecomputesEligibilityAcrossTerminalResumeLineage` | leaf 删除释放 parent 后 `More=true`，三层 terminal lineage 以三个独立 batch 完成 |
| close/reopen | maintenance close test、integration lifecycle | close 排空 accepted normal/maintenance；两次 reopen 后 schema、状态和数据可读 |
| abnormal startup recovery | `TestStoreIntegrationReopensAfterAbnormalExit`、`TestInterruptAndResumeJobRunsPreserveHistoryAndCursor` | subprocess 提交后直接 `os.Exit` 且不调用 `Store.Close`；重开 committed WAL、running→interrupted、cleanup 与 resume contract 可用 |

## 验证命令

### 1. maintenance 与 retention focused

```bash
CGO_ENABLED=0 go test ./internal/store/sqlite \
  -run 'Maintenance|Close|ConcurrentWAL' -count=1
CGO_ENABLED=0 go test ./internal/store \
  -run 'Migration|Retention|CleanupRetention|StoreIntegration' -count=1
```

成功判据：所有测试退出码为 0；normal-priority、固定 cutoff、引用保护、batch、cancel/retry、rollback 和 reopen 均命中。

### 2. fresh/upgrade/replay/FK 完整矩阵

```bash
CGO_ENABLED=0 go test ./internal/store ./internal/store/sqlite -count=1
```

成功判据：Store、migration、schema、core/runtime repository、retention 与 lifecycle 全部通过。

### 3. Pure Go dependency 与生产 raw SQL 审计

```bash
if CGO_ENABLED=0 go list -deps ./internal/store | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
rg -n '\.(Raw|Exec)\(' internal/store/retention.go
```

成功判据：第一条没有匹配并退出 0；第二条没有输出。`go.mod` 的上游间接 metadata 不等于实际编译依赖。

### 4. affected race 与全仓门禁

```bash
CGO_ENABLED=0 go test -race ./internal/store ./internal/store/sqlite -count=1
go test ./internal/app -count=1
go test ./... -count=1
go vet ./...
go test -race ./...
make harness-verify
make project-check
git diff --check
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
make verify
```

成功判据：全部命令退出码为 0；Wails 可保留既有 macOS deployment-target warning，但不得有 test/vet/race/gate 失败。

## 失败诊断与恢复

| 失败面 | 先检查 | 恢复方式 |
| --- | --- | --- |
| cutoff/误删 | `< cutoff`、状态 allowlist、health/resume `NOT EXISTS` | 修复 GORM scope；从 focused eligibility test 重跑，禁止用恢复脚本掩盖误删 |
| priority/close | normal 与 maintenance queue depth、closing admission、drain 顺序 | 修复 worker 状态机；重跑 maintenance + close + race |
| cancel | observer 是否在 commit 后触发、report 是否只含 committed batch | 用新 context 重试；不得保存或推进 opaque cursor |
| rollback | trigger 是否在同一 writer transaction 中触发 | 确认三类行均未变化，再移除临时 trigger 重试 |
| migration/reopen | user_version、history、backup、WAL/foreign key readback | 转到 `docs/test/migrations.md`，不得 drop/rebuild 真实库 |
| dependency | `go list -deps` 出现禁用 driver | 沿 import chain 移除 CGO dialector，再执行步骤 1～4 |

## 清理与结果回写

- `TempDir` fixture 自动清理；测试进程异常退出时只删除对应临时目录，不触碰默认应用数据库。
- closeout 前清理本轮 ignored 构建产物，不删除用户已有 plan/state/run 文件。
- 执行后更新本文顶部与“本次执行摘要”；未执行 gate 不得写成 PASS。
- Issue 回写记录 `actions_disabled_by_user`；普通 Execution 不做版本 bump、tag、Release 或正式发布。

## 本次执行摘要

| Gate | 结果 |
| --- | --- |
| maintenance focused | PASS：normal-priority、单槽满/取消、close drain/reopen |
| retention focused | PASS（Rework）：v2 index/query plan、24h eligibility、resume lineage `More`、cancel/retry、rollback |
| lifecycle integration | PASS（Rework focused）：fresh/replay/graceful reopen；subprocess 无 Close 异常退出后 committed WAL、running interruption、cleanup |
| Pure Go full Store | PASS：`CGO_ENABLED=0` full store；forbidden driver 与 retention raw-SQL audit 无匹配 |
| affected/full race | PASS：Pure Go affected race 与默认 macOS CGO 全仓 race |
| app/full test/vet | PASS：全仓 test、`go vet ./...` 与 race；仅保留既有 macOS deployment-target warning |
| harness/project/version/diff/make verify | PASS：fixed Wails PATH、199 个 lockfile 依赖、generated stability、arm64/minOS 15/ad-hoc app/zip；version `findings=[]` |
| 独立 review / final scope review | implementation re-review：ZERO_FINDINGS；final scope 的 1 Medium（机器本地 Wails PATH）已关闭，最终 ZERO_FINDINGS / READY_TO_COMMIT |
| Actions | `actions_disabled_by_user`：不触发、不等待、不作为 gate |

执行说明：首次直接运行 `make project-check` 因固定 Wails CLI 不在当前 shell 的 `PATH` 以 `TOOLCHAIN-001` 正确停止；补齐当前 shell 的 Wails CLI PATH 后通过。首次 `make verify` 在 `vue-tsc` 前因 `frontend/node_modules` 未恢复停止；按 lockfile 执行 `npm --prefix frontend ci` 后从完整 verify 重跑通过，199 个依赖 audit 为 0。`glob@10.5.0` 输出既有 deprecated warning，本卡不顺手升级依赖。验证完成后已删除本轮 `frontend/node_modules`、`.task`、`bin` 与 dist 构建文件，保留 tracked `.gitkeep`；go.mod/go.sum/bindings 无漂移。Wails 链接阶段仍有既有 macOS SDK deployment-target warning，相关命令退出码均为 0。
