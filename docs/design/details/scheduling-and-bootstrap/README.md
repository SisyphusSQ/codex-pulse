# Scheduling and Bootstrap

## 设计目标

不同数据源独立调度；后台低资源推进，前台交互优先。用户刷新只追平增量，不与后台启动重复任务，也不隐式全量重扫。

适用机器：`sqmc04`。2026-07-11 读回 Codex Runway `0.0.16` 的刷新间隔为 300 秒，源码默认同为 300 秒，可配置 60～1800 秒。Runway 启动、每个周期和 reset 后会做全量 refresh，成本摘要还可能枚举 JSONL 并整文件读取；Codex Pulse 不沿用这个单一全量刷新模型。

## 数据源周期

| 数据 | 后台刷新 | 前台打开或手动刷新 |
| --- | --- | --- |
| 5h / weekly quota | 正常 5 分钟；低于 20% 或临近 reset 时 2 分钟 | 上次成功超过 60 秒则立即获取 |
| reset credits | 30 分钟 | 打开对应页面或手动刷新时，距上次同类请求已满 60 秒才立即获取 |
| App Server metadata / JSONL 轻量索引 | 常驻 Helper 每 30 秒 coalesced reconcile；相同 metadata 不推进 generation，未变化 rollout 不读正文 | 前台激活或系统唤醒立即触发；增长文件从 durable offset 追平 |
| Session 完整目录对账 | 30 分钟 | 仅显式重建索引时满速 |
| 进程 / 端口状态 | 30～60 秒 | 前台 5～10 秒 |
| 活跃项目 Git 状态 | 2～5 分钟 | 打开项目时立即获取 |
| Pricing | 每天或手动 | 手动 |
| 应用更新 | 默认每小时、最低优先级 | 设置页或菜单手动检查 |
| Session index repair | 不自动 | 仅显式 dry-run |

Quota 网络失败使用 5、10、20、30 分钟带 jitter 退避；手动刷新绕过普通退避但不绕过 60 秒 durable 最小间隔、未来 Retry-After 或结果校验。服务端 Retry-After 作为独立 fence 保存在 source state 中，即使更长的本地退避让 schedule reason 显示为 `network_backoff`，manual 也必须等 server fence 到期；foreground 与 wake 不越过仍生效的普通错误退避。startup、crash recovery 与 Settings reconcile 恢复已持久的错误 due，不以当前重启时钟重新计算并向后推；原 due 已过才立即抓取，schedule 与 source-state 两个 Retry-After fence 始终取较晚者。系统从休眠恢复后，只立即刷新 stale 来源。reset 到达后在 `reset + 3 秒`尝试一次，失败继续退避，不推断已经重置。

## 任务优先级和预算

- `interactive`：用户打开页面或手动刷新，优先返回 quota 等轻量结果，并满速追平已有增量。
- `background`：日常采集，单 worker、限制读取和事务批次，在工作切片间主动 yield。
- `maintenance`：目录对账、索引重建、SQLite checkpoint/vacuum，只在空闲或显式触发时执行。

Go 侧固定一个重型 worker，使用协作式 `ScanBudget`，不动态修改整个进程的 `GOMAXPROCS` 或 nice，避免连 UI 和 SQLite 查询一起降速。默认预算由 service class 和系统压力共同决定：

| class / system | files | bytes | active | yield |
| --- | ---: | ---: | ---: | ---: |
| background normal | 8 | 4 MiB | 50 ms | 150 ms |
| background low power | 4 | 2 MiB | 25 ms | 250 ms |
| background CPU / memory pressure | 1 | 256 KiB | 10 ms | 500 ms |
| interactive normal | 16 | 16 MiB | 200 ms | 0 |
| interactive CPU / memory pressure | 4 | 4 MiB | 50 ms | 50 ms |
| Store pressure | 0 | 0 | 0 | 500 ms |

Store pressure 仍写入零消耗 `system_pressure` cycle，但不调用 target executor。其它 budget 只在文件、chunk 或 phase 边界协作停止；target runtime 保留检查点之间的真实墙钟观测，scheduler 内建 adapter 则把 cycle 的 active 记账限定在已授予 budget 内，异常 executor 越界仍作为 typed failure 中断，不能把超额消费提交进累计状态。当前 backfill slice 执行中出现 live task 时不异步取消 SQLite/解析事务，而在本 slice 边界记录 `live_preempted`，下一 cycle 优先选择 live。

同类后台任务已经运行时，交互操作提升现有任务到 interactive、放宽预算并沿用当前 offset，不启动第二份扫描。全量重建是独立的显式操作。

SQLite 使用 WAL、单写队列和批量事务。UI 始终查询投影；quota 快速结果先返回，Session 对账和聚合通过增量事件逐步更新。

## 首次启动

采用“轻 onboarding + 立即可用 + 后台补齐历史”：

```text
启动应用
    -> 探测 Codex home
    -> 展示一次隐私与数据边界
    -> 用户确认本地索引
    -> 立即打开概览
    -> 快速读取最近状态
    -> 后台补齐历史
    -> 默认启动在线 quota 与 reset credits 调度
```

首次启动主动打开主窗口，后续启动默认进入 Tray。

Codex home 探测优先级：非空 `CODEX_HOME`、`~/.codex`、用户手动选择。输入路径先 clean，同一输入只探测一次；最终目录拒绝 symlink，祖先路径先解析成绝对物理路径，候选按解析后的路径去重。canonicalize 前先记录最终 Home 的 device/inode；确认前从 `/` 开始逐组件 `openat(..., O_NOFOLLOW|O_DIRECTORY)` 打开 canonical Home，FD identity 必须与这份最早观察一致，因此祖先无论换成 symlink 还是真实目录都不能跨过 canonicalize→open 窗口。随后只通过固定 root FD 枚举，并用 metadata stat 检查 `sessions/`、`archived_sessions/`、`session_index.jsonl`、`auth.json` 是否存在，以及 JSONL 文件数量和总字节数；每个递归目录在扫描前后核对目录 identity 和稳定排序的 entry identity snapshot，目录替换或 entry 增删/替换返回 `changed_during_scan`。不得打开 JSONL 或 `auth.json` 内容，也不在 Codex home 内创建、修改、修复或移动文件。缺少 allowlisted 入口或存在一个空 Home 都是合法候选；symlink、非普通 `*.jsonl`、结构竞态和不支持的入口 fail closed。

每个 ready 候选生成只绑定 `source + canonical path + root device/inode` 的 confirmation ID，展示用的 JSONL 数量和字节数不进入 ID。点击“开始”时必须重新执行同一 metadata-only probe，并核对 physical identity 和安全结构；正常 live append 可以改变文件数/字节数而不让确认永久失效，root replacement、symlink 或结构异常则零写入并要求重新检测。failure 只暴露 `missing`、`permission`、`unsafe_symlink`、`unsupported_entry`、`changed`、`invalid_path`、`io` 等 allowlisted reason，不向 UI 透传 raw filesystem error。

v0.1 只支持一个 Codex home。更换路径时，用户必须明确选择“新建独立数据库”或“清空当前派生索引后重建”，不能静默混合多个 home。

首次隐私说明展示检测路径、Tracker 数据库位置、读取范围、不保存内容，以及“在线 quota 与 reset credits 默认开启、可随时关闭”。建议文案：

> Codex Pulse 将只读本机 Codex session 和索引文件，并把 token、项目、模型、配额等结构化数据保存到本机 SQLite。在线 quota 与 reset credits 默认开启，会临时读取当前 access token 请求 ChatGPT 内部只读接口；凭证不会写入数据库或日志。你可以在开始前或之后随时关闭这两项能力。

操作为“开始”“选择其他目录”“退出”，并在同页提供在线 quota 与 reset credits 两个默认开启的独立开关。本地索引确认和两个在线能力偏好是同一次用户决定中的独立字段，但首次确认必须作为一个版本化 preferences snapshot 原子发布：目录 `0700`、文件 `0600`；新建 private 目录后先 fsync 其 containing directory，再用同目录 private temp 完整写入并 fsync、以不覆盖方式发布，最后 fsync private 目录。相同 source identity 与开关的重复确认不重写；已有不同 Home 或开关的确认不能由 onboarding 静默覆盖，交给 Preferences/Home switch 协议处理。发布前失败保持未配置；若文件已经可见但目录 fsync/cleanup 失败，返回 `durability_unknown`，启动方必须用不继承原请求取消信号的有界 `Load` 读回，不能假定零写入后盲目重试。Confirm、Cancel 和 Resume 在进程内按同一状态锁线性化：提交点之后的 Cancel 不得把已经确认或 durability-unknown 的状态改写成 canceled；只有 post-commit readback 或后续 Resume 权威读回明确为未配置时，才清除 conservative persistence latch 并恢复 Detect/Cancel。

## Preferences v2 与单 Home 切换

首次确认直接写 current `schema_version=2` typed Preferences；已有 v1 flat onboarding snapshot 在私有跨实例锁内严格解码、原子迁移并读回校验。快照包含独立的 `revision`、onboarding、active `codex_home`、在线能力、刷新周期、更新和 UI 设置，以及可选的 `detached_homes`、`pending_resume`、`pending_switch` 和 `last_switch`。每层 JSON object 都按精确大小写 allowlist 解码；未知/未来 schema、duplicate key、大小写别名、缺失/null 字段、未知字段、非法枚举、越界周期、非绝对 clean path、宽权限文件或 symlink 均 fail closed，不用 `encoding/json` 的宽松匹配或“填默认值”掩盖损坏配置。migration 在 rename 前失败保留 v1 bytes；rename 已发生但 durability 响应不确定或原请求随后取消时，在独立有界 context 中读回并只接受完整 v2，不能笼统声称旧 bytes 仍权威。

`revision` 是整个 JSON 快照的 compare-and-swap 版本，每次成功替换必须精确 `+1`；`codex_home.generation` 是 active Home 的调度 fence，每次切换精确 `+1`。它们都不同于 SQLite 的逐文件 `source_generations`：后者只表达某个 source file 截断/替换后的解析代际，不能用于判断任务属于哪个 Home。跨进程 CAS 使用 private `0700` 目录中的 `0600` `.preferences.lock` advisory lock；同目录 temp 完整写入并 fsync 后原子 rename，再 fsync 目录。Confirm/Recover 另持有 `0600` `.preferences.switch.lock` execution lease，租约覆盖从权威 load 到全部 runtime side effect 和最终配置 resolution，防止 live owner 被另一进程恢复抢占；进程退出时 OS 自动释放 flock，恢复方才能接管。两个锁都只约束 Codex Pulse cooperating writers；private 目录阻断其他用户访问，但不会把同一用户绕过本协议直接替换文件的外部程序误称为可原子 CAS。

Settings 更新只接受 typed online/refresh/update/UI 字段和调用方读到的 `expected_revision`，完整保留 Home identity、Home generation、data-store key 和 switch journal。stale revision 返回 conflict；精确重复请求幂等返回当前快照；存在 pending resume/switch 时拒绝普通设置写入，先完成恢复。CAS 返回 durability unknown 或请求在 replace 后取消时，使用不继承原取消信号的有界 context 读回 old/new/conflicting/unreadable 四态，返回实际可见快照，不能固定返回旧值。

Home preview 使用 metadata-only probe，拒绝当前 physical identity，并为当前 revision、目标 identity 和明确 strategy 生成稳定 plan。确认和恢复必须先竞争同一个跨进程 execution lease；确认持租约重新读配置并重探测目标，顺序固定为：

```text
acquire switch execution lease
    -> CAS 发布 pending_resume guard
    -> drain old home_generation
    -> CAS 发布 target generation + pending_switch，并原子清除 guard
    -> 按 (switch_id, generation) 幂等启动 bootstrap
    -> CAS 清除 journal 并写入 last_switch audit
    -> release switch execution lease
```

`pending_resume` 在 drain 前先持久化，表示旧 generation 只要没有被 target journal 取代，就必须幂等 Resume；每次 Confirm 生成独立的 128-bit `attempt_id`，只有读回自己 attempt 且仍持 execution lease 的请求获得 Drain 权，相同 plan/目标的并发确认不能共享 guard。Recover 必须等 live owner 释放租约；只有 owner 进程退出、OS 释放租约后，恢复方才可把 guard 当成遗留恢复责任执行 Resume。`attempt_id` 随 target journal 与 rollback marker 保持不变，pending CAS 不确定态也按完整 attempt 精确读回。drain failure、pending CAS failure、进程退出和 rollback 后 Resume failure 都不能清掉恢复责任。Resume 成功后再 CAS 清除 marker 并写 rolled-back audit。`HomeRuntime.Drain` 只有在旧 generation 不再存在已接纳或执行中的 writer 后才能返回；`Resume` 必须幂等；后续 scheduler/admission/commit 都必须携带 Home generation。`StartBootstrap` 必须按精确 `(switch_id, generation)` 持久幂等。`independent_database` 为目标分配独立 `data_store_key`，保留旧 Home 的 detached metadata 与可审计事实；切回该 physical Home 时复用原 key，但始终只有一个 active Home。`clear_and_rebuild` 复用当前 key，只授权后续 bootstrap 清理派生索引，不删除 Codex 原始文件；若目标恰好也存在 detached metadata，旧独立数据库仍保持 detached，不会被静默激活或删除。

target `pending_switch` 表示新 generation 已成为 active 但 bootstrap 结果尚未完成配置 finalization。启动或 bootstrap start 结果不明确时，先读取 runtime 的 durable status：`not_started` / `failed_safe_to_rollback` 回滚 previous、写入 `pending_resume` 并在 Resume 成功后清除；`queued` / `running` / `succeeded` / `failed_requires_resume` 保持 target 为 active 并完成配置 finalization，其中 failed 状态由后续 scheduler 显式续跑，不在恢复路径偷偷启动第二份任务；status unknown/error 保留 journal 并返回 recovery error。guard/pending/final CAS 的可见性不明确时都必须使用独立有界 context 读回权威状态，不能猜测成功或盲目清除恢复标记。

## 初始索引阶段

TOO-259 将首次索引冻结为可恢复的四阶段同步状态机；后续调度器只负责调用和排队，不重新解释已经持久化的 discovery plan：

1. `discover`：以 Store 中 active source fingerprint 为 previous truth，执行 `DiscoverAgainst + PlanReconcile`，把完整 action snapshot 冻结成 typed plan item。Discoverer、snapshot reader 和可选 session-index reader 都必须核对 bootstrap facts 中持久化的 confirmed Home `device/inode`，不能重新信任当前占用同一路径的目录；root identity 漂移直接失败，不把替换目录中的文件纳入计划。计划冻结前不显示百分比。
2. `fast_bootstrap`：已有 append lineage、今天和稳定近期顺序的 Session 在固定文件数/字节预算内优先。相同 fingerprint 只有在 authoritative committed offset 已到 snapshot size 时才算完成；offset 未到 EOF 的 unchanged active source 仍必须以 `active_append` 进入 fast plan 并从 Store checkpoint 续读。根级 `session_index.jsonl` 只提供可选 recency hint，不进入 rollout parser，不产生 Session/Turn facts；普通 hint 读取、解析或未来时间异常时退回文件 mtime，但 confirmed Home identity 不匹配不能降级忽略。
3. `history_backfill`：其余固定 plan item 按最近 7 天、30 天、更早历史分层，逐 source 串行处理。每个 source 继续复用 `source_generations + parser_checkpoints`；source checkpoint 是权威恢复位置，bootstrap item/job progress 允许落后但不能领先。initial pass 中已知 source 的 unreadable action 只表示 discovery 瞬时不可读，执行时标记为 drifted 并强制交给 final discovery 重新证明；已冻结 source 在真正 open 前被归档、重命名或删除，也按 `changed_during_scan` 进入同一 drift/final-reconcile 语义。危险 symlink、Home identity 漂移和 reconcile pass 仍不可读继续阻止 full-ready，避免把安全失败降级或让 Resume 永久重放 pass 0。
4. `reconcile`：初始 fast/backfill 完成后重新发现目录，把本次快照冻结成独立 reconcile pass。`reconcile_pass + reconcile_plan_at_ms` 即使在空 pass 也必须原子落盘，避免崩溃后更换完成边界；added/grown/moved/replaced 继续走 ingester，deleted 只在 expected active fingerprint CAS 成功后把 source 标记 unavailable，unreadable、读中 drift、stale CAS 或 discovery issue 都阻止 full-ready。失败/中断后的新 attempt 保留旧 pass items 作为审计，但必须重新 discovery、递增 pass 并只以最新 pass 的全部 succeeded + issue=0 作为闭合条件。

`first_screen_ready_at_ms` 与 `full_history_ready_at_ms` 是不同事实：第一份可用 fast source 完成，或确认 fast 结果为空时写 first-ready；只有 final reconcile pass 全部应用且没有 blocking issue 时，才同时写 full-ready、reconciled 与 succeeded。不能用 backfill 结束冒充全历史完成，也不能给全历史承诺固定秒数；样本不足时 ETA 保持 `unknown`，成功后才进入 `complete`。

TOO-299 补齐生产装配：`StartBootstrap` 冻结 durable plan 后，application startup 与 Home switch post-commit 都会按稳定 `task-<job_id>` / `bootstrap:<job_id>` 幂等入队，不能依赖测试或外部调用方手工 enqueue。首次初始化使用 `backfill` lane + `interactive` service class：normal snapshot 下以 16 MiB / 200 ms、`YieldFor=0` 连续推进；lane 仍保留 live 优先、用户 pause、sleep、Home generation fence 和 injectable CPU/memory/store pressure。application 当前使用静态 normal probe，因此“全速”表示不人为插入 idle delay；pressure policy 与逐 cycle 重读由 scheduler contract 保留，但不声称当前已从 OS 动态生成 pressure bit。无论 probe 类型如何，都不绕过 cooperative slice、SQLite 单 writer 或系统保护。

schema v7 在 v6 bootstrap facts 之上追加 `scheduler_tasks`、`scheduler_cycles` 与 `live_scan_jobs`。`scheduler_tasks` 保存 immutable admission target/service class、当前 target/service class、lane、queue order 和累计消费，不复制 source cursor；同一 admission 在 promotion、yield、terminal 或 recovery 换绑后重放仍返回当前 durable task，改变原始 payload 才冲突。`scheduler_cycles` 追加 SQLite 全局自增 `commit_order`、每次选择、预算、实际消费、queue depth、oldest wait 与停止原因；`live_scan_jobs` 以 typed columns 保存 confirmed Home、action kind 和 previous/current fingerprint，不保存 opaque JSON 或文件正文。所有 CRUD/filter/transaction 编排使用 GORM；canonical DDL、PRAGMA、schema readback 和 query-plan gate 仍是 raw SQL 例外。生产 SQLite 依赖固定为 GORM + `github.com/libtnb/sqlite` / `modernc.org/sqlite` Pure Go driver，不引入 `gorm.io/driver/sqlite` 或 `mattn/go-sqlite3`。

schema v8 在该事实面上追加单行 `scheduler_lifecycle` 和逐 task `scheduler_retry_states`。lifecycle 把 active Home `generation`、用户暂停范围 `none/backfill/all`、系统状态 `awake/sleeping`、转换状态 `steady/draining/reconciling/blocked` 与来源状态 `unknown/available/unavailable` 分开保存；`revision + last_event_id` 提供 CAS 和精确重放。retry state 只保存失败次数、有限 error class、可选 `next_retry_at_ms` 和 typed recovery action，不保存原始错误、路径或文件正文。迁移仍是 append-only version/checksum ledger；普通读写和事务编排使用 GORM，`STRICT`/`CHECK`/特殊 index 的 canonical DDL 继续集中在 migration adapter。

### Cron 触发边界

所有周期性和日历型任务固定使用 `github.com/robfig/cron/v3 v3.0.1`；生产代码不再用自写 `for + time.NewTimer/time.Ticker/time.Sleep` 或复制 cron parser 驱动周期任务。当前 scheduler worker 使用 SDK 的 `@every 1s` 作为唯一周期 trigger：v3.0.1 对 interval 的最低精度是 1 秒，因此不额外造 sub-second timer 恢复旧 250 ms idle delay。一次 cron 唤醒可以执行一个 durable work burst：每个 cycle 后重新读取 lifecycle、Home generation、live/backfill queue 和 system pressure，只有已提交 `YieldFor=0` 才立即重选下一 cycle；出现 background/pressure yield、空队列、失败或取消就结束本次 callback，等待 SDK 的下一次 tick。这是 cron job 内的 bounded queue drain，不是第二套计时器。到期 retry 只可能在持久 `next_retry_at_ms` 之后最多一个 tick 被重新检查，绝不因 cron 内存提前执行。

`Service.Run` 仍从 startup recovery 到最后一个 cycle 持有同一 OS scheduler owner lease。它在启动 cron 前立即恢复遗留 task 并推进一次 cycle；后续 job 每次都重新读取 lifecycle permit、Home generation、due retry、runnable queue、ScanBudget 和 checkpoint，再进入既有串行 cycle/Store transaction。cron Entry 的 `Prev/Next`、进程内 job 状态和 trigger 是否被 skip 都不是业务真相，进程重启后直接丢弃并从 SQLite 恢复。

最近 24 小时 retention 是独立的轻量 maintenance service，也固定使用 `github.com/robfig/cron/v3 v3.0.1`。它在启动时立即按时间谓词补跑，之后用 `@every 1m` 检查内存中的 typed `next due`；成功周期 1 小时，cleanup/checkpoint 失败按 5/10/20/40/60 分钟退避并封顶。每个删除 batch 和固定 PASSIVE WAL checkpoint 都进入 Store 的低优先级 maintenance lane，普通写在 operation 边界优先；不创建第二 writer、不使用自造 timer loop，也不执行 vacuum 或阻塞式 checkpoint。进程重启不恢复 cursor，而是重新计算 `< now-24h` eligibility。

在线 quota/reset credits 使用独立的轻量 `QuotaRefreshCoordinator`，但遵守同一个 cron 边界。coordinator 从 typed Preferences 读取两个独立开关与周期，为 `quota:wham:default`、`reset_credits:wham:default` 分别维护 `source_refresh_schedules` 和 append-only `source_refresh_claims` generation fence；启动先恢复到期 claim、重新校验 wall clock/preferences/current state 并立即跑一次 due cycle，随后复用 `@every 1s` cron factory。每个普通 cycle 也先检查本 tick 前新到期的遗留 claim，因此“启动时尚未到期、启动后才到期”的 crash lease 不会永久阻塞。

一次 refresh 固定为 `list due -> revision CAS claim + active fence -> typed service adapter -> attempt/facts transaction -> policy re-evaluate -> claim CAS complete + completed fence`。cron 内存 entry、HTTP goroutine 或 UI loading 都不是 due truth。manual/foreground/wake 先由 policy 生成可审计 reason，再写入同一 durable schedule 并领取；Store 仍二次校验 due、revision、claim overlap 与 manual 60 秒间隔。调用方取消后，quota service 已用 detached bounded context 记录 cancelled attempt，coordinator 同样在 detached bounded completion context 中释放 claim；若 recorder 返回 durability unknown，则不猜测成功，保留 claim 到 lease recovery。恢复按 claim/request ID 检查 attempt：已记录就按 current state 完成原 claim，未记录才把 fence 标为 `abandoned` 并释放；release 之后到达的旧 attempt 会被同一 SQLite writer 序列拒绝。正常 CAS loser 返回可恢复 conflict 并在下一 tick 重读，事实 contract、权限/只读、磁盘满或损坏才停止 runner。Settings 成功提交后调用 `ReconcilePreferences`，关闭来源立即清空 next due 但不删 history，重新开启 never-loaded 来源进入 startup due。

同一 cron entry 使用 `SkipIfStillRunning` 阻止重叠 trigger并以 `Recover` 保护 wrapper；panic 后必须释放 overlap token，使后续 tick 仍能进入。通过 `cycleMu`、跨进程 owner lease、Store running-task/claim/CAS 继续提供分层防线。Recover/Retry target mutation 属于 cycle preflight writer，在读取 durable lifecycle 到完成 target/rebind 之间登记独立 activity；Drain 对两个 pause scope 都等待 preflight，普通 slice阶段仍保持 backfill pause不等待纯 live slice。queue empty、未到期 durable retry 和已经原子持久化的 failed cycle 等待下一 tick；system probe、cron registration 或其它非持久化 fatal error必须让 `Run` 可观察地退出。首个 fatal 先取消共享 job context再发布错误，确保已经排队但尚未进入业务读取的 trigger 被 fence；context 取消或 fatal 随后都调用 `cron.Stop()` 阻止新 trigger，并等待其返回的 context，确认在途 job 到协作边界后才释放 owner lease。Home generation 切换先让 lifecycle 持久化 draining/blocked intent并调用 scheduler `Drain(all)`，再 drain quota/bootstrap；target Preferences resolution 后 `HomeChanged` 原子推进 lifecycle generation，在保留 user pause/system sleep 的前提下 reconcile 新 Home，最后 Resume quota。应用关闭顺序为 event adapter 停止接纳、application control admission seal/drain、quota runner cancel/drain、bootstrap scheduler worker、local coordinator、SQLite。

`StartBootstrap` 对精确 `(switch_id, home_generation)` 使用稳定 initial job identity，负责创建 discover job 并冻结 initial plan；Resume lineage 中最新 attempt ID 可以变化，但 immutable request facts 相同的 `StartBootstrap` 仍是 exact no-op，改变 Home identity/data-store/strategy 才冲突。ready/terminal attempt 的 exact Start 可在 admission 冲突前只读核对 durable facts，因此执行中 Run 不会把幂等 readback 误报为冲突；pending attempt 的并发同 identity Start 与并发 Resume loser等待同 operation owner 结束后再读权威状态，不执行第二份副作用。`Run(job_id)` 与 `RunSlice(job_id, budget)` 复用同一四阶段状态机；前者保持完整同步语义，后者按权威 checkpoint 做协作式 slice，budget yield 不伪装成 terminal。直接取消或 `Drain` 将进行中 attempt 标记 interrupted，plan-ready 的 failed/cancelled/interrupted attempt 都由 `Resume` 克隆为新 queued attempt；固定 initial plan/facts 被复制，逐 source generation/checkpoint 不复制。Start、Run、RunSlice、Resume 在任何 Store/文件系统 side effect 前统一登记 generation operation；不同 operation 仍按 generation 互斥，`Drain` 先关闭 admission，取消并等待这些在途 operation，再终止持久 queued/running attempt并返回无 writer 状态，执行中的 Drain 未完成时 Resume 不得重开 admission。

Discover 为每个文件记录初始 size，初始 plan 分母不会因扫描期间 append 改写。读取使用绑定 confirmed Home physical identity 的 no-follow snapshot reader，按 chunk 读取并在前后复核 identity、size、mtime 和 prefix digest；发现 drift 时保留已经提交的 source offset，把 item 标记 drifted，交给 final reconcile 生成新 action，不清库、不跳过未证明的字节。limited read 的 byte budget 统计真实文件 IO：每个 slice 的 prefix proof 与正文读取都计入 `BytesRead`，内容游标推进量另记为 `ContentBytes`；预算小于 4096 字节 proof 下限时在打开正文前拒绝，固定 ScanBudget 均高于该下限。time budget 只能通过 reader 的受控 stop contract 在当前 chunk 后协作式停止，reader 仍先完成 after-stat；若停止窗口发生 drift，`changed_during_scan` 优先于普通 time yield。

调度器在这些持久原语之上维护两个 bounded lane：

- `live queue`：typed reconcile action 对应的增量 attempt；两个 lane 都有任务时优先 live；
- `backfill queue`：未完成 bootstrap attempt，按 background ScanBudget 限速；
- lane 内按持久 `queue_order_ms` 轮转；yield 后移到本 lane 队尾；
- recent history 按 SQLite 在 cycle 提交事务中分配的全局自增 `commit_order` 读回，不按可能重复的 `finished_at_ms` 或随机 UUID 猜顺序；最近 8 个持久 cycle 都选择 live 且 backfill 仍有任务时，第 9 个 cycle 强制选择 backfill，进程重启不会重置或重排公平计数；
- queue 选择由 Store 在同一只读事务快照中只对 active Home generation、`awake + steady + available` 且未被用户暂停的 queued task 分 lane 聚合 depth、最老候选和 tail，不从最多 500 条的通用列表推断。claim 在同一写事务内再次检查 lifecycle permit，避免 pause/generation 变更与 claim 竞争；审计入口仍可读取全量队列，但旧 generation 或暂停 lane 不参与 live preemption、oldest wait 和执行选择；
- 每个数据库旁固定保留一个 `0600`、拒绝 symlink 的 scheduler owner lock。长生命周期 `Run` 从恢复前到最后一个 cycle 持有 OS advisory lease，直接 `RunCycle` 只做非阻塞竞争；只有 owner 正常释放或进程退出后 OS 自动释放，另一 Service 才能恢复/执行。进程内 cycle mutex 与 Store“已有任意 running task 时拒绝第二个 claim”继续作为同 Service 串行和持久状态防线；promotion 与 commit 竞争只重试未执行的 claim或同一 cycle commit，不重复调用 target；
- scheduler owner 取得 lease 后分页接管全部遗留 running/interrupted task。target 仍可续跑时创建新 attempt并通过 recovery CAS 原地换绑，恢复时间戳同时晚于旧 queue order、`updated_at_ms` 与全局 tail；target 已 succeeded/failed/cancelled 时原子补齐 terminal scheduler cycle，覆盖 target 先提交、scheduler commit 前进程退出的 crash gap；活跃 owner 持 lease 时其它 Service 不得恢复，queued task也不重复恢复。

live runtime 只接受 added/unchanged/grown/truncated/moved/replaced typed action。stable request 的 exact replay先读 durable facts，因此 Home 后续迁移也不会把已完成请求误报为新任务；deleted/unreadable 仍由目录 reconcile/bootstrap 处理。live 与 bootstrap 都通过 confirmed snapshot reader、同一个 indexer 和 Store 单写队列提交 source checkpoint，不存在第二份 SQLite writer。

用户暂停、sleep/wake、来源变化和启动恢复由单一 lifecycle coordinator 串行化。任何暂停/休眠先持久化 intent，再由 scheduler generation fence 阻止新 claim；`backfill` 暂停只等待正在执行的 backfill slice，`all` 与 sleep 等待当前重型 slice 到协作边界，最后写入 steady。Swift client 把 `system_will_sleep`、`system_did_wake` 与 `application_did_become_active` 有限枚举通过 `NotifyLifecycle` RPC 交给 Helper，不允许任意事件名或平台 payload 进入 Core；Helper adapter 再以有界 context 调用 coordinator，foreground 事件仅表示重新探测 confirmed Home，不预先宣称来源可用。

wake、显式 resume 与来源恢复不会直接开放 worker，而是先进入 `reconciling`，在前后两次核对 confirmed Home 的 generation、canonical path、device 与 inode，并执行轻量 reconcile。当前正常 App 的轻量索引模式把 foreground/wake reconcile 转成同一常驻 worker 的 coalesced trigger：先重新读取 App Server metadata，只有真实字段变化才推进 metadata generation，再以文件 identity 和 durable offset 检查 rollout；周期与 lifecycle 事件不会并发扫描。旧 bootstrap 模式的 added/grown/truncated/moved/replaced action 仍使用稳定 identity 进入既有 live runtime 与 durable scheduler queue，deleted/unreadable 交给 bootstrap/reconcile contract。任一来源不可用或 generation 漂移都持久化 `blocked + unavailable`。用户 pause 与 system sleep 正交，因此 wake 不清除用户暂停；重复 pause/wake/source event 精确重放时不增加 revision。Helper 启动在打开 SQLite 后真实装配 bootstrap/live executor、scheduler worker、local lifecycle coordinator、quota/reset-credits clients/services、durable quota coordinator、robfig quota runner 与 lifecycle RPC adapter；quota runner 首先保持 suspended，Preferences crash journal rollback/finalize 完成后才开放最终 Home generation，unknown recovery 不发 HTTP。local startup recover/reconcile 和在线 startup due 都从同一个恢复后 Store/Preferences truth 开始。退出先注销 adapter 并关闭 quota settings/manual/Home-switch admission，再 cancel/drain quota cron 与在途 request，然后停止轻量索引或 bootstrap worker、关闭 local coordinator，最后由最外层关闭 SQLite。启动看到遗留 `pending_resume/pending_switch/draining/reconciling` 会先完成对应步骤；`pause=all` 时不恢复 active task，避免启动绕过用户意图。

调度失败与 scheduler cycle 在同一 SQLite 事务中写入 retry state。`busy/timeout/io/unavailable/unknown` 采用最多 5 次、1 秒起步、2 倍增长、最终 30 秒封顶并带 `[0,50%)` 可注入 jitter 的 due time；重启只按持久 `next_retry_at_ms` 恢复。target 已执行后的 cycle commit、unknown-response readback 与 CAS conflict retry 共用同一个脱离父取消但保留 deadline 的有界 context；promotion 等竞争把 commit 时间后移时，waiting due 按原 delay 从新 commit 基准重算，余量不足则转 typed blocked，Store 同时拒绝 `next_retry_at_ms <= updated_at_ms`。到期重试复用同一 scheduler task，并通过 target 的 stable retry identity 换绑，不创建第二份逻辑任务。永久错误立即 blocked：磁盘满、权限/只读、来源不可用、Home 选择和 Store 损坏分别映射到 `free_space`、`grant_permission`、`check_source`、`choose_home`、`repair_store`；退避耗尽也转为 blocked，不无限自旋。低电量模式仍只降低 backfill 速度，v0.1 不自动把它解释为用户暂停。

进度同时展示文件和字节，例如“历史索引 43% · 38/126 个 Session · 148/342 MB · 当前最近 30 天”。并区分“今日完整 / 最近 30 天处理中 / 全部历史 43%”。退出后通过 offset 和 job checkpoint 续传。

本地概览可用后，用非阻塞卡片确认在线能力状态：

> 在线 quota 与 reset credits 已开启。Codex Pulse 只会从 `~/.codex/auth.json` 临时读取当前 access token，并请求 ChatGPT 内部只读接口。凭证不会写入数据库或日志；关闭后立即停止对应在线调度。

卡片提供“查看来源”和“管理设置”，不要求二次授权。关闭开关后停止对应在线调度，但不删除已有非敏感 observation history；升级时保留用户已有关闭偏好。

## Codex 文件发现与来源对账

TOO-251 把 discovery 固定为无副作用的只读边界。调用方必须先确认单一 Codex Home；发现器只允许访问以下入口：

- `sessions/**/*.jsonl`
- `archived_sessions/**/*.jsonl`
- 根级 `session_index.jsonl`

其它根级 JSONL、非 `.jsonl` 文件、非普通文件和任意用户目录都不进入 snapshot。Codex Home 必须是绝对、既有、最终路径组件非 symlink 的目录。构造发现器时先记录 confirmed home 的 device/inode；每轮 discovery 重新打开且只持有一个 root FD，核对物理身份后，所有目录枚举和文件 probe 都从该 FD 逐段 `openat(..., O_NOFOLLOW)`。home 被替换或父 symlink 改指其它目录时整轮返回 `home changed`，不读取未经确认的目录。最终文件读取前后核对 device、inode、mode、size、mtime 和 ctime；任一中间路径变为 symlink、文件在扫描中变化或不再是普通文件时，当前 probe 失败并等待下一轮，不产生可被消费的 snapshot。

每个成功 snapshot 只包含：

| 字段 | 语义 |
| --- | --- |
| `source_file_id` | `provider=codex + device_id + inode` 的 canonical SHA-256；不含路径或内容，移动到 archived 目录后保持不变。 |
| `kind` / `path` | `session`、`archived_session` 或 `session_index` 及 confirmed home 内的当前绝对路径。 |
| `size_bytes` / `mtime_ns` | 当前文件大小和纳秒修改时间。 |
| `prefix_bytes` / `prefix_sha256` | 最多 4096 字节的实际采样长度和 SHA-256；采样原文立即丢弃。 |
| `fingerprint_digest` | 对上述 identity、size、mtime 和 prefix digest 做长度分隔编码后的 SHA-256。 |
| `comparison` | `DiscoverAgainst(previous)` 针对上一快照 prefix 长度生成的临时 SHA-256 证明；只供本轮 reconcile，既不进入 fingerprint digest，也不替代 TOO-253 的持久化 checkpoint。 |

同一轮发现按 path 稳定排序；同一 physical identity 同时出现在多个路径（例如 hardlink）时不任意选择，所有冲突路径都标为 `duplicate_identity`，并携带不含内容的 `source_file_id`，使 identity 已移动时既有来源仍被保护为 unreadable 而不是误报 deleted。partial failure 只输出 allowlisted issue code：`permission`、`io`、`unsafe_symlink`、`unsupported_file`、`changed_during_scan`、`duplicate_identity`，不携带 raw error 或文件内容。顶层 `sessions` / `archived_sessions` 不存在是合法空状态；父目录已经枚举到的递归子目录随后消失则属于竞态，必须产生 `changed_during_scan` subtree issue。所有目录级失败都用 subtree scope，避免把未观察到的既有来源误判为删除。

纯 reconcile planner 的入口固定为 `PlanReconcile(confirmedHome, previous, discovery)`。它先校验 previous/current snapshot 和 issue 全部落在 confirmed home 的三个 allowlisted 范围内，且 `kind` 与 path 一致；包外路径、其它根级 JSONL 和 kind/path mismatch 直接失败。匹配采用真正的全局两阶段：先消费所有 physical identity，再只用尚未消费的 previous 按相同 path 识别原子替换，保证一个 previous 最多进入一个 action。

| 结果 | 决策 |
| --- | --- |
| `added` | 当前 snapshot 没有可用的既有 identity，且相同 path 的 previous 不存在或已被 identity pass 消费。 |
| `unchanged` | identity、path、size 和可比较 prefix 均未显示变化；mtime 变更仍通过 current snapshot 传给后续执行器。 |
| `grown` | 同 identity、size 增大，且 current 在 previous prefix 长度上的比较证明与 previous prefix SHA-256 一致。 |
| `truncated` | 同 identity，size 变小；后续执行器必须开启新 generation。 |
| `moved` | 同 identity、内容/size 未变，但 path 或 source kind 改变。 |
| `replaced` | 尚未被 identity pass 消费的相同 path 出现不同 identity；或同 identity 下 mtime 回退、相同 size 的 prefix 改变、增长时旧 prefix 证明冲突或缺失。 |
| `deleted` | 既有 snapshot 在完整且无覆盖 issue 的本轮发现中消失。 |
| `unreadable` | exact/subtree issue 覆盖该路径；保持既有事实和游标，等待重试。 |

短文件 append 会使当前常规 prefix 变长，不能仅靠两个不同长度的摘要推断旧前缀未变；调用方必须使用 `DiscoverAgainst(previous)` 取得旧窗口证明。证明缺失时 planner 保守返回 `replaced`，防止后续执行器沿旧 offset 跳过已被改写的字节。`PathChanged` 与主结果正交保留，所以 moved + grown 不会丢失移动事实。planner 输入中的 duplicate identity/path 或非法 fingerprint 直接失败，不采用 last-wins。

TOO-251 交付纯 snapshot/reconcile，TOO-253 交付逐 source generation/checkpoint 和原子事实提交；TOO-259 在两者之上增加固定 bootstrap plan、阶段事实、no-follow source reader、final reconcile 与 attempt recovery。逐 source checkpoint 仍是字节恢复权威，bootstrap 状态机不复制或重定义它。

## 空状态与恢复

- 未找到 Codex home：选择目录、重新检测、打开设置。
- 目录存在但无 Session：显示已连接并保持 watcher，不视为错误。
- JSONL 部分损坏：跳过坏行或坏文件，记录 path、offset 和错误类型，不保存正文；支持单文件重试。
- 权限不足：展示不可读路径和系统授权说明，不自动修改权限。
- 磁盘不足：事务 rollback、不推进 offset、暂停后台写入。
- SQLite 无法打开：重试、打开数据目录、查看安全日志、备份后显式重建。
- migration 失败：只读安全模式，停止索引，保留查看数据、检查更新和恢复备份。

独立 Preferences v2 保存 confirmed Home physical identity、active Home generation/data-store key、两个在线能力、刷新/更新/UI 设置和切换恢复信息；新安装的两个在线能力默认值为 `true`，迁移保留已有显式值。首次没有 Preferences 时，Helper 对 `${CODEX_HOME:-$HOME/.codex}` 做 metadata-only probe 和 path/device/inode 二次校验后自动原子确认；候选缺失、不安全或变化时保持未配置。`initial_index_started_at` 只有在后续 bootstrap 真正启动时才由后续任务状态记录，onboarding 和 Preferences 不提前伪造。之后每次启动先 `LoadPreferences`；存在 pending journal 时先按 durable runtime status 恢复，否则用 metadata-only probe 复核保存的 canonical path/device/inode。source replacement 不授予 indexing。事实、进度和逐文件游标仍以 SQLite 为准。

首次启动验收：无网络/无 auth 可完成本地初始化；主窗口不等待全量历史；部分数据明确标注；退出可续传；live append 不被 backfill 饿死；坏文件不阻塞全局；重建数据库和更换 home 必须显式确认。
