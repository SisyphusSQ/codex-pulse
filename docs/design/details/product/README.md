# Product Design

## 产品目标

Codex Pulse 不只是 quota meter，而是 Codex 本机观测工具。它应让用户快速回答：

- 当前真实额度窗口是否可信、还能使用多少、何时 reset；
- 今天哪些项目、模型和 Session 消耗最多；
- 最近 Session 是否仍有 active turn；
- 本地索引是否完整、后台是否推进、应用最近是否健康；
- 数据来自本地还是可选在线接口，失败时当前展示还是否可信。

v0.1 先服务单机、单账号、Codex-only 场景。UI 支持跟随 macOS 系统语言自动选择简体中文或英文，也支持在 App 设置中显式切换并持久化选择；绝对路径可复制，不做云同步或公网访问。

## 渐进披露

产品使用“Tray 一眼判断、Popover 快速查看、概览分析下钻”的结构；运行诊断收敛到靠近设置的“本机状态”。

### Tray

默认显示周额度剩余与同一额度周期内的累计 Token，不显示 active session 数：

```text
fresh       周剩 30% / 已用 7.9亿
stale       周剩 30% / 已用 7.9亿
unknown     周剩 -- / 已用 --
exhausted   周剩 0% / 已用 9.4亿
```

百分比始终表示 remaining。界面只显示普通百分比，不使用 `≤`、`?` 或状态胶囊向用户解释不确定性。stale、conflict 和 expired_unknown 有 last-known-good 时继续显示当前选定值；从未取得数值时显示 `--`；`0%` 只表示确认耗尽。

状态栏采用紧凑双行额度摘要：左侧可在 A 基准圆环、B 缺口圆环、D 仪表弧之间切换，右侧第一行显示从真实 `window_minutes` 派生的周期标签与 remaining 百分比，第二行显示同一窗口起点至本次 `evaluated_at_ms` 的累计 Token。存在多个同周期额度时，状态栏固定优先 `limit_id=codex` 的通用额度，不得任取模型专属额度；详细窗口按 `limit_name + window_minutes` 明确标注。模型专属额度的选中数值 observation 缺少名称时，可使用同额度最新 accepted observation 的 `limit_name`；最终仍无名称时显示“模型专属额度”，不得暴露内部 `limit_id`。默认使用 A。remaining 大于 40% 使用系统绿色、21%～40% 使用黄色、20% 及以下使用红色；fresh、stale 与 suspicious 只要仍有选中的 last-known-good 都保留该余量色，stale 以灰色状态点、suspicious 以橙色状态点单独表达可信度，unknown、expired_unknown、never_loaded 或无可用数值时才整体使用系统灰色。辅助功能标签必须同步说明“显示上次可信额度”或“新额度数据异常”，不能只依赖颜色和状态点。Usage 查询范围与额度窗口无法精确对齐时必须显示“已用 --”，不得拿今天、自然周或最近 7 天冒充本周期用量。

App 内表示额度或容量风险的百分比进度条统一使用同一组语义色：remaining 采用上述 40% / 20% 阈值，used 采用其补集阈值，即低于 60% 为绿色、60%～不足 80% 为黄色、80% 及以上为红色；明确 rate-limited 时直接使用红色，数值缺失或非有限值时使用系统灰色。进度条的辅助功能值必须同时包含百分比和“正常 / 需要关注 / 需要处理 / 暂不可用”状态，不能只依赖颜色。模型、项目和工具等排行或构成占比属于中性比较，不套用风险阈值，继续使用类别色。

正常时不显示来源、更新时间、索引进度、CPU、读取字节或队列。`blocked` 立即在模板图标右上增加红色健康点；`degraded` 只有持续 2 分钟，并且影响当前数据可信度或需要用户处理时，才增加橙色健康点。健康点独立于额度进度条颜色，普通 quota 网络失败不额外制造全局警告。

左键打开 Popover；右键原生菜单包含刷新当前数据、打开概览、暂停/继续历史索引、设置、检查更新和退出。存在已升级的 `degraded` / `blocked` 时，菜单顶部条件显示“查看数据健康…”，并展示当前问题数。

### Popover（暂定，待最终确认）

Popover 不放复杂图表，也不显示来源冲突、网络失败或索引异常说明，顺序为：

1. 动态 quota windows：用后端 `limit_name`（默认桶回退“通用额度”）与真实周期联合标注，展示普通 remaining 百分比、进度条和 reset 倒计时。
2. Reset credits：可用次数、总次数、累计剩余时间、最近到期时间和 Quota 详情入口。
3. API 等价成本：今日金额、token 总量、统计周期、计算时间和概览详情入口。
4. Session 摘要：最多 5 个 Session 按最近活动时间倒序展示，并在右侧显示与顶部相同口径的 Session API 等价成本；未计算、不适用或暂不可用必须使用明确文案，不显示无标签的 `--`。
5. 固定操作：刷新、打开概览。

Popover 顶部还固定提供三项快捷功能：

1. 账户套餐 / 账号摘要：只复用当前 Overview 已有的展示级 Quota 聚合，显示隐私安全的“当前 Codex 账号”以及套餐/额度可用性；后端未提供套餐名称时如实显示“套餐信息未提供”，不得用 raw account scope、完整账号标识、token、路径或鉴权材料补齐。
2. 项目主页：固定打开 `https://github.com/SisyphusSQ/codex-pulse`。外跳只能由用户点击该原生按钮，或在按钮获得键盘焦点后使用 Return / Space 显式激活；打开 Popover、刷新或后台状态变化都不得自动触发，也不增加遥测或其它网络请求。
3. 复制完整截图：直接捕获当前已经打开的 Popover 原生视图，顶栏和底栏各保留一次，中间滚动区按真实滚动位置分段捕获并拼接为完整长图；不得另建截图专用 SwiftUI 页面或从 DTO 重构另一套卡片。原生材质的透明区域按当前系统外观合成到窗口底色，保证图片粘贴到浅色或深色背景时仍可读。截图期间必须隐藏账号和套餐文字，完成或失败后恢复原滚动位置和正常展示状态。成功时在同一个系统剪贴板 item 中同时写入不含账号/套餐的 `.string` 隐私说明和 `.png` 完整截图。

三项快捷按钮都必须进入整个 Popover 的原生键盘焦点顺序，支持 Return / Space 激活，并提供明确的无障碍标签。它们不得拦截 Tab 或建立只在三项按钮之间循环的私有焦点链：前向 Tab 必须能继续到 Reset Credits、设置和退出，Shift-Tab 必须能反向回到刷新和打开概览等已有控件。完整截图包含主 Popover 当前实际展示的额度、每日 Token、Reset Credits、已启用的成本/项目排行以及固定操作，不递归捕获 Reset Credits 或显示设置子页。截图渲染不可用、系统剪贴板写入失败或外部 URL 打开失败时必须显示用户可见错误；失败路径不得回退复制原始 Session、JSONL、完整账号标识、套餐、鉴权材料、日志或其它敏感数据，也不得在只写入其中一种剪贴板表示后宣称成功。

打开 Popover 时，quota 上次成功超过 60 秒则异步刷新。有 last-known-good 时继续显示当前数值，不切换成空白 loading，也不增加不确定性说明。Reset credits 与 API 等价成本摘要整块可点击，分别进入配额和概览对应区域。

### 概览

概览是默认落地页，由原 Usage 页面升级而来，按“先看额度、再看趋势和成本”排序：

- 顶部紧凑摘要：按额度名称和真实窗口周期区分通用/模型专属额度，展示普通百分比、进度条与 reset 时间。同一 `limit_id + window_minutes` 的 primary/secondary 只展示一个，优先可信 reset、较新鲜状态、完整名称和 primary；同一额度的不同真实周期（例如 5 小时与 7 天）必须分别保留。去重只属于 Swift 展示层，不裁剪 Helper 返回的完整窗口与仲裁证据。
- 年度 Token 活动：在额度摘要与当前范围消耗之间展示独立的过去 365 个本地自然日日历热力图，并汇总近 365 天 Token、峰值日 Token、活跃天数、当前连续天数和最长连续天数。当前连续天数允许今天尚未活动时从昨天回溯；热力图不跟随顶部时间范围切换。完整响应缺失的日期表示真实零活动，partial 响应缺失的日期保持未知；如果本机已知的每日 Token 与合计能够对账，仍按本机现有数据展示五项汇总并明确标注本地口径。格子悬浮立即展示日期、紧凑单位 Token 和已知轮数；局部查询失败不得隐藏额度或当前范围消耗。
- 当前范围活动与高消耗会话：复用顶部选择的精确时间范围，不再提供第二套日 / 周 / 月切换。左侧用柱状时间线和“星期 × 小时”热力图组成两个活动视图，并可在“Token 消耗 / 会话数量”之间切换；今天按小时，其它预设按天。会话数量按格子内出现过 final usage 的去重 Session 计数，同一 Session 可分别出现在不同时间桶；Token 使用现有总量口径。右侧展示同范围内按 Token 总量倒序的前 10 个高消耗会话，并可下钻到保持同一范围的会话详情。宽窗口使用双栏，窄窗口自动改为纵向布局。
- 时间范围：今天、7 天、30 天和自定义区间，默认 7 天。
- 使用趋势：每日 Token 按模型堆叠，纵轴使用本地化数量级，鼠标移入某日后展示总量、各模型 Token 与占比；模型分项无法与当日总量精确对账时只展示已知总量并明确提示明细不可用，不按比例推算。
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
  (input_tokens - cached_input_tokens) / 1_000_000 * input_price
+ cached_input_tokens / 1_000_000 * cached_input_price
+ (output_tokens + reasoning_tokens) / 1_000_000 * output_price
```

Codex JSONL 的 cached input 是 input 的子集，必须先从 input 扣除，不能按完整 input 价格重复计费；`cached_input_tokens > input_tokens` 视为非法事实并 fail closed。reasoning token 单独展示，并按模型公开 output 价格折算。无法匹配模型时必须显示 `unpriced` 或明确 fallback，不能静默乱算。

Pricing Catalog 本地版本化，每条记录包含 model、input/cached/output price、currency、effective date 和 source。历史 turn 使用当时选定的 pricing version，避免价格变化导致历史报表漂移。`PricingCatalogCurrent` 独立返回当前生效的 exact-only 完整目录、每百万 Token 单位、基础计价口径和官方来源，不得从历史 `UsageCost` 反推当前单价；v0.1 不自动联网更新价格。

### 查询、下钻与降级语义

- 概览按用户选择的 IANA timezone 和本地日半开区间查询，日趋势直接读取 active daily rollup；轻量启动模式则从带安全模型归因的 timed token delta 在同一 read snapshot 中聚合。周、月只合并 daily rows，不维护第二套持久聚合。响应同时返回全局 totals、按模型统计及每个模型同粒度的 `UsageModelItem.trend`、pricing source/currency、range 内 pricing versions，以及能够确定的未定价语义。Swift 只能按 bucket 对账并展示这些模型事实，不得用 range 总量或比例反推日明细。
- `UsageCostRequest.include_activity_distribution=true` 时，Go 在同一查询范围内额外返回非空时间桶和稀疏的 ISO weekday（周一 1、周日 7）小时格。每个点同时包含 Token 总量和格内去重 Session 数；热力图跨日期合并相同星期小时，DST 回拨的两个真实小时在时间线中保持不同 instant，但在星期小时视图中归入同一墙钟小时。Swift 只有在时间线 Token、星期小时 Token 与响应 totals 能对账时，才把未返回格解释为真实零；否则保持 unknown。
- active cost generation 暂不可见时，概览只从 final usage 做有界 token fallback：cost/pricing 保持 unknown，响应标记 `partial / rollup_missing`；查询路径不得触发 ledger rebuild。无事实但 active generation 正常存在时是 known-empty complete，真实 `0` 不显示成 `--`。
- 原生“额度与用量”页独立加载当前参考价格目录，因此当前范围没有用量时仍可查看参考价格；目录默认直接显示为原生四列表格，按 exact model id 列出 `gpt-5.3` 及后续有效模型的输入、缓存输入和输出费率，不在模型用量行重复长价格文案。`gpt-5`、`gpt-5.1`、`gpt-5.2` 家族保留在 Go 的不可变 catalog 中供历史成本折算，但不进入当前参考价格表；无后缀 `gpt-5.6` 是 `gpt-5.6-sol` 的官方 alias，同样保留用于 exact 成本匹配，但为避免与 Sol 重复而不展示。`gpt-5.6-luna`、`gpt-5.6-sol`、`gpt-5.6-terra` 等后缀模型正常展示。Swift 仅将整数微美元换算为 `USD / 100 万 Token` 展示，unknown 显示“暂无”，不得重新定价。页面必须明确标注这是 OpenAI API Standard 基础文本参考价和 API 等价折算依据，不是 Codex 订阅账单；长上下文、cache-write、Batch、Flex、Fast mode（原 Priority）和区域处理差异不在当前 contract 中。
- Sessions 列表和详情只展示安全 title/project/model；轻量模式优先使用 Codex App Server `thread/list` 返回的 name，缺失时使用不可逆 Session ID fallback。常驻 Helper 每 30 秒刷新一次 metadata，并在 App 前台激活或系统唤醒时立即触发；相同 snapshot 不推进 metadata generation，标题变化原子发布后通过 index invalidation 到达 Swift。严格模式继续使用 `session_attributions`；active/idle 由是否存在未完成 Turn 判断。Session 详情趋势由 Go 按请求的 IANA timezone 检查完整会话用量覆盖的本地自然日：全部位于同一日时返回 `trend_granularity=hour` 和小时 bucket，只要跨越两个或更多本地日期就返回 `trend_granularity=day` 和每日 bucket；只有轻量 timed delta 或同一 active cost generation 的 final usage 可以生成趋势。DST 回拨的重复墙钟小时必须保留各自真实 instant/UTC offset，作为两个有序 bucket 返回。Swift 只消费 granularity、timezone、bucket 时间和 totals，不得用当前页面点数或本机日历自行猜测口径。两种模式都不返回 cwd、raw model、root path 或对话内容。列表支持最近活动、token、API 等价成本排序以及 project/model/activity/time filter，cursor 是不可解析的 keyset token。
- Session 详情还返回按 `startedAt + turn identity` 稳定倒序、默认 20 / 最大 50 条的 content-free Turn usage/cost 时间线。每项只包含不可逆 timeline key、active/complete、安全 model attribution、时间、整数 usage、pricing status/version/reason；`completed_at_ms IS NULL` 已明确定义 active，unknown 只用于 usage/cost/time 数值，不伪造不可达的 lifecycle 状态。不得返回 raw Session/Turn ID、正文事件、tool、路径、offset 或 generation。下一页 cursor 由 process-key AEAD 认证加密并绑定当前 Session，native client 只可原样回传；完整首屏必须与 Session aggregate 精确对账，截断/后续页必须满足 aggregate 下界和 pricing evidence membership，page totals 不得覆盖整段 Session aggregate。
- `/sessions` 的 UI 状态只把有限 activity/time/sort、精确 safe project/model、列表 cursor 与选中 Session 写入 URL；未知 key、重复值、空值和非法枚举会归一到安全默认。list cursor 不解析，筛选/排序变化清空 cursor 与 selection；Turn cursor 只存在于当前 detail 生命周期，切换 Session 或进程重启后失效，不进入 URL、Preferences 或 Web Storage。
- 当前 query contract 不提供 title/contains search 或全库 project/model option endpoint，页面不得在当前 page 本地过滤后伪装成全量搜索。列表、详情、activity、totals、pricing、partial 与时间线顺序全部使用 generated Protobuf DTO；Swift client 只做 locale 格式化和微美元显示换算，不重新聚合或定价。
- Session cost generation 缺失时仍可展示安全 Session 身份与 active/idle；Session aggregate token/cost 显式 unknown、趋势为空且不声明粒度，并局部标记 `partial / rollup_missing`，不得从 final usage 旁路伪造累计值。这里的 aggregate 降级不抹除上一条定义的 content-free Turn timeline 已有 final usage 事实，但客户端不得把 Turn 页重新累计成 Session totals 或趋势。未指定 reporting timezone 且同时存在多个合法 active generation 时不得任取一份 ledger，必须返回同样的安全身份事实、unknown totals 和空趋势，并标记 `partial / rollup_ambiguous`。详情与列表 item 使用同一 mapper，pricing evidence 只来自同一 active generation。
- Projects 必须从所选 range 的 active `project_usage_daily` 查询；known project 与 unknown/conflict/invalid 维度都参与全局对账。range 聚合后的最保守 confidence 决定筛选结果，不能先按 daily confidence 截断后再汇总。
- Project list/detail 同时返回 global、matched、page totals；无筛选全局 Project totals 必须与同 range `usage_daily` 一致，detail daily 合计必须与 list item 一致。任一对账漂移或 active generation 缺失都返回 unavailable，不能伪装成空项目。
- Project list item 返回所选 range 内贡献 Turn 的精确 distinct Session 数，以及该 range 末尾最多 30 个已有日 bucket 的升序 trend。trend 只用于趋势展示，不是 full-range totals；native client 不得用当前页 Session 或 daily 重算 count/totals。
- Project detail 沿用既有方法返回 Project contribution 的两组独立 keyset page：Session 按 `lastActivityAt DESC, session identity DESC`，Model 按 `totalTokens DESC NULLS LAST, model dimension DESC`；两者均默认 20、最大 50。Session 页只复用安全 title/current-model attribution，totals 仅统计当前 Project dimension 在 range 内的贡献，不得用 `ListSessions(projectId)` 的整 Session rollup 替代。两类 opaque cursor 同时绑定当前 active generation；同进程 generation rollover、Project/range变化或进程重启后必须从首页恢复。
- 轻量索引的 Project detail 必须按列表返回的稳定 `dimension_key` 查找；已归类项目的 `project_id` 可以与其相同，但未归类“其他”的 `project_id` 为 `NULL`，不得拿 `dimension_key` 冒充 `project_id` 后把可见列表项错误返回为 not-found。Session contribution 需要分页时，opaque cursor 必须绑定当前 metadata/token-scan 组合代际并在下一页读回时校验；不得因为轻量模式没有 cost generation 就写入空 generation，也不得在索引变化后继续消费旧页。
- Store 必须在同一 active generation/read snapshot 中，分别把全量 Session groups 和全量 Model groups 的 NULL-preserving totals 对账到 Project item；unknown/conflict/invalid Project/Model dimension 不丢弃。任一分组对账失败都 fail closed 为 unavailable，不返回局部伪造页。
- `/projects` 产品交互固定为默认近 7 天，并支持今日、近 30 天、自定义本地日半开区间、range-level confidence、服务端排序与稳定分页。列表选中 Project 后在同页下钻 daily、Model contribution 与 Session contribution；list cursor可进入URL，两类detail cursor不进入URL或持久状态。Project/range变化清空两类detail page，单类cursor validation只恢复对应page；not-found关闭旧detail并保留列表。页面不提供没有provider contract的path、Finder/reveal或全文搜索，也不显示opaque identity。
- 原生 Popover 固定展示“本周项目 Token 排行”，按当前通用周额度的精确 UTC 周期和 `totalTokens DESC` 取前 5 个已归类项目。该请求独立于主 Overview 的范围选择，unknown confidence 的未归类用量在分页前排除；“其他”不显示、不占名次，但仍保留在主 Overview 与全局 totals 的对账口径中。周额度范围缺失时必须局部显示不可用，不能回退成自然周或最近 7 天后继续称为“本周额度”。
- 所有 count/token/微美元保持整数和 unknown reason；只要存在未定价 Turn，即使已定价小计非空，相关 Session/Project 响应也必须是 partial。priced turn 必须至少关联一个 pricing version，未定价原因计数之和必须严格等于 unpriced turn count，否则 fail closed 为 unavailable。金额仍是 API 等价估算，不接入或对账云账单。

## Settings 与 Codex Home

Settings 使用强类型 Preferences，不把空值或非法值静默折成默认值。v0.1 可配置在线 quota/reset credits、对应刷新周期、JSONL debounce、更新检查、UI 启动/概览范围和语言；语言支持 `system`、`zh-CN`、`en-US`，其中 `system` 按 macOS 首选语言自动解析。stable update channel 固定，自动下载保持关闭。保存使用 revision conflict 提示，不采用 last-writer-wins；切换恢复进行中时普通设置暂不可保存。

普通首次启动且不存在 Preferences 时，Go Helper 自动选择
`${CODEX_HOME:-$HOME/.codex}`。它先执行 metadata-only probe，再用相同
canonical path/device/inode 重新探测并原子保存；这个受信默认来源不要求用户点击
确认，且在线 quota/reset credits 初始均为开启、之后可分别关闭。候选缺失、不安全、探测中变化或
持久化结果不确定时不启动索引、不猜测替代目录，Settings 显示默认 Home 不可用。
已有 Preferences 永远优先，自动路径不能覆盖已有选择。

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
- 独立的“API 与订阅”页面：DeepSeek 显示官方余额、余额趋势和采样估算的总充值/总消耗，OpenCode Go 显示 5 小时、周、月额度；两者还共享一个使用 Codex 蓝色档位的 365 天活动热力图，但不进入 Agent Provider 选择器。
- 本地版本化 pricing catalog。
- 在线 quota / reset credits 作为默认开启、可随时关闭的实验性能力；始终显示来源、更新时间和失败降级状态。
- session index repair 仅 dry-run + 显式确认。

### M11 最终验收入口

v0.1 的最终集成验收以 [`docs/test/m11-e1.md`](../../../test/m11-e1.md) 为统一 runbook。该矩阵使用稳定场景 ID，把 Onboarding、索引、账本、Quota、UI、Tray、Health、更新、性能、隐私、辅助功能和发布就绪分别映射到 TOO-298～303；已有子 runbook 只作为实现证据入口，不替代当前主干上的 required live E2E。未执行、失败或缺少清理证据的 required 场景保持 blocking，不能用演示 fixture 或历史结果标记 M11 完成。

TOO-299 的性能验收见 [`docs/test/m11-e3.md`](../../../test/m11-e3.md)：首次初始化在无 pause、sleep、Home fence 或 pressure 时使用 interactive budget 连续推进，同时保持 backfill lane 与 live 抢占。约 6.56GB 真实只读 Home 的三轮 core bootstrap 均通过冻结门槛；该 runner 不经过 production scheduler，也不等同 packaged app 启动或视觉渲染，产品端到端 wall time 仍不得用该数值替代。

TOO-300 的隐私审计见 [`docs/test/m11-e4.md`](../../../test/m11-e4.md)：统一 contract 以 synthetic canary 穿过真实 parser、indexer、Store 与 backup，并静态审计 generated DTO、进程内 query cache、启动日志、tracked screenshot/workflow 和 packaged App。private Store 继续保存索引必需的路径 metadata；公共 DTO、日志、health、cache 与 artifact 递归禁止绝对路径、正文、凭据和 raw error。审计不读取真实用户内容，也不把 fixture 结果冒充真实数据功能验收。

GitHub Actions 当前按用户要求停用；最终验收使用本地 gate 并如实记录 `actions_disabled_by_user`。正式发布、tag/release、真实 appcast/密钥和外部分发仍需用户另行明确授权；未授权时发布卡只能收口到可复验的 release-readiness。

## 后续阶段

1. Codex-only 本地账本和工作台。
2. live 运行态：进程、端口、Git 状态和 PID 到 JSONL 的映射；不扩展成 Waiting/Blocked/Done Session 状态机。
3. 配额提醒：阈值、burn rate 和可信度状态栏文案。
4. 个人工作流：项目别名、Obsidian 摘要、高成本 Session 诊断、Tailscale 只读视图。
5. Grok 作为第三个独立客户端：本地 Session / Project / Token / Tool、账号胶囊与非稳定 credits 额度；显示名为“Grok”，不与 Cursor 内 Grok 模型对账。详见 [Agent Provider、Cursor 与 Grok](../providers/README.md)。

## 明确不做

- 不调用 `codex app-server` 查配额或作为兜底。
- 不把 `wham/*` 当稳定 API；v0.1 默认启用在线 quota 与 reset credits，但必须显示来源，允许用户随时关闭。
- 不复制原始 JSONL，不保存完整对话或工具输出。
- 不把内部 HTTP 接口当稳定 API。
- 不用一个全局周期反复全量扫描，也不在前台刷新时重复启动扫描。
- 不静默下载、强制安装或在 SQLite 事务未安全结束时重启。
- v0.1 不提供 Usage JSON/CSV、诊断包或其他用户数据导出。
- v0.1 只提供简体中文和英文资源，不承诺其他语言；新增文案必须进入 SwiftPM 的 `en.lproj` / `zh-hans.lproj` 资源，并通过 App Bundle 组装脚本复制到最终 `.app`。语言切换只影响 UI 展示、日期、数字和无障碍文案，不改变 Go Helper 的业务数据或 contract 语义。Token 紧凑数量在英文中使用十进制 `K/M/B`，在简体中文中使用 `万/亿`；英文计数文案必须区分单复数。
- 不在 v0.1 做 turn 完成通知、Attention、云同步或公网访问。
- 不复制 Codex Runway 的代码、图标或高度相似 UI。
