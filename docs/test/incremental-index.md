# Incremental Index Runbook

## 当前验证结果

- 记录时间：2026-07-14
- 本轮任务性质：生成 + 执行 + 回写
- 当前结论：`PASS`（第五轮 review 的 Usage-only Session 绑定 finding 已完成 RED/GREEN；focused、CGO0 count=20、coverage、race 与完整集成 gate 均已重跑）
- 自动化入口：`internal/store/*ingest*_test.go`、`internal/indexer/*_test.go`
- 对应 issue：TOO-253
- 结果说明：schema v3、typed checkpoint、active append、durable building CAS/精确 sibling restart、多跳 physical replacement、target+commit-epoch batch identity、generation Session identity、Usage-only Session 绑定、canonical Session source kind、sibling/dependent building invalidation、superseded staging cleanup、EOF base CAS、authoritative Session replacement、空快照、half-line restart、terminal-before-start、counter epoch、job cursor 与事务故障回滚已通过 synthetic fixture 验证。五轮 implementation review 累计 17 个 findings 已完成 RED/GREEN 并通过完整重验；第六轮复审 `blocking_findings: 0`，changelog 写入后的 post-integration 全矩阵也已通过。

### 本次执行结果

- 执行时间：2026-07-14
- 本次结论：`PASS`
- 影响范围：仅创建 Go test 临时目录、临时 Pure Go SQLite 数据库和语言运行时 test/build cache。
- 清理结果：临时数据库和 synthetic Codex home 由 `testing.T.TempDir` 自动清理；没有常驻进程或外部数据。
- 外部系统：未访问真实 Codex home、网络 API、GitHub Actions 或其它 live 系统。
- 敏感信息处理：fixture 只含固定 synthetic ID/path；未写入真实 JSONL、prompt、response、reasoning、tool output、凭据、连接串或机器本地临时路径。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| schema/checkpoint focused tests | PASS | v1/v2 checksum、v3 migration、完整 seed/projector roundtrip、invalid bounds/opaque field |
| Store/indexer tests | PASS | `CGO_ENABLED=0` 下两个包通过 |
| coverage | PASS | Store 78.9%、Store/sqlite 74.5%、indexer 85.3% |
| race | PASS | Store/indexer race 通过 |
| transaction fault matrix | PASS | diagnostic、checkpoint、fact、source、job、activation、prepare/winner/active staging cleanup 全部 rollback |
| full integration gate | PASS | count=20、全仓 test/race/vet、harness、project-check、version check 与 `make verify` 全部通过 |

## 目标

验证 rollout parser 到 SQLite 的 exactly-once-like 本地提交协议：

- 完整行 facts、diagnostics、parser/projector checkpoint、source offset 与可选 job cursor 原子提交。
- 半行不推进 durable offset；重启后从旧 offset 重读且事实只出现一次。
- append 使用 fingerprint/offset CAS；truncate、replace 与 parser upgrade 使用持久 building generation。
- building target/parser 再漂移时必须提交精确 CAS token，旧 stream 失效并从新 generation offset 0 重放。
- building 未到 EOF 时旧 facts 完整可见；EOF 原子替换同 session 陈旧事实并切换 active generation。
- EOF 使用持久 active base token 做 CAS；空快照、Session A→B 和 metadata 缩短/改变都按 generation authoritative snapshot 替换。
- active base 前进或 building winner 激活会原子 supersede sibling/dependent building、删除 staging receipt、保留 diagnostics；失败时整笔回滚。
- receipt/diagnostic key 使用完整 target identity + 严格单调 commit epoch；连续 metadata-only move 与 target 往返可独立 replay。
- checkpoint、所有 session-scoped facts 与 generation 内既有 Session ID 强绑定；Session ID/source kind 不得在同 generation 漂移。
- ingest 的每个 TurnUsage fact 必须在同一 FactBatch 中携带可解析 Session 上下文；Usage-only batch 不能借用另一 source/session 的同 generation turn。
- terminal-before-start 保留原始 source positions；nullable token 与 counter epoch 不丢语义。
- Store/indexer 生产编译依赖为 GORM + Pure Go SQLite，不包含 CGO SQLite driver。

## 执行副作用

- DB：每个测试创建独立临时 SQLite 文件，执行 application schema bootstrap/migration、synthetic seed 与自动清理。
- 文件：测试创建临时 `sessions` / `archived_sessions` synthetic JSONL；`make verify` 在 ignored `bin/` 下生成本地 ad-hoc `.app` 与 ZIP 并执行签名/解包校验；不读取真实用户目录。
- 缓存：允许写入 Go build/test cache；race/coverage 会产生正常临时编译产物。
- 服务与外部系统：不启动 server，不访问网络，不触发或查询 GitHub Actions，不执行发布或上传。
- 故障注入：测试数据库内短暂创建 SQLite trigger，测试结束随临时数据库删除。

## 前置条件

1. 位于仓库根目录。
2. Go toolchain 满足 `go.mod`。
3. 不需要 CGO、网络、凭据或真实 Codex 数据。
4. 工作树中的无关用户修改必须保留。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(pwd)}"
cd "$REPO_ROOT"

test -f go.mod
test -d internal/store
test -d internal/indexer
```

预期结果：所有前置路径存在，命令成功退出，不打印敏感配置。

## 主路径

### 1. Store 与 Indexer 稳定性

```bash
CGO_ENABLED=0 go test ./internal/store/... ./internal/indexer/... -count=20
CGO_ENABLED=0 go test -race ./internal/store/... ./internal/indexer/... -count=1
CGO_ENABLED=0 go test -cover ./internal/store/... ./internal/indexer/... -count=1
```

预期结果：

- 20 次重复执行无 flaky failure。
- race detector 无 data race。
- coverage 命令输出两个包的 statement coverage。

### 2. Pure Go 编译依赖

```bash
if CGO_ENABLED=0 go list -deps ./internal/store ./internal/indexer \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
```

预期结果：无输出并成功退出。`go.mod` 的模块图可能包含 GORM 上游测试元数据；本 gate 检查实际 Store/indexer 生产编译依赖。

### 3. 全仓集成 Gate

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make harness-verify
PATH="/tmp/codex-pulse-tools/bin:$PATH" make project-check
git diff --check
python3 .agents/skills/project-version-release/scripts/project_version_release.py check --repo "$PWD" --json
PATH="/tmp/codex-pulse-tools/bin:$PATH" make verify
```

预期结果：全部命令成功退出；`make verify` 可以生成 ignored 的本地 package-verify 产物，但普通 Execution Issue 不创建 release、不上传产物。

## 必测行为矩阵

| 行为 | 自动化证据 | 预期结果 |
| --- | --- | --- |
| incomplete tail + restart | `TestIngesterRestartsFromCommittedOffsetAndReplaysHalfLineOnce` | durable offset 停在最后完整行；重启后 turn 只出现一次 |
| open turn restart | `TestIngesterRestoresOpenTurnProjectionAcrossRestart` | 保留 start offset/model/cwd/effort，terminal 正常完成 |
| terminal-before-start | `TestIngesterPersistsTerminalBeforeStartWithoutReorderingOffsets` | `complete_offset < start_offset` 原样持久化 |
| counter reset | `TestIngesterRestoresCounterEpochAcrossRestart` | known counter 下降开启 epoch+1；NULL 不伪造下降 |
| truncate / same-ID replace | `TestIngesterKeepsOldFactsVisibleUntilReplacementEOF` | EOF 前旧 facts 可见；EOF 后只见新 generation |
| building target drift | `TestIngesterSupersedesGrowingBuildingAndRejectsStaleStream` | 精确 CAS supersede 旧 building；新 generation 从 0 重放；旧 stream 失效 |
| durable building recovery | `TestIngesterSupersedesGrowingBuildingAndRejectsStaleStream`、`TestIngesterResumesInitialPhysicalReplacementWithoutActiveSnapshot` | previous 只含 active 或为空时仍从 durable building token 恢复；非零 offset 可用空 EOF 激活 |
| durable sibling recovery | `TestIngesterResumesExactDurableSiblingAndSupersedesCompetitor` | crash 后 exact source identity 优先于相同 path/lineage competitor；winner EOF 激活并清理 sibling |
| building parser/identity drift | `TestPrepareGenerationSupersedesDriftedBuildingWithCAS`、`TestPrepareGenerationSupersedesBuildingAcrossPhysicalReplacement` | parser/target/new identity 受控重启；stale token 不改状态 |
| multi-hop physical replacement | `TestIngesterSupersedesPhysicalReplacementChainFromDurableBase`、`TestPrepareGenerationSupersedesInitialPhysicalReplacementChain` | active/base 与 immediate predecessor 分离；A/B 均 unavailable，C 激活 |
| new physical identity | `TestIngesterReplacesNewPhysicalIdentityEndToEnd` | replacement source 激活，旧 source unavailable，batch 不误用旧 fingerprint |
| empty / Session replacement | `TestCommitIngestBatchRebuildsSameSourceToEmptySnapshot`、`TestCommitIngestBatchRebuildReplacesSessionIdentity`、`TestCommitIngestBatchRebuildReplacesSessionMetadataAuthoritatively` | 清理旧事实/source session；A→B 与 authoritative metadata 生效 |
| dual replacement lineage | `TestCommitIngestBatchRejectsStaleDualReplacementAtEOF` | 同一 active base 只允许一个 replacement 在 EOF CAS 激活 |
| sibling/dependent cleanup | `TestCommitIngestBatchSupersedesInitialSiblingBuilding`、`TestCommitIngestBatchSupersedesDependentsWhenActiveBaseAdvances` | winner 或 active base 前进后 loser 进入 superseded，staging 清零，物理 loser unavailable，diagnostics 保留 |
| parser upgrade | `TestIngesterRebuildsWhenParserVersionChanges` | 新 building generation 从 offset 0 开始 |
| repeated zero-progress commit | `TestCommitIngestBatchReplacesRepeatedZeroProgressReceipt` | 同 offset 不同 fingerprint receipt 不冲突；exact replay 与历史 diagnostics 均保留 |
| metadata-only archive move | `TestIngesterCommitsMetadataOnlyMoveWithoutReplayingFacts` | path/file kind 更新，不重放 facts；move 后增长和 parser rebuild 保持 canonical Session source kind |
| metadata-only repeated/return move | `TestCommitIngestBatchAcceptsRepeatedMetadataMovesWithSamePhysicalFingerprint` | 相同物理 digest 的连续 path/source-kind snapshot 与 A→B→A 往返使用独立 batch identity，exact replay 与历史 diagnostics 均稳定 |
| Session identity | `TestCommitIngestBatchRejectsSessionFactWithoutCheckpointSession`、`TestCommitIngestBatchRejectsSessionIdentityDriftWithinGeneration` | scoped fact 必须匹配 checkpoint Session；同 generation Session ID 不漂移 |
| commit epoch | `TestCommitIngestBatchRejectsDistinctBuildingCommitAtSameTime` | distinct commit 的时间必须严格前进，保证 batch identity 不复用 |
| exact replay / conflict | `TestCommitIngestBatchActivatesInitialGenerationAtomicallyAndReplays`、`TestCommitIngestBatchExactReplayTreatsNilDiagnosticsAsEmpty` | 完全一致 no-op；nil/empty diagnostic 语义相等；同 key 不同事实拒绝 |
| transaction faults | `TestCommitIngestBatchRollsBackEveryActiveAppendPhase`、`TestPrepareGenerationRollsBackSupersededStagingCleanup`、`TestCommitIngestBatchRollsBackCompetingBuildingCleanup`、`TestCommitIngestBatchRollsBackDependentCleanupOnActiveAdvance` | 普通提交与 prepare/winner/active cleanup 任一阶段失败均无部分写入 |
| activation fault / resume | `TestCommitIngestBatchRollsBackActivationAndCanResume` | old active 保持完整，移除 trigger 后从旧 building checkpoint 成功恢复 |

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| schema/checksum | 任一 frozen checksum 或 migration readback 不一致 | 记录 migration version 与脱敏错误摘要 | 核对历史对象集；不得直接改旧 checksum |
| checkpoint decode | version、size、unknown field、seed/projector drift | 记录稳定错误类别 | 修复 typed mapping/validation 后重跑 focused test |
| transaction fault | checkpoint/source/facts 任一越前 | 记录失败阶段和旧/新安全 offset | 修复 transaction ordering，重新打开 stream 后重跑 |
| Pure Go gate | 命中任一 CGO SQLite package | 记录包名 | 修正 adapter/import 后以 `CGO_ENABLED=0` 重跑 |
| race/flaky | race 或 count=20 任一次失败 | 记录 test 名与稳定错误摘要 | 使用同一 synthetic fixture 定向重跑，不读取真实数据 |

## 清理

测试不需要手工 cleanup。若测试被强制中断，只需删除 Go test 自己创建的临时目录；不得清理或修改真实 Codex home。SQLite trigger、WAL/SHM 和 fixture 都与临时数据库一起删除。

## 结果回写

每次执行后更新本文前部的日期、命令结果、coverage 与未执行项。保留历史脱敏摘要，不把已执行结果删回空模板；原始长输出只保留在本地运行记录，不提交机器路径或临时数据库信息。
