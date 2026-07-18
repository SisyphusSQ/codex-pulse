# Product Design

## 产品目标

Codex Pulse 不只是 quota meter，而是 Codex 本机观测工具。它应让用户快速回答：

- 5 小时和周额度是否可信、还能使用多少、何时 reset；
- 今天哪些项目、模型和 Session 消耗最多；
- 最近 Session 是否仍有 active turn；
- 本地索引是否完整、后台是否推进、应用最近是否健康；
- 数据来自本地还是可选在线接口，失败时当前展示还是否可信。

v0.1 先服务单机、单账号、Codex-only 场景。仅提供简体中文（`zh-CN`）UI，不提供语言切换；绝对路径可复制，不做云同步或公网访问。

## 渐进披露

产品使用“Tray 一眼判断、Popover 快速查看、概览分析下钻”的结构；运行诊断收敛到靠近设置的“本机状态”。

### Tray

默认只显示 5h 和 weekly 剩余，不显示 active session 数：

```text
fresh       5h 62% · W 71%
stale       5h 62% · W 71%
conflict    5h 55% · W 71%
unknown     5h 55% · W 71%
exhausted   5h 0% · 42m
```

百分比始终表示 remaining。界面只显示普通百分比，不使用 `≤`、`?` 或状态胶囊向用户解释不确定性。stale、conflict 和 expired_unknown 有 last-known-good 时继续显示当前选定值；从未取得数值时显示 `--`；`0%` 只表示确认耗尽。

状态栏采用紧凑双行额度仪表：左侧为 Codex Pulse 单色模板图标，右侧分别展示“5 小时”和“本周”的短进度条与普通百分比。正常状态使用低饱和系统蓝 / 紫；stale 与 unknown 弱化为灰色；conflict 使用橙色；确认耗尽的对应额度使用红色。颜色只辅助扫读，百分比文字始终保留。

正常时不显示来源、更新时间、索引进度、CPU、读取字节或队列。`blocked` 立即在模板图标右上增加红色健康点；`degraded` 只有持续 2 分钟，并且影响当前数据可信度或需要用户处理时，才增加橙色健康点。健康点独立于额度进度条颜色，普通 quota 网络失败不额外制造全局警告。

左键打开 Popover；右键原生菜单包含刷新当前数据、打开概览、暂停/继续历史索引、设置、检查更新和退出。存在已升级的 `degraded` / `blocked` 时，菜单顶部条件显示“查看数据健康…”，并展示当前问题数。

### Popover（暂定，待最终确认）

Popover 不放复杂图表，也不显示来源冲突、网络失败或索引异常说明，顺序为：

1. 5h / weekly quota：普通 remaining 百分比、进度条和 reset 倒计时。
2. Reset credits：可用次数、总次数、累计剩余时间、最近到期时间和 Quota 详情入口。
3. API 等价成本：今日金额、token 总量、统计周期、计算时间和概览详情入口。
4. Session 摘要：active session 数、最近活动和最多 5 个 Session，只表达 active / idle。
5. 固定操作：刷新、打开概览。

打开 Popover 时，quota 上次成功超过 60 秒则异步刷新。有 last-known-good 时继续显示当前数值，不切换成空白 loading，也不增加不确定性说明。Reset credits 与 API 等价成本摘要整块可点击，分别进入配额和概览对应区域。

### 概览

概览是默认落地页，由原 Usage 页面升级而来，按“先看额度、再看趋势和成本”排序：

- 顶部紧凑摘要：5 小时和本周额度、普通百分比、进度条与 reset 时间。
- 时间范围：今天、7 天、30 天和自定义区间，默认 7 天。
- 使用趋势：input、cached input、output 与 reasoning token。
- 用量构成与 API 等价成本：保留估算口径和未定价提示。
- 每日明细：日期、token、cached、output、API 等价成本和完整性。

概览不承担完整运行诊断；只有影响当前分析时才显示轻量健康入口或局部状态，详细原因进入本机状态 / Data Health。后台刷新不驱动全局 loading，quota、趋势和当前区间数据分别定向更新。

### 本机状态

本机状态由原 Dashboard 收敛而来，位于设置上方，不作为默认落地页。它只回答运行与数据可靠性问题：

- 数据完整性与历史补齐进度；
- 索引新鲜度、已索引 Session 和待处理任务；
- 本地 Session、在线配额和 Reset credits 等数据来源；
- 后台任务、数据库与存储状态；
- 最近运行记录和 Data Health 入口。

条件 Banner 同一时间最多显示一个，按 `blocked` 红色、影响当前数据的持续 `degraded` 橙色、历史补齐或部分数据蓝色排序。普通在线 quota 失败和不影响当前视图的 warning 不占用全局 Banner。手动刷新按 quota、live queue 和当前页面数据执行，不触发历史重扫。

主导航固定为“概览 / 会话 / 项目 / 配额 / 本机状态 / 设置”。不包含 Attention；Data Health 继续作为本机状态下钻页，不单列主导航。

### Data Health

Data Health 是由本机状态健康入口或异常 Tray 菜单打开的二级页面，不增加主导航项。页面按“影响优先”排序：

1. 最高优先级影响：说明当前影响、保护措施和首要恢复动作。
2. 数据领域：本地索引、live queue、历史补齐、在线配额、SQLite / 磁盘和更新器。
3. 当前工作：任务、状态、进度、速率 / 延迟和 next retry。
4. 最近事件：合并同类事件，显示影响、累计次数、last seen 和恢复时间。
5. 最近 24 小时资源：进程级 CPU / RSS、DB / WAL 和可用磁盘。
6. 可执行操作：重试、暂停 / 继续索引、重新授权在线能力和打开日志。

Data Health 不展示原始 JSONL、凭证或完整错误堆栈；v0.1 不提供诊断导出。Quota observation 仲裁细节继续进入 Quota 页面，Data Health 只解释来源可用性和当前影响。

空值语义统一：

- `0%`：确认耗尽；
- `--`：从未取得数值、不适用或尚未计算；
- 普通百分比：包含 fresh 与 last-known-good，展示格式不区分内部可信状态；
- “部分数据”：目标时间范围尚未索引完整；
- “本地来源”：在线 quota 未启用，不是错误。

## 概览用量与 API 等价成本

概览展示“API 等价成本”，不能命名为真实花费。计算口径：

```text
api_equivalent_cost =
  input_tokens / 1_000_000 * input_price
+ cached_input_tokens / 1_000_000 * cached_input_price
+ output_tokens / 1_000_000 * output_price
```

reasoning token 单独展示；若没有单独价格，按模型公开口径折算并明确标注。无法匹配模型时必须显示 `unpriced` 或明确 fallback，不能静默乱算。

Pricing Catalog 本地版本化，每条记录包含 model、input/cached/output price、currency、effective date 和 source。历史 turn 使用当时选定的 pricing version，避免价格变化导致历史报表漂移。v0.1 不自动联网更新价格。

### 查询、下钻与降级语义

- 概览按用户选择的 IANA timezone 和本地日半开区间查询，日趋势直接读取 active daily rollup；周、月只合并 daily rows，不维护第二套持久聚合。响应同时保留 pricing source/currency、range 内 pricing versions、未定价原因与 priced/unpriced turn count。
- active cost generation 暂不可见时，概览只从 final usage 做有界 token fallback：cost/pricing 保持 unknown，响应标记 `partial / rollup_missing`；查询路径不得触发 ledger rebuild。无事实但 active generation 正常存在时是 known-empty complete，真实 `0` 不显示成 `--`。
- Sessions 列表和详情只展示安全 `session_attributions` 的 title/project/model，active/idle 由是否存在未完成 Turn 判断；不返回 cwd、raw model、root path 或对话内容。列表支持最近活动、token、API 等价成本排序以及 project/model/activity/time filter，cursor 是不可解析的 keyset token。
- Session 详情还返回按 `startedAt + turn identity` 稳定倒序、默认 20 / 最大 50 条的 content-free Turn usage/cost 时间线。每项只包含不可逆 timeline key、active/complete、安全 model attribution、时间、整数 usage、pricing status/version/reason；`completed_at_ms IS NULL` 已明确定义 active，unknown 只用于 usage/cost/time 数值，不伪造不可达的 lifecycle 状态。不得返回 raw Session/Turn ID、正文事件、tool、路径、offset 或 generation。下一页 cursor 由 process-key AEAD 认证加密并绑定当前 Session，前端只可原样回传；完整首屏必须与 Session aggregate 精确对账，截断/后续页必须满足 aggregate 下界和 pricing evidence membership，page totals 不得覆盖整段 Session aggregate。
- `/sessions` 的 UI 状态只把有限 activity/time/sort、精确 safe project/model、列表 cursor 与选中 Session 写入 URL；未知 key、重复值、空值和非法枚举会归一到安全默认。list cursor 不解析，筛选/排序变化清空 cursor 与 selection；Turn cursor 只存在于当前 detail 生命周期，切换 Session 或进程重启后失效，不进入 URL、Preferences 或 Web Storage。
- 当前 query contract 不提供 title/contains search 或全库 project/model option endpoint，页面不得在当前 page 本地过滤后伪装成全量搜索。列表、详情、activity、totals、pricing、partial 与时间线顺序全部使用 generated DTO；Vue 只做 locale 格式化和微美元显示换算，不重新聚合或定价。
- Session cost generation 缺失时仍可展示安全 Session 身份与 active/idle；token/cost 显式 unknown 并局部标记 `partial / rollup_missing`，不伪造累计值。未指定 reporting timezone 且同时存在多个合法 active generation 时不得任取一份 ledger，必须返回同样的安全身份事实与 unknown totals，并标记 `partial / rollup_ambiguous`。详情与列表 item 使用同一 mapper，pricing evidence 只来自同一 active generation。
- Projects 必须从所选 range 的 active `project_usage_daily` 查询；known project 与 unknown/conflict/invalid 维度都参与全局对账。range 聚合后的最保守 confidence 决定筛选结果，不能先按 daily confidence 截断后再汇总。
- Project list/detail 同时返回 global、matched、page totals；无筛选全局 Project totals 必须与同 range `usage_daily` 一致，detail daily 合计必须与 list item 一致。任一对账漂移或 active generation 缺失都返回 unavailable，不能伪装成空项目。
- Project list item 返回所选 range 内贡献 Turn 的精确 distinct Session 数，以及该 range 末尾最多 30 个已有日 bucket 的升序 trend。trend 只用于趋势展示，不是 full-range totals；前端不得用当前页 Session 或 daily 重算 count/totals。
- Project detail 沿用既有方法返回 Project contribution 的两组独立 keyset page：Session 按 `lastActivityAt DESC, session identity DESC`，Model 按 `totalTokens DESC NULLS LAST, model dimension DESC`；两者均默认 20、最大 50。Session 页只复用安全 title/current-model attribution，totals 仅统计当前 Project dimension 在 range 内的贡献，不得用 `ListSessions(projectId)` 的整 Session rollup 替代。两类 opaque cursor 同时绑定当前 active generation；同进程 generation rollover、Project/range变化或进程重启后必须从首页恢复。
- Store 必须在同一 active generation/read snapshot 中，分别把全量 Session groups 和全量 Model groups 的 NULL-preserving totals 对账到 Project item；unknown/conflict/invalid Project/Model dimension 不丢弃。任一分组对账失败都 fail closed 为 unavailable，不返回局部伪造页。
- `/projects` 产品交互固定为默认近 7 天，并支持今日、近 30 天、自定义本地日半开区间、range-level confidence、服务端排序与稳定分页。列表选中 Project 后在同页下钻 daily、Model contribution 与 Session contribution；list cursor可进入URL，两类detail cursor不进入URL或持久状态。Project/range变化清空两类detail page，单类cursor validation只恢复对应page；not-found关闭旧detail并保留列表。页面不提供没有provider contract的path、Finder/reveal或全文搜索，也不显示opaque identity。
- 所有 count/token/微美元保持整数和 unknown reason；只要存在未定价 Turn，即使已定价小计非空，相关 Session/Project 响应也必须是 partial。priced turn 必须至少关联一个 pricing version，未定价原因计数之和必须严格等于 unpriced turn count，否则 fail closed 为 unavailable。金额仍是 API 等价估算，不接入或对账云账单。

## Settings 与 Codex Home

Settings 使用强类型 Preferences，不把空值或非法值静默折成默认值。v0.1 可配置在线 quota/reset credits、对应刷新周期、JSONL debounce、更新检查和 UI 启动/概览范围；`zh-CN` 和 stable update channel 固定，自动下载保持关闭。保存使用 revision conflict 提示，不采用 last-writer-wins；切换恢复进行中时普通设置暂不可保存。

Codex Home 更换是独立的两步确认，不属于普通设置保存：先 metadata-only 检测目标并展示影响，再由用户明确选择“新建独立数据库”或“清空当前派生索引后重建”。前者保留旧数据库与审计事实，切回相同 Home 时复用；后者复用当前数据库 key，由 bootstrap 清理派生索引后重建。两种策略都不删除或修改 Codex JSONL/auth，也不允许同时激活两个 Home。

确认后 UI 进入不可伪装为普通 loading 的切换状态：先取得跨进程切换 execution lease、持久登记旧 Home 的恢复责任，再等待旧任务 drain，随后发布新 Home generation 并启动 bootstrap。并发确认或恢复必须等待同一租约；live owner 不得被另一进程提前 Resume/清 marker，owner 进程退出并由 OS 释放租约后才允许接管。应用退出、请求取消、Resume 失败或启动结果不明确时，重启根据持久 journal 和 runtime status 明确继续、回滚或提示恢复；不能因为没收到成功响应就重复启动任务，也不能在旧任务仍 drained 时把恢复标记清掉。后台任务、设置保存和数据查询都只认当前 active Home generation。

## v0.1 交付范围

- 本地只读、按 offset 增量索引 Codex JSONL 和 `session_index.jsonl`。
- SQLite session / turn / usage / quota / source state / file cursor / job run schema，以及 project/model 日聚合。
- 配额 last-known-good、fresh / stale / expired_unknown / suspicious 和来源时间。
- 分数据源调度、后台限速、前台提权和失败退避。
- 一页隐私说明、Codex home 探测、fast bootstrap、live/backfill 双队列、可续传初始索引和安全错误恢复。
- 最近 24 小时资源与故障观测和 Data Health。
- Tray、Popover、概览、Sessions、Projects、Quota、本机状态、Settings。
- 本地版本化 pricing catalog。
- 在线 quota / reset credits 作为默认开启、可随时关闭的实验性能力；始终显示来源、更新时间和失败降级状态。
- session index repair 仅 dry-run + 显式确认。

### M11 最终验收入口

v0.1 的最终集成验收以 [`docs/test/m11-e1.md`](../../../test/m11-e1.md) 为统一 runbook。该矩阵使用稳定场景 ID，把 Onboarding、索引、账本、Quota、UI、Tray、Health、更新、性能、隐私、辅助功能和发布就绪分别映射到 TOO-298～303；已有子 runbook 只作为实现证据入口，不替代当前主干上的 required live E2E。未执行、失败或缺少清理证据的 required 场景保持 blocking，不能用演示 fixture 或历史结果标记 M11 完成。

GitHub Actions 当前按用户要求停用；最终验收使用本地 gate 并如实记录 `actions_disabled_by_user`。正式发布、tag/release、真实 appcast/密钥和外部分发仍需用户另行明确授权；未授权时发布卡只能收口到可复验的 release-readiness。

## 后续阶段

1. Codex-only 本地账本和工作台。
2. live 运行态：进程、端口、Git 状态和 PID 到 JSONL 的映射；不扩展成 Waiting/Blocked/Done Session 状态机。
3. 配额提醒：阈值、burn rate 和可信度状态栏文案。
4. 个人工作流：项目别名、Obsidian 摘要、高成本 Session 诊断、Tailscale 只读视图和多 agent provider。

## 明确不做

- 不调用 `codex app-server` 查配额或作为兜底。
- 不把 `wham/*` 当稳定 API；v0.1 默认启用在线 quota 与 reset credits，但必须显示来源，允许用户随时关闭。
- 不复制原始 JSONL，不保存完整对话或工具输出。
- 不把内部 HTTP 接口当稳定 API。
- 不用一个全局周期反复全量扫描，也不在前台刷新时重复启动扫描。
- 不静默下载、强制安装或在 SQLite 事务未安全结束时重启。
- v0.1 不提供 Usage JSON/CSV、诊断包或其他用户数据导出。
- v0.1 不提供语言切换或其他语言包，但前端文案按 i18n message key 组织。
- 不在 v0.1 做 turn 完成通知、Attention、云同步或公网访问。
- 不复制 Codex Runway 的代码、图标或高度相似 UI。
