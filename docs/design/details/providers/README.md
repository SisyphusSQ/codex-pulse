# Agent Provider、Cursor 与 Grok

## 产品与代码语义

Codex Pulse 以一个明确的客户端上下文查询和展示数据。产品 UI 使用“客户端”；代码使用 `AgentProvider` / `ProviderScope`，避免与 OpenAI、Anthropic、Grok 等模型供应方混淆。当前客户端是 `codex`、`cursor` 和 `grok`，没有跨客户端“全部”视图。Grok 在 UI 中的显示名固定为“Grok”，代码身份为 `grok`；它指 Grok Build TUI / CLI，不是 Cursor 或其它客户端里出现的 Grok 模型。

这是一条窄而明确的扩展缝，不是通用插件框架：Codex 原有 parser、collector、quota 与 pricing 路径保持不变；Cursor 与 Grok 各自使用独立 collector、事实表和查询实现。Core RPC 的使用量、调用、Session、Project、Quota 与 Account 请求显式携带 Provider scope，响应回显 effective provider、source、capabilities 和 coverage。Overview、菜单栏和账号胶囊都按客户端路由，不把 Codex、Cursor、Grok 的 account / quota / usage 合并，也不把 Cursor 内的 `grok-*` 模型用量并进 Grok 客户端。

空 `ProviderScope` 仍归一为 `codex`，以兼容旧请求；未知非空值必须失败，不得默认成 Codex 或 Cursor。Router、AccountSnapshot、PricingCatalog 和 Swift 展示必须显式三路分发，禁止 `if cursor else Codex` 把 Grok 漏进另一家客户端。

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

Cursor 页面查询优先读取已提交 snapshot，并在进程内按 generation 共享同一份只读快照；只有首次没有任何 snapshot 时才同步建立本地基线。已有数据时，本地 collector 与 DashboardService 都以 single-flight 后台任务按各自最小刷新间隔更新；成功提交后先失效内存快照，再发送 query invalidation，Swift 随后重查新 generation。全量本地扫描、SQLite 替换和网络延迟都不阻塞首屏或菜单栏展开。账号胶囊同样不依赖 DashboardService：Helper 只从本地 `state.vscdb` 白名单读取 `cachedEmail`、`stripeMembershipType` 与订阅状态，绝不返回 token、refresh token 或 profile 原文。

## 身份、合并与持久化

application schema v22 新增 Provider snapshot/source 表和 Cursor 本地事实表；v23 增加独立 Dashboard snapshot/usage event 表；v24 在保留 v23 last-good usage event 的前提下增加账期与 plan spending 摘要；v25 为 Cursor Session 增加安全的 `display_title` 与 `title_source`。Cursor Session 使用内部 surrogate key，并以 `(provider, external_session_id)` 唯一；所有查询身份都包含 Provider。

同一个 `conversation_id` 可能出现在多个 project bucket。collector 以 `provider + external ID` 合并业务 Session，同时保留每条来源 lineage 的脱敏 key 与 content digest；digest 不一致时标记 conflict，不按路径重复计数，也不采用 largest-wins。request 只按稳定的 `generation_id` 或官方 usage event 去重。AI edits 按官方 request ID 去重；来源间 model/conversation 冲突时不任取一份。

空的 draft/ephemeral composer 不单独成为业务 Session；正式 composer，或具有 transcript、request、usage 证据的 conversation 才进入列表。标题按 `composerHeaders.name`、`conversations.title`、带日期的“未命名会话”顺序选择，并回显 `cursor_composer_header`、`cursor_conversation_search` 或 `fallback` 来源及对应置信度。项目内部身份优先使用带 Cursor namespace 的 `workspaceIdentifier.id` hash；显示名只保留 workspace/repository path 的 basename。完整路径只在 collector 内存中用于派生，写盘前丢弃；相同 basename 的不同 workspace 仍由不同 project key 分组。

Snapshot 采用事务性全量替换：先验证所有白名单记录，再在一个 writer transaction 中更新 generation、来源、Session、lineage 与事件。取消或失败不会覆盖上一份成功快照。

## Grok 第一期范围

Grok 第一期交付一个完整独立客户端，与 Codex、Cursor 能力面对齐，而不是先做半套本地账本再补额度。范围内包括：

- 主窗口与菜单栏可独立切换到“Grok”
- 本地 Session、Project、Model、Token、Tool
- 账号胶囊
- 在线额度摘要与状态栏额度环
- reported / estimated 成本分开展示
- 本机状态里按客户端分组的来源健康

第一期不做：三家合并视图、与 Cursor 内 Grok 模型对账、Codex Home 那种两步切换、Reset Credits、AI edits、通用插件框架，以及把 `signals.json` 或 transcript 长度估成 Token。

## Grok 来源边界

Grok 在业务页面中始终是一个完整客户端。默认 Home 为 `${GROK_HOME:-$HOME/.grok}`；测试与 CI 只能通过绝对路径 `CODEX_PULSE_GROK_HOME` 绑定隔离根目录。第一期不把 Grok Home 写入 Preferences，也不做两步切换：目录缺失或不安全时只让 Grok 能力 unavailable，不得影响 Codex 或 Cursor。Go Helper 在内部合并以下来源：

| 来源 | 用途 | Checkpoint | 失败语义 |
| --- | --- | --- | --- |
| `summary.json` | Session 身份、安全标题、模型、时间、项目派生 | `filesystem_scan` | 缺失或不可解析时该 Session 不进入列表 |
| `updates.jsonl` 的 `turn_completed.usage` | 精确 Token、按模型拆分、可选 reported cost | `filesystem_scan` | 无 usage 事件时 Token/cost 为 unknown，不伪造零 |
| `updates.jsonl` 的 `tool_call` / `tool_call_update` | Tool 名称、次数与结果 | `filesystem_scan` | 只取 `tool_name` / `kind` / `status`；缺字段则 unknown |
| `session_search.sqlite` | 不采集 | `not_configured` | 禁止读取；FTS `content` 含 prompt 原文 |
| `auth.json` 白名单资料 | 账号胶囊的脱敏邮箱与身份类型 | 调用期内存 | 缺文件或不可解析时账号 unavailable |
| CLI proxy `GET /billing?format=credits` | 官方 credits 百分比、周期、on-demand、prepaid、套餐名 | `snapshot` | 非稳定内部接口；鉴权过期、协议漂移或网络失败时保留 last-good 并标记 unavailable |

Grok 不进入 Codex JSONL 的 `source_files` / `source_generations` byte-offset 状态机，也不复用 `cursor_*` 事实表。本地扫描可以对单个 `updates.jsonl` 记住 content-free 文件身份与已消费 offset，以免每次全量重读正文；该 checkpoint 属于 Grok collector 私有状态，不得写入 Codex `parsed_offset`。

`updates.jsonl` 是 ACP `session/update` 流。collector 只接受：

```text
params.update.sessionUpdate = turn_completed
params.update.usage = {
  inputTokens, outputTokens, totalTokens,
  cachedReadTokens, cacheCreationTokens, reasoningTokens,
  modelCalls, apiDurationMs, costUsdTicks, numTurns,
  modelUsage.<modelId>.*
}
```

以及同文件中的 `tool_call` / `tool_call_update` 元数据。`rawInput`、`rawOutput`、`content`、thought / message chunk、`chat_history.jsonl`、`system_prompt.txt` 和未知字段在解析边界丢弃。`signals.json` 只描述 context window 与延迟，不是计费 Token，不得写入 usage 事件。

页面查询优先读取已提交 Grok snapshot，并在进程内按 generation 共享同一份只读快照；只有首次没有任何 snapshot 时才同步建立本地基线。已有数据时，本地 collector 与 billing 都以 single-flight 后台任务按各自最小刷新间隔更新；成功提交后先失效内存快照，再发送 query invalidation。全量扫描和网络延迟都不阻塞首屏或菜单栏展开。

## Grok 身份、合并与持久化

application schema v27 把 `agent_provider_snapshots` / `agent_provider_sources` 的 `provider` CHECK 扩展为 `('codex','cursor','grok')`，并新增 Grok 事实表：`grok_sessions`、`grok_session_lineage`、`grok_usage_events`、`grok_tool_events`、`grok_billing_snapshots`、`grok_billing_quota_observations`。登记表可按 provider 分片共用；事实表不得与 `cursor_*` 或 Codex `sessions` / `turns` / `usage_daily` 混写。Cursor 表继续保持 `provider = 'cursor'` 与 Cursor provenance。

Grok Session 使用内部 surrogate key，并以 `(provider='grok', external_session_id)` 唯一；`external_session_id` 取 `summary.json` 的 `info.id`。查询身份都包含 Provider。父 Session / fork 只保留脱敏的 parent 引用，不把子 agent 展开成第二套客户端。

项目内部身份使用 `grok` namespace 加上 `git_root_dir` 或 `info.cwd` 的稳定 hash；显示名只保留 path basename。完整路径只在 collector 内存中用于派生，写盘前丢弃；相同 basename 的不同目录仍由不同 project key 分组。

标题按 `generated_title`、带日期的“未命名会话”顺序选择，并回显 `grok_summary` 或 `fallback`。`title_is_manual` 只影响来源置信度，不改变隐私边界。没有 `summary.json` 的目录不成为业务 Session。

Usage 事件按 `prompt_id` 或 `(external_session_id, occurred_at_ms, model, token tuple)` 的稳定 hash 去重；同一 turn 重放不得重复计数。`cachedReadTokens` / `cacheCreationTokens` / `reasoningTokens` 按独立桶持久化。`costUsdTicks` 仅在来源给出完整值时作为 reported cost；缺失或 `cost_is_partial` 同类语义时整段成本保持 unknown，不得把部分 ticks 加总成完整账单。estimated cost 只来自 Helper 内版本化 xAI 参考价，与 reported 分开展示，也不进入 Codex/OpenAI 或 Cursor catalog。

Tool 事件只保存安全 `tool_name`、有限 outcome 和观测时间。MCP / 自定义工具名称按现有 Invocation 规则折叠到允许的 token；参数、路径和输出不得落库。

Snapshot 采用事务性全量替换或按 generation 提交：先验证所有白名单记录，再在一个 writer transaction 中更新 generation、来源、Session、lineage、事件与 billing。取消或失败不会覆盖上一份成功快照。Grok collector 失败不得暂停 Codex 索引或 Cursor 刷新。

## Grok 额度与账号

Grok 在线额度对标 Codex `wham` 与 Cursor Dashboard：默认开启、可随时关闭、必须显示来源，且不得写成稳定公共 API。实现读取 Grok Build 已登录态的 `~/.grok/auth.json`，只在调用期内存持有 Bearer 与 `user_id`，按 Grok CLI 自身方式请求 `{cli_chat_proxy_base_url}/billing?format=credits`。token、refresh token、Authorization、`x-userid` 和原始响应不得进入 SQLite、preferences、日志、RPC 或仓库。不调用 auto-topup 变更接口，也不修改 `auth.json`。

billing 响应优先消费新 credits 形状，旧 `monthlyLimit` / `used` 只做 fallback：

| 字段 | 产品映射 |
| --- | --- |
| `creditUsagePercent` | 当前 included credits 的 `used_percent`；展示层 `remaining = 100 - used` |
| `currentPeriod.type/start/end` | 窗口周期。`USAGE_PERIOD_TYPE_WEEKLY` 走周额度，`USAGE_PERIOD_TYPE_MONTHLY` 走月额度；不得用自然周或最近 7 天冒充 |
| `onDemandUsed` / `onDemandCap` | 独立 on-demand 窗口；缺 cap 时不伪造上限 |
| `prepaidBalance` | 已购余额摘要，不是 Reset Credits |
| `subscriptionTier` | 账号胶囊套餐名；缺失时显示“套餐信息未提供” |
| `isUnifiedBillingUser` | 只进入 coverage / 来源说明，不改 Token 账本 |

账号胶囊不依赖 billing：Helper 只从 `auth.json` 白名单读取可展示的 `email` 与 `principal_type` / `auth_mode`。完整账号标识、token、refresh token、OIDC 字段和 profile 原文不得返回。邮箱展示遵循现有 Popover 脱敏与截图隐藏规则。

额度刷新失败时，当前周期已有成功快照继续作为 last-known 返回，响应标记 `partial` 并回显 `dataAsOfMs`。跨过 `currentPeriod.end` 后，旧快照不得冒充新周期；无新成功值时显示 `--`。协议漂移、缺字段或百分比越界 fail closed，不写 `used_percent=0`。Grok 没有 Reset Credits，对应模块按 capability 隐藏。

Preferences 增加独立的 `online.grok_quota_enabled`，默认开启、可关；关闭后停止 billing 调度，保留已有非敏感 last-known。该开关不影响 Codex `wham` / reset credits 或 Cursor Dashboard。

## 精确度与 capability

- Codex 保留 quota、reset、account/plan、Token、API-equivalent cost、Session 与 Project 能力。
- Cursor 提供 Session、Project、Model、Tool、AI edits 和今日请求数；只有来源提供稳定精确事件时才提供 Token。
- Grok 提供 Session、Project、Model、Tool、Token、account 和 quota；有完整 `costUsdTicks` 时提供 `reported_cost`，有 xAI 参考价时提供 `estimated_cost`。不提供 Reset Credits 或 AI edits。
- Cursor / Grok usage model 都按事件中的 model key 分组，并为每个模型返回与总趋势相同粒度的 bucket；UI 只有在模型 bucket 无法与总量核对时才使用诚实的聚合降级，不把可识别模型压成“全部模型”。
- 今日使用 App reporting timezone 的 `[00:00, now)`。Grok 状态栏“已用 Token”优先对齐当前 billing `currentPeriod`；周期边界未知时显示 `已用 --`，不得拿今天、自然周或最近 7 天冒充本周期用量。
- 只要观察到的请求存在缺失或冲突 Token，相关 Token 聚合就是 unknown；Cursor state 中 request 对应的 `0/0` tokenCount，以及 Grok 没有 `turn_completed.usage` 的 Session，都是未提供值，不作为精确零持久化。不得按 transcript 长度、`signals.json` context usage 或其他指标估算。
- Cursor Dashboard 的 `charged_cents`、Cursor Token fee 与 plan spending，以及 Grok 的 `costUsdTicks`，作为 reported 数据分别回显，不进入另一家客户端的 pricing catalog。按各客户端官方/参考价固定版本计算的费用只标记为 estimated；reported 与 estimated 在 contract 和 UI 中分开展示。
- Dashboard 或 Grok billing 刷新失败时，当前周期已有成功快照继续作为 last-known 返回，响应标记 `partial` 并回显 `dataAsOfMs`；跨周期后旧快照不冒充当前数据。
- UI 按 capability 隐藏不适用模块，不能用一屏 `--` 伪装支持。所有 `if cursor else Codex` 展示分支必须改成显式三路或 capability 驱动，避免 Grok 误用 Codex 周环、Reset Credits 或 Cursor 月额度文案。

## Swift 状态与竞态

主窗口 `selectedProvider` 与菜单栏 `statusProvider` 独立持久化，并分别由下拉框选择；Popover 的下拉框位于原产品标题位置。主窗口切换客户端时清空详情、分页和错误状态，推进 request generation 并取消旧页面任务，但不取消或重载菜单栏任务；非 Overview 页面立即发起自己的 Provider 请求，不等待 Overview 聚合完成。主窗口按 `provider + range`、Popover 按 provider 保留进程内 last-success 展示缓存，切回已加载客户端时先呈现目标客户端缓存并后台刷新，绝不沿用另一客户端的数据；只有 provider 与 generation 都匹配的响应才能落入对应 presentation state。Popover 使用独立的 `statusOverviewState`，只请求当前界面会展示的额度、用量、今日摘要及 Codex 周项目排行，不为未渲染的 invocation 区块增加首屏 RPC。切换和刷新都不改变主窗口客户端，“打开主窗口”也只负责打开现有页面。连续 invalidation 期间状态栏刷新保持 single-flight，避免重复取消和重启同一批请求。

菜单栏在 Codex 下保持既有额度展示。Cursor 与 Grok 复用同一套图标和双行额度摘要：顶行是当前官方周期的剩余百分比，底行是同一周期内的累计 Token；没有成功额度快照时顶行为 `--`，没有可靠本地 Token 时底行为 `已用 --`。有 last-known 时显示最后成功值，并在 Popover 标注数据截至时间。Grok 的周期标签必须来自 billing `currentPeriod`，不得固定写成“周剩”或“月剩”。Popover 复用账号/套餐胶囊、额度卡片、趋势卡片和消费进度卡片的视觉结构，不再直接罗列内部来源字段；Grok 不展示 Reset Credits，也不把 prepaid 余额画成 Reset Credits。系统页可以同时观察所有客户端，但来源按 Codex / Cursor / Grok 分组，各客户端内部来源只在该分组展开。

## 隐私与验证

Cursor collector 在写盘前丢弃 prompt、response、thought、tool input/output、FTS body、tracked file content、browser logs/cookies、邮箱、原始路径和未知字段。Grok collector 在写盘前丢弃 `updates.jsonl` 正文与 tool payload、`chat_history.jsonl`、`system_prompt.txt`、`session_search.sqlite` 的 title/content、`auth.json` 密钥材料、完整路径和未知字段。公共 DTO、日志与提交版证据不得包含绝对路径、原始 ID、内容或凭据。Cursor Desktop access token 与 Grok `auth.json` Bearer / refresh token 都只在单次请求内存中使用，不写入配置、日志、Codex Pulse 数据库或仓库。

单元、contract、CI 和 deterministic smoke 使用 synthetic/empty Codex、Cursor 与 Grok Home。`CODEX_PULSE_CURSOR_HOME` 与 `CODEX_PULSE_GROK_HOME` 只用于给这些进程显式绑定隔离的绝对测试根目录。真实产品验收可只读访问真实 Codex / Cursor / Grok 数据，但必须写入私有 mode `0700` runtime，并只保存脱敏聚合证据。Grok 验收不得用 isolated / empty Home 冒充真实产品结论，也不得把 Cursor 内的 Grok 模型用量当作 Grok 客户端证据。
