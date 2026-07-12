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
| Projects | 如何按本地项目路径归因 token、成本和 Session | [06-projects.png](previews/06-projects.png) |
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
- shadcn-vue / Reka UI 只用于菜单、弹窗、Popover、Tooltip、Switch 等基础交互与可访问性行为，组件外观必须按本设计稿重做，不能直接使用默认主题替代设计。
- Apache ECharts 实现概览、Projects 和模型分布等数据可视化；图表 tooltip、legend、空数据和部分数据状态使用统一中文文案。
- Wails 原生窗口与平台 adapter 负责透明窗口、系统托盘和系统行为；Vue 组件不得直接依赖 macOS API。
- Reduce Transparency 下将玻璃替换为高不透明内容表面；Increase Contrast 下加强边界与文字对比；Reduce Motion 下取消非必要位移和缩放动画。

## 后续评审重点

本轮是高保真静态设计，尚未实现真实交互。图标方向和健康信息层级已经冻结；进入 Wails3 / AppKit 实现前，继续确认 Reduce Transparency 下的替代表面和 macOS-only 发布范围。
