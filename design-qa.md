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

# 会话自适应趋势与项目每日趋势 Design QA

## 对照目标

- 每日趋势参考图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-aa4253e4-b01a-4aee-be9e-0d64c4ef99d8.png`
- 虚线选中态参考图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-7e834aaa-7f86-4b54-a87d-d480b0e4e092.png`
- 会话详情实现截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/session-project-daily-trend-session.png`
- 项目详情实现截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/session-project-daily-trend-project.png`
- 同画布对照图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/session-project-daily-trend-comparison.png`
- source pixels: 每日趋势 `864 x 316`；虚线选中态 `868 x 676`
- implementation pixels: 会话与项目窗口均为 `1124 x 768`
- evidence status: `pending`
- 历史证据边界：上述三张 `.artifacts/design-qa/session-project-daily-trend-*`
  文件生成于 2026-07-24，只能作为旧“每日趋势”视觉基线；不能证明本轮
  Session 小时路径、跨日切换、DST 重复小时、`usage-trend.selection-detail`
  Accessibility 读回或当前工作树的真实 Home 状态。

## Full-view comparison evidence

- pending：尚未生成当前工作树的 Session 同日小时趋势截图。
- pending：尚未生成当前工作树的 Session 跨日每日趋势截图。
- pending：尚未生成当前工作树的项目每日趋势截图。
- 2026-07-27 当前未提交工作树实际执行
  `CODEX_PULSE_APP_RUNTIME=/private/tmp/cp-codex-pulse-live-502 make verify-live`
  并读回 passed：`confirmed_home=real`、`primary_pages=loaded`、
  `sessions=20`、`details_read=5`、`unavailable=none`、`shutdown=clean`。
  这只证明真实 Home 功能链路，不证明小时/跨日视觉或 Accessibility。
- 自动化 contract/Store/Query/Swift 测试同样不替代原生窗口像素或
  Accessibility 验收。

## Focused region comparison evidence

- 旧 `session-project-daily-trend-comparison.png` 只证明 2026-07-24 每日趋势
  基线中的蓝色数据点、竖向虚线和图表下方日期，不证明自适应小时实现。
- pending：仓库当前没有与本轮实现对应的脱敏 Accessibility dump，不能把
  `usage-trend.selection-detail` 写成已读回。
- 自动化 Swift 测试只证明普通小时省略 offset、重复 DST 小时追加 offset
  以及每日 formatter 文案，不是
  Accessibility Inspector 或真实 UI 读回。

## Findings

- [implementation resolved / visual pending P1] 固定每日趋势无法表达单日变化
  - 实现：Helper 按请求 IANA timezone 返回小时或每日 bucket，Proto 传递
    `trend_granularity`；fallback aggregate 趋势保持 unavailable。
  - 待验：真实 Home 的小时路径与跨日路径像素证据。
- [implementation resolved / visual pending P1] DST 回拨小时不可区分
  - 实现：Go 保留真实 instant/offset，Query key 包含数值 UTC offset；
    Swift 普通小时省略 offset，只在同一墙钟时间存在多个实际 instant 时追加。
  - 待验：真实 UI 中两个重复墙钟小时的顺序、选中详情与 Accessibility 文案。
- [pending P2] 最新点默认选中、竖向虚线和详情布局
  - 旧每日截图可作为历史基线；本轮自适应路径尚未重新捕获。
- 当前不能据此断言“没有未解决的视觉问题”；视觉与 Accessibility 结论保持
  pending。

## Required fidelity surfaces

- Fonts and typography: pending（无本轮真实窗口截图）。
- Spacing and layout rhythm: pending（无本轮小时/跨日截图）。
- Colors and visual tokens: pending（旧每日基线不能证明当前工作树）。
- Image quality and asset fidelity: pending（无本轮新像素证据）。
- Copy and content: automated tests 与真实 Home 功能 smoke passed；真实
  UI/Accessibility readback pending。

## Comparison history

1. 2026-07-24 旧截图证明当时的每日趋势、默认选中态和竖向提示线。
2. 本轮实现引入 Session 小时/每日自适应、fallback unavailable 与 DST offset。
3. 在新截图、Accessibility dump 和真实 Home readback 产生前，不沿用旧 artifact
   作为本轮 passed 证据。

## Implementation checklist

- [x] 会话详情接入自适应小时/每日趋势数据
- [x] 会话与项目共用同一趋势组件
- [x] 最新一天默认选中
- [x] 显示竖向虚线
- [x] 图表下方显示完整日期和 Token 明细
- [x] DST 重复小时使用 offset 区分
- [x] 当前工作树真实 Home development App 功能读回
- [ ] 当前工作树同日小时路径截图
- [ ] 当前工作树跨日路径截图
- [ ] 当前工作树脱敏 Accessibility 读回

final result: pending
