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
| 运行状态 | 游标、调度、采集健康与价格目录 | `source_files`、`source_state`、`source_attempts`、`job_runs`、`health_events`、`pricing_versions` |

## Session 与 Turn

TOO-247 固化六张 `STRICT` 核心表。所有时间为 UTC epoch milliseconds，来源 offset 和 generation 为非负整数。

| 表 | 主键 / 唯一键 | 责任 |
| --- | --- | --- |
| `projects` | `project_id` | 保存脱敏项目维度：`display_name`、`root_path`、可选 `git_remote_sanitized` 和创建/更新时间。 |
| `sessions` | `session_id` | 保存 provider、source kind、初始 cwd、可选项目等稳定 metadata，以及 created/first-seen/last-seen 时间。 |
| `turns` | `turn_id`；`(session_id, source_generation, start_offset)` 唯一 | 保存 turn 生命周期、模型/推理强度、可选项目和可追溯来源位置。 |
| `session_current` | `session_id` | 保存 thread name、active turn、当前模型/cwd 和最后活动时间等可重建投影。 |
| `turn_usage` | `turn_id` | 保存 turn 当前 token snapshot、final 标记、`(source_generation, source_offset)` 观测位置和置信度。 |
| `session_usage_current` | `session_id` | 保存 session 累计计数器的 epoch、当前 snapshot、`(source_generation, source_offset)` 来源位置和 counter state。 |

`sessions.project_id`、`turns.project_id` 删除项目时置空；session 删除会级联清理 turn 和当前投影，turn 删除会级联清理 usage。`session_current.active_turn_id` 只能引用同一 session 的既有 turn，删除 turn 时置空。一个 `FactBatch` 只允许包含同一 Session 的对象，Usage 的 Session 归属在事务内通过关联 Turn 读回核对；不一致返回 `ErrInvalidRecord` 并整批回滚。Repository 在写入前把缺失引用、跨 session active turn 和 stable identity 冲突归一为 `ErrInvalidRecord`，不把 SQLite driver 文本当业务错误 contract。canonical SQL 同时用 CHECK 保证核心 ID、必填文本和非空 optional 文本不接受空字符串，防止绕过 typed repository 后写入另一套语义。

来源文件实体由 TOO-248 的 `source_files` 建立；本卡在事实层使用 `(session_id, source_generation, start_offset)` 定位一条 turn。generation 只允许前进，同一 generation 下同一 turn 的 start offset 不可变化，且一个来源位置不能映射两个 turn。已完成 turn 只有在更高 generation 同时提供完整 completion tuple 时才切换来源位置；同一来源身份的 non-null 字段冲突返回 `ErrInvalidRecord`，provisional replay 不再改写已完成事实。这样 parser 即使重复扫描或旧 generation 乱序到达，也不会生成重复事实或回退当前来源位置。

`sessions` 只存身份和稳定 metadata。重复 `session_meta` 使用 first-known-wins 补充 optional 字段：首次已提交的非空值成为权威，后续不同的非空值返回 `ErrInvalidRecord`，不会覆盖既有事实；首次提交本身仍是明确的权威来源边界。`created_at_ms` / `first_seen_at_ms` 只向更早收敛，`last_seen_at_ms` 只向更晚推进。项目 metadata 只有 `updated_at_ms` 更大时才更新；同一更新时间的冲突 payload 被拒绝，旧扫描只能补充更早的 `created_at_ms`。

thread name、当前模型、最后活动和 active turn 属于可变投影，放在 `session_current`。thread name 使用自己的 `thread_name_updated_at_ms` 合并：缺失或更旧名称不清空/回退现值，同一字段时间的冲突名称返回 `ErrInvalidRecord`；其他 current 字段按整行 `updated_at_ms` 更新，同一时间的冲突 payload 同样拒绝。Session 完成后仍可恢复，因此不使用永久 `ended_at`。

不持久化 Session status。查询层派生最小活动态：

```text
source freshness 不可用或索引不完整 -> unknown
存在 completed_at_ms IS NULL 的 turn -> active
没有未完成 turn                    -> idle
```

`Done` 是 turn outcome；`Stale` 是来源 freshness。v0.1 不推断 Waiting、Blocked 或 Error。

## Usage 与 Cost

- `turn_usage(turn_id, observed_at_ms, is_final, input_tokens, cached_input_tokens, output_tokens, reasoning_tokens, context_window, source_generation, source_offset, confidence, updated_at_ms)`
- `session_usage_current(session_id, counter_epoch, total_input_tokens, total_cached_tokens, total_output_tokens, total_reasoning_tokens, observed_at_ms, source_generation, source_offset, counter_state)`
- `pricing_versions(pricing_version, source, currency, effective_from_ms, created_at_ms)`
- `model_prices(pricing_version, match_kind, model_pattern, priority, input_micros_per_million, cached_input_micros_per_million, output_micros_per_million)`
- `turn_costs(turn_id, pricing_version, estimated_usd_micros, pricing_status, calculated_at_ms)`

同一 turn 的 `last_token_usage` 会多次更新，因此只 upsert 一条 `turn_usage`。`task_started` 创建 provisional 行，token snapshot 覆盖当前值，`task_complete` 将 `is_final = 1`。历史报表和日聚合只统计 final turn；Dashboard 可单独叠加 active turn 暂估，完成后由同一行替换，不能重复计费。

`session_usage_current` 只用于总量展示、单调性校验和 counter reset 检测，不参与日/周成本求和。累计值下降时开启新的 `counter_epoch`，不能生成负 delta。

所有 token 字段均可为 `NULL`：`NULL` 表示 source 没有提供该值，整数 `0` 表示已观测到真实零。typed repository 以 pointer 保留这个差异。`turn_usage` 按 `(source_generation, source_offset)` 接受严格更新，同 generation 较小 offset 返回 `ErrInvalidRecord`，final snapshot 不能被 provisional snapshot 覆盖；completed Turn 只接受 `is_final = 1` 的 usage，Turn-only completion 会清除既有的同 generation provisional current usage。completion 同批携带 usage 时先保留旧 ordering evidence 完成校验/upsert，再执行 cleanup，equal 冲突或 lower offset 会让 Turn completion 一并回滚。同一 batch 的 turn/usage generation 必须相等，且 usage generation 必须精确等于事务内实际采用的 turn generation，否则整批回滚。Turn 切换到新 generation 时删除旧 generation 的 current usage，typed query 也只连接同 generation snapshot，且不会向 completed Turn 暴露 provisional row；直到新 generation final usage 到达前保持 unknown。`session_usage_current` 按 `(source_generation, counter_epoch, source_offset)` 接受严格更新，新 generation 允许文件 offset 和重建后的 counter epoch 从低值重新开始。相同排序键只允许 payload 完全一致地重放，冲突值返回 `ErrInvalidRecord`，不使用 `>=` 做 last-arrival-wins。

金额统一使用整数微币种单位，不使用 SQLite `REAL`；v0.1 内置目录的币种为 `USD`，每个值表示每百万 token 的微美元。价格字段允许 `NULL` 表示目录没有给出该类别，非空整数 `0` 才表示真实免费。

Pricing Catalog 以 `(source, currency, effective_from_ms)` 形成不可变时间线。同 source/currency 的版本生效区间由相邻版本推导为 `[effective_from_ms, next_effective_from_ms)`，最后一个版本的结束时间为 `NULL`；新增版本不更新上一版本。模型规则只允许 `exact`、`prefix`、`default`，`default` pattern 固定为 `*`。匹配先按 `priority DESC`，再按 exact、prefix、default，随后按 pattern 长度和字典序稳定决胜；没有有效版本或规则时返回 `ErrNotFound`，成本层保留 `unpriced`，不能伪装为零成本。版本与全部规则在同一 writer transaction 追加；完全一致重放 no-op，同 version 或同生效边界的冲突全部回滚。

Pricing Catalog 更新时由后续成本层基于 `turn_usage` 重算 cost，不修改 token 事实。

## Quota

- `quota_observations(observation_id, account_scope, source, limit_id, window_kind, used_percent, window_minutes, resets_at_ms, plan_type, validity, rejection_reason, first_observed_at_ms, last_observed_at_ms, sample_count, request_id, session_id, source_offset)`
- `quota_current(account_scope, window_kind, observation_id, effective_used_percent, window_generation, selected_source, freshness_state, conflict_state, fresh_until_ms, last_success_at_ms, last_attempt_at_ms)`

v0.1 的 `account_scope` 固定为 `default`。同一来源、窗口代际、used 和 validity 连续相同时，只更新 observation 的时间范围和 sample count；值、reset、来源或 validity 变化时才新增 observation。

完整可信和仲裁规则见 [配额设计](../quota/README.md)。

## 运行与增量索引

- `source_files(source_file_id, provider, session_id, current_path, device_id, inode, size_bytes, mtime_ns, parsed_offset, parser_version, active_generation, state, last_scanned_at_ms, last_error_class, updated_at_ms)`
- `source_state(source_instance_id, source_type, scope_key, last_attempt_at_ms, last_success_at_ms, next_due_at_ms, consecutive_failures, last_error_class, freshness_state, cursor_version, updated_at_ms)`
- `source_attempts(request_id, source_instance_id, started_at_ms, finished_at_ms, outcome, http_status, error_class, payload_sha256)`
- `job_runs(job_id, job_type, requested_by, priority, state, phase, source_file_id, resume_of_job_id, created_at_ms, started_at_ms, finished_at_ms, progress_current, progress_total, resume_generation, resume_offset, error_class, updated_at_ms)`
- `process_snapshots(snapshot_id, session_id, pid, cpu, rss, ports_json, child_count, captured_at_ms, valid_until_ms)`
- `app_runtime_samples(captured_at_ms, cpu_percent, cpu_user_ms, cpu_system_ms, rss_bytes, goroutine_count, db_bytes, wal_bytes, disk_free_bytes, live_queue_depth, backfill_queue_depth)`
- `health_events(event_id, fingerprint, domain, severity, code, source_file_id, job_id, error_class, first_seen_at_ms, last_seen_at_ms, resolved_at_ms, occurrence_count, updated_at_ms)`

`source_file_id` 不依赖路径，稳定物理身份为 `(provider, device_id, inode)`；可选 `session_id` 只能引用既有 Session。文件移入 `archived_sessions` 时只更新 `current_path`。同 generation 的 `size_bytes`、`mtime_ns` 和 `parsed_offset` 不能倒退，且 offset 不得超过 size；新 generation 可以从较小 size/offset 重新开始，旧 generation 会被拒绝。本卡只约束 `source_files` 自身单调性；事实与 offset 的跨表原子推进由 TOO-249 提供。

`source_state` 以 `(source_type, scope_key)` 绑定 stable identity，按 `updated_at_ms` 与 `cursor_version` 单调推进；due query 保留 `NULL next_due_at_ms` 与真实时间的差异。`source_attempts` 是 append-only 完成历史，`request_id` 完全一致时才允许幂等重放。`http_status=NULL` 表示没有 HTTP 响应，不能用 `0` 代替。`payload_sha256` 只接受固定 64 位小写十六进制 SHA-256 digest；调用方先对稳定结构化 identity 做摘要，持久层不接收 payload、prompt、response、tool output 或完整 JSONL 行。

Job state 只允许 `queued`、`running`、`succeeded`、`failed`、`cancelled`、`interrupted`，phase 只允许 `discover`、`fast_bootstrap`、`history_backfill`、`reconcile`、`live`、`maintenance`。Repository 允许 `queued -> running/cancelled/interrupted` 和 `running -> running/succeeded/failed/cancelled/interrupted`；phase、progress 和 update time 不能倒退，terminal row 不可复活。应用重启时把遗留 queued/running jobs 原子标为 `interrupted`；恢复只能经 `ResumeInterruptedJob` 新建 queued job，`resume_of_job_id` 必须指向 interrupted history，新 job 完整继承 type、priority、source、phase、progress 与 typed `JobCursor{Generation, Offset}`，且 created/updated time 不早于旧 terminal ordering key。公共 `CreateJobRun` 拒绝直接写 resume lineage。

Health event 用唯一 64 位小写 SHA-256 `fingerprint` 折叠同类问题，domain 只能是 `source/job/store/pricing/runtime`。code 不是开放字符串：Source 只允许 `timeout/unavailable/permission/corrupt/stale`，Job 只允许 `interrupted/failed/cancelled`，Store 只允许 `busy/disk_full/read_only/permission/io/corrupt/unavailable/unknown`，Pricing 只允许 `unavailable/invalid`，Runtime 只允许 `unknown`，实际值均带对应 domain 前缀。Repository 与 STRICT DDL 共同校验完整 domain/code pair。完全相同的 observation time/payload 重放 no-op；更晚观测增加 `occurrence_count` 并重开已解决事件；同时间冲突或更早观测会被拒绝。resolve time 不得早于 last seen，且同一生命周期的 resolve 只能完全一致重放；已解决事件只接受严格晚于 `resolved_at_ms` 的观测重开，resolve 之前迟到的 observation 不得回退 `updated_at_ms`。`source_file_id` 与 `job_id` 是可选关联，删除引用对象时置空而不删除事件。

运行事实只持久化 allowlisted `error_class`：`canceled`、`busy`、`disk_full`、`read_only`、`permission`、`io`、`corrupt`、`timeout`、`unavailable`、`invalid_input`、`unknown`。typed API 和 STRICT DDL 都不提供 raw error/message/body/stack、token、cookie、Authorization 或完整内容字段；fingerprint/payload 只能由 opaque `SHA256Digest` 值对象提供，cursor 只能是两个非负整数，health domain/code 只接受上述有限组合。Source/Job 的业务 identity、路径等语义 metadata 仍会按模型原样持久化；Repository 不是任意字符串的凭据扫描器，调用方不得把密钥放入这些 metadata 字段。

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

TOO-247 的 `FactBatch` 在 TOO-246 提供的唯一 writer queue 上按 `projects -> sessions -> turns -> turn_usage -> session_current -> session_usage_current` 写入；任一校验、外键或 SQL 步骤失败都会回滚整批。`Session`、`Turn` 与 `ListTurns` typed query 只从已提交事实读取，列表查询支持 session、项目、模型、来源位置和起始时间范围过滤，并与可选 usage 一次 join 返回。

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

TOO-249 已把连接与 Repository 收敛为 GORM-first Pure Go adapter。依赖固定为 `gorm.io/gorm v1.31.2`、`github.com/libtnb/sqlite v1.2.0` 和 `modernc.org/sqlite v1.53.0`；生产 SQLite 编译链不使用 `gorm.io/driver/sqlite` 或 `github.com/mattn/go-sqlite3`，`internal/store/sqlite` 与 `internal/store` 必须在 `CGO_ENABLED=0` 下构建和测试。固定版本 GORM 的上游 `go.mod` 自带 official SQLite driver/mattn 间接元数据，但 `CGO_ENABLED=0 go list -deps ./internal/store` 的实际编译依赖不得包含二者。Wails macOS adapter 自身仍需要 CGO，因此 `internal/app` 和全仓 Wails 验证使用 macOS 默认 CGO；这与 SQLite Pure Go门禁是两个独立事实。

应用启动时由 `internal/app.Run` 打开一个 Store，Store 内部持有一个物理 writer connection 和独立 read-only pool；业务 repository 不得自行打开第二条写路径。GORM models 只存在于 persistence adapter，显式映射到 `Project`、`Session`、`Turn`、`SourceFile`、`JobRun`、`HealthEvent`、`PricingVersion` 等 domain types，不向 UI 或业务层泄漏。

空配置的实际默认值：

| 项 | 默认值 | 启动期验证 |
| --- | --- | --- |
| 数据库 | `~/Library/Application Support/Codex Pulse/codex-pulse.db` | 解析绝对路径；默认专用目录拒绝 symlink，权限收紧并读回为 `0700`；DB 文件为 `0600` |
| writer | `mode=rwc`、private cache、一个 physical connection | `journal_mode=wal`、`foreign_keys=1`、`synchronous=1 (NORMAL)`、`busy_timeout=5000`、`query_only=0` |
| readers | `mode=ro`、private cache、最多 4 个 physical connections | `journal_mode=wal`、`foreign_keys=1`、`synchronous=1 (NORMAL)`、`busy_timeout=5000`、`query_only=1` |
| 写队列 | 容量 128 | 非阻塞 admission；满时返回可判定的 `ErrQueueFull` |

writer transaction 使用 `BEGIN IMMEDIATE`，由唯一 worker 按 FIFO 创建、提交或回滚；底层 callback 得到绑定当前 `database/sql.Tx` 的 `*gorm.DB`，不能自行 `Commit` / `Rollback`。跨 core/runtime 写入使用 `Repository.WithinWriteUnit`，其 `WriteUnit` 只暴露 typed operations，callback 返回后立即失效；现有单操作 Repository 方法复用同一事务体。读 callback 得到绑定 read-only pool 的 GORM session，底层 DSN 同时使用 `mode=ro` 与 `query_only=ON`，防止把只读入口变成旁路写入。

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

应用打开 Store 后、将其暴露给任何 runtime reader/writer 之前，由显式 migration catalog 管理 `PRAGMA user_version` 与 `schema_migrations(version, name, checksum, applied_at_ms)` append-only ledger。`MigrateApplicationSchema` 只属于该启动期 bootstrap 契约；若未来需要运行期 maintenance migration，必须先由上层实现任务排空和 Store 独占，不得直接复用启动调用。catalog 必须从 1 连续、名称和 SHA-256 checksum 稳定；history 缺号、checksum drift、ledger/user_version 分叉或数据库版本高于二进制都会 fail closed。所有 pending migration 在同一个 writer transaction 中顺序执行，只有全部 schema readback 和 ledger 写入成功后才推进 `user_version`，任一步失败会连同本轮所有 pending objects 一起回滚。

fresh 空库不做无意义备份。已有用户 schema 且存在 pending migration 时，runner 先只读检查状态和可用空间，再通过 modernc `NewBackup` / `Step` / `Remaining` / `PageCount` 创建包含 committed WAL 页的 `0600` 快照，成功发布后才进入 migration transaction；失败或取消只清理 `.partial-*`。文件级恢复原语使用 `NewRestore`，只恢复到尚不存在的新文件，不在运行中覆盖当前 Store。`STRICT`、复杂 `CHECK`、特殊 index、连接 PRAGMA、`sqlite_schema` canonical readback 和 `EXPLAIN QUERY PLAN` 是隔离的 raw SQL 例外；普通 CRUD、filter、关联检查和事务编排使用 GORM。

当前核心索引为：

- 唯一 `turns(session_id, source_generation, start_offset)`，用于来源去重和定位。
- `turns(session_id, started_at_ms DESC, turn_id DESC, completed_at_ms)`，用于 session 时间列表、稳定排序和生命周期覆盖。
- `turns(project_id, started_at_ms DESC, turn_id DESC, completed_at_ms)` 与 `turns(model, started_at_ms DESC, turn_id DESC, completed_at_ms)`，用于项目/模型时间列表并避免额外临时排序。
- `session_current(last_activity_at_ms)`，用于最近 session 投影。
- `turn_usage(observed_at_ms, is_final)`，用于按观测时间筛选 final usage。

当前 runtime 索引为：

- `source_files(session_id, state, last_scanned_at_ms, source_file_id)` 与 `source_state(next_due_at_ms, source_instance_id)`，用于 session/state 扫描和 due source。
- `source_attempts(source_instance_id, started_at_ms DESC, request_id DESC)`，用于来源尝试历史。
- `job_runs(state, updated_at_ms, priority DESC, job_id)`、`job_runs(source_file_id, created_at_ms DESC, job_id DESC)` 与 `job_runs(updated_at_ms, priority DESC, job_id)`，用于 startup recovery、队列、source 作业历史和无 filter 列表。
- `health_events(resolved_at_ms, last_seen_at_ms DESC, event_id, severity)`、`health_events(last_seen_at_ms DESC, event_id)`、`health_events(severity, last_seen_at_ms DESC, event_id)`、`health_events(source_file_id, last_seen_at_ms DESC, event_id)` 与 `health_events(job_id, last_seen_at_ms DESC, event_id)`，用于 active/resolved/history/severity 和 source/job 单关系追溯。
- `pricing_versions(source, currency, effective_from_ms DESC)` 与 `model_prices(pricing_version, priority DESC, match_kind, model_pattern)`，用于 as-of 版本和模型规则匹配。

上述 required query 由 GORM model/scopes 构造；测试文件维护等价的代表查询用于 `EXPLAIN QUERY PLAN`，要求命名索引被选择且不得出现临时排序。quota、process snapshot 和 app runtime sample 索引由对应后续 Execution 卡创建。core/runtime 表不提供 prompt、response、tool output、原始 JSONL、鉴权 token 或 raw error 的专用字段；schema contract 和数据库 bytes 测试会对受控 code/digest/cursor/error 输入面使用 synthetic marker 验证拒绝与不可见性。业务 identity/path metadata 的凭据卫生仍由调用方负责。

## 保留策略

session、turn、final usage、turn cost、quota observation 和 daily rollup 长期保留。

资源与故障明细只服务“最近是否正常”：`source_attempts`、已完成 `job_runs`、`process_snapshots`、`app_runtime_samples` 和已解决 `health_events` 滚动保留 24 小时。未解决 event 和 active/resumable job 不受清理影响，解决或完成后再开始计时。

current projection 随时可重建。任何清理都不能删除 Codex 原始 JSONL，Tracker 本来也不拥有这些文件。
