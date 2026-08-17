# Cursor 状态栏月额度摘要 Design QA

## 对照目标

- 用户提供的旧状态：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-f6e43d9b-79f2-437f-a1a5-0514900bdae9.png`
- 新状态栏截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/cursor-status-monthly-summary-focus.png`
- 同画布对照图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/cursor-status-monthly-summary-comparison.png`
- source pixels: `224 x 60`
- implementation pixels: `224 x 60`
- viewport: 菜单栏组件 `112 x 30` points，macOS `@2x`
- density normalization: source 与实现均归一化为 `224 x 60` pixels，无额外缩放
- state: Cursor 状态栏 Provider、月额度、剩余 `90% / 100%`、本周期累计 `2.1亿 Token`、浅色菜单栏

## Full-view comparison evidence

- 对照目标就是完整菜单栏组件；`cursor-status-monthly-summary-comparison.png` 在同一画布中展示旧状态与新状态。
- 新状态使用与 Codex 相同的额度环、上下两行文本和语义颜色，不再显示 terminal 图标或“今日次数”。
- 顶行显示“月剩 90% · 100%”，底行显示“已用 2.1亿”。

## Focused region comparison evidence

- 完整组件只有 `224 x 60` pixels，百分比、Token 总量和额度环均已清晰可读，因此无需再做更小裁切。
- live smoke 同时读回 Cursor 状态栏 Accessibility 摘要，并确认账号、弹窗与真实月额度数据链路可用。

## Findings

- [resolved P1] Cursor 状态栏仍展示“今日次数 / 今日 Token”，与 Codex 的额度周期摘要语义不一致。
  - 处理：Cursor 改用正式额度摘要；顶行组合两个官方月度窗口的剩余百分比，底行使用月额度范围内的总 Token，并复用 Codex 的“已用”格式。
- [resolved P2] Cursor fallback 使用 terminal 图标，无法表达额度健康状态。
  - 处理：有月额度事实时复用 Codex 的额度环；加载阶段保留“月剩 -- / 已用 --”结构，不再退回今日活动文案。
- 当前没有未解决的 P0、P1 或 P2 视觉问题。

## Required fidelity surfaces

- Fonts and typography: passed，复用 Codex 状态栏的系统字体、字号、字重和两行基线。
- Spacing and layout rhythm: passed，图标、文本间距和 `112 x 30` points 组件尺寸与 Codex 一致，无截断。
- Colors and visual tokens: passed，额度环继续使用健康绿色，文字使用系统 label/secondary label 色。
- Image quality and asset fidelity: passed，额度环由现有原生 AppKit 绘制，无新增图片或替代资产。
- Copy and content: passed，顶行是“月剩 90% · 100%”，底行是月额度累计“已用 2.1亿”。

## Comparison history

1. 用户截图确认旧实现显示“今日 21 次 / 30.6M Token”和 terminal 图标。
2. 新增失败测试，旧实现因不能生成“月剩 90% · 100%”而按预期失败。
3. Cursor 改用月额度摘要，测试转为通过。
4. 重新构建并在隔离真实 Home App 中截取 `224 x 60` 菜单栏组件。
5. 同画布对照确认额度环、双百分比和月度 Token 总量清晰，live smoke 通过。

## Implementation checklist

- [x] 顶行展示两个官方 Cursor 月度剩余百分比
- [x] 底行展示月额度范围累计 Token
- [x] 文案、格式和额度环复用 Codex 状态栏语义
- [x] 加载 fallback 不再显示今日次数
- [x] Swift deterministic tests 与真实 Home live smoke 通过

final result: passed

# Cursor 配额使用率绿色 Design QA

## 对照目标

- 用户参考图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-97b8aad4-9e21-4218-858b-1ff8efd6ba03.png`
- 原生窗口截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/cursor-quota-green.png`
- 配额区域裁切：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/cursor-quota-green-focus.png`
- 同画布对照图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/cursor-quota-green-comparison.png`
- source pixels: `760 x 167`
- implementation pixels: 原生窗口 `1124 x 768`；聚焦裁切 `760 x 167`
- viewport: `1124 x 768` points；聚焦区域按 source 的 `760 x 167` 像素规格归一化
- density normalization: source 与聚焦实现均按 `760 x 167` 像素直接比较，无额外缩放
- state: Cursor 概览、月额度、浅色外观、真实 Codex Home、复制后的私有 runtime DB

## Full-view comparison evidence

- 原生窗口保留 Cursor 概览既有布局、额度文案、数值和两个进度条的位置。
- Accessibility 读回两个 `cursor.overview.quota` 进度条，数值分别为约 `10%` 和 `0%`。
- 主窗口截图确认两条进度条均使用与 Codex 健康额度一致的绿色语义色。

## Focused region comparison evidence

- `cursor-quota-green-comparison.png` 将用户提供的蓝色旧状态与绿色实现放在同一画布中。
- 聚焦裁切与 source 同为 `760 x 167`，两条额度进度条清晰可辨，因此无需额外更小裁切。

## Findings

- [resolved P2] Cursor 概览的两条使用率进度条仍为蓝色，与 Codex 健康额度色不一致。
  - 处理：只将 Cursor 额度条的 tint 从 `.blue` 改为 `.green`；数据、文案、布局和其他图表配色不变。
- 当前没有未解决的 P0、P1 或 P2 视觉问题。

## Required fidelity surfaces

- Fonts and typography: passed，字体、字号、字重和截断行为未改动。
- Spacing and layout rhythm: passed，卡片尺寸、分栏、间距和圆角未改动。
- Colors and visual tokens: passed，两条使用率进度条均改为 Codex 健康额度使用的绿色。
- Image quality and asset fidelity: passed，无新增或替换图片资产，继续使用原生 SwiftUI 控件和 SF Symbols。
- Copy and content: passed，标题、百分比、重置时间和预测文案保持原样。

## Comparison history

1. 用户参考图显示两条 Cursor 使用率进度条为蓝色。
2. 将 `CursorOverviewContentView` 中唯一对应的额度条 tint 改为绿色。
3. 重新构建隔离开发 App，并在相同 Cursor 概览状态捕获原生窗口。
4. 同画布对照确认两条进度条均为绿色，且没有引入布局或文案变化。

## Implementation checklist

- [x] Cursor Models 使用率进度条改为绿色
- [x] Other Models 使用率进度条改为绿色
- [x] 保持数值、布局、文案及其他图表配色不变
- [x] 隔离 development App 原生窗口视觉检查

final result: passed

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

# Cursor 模型粒度、账号胶囊与本地优先 Design QA

## 对照目标

- 用户参考图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-a0c1a1a1-8012-459d-9ef2-e5250b395a8d.png`
- Cursor Popover 实现：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/cursor-local-first-popover.png`
- 模型趋势同画布对照：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/cursor-model-trend-comparison.png`
- source pixels: `878 x 688`
- implementation pixels: Popover `920 x 1726`；趋势对照使用同宽裁切
- state: 真实 Codex Home、真实 Cursor 本地状态、复制后的私有 runtime DB、浅色外观

## Full-view comparison evidence

- Cursor Popover 继续复用既有原生材质、额度卡、今日使用卡、每日趋势卡与账期消费卡。
- 顶部账号/套餐胶囊与 Codex 位于同一位置；提交的截图证据按隐私约定隐藏账号内容，live smoke 同时读回 `cursor_account=available`。
- “今日活动”卡片已移除，今日请求和 Token 保留在单一“今日使用”卡片中。

## Focused region comparison evidence

- 参考图中的“全部模型”聚合已替换为两个真实 Cursor model bucket：`cursor-grok-4.6-xhigh` 与 `cursor-grok-4.6-xhigh-fast`。
- 每日柱状图按模型堆叠；窄 Popover 使用单列完整模型名，避免两个长名称在相同前缀处截断而无法区分。
- Go query test 验证每个模型都带与总趋势同粒度的 daily bucket；Swift resolver 只在模型 bucket 无法与总量核对时保留诚实降级。

## Findings

- [resolved P1] Cursor 每次查询同步等待 Dashboard API，页面和菜单栏展开缓慢
  - 处理：本地 collector/snapshot 先返回，Dashboard 使用 single-flight 后台刷新；提交后发 invalidation 让 UI 重查本地缓存。
- [resolved P1] Cursor 趋势只能显示“全部模型”
  - 处理：Dashboard 与本地 usage 都返回 per-model trend；真实数据读回两个模型并在柱状图中分段。
- [resolved P1] Cursor Popover 缺少账号和订阅等级
  - 处理：Helper 仅从本地 Cursor state 白名单读取邮箱、membership 与 subscription status；真实会员类型 `pro_plus` 显示为 `Pro+`。
- [resolved P2] “今日活动”重复占用纵向空间
  - 处理：移除该卡片，保留“今日使用”与趋势/账期事实。
- 当前没有未解决的 P0、P1 或 P2 视觉问题。

## Required fidelity surfaces

- Fonts and typography: passed，沿用系统字体、圆角数字和既有层级。
- Spacing and layout rhythm: passed，移除重复卡片后区块间距一致。
- Colors and visual tokens: passed，沿用现有蓝/绿图表色、橙色账号胶囊和系统材质。
- Image quality and asset fidelity: passed，仅使用 SF Symbols 与原生 Charts，无新增外部资产。
- Copy and content: passed，模型名可区分，会员等级规范化为 `Pro+`，无“全部模型”和“今日活动”。

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

# Tool / Skill 调用统计 Design QA

## 对照目标

- 参考截图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-58cbf0b1-4a10-4e87-a26a-766797349548.png`
- 实现截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/invocation-usage-real-home.png`
- 同画布对照图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/invocation-usage-reference-comparison.png`
- source pixels: `1290 x 2796`；implementation pixels: `3104 x 2192`；comparison pixels: `3272 x 1400`
- implementation viewport: `1440 x 984` points
- state: 真实 Codex Home、近 7 天、全部来源、总览
- 参考图来自手机视频画面，原始桌面 viewport 不可读，因此不能声称逐像素同视口复刻；本轮把它作为信息架构和密度参考，并以项目现有原生 macOS 设计语言为最终验收基线。

## Full-view comparison evidence

- 对照图将参考画面的统计面板和当前真实 Home 原生窗口放在同一画布，覆盖顶部摘要、时间/来源筛选、趋势、Tool/Skill 双排行。
- 最终实现保留参考图的核心层级：四项摘要、范围切换、趋势和并列排行；同时沿用 Codex Pulse 现有 sidebar、SectionCard、系统字体和 SF Symbols。
- `1440 x 984` 下四张摘要卡等宽铺满内容区，趋势和双排行无重叠、裁切或横向溢出。

## Focused region comparison evidence

- 趋势图最终使用线性插值，非负调用数据不再出现低于零轴的视觉误导。
- Tool / Skill 通过语义色标生成可见图例；来源选择器完整显示“全部来源”。
- Tool 与 Skill 排行使用一致的行高、进度条、计数对齐和可展开详情；长 Skill 名称在当前视口内可读。

## Required fidelity surfaces

- Fonts and typography: passed，使用原生系统字体和项目既有标题层级。
- Spacing and layout rhythm: passed，四卡铺满、区块间距一致、双排行等宽。
- Colors and visual tokens: passed，Tool 蓝、Skill 紫与趋势、卡片和排行保持一致。
- Image quality and asset fidelity: passed，无新增位图资产；图标全部使用 SF Symbols。
- Copy and content: passed，明确区分 Tool 调用和 Skill 检测活动，并说明隐私与非 Token 归因边界。

## Comparison history

1. 第一版真实窗口发现趋势 `.catmullRom` 在零值附近下探、图例缺失、来源筛选截断。
2. 第二版改为线性插值、语义色标图例并加宽来源选择器；随后发现 adaptive grid 在宽屏只占半行。
3. 最终版使用四个 flexible 列铺满摘要区；同画布复核未发现 P0、P1 或 P2 视觉问题。

## Interaction checks

- [x] 总览 / Tool / Skill 分段切换并读回对应趋势与排行
- [x] 全部来源 / 结构化事件筛选并读回计数变化
- [x] Tool 排行详情展开，读回成功、失败、未知结果、来源、最近时间和平均耗时
- [x] 真实 Home 页面加载，当前动态计数非零

## 2026-08-07 排行与时间范围细化

- 本轮对照截图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-9d88aaff-61b7-4a7f-9753-90616d12914d.png`。
- 近 7 天同状态截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/invocation-usage-refined-seven-days.png`（`3016 x 2104` pixels）。
- 周额度截图：`/Users/suqing/Coding/golang/00_self/codex-pulse/.artifacts/design-qa/invocation-usage-refined-quota-week.png`（`3104 x 2192` pixels）。
- 参考截图与最新近 7 天截图已在同一次视觉输入中对照；内容区 viewport 和交互状态一致，未复用旧截图替代当前工作树。
- [resolved P1] “展示内容”分段控件原先因 Picker 自带标签占位产生额外缩进；当前标签与 segmented control 拆分，整组左边缘与摘要卡、趋势卡严格对齐。
- [resolved P1] Tool 与 Skill 的 AreaMark / LineMark 原先缺少独立 series，双序列会被连成锯齿；当前分别声明 Tool、Skill series，面积、折线和点位各自连续。
- [resolved P2] 顶部时间范围补齐“周额度”，并与既有图表共用 `UsageTrendChartPresentation`：真实 UI 读回周额度为“每日趋势 · 自 8月5日 · 按天”，今天为“每小时趋势 · 自 8月7日 00:00 · 按小时”；横轴刻度和悬停日期使用同一 IANA 时区格式。
- [resolved P2] Tool / Skill 请求上限均为 10，UI 也限制 `prefix(10)`；Accessibility 读回总览中两列各 10 行，Tool 单列模式仅保留 Tool Top 10。
- 真实周额度筛选将范围从近 7 天切换为额度重置边界，计数与趋势点同步变化；额度周期不可用时界面明确提示回退近 7 天。
- 当前没有未解决的 P0、P1 或 P2 视觉问题。

final result: passed

## 2026-08-07 柱状图与重叠模式

### 对照目标

- source visual truth: `/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-9d88aaff-61b7-4a7f-9753-90616d12914d.png`
- implementation screenshot: `/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/com.openai.sky.CUAService/Codex Pulse Screenshot 2026-08-07 at 17.58.39.jpeg`
- source pixels: `1440 x 984`
- implementation pixels / viewport: `1124 x 768`，原生窗口 1x 捕获
- density normalization: 未做像素缩放；参考图和实现图在同一次视觉输入中按整体层级、控件位置、图表语义与密度对照，不声称逐像素复刻。
- state: 真实 Codex Home、近 7 天、全部来源、总览、浅色外观。

### Full-view comparison evidence

- 最新实现保持四项摘要、贴左的“展示内容”、趋势区和双列 Top 10 排行，窗口内没有裁切、横向溢出或控件错位。
- 趋势从折线/面积图改为柱状图；Tool 使用蓝色、Skill 使用紫色，图例、摘要卡和排行颜色保持一致。
- 用户确认采用重叠模式后，两组柱共享同一时间桶中心；使用 `.unstacked` 保留独立真实值，不做加总堆叠。

### Focused region comparison evidence

- 图表在全窗口截图中占据完整内容宽度，柱体、零轴、图例和日期刻度均可直接辨认，因此无需额外裁切放大。
- Accessibility 读回近 7 天为“每日趋势 · 自 8月1日 · 按天”，今天为“每小时趋势 · 自 8月7日 00:00 · 按小时”，周额度为“每日趋势 · 自 8月5日 · 按天”。
- 悬停详情仍同时读出同一桶内 Tool 与 Skill 的独立计数；总览 / Tool / Skill 切换不会合并两类数值。

### Findings

- [resolved P1] 折线图在稀疏数据下只在末端形成陡升斜线，阅读效率低。
  - fix: 改为按小时或按天的柱状图，并根据今天、周范围和近 30 天自适应柱宽。
- [resolved P2] 第一轮柱状图采用左右并列，与用户最终确认的重叠展示不符。
  - fix: 两组柱改为同一时间坐标并关闭数值堆叠，紫色 Skill 柱覆盖在蓝色 Tool 柱前方；单系列模式保持居中。
- 当前没有未解决的 P0、P1 或 P2 视觉问题。

### Required fidelity surfaces

- Fonts and typography: passed，继续使用系统字体、现有字号和字重层级。
- Spacing and layout rhythm: passed，摘要、分段控件、趋势和双排行左边缘及间距一致。
- Colors and visual tokens: passed，Tool 蓝与 Skill 紫在柱体、图例、卡片和排行中语义统一。
- Image quality and asset fidelity: passed，无新增位图或近似绘制资产；现有 SF Symbols 保持不变。
- Copy and content: passed，时间范围、按小时/按天、Top 10 和统计边界文案均保持准确。

### Comparison history

1. 折线/面积趋势按用户反馈改为柱状图。
2. 第一轮真机截图发现默认 BarMark 会产生加总堆叠，修正为 `.unstacked`。
3. 第二轮按并列方案左右错开，真机截图确认两柱可分辨。
4. 用户最终选择重叠展示；第三轮将两柱恢复同一桶中心并保留 `.unstacked`，同画面对照确认无新增 P0/P1/P2 问题。

### Implementation checklist

- [x] Tool / Skill 重叠柱状图且数值不累加
- [x] 今天窄柱与每小时刻度
- [x] 周额度、近 7 天宽柱与每日刻度
- [x] 近 30 天窄柱与每日刻度
- [x] 悬停详情保留 Tool / Skill 独立计数
- [x] 总览 / Tool / Skill 单独模式
- [x] 真实 Home 原生窗口视觉检查

final result: passed

## 2026-08-07 移除统计说明

- source visual truth: `/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-b024033a-1842-4657-a91f-11356392d31b.png`
- implementation screenshot: `/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/com.openai.sky.CUAService/Codex Pulse Screenshot 2026-08-07 at 18.05.01.jpeg`
- implementation viewport: `1124 x 768`，真实 Codex Home、近 7 天、全部来源、总览、浅色外观，滚动条位于页面底部。
- 参考图与实现图已在同一次视觉输入中对照：原先的“统计说明”卡片、说明正文和覆盖计数均已移除；Top 10 双排行成为页面最后一个内容区块。
- Accessibility 读回不再包含“统计说明”“内容检测”或 `lock.shield`；排行末尾与页面底部间距沿用现有 `ScrollView` 的 18pt 内边距。
- 当前没有未解决的 P0、P1 或 P2 视觉问题。

final result: passed

## 2026-08-07 侧栏调用统计顺序

- source visual truth: `/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-23bf0b97-1175-4414-9a87-602d447b5c6a.png`
- implementation screenshot: `/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/com.openai.sky.CUAService/Codex Pulse Screenshot 2026-08-07 at 18.09.09.jpeg`
- source pixels: `504 x 490` focused crop；implementation pixels / viewport: `1124 x 768` native window capture；未做密度缩放。
- state: 真实 Codex Home、调用统计选中、近 7 天、全部来源、总览、浅色外观。
- full-view comparison: 调用统计页面内容、侧栏宽度、分组结构与选中态均保持不变；只调整“使用情况”分组内的入口顺序。
- focused comparison: 参考图和实现图已在同一次视觉输入中对照；实现侧栏明确读作“概览 → 会话 → 项目 → 调用统计 → 额度与用量”，满足调用统计位于额度与用量上方的目标。
- Fonts and typography: passed，系统字体、字号与字重未变。
- Spacing and layout rhythm: passed，行高、内边距、分组间距与选中背景未变。
- Colors and visual tokens: passed，蓝色选中态与浅色侧栏 token 未变。
- Image quality and asset fidelity: passed，继续使用既有 SF Symbols，无新增图像资产。
- Copy and content: passed，入口名称、图标和页面路由保持原义。
- comparison history: 初始真实窗口 Accessibility 顺序为“项目 → 额度与用量 → 调用统计”；修复后读回为“项目 → 调用统计 → 额度与用量”，未发现 P0、P1 或 P2 问题。

final result: passed

# Design QA

## 验收对象

- 原始参考 1：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-20236035-8b39-42d2-87e2-8bd62a53c1bc.png`（197 × 98 px）
- 原始参考 2：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-8c111fc9-0eba-46f0-a227-6666f81e5017.png`（486 × 706 px）
- 主窗口实现截图：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-shot-2026-08-17_11-03-25-d3.png`（1920 × 1080 px）
- Cursor 弹窗实现截图：`/private/tmp/cp-cursor-popover.png`（3840 × 2160 px，Retina 2x）
- 弹窗下拉框展开截图：`/private/tmp/cp-cursor-picker-open-2.png`（3840 × 2160 px，Retina 2x）

## 对照输入

- 聚焦对照：`.artifacts/design-qa/provider-main-comparison.png`（788 × 236 px），同一张图内并排比较原分段控件与主窗口下拉框实现。
- 完整对照：`.artifacts/design-qa/provider-popover-comparison.png`（972 × 750 px），同一张图内并排比较原 Codex 弹窗与 Cursor 卡片化弹窗实现。

## 状态与交互

- 主窗口处于 Codex，状态栏处于 Cursor；状态栏仍显示 Cursor 的今日请求与 Token，验证两个 provider 状态互不覆盖。
- 主窗口 provider 控件为原生 menu picker。
- 弹窗左上角 provider 控件替换原 `Codex Pulse` 标题；展开菜单显示 Codex / Cursor，当前 Cursor 有选中标记。
- Cursor 弹窗按 Codex 的信息密度组织为配额、今日使用、本月每日 Token、账期消费和今日活动卡片，未知值不伪装为 0。

## 视觉检查

- 布局：通过。控件、卡片、滚动区域和底栏没有重叠或裁切。
- 间距：通过。弹窗沿用既有 18 pt 内容边距和 section spacing，标题、卡片与分隔线节奏一致。
- 字体与层级：通过。provider 下拉框保持原生可识别性；Cursor 的数值、辅助文本、图表标题层级与 Codex 一致。
- 色彩与组件：通过。复用现有 glass backdrop、PulseCard、绿色额度条、蓝色趋势图和 SF Symbols，没有引入新的视觉体系。
- 功能状态：通过。主窗口下拉框、弹窗下拉框、Cursor 数据展示和主/状态 provider 独立状态均有实机证据。

## 迭代记录

1. 将两个 segmented provider 控件改为原生 menu picker，并把弹窗 picker 放到原 App 标题位置。
2. 移除 Cursor 原始字段列表，改为与 Codex 一致的卡片与图表层级。
3. 将状态 provider 请求与主页面请求拆开，并修正可选账号请求变慢时阻塞状态主体的问题。

## 最终结果

passed
