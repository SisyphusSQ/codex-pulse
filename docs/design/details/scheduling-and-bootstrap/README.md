# Scheduling and Bootstrap

## 设计目标

不同数据源独立调度；后台低资源推进，前台交互优先。用户刷新只追平增量，不与后台启动重复任务，也不隐式全量重扫。

适用机器：`sqmc04`。2026-07-11 读回 Codex Runway `0.0.16` 的刷新间隔为 300 秒，源码默认同为 300 秒，可配置 60～1800 秒。Runway 启动、每个周期和 reset 后会做全量 refresh，成本摘要还可能枚举 JSONL 并整文件读取；Codex Pulse 不沿用这个单一全量刷新模型。

## 数据源周期

| 数据 | 后台刷新 | 前台打开或手动刷新 |
| --- | --- | --- |
| 5h / weekly quota | 正常 5 分钟；低于 20% 或临近 reset 时 2 分钟 | 上次成功超过 60 秒则立即获取 |
| reset credits | 30 分钟 | 打开对应页面时立即获取 |
| JSONL 增量索引 | 文件变化后 debounce 3～5 秒 | 立即追平新增字节 |
| Session 完整目录对账 | 30 分钟 | 仅显式重建索引时满速 |
| 进程 / 端口状态 | 30～60 秒 | 前台 5～10 秒 |
| 活跃项目 Git 状态 | 2～5 分钟 | 打开项目时立即获取 |
| Pricing | 每天或手动 | 手动 |
| 应用更新 | 默认每小时、最低优先级 | 设置页或菜单手动检查 |
| Session index repair | 不自动 | 仅显式 dry-run |

Quota 网络失败使用 5、10、20、30 分钟带 jitter 退避；手动刷新绕过退避但不绕过校验。系统从休眠恢复后，只立即刷新 stale 来源。reset 到达后在 `reset + 3 秒`尝试一次，失败继续退避，不推断已经重置。

## 任务优先级和预算

- `interactive`：用户打开页面或手动刷新，优先返回 quota 等轻量结果，并满速追平已有增量。
- `background`：日常采集，单 worker、限制读取和事务批次，在工作切片间主动 yield。
- `maintenance`：目录对账、索引重建、SQLite checkpoint/vacuum，只在空闲或显式触发时执行。

后台初始预算：单 worker、2～4 MiB/s、每批 4～8 个文件、工作 50 ms 后 yield 100～200 ms、每事务 100～250 行。前台可以使用 2 个 worker、取消主动读限速、每事务 500～1000 行。

Go 侧使用协作式 `ScanBudget` 和 worker pool，不动态修改整个进程的 `GOMAXPROCS` 或 nice，避免连 UI 和 SQLite 查询一起降速。

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

## 初始索引阶段

1. `discover`：枚举和 stat 文件，记录 path、size、mtime，得到 total files/bytes。完成前不显示虚假百分比。
2. `fast_bootstrap`：优先读取 session index、活跃 JSONL 尾部 rate limits、今天和最近修改的 Session，尽快展示 quota、今日初步 usage、active turn 和项目。
3. `history_backfill`：其余文件按最近 7 天、30 天、更早历史的顺序处理；文件之间最近优先，单文件内部始终从头到尾。
4. `reconcile`：校验 turn/token 总数、project/model rollup 和 cursor，通过后标记初始索引完成。

尾部快读必须记录真实 byte offset，后续完整索引用既定幂等键去重。Discover 为每个文件记录初始 size，进度只计算这份快照；期间新 append 进入 live queue，不能让进度倒退。

调度器维护：

- `live queue`：活跃文件的新 append，始终优先；
- `backfill queue`：未完成历史文件，按 background ScanBudget 限速。

用户可暂停/继续 backfill，或显式切到前台满速；暂停历史时 live queue 继续。另提供“暂停所有采集”。低电量模式可降低 backfill 速度，v0.1 不要求自动完全暂停。

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

本 slice 不修改 SQLite，也不推进 `parsed_offset`：TOO-251 只交付 snapshot 与 reconcile plan。TOO-253 负责用 versioned migration 持久化 fingerprint checkpoint，并把 plan、`source_files`、generation、事实替换和 offset/job progress 放进原子写入边界。在该接线完成前，不得宣称 discovery 已具备跨进程 cursor 恢复。

## 空状态与恢复

- 未找到 Codex home：选择目录、重新检测、打开设置。
- 目录存在但无 Session：显示已连接并保持 watcher，不视为错误。
- JSONL 部分损坏：跳过坏行或坏文件，记录 path、offset 和错误类型，不保存正文；支持单文件重试。
- 权限不足：展示不可读路径和系统授权说明，不自动修改权限。
- 磁盘不足：事务 rollback、不推进 offset、暂停后台写入。
- SQLite 无法打开：重试、打开数据目录、查看安全日志、备份后显式重建。
- migration 失败：只读安全模式，停止索引，保留查看数据、检查更新和恢复备份。

独立 preferences 的 onboarding base snapshot 保存 `schema_version`、`onboarding_version`、`onboarding_completed`、confirmed `codex_home` physical identity、`online_quota_enabled` 和 `reset_credits_enabled`；新安装的两个在线能力默认值为 `true`，升级不覆盖已有显式值。`initial_index_started_at` 只有在后续 bootstrap 真正启动时才由 typed Preferences/migration 协议增加，onboarding 不提前伪造。每次启动先 `Load` 并用 metadata-only probe 复核保存的 canonical path/device/inode；source replacement 返回 `source_changed`，不授予 indexing。事实、进度和游标仍以 SQLite 为准。

首次启动验收：无网络/无 auth 可完成本地初始化；主窗口不等待全量历史；部分数据明确标注；退出可续传；live append 不被 backfill 饿死；坏文件不阻塞全局；重建数据库和更换 home 必须显式确认。
