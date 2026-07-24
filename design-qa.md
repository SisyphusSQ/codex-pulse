# Token 明细与状态栏 Design QA

## 对照目标

- 输入/输出参考图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-8a0af528-b23f-4867-a858-c77e45deaa07.png`
- 状态栏总量参考图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-b7b9e0be-6fc2-4b59-a1c0-6fde4902f45e.png`
- 概览对比图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/token-breakdown-refresh/reference-vs-overview.png`
- 状态栏详情上半部分：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/token-breakdown-refresh/status-popover-upper.jpeg`
- 状态栏详情成本与排行：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/token-breakdown-refresh/status-popover-total-ranking.jpeg`
- source pixels: 输入/输出参考图 `400 x 345`
- implementation pixels: 概览重点区域 `835 x 255`；状态栏详情窗口 `420 x 672`
- state: 真实 Codex Home、周额度周期、原生浅色外观、动态本机数据

## Full-view comparison evidence

- 使用真实 Home 启动 development App，检查概览、会话列表、会话详情和状态栏详情。
- 状态栏详情通过与正式 `NSPopover` 共用的 `MenuBarPopoverView` 在 `420 x 640` points 标准窗口中捕获；验收辅助入口未保留在交付代码中。
- 状态栏菜单项通过 macOS Accessibility 读回为 `Codex Pulse · 周剩 0%，已用 12.5亿 Token`，保持原来的总量单行样式。

## Focused region comparison evidence

- `reference-vs-overview.png` 把用户的输入/输出/总量参考图与概览实现放在同一张图中检查。
- 概览、会话和成本卡片均区分输入、输出、总量；缓存作为输入子项，推理作为输出子项。
- `status-popover-total-ranking.jpeg` 显示项目排行仅保留总量，未把输入/输出塞进窄排行列表。
- 状态栏每日趋势悬停详情位于图表下方，避免遮挡柱状图。

## Findings

- [resolved P1] 旧验收无法捕获原生弹窗
  - 旧证据：离屏缓存未合成文字和系统材质，系统窗口截图被 Screen Recording 权限阻断。
  - 处理：用相同 `MenuBarPopoverView` 和真实数据建立临时标准窗口，获得完整像素证据后移除验收入口。
- [resolved P2] 状态栏项目排行输入/输出换行拥挤
  - 证据：`420` points 窄窗口中三组明细会压缩成多行。
  - 处理：按用户确认恢复为项目总量单值；成本卡片仍保留输入/输出明细。
- 当前没有未解决的 P0、P1 或 P2 视觉问题。

## Required fidelity surfaces

- Fonts and typography: passed，沿用系统字体、现有字号和字重。
- Spacing and layout rhythm: passed，主窗口明细列与状态栏成本卡片对齐；排行恢复为单值后无拥挤换行。
- Colors and visual tokens: passed，沿用原生系统材质、现有强调色和图表色。
- Image quality and asset fidelity: passed，界面仅使用 SF Symbols，无新增外部位图资产。
- Copy and content: passed，菜单栏使用“已用总量”，详情面按确认范围展示输入/输出。

## Comparison history

1. 旧状态栏弹窗离屏捕获只保留状态色图形，判定为无效视觉证据。
2. 本轮真实窗口捕获确认概览和会话 Token 明细可读。
3. 第一次状态栏详情捕获发现项目排行拆分后过于拥挤。
4. 按用户确认将排行恢复为总量，第二次捕获确认布局清晰。
5. Accessibility 读回确认状态栏菜单项恢复“周剩 … / 已用 …”原样。

## Implementation checklist

- [x] 概览、会话、项目、额度与用量的 Token 明细区分输入/输出/总量
- [x] 缓存归于输入子项，推理归于输出子项
- [x] 状态栏成本卡片保留输入/输出明细
- [x] 状态栏菜单项保持总量样式
- [x] 状态栏项目排行只显示总量
- [x] 状态栏趋势悬停详情放在图表下方
- [x] 真实 Home development App 视觉检查

final result: passed

# 会话与项目每日趋势 Design QA

## 对照目标

- 每日趋势参考图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-aa4253e4-b01a-4aee-be9e-0d64c4ef99d8.png`
- 虚线选中态参考图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-7e834aaa-7f86-4b54-a87d-d480b0e4e092.png`
- 会话详情实现截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/session-project-daily-trend-session.png`
- 项目详情实现截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/session-project-daily-trend-project.png`
- 同画布对照图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/session-project-daily-trend-comparison.png`
- source pixels: 每日趋势 `864 x 316`；虚线选中态 `868 x 676`
- implementation pixels: 会话与项目窗口均为 `1124 x 768`
- state: 真实 Codex Home、原生浅色外观、最新日期默认选中

## Full-view comparison evidence

- 会话与项目详情均使用同一个每日趋势组件，延续现有卡片、系统字体、蓝色折线和右侧 Token 轴。
- 会话详情真实数据只有一个每日桶时，仍显示数据点、贯穿绘图区的竖向虚线和图表下方日期详情。
- 项目详情存在多个每日桶时，默认选中最新一天；后续可通过图表横向选择切换日期。

## Focused region comparison evidence

- `session-project-daily-trend-comparison.png` 把两张用户参考图和会话实现的选中态放在同一画布检查。
- 实现与参考图一致保留蓝色数据点和竖向虚线；虚线使用次级色，避免压过趋势主线。
- 图表下方明确显示 `2026年7月24日`，随后展示输入、输出、总量、缓存和推理明细。
- Accessibility 读回在会话和项目详情中均包含 `daily-trend.selection-detail` 和完整日期。

## Findings

- [resolved P1] 会话详情原来没有每日趋势数据契约
  - 处理：从 Helper 的 light daily index 返回会话每日桶，并经 Proto 传给 Swift App。
- [resolved P1] 选中态只能在指针交互后出现，不利于首次读取
  - 处理：默认选中最新一天，因此首屏即可看到虚线和日期；横向选择仍可更新选中日期。
- [resolved P2] light daily 桶误带活动时间会触发详情 unavailable
  - 处理：每日桶只携带可信 Token 事实，不伪造 turn 活动时间；真实 Home smoke 已恢复 `unavailable=none`。
- 当前没有未解决的 P0、P1 或 P2 视觉问题。

## Required fidelity surfaces

- Fonts and typography: passed，沿用 macOS 系统字体和现有图表字号。
- Spacing and layout rhythm: passed，日期详情位于图表下方，不遮挡数据点或坐标轴。
- Colors and visual tokens: passed，沿用现有 tint 折线和 secondary 虚线。
- Image quality and asset fidelity: passed，无新增图片资产。
- Copy and content: passed，会话与项目均显示完整中文日期和 Token 明细。

## Comparison history

1. 首次实现仅在指针选择后显示虚线，自动化截图无法稳定进入选中态。
2. 改为默认选中最新一天，真实会话和项目详情首屏均出现虚线与日期。
3. 同画布对照确认参考图要求的竖向提示线和图表下方日期均已落地。

## Implementation checklist

- [x] 会话详情接入每日趋势数据
- [x] 会话与项目共用同一趋势组件
- [x] 最新一天默认选中
- [x] 显示竖向虚线
- [x] 图表下方显示完整日期和 Token 明细
- [x] 真实 Home development App 视觉检查

final result: passed
