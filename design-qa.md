# TOO-272 Design QA

## Source truth

- `docs/design/front/previews/00-design-system.png`：颜色、表面、状态、圆角与控件语言。
- `docs/design/front/previews/04-overview.png`：1440×1024 主壳、侧栏、标题与内容面。
- `docs/design/front/previews/01-local-status.png`：1440×1024 侧栏复核与内容层级。
- `docs/design/front/assets/icons/codex-pulse-app-icon-64.png`：正式应用图标资产。

## Implementation evidence

所有截图与 comparison 都位于 ignored `.agents/runs/too-272-visual-qa/`，不作为发布资产提交：

- `implementation-final-1280x770.png` / `comparison-final.png`
- `implementation-final-1440x1024.png` / `shell-comparison-final.png`
- `sidebar-comparison-final.png`
- `min-window-900x600-pass-2.png`
- `native-ready.png`（打包版 1120×720 Wails 窗口）

## Viewports and states

| 验证面 | 状态 | 结果 |
| --- | --- | --- |
| 浏览器 900×600 | `/overview`，无 Wails runtime | 无水平/垂直溢出；六项导航、当前项、错误说明与重试入口完整可见 |
| 浏览器 1280×770 | `/overview`，无 Wails runtime | token、sidebar、titlebar、error state 与 Design System source 同屏比较通过 |
| 浏览器 1440×1024 | `/overview`，无 Wails runtime | 侧栏宽度、24px 外边距、内容面宽度和标题层级与 Overview/Local Status source 比较通过 |
| 打包版 1120×720 | `/overview`，真实 Wails runtime | 显示“本机服务已连接”、`darwin`、`zh-CN`；六个导航与原生窗口控制完整 |

浏览器 error state 是预期降级：独立 Vite 页面无法调用 Wails generated binding，但仍必须保留共享壳、可理解错误文案和重试动作。原生打包态证明相同页面连接真实 Bootstrap 后进入 ready，不使用 mock 数据。

## Interaction and accessibility checks

- 通过“设置”router link切换到 `/settings`，active route 与键盘 focus 保持正确；根路径与未知地址由 router contract归一到概览。
- 浏览器 console 无实现错误；仅出现脱离 Wails runtime 的预期提示。
- 所有导航均为真实 link，当前项通过 `aria-current` 表达；按钮、卡片、表格、empty/error/skeleton 有独立语义测试。
- hidden inset titlebar 使用 `--wails-draggable: drag`，交互元素显式回退 `no-drag`；打包版通过 Computer Use 对标题区执行 drag gesture 后仍保持 `/overview` ready state，未产生文本选择或导航副作用。
- Bootstrap pending/error 只保留状态组件自身的单一 `role=status` / `role=alert`，不再嵌套外层 live region；品牌图标作为相邻应用名的装饰资产使用空 `alt`。
- `prefers-reduced-transparency`、`prefers-contrast`、`prefers-reduced-motion` 均有 CSS 降级；`data-transparency="reduce"` 提供确定性复验入口。
- 字体使用系统栈；图标来自冻结 app icon 与 `@lucide/vue`，没有手绘 SVG、emoji 或占位资产。

## Iteration history

1. 首轮 900×600 检查发现侧栏品牌副文案被压缩成孤立换行（P2）。在 `<=1040px` 隐藏 `.sidebar-secondary-copy`，复测 viewport 与 scroll size 均为 900×600，导航不溢出。
2. 首轮 1440×1024 检查发现内容区被 `max-w-5xl` 限制，和 source 的全宽内容面偏差明显（P2）。移除该限制后，内容从约 x=280 延伸到右侧 24px 外边距，最终 comparison 通过。
3. 打包并启动 ad-hoc `.app`，读回 Accessibility tree 与截图，确认 WebView URL 为 `wails://localhost/overview`、真实 ready state 和原生窗口控制均存在。
4. 独立 implementation review发现标题区缺少 Wails drag contract 与 live region嵌套两个P2，以及品牌图标重复播报一个P3。三项均先新增失败测试，再实现 drag/no-drag、单一状态播报和装饰图标语义；focused test/typecheck/build与打包版 drag gesture复验通过。

## Fidelity assessment

- 字体与层级：系统字体、标题/副标题/正文/辅助文字权重和对比符合 source。
- 间距与圆角：窗口、侧栏、内容卡和控件保持同心圆角；侧栏与内容起点、底部本机数据块比例一致。
- 色彩与材质：light base、near-white content、selected blue、soft border/shadow 与冻结 PNG 一致；辅助模式有不透明/高对比降级。
- 图像与图标：复用正式 app icon；导航采用同一线性图标语言。
- 文案与数据：中文集中 i18n；browser/native 状态均使用真实运行边界，不展示演示业务数字。
- 剩余差异：应用图标和品牌副标题按现有正式资产/文案保留，属于已冻结产品语义；无未关闭 P0/P1/P2 视觉 finding。

final result: passed

---

# TOO-273 Overview Design QA

## Source truth and evidence

- Source：`docs/design/front/previews/04-overview.png`，固定 1440×1024。
- Implementation：ignored `.agents/runs/TOO-273-overview-qa.png`。
- Combined comparison：ignored `.agents/runs/TOO-273-overview-comparison.png`，source 在左、implementation 在右，同为 1440×1024 后横向合并。
- Minimum fallback：ignored `.agents/runs/TOO-273-min-900x600.png`。
- Native packaged：ignored `.agents/runs/TOO-273-native-1120x720.png`，nominal 1120×720 Wails window 的截屏像素为1119×719。
- Normal-state 数据来自临时隔离 typed DTO cache；夹具不进入 production route、产品 runtime 或提交范围，验证后已删除。

## Fidelity assessment

| 验证面 | 结果 | 结论 |
| --- | --- | --- |
| 首屏顺序 | PASS | 配额 → Token 趋势/范围 → Token 构成/API 等价成本 → 每日明细与 source 一致 |
| quota | PASS | 双卡、remaining 百分比、蓝/紫进度、reset/source/freshness保持 source 比例；Reset credits 作为冻结新增事实压缩进次级卡标题 |
| range/trend | PASS | 默认近 7 天；范围控件位于趋势卡右上；四个 series 使用 ECharts canvas、legend、Aria/decal，未用 div/SVG 手绘图表 |
| composition/cost | PASS | 两列同高内容卡；构成色点与金额层级一致；pricing source/version 与未定价事实直接来自 DTO |
| daily | PASS | 1440×1024 首屏可见表头和首行；review 后改回 source 的 Cached 列，并使用服务端 start/timezone 本地化日期；DTO 无 completeness事实，因此不伪造该列 |
| typography/material | PASS | 系统字栈、near-white content surface、soft border/shadow、selected blue、同心圆角与 TOO-272 token 一致 |
| privacy/truth | PASS | 未显示 session/dimension/source opaque ID、路径、原始错误或用户内容；API 等价成本保留估算说明 |

## Interaction and accessibility

- “近 30 天”点击后读回 `aria-pressed=true`；“自定义”点击后出现具名“开始日期”和“结束日期（不含）”输入。
- ECharts 注册 AriaComponent 与 decal，Reduce Motion 关闭动画；独立组件测试覆盖 canvas init/setOption/dispose。
- generic query region 与 Overview 集成共同覆盖 loading/empty/partial/stale/error/retry；同一 Usage query 只有主趋势区拥有 live status，派生构成/成本/每日视图不重复播报。fatal Usage 不遮蔽 quota，底层 error cause 不进入 DOM。
- in-app Browser error log 为 0；只出现 Wails 浏览器环境自身的预期开发提示，不属于页面实现错误。
- source 与 implementation 已放入同一个 comparison 输入复核；当前无未关闭 P0/P1/P2 视觉 finding。

## Iteration history

1. 首轮 implementation 把 range 放在独立顶部面板、quota 包入大外卡、趋势落到首屏下方，和 source 顺序/密度偏差明显。改为 bare 双 quota 卡，并把范围移入趋势卡。
2. 第二轮趋势卡与成本卡过高，导致每日明细完全落出 1024px 首屏。将 chart 从 256px 收敛到 208px、压缩 quota metadata，并把 pricing source/version 移到金额右侧，重新生成 combined comparison。
3. 最终首屏已显示 daily 表头/首行；Linear 额外要求的 recent/index/health留在下方滚动区。范围切换、自定义输入与 console 复验通过。
4. implementation review 发现 freshness enum、unknown/0、partial-empty、重复 live status、日期/tooltip i18n 与缺少最小/native证据六个 P2。全部先形成6项失败断言再修复；额外把 daily列改回 Cached，并异步加载ECharts。
5. 900×600 浏览器降级态没有水平溢出或 console error；nominal 1120×720 packaged Wails在隔离HOME下同屏显示quota empty、Usage/Session partial-empty、Project fatal error、Source/Health ready，且empty Usage仍保留range controls。
6. review rework后重新生成1440×1024 implementation与combined comparison；当前画面已显示Cached列、本地化日期和中文未定价原因，Browser snapshot精确读回这些事实且console error=0，旧画面不再作为最终证据。

final result: passed
