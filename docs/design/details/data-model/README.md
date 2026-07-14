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

### Rollout Parser Contract

`internal/codex/logs.StreamParser` 的当前版本为 `codex-rollout-v1`，只接受 `session` 和 `archived_session` source；`session_index.jsonl` 的名称/repair 语义由后续独立流程处理。caller 从已提交的非负 offset 创建 parser，并按严格连续 offset feed 字节块：

- `ReadOffset` 表示 parser 已接收的字节末端。
- `CommittableOffset` 只越过 `\n` 或 `\r\n` 结束的完整行；半行即使已读入也不会推进。尚待 start 的 context/terminal 不再钉住 offset，而是进入 `NextSeed.PendingTurns` 的有界安全 checkpoint。
- 默认单行内容上限为 16 MiB，硬上限为 64 MiB。超长行不继续保留内容，parser 丢弃到下一换行，输出一次 content-free diagnostic 后恢复。
- gap 或 replay feed 返回稳定 sentinel，parser state 不变。持久事务失败时 caller 必须丢弃当前 parser，并使用旧 committed offset 与旧 seed 重建。
- offset 为 0 时不接受 seed，默认由 `session_meta` 建立 lifecycle。offset 大于 0 时必须提供上一成功事务原样持久化的 `NextSeed`；如果该位置之前尚未见到 session，允许空 seed。
- `NextSeed` 是 content-free、深拷贝、可校验的完整 parser checkpoint：完整权威 `SessionMetaFact`；最多 64 个 open turn 的 start/context/latest provisional usage；最多 1024 个 pending turn 的安全 context/terminal 与原 source position；最近 1024 个 closed turn 的 start/terminal 摘要。它不包含 raw JSONL、prompt、response、reasoning、tool output、raw type/error 或未知原文。

每条完整行先做 UTF-8、完整 JSON 和全层级重复 key 检查，再只按 `timestamp/type/payload` envelope 分类。支持的结构化 family 固定为：

| Rollout family | 当前消费字段 |
| --- | --- |
| `session_meta` | thread `id` 作为 domain session ID；legacy `session_id` 缺失时回退 `id`；created time、source kind、cwd、originator、CLI version、session source、model provider |
| `turn_context` | optional turn ID、observed time、cwd、model、reasoning effort；当前官方值含 `ultra`，未知非空未来值只归一为 `custom`，不回显原文 |
| `event_msg.task_started/turn_started` | turn ID、explicit/fallback started time、optional context window |
| `event_msg.task_complete/turn_complete` | turn ID、explicit/fallback completed time、`completed` outcome |
| `event_msg.turn_aborted` | optional turn ID、explicit/fallback completed time、四种 allowlisted abort outcome |
| `event_msg.token_count` | session cumulative 与 last-turn nullable input/cached/output/reasoning counters、optional context window；`rate_limits` 留给 quota 模块 |

`response_item`、`compacted`、`inter_agent_communication` 和已知 content-bearing `event_msg` 只计为 known ignored line。新 rollout/event type 输出无原始名称的 compatibility diagnostic；空行、坏 JSON、重复 key、非法字段、未知类型或超长行都不会阻塞后续完整行。diagnostic 只包含固定 class/code、行起止 offset 和 retryable，不包含 path、原始 JSON、原始 type、decoder error 或内容摘要。

Turn lifecycle 只接受显式事件：start 建立 open turn；无 turn ID 的 context/abort 只有恰好一个 open turn 时才能关联；last-turn usage 在唯一 open turn 上形成 provisional snapshot；terminal 复制该 turn 最新 snapshot 为 final，没有 snapshot 时保持 `NULL`。带明确 turn ID 的 context/terminal 可以先于 start 暂存为安全小结构，start 到达后按 start→context→terminal 的因果顺序发出；terminal 后才出现的 context 不会被重新排序到 terminal 前。context/turn usage/terminal 早于 start time、冲突重放等情况 fail closed。内存和 checkpoint 状态固定限制为 64 个 open turn、1024 个 pending turn 和最近 1024 个 closed turn；超限输出 `state_limit_exceeded` 并丢弃不能安全保留的状态，closed replay 保证只覆盖该有界窗口。parser 不从新 turn、EOF、mtime 或文件归档猜测 interruption。

因果输出顺序与 source position 是两个不同维度。terminal-before-start 在 late start 到达后按 `TurnStarted -> TurnEnded` 输出，但两条事件仍保留原始行位置，因此合法存在 `TurnEnd.Position.StartOffset < TurnStart.Position.StartOffset`。Store v3 保留这些原始 offset，不做重写或排序；`complete_offset < start_offset` 是合法状态，只继续要求二者非负且 `completed_at_ms >= started_at_ms`。

diagnostic 按实际 emission order 返回：当前输入中的 framing/decoder 结果按 source order 处理；pending 在后续 start 处解析产生的延迟 diagnostic 在解析时追加，不做跨 Feed 的全局 offset 重排。因此相同字节流的一次 Feed 与任意连续分块 Feed 累积结果一致。

所有 token 字段继续保持 `NULL` 与真实 `0` 的差异。Store v3 已提供专用 parser checkpoint、generation、staging 和 diagnostic 表；`internal/indexer` 把一次 `ParseResult` 的全部事实、diagnostic、完整 `NextSeed`、projector state 与 `CommittableOffset` 交给同一个 writer transaction。事务成功后才采用新 offset/seed，失败时 stream 立即失效，调用方只能从旧 durable checkpoint 重新 `Open`。禁止从 `turn_usage`、`session_current` 或其他 mutable current projection 反推 seed：这些表会被后续 snapshot 覆盖，无法恢复 checkpoint 时刻的 provisional usage/pending/closed 状态。checkpoint persistence 只保存 typed safe fields，不保存原始 JSONL 或 content payload。parser 本身仍不写 SQLite，也不提前推进 `source_files.parsed_offset`。可复用验证入口见 [`docs/test/jsonl-parser.md`](../../../test/jsonl-parser.md) 与 [`docs/test/incremental-index.md`](../../../test/incremental-index.md)。

### 持久 Checkpoint 与 Generation

SQLite application schema v3 增加四张 `STRICT` 表：

| 表 | 责任 |
| --- | --- |
| `source_generations` | 保存 `(source_file_id, generation)`、`building/active/superseded`、完整 fingerprint、parser version、committed offset、session、active base token 和被 supersede building token。 |
| `parser_checkpoints` | 保存 versioned typed parser seed 与 projector state；二者均为 content-free BLOB，由 GORM 读写并在解码后重新校验。 |
| `source_generation_batches` | 暂存 building generation 的有序 typed `FactBatch`；batch identity 包含完整 target snapshot 与严格单调 commit epoch，同 offset 的 metadata move 或 A→B→A target 往返不冲突。 |
| `parser_diagnostics` | 按 batch end offset、目标 fingerprint 与 ordinal 保存 allowlisted class/code/offset/retryable，不保存 raw error、type 或 source bytes。 |

projector checkpoint 除 `session_current` 与 session counter previous 外，还保存 canonical Session source kind，以及最多 64 个 open turn 的安全投影：turn/session ID、start time、generation、start offset、model、cwd 和 reasoning effort。parser seed 的 source kind 表示当前物理文件位于 `sessions` 还是 `archived_sessions`，canonical Session source kind 则保持首次权威事实；二者分离后，archive move、move 后增长与 parser rebuild 不会制造稳定身份冲突。parser seed 本身不含 start source offset；没有独立 open-turn 状态，building generation 在进程重启后无法用稳定 key 完成 open turn。Store 会逐项核对 parser seed 与 projector open state，并要求所有 session-scoped facts 匹配 checkpoint Session；ingest 中携带 `TurnUsage` 的每个 `FactBatch` 还必须同时提供可解析的 Session 上下文，不能用 Usage-only batch 绕过该绑定。Session ID 与 canonical source kind 一旦在 generation 内建立就不得漂移，任何 ID、start time、context 或 generation 漂移都 fail closed。

状态机固定为：

```text
absent -> building(0, offset=0) -> active(0, offset=N)
active(G, N) + append -> active(G, N')
active(G, N) + truncate/replace/parser upgrade
    -> building(G+1, 0..N')
    -> active(G+1, EOF) + superseded(G)
building(G, N) + target/parser drift
    -> CAS superseded(G) + building(G+1, offset=0)
```

active append 以旧 fingerprint 与旧 committed offset 做 CAS，facts、diagnostics、parser/projector checkpoint、generation fingerprint、`source_files` offset 和可选 job cursor 原子推进。distinct commit 的 `AtMS` 必须严格前进；完全一致 replay 使用原 batch identity。building generation 分批持久化安全 staging，但查询继续读取旧 active facts；文件继续增长、再次替换或 parser version 再变化时，Indexer 从全部 durable building cursor 中按 source/path/base lineage 恢复 `(source_file_id, generation, fingerprint, parser version)` token。恢复时 exact source identity 优先于较弱的 path/base lineage，使 crash 后并存的 sibling 能选中自身完成 EOF，并在同一事务清理 competitor。building CAS fingerprint 与 active base 分开：Store 只允许精确 `RowsAffected=1` 的 CAS supersede，然后创建新 generation 从 offset 0 重放；旧 stream 随即失效，stale token 不能覆盖新 building。即使没有 active snapshot，已提交到非零 offset 的 building 也能重启，并用空 chunk EOF 完成激活。

每个 rebuild building 同时固化 Prepare 时观察到的 active base `(source_file_id, generation, fingerprint)`，并独立保存物理 replacement predecessor。A(active)→B(building)→C 时，C 继承 authoritative base A、记录 immediate predecessor B；EOF 沿整条 predecessor chain 校验并把 A/B source 都置为 unavailable，而 active CAS 始终只针对 A。EOF transaction 必须先证明 base 仍为 active，再删除旧 Session aggregate（外键级联清理 turn/current/usage，并把 source/session cursor 置空）、按顺序应用全部 staging、CAS supersede 精确 base、激活新 generation。Session metadata 因此是 generation-authoritative replacement，允许 LastSeen 缩短、稳定字段改变或 A→B；空/全坏行快照也会激活并清除旧 Session，不会因 offset 仍为 0 被误当 replay。active generation 的非空 session ownership唯一，防止清理一个 Session 时误伤另一条 active source。任一阶段失败都整体回滚；旧 active facts 与旧 cursor 保持可见，building 从上一次成功 checkpoint 继续。两个 replacement 即使都基于同一个旧快照，只有第一个能完成 base CAS，后一个在 EOF fail closed。

session 累计计数器的 previous 只取自同一 checkpoint 的 projector state。同 generation 中任一双方已知的 counter 下降会开启 `counter_epoch + 1`；`NULL` 不伪造下降。新 generation 从 epoch 0 以 `rebuilt` 状态开始。

## 数据层次

| 层级 | 含义 | 典型对象 |
| --- | --- | --- |
| 外部事实源 | Codex 维护，Tracker 不复制 | JSONL、`session_index.jsonl` |
| 持久事实 | 长期保留、可审计的结构化数据 | `sessions`、`turns`、`turn_usage`、`quota_observations` |
| 当前与聚合投影 | 可重建，服务 UI 快速查询 | `session_current`、`session_attributions`、`turn_attributions`、`quota_current`、daily usage |
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

`sessions.project_id`、`turns.project_id` 删除项目时置空；session 删除会级联清理 turn 和当前投影，turn 删除会级联清理 usage。`session_current.active_turn_id` 只能引用同一 session 的既有 turn，删除 turn 时置空。一个 `FactBatch` 只允许包含同一 Session 的对象，Usage 的 Session 归属在事务内通过关联 Turn 读回核对；不一致返回 `ErrInvalidRecord` 并整批回滚。直接 typed Repository 更新 Usage-only fact 时只存在 stored turn 这一项归属；进入 source ingest transaction 后还必须由同一 `FactBatch` 的 Session、Turn 或 current projection 显式给出 Session，才能再与 checkpoint Session 核对。Repository 在写入前把缺失引用、跨 session active turn 和 stable identity 冲突归一为 `ErrInvalidRecord`，不把 SQLite driver 文本当业务错误 contract。canonical SQL 同时用 CHECK 保证核心 ID、必填文本和非空 optional 文本不接受空字符串，防止绕过 typed repository 后写入另一套语义。

来源文件实体由 TOO-248 的 `source_files` 建立；本卡在事实层使用 `(session_id, source_generation, start_offset)` 定位一条 turn。generation 只允许前进，同一 generation 下同一 turn 的 start offset 不可变化，且一个来源位置不能映射两个 turn。已完成 turn 只有在更高 generation 同时提供完整 completion tuple 时才切换来源位置；同一来源身份的 non-null 字段冲突返回 `ErrInvalidRecord`，provisional replay 不再改写已完成事实。这样 parser 即使重复扫描或旧 generation 乱序到达，也不会生成重复事实或回退当前来源位置。

application schema v3 已通过 append-only migration 重建 `turns`、`turn_usage` 与 `session_current` 的外键闭包，移除旧的 `complete_offset >= start_offset` CHECK；`start_offset >= 0`、`complete_offset >= 0`、completion tuple 原子性与时间顺序保持不变。migration v1/v2 的对象集合与 checksum 独立冻结，fresh、v1、v2 数据库都按版本顺序升级并保留 backup/rollback contract。terminal-before-start 已由 parser → projector → GORM transaction → restart/readback 集成测试覆盖。

`sessions` 只存身份和稳定 metadata。重复 `session_meta` 使用 first-known-wins 补充 optional 字段：首次已提交的非空值成为权威，后续不同的非空值返回 `ErrInvalidRecord`，不会覆盖既有事实；首次提交本身仍是明确的权威来源边界。`created_at_ms` / `first_seen_at_ms` 只向更早收敛，`last_seen_at_ms` 只向更晚推进。项目 metadata 只有 `updated_at_ms` 更大时才更新；同一更新时间的冲突 payload 被拒绝，旧扫描只能补充更早的 `created_at_ms`。

thread name、当前模型、最后活动和 active turn 属于可变投影，放在 `session_current`。thread name 使用自己的 `thread_name_updated_at_ms` 合并：缺失或更旧名称不清空/回退现值，同一字段时间的冲突名称返回 `ErrInvalidRecord`；其他 current 字段按整行 `updated_at_ms` 更新，同一时间的冲突 payload 同样拒绝。Session 完成后仍可恢复，因此不使用永久 `ended_at`。

不持久化 Session status。查询层派生最小活动态：

```text
source freshness 不可用或索引不完整 -> unknown
存在 completed_at_ms IS NULL 的 turn -> active
没有未完成 turn                    -> idle
```

`Done` 是 turn outcome；`Stale` 是来源 freshness。v0.1 不推断 Waiting、Blocked 或 Error。

### Session Index Repair

`session_index.jsonl` 是 Codex 自己维护的根级 append-only 索引，不进入 rollout parser、source generation 或事实表。Codex Pulse 只把 `session_current.thread_name + thread_name_updated_at_ms` 两字段均完整且 Session 外键存在的行作为 expected projection，并通过 GORM reader 按 `session_id` 排序读取。dry-run 把该完整 projection 的 canonical SHA-256、index 的 exists/device/inode/size/mtime/full SHA-256 和分析时间一起固化进 immutable plan；确认后在 preflight 已存在的 Store projection 或 index 物理版本漂移，必须在 audit、备份和文件写入前 fail closed，执行期间新发生的漂移则按下述写后 proof、reconcile 与 terminal transaction 协议失败收口。

repair 遵循 Codex 官方 append-order/latest-valid-row-wins 语义：缺失 ID 可以追加 expected entry；名称不同且 Store 字段时间严格更新时可以追加 stale correction；index 时间更新、相等或不可比较时只报告冲突。同 ID 多个有效行是正常 rename history，不执行 compaction、全文件重写或原行删除。官方 `SessionIndexEntry.updated_at` 是普通字符串，格式化失败时允许写入 `unknown`；因此该行仍参与 latest view，但不可用于证明 Store 更新，只能在同名时视为已对齐、异名时进入 conflict。parser 对文件、单行、UTF-8、JSON duplicate key、UUID 和必填字符串做有界校验：malformed、duplicate key、invalid UUID 或缺失/null 必填字符串属于上游也无效的行，只形成 content-free diagnostic 并跳过；空白或超过 4096 bytes 的名称、超过 1 MiB 的完整行仍可能被上游 `String` schema 接受，本地不得把它伪装成坏行后继续 repair，而必须让整个 Analyze fail closed。不可解析但存在的 `updated_at` 仍保留 latest entry 并记录 diagnostic；高密度空行使用增量切片，避免总文件限制内的行切片放大。

执行必须精确确认同一 plan ID，并复用现有 `job_runs` 记录 `maintenance` 阶段、进度、terminal state 和 error class，不新增 schema。Analyze 与 Verify 会先把 correction 编码为 canonical JSONL，并按现有 size 加最坏一个分隔换行验证 64 MiB 最终容量；不可执行 plan 不得进入 audit/backup。真正 append 前依次完成 Codex Pulse Pure Go SQLite online backup 与 index byte-for-byte `0600` backup；index 原本缺失时写空的 absence marker。备份位置固定为 `<db-dir>/backups/session-index/<job-id>/`，每层新目录、backup 发布和新建 index 都同步父目录，只有掉电后可恢复的目录项才报告成功。

写入只通过确认根目录 FD 对根级文件执行 no-follow、`O_APPEND`、canonical JSONL 和 `fsync`；缺失末尾换行时只追加分隔换行，不修改历史 bytes。Store projection 在 audit 前、双备份后、append 后和 reconcile 后重复核对同一 digest；最终确认 snapshot 的逐项精确比较与 `JobRunning -> JobSucceeded` 状态迁移必须位于同一 GORM writer transaction，succeeded replay 也必须先核对当前 projection。index append 以确认 bytes 加实际 payload 计算 expected final size/full SHA-256；写后任一外部 append、截断或内容漂移都会返回 `ErrPlanDrift` 并把 job 置为 failed，不能只因旧 plan 名称暂时成为 latest 而静默成功。跨进程 Codex writer 不共享锁，因此已发生的并发 append/correction 都按 append-only 保留，恢复入口仍是刷新 Store 后重新 dry-run，不做破坏性回滚。

同一 succeeded plan replay 只读 audit 并重新 reconcile，不重复追加。append 后进程中断时，启动恢复沿用现有 job interruption 语义；默认恢复入口是重新 dry-run，新 plan 必须基于当前 Store projection 和 index version。repair 不读取 Codex `state_*.sqlite`，不遍历或修改 `sessions/`、`archived_sessions/` 原始 JSONL。可复用验证入口见 [`docs/test/session-index-repair.md`](../../../test/session-index-repair.md)。

### Session、Project 与 Model 派生归因

application schema v4 在 canonical Session/Turn 事实之外新增两张可重算 `STRICT` 派生表：

| 表 | 主键 | 责任 |
| --- | --- | --- |
| `session_attributions` | `session_id` | opaque display title、可选 project/model identity 与 display、各维度 confidence/source/reason、rule version 和更新时间。 |
| `turn_attributions` | `turn_id` | 单个 turn 的可选 project/model identity 与 display、confidence/source/reason、rule version 和更新时间。 |

原始 `sessions.initial_cwd`、`turns.cwd/model` 与 `session_current.current_cwd/current_model` 仍是 parser 观察到的本机事实，不会因规则变化被改写。安全 attribution projection 只暴露 stable ID、受限 display、固定 confidence/source/reason 和 rule version；它没有 `root_path`、`initial_cwd`、`cwd`、`current_cwd` 或 raw model 字段。后续 M6 query/Wails 层只能消费该安全 projection，不能直接 JSON 序列化 core Store record。需要展示原始路径时必须另设受控、本机、显式 reveal capability，不得扩张 attribution DTO。

Session title 当前只使用 `session_id_fallback`：对 Session ID 做版本化 SHA-256 后显示 `Session <opaque-short-id>`，不读取 prompt、response、reasoning、tool output 或 thread 内容。Project identity 的规则为：

1. 只接受规范化绝对 cwd；相对路径或非法路径返回 `unknown/invalid_path`。
2. Store transaction 内读取 `projects.root_path`，通过 `filepath.Rel` 做 segment-aware 最长 root 匹配；唯一命中为 `high/registered_root`，同长度不同 identity 为 `low/conflict`。
3. 没有登记 root 时使用 `local-path-v1:<sha256(normalized-absolute-path)>` 建立 `medium/cwd_path_digest` 本机 identity；basename 仅作为受限 display，绝不作为唯一键。自动生成的 path-digest Project 不升级为跨 Session registered root；它只在当前 Session 内让 initial cwd 与其 nested turn/current 对齐，避免归因受 Session 写入顺序影响。
4. 路径移动会生成新的本机 identity；v0.1 不读取 `.git`、remote 或仓库内容，也不猜测跨路径、跨机器项目合并。

Model 只接受最长 128 bytes 的小写字母数字、`.`、`_`、`-` token，归一化已知 `openai/`、`openai:`、`codex/`、`codex:` prefix 和大小写 alias。安全但未知的未来 token 可作为 normalized key/display；路径形、内容形、超长或非法字符输入返回 `unknown/invalid_model`，不会回显原文。Session current 与最新 started turn 是同等级候选；project 或 model 不一致时 fail closed 为 nil identity 加 `low/conflict`，不会用 last-write-wins 隐藏冲突。高等级 project 都缺失时才使用 `session.initial_cwd` fallback；`model_provider` 不冒充具体模型。

`session_attributions.session_id` 与 `turn_attributions.turn_id` 分别对 canonical Session/Turn 使用 `ON DELETE CASCADE`。Project identity/display 是一对原子派生快照，不对 `projects` 建单列外键：单列 `ON DELETE SET NULL` 会留下半个 tuple；当前 Project 没有删除业务入口，规则或登记 root 变化必须调用 typed `RecomputeAttributions`，在一个 writer transaction 中先清除派生行、清理未被 canonical facts 引用的 path-derived Project，再重建全部归因。任一步失败都会回滚，canonical Session/Turn/Usage/model/cwd 不变。

v4 为 session/turn attribution 分别建立 `(project_id, entity_id)` 与 `(model_key, entity_id)` 索引。`project_id/project_display_name` 和 `model_key/model_display_name` 必须同时为 `NULL` 或同时为非空字符串；SQLite `CHECK` 显式要求两边 `IS NOT NULL`，避免三值逻辑把单边 `NULL` 当作通过。归因 CRUD、查询和重算使用 GORM；raw SQL 仍只限 append-only migration 的 `STRICT/CHECK/index` 与测试 schema introspection。可复用隐私、冲突、重算、rollback 和 synthetic indexer 验证见 [`docs/test/privacy-attribution.md`](../../../test/privacy-attribution.md)。

v3→v4 migration 在创建 attribution schema 后，使用同一个 GORM writer transaction 从既有 canonical Session/Turn facts 全量回填 rule v1 归因；只有 DDL、全部派生行、migration ledger 与 `user_version` 一起成功才提交。backfill 任一步失败会整体回滚到 v3，不能留下“schema 已是 v4、历史归因为空”的状态。后续正常 FactBatch 只在 `WriteUnit` 内登记 dirty Session；callback 全部成功后、commit 前按 Session ID 排序，每个 Session 最多刷新一次，避免 Indexer 逐 Fact 激活时重复扫描历史 Turns。显式全量重算仍用于规则/登记 root 变化后的维护。

## Usage 与 Cost

- `turn_usage(turn_id, observed_at_ms, is_final, input_tokens, cached_input_tokens, output_tokens, reasoning_tokens, context_window, source_generation, source_offset, confidence, updated_at_ms)`
- `session_usage_current(session_id, counter_epoch, total_input_tokens, total_cached_tokens, total_output_tokens, total_reasoning_tokens, observed_at_ms, source_generation, source_offset, counter_state)`
- `pricing_versions(pricing_version, source, currency, effective_from_ms, created_at_ms)`
- `model_prices(pricing_version, match_kind, model_pattern, priority, input_micros_per_million, cached_input_micros_per_million, output_micros_per_million)`
- `pricing_catalog_metadata(pricing_version, source_url, verified_at_ms)`
- `cost_rollup_generations(generation_id, reporting_timezone, pricing_source, currency, rollup_version, state, created_at_ms, completed_at_ms, updated_at_ms)`
- `turn_costs(generation_id, turn_id, pricing_version, estimated_usd_micros, pricing_status, pricing_reason, calculated_at_ms)`
- `session_usage_rollups(generation_id, session_id, turn_count, input_tokens, cached_input_tokens, output_tokens, reasoning_tokens, total_tokens, estimated_usd_micros, priced_turn_count, unpriced_turn_count, first_activity_at_ms, last_activity_at_ms, updated_at_ms)`

同一 turn 的 `last_token_usage` 会多次更新，因此只 upsert 一条 `turn_usage`。`task_started` 创建 provisional 行，token snapshot 覆盖当前值，`task_complete` 将 `is_final = 1`。历史报表和日聚合只统计 final turn；Dashboard 可单独叠加 active turn 暂估，完成后由同一行替换，不能重复计费。

`session_usage_current` 只用于总量展示、单调性校验和 counter reset 检测，不参与日/周成本求和。累计值下降时开启新的 `counter_epoch`，不能生成负 delta。

所有 token 字段均可为 `NULL`：`NULL` 表示 source 没有提供该值，整数 `0` 表示已观测到真实零。typed repository 以 pointer 保留这个差异。`turn_usage` 按 `(source_generation, source_offset)` 接受严格更新，同 generation 较小 offset 返回 `ErrInvalidRecord`，final snapshot 不能被 provisional snapshot 覆盖；completed Turn 只接受 `is_final = 1` 的 usage，Turn-only completion 会清除既有的同 generation provisional current usage。completion 同批携带 usage 时先保留旧 ordering evidence 完成校验/upsert，再执行 cleanup，equal 冲突或 lower offset 会让 Turn completion 一并回滚。同一 batch 的 turn/usage generation 必须相等，且 usage generation 必须精确等于事务内实际采用的 turn generation，否则整批回滚。Turn 切换到新 generation 时删除旧 generation 的 current usage，typed query 也只连接同 generation snapshot，且不会向 completed Turn 暴露 provisional row；直到新 generation final usage 到达前保持 unknown。`session_usage_current` 按 `(source_generation, counter_epoch, source_offset)` 接受严格更新，新 generation 允许文件 offset 和重建后的 counter epoch 从低值重新开始。相同排序键只允许 payload 完全一致地重放，冲突值返回 `ErrInvalidRecord`，不使用 `>=` 做 last-arrival-wins。

金额统一使用整数微币种单位，不使用 SQLite `REAL`；v0.1 内置目录的币种为 `USD`，每个值表示每百万 token 的微美元。价格字段允许 `NULL` 表示目录没有给出该类别，非空整数 `0` 才表示真实免费。

Pricing Catalog 以 `(source, currency, effective_from_ms)` 形成不可变时间线。同 source/currency 的版本生效区间由相邻版本推导为 `[effective_from_ms, next_effective_from_ms)`，最后一个版本的结束时间为 `NULL`；新增版本不更新上一版本。通用查询允许 `exact`、`prefix`、`default`，但成本重建只接受 normalized model key 的 `exact` 规则，不能用相似名称或 fallback 猜价。没有生效版本记为 `unpriced/catalog_not_effective`；版本存在但没有 exact rule 记为 `unpriced/model_not_listed`。版本、全部规则和可选来源 metadata 在同一 writer transaction 追加；完全一致重放 no-op，同 version、metadata 或生效边界冲突全部回滚。

应用启动在 schema v5 完成后幂等安装 `openai-api-2026-07-14`：`USD`、`effective_from_ms=0`、verified time `1783987200000`，来源为 [OpenAI API Pricing](https://developers.openai.com/api/docs/pricing)。该 snapshot 只包含已有官方逐模型页面证据的 exact key；运行时不联网刷新。`gpt-5.2-codex-max`、`gpt-5.3-codex-spark`、Pro、日期 snapshot、长上下文和 Batch/Flex/Priority 倍率没有当前完整事实输入时保持 unpriced。金额是 API 等价估算，不是真实账单或 Codex 订阅额度。

单 turn 公式为：先精确求和 `input_tokens × input_rate + cached_input_tokens × cached_rate + (output_tokens + reasoning_tokens) × output_rate`，再统一除以 `1_000_000` 并只做一次 round-half-up；全程使用整数和 `math/big`，不使用 float。Codex JSONL 的 reasoning 是 output 之外的独立计数，因此两者都使用公开 output rate且不能重复包含。任一 token 为 `NULL` 时记为 `unpriced/missing_token`；某个正 token 类别缺少 rate 时记为 `unpriced/missing_price_component`；全零 token 可以得到真实的 priced `0`。模型归因缺失、冲突或非法分别保留 `missing_model`、`conflict_model`、`invalid_model`，完整 reason 集合由 STRICT CHECK 固定。

`RebuildCostLedger` 只读取 `turn_usage.is_final=1`，并在一个 `WriteMaintenance` transaction 中构建 shadow generation：写 turn cost 和四类 rollup、精确校验 generation/batch/state transition 的 `RowsAffected`，再用 GORM 从 shadow generation 逐表读回完整行并重新核对 turn/token/priced/unpriced/cost，之后才把旧 active 标为 superseded、把新 generation 切到 active。generation ID 与 request metadata 完全一致的重放 no-op；冲突重放、context 取消、SQLite 静默跳过/写后缺行、cost/token overflow 或对账失败都会整体 rollback，旧 active generation 保持可读。Pricing Catalog 更新后通过新的 generation 重算 cost，不修改 token 或 attribution 事实。

完整价格证据、公式、fixture、rollback、restart 与本地门禁见 [`docs/test/cost-ledger.md`](../../../test/cost-ledger.md)。

成本与 daily rollup 只以 `turn_attributions.project_id/model_key` 作为归一化维度入口，并保留 unknown/conflict/invalid 的未归因语义；不得直接把 `turns.cwd`、`turns.model` 或 basename 当作聚合键。known identity 使用 stable ID/key：同一日内 provenance 演进不拆分 identity，confidence 取最保守值，source/reason 不一致标记为 `mixed`，display 不一致则回退 stable identity；该合并与输入顺序无关。unknown dimension key 只由固定 confidence/source/reason 组成。raw model 仅留在本机事实层用于审计与规则重算，不进入 cost typed readback。

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
- `bootstrap_jobs(job_id, switch_id, home_generation, home_path, home_device_id, home_inode, data_store_key, strategy, plan_state, plan_sha256, phase_progress_current, phase_progress_total, eta_state, eta_remaining_ms, pause_reason, first_screen_ready_at_ms, reconcile_pass, reconcile_plan_at_ms, full_history_ready_at_ms, reconciled_at_ms, reconcile_change_count, reconcile_issue_count, updated_at_ms)`
- `bootstrap_plan_items(job_id, ordinal, pass, lane, tier, action_kind, previous_*, current_*, state, source_generation, progress_current, progress_total, updated_at_ms)`
- `process_snapshots(snapshot_id, session_id, pid, cpu, rss, ports_json, child_count, captured_at_ms, valid_until_ms)`
- `app_runtime_samples(captured_at_ms, cpu_percent, cpu_user_ms, cpu_system_ms, rss_bytes, goroutine_count, db_bytes, wal_bytes, disk_free_bytes, live_queue_depth, backfill_queue_depth)`
- `health_events(event_id, fingerprint, domain, severity, code, source_file_id, job_id, error_class, first_seen_at_ms, last_seen_at_ms, resolved_at_ms, occurrence_count, updated_at_ms)`

`source_file_id` 不依赖路径，稳定物理身份为 `(provider, device_id, inode)`；可选 `session_id` 只能引用既有 Session。文件移入 `archived_sessions` 时通过 metadata-only append commit 更新 generation path/source kind 与 parser seed，不重放 facts；canonical Session source kind 由 projector checkpoint 独立保留，后续增长或 rebuild 仍使用原始 Session 身份。相同 generation 的 `size_bytes`、`mtime_ns` 和 `parsed_offset` 不能倒退，且 offset 不得超过 size；新 generation 可以从较小 size/offset 重新开始，旧 generation 会被拒绝。TOO-249 提供唯一 writer queue/typed write unit，v3 ingest repository 在该边界内原子推进 facts、checkpoint、source 和 job cursor。

`source_state` 以 `(source_type, scope_key)` 绑定 stable identity，按 `updated_at_ms` 与 `cursor_version` 单调推进；due query 保留 `NULL next_due_at_ms` 与真实时间的差异。`source_attempts` 是 append-only 完成历史，`request_id` 完全一致时才允许幂等重放。`http_status=NULL` 表示没有 HTTP 响应，不能用 `0` 代替。`payload_sha256` 只接受固定 64 位小写十六进制 SHA-256 digest；调用方先对稳定结构化 identity 做摘要，持久层不接收 payload、prompt、response、tool output 或完整 JSONL 行。

Job state 只允许 `queued`、`running`、`succeeded`、`failed`、`cancelled`、`interrupted`，phase 只允许 `discover`、`fast_bootstrap`、`history_backfill`、`reconcile`、`live`、`maintenance`。Repository 允许 `queued -> running/cancelled/interrupted` 和 `running -> running/succeeded/failed/cancelled/interrupted`；phase、progress 和 update time 不能倒退，terminal row 不可复活。应用重启时把遗留 queued/running jobs 原子标为 `interrupted`；恢复只能经 `ResumeInterruptedJob` 新建 queued job，`resume_of_job_id` 必须指向 interrupted history，新 job 完整继承 type、priority、source、phase、progress 与 typed `JobCursor{Generation, Offset}`，且 created/updated time 不早于旧 terminal ordering key。公共 `CreateJobRun` 拒绝直接写 resume lineage。

`bootstrap_jobs` 以同一个 `job_id` 扩展首次索引的 immutable Home/switch facts、计划摘要、阶段进度、readiness 和 reconcile 边界；`bootstrap_plan_items` 冻结 initial/reconcile action snapshot。`pass=0` 只属于 initial plan，reconcile 使用严格递增的正整数 pass；空 reconcile pass 也必须持久化 `reconcile_pass + reconcile_plan_at_ms`。恢复新建 attempt 时保留旧 pass 作为审计，只以最新 pass 的全部 item succeeded 且 `reconcile_issue_count=0` 闭合 full-ready。逐 source 的真实恢复位置仍由 `source_generations + parser_checkpoints` 负责，bootstrap progress 不得领先于该权威游标。

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
    -> 写入安全 diagnostics / replay receipt
    -> UPSERT sessions / turns / usage / current projections
    -> 写入完整 parser seed 与 projector checkpoint
    -> 更新 generation fingerprint / committed offset
    -> 更新 source_files parsed_offset 与可选 job cursor
    -> COMMIT
```

任何步骤失败都 rollback，事实、diagnostic、checkpoint、source offset 和 job cursor 一起保持旧值。文件截断、same-inode replace、new-identity replace 或 parser version 升级时，在持久 building generation 中分批构建派生事实；target/parser 再漂移必须用显式 building CAS token 开启下一 generation。EOF 时按持久 base token 原子替换 authoritative Session aggregate 并切换 `active_generation`；旧 generation 随精确 CAS 标记为 `superseded`。

TOO-247 的 `FactBatch` 在 TOO-246 提供的唯一 writer queue 上按 `projects -> sessions -> turns -> turn_usage -> session_current -> session_usage_current` 写入；任一校验、外键或 SQL 步骤失败都会回滚整批。`Session`、`Turn` 与 `ListTurns` typed query 只从已提交事实读取，列表查询支持 session、项目、模型、来源位置和起始时间范围过滤，并与可选 usage 一次 join 返回。

## Daily 聚合

- `usage_daily(generation_id, bucket_start_ms, reporting_timezone, ...rollup totals)`
- `project_usage_daily(generation_id, bucket_start_ms, reporting_timezone, dimension_key, project_id, project_display_name, attribution_confidence, attribution_source, attribution_reason, ...rollup totals)`
- `model_usage_daily(generation_id, bucket_start_ms, reporting_timezone, dimension_key, model_key, model_display_name, attribution_confidence, attribution_source, attribution_reason, ...rollup totals)`

所有 daily 行绑定同一个 `generation_id`；主键分别为 `(generation_id, bucket_start_ms)` 和 `(generation_id, bucket_start_ms, dimension_key)`。request 必须显式传入 IANA reporting timezone；当前默认调用口径为 `Asia/Shanghai`。turn 归属日期使用 final `turn_usage.observed_at_ms` 在该时区的本地日，持久化的 `bucket_start_ms` 是本地午夜对应的 UTC epoch milliseconds，因此 DST 重复或跳过小时不会用固定 offset 猜算。

每个 token component 只要任一成员为 `NULL`，该 rollup component 与 `total_tokens` 就保持 `NULL`；其他完整 component 仍可独立求和。至少一个 priced turn 时 `estimated_usd_micros` 保存 priced subtotal，并同时保留 unpriced count；完全没有 priced turn 时金额为 `NULL` 而非零。周、月、年从 active generation 的 daily 表 `SUM`，不维护 weekly/monthly 表。修正 parser、项目归属、模型或价格时直接创建新的 shadow generation，不在 active 行上原地做减加。

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
| maintenance 队列 | 固定容量 1 | 只在没有普通写等待时执行；满时返回 `ErrQueueFull`，不扩展为第二个 writer |

writer transaction 使用 `BEGIN IMMEDIATE`，由唯一 worker 创建、提交或回滚；普通 `Write` 在容量 128 的 FIFO lane 中保持顺序，`WriteMaintenance` 使用独立的单槽 lane。worker 在 maintenance transaction 开始前优先处理已经排队的普通写，持续普通流量允许 maintenance 饥饿；maintenance 一旦开始则和普通写一样拥有一个有界事务，不存在并行的第二个 writer。底层 callback 得到绑定当前 `database/sql.Tx` 的 `*gorm.DB`，不能自行 `Commit` / `Rollback`。跨 core/runtime 写入使用 `Repository.WithinWriteUnit`，其 `WriteUnit` 只暴露 typed operations，callback 返回后立即失效；现有单操作 Repository 方法复用同一事务体。读 callback 得到绑定 read-only pool 的 GORM session，底层 DSN 同时使用 `mode=ro` 与 `query_only=ON`，防止把只读入口变成旁路写入。

空 Path 表示 Store 自己管理上述默认专用目录，可以收紧既有默认目录和 DB 权限。显式 Path 的既有父目录与 DB 仍归调用方所有：Store 只接受已分别为 `0700` / `0600` 的路径，不会静默 chmod 共享目录或文件；若显式路径的最终目录尚不存在，Store 才创建新的私有目录。

context 取消在入队前可以直接拒绝。job 一旦被队列接受，`Write` 必须等待 worker 的 authoritative result：排队取消会在开始事务前返回取消，执行中取消会 rollback；如果 Commit 已经赢得竞争并成功，则调用方收到成功而不是猜测性的取消，避免“返回已取消但事实已落盘”后重试产生重复数据。

`Close` 的固定顺序是：

```text
原子切换为 closing 并拒绝新读写
    -> 先普通写、后 maintenance 地排空切换前已接受的两个 queue
    -> 等待在途 read callback 返回
    -> 关闭 read pool
    -> 关闭 writer connection
    -> 切换为 closed 并唤醒所有 Close 等待者
```

调用方取消等待不会中止已经开始的关闭。队列满、context 取消、closing/closed、SQLite busy/locked、disk full、read-only、permission、I/O 和 corrupt 都有稳定 sentinel；底层 context 与 driver error 仍保留在 error chain 中，禁止依赖 `database is locked` 等字符串分支。

应用打开 Store 后、将其暴露给任何 runtime reader/writer 之前，由显式 migration catalog 管理 `PRAGMA user_version` 与 `schema_migrations(version, name, checksum, applied_at_ms)` append-only ledger。`MigrateApplicationSchema` 只属于该启动期 bootstrap 契约；若未来需要运行期 maintenance migration，必须先由上层实现任务排空和 Store 独占，不得直接复用启动调用。catalog 必须从 1 连续、名称和 SHA-256 checksum 稳定；history 缺号、checksum drift、ledger/user_version 分叉或数据库版本高于二进制都会 fail closed。所有 pending migration 在同一个 writer transaction 中顺序执行，只有全部 schema readback 和 ledger 写入成功后才推进 `user_version`，任一步失败会连同本轮所有 pending objects 一起回滚。当前 v1 固定为初始 core/runtime schema，v2 只追加 retention query indexes，v3 增加 durable incremental-ingest checkpoint/generation 并重建 turn 外键闭包，v4 增加 Session/Turn project/model attribution，v5 增加 pricing metadata、cost generation、turn cost 与 session/global/project/model rollup，v6 增加首次索引 bootstrap job/plan、阶段进度、readiness 与多 pass reconcile 状态；v1-v6 checksum 均由各自冻结测试保护，新增 migration 不得重算或改写既有 history。

fresh 空库不做无意义备份。已有用户 schema 且存在 pending migration 时，runner 先只读检查状态和可用空间，再通过 modernc `NewBackup` / `Step` / `Remaining` / `PageCount` 创建包含 committed WAL 页的 `0600` 快照，成功发布后才进入 migration transaction；失败或取消只清理 `.partial-*`。文件级恢复原语使用 `NewRestore`，只恢复到尚不存在的新文件，不在运行中覆盖当前 Store。`STRICT`、复杂 `CHECK`、特殊 index、连接 PRAGMA、`sqlite_schema` canonical readback 和 `EXPLAIN QUERY PLAN` 是隔离的 raw SQL 例外；普通 CRUD、filter、关联检查和事务编排使用 GORM。

当前核心索引为：

- 唯一 `turns(session_id, source_generation, start_offset)`，用于来源去重和定位。
- `turns(session_id, started_at_ms DESC, turn_id DESC, completed_at_ms)`，用于 session 时间列表、稳定排序和生命周期覆盖。
- `turns(project_id, started_at_ms DESC, turn_id DESC, completed_at_ms)` 与 `turns(model, started_at_ms DESC, turn_id DESC, completed_at_ms)`，用于项目/模型时间列表并避免额外临时排序。
- `session_current(last_activity_at_ms)`，用于最近 session 投影。
- `turn_usage(observed_at_ms, is_final)`，用于按观测时间筛选 final usage。
- `session_attributions(project_id, session_id)` 与 `session_attributions(model_key, session_id)`，用于安全 Session project/model 维度查询。
- `turn_attributions(project_id, turn_id)` 与 `turn_attributions(model_key, turn_id)`，用于后续 cost/rollup 消费 normalized 维度。

当前 runtime 索引为：

- `source_files(session_id, state, last_scanned_at_ms, source_file_id)` 与 `source_state(next_due_at_ms, source_instance_id)`，用于 session/state 扫描和 due source。
- `source_attempts(source_instance_id, started_at_ms DESC, request_id DESC)`，用于来源尝试历史。
- `job_runs(state, updated_at_ms, priority DESC, job_id)`、`job_runs(source_file_id, created_at_ms DESC, job_id DESC)` 与 `job_runs(updated_at_ms, priority DESC, job_id)`，用于 startup recovery、队列、source 作业历史和无 filter 列表。
- `health_events(resolved_at_ms, last_seen_at_ms DESC, event_id, severity)`、`health_events(last_seen_at_ms DESC, event_id)`、`health_events(severity, last_seen_at_ms DESC, event_id)`、`health_events(source_file_id, last_seen_at_ms DESC, event_id)` 与 `health_events(job_id, last_seen_at_ms DESC, event_id)`，用于 active/resolved/history/severity 和 source/job 单关系追溯。
- `pricing_versions(source, currency, effective_from_ms DESC)` 与 `model_prices(pricing_version, priority DESC, match_kind, model_pattern)`，用于 as-of 版本和模型规则匹配。
- `cost_rollup_generations(reporting_timezone) WHERE state='active'` 保证每个时区至多一个 active；turn/session/global/project/model cost 索引分别支持按实体、日期与安全 dimension 读取同一 generation。
- v2 retention 专用索引为 `health_events(resolved_at_ms, event_id)`、`job_runs(state, finished_at_ms, job_id)`、`job_runs(resume_of_job_id, job_id)` 与 `source_attempts(finished_at_ms, request_id)`；terminal job 按固定 state 顺序逐段选择，以同时保持有界、确定排序和索引命中。

上述 required query 由 GORM model/scopes 构造；测试文件维护等价的代表查询用于 `EXPLAIN QUERY PLAN`，要求命名索引被选择且不得出现临时排序。quota、process snapshot 和 app runtime sample 索引由对应后续 Execution 卡创建。core/runtime 表不提供 prompt、response、tool output、原始 JSONL、鉴权 token 或 raw error 的专用字段；schema contract 和数据库 bytes 测试会对受控 code/digest/cursor/error 输入面使用 synthetic marker 验证拒绝与不可见性。业务 identity/path metadata 的凭据卫生仍由调用方负责。

## 保留策略

v0.1 的窗口固定为 24 小时，不提供用户可配置天数。cutoff 是 `now - 24h` 的 UTC epoch milliseconds；只有时间严格小于 cutoff 的行可删除，恰好等于 cutoff 的行仍保留。

| 数据 | v0.1 策略 | 开始计时字段 / 条件 |
| --- | --- | --- |
| session、turn、final usage、turn cost、quota observation、daily rollup | 长期保留 | cleanup 不触碰 |
| `source_attempts` | 滚动 24 小时 | `finished_at_ms < cutoff` |
| `job_runs` | 仅 `succeeded` / `failed` / `cancelled` 滚动 24 小时 | `finished_at_ms < cutoff`，并且没有剩余 health event 或 resume lineage 引用 |
| `health_events` | 仅已解决事件滚动 24 小时 | `resolved_at_ms IS NOT NULL AND resolved_at_ms < cutoff` |
| `process_snapshots`、`app_runtime_samples` | 设计上滚动 24 小时 | 当前 schema 尚未创建，TOO-250 不提前落表或伪造 cleanup |
| current projection | 可重建 | 由各投影自己的后续实现管理，本轮不删除 |

`queued`、`running` 与 `interrupted` job 都不清理；`interrupted` 是可恢复终态，不能因已有 `finished_at_ms` 被误当成普通已完成历史。未解决 health event 永久越过当前 cleanup；已解决但仍在窗口内的 event 继续阻止关联 job 删除。任一 resume child 仍引用 parent 时，parent 也保留，不能依赖 `ON DELETE SET NULL` 抹掉解释和恢复血缘。

`Repository.CleanupRetentionBatch` 通过低优先级 maintenance lane 在一个 GORM transaction 中最多删除 `BatchSize` 行；默认 100、硬上限 1000，batch size 只控制事务工作量，不改变 24 小时产品语义。总 budget 按以下顺序消费：

```text
eligible resolved health_events
    -> eligible terminal and now-unreferenced job_runs
    -> eligible source_attempts
```

候选通过 GORM condition/subquery、稳定时间与主键排序、`Limit` 和 `Pluck` 选择，再由 GORM `Delete` 删除；production retention 不使用 `Raw` / `Exec`。实际 GORM DryRun SQL 由真实 `EXPLAIN QUERY PLAN` 验证专用索引、health/job 引用索引和 resume lineage 索引均命中，且候选排序不使用临时 B-tree。每批独立提交，只有 commit 成功后才对外累加计数；任一批删除后都重新查询 `More`，因为删除 terminal resume leaf 会在同一事务中释放 parent，即使 batch budget 尚有剩余也必须继续下一批。`CleanupRetention` 在批间调用 observer；context 取消会返回稳定的 SQLite cancel error 和已提交 report，下次调用重新按条件计算，不保存游标，也不会回滚先前已经成功提交的批次。当前卡只交付 Store/service contract；启动与小时级 scheduler 接线留给对应调度 Execution。

任何清理都不能删除 Codex 原始 JSONL，Tracker 本来也不拥有这些文件。完整 fixture、取消、rollback、close/reopen 与 Pure Go验证入口见 [`docs/test/store-integration.md`](../../../test/store-integration.md)。
