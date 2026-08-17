# Agent Provider 与 Cursor

## 产品与代码语义

Codex Pulse 以一个明确的客户端上下文查询和展示数据。产品 UI 使用“客户端”；代码使用 `AgentProvider` / `ProviderScope`，避免与 OpenAI、Anthropic、Grok 等模型供应方混淆。当前客户端只有 `codex` 和 `cursor`，没有跨客户端“全部”视图。

这是一条窄而明确的扩展缝，不是通用插件框架：Codex 原有 parser、collector、quota 与 pricing 路径保持不变；Cursor 使用独立 collector、snapshot 表和查询实现。Core RPC 的使用量、调用、Session 与 Project 请求显式携带 Provider scope，响应回显 effective provider、source、capabilities 和 coverage。Overview 也按客户端路由，不把 Codex account/quota 与 Cursor usage 合并。

## Cursor 来源边界

Cursor 在业务页面中始终是一个完整客户端。Go Helper 在内部合并以下来源：

| 来源 | 用途 | Checkpoint | 失败语义 |
| --- | --- | --- | --- |
| Local transcripts | Session、Project、时间、模型与工具白名单元数据 | `filesystem_scan` | 缺失、不可读或解析不完整时降级 |
| composer/state SQLite | generation/request、可用的精确 Token | `snapshot` | schema gate、WAL snapshot 或读取失败时 unavailable |
| conversation-search SQLite | 会话存在性与来源健康 | `snapshot` | 不读取 FTS conversation body |
| AI Code Tracking SQLite | 官方 request ID、model、时间与编辑归因 | `snapshot` | 不读取 tracked file content |
| Hooks | 未来实时来源 | `not_configured` | 当前明确显示未配置 |
| DashboardService | 个人账号官方 usage event、精确 Token、reported charge | `snapshot` | 复用 Cursor Desktop Bearer；鉴权过期、协议漂移或网络失败时保留 last-good 并标记 unavailable |

活跃 SQLite 使用只读连接、`query_only`、schema/version gate 和一致 read transaction；不得裸复制单个数据库文件。每类来源保存自己的 source instance、coverage、health 与 checkpoint 语义，SQLite/API snapshot 不进入 Codex JSONL 的 `source_generations` byte-offset 状态机。

DashboardService 使用 Cursor Desktop 已登录态在 `state.vscdb` 中维护的 access token。Helper 只在内存中读取并校验 JWT 有效期，按 Cursor Desktop 自身的 Bearer RPC 方式访问固定的 `api2.cursor.sh/aiserver.v1.DashboardService`；token 不进入 Codex Pulse SQLite、preferences、日志或 RPC。Dashboard 返回的 owner/email 等展示字段在解析边界即丢弃。

Cursor 页面查询始终先刷新并读取本地 snapshot，再以 single-flight 后台任务按最小刷新间隔请求 DashboardService。远端成功或失败状态提交后只发送 query invalidation，Swift 随后重查本地 snapshot；网络延迟不阻塞首屏或菜单栏展开。账号胶囊同样不依赖 DashboardService：Helper 只从本地 `state.vscdb` 白名单读取 `cachedEmail`、`stripeMembershipType` 与订阅状态，绝不返回 token、refresh token 或 profile 原文。

## 身份、合并与持久化

application schema v22 新增 Provider snapshot/source 表和 Cursor 本地事实表；v23 增加独立 Dashboard snapshot/usage event 表；v24 在保留 v23 last-good usage event 的前提下增加账期与 plan spending 摘要；v25 为 Cursor Session 增加安全的 `display_title` 与 `title_source`。Cursor Session 使用内部 surrogate key，并以 `(provider, external_session_id)` 唯一；所有查询身份都包含 Provider。

同一个 `conversation_id` 可能出现在多个 project bucket。collector 以 `provider + external ID` 合并业务 Session，同时保留每条来源 lineage 的脱敏 key 与 content digest；digest 不一致时标记 conflict，不按路径重复计数，也不采用 largest-wins。request 只按稳定的 `generation_id` 或官方 usage event 去重。AI edits 按官方 request ID 去重；来源间 model/conversation 冲突时不任取一份。

空的 draft/ephemeral composer 不单独成为业务 Session；正式 composer，或具有 transcript、request、usage 证据的 conversation 才进入列表。标题按 `composerHeaders.name`、`conversations.title`、带日期的“未命名会话”顺序选择，并回显 `cursor_composer_header`、`cursor_conversation_search` 或 `fallback` 来源及对应置信度。项目内部身份优先使用带 Cursor namespace 的 `workspaceIdentifier.id` hash；显示名只保留 workspace/repository path 的 basename。完整路径只在 collector 内存中用于派生，写盘前丢弃；相同 basename 的不同 workspace 仍由不同 project key 分组。

Snapshot 采用事务性全量替换：先验证所有白名单记录，再在一个 writer transaction 中更新 generation、来源、Session、lineage 与事件。取消或失败不会覆盖上一份成功快照。

## 精确度与 capability

- Codex 保留 quota、reset、account/plan、Token、API-equivalent cost、Session 与 Project 能力。
- Cursor 提供 Session、Project、Model、Tool、AI edits 和今日请求数；只有来源提供稳定精确事件时才提供 Token。
- Cursor usage model 按官方事件中的 model key 分组，并为每个模型返回与总趋势相同粒度的 bucket；UI 只有在模型 bucket 无法与总量核对时才使用诚实的聚合降级，不把可识别模型压成“全部模型”。
- 今日使用 App reporting timezone 的 `[00:00, now)`。
- 只要观察到的请求存在缺失或冲突 Token，相关 Token 聚合就是 unknown；Cursor state 中 request 对应的 `0/0` tokenCount 是未提供值，不作为精确零持久化。不得按 transcript 长度、context usage 或其他指标估算。
- Cursor Dashboard 的 `charged_cents`、Cursor Token fee 与 plan spending 作为 reported 数据分别回显，不进入 Codex/OpenAI pricing catalog。按 Cursor 官方 Models & Pricing 固定版本计算的费用只标记为 estimated；reported 与 estimated 在 contract 和 UI 中分开展示。
- Dashboard 刷新失败时，当天已有成功快照继续作为 last-known 返回，响应标记 `partial` 并回显 `dataAsOfMs`；跨日后旧快照不冒充今日数据。
- UI 按 capability 隐藏不适用模块，不能用一屏 `--` 伪装支持。

## Swift 状态与竞态

主窗口 `selectedProvider` 与菜单栏 `statusProvider` 独立持久化，并分别由下拉框选择；Popover 的下拉框位于原产品标题位置。主窗口切换客户端时清空详情、分页和错误状态，推进 request generation 并取消旧页面任务，但不取消或重载菜单栏任务；只有 provider 与 generation 都匹配的响应才能落入对应 presentation state。Popover 使用独立的 `statusOverviewState` 请求完整状态快照，切换和刷新都不改变主窗口客户端，“打开主窗口”也只负责打开现有页面。连续 invalidation 期间状态栏刷新保持 single-flight，避免重复取消和重启同一批请求。

菜单栏在 Codex 下保持既有额度展示；Cursor 下用与 Codex 相同的图标加双行摘要显示“今日请求数 · 今日 Token”，Popover 则复用账号/套餐胶囊、额度卡片、趋势卡片和消费进度卡片的视觉结构，不再直接罗列内部来源字段，也不重复展示“今日活动”卡片。没有成功 Dashboard 快照和可靠本地 Token 时显示 `Token --`；有 last-known 时显示最后成功值，并在 Popover 标注数据截至时间。系统页可以同时观察所有客户端，但来源按 Codex/Cursor 分组，Cursor 内部来源只在该分组展开。

## 隐私与验证

Cursor collector 在写盘前丢弃 prompt、response、thought、tool input/output、FTS body、tracked file content、browser logs/cookies、邮箱、原始路径和未知字段。公共 DTO、日志与提交版证据不得包含绝对路径、原始 ID、内容或凭据。Desktop access token 只在单次请求内存中使用，不写入配置、日志、Codex Pulse 数据库或仓库。

单元、contract、CI 和 deterministic smoke 使用 synthetic/empty Codex 与 Cursor Home。`CODEX_PULSE_CURSOR_HOME` 只用于给这些进程显式绑定隔离的绝对测试根目录。真实产品验收可只读访问真实 Codex/Cursor 数据，但必须写入私有 mode `0700` runtime，并只保存脱敏聚合证据。
