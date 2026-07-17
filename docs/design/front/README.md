# Codex Pulse Design

适用机器：`sqmc04`（设计稿制作与验证）；产品设计本身面向 macOS。

## 交付物

- [codex-pulse-liquid-glass.pen](codex-pulse-liquid-glass.pen)：Pencil 可编辑源文件。
- [previews/](previews/)：按页面导出的 PNG 预览。
- [assets/icons/](assets/icons/)：03“深空控制台”正式应用图标与状态栏模板 PNG。

当前 Pencil 画布与 PNG 预览已经统一为 `Codex Pulse`。正式图标采用 03“深空控制台”，概览、本机状态、Popover、各页面侧栏与 Tray 均使用同一套品牌语言。

## 页面清单

| 页面 | 主要回答的问题 | 预览 |
| --- | --- | --- |
| Design System | Liquid Glass、颜色、状态和操作语言如何统一 | [00-design-system.png](previews/00-design-system.png) |
| Icon System | 03“深空控制台”如何统一应用图标、小尺寸图标与状态栏模板 | [09-icon-system-exploration.png](previews/09-icon-system-exploration.png) |
| 本机状态 | 数据是否完整、索引是否新鲜、后台任务和数据源是否正常 | [01-local-status.png](previews/01-local-status.png) |
| Popover | 不进入复杂分析时，如何快速查看额度、Reset credits、API 等价成本和最近会话 | [02-popover-overview.png](previews/02-popover-overview.png) |
| Tray | fresh / stale / conflict / unknown / exhausted 如何一眼区分 | [03-tray-states.png](previews/03-tray-states.png) |
| 概览 | 5 小时 / 本周额度、7 天 token、缓存、输出和 API 等价成本如何一起判断 | [04-overview.png](previews/04-overview.png) |
| Sessions | Session 列表与 active turn 元数据如何同时查看 | [05-sessions.png](previews/05-sessions.png) |
| Projects | 如何按安全项目归因查看 token、API 等价成本和 Session | [06-projects.png](previews/06-projects.png) |
| Quota | remaining、reset、来源、last-known-good 和实验能力如何解释 | [07-quota.png](previews/07-quota.png) |
| Settings | Codex home、索引、隐私、在线能力和更新如何配置 | [08-settings.png](previews/08-settings.png) |
| Data Health | 当前问题影响什么、系统如何保护数据、用户如何恢复 | [10-data-health.png](previews/10-data-health.png) |

## 视觉方向

采用 Apple Liquid Glass 的层级原则，而不是把所有内容都做成半透明卡片：

- 玻璃主要用于浮动导航、Popover、工具栏和关键操作，形成独立的功能层。
- 数据主体使用接近实色的内容表面，保证数字、表格和长路径在复杂背景上仍可读。
- 内容延伸到 inset sidebar 后方；柔和环境光只用于建立空间关系，不承载语义。
- 主要动作使用单一系统蓝 tint；绿色、橙色、红色只表达可信、警告和确认异常。
- 圆角遵循同心关系：窗口 30～32、内容区 22～24、控件 13～17。
- 动效实现时应支持 Reduce Transparency、Increase Contrast 和 Reduce Motion。

## 产品语义

- 百分比始终表示 `remaining`。
- 所有额度只显示普通百分比，不使用 `≤`、`?` 或状态胶囊解释不确定性；存在 last-known-good 时继续显示当前选定值，从未取得数值时显示 `--`。
- 状态栏使用 Codex Pulse 单色模板图标 + 双行额度仪表；“5 小时”和“本周”各自保留短进度条与百分比，并提供浅色、深色和异常状态适配。
- Popover 删除来源冲突、网络失败和索引异常说明区，使用数值与进度条直接表达 remaining。
- Popover 增加 Reset credits 摘要，展示可用次数、总次数、累计剩余时间和最近到期时间。
- Popover 增加今日 API 等价成本摘要，展示金额、token、周期和计算时间；最近会话扩展到 5 条。
- `API 等价成本` 始终带估算说明，不写成真实花费。
- 在线 quota 和 reset credits 默认开启、可随时关闭，并继续明确标记 `EXPERIMENTAL`；界面始终显示来源、更新时间与失败降级状态。
- Session 只表达 `ACTIVE / IDLE`，不引入 Waiting、Blocked、Done 状态机。
- v0.1 不提供导出功能；概览、Data Health 和 Settings 均不显示导出入口。
- 主侧栏统一使用“概览 / 会话 / 项目 / 配额 / 本机状态 / 设置”。概览为默认落地页，本机状态位于设置上方。v0.1 只交付 `zh-CN`，不展示语言切换；实现仍使用 i18n message key 和集中语言目录。
- 原始对话和工具输出不进入页面；绝对路径可复制，但设计中的路径与数据均为演示值。
- Tray 健康点与额度颜色分离：blocked 立即显示红点；degraded 持续 2 分钟且影响可信度或需要处理时显示橙点。
- 本机状态同一时间最多显示一个条件 Banner；历史补齐使用蓝色信息级，持续 degraded 使用橙色，blocked 使用红色。
- Data Health 是本机状态下钻的二级页面，不增加独立主导航项；页面先解释影响和保护措施，再展示领域、任务、事件、资源和恢复动作。

## Query DTO 消费边界

- 前端业务查询只能导入 `frontend/bindings` 中由 Go 签名生成的 `internal/app/service` 与 reachable models；禁止手写同名 request/response/enum/error shadow type，也禁止绕过 façade 调用 Repository、SQLite、文件、shell、网络或 credential primitive。
- `wails-bindings-v1` 当前固定为 15 个只读方法：Bootstrap、Contracts、Usage/Session/Project、Quota、Source、Job、Health 与 Settings 查询；`commandMethods` 必须是显式空数组。Preferences、Schedule、Codex Home 切换和 recovery executor 在后续独立 command contract 完成前不得从组件调用。
- 13 个业务数据 query 返回 `CancellablePromise<T>` 且首个 Go 参数为 context。页面卸载、query 被替代或用户取消时应调用生成 client 的取消能力，不另造不可取消 Promise wrapper；Go 侧的 cancel/deadline 继续映射为 typed error code。`Bootstrap` / `Contracts` 是同步元数据方法，不依赖其取消来释放后端工作。
- 业务 error 与 recovered panic 的 Wails `RuntimeError.message` 固定为 `binding query failed`；页面只从 `cause` 解码 `query.ErrorEnvelope` 并以有限 `messageKey` 渲染。参数/JSON 错误由 Wails 标记为 `TypeError`，其 framework message 同样不得展示。无法识别的 kind/version/code 必须按 internal fail closed，不能显示底层 message、路径、请求参数、panic value 或 driver cause。
- 后续 Wails bindings 只暴露 Go `query-v1` 与各业务 query service 组合出的非泛型 DTO；组件不接收 GORM model、SQLite row、SQL 字段或任意 map。
- request 的 page/sort/filter/time range 必须先经 Go endpoint specification allowlist。前端把 cursor 当 opaque token；不得解析 cursor、猜测数据库 offset 或请求无限列表。
- token、count 与 API 等价成本使用 Go 校验后的整数 DTO；微美元只在 locale formatter 转成展示金额，不在 Vue 中重新计算或用浮点累计。超出 JavaScript safe integer 的事实由 Go fail closed，不能静默舍入。
- 本地日期选择传递 `YYYY-MM-DD`、exclusive end 和 IANA timezone；UTC 边界由 Go 计算，前端不得用固定 24 小时或固定 offset 猜测 DST 日期。
- 已知空集合固定为 `[]`；真实数值 `0` 保留为 `0`；unknown 使用 `null + unknownReason` 并展示 `--`。partial 保留可用数据并局部提示，不切换全局 loading；unavailable 使用稳定 error code/message key，不显示底层 error text。
- Go 返回的 message key 只用于 Vue I18n 查找 `zh-CN` 文案；业务 service 和组件都不得把用户路径、凭证、原始响应或 driver 错误塞入 message key/field。
- 概览只消费 Go 已聚合的 day/week/month trend、pricing evidence 和 totals；Vue 不从 daily rows 再算周/月、cost 或 priced subtotal。`rollup_missing` 时保留可用 token 并局部显示部分数据，不自行补价格。
- Sessions 组件只消费安全 title/project/model、`ACTIVE / IDLE`、last activity 与 Go totals；不得读取或推导 cwd/root path/raw model，也不得用 offset、数组下标或 Session ID 猜下一页。
- Session 详情 Turn 区只消费 generated `turnPage/turns`：按服务端顺序展示 active/complete、时间、safe model、usage 与 pricing evidence；lifecycle 不提供不可达的 unknown，数值 unknown 继续显示 `--`。`timelineKey` 只作稳定渲染 key，AEAD cursor 只原样回传且进程重启后不得复用。不得从页面 aggregate 重算 Turn cost，不得展示或推导 raw Session/Turn ID、正文事件、tool、路径、offset 或 generation；fallback cost unknown 与明确 unpriced 必须分开展示。
- Session 的 `rollup_missing` 与 `rollup_ambiguous` 都展示局部 partial，不把 unknown totals 渲染成 `0`；后者表示调用未指定 reporting timezone 且 Go 发现多个 active generation，前端不得自行选择 timezone 或 ledger 重试。
- Projects 组件必须保留 unknown/conflict/invalid dimension 行，分别展示 global/matched/page totals；confidence filter 使用 Go 返回的 range-level confidence。详情 daily 只用于下钻展示，前端不得重算并覆盖 list totals 或绕过 reconciliation failure。
- Projects 列表直接消费 generated `sessionCount/trend`；trend 是所选 range 末尾最多 30 个已有日 bucket，不补零、不延伸、不代替 full totals。Project 详情的 `sessionPage/sessions` 与 `modelPage/models` 各自保持服务端顺序和 opaque cursor；两类 process-key cursor 不解析、不持久化，并绑定 active generation，Project/range 变化、同进程 generation rollover 或进程重启后均从首页恢复。页面不得复用 `ListSessions(projectId)` 冒充 Project contribution，也不得用当前页重算 Session/Model 总量。
- active Project rollup 不可用是 fatal unavailable，不渲染“0 个项目”；active rollup 下的真实空 range 才渲染 known-empty。Session 缺 rollup 则是 partial，两条路径不得共用同一个空态。
- Quota 直接消费既有 `quota-current-v1`，外层统一使用 `query-v1` meta；Source、Job、Health 与 Settings 消费 `runtime-info-v1` typed DTO。前端不得读取 `source_files`、`source_state`、`job_runs`、`health_events`、scheduler 或 Preferences persistence model。
- Source 列表把本机文件与在线来源分别映射为 `local_file:<opaque-id>`、`online:<opaque-id>`，按 `updatedAt + sourceKey` 稳定分页；只展示 provider/source type、state/freshness、字节进度、last attempt/success、next due、有限 error/failure code 和恢复动作，不返回当前路径、device/inode、scope、request/payload identity。任一来源种类读取失败时保留另一种的可用结果，并通过 `partial + unavailableKinds` 显式说明；调用方所选种类全部失败时才返回 fatal unavailable。
- Job 只展示稳定 job identity、state/phase、进度、时间、失败计数、next retry 和 typed recovery action；resume cursor、scheduler task ID 与内部 dedupe key 不进入 DTO。Health 只展示 event/domain/severity/code、active/resolved、occurrence、last seen 和安全关联，不返回 fingerprint 或底层 error text。
- Health 当前级别只聚合 active events：resolved critical 仍保留历史计数，但不能让当前状态永久 `blocked`。`paused` 只来自 durable user pause 或 system sleeping；`blocked/degraded/busy` 再按 lifecycle 与 active critical/error/warning 映射。
- Settings 将 revision/Home generation 作为十进制字符串返回，并把 snooze/last-check 映射为 JS-safe numeric value；Home path、data store key、device/inode、detached Home、switch/attempt ID 永不进入响应。可编辑字段由 Go 返回固定 type/min/max/options metadata，固定 `zh-CN`、stable channel 与关闭 auto-download 明确标记为只读。
- recovery action 只允许 `none/retry/check_source/grant_permission/free_space/choose_home/repair_store`，且非 `none` 必须引用 Go contract 中的固定 command key；typed error、failure 与 health code 先按完整有限矩阵决定动作，state/attention 仅作为没有 code 的 fallback。query service 只返回引用，不执行 command、不写设置、不修库。

## Wails Event 与 Query Cache 边界

- custom event 只有 `codex-pulse:query-invalidated`，typed payload 只含 `query-invalidation-v1` 和 `index/quota/health/settings`；组件不得从事件读取 session、quota、health 或 settings 事实。
- 13 个业务 query 使用共享 key factory。业务 request 必须完整进入 key；Quota current 使用稳定singleton key且每次fetch重新读取当前时刻。每个queryFn必须以`cancelOn(signal)`连接TanStack AbortSignal与generated CancellablePromise，observer卸载或查询替换时取消Go查询。usage/session/project 的 stale time与active refetch interval为15秒，quota/source/job/health为5秒，settings为60秒；background interval关闭。Bootstrap是进程静态元数据，保持永久新鲜且不参与业务invalidation。
- `index` 失效 usage/sessions/projects/sources/jobs/health；`quota` 失效 quota/sources/health；`health` 只失效 health；`settings` 失效 settings/quota/sources。失效只按这些 root 执行，不扫描或解释任意 payload 字段。
- event storm 与重复事件在 50ms 内合并，同一 root 每批最多失效一次；使用 `refetchType: active`，只让当前可见查询主动重取，inactive cache 只标记 stale。
- event handler 禁止 `setQueryData`、optimistic copy 或自行合并业务对象。Go query/SQLite/Preferences 始终是唯一事实源。
- event 丢失或断连不影响正确性：持续前台active query按interval有界重取，inactive query重新观察时按stale状态重取；system wake、window runtime ready、macOS foreground与malformed/未知event都触发全业务root invalidate。应用卸载必须释放全部Wails subscription、取消pending timer，observer卸载后停止周期刷新。

## 图标规范

- 正式方向：03“深空控制台”。
- 应用图标：深色光学镜片、蓝紫轨道、`>_` 终端光标和右上运行信标。
- 64 / 32 / 16px：保留深空配色与终端光标，减少轨道、反射和景深细节。
- 状态栏：使用 19px 单色 `>_` 终端模板；正常、stale、conflict、unknown 和 exhausted 只改变系统模板色与额度语义，不更换品牌轮廓。
- PNG 仅作为设计与实现交接资产；正式 macOS AppIcon / `.icns` 在代码仓库中从 Pencil 源稿生成并按 Apple 安全区复核。

## Apple 设计依据

- [Meet Liquid Glass](https://developer.apple.com/videos/play/wwdc2025/219/)
- [Get to know the new design system](https://developer.apple.com/videos/play/wwdc2025/356/)
- [Build an AppKit app with the new design](https://developer.apple.com/videos/play/wwdc2025/310/)
- [Human Interface Guidelines: Sidebars](https://developer.apple.com/design/human-interface-guidelines/sidebars)

## 前端实现映射

- 实现栈固定为 Vue 3 + TypeScript + Vite，运行于 Wails3；Tailwind CSS v4 承载颜色、圆角、间距、玻璃表面和状态 token。
- TOO-272 已把 `surface-base/content/glass`、文字层级、状态色、同心圆角、阴影、focus ring 与 motion 收敛为 `frontend/src/styles.css` 的语义 token；`AppShell` 组合 240px 侧栏、标题区与内容面，`/` 和未知地址均归一到 `/overview`。
- 主导航固定为六个可键盘访问的 router link；应用图标直接复用 `assets/icons/codex-pulse-app-icon-64.png`。普通线性图标锁定 `@lucide/vue@1.23.0`；已弃用的 `lucide-vue-next` 不再作为实现依赖。
- `UiButton`、`UiCard`、`UiTable`、`StateEmpty`、`StateError`、`StateSkeleton` 是 M7 页面共享基础组件；表格由调用方提供稳定 `rowKey`，loading/error/empty 都保留明确语义和辅助技术状态。
- v0.1 仅启用 `zh-CN`，所有生产可见中文集中在 `frontend/src/i18n/messages/zh-CN.ts`；number/dateTime/relativeTime 使用同一 locale formatter，测试会拒绝生产 Vue/TS 中新增中文硬编码。
- 共享壳只读取真实 Wails `Bootstrap` 元数据。浏览器脱离 Wails 时展示可重试 error state；打包版连接成功后展示 `darwin` 与 `zh-CN`，不得为后续页面伪造 quota、usage 或 health 数值。
- shadcn-vue / Reka UI 只用于菜单、弹窗、Popover、Tooltip、Switch 等基础交互与可访问性行为，组件外观必须按本设计稿重做，不能直接使用默认主题替代设计。
- Apache ECharts 实现概览、Projects 和模型分布等数据可视化；图表 tooltip、legend、空数据和部分数据状态使用统一中文文案。
- Wails 原生窗口与平台 adapter 负责透明窗口、系统托盘和系统行为；Vue 组件不得直接依赖 macOS API。
- Reduce Transparency 下将玻璃替换为高不透明内容表面；Increase Contrast 下加强边界与文字对比；Reduce Motion 下取消非必要位移和缩放动画。

TOO-272 的视觉 QA 以 `00-design-system.png`、`04-overview.png` 与 `01-local-status.png` 为 source truth，在 900×600、1120×720、1280×770 与 1440×1024 检查窗口、侧栏和内容层级。浏览器比较证据保存在本地 ignored run 目录；可复现步骤和通过摘要见 `docs/test/m7-e1.md`，逐项比较结论见仓库根目录 `design-qa.md`。

TOO-273 已把 `/overview` 映射为真实 query-v1 页面：顶部两个紧凑 quota remaining 卡直接显示 reset/source/freshness 与 Reset credits；趋势、范围选择、Token 构成、API 等价成本和每日明细严格沿用 `04-overview.png` 首屏顺序。最近 Session/Project 与索引/健康属于 Linear 追加验收，位于每日明细之后的下方滚动区，不改变冻结首屏。

概览范围固定为今天、近 7 天、近 30 天和自定义本地日半开区间；UTC 归一化仍由 Go 负责。页面只消费 generated Usage/Quota/Session/Project/Source/Health DTO，保留 unknown、真实 0、partial、stale、known empty 与 fatal unavailable，不从 daily rows 重算总量或成本。`echarts@6.1.0` 通过模块化 core/charts/components/CanvasRenderer 接入，注册 Aria/decal 并服从 Reduce Motion；TanStack Query 继续是唯一 cache/cancel/invalidation owner。

TOO-273 的 1440×1024 normal-state 视觉 QA 使用不进入产品路径的隔离 typed DTO cache，source 与 implementation 以同 viewport 合并比较；QA 夹具在验证后删除，只保留 ignored screenshot/comparison。本次可复现步骤和脱敏结果见 `docs/test/m7-e2.md`，逐项结论见根目录 `design-qa.md`。

TOO-307 已为 TOO-274 提供 content-free Session Turn usage/cost provider contract：沿用现有 `SessionDetail` 方法、TanStack cancel/cache/invalidation owner 与 15 秒 active refetch，只新增 bounded generated page。可复现的 synthetic Store/query/generated 验证见 `docs/test/session-turn-timeline.md`；Sessions 页面不得绕开该 provider 读取 SQLite 或构造演示事件。

TOO-308 已为 TOO-275 冻结 Project Session/Model drill-down provider contract：沿用 `ListProjects/ProjectDetail`，提供 exact SessionCount、30-bucket trend suffix、双 bounded contribution page 与 range/generation-bound AEAD cursor，不增加 Wails method。可复现的 synthetic GORM/Store/query/generated 验证见 `docs/test/project-session-model-drilldown.md`；Projects 页面不得绕开该 provider 读取 SQLite 或在浏览器聚合业务事实。

TOO-274 已把 `/sessions` 映射为真实 query-v1 页面：activity、时间、project、model 与排序都是具名有限控件；列表保持服务端稳定顺序和 opaque keyset cursor，筛选或排序变化清空 cursor 与 selection。当前 provider 没有全文搜索 contract，因此不实现设计稿中的伪本地搜索；选项只来自当前安全结果与 URL 已选值，不声称覆盖全库。

Session 详情只展示 safe attribution、activity、aggregate、pricing evidence 与 content-free Turn usage/cost timeline。列表 cursor 原样进入 URL 以便恢复；process-key Turn AEAD cursor 只保留在当前 UI 生命周期，切换 Session 或进程重启即清空，不解析、不持久化。页面保留 unknown、真实 0、partial/stale、known empty、fatal、not-found 与 cursor recovery，任何 prompt/response/tool output/path/raw error/opaque identity 都不得进入 DOM。

TOO-274 的 browser visual QA 以 `05-sessions.png` 为 source truth：1440×1024 normal-state 与 source 已在一个横向 comparison 输入中复核，900×600 堆叠态无页面级水平溢出且 console error=0；临时 typed DTO 夹具验证后删除，仅保留 ignored evidence。可复现步骤与当前门禁状态见 `docs/test/m7-e3.md`，逐项结论见根目录 `design-qa.md`；packaged 1120×720 结论必须在原生隔离验证完成后回写。

## 后续评审重点

TOO-272 已实现共享应用壳、路由、基础状态交互和 Wails Bootstrap ready/error/retry；TOO-273 已实现概览，TOO-274 已实现 Sessions 列表与详情并进入独立 review/集成门禁。Projects、Quota、本机状态与 Settings 的独立页面仍由后续卡完成。图标方向和健康信息层级继续冻结；后续页面必须复用当前 token、query-state、辅助模式降级与 macOS-only 边界。
