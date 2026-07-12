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

Go 难以准确获得单 goroutine CPU 时间，因此 job CPU 使用进程级 user/system CPU delta 近似。后台只有一个重型 worker 时可以近似归因，但 UI 和诊断必须标注 process-level estimate，不能称为精确任务 CPU。

## 低开销采集

- 扫描器在内存累加 bytes/lines/rows，job 完成或每 30～60 秒批量 flush。
- 进程 CPU/RSS 正常每 30 秒采样；打开 Data Health 时临时提高到 5 秒。
- 默认不开 pprof；metrics 使用最低优先级写入。
- 目标：观测额外 CPU 平均低于 0.5%，观测失败不能影响索引或 quota。

不建设小时或长期汇总表。每小时低优先级清理一次，启动时补清理：

- runtime samples、已完成 job、source attempts、process snapshots、已解决 event：滚动 24 小时；
- 未解决 event 和 active/resumable job：保留到解决或完成，再计时 24 小时；
- Data Health、趋势图和诊断包：只展示当前与最近 24 小时。

## 健康状态

应用健康分为 `healthy`、`busy`、`paused`、`degraded`、`blocked`，与 Session 活动态无关。

local index、live queue、history backfill、online quota、SQLite、disk 和 updater 分别判断。online quota degraded 不能把整个 local-first 应用显示成故障；busy 和用户主动 paused 也不是异常。

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

Data Health 先展示影响、保护措施和首要恢复动作，再展示各领域状态、live/backfill queue、当前及最近 job、最近 24 小时 CPU/RSS、DB/WAL/磁盘、连续失败和 next retry。操作只保留重试、暂停 / 继续索引、重新授权在线能力和打开日志。

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
