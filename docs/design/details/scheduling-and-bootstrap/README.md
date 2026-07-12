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

Codex home 探测优先级：`CODEX_HOME`、`~/.codex`、用户手动选择。只检查目录可读性、`sessions/`、`archived_sessions/`、`session_index.jsonl`、`auth.json` 是否存在，以及 JSONL 文件数量和总字节数；不在 Codex home 内创建、修改、修复或移动文件。

v0.1 只支持一个 Codex home。更换路径时，用户必须明确选择“新建独立数据库”或“清空当前派生索引后重建”，不能静默混合多个 home。

首次隐私说明展示检测路径、Tracker 数据库位置、读取范围、不保存内容，以及“在线 quota 与 reset credits 默认开启、可随时关闭”。建议文案：

> Codex Pulse 将只读本机 Codex session 和索引文件，并把 token、项目、模型、配额等结构化数据保存到本机 SQLite。在线 quota 与 reset credits 默认开启，会临时读取当前 access token 请求 ChatGPT 内部只读接口；凭证不会写入数据库或日志。你可以在开始前或之后随时关闭这两项能力。

操作为“开始”“选择其他目录”“退出”，并在同页提供在线 quota 与 reset credits 两个默认开启的开关。本地索引确认和在线能力偏好分别持久化。

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

## 空状态与恢复

- 未找到 Codex home：选择目录、重新检测、打开设置。
- 目录存在但无 Session：显示已连接并保持 watcher，不视为错误。
- JSONL 部分损坏：跳过坏行或坏文件，记录 path、offset 和错误类型，不保存正文；支持单文件重试。
- 权限不足：展示不可读路径和系统授权说明，不自动修改权限。
- 磁盘不足：事务 rollback、不推进 offset、暂停后台写入。
- SQLite 无法打开：重试、打开数据目录、查看安全日志、备份后显式重建。
- migration 失败：只读安全模式，停止索引，保留查看数据、检查更新和恢复备份。

独立 preferences 保存 `onboarding_version`、`onboarding_completed`、`codex_home`、`online_quota_enabled`、`reset_credits_enabled` 和 `initial_index_started_at`；新安装的两个在线能力默认值为 `true`，升级不覆盖已有显式值。事实、进度和游标仍以 SQLite 为准。

首次启动验收：无网络/无 auth 可完成本地初始化；主窗口不等待全量历史；部分数据明确标注；退出可续传；live append 不被 backfill 饿死；坏文件不阻塞全局；重建数据库和更换 home 必须显式确认。
