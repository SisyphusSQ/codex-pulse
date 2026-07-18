# Observability and Data Health

## 目标

这不是长期监控系统，而是帮助用户判断最近是否正常：后台索引是否推进、Tracker 是否占用过多资源、live queue 是否被 backfill 堵住、某来源为什么失败以及何时重试。

所有资源和故障明细只覆盖最近 24 小时。

## 指标

| 领域 | 指标 |
| --- | --- |
| 文件读取 | files scanned、bytes read、lines parsed、坏行数 |
| SQLite | rows written、事务耗时、busy 次数、DB/WAL 大小 |
| CPU / 内存 | process user/system CPU time、CPU percent、RSS、peak RSS |
| 队列 | live/backfill depth、最老任务等待、处理速度 |
| 调度 | active work、yield、throttled、queue wait |
| 来源 | consecutive failures、last success/error、next retry |
| 磁盘 | free bytes、写入失败、`SQLITE_FULL` |
| 索引 | phase、total/processed files、total/processed bytes |

schema v7 的 `scheduler_cycles` 是 queue/slice 观测的提交版事实。每行记录 `selection_reason`、`stop_reason`、`outcome`、budget/consumed files/bytes/active ms、选择时的 live/backfill depth、两个 lane 的 oldest wait，以及 started/finished time；`scheduler_tasks` 提供当前 state、service class、queue order、累计 files/bytes/slice count 和有限 error class。bytes consumed 使用 snapshot reader 的真实文件 IO，包含每个 slice 的 prefix identity proof；内容游标推进仍由 source checkpoint 单独表达，不能拿两者相减推断丢失或重复。Data Health 后续直接聚合这些 typed facts，不从日志正文、channel 长度或进程内 counter 猜测调度状态。

schema v8 的 `scheduler_lifecycle` 与 `scheduler_retry_states` 补齐控制和恢复事实。Data Health 读取正交的用户 pause、system sleep、transition、source availability、Home generation 和 lifecycle revision；不能从“队列暂时不动”猜测暂停，也不能把 wake 当成 resume。每个失败 task 的 `waiting/blocked/resolved`、failure count、next retry 与 typed recovery action 都可直接读回；由于 retry mutation 与 failed cycle 原子提交，不会出现 UI 显示失败但没有恢复语义的 crash gap。旧 Home generation 与 paused lane 不进入 runnable queue depth/oldest wait，避免把被 fence 保护的工作误报为 live 堵塞。

TOO-269 冻结只读查询边界：`runtimeinfo.Service` 通过 GORM-first Store snapshot 读取 Source、Job、Health 与 scheduler lifecycle，不运行 evaluator、不执行恢复动作。每个包含多次 SELECT 的 Source/Job/Health page 与 Job detail 都在一个显式 read transaction 中完成，summary、items、retry、task、lifecycle 与 event 不会跨提交拼成混合快照。Source 把本机文件和在线来源映射为带类型前缀的 opaque key；任一来源种类失败时保留另一种并返回 `partial + unavailableKinds`，所选种类全部失败才 unavailable。Job 可关联 typed retry state，但不暴露 scheduler task ID；Health 不返回 fingerprint。summary、list 与 detail 只包含有限 enum、JS-safe numeric value、content-free error/failure code 和固定 command reference；路径、device/inode、scope、request/payload identity、raw error/token 均在 Go 服务端裁剪。

当前健康级别使用 active event 分桶，而不是全部历史 severity：resolved critical 仍计入 24 小时历史和 occurrence，但不再使 current health 保持 `blocked`。聚合只执行 active/resolved 两组固定查询，并按四种 severity 分桶，最多读取 8 个 aggregate row，不按每个 resolved timestamp 扩张。优先级为 active critical 或 lifecycle blocked -> `blocked`，durable pause/sleep -> `paused`，active error 或 source unavailable -> `degraded`，active warning 或 draining/reconciling -> `busy`，其余 -> `healthy`。TOO-280 evaluator 负责产生/合并新的受管 `health_events`；M6 query 仍不从日志、零吞吐或字符串错误猜测状态。

固定 reason 包括 live priority、live-only、backfill-only、8-cycle fairness，以及 completed、file/byte/time budget、system pressure、live preempted、cancelled、dependency error、worker panic。原始 executor error、panic 正文、JSONL 内容和绝对 Home 路径不进入 cycle；路径只存在于既有 source/live typed identity 表，不复制到调度观测表。

Go 难以准确获得单 goroutine CPU 时间，因此 job CPU 使用进程级 user/system CPU delta 近似。后台只有一个重型 worker 时可以近似归因，但 UI 和诊断必须标注 process-level estimate，不能称为精确任务 CPU。

TOO-279 交付 schema v13 的 `app_runtime_samples` 与统一 `MetricsSnapshot`。runtime sample 只包含进程累计 user/system CPU 毫秒、相邻成功样本计算的 CPU percent、RSS/进程内 peak RSS、goroutine 数、DB/WAL/磁盘 free bytes、runnable live/backfill depth 与 oldest wait、Wails query count/total/max latency、collector duration 和累计 dropped samples。CPU percent 使用“单核 100%”口径，因此多核并行可以超过 100%；第一次成功采样或失败后尚无已提交基线时为 0。进程探针固定使用 `github.com/shirou/gopsutil/v4 v4.26.6`，Store 文件/队列探针分别读取 SQLite 文件元数据、磁盘空间和 `SchedulerRunnableQueueSnapshot`；尚未建立 scheduler lifecycle 时队列按空态处理，旧 generation 与 paused lane 不混入 runnable 指标。

所有 Wails read binding（包括 Bootstrap/Contracts 和 Usage/Session/Project/Quota/Source/Job/Health/Settings 查询）在既有 response/error/panic 规范化完成后只累加 content-free latency；command 不计入。collector 只有在进程、文件/队列和查询聚合全部成功后才通过 `WriteMaintenance` 写一个 sample；显式错误或 dependency panic 都会恢复已 drain 的 query aggregate，panic payload 不进入 error/Store。sink 失败不推进已提交 CPU/RSS baseline；wall clock、CPU counter 回退或 delta overflow 会丢弃当前点并清除 CPU baseline，下一次有效采样以 CPU 0 重建，避免持续失采。probe、sink、observer 故障或 overlap skip 不改变业务查询、索引、quota 或应用关闭，均只增加 saturating `dropped_samples`；该值是进程生命周期内的累计缺口，不是持久 error log。

`MetricsSnapshot` 在一个显式 GORM read transaction 中读取 runtime series，并聚合既有 `scheduler_cycles/scheduler_tasks/scheduler_retry_states`、`job_runs`、`source_state/source_attempts` 权威事实；除 cycle outcome/吞吐外，还返回当前 active task state、lane、service class、waiting/blocked retry、窗口内 stop reason、最后一次全局实际消费和最后一次 backfill lane 实际消费 files/bytes 的 cycle 完成时间，以及 source current/attempt 的 allowlisted error class/failure code 分桶。backfill stall 只读取 lane-specific progress，持续 live 消费不能掩盖 backfill 停滞。窗口固定为 24 小时半开区间 `[from_ms, until_ms)`；Detailed 5 秒 cadence 的 17,280 个点完整返回，多于该容量会显式 fail closed，不静默截断。interrupted job 只有 `resume_consumed_by_job_id IS NULL` 才计为当前可恢复工作；该 v13 持久事实由全部 typed resume API 原子写入并回填既有 lineage，不会因 terminal child 被 retention 删除而复活。不从日志、channel 长度或开放字符串推断状态。`process_snapshots` 仍未创建：当前没有可信的 session→PID 映射，不能把 Codex 外部进程资源伪装成已归因事实。TOO-283 已在该 snapshot 上交付只读 Data Health UI。

TOO-280 新增 `internal/health` 确定性 evaluator。`HealthEvaluationSnapshot` 在同一个 GORM read transaction 中读取完整 MetricsSnapshot、scheduler lifecycle 与按 `domain/severity/code` 聚合的外部活跃 health event；只排除 evaluator 提供的完整 `event_id + fingerprint + domain + code` descriptor，自管前缀碰撞仍作为外部事件可见，精确 ID 的所有权冲突则 fail closed。七个稳定 component 为 local index、live queue、history backfill、online quota、storage、runtime、updater；每项只输出有限 level/evidence/reason/impact/protection/recovery action。全局 priority 固定为 `blocked > paused > degraded > busy > healthy`，component 顺序和输入排序不改变唯一 primary。durable lifecycle 不按墙钟自行过期，但缺失、未来时间或 source unknown 明确为 unknown/degraded；runtime sample 缺失/过期也只输出 unknown/metrics-stale，不用旧 sample 续期具体 queue/disk 事件。尚无 M10 adapter 的 updater 明确为 `not_configured`，且不能覆盖已有更高优先级证据。

受管规则使用稳定 ID 与 `component + rule + code` 的 SHA-256 fingerprint。live wait 超过 30 秒、backfill 超过 5 分钟且没有可信 backfill progress、来源连续失败 3/10 次、auth required、磁盘不足 1 GiB、后台 CPU/RSS 连续 2 分钟和 metrics stale 都只消费 typed snapshot；CPU/RSS 必须由不超过 65 秒缺口的样本连续覆盖窗口，单点尖峰不触发。auth required 的 protection 明确为 automatic retry stopped。活跃规则通过 maintenance writer 的单一事务 observe，恢复规则在同一事务按完整 descriptor resolve；完全相同 evaluation replay no-op，更晚事实累加 occurrence，解决后更晚复发重开，任一 ownership 冲突或写失败整体回滚且不关闭非 evaluator 事件。

health service 只用 `github.com/robfig/cron/v3 v3.0.1` 的 `@every 30s` entry，并在启动时立即评估。共享 atomic gate 跳过 overlap；snapshot/evaluator/sink failure 或 dependency panic 都保留最后成功 projection 并标记有限 stale failure stage，不保存 panic/error 正文。关闭先 cancel，再等待 `cron.Stop()` drain 在途 evaluation；单次 health failure 不改变 query/index/quota 业务生命周期。

TOO-282 将上述进程内 projection 通过只读 `HealthProjection` Wails query 接入统一 health query cache。DTO 只包含 `hasValue/stale/failure/evaluatedAt/level/primary/components` 以及七组件的有限 evidence/reason/impact/protection/recovery action；缺失 reader、非法组件数、重复 component、非法 enum 或 primary/level 不一致均 fail closed 为标准 binding error，不回退到持久事件数量、日志或前端推断。health invalidation、wake、runtime ready 与 foreground 只使 cache 失效并重取；5 秒 active polling 覆盖 evaluator 周期投影更新，event payload 不携带业务状态。

主导航的“本机状态”项消费同一 projection：可信 healthy 显示七项正常，异常显示需关注组件数，unknown/query error/stale 有独立颜色和文案，stale 明确标注“上次可信”。全局 shell 同一时刻只渲染一个 Banner：优先保留权威 primary 的 blocked、影响数据的 degraded，再处理 query unavailable、offline、unknown/stale、paused/busy/loading；Banner 展示影响、原因、评估时间和有限恢复入口。`retry` 只重新读取 projection，其余已登记 action 进入 `/local-status/data-health`。healthy 不占用 live region，动作失败就地提示且详情导航保留。

TOO-283 将 Data Health 固定为“本机状态”下的二级路由，不新增主导航项。只读 `DataHealth(evaluatedAtMS)` Wails query 从既有 GORM-first `MetricsSnapshot` 取得精确 24 小时半开窗口，并映射 runtime CPU/RSS、DB/WAL/磁盘、live/backfill queue、scheduler/job/source 聚合；Detailed 模式最多 17,280 个原始点由 Go 确定性压缩为最多 289 个趋势点，保留最新与窗口内最早证据，前端不自行读取 SQLite 或从图表推断健康。同一 query 另以 Store typed `CurrentOnly` Job page 优先读取 queued/running 与 `resume_consumed_by_job_id IS NULL` 的 interrupted，再以 active Health page 读取 open 事实；各自剩余的 12 项显示预算只补充窗口内 terminal/resolved 历史。已被 resume child 消费的 interrupted 不再是 current，较新的终态记录不能挤掉当前/open 事实，窗口外历史在 Go 中裁剪。资源/活动读取以 `DataHealth.evaluatedAtMs` 标记，Health Projection 显示自己的 `evaluatedAtMs`；两者不被伪装成同一数据库事务。

Data Health 先展示 primary 的影响和保护措施，再展示七组件 evidence/reason、来源和 scheduler 聚合、当前/最近 job、open/recent event 的 occurrence/impact，以及最近 24 小时 CPU/RSS 和最新 DB/WAL/磁盘/queue。Job phase 只接受真实 contract 的 `discover/fast_bootstrap/history_backfill/reconcile/live/maintenance`，未知值显示“未知任务”而不猜测。动作 registry 不接受任意 command key：`retry` 只重取，`check_source` 映射现有 reconcile command，`repair_store` 只映射 Session index Analyze-only dry-run，`grant_permission/free_space/choose_home` 进入 Settings；未知值不渲染动作。reconcile 与 dry-run 均先显示安全预览，执行中有进度，完成后显示有限 receipt 并失效 Source/Job/Health/Data Health 查询以验证结果；失败不关闭原 health event。repair execute、shell/SQL/filesystem primitive、自动权限/磁盘修改、原始错误和诊断导出仍不属于 v0.1。

retention service 同样只用 `github.com/robfig/cron/v3 v3.0.1`：启动立即补跑，`@every 1m` 只唤醒 due-check，成功后下一次真实 cleanup 为 1 小时，连续失败按 5/10/20/40/60 分钟退避并在 60 分钟封顶。cleanup、checkpoint 与 panic 只映射到有限 failure stage；projection 记录本次起止/耗时、各表 committed deletion、checkpoint frame、连续失败、next due 与最后成功结果，不保存 raw error 或 panic。atomic gate 跳过 overlap，关闭先 cancel 再等待 cron/in-flight operation。应用装配顺序为 metrics、lifecycle、health、retention，退出按 retention、health、lifecycle、metrics、Store 逆序关闭。

## 低开销采集

- 扫描器在内存累加 bytes/lines/rows，job 完成或每 30～60 秒批量 flush。
- 进程 CPU/RSS 正常每 30 秒采样；Detailed 模式为 5 秒。两种周期都由 `github.com/robfig/cron/v3 v3.0.1` 驱动，启动时立即采样一次；service 共享 atomic gate 覆盖 mode 切换时的跨 entry overlap 并计 dropped，cron/collector 双层 panic recovery 保证 scheduler 与 query batch 可继续，不存在自造 ticker/timer loop。
- 默认不开 pprof；metrics 使用最低优先级写入。
- 目标：观测额外 CPU 平均低于 0.5%，观测失败不能影响索引或 quota。

TOO-284 将该目标收敛为可重复的 macOS arm64 validation harness：真实 application collector 每轮同时执行 gopsutil process probe、SQLite/WAL/磁盘与 runnable queue probe、24 小时 MetricsSnapshot query 和 maintenance writer，并按 Detailed 5 秒 cadence 报告 `duty_pct`。机械门槛固定为 duty `<0.5%`、query `<50ms`、RSS `<512MiB`、WAL `<256MiB`；每轮 100 次、重复 5 次，任一缺少 metric 或越界都 fail closed。idle/live/backfill/query/cleanup 另以 BSD time 记录阶段 process envelope，但该 envelope 包含 Go test runner，不能冒充产品常驻 RSS；产品数值只使用 collector 持久的 typed sample。

同一 harness 只在临时目录、fake transport/adapter 与专用 child process 注入 permission、disk full/read-only、SQLite lock、malformed row、network、sleep/wake 和 process interruption。它不修改真实 Home、应用数据库、权限、磁盘、网络或系统睡眠。提交版结果见 [`docs/test/m8-e6.md`](../../../test/m8-e6.md)；原始命令与 time 输出只保存在忽略的 `.agents/runs`。当前实测固定 macOS 15.0 deployment target，但运行 OS 必须单独记录，不能用较新系统的通过结果冒充实际 macOS 15 runtime。

不建设小时或长期汇总表。每小时低优先级清理一次，启动时补清理：

- runtime samples、已完成 job、source attempts、已解决 event：滚动 24 小时；`process_snapshots` 尚未创建，不参与当前 cleanup；
- 未解决 event 和 active/resumable job：保留到解决或完成，再计时 24 小时；
- Data Health 与趋势图只展示当前与最近 24 小时；v0.1 不提供诊断包。

## 健康状态

应用健康分为 `healthy`、`busy`、`paused`、`degraded`、`blocked`，与 Session 活动态无关。

local index、live queue、history backfill、online quota、SQLite、disk 和 updater 分别判断。online quota degraded 不能把整个 local-first 应用显示成故障；busy 和用户主动 paused 也不是异常。

`paused` 只来自 durable `user_pause_scope` 或 system sleeping，不由零吞吐推断；system wake 后仍保留用户 pause。`reconciling` 是短暂 busy，只有 reconcile 失败、confirmed Home generation/identity 漂移或 retry disposition 为 blocked 时才升级为 `blocked`/`degraded`。恢复动作必须使用持久 typed action，不能把原始文件系统或 SQLite 错误直接透传到界面。

初始可调阈值：

- live queue 最老任务等待超过 30 秒：degraded；
- backfill 未暂停但 5 分钟无字节进展：stalled；
- 同一来源连续失败 3 次：warning；10 次：degraded；
- auth 失效：online quota degraded，并停止自动重试；
- SQLite 无法写入或 `SQLITE_FULL`：blocked；
- 磁盘不足 1 GB：warning；
- WAL 超过 256 MB 且 checkpoint 失败：warning；
- 后台 CPU 超过单核 20% 持续 2 分钟：pressure；
- RSS 超过 512 MB 或系统内存 5% 持续 2 分钟：pressure；
- 写事务超过 2 秒或连续 busy：pressure。

阈值需要按真实运行调整；v0.1 不暴露大量高级设置。

同类问题更新同一个 `health_events`，累加 occurrence 和 last seen，不在每次刷新时创建新警告。连续失败在成功后归零。错误详情只保存 error class、影响来源、last attempt 和 next retry，不保存源内容或凭证。

## 展示层级

- Tray：不显示 bytes、CPU 或 queue depth；`blocked` 立即增加红色健康点，`degraded` 持续 2 分钟且影响数据可信度或需要用户操作时增加橙色健康点。
- Popover：不显示来源冲突、网络失败、索引异常或原始 CPU 百分比；诊断信息统一进入本机状态 / Data Health。
- 概览：默认呈现额度、用量趋势和 API 等价成本；只在问题影响当前分析时显示轻量状态，不展开运行诊断。
- 本机状态：健康入口正常时弱化，异常时显示数量；同一时间最多显示一个 Banner，优先级为 blocked、影响当前数据的 degraded、backfill / 部分数据。
- ETA：至少三个稳定采样窗口后才显示。
- Data Health：由本机状态打开，不增加独立主导航项。

Data Health 先展示影响、保护措施和首要恢复动作，再展示各领域状态、live/backfill queue、当前及最近 job、最近 24 小时 CPU/RSS、DB/WAL/磁盘、连续失败和 next retry。当前动作只保留 query 重取、来源 reconcile、Session index Analyze-only dry-run 与 Settings 导航；未由 projection 注册的动作不显示，v0.1 不提供打开原始日志或任意命令入口。

故障文案先解释影响，再显示 error class，例如：“在线配额暂不可用，本地配额快照仍可使用”或“数据库空间不足，索引已安全暂停，游标没有前进”。普通连续失败不产生系统通知。

## 后续能力：安全诊断导出（不属于 v0.1）

v0.1 不提供诊断包或其他用户数据导出。后续若重新评估，只允许按以下边界设计：

允许包含：应用/OS/schema/parser version、最近 24 小时 job/runtime 汇总、health events、错误类型、脱敏路径和非敏感开关。

绝不包含：JSONL 原文、prompt/response/tool output、access/refresh token、Authorization header、Cookie 或完整 `auth.json`。

## 对产品界面的约束

- Tray 保持纯净；blocked 立即升级，degraded 固定持续 2 分钟后再判断是否升级。
- quota 网络失败不在 Tray 或 Popover 增加说明；有 last-known-good 时继续显示普通百分比。
- Popover 不承担健康解释；原始资源指标与故障原因只进入 Data Health。
- history backfill 进度留在本机状态 Banner。
- 本机状态健康入口承担“最近 24 小时是否正常”的统一入口。
