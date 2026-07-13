# Data Model and Indexing

## JSONL 增量索引边界

“增量导入”的准确含义是增量索引 / 增量解析。Codex 已经把 JSONL 写入 `~/.codex/sessions` 和 `~/.codex/archived_sessions`；Codex Pulse 不复制、不归档这些文件，只从 `source_files.parsed_offset` 继续读取新增字节。

```text
发现文件变化
    -> 从 parsed_offset 读取新增完整行
    -> 解析有限结构化字段
    -> 丢弃原始 JSONL 行
    -> 事务写入结构化数据
    -> 写入成功后提交 parsed_offset
```

必须先成功写入事实，再推进 offset。不完整尾行留到下次文件增长后解析。inode 改变、size 变小或 parser version 升级时，只重建受影响文件；正常刷新不全量重扫。

源 JSONL 被 Codex 删除或归档后，Tracker 仍保留 session 时间、项目、模型、usage 和 quota observation，但无法还原完整对话。这是有意设计的隐私边界。

## 数据层次

| 层级 | 含义 | 典型对象 |
| --- | --- | --- |
| 外部事实源 | Codex 维护，Tracker 不复制 | JSONL、`session_index.jsonl` |
| 持久事实 | 长期保留、可审计的结构化数据 | `sessions`、`turns`、`turn_usage`、`quota_observations` |
| 当前与聚合投影 | 可重建，服务 UI 快速查询 | `session_current`、`quota_current`、daily usage |
| 运行状态 | 游标、调度和采集健康 | `source_files`、`source_state`、`source_attempts`、`job_runs` |

## Session 与 Turn

- `projects(project_id, display_name, root_path, git_remote_sanitized, created_at_ms, updated_at_ms)`
- `sessions(session_id, provider, originator, source_kind, model_provider, initial_cwd, project_id, cli_version, created_at_ms, first_seen_at_ms, last_seen_at_ms)`
- `session_current(session_id, thread_name, thread_name_updated_at_ms, active_turn_id, current_model, current_cwd, last_activity_at_ms, updated_at_ms)`
- `turns(turn_id, session_id, started_at_ms, completed_at_ms, outcome, model, reasoning_effort, cwd, project_id, source_generation, start_offset, complete_offset)`

`sessions` 只存身份和稳定 metadata。重复 `session_meta` 使用 upsert 补充信息，不能重置 `created_at_ms`。thread name、当前模型、最后活动和 active turn 属于可变投影，放在 `session_current`。Session 完成后仍可恢复，因此不使用永久 `ended_at`。

不持久化 Session status。查询层派生最小活动态：

```text
source freshness 不可用或索引不完整 -> unknown
存在 completed_at_ms IS NULL 的 turn -> active
没有未完成 turn                    -> idle
```

`Done` 是 turn outcome；`Stale` 是来源 freshness。v0.1 不推断 Waiting、Blocked 或 Error。

## Usage 与 Cost

- `turn_usage(turn_id, observed_at_ms, is_final, input_tokens, cached_input_tokens, output_tokens, reasoning_tokens, context_window, source_offset, confidence, updated_at_ms)`
- `session_usage_current(session_id, counter_epoch, total_input_tokens, total_cached_tokens, total_output_tokens, total_reasoning_tokens, observed_at_ms, source_offset, counter_state)`
- `pricing_versions(pricing_version, effective_at_ms, source, created_at_ms)`
- `model_prices(pricing_version, model_pattern, input_usd_micros_per_million, cached_usd_micros_per_million, output_usd_micros_per_million)`
- `turn_costs(turn_id, pricing_version, estimated_usd_micros, pricing_status, calculated_at_ms)`

同一 turn 的 `last_token_usage` 会多次更新，因此只 upsert 一条 `turn_usage`。`task_started` 创建 provisional 行，token snapshot 覆盖当前值，`task_complete` 将 `is_final = 1`。历史报表和日聚合只统计 final turn；Dashboard 可单独叠加 active turn 暂估，完成后由同一行替换，不能重复计费。

`session_usage_current` 只用于总量展示、单调性校验和 counter reset 检测，不参与日/周成本求和。累计值下降时开启新的 `counter_epoch`，不能生成负 delta。

金额统一使用整数微美元，不使用 SQLite `REAL`。Pricing Catalog 更新时由 `turn_usage` 重算 cost，不修改 token 事实；未匹配模型保留 `unpriced`。

## Quota

- `quota_observations(observation_id, account_scope, source, limit_id, window_kind, used_percent, window_minutes, resets_at_ms, plan_type, validity, rejection_reason, first_observed_at_ms, last_observed_at_ms, sample_count, request_id, session_id, source_offset)`
- `quota_current(account_scope, window_kind, observation_id, effective_used_percent, window_generation, selected_source, freshness_state, conflict_state, fresh_until_ms, last_success_at_ms, last_attempt_at_ms)`

v0.1 的 `account_scope` 固定为 `default`。同一来源、窗口代际、used 和 validity 连续相同时，只更新 observation 的时间范围和 sample count；值、reset、来源或 validity 变化时才新增 observation。

完整可信和仲裁规则见 [配额设计](../quota/README.md)。

## 运行与增量索引

- `source_files(source_file_id, provider, session_id, current_path, device_id, inode, size, mtime_ns, parsed_offset, parser_version, active_generation, state, last_scanned_at_ms, last_error)`
- `source_state(source_instance_id, source_type, scope_key, last_attempt_at_ms, last_success_at_ms, next_due_at_ms, consecutive_failures, last_error_class, freshness_state, cursor_version)`
- `source_attempts(request_id, source_instance_id, started_at_ms, finished_at_ms, outcome, http_status, error_class, payload_hash)`
- `job_runs(job_id, job_type, requested_by, priority, phase, started_at_ms, finished_at_ms, outcome, total_files, files_scanned, total_bytes, bytes_read, rows_written, checkpoint_at_ms, duration_ms, cpu_user_ms, cpu_system_ms, queue_wait_ms, active_work_ms, yield_ms, peak_live_queue_depth, peak_backfill_queue_depth, oldest_wait_ms, error_class)`
- `process_snapshots(snapshot_id, session_id, pid, cpu, rss, ports_json, child_count, captured_at_ms, valid_until_ms)`
- `app_runtime_samples(captured_at_ms, cpu_percent, cpu_user_ms, cpu_system_ms, rss_bytes, goroutine_count, db_bytes, wal_bytes, disk_free_bytes, live_queue_depth, backfill_queue_depth)`
- `health_events(event_id, domain, severity, code, first_seen_at_ms, last_seen_at_ms, resolved_at_ms, occurrence_count)`

`source_file_id` 与 Session 身份绑定，不依赖路径；文件移入 `archived_sessions` 时只更新 `current_path`。`payload_hash` 只用于不含正文的结构化 quota 响应，不对 prompt、response、tool output 或完整 JSONL 行做 hash。

## 幂等键和事务

优先使用 provider 原生 ID：

```text
turn_started  = codex:{session_id}:turn:{turn_id}:started
turn_complete = codex:{session_id}:turn:{turn_id}:complete
turn_usage    = codex:{session_id}:turn:{turn_id}:usage
```

没有原生 ID 的结构化观测使用 `codex:{session_id}:offset:{line_start_offset}:{event_kind}`；同一行的多个 quota window 追加 window 标识。key 不包含文件路径或用户内容 hash。

每批完整行在一个写事务中提交：

```text
读取完整行并识别结构化字段
    -> BEGIN IMMEDIATE
    -> UPSERT sessions / turns / usage
    -> 压缩写入 quota observations
    -> 更新 current projections
    -> 更新 parsed_offset
    -> COMMIT
```

任何步骤失败都 rollback，事实和 offset 一起保持旧值。文件截断、inode 异常或 parser version 升级时，在新 generation 构建并校验派生事实，原子切换 `active_generation` 后再延迟清理旧 generation。

## Daily 聚合

- `project_usage_daily(bucket_start_ms, reporting_timezone, project_id, turn_count, input_tokens, cached_input_tokens, output_tokens, reasoning_tokens, total_tokens, estimated_usd_micros, priced_turn_count, unpriced_turn_count, first_activity_at_ms, last_activity_at_ms, rollup_version, updated_at_ms)`
- `model_usage_daily(bucket_start_ms, reporting_timezone, model, turn_count, input_tokens, cached_input_tokens, output_tokens, reasoning_tokens, total_tokens, estimated_usd_micros, priced_turn_count, unpriced_turn_count, first_activity_at_ms, last_activity_at_ms, rollup_version, updated_at_ms)`

主键分别为 `(bucket_start_ms, reporting_timezone, project_id)` 和 `(bucket_start_ms, reporting_timezone, model)`。默认 reporting timezone 为系统时区，当前使用 `Asia/Shanghai`。turn 归属日期使用 final `turn_usage.observed_at_ms`。

周、月、年从 daily 表 `SUM`，不维护 weekly/monthly 表。修正 parser、项目归属、模型或价格时，必须先从旧 bucket 减去旧贡献，再向新 bucket 加入；大规模重建使用 shadow generation，校验后原子切换。

## SQLite 约定

- UTC 时间：epoch milliseconds `INTEGER`；mtime：纳秒整数。
- token：非负 `INTEGER`；quota percent：带 `0..100` 约束的 `REAL`。
- bool：`0/1 INTEGER`；金额：整数微美元；表尽量使用 `STRICT`。
- 启用 `foreign_keys=ON`、WAL、`synchronous=NORMAL`、`busy_timeout=5000`。
- 使用单写入队列与独立只读连接。

### 连接与关闭基线

TOO-246 已把上述约定固化在 `internal/store/sqlite`。应用启动时由 `internal/app.Run` 打开一个 Store，Store 内部持有一个物理 writer connection 和独立 read-only pool；业务 repository 不得自行打开第二条写路径。SQLite driver 固定为 `github.com/mattn/go-sqlite3 v1.14.48`，因此构建和测试要求 `CGO_ENABLED=1`。

空配置的实际默认值：

| 项 | 默认值 | 启动期验证 |
| --- | --- | --- |
| 数据库 | `~/Library/Application Support/Codex Pulse/codex-pulse.db` | 解析绝对路径；默认专用目录拒绝 symlink，权限收紧并读回为 `0700`；DB 文件为 `0600` |
| writer | `mode=rwc`、private cache、一个 physical connection | `journal_mode=wal`、`foreign_keys=1`、`synchronous=1 (NORMAL)`、`busy_timeout=5000`、`query_only=0` |
| readers | `mode=ro`、private cache、最多 4 个 physical connections | `journal_mode=wal`、`foreign_keys=1`、`synchronous=1 (NORMAL)`、`busy_timeout=5000`、`query_only=1` |
| 写队列 | 容量 128 | 非阻塞 admission；满时返回可判定的 `ErrQueueFull` |

writer transaction 使用 `BEGIN IMMEDIATE`，由唯一 worker 按 FIFO 创建、提交或回滚；callback 只能得到不暴露 `Commit` / `Rollback` 的 `WriteTx`。读 callback 只能得到 query surface，底层 DSN 同时使用 `mode=ro` 与 `query_only=ON`，防止把只读入口变成旁路写入。

空 Path 表示 Store 自己管理上述默认专用目录，可以收紧既有默认目录和 DB 权限。显式 Path 的既有父目录与 DB 仍归调用方所有：Store 只接受已分别为 `0700` / `0600` 的路径，不会静默 chmod 共享目录或文件；若显式路径的最终目录尚不存在，Store 才创建新的私有目录。

context 取消在入队前可以直接拒绝。job 一旦被队列接受，`Write` 必须等待 worker 的 authoritative result：排队取消会在开始事务前返回取消，执行中取消会 rollback；如果 Commit 已经赢得竞争并成功，则调用方收到成功而不是猜测性的取消，避免“返回已取消但事实已落盘”后重试产生重复数据。

`Close` 的固定顺序是：

```text
原子切换为 closing 并拒绝新读写
    -> 排空切换前已接受的 writer queue
    -> 等待在途 read callback 返回
    -> 关闭 read pool
    -> 关闭 writer connection
    -> 切换为 closed 并唤醒所有 Close 等待者
```

调用方取消等待不会中止已经开始的关闭。队列满、context 取消、closing/closed、SQLite busy/locked、disk full、read-only、permission、I/O 和 corrupt 都有稳定 sentinel；底层 context 与 driver error 仍保留在 error chain 中，禁止依赖 `database is locked` 等字符串分支。

本基线不创建业务表。下面的事实、投影、游标、migration 和保留策略仍由 TOO-247～250 分卡落地。

必要索引至少包括：`turns(session_id, completed_at_ms, started_at_ms)`、`turns(project_id, completed_at_ms)`、`turns(model, completed_at_ms)`、`session_current(last_activity_at_ms)`、`quota_observations(account_scope, window_kind, last_observed_at_ms)`、`job_runs(job_type, started_at_ms)`。

## 保留策略

session、turn、final usage、turn cost、quota observation 和 daily rollup 长期保留。

资源与故障明细只服务“最近是否正常”：`source_attempts`、已完成 `job_runs`、`process_snapshots`、`app_runtime_samples` 和已解决 `health_events` 滚动保留 24 小时。未解决 event 和 active/resumable job 不受清理影响，解决或完成后再开始计时。

current projection 随时可重建。任何清理都不能删除 Codex 原始 JSONL，Tracker 本来也不拥有这些文件。
