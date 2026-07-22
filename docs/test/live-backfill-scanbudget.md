# Live / Backfill 双队列与 ScanBudget 验证

## 当前验证结果

- 记录时间：2026-07-15
- 记录目录：Codex Pulse 仓库根目录
- 本轮任务性质：TOO-260 实现验证
- 当前结论：`第五轮 implementation review 与第二次 final scope/freeze review 均零阻断；pre-commit 本地门禁通过，准备原子提交与 PR self-merge`
- 自动化入口：Go package tests、race、repository harness
- 对应 issue：TOO-260
- 结果说明：schema v7、Pure-Go GORM repository、精确 lane 聚合、单重型 owner、真实 IO ScanBudget、bootstrap/live slice、crash-gap recovery 和同 Home 集成路径已通过当前本地回归；GitHub Actions 按 Root Goal 明确停用。

### 本次执行结果

- 执行时间：2026-07-15
- 执行目录：仓库根目录
- 本次结论：`阶段通过`
- 影响范围：`internal/store`、`internal/scheduler`、`internal/bootstrap`、`internal/liveindex`、`internal/codex/logs`、`internal/indexer`
- 清理结果：测试数据库与 confirmed Home fixture 均位于 `t.TempDir()` 并由 Go test 清理；未创建常驻 worker 或外部资源。
- 敏感信息处理：未写入真实凭据、token、cookie、JSONL 原文、机器绝对临时路径或原始错误正文；测试仅使用合成 rollout。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| schema / repository / budget focused | 通过 | CGO=0；v7 checksum、typed exact/CAS、policy matrix |
| live/backfill 真实集成 | 通过 | 同一 confirmed Home；live 先执行，backfill 可多 slice 持续完成 |
| 全量 Go test | 通过 | 默认 CGO `go test ./... -count=1`；Wails 仅有既有 macOS deployment target warning |
| Pure-Go 核心包 | 通过 | CGO=0 核心包通过；根/app 因 Wails macOS build constraint 不支持 CGO=0，不属于 SQLite driver 回退 |
| race | 通过 | 全仓 `go test -race ./... -count=1`；取消/owner focused race `count=20` |
| 核心重复验证 | 通过 | Store SQLite、store、scheduler、logs、bootstrap、liveindex CGO=0 `count=20`；取消/owner focused CGO=0 `count=100` |
| coverage | 通过 | scheduler 76.6%、liveindex 69.1%、bootstrap 78.9%、logs 83.5%、indexer 85.2%、store 78.1% |
| repository harness | 通过 | `make verify-architecture` |
| 独立 implementation review | 通过 | 第五轮确认 cancellation finding 与此前 10 项阻断全部 CLOSED；`blocking_findings: 0`、`READY_FOR_CHANGELOG: YES` |
| CHANGELOG closeout | 通过待 final freeze | 按 changelog-style 在 Unreleased feature 恢复唯一 `[TOO-260]` 已完成事实条目；diff、harness、version gate 通过 |
| 最终 scope review / post-merge | review 通过 / post-merge 待执行 | 第二次复审 `FINDINGS: 0`、delta/freeze/pre-commit 全 YES；待 merge 后在 main 权威 commit 重跑 |
| 聚合 verify / macOS package | 通过 | 前端 typecheck/test/build、generated bindings、arm64 ad-hoc app/zip bundle 校验全部通过；仅有既有 linker warning与 npm glob deprecation warning |

### Rework finding 与当前证据

| Finding | 修复事实 | 当前测试证据 |
| --- | --- | --- |
| target succeeded、scheduler commit缺失 | target recovery返回typed `JobRun`；running task按terminal状态补齐同一scheduler cycle，不盲目Resume | recording executor + 真实 live + 真实 bootstrap crash-gap集成 |
| 多Service并发重型slice | Service cycle mutex + Store“已有running不得claim第二个task”门禁 | 两个Service共享Repository并发，第二个executor零调用，首轮完成后可重试成功 |
| 全局500 snapshot截断 | Store直接聚合lane depth/oldest candidate/tail；recoverable task使用keyset分页 | lane snapshot与page-size=1恢复分页repository测试 |
| claim/promotion CAS停止worker | claim loser返回`ErrSchedulerRetry`；promotion-before-claim重新选择，promotion-before-commit只重试同一commit | 双contender与两条promotion CAS测试；dependency failure后长期Run继续下一task |
| prefix proof未计入byte budget | `BytesRead`统计proof+正文实际ReadAt，`ContentBytes`记录内容推进；不足proof下限零读取拒绝 | 可观测ReadAt seam验证4103预算恰好4103物理字节 |

### 第二轮 Rework finding 与当前证据

| Finding | 修复事实 | 当前测试证据 |
| --- | --- | --- |
| 活跃 Service 可被另一 Service 恢复/换绑 | 数据库旁 `0600` non-symlink lock 使用 OS advisory lease；`Run` 从 recovery 前持有到退出，直接 `RunCycle` 非阻塞竞争，只有正常释放/进程退出后可接管 | 底层 lease 串行、取消、释放与不安全路径测试；双 Service 证明活 owner 时 recovery/executor 零调用，释放后完成接管 |
| lane depth/candidate/tail 可能来自不同读时点 | `SchedulerQueueSnapshot` 的两 lane count/candidate 与全局 tail 全部置于同一 GORM 只读事务 | 在 live count 后并发提交 enqueue；当前 snapshot 保持旧视图，下一 snapshot 完整看到新 task |
| 恢复时间戳可能落后于原 owner 最新提交 | recovery queue order/时间戳以 task `updated_at_ms`、旧 queue order 与全局 tail 的最大值为单调下界 | 两个独立 Service 使用不同测试时钟，原 owner interrupted 后接管仍能 CAS requeue 并完成 task |

### Final scope Rework finding

| Finding | 计划修复 | RED 证据状态 |
| --- | --- | --- |
| mutable task 导致 exact replay conflict | schema v7 固定 admission target/service class，只比较 immutable admission payload并返回当前 durable task | promotion/yield/recovery/terminal replay与不同初始class冲突均GREEN |
| 同毫秒 cycle 用随机 ID 重排 fairness history | cycle 增加 SQLite 全局自增 commit order，recent/task history统一按 commit order | 同毫秒、非字典序ID、live/backfill交错与Repository重建读回GREEN |
| time-budget yield 绕过 reader after-stat | reader 增加受控 stop contract，完成 after-fstat 后才返回；drift优先 | reader stable/漂移、bootstrap time+drift、live time+drift均GREEN |

### 第四轮 implementation Rework finding

| Finding | 修复事实 | 当前测试证据 |
| --- | --- | --- |
| cancel 与 GORM 只读 transaction 重叠时只返回 `SQLITE_INTERRUPT` / `sql.ErrTxDone` | queue snapshot hook 可确定性注入 transaction 错误；SQLite `View` 仅在 callback 已失败且 context 同时结束时 join 双因果，既保留 `context.Canceled` 也保留原始 driver/transaction 错误 | RED 精确返回 `sqlite view: sql: transaction has already been committed or rolled back`；GREEN 同时通过 `errors.Is(context.Canceled)` 与 `errors.Is(sql.ErrTxDone)`，focused CGO=0 count=100、race count=20、6包 count=20/race及全仓 test/race通过 |

## 目标

- 验证 live 优先不饿死 backfill，lane 内 round-robin 与 8:1 公平跨 cycle 持久。
- 验证 ScanBudget 在 normal、low-power、CPU/memory pressure 与 Store pressure 下产生确定预算。
- 验证 bootstrap/live 只从 authoritative source checkpoint 续传，prefix proof与正文合计不超过byte budget，内容推进与物理IO分开观测。
- 验证取消、panic、dependency error、进程遗留 running task 都有可读回结果。
- 验证新增 SQLite 路径使用 GORM + Pure Go driver，不引入 CGO SQLite 依赖。

## 执行副作用

- Go test 会写入本机 Go build/test cache。
- 测试在私有临时目录创建合成 Codex Home 与 SQLite/WAL 文件，结束后自动清理。
- 不访问网络、Linear、GitHub Actions 或真实 Codex Home；不执行 release。

## 前置条件

1. 当前工作目录为仓库根目录。
2. Go module 依赖已下载。
3. macOS 全量 UI test 使用默认 CGO；Pure-Go 数据路径验证显式使用 `CGO_ENABLED=0`。

## 主路径

### 1. Pure-Go 核心路径

```bash
CGO_ENABLED=0 go test ./internal/scheduler ./internal/liveindex ./internal/bootstrap ./internal/codex/logs ./internal/indexer ./internal/store -count=1
go list -deps ./internal/store ./internal/scheduler ./internal/liveindex ./internal/bootstrap \
  | rg 'mattn/go-sqlite3|gorm.io/driver/sqlite|libtnb/sqlite|modernc.org/sqlite'
```

预期结果：测试全部通过；依赖读回只包含 `github.com/libtnb/sqlite` 与 `modernc.org/sqlite`，不包含两个 CGO SQLite 路径。

### 2. 双队列与真实 target 集成

```bash
CGO_ENABLED=0 go test ./internal/scheduler -run \
  'TestServiceRunCycle|TestServiceRecoverActiveTasks|TestSchedulerIntegration' -count=20
```

预期结果：live priority、8:1 fairness、round-robin、blocked budget、live preempted、跨 Service owner lease、CAS retry、commit readback、terminal crash-gap recovery 与同 Home live/backfill 集成重复通过。

### 3. race 与全量回归

```bash
go test -race ./internal/scheduler ./internal/liveindex ./internal/bootstrap \
  ./internal/codex/logs ./internal/indexer ./internal/store -count=1
go test ./... -count=1
make verify-architecture
```

预期结果：race、全量 Go test 与仓库 harness 全部通过。Wails linker 可输出既有 deployment target warning，但命令必须成功退出。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| schema/checksum | 任一历史 checksum 或 v7 catalog 不符 | 保留测试名与有限错误 | 只追加 migration，修复后重跑 store |
| queue/fairness | 选择顺序或 persisted reason 不符 | 记录 task/cycle typed readback | 新 RED 修复后重复 20 次 |
| source checkpoint | offset 越界、重复事实、budget 多读 | 记录有限 offset/counter | 修复 reader/runtime 后重跑 logs/indexer/bootstrap/live |
| race | 任一 data race | 停止 review | 修复并重跑全部 race packages |
| full gate | 非既有 Wails warning 的失败 | 停止 closeout | 按失败 package focused 修复 |

## 结果回写

review 与最终完整 gate 完成后更新本文件顶部状态；post-merge 只追加 main 权威结果，不删除本次已执行摘要。
