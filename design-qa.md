# TOO-516 热力图置顶与自适应补充（2026-10-02）

最新用户要求为本轮视觉依据：热力图在筛选下方、指标和趋势上方；拉宽时正方格同步增大并铺满内容区。该要求覆盖下方方案1历史记录的底部位置与固定14px上限；旧原图不再作为这两项的验收依据。

- 实现：等宽 Grid 列随容器增长，最小14px、3px间距、七行与完整365日期保持；末列月份右对齐，避免标签产生额外横向溢出。
- 1280×914 Chrome：方格14.828×14.828px，日历963px，滚动容器 clientWidth/scrollWidth 均969px，无额外横向滚动。热力图、摘要、趋势依次可见。
- 默认1971×914 Chrome：方格24.469×24.469px，日历1474px，容器 clientWidth/scrollWidth 均1480px。活动/摘要/趋势顶部约122/431/562px；完整365格，横向填满。
- 390×844 Chrome：方格14×14px，365格保留，容器315px、内容925px；仅日历局部滚动，documentWidth375px（15px滚动条），无页面横向溢出。End键访问最新日期2026-10-02，星期固定左侧；方向键和未知/零语义有原聚焦行为证据。
- 年度范围仍独立于上方统计日期，年度指标来自Server；摘要5792.6万/$24.01/18保持。捕获console error为空。

截图在ignored `.artifacts/multi-machine/heatmap-top-20261002/`：`overview-wide-final.png`、`overview-1280-final.png`、`overview-mobile-final.png`、`heatmap-mobile-end.png`。默认视口已恢复，预览托管最终构建并复用原合成SQLite和浏览器授权。

开发检查：Overview/ActivityHeatmap两个文件9场景通过；末月边缘修正后ActivityHeatmap的4场景再次通过；最终类型/生产构建与AntD lint通过（0问题）。仅Web几何与显示顺序调整，未改认证/DTO/Go/数据库/本机采集；安全自查确认React文本转义和既有权限边界保持。没有新增待修P0/P1/P2；真实MySQL/三机/生产仍待Master，提交推送收尾不重复测试。

final result: passed

---

以下为27b2f4e阶段的历史视觉记录；底部位置和固定14px选择已由上方用户修正覆盖。

# TOO-516 方案 1 视觉验收（本机合成预览）

- source visual truth path: `.artifacts/multi-machine/design-review-20261002/candidate-1.png`
- implementation screenshot path: `.artifacts/multi-machine/design-review-20261002/implementation-final.png`
- viewport: Chrome，1487 × 1058 CSS px，浅色，概览，近 30 日，Asia/Shanghai，已授权管理浏览器。
- 像素：原图 1487 × 1058；浏览器截图 1472 × 1047。比较将原图归一为 1472 × 1047；浏览器截图保留原像素。浏览器截图桥接密度与 CSS 存在约 1% 差异，不能把这点当成排版错误。
- full-view comparison evidence: `comparison-final.png`，左原图、右实现，位于同一证据图。
- 状态差异：原图是生成示例；实现使用原合成 SQLite 的真实 HTTP 数据，因此只有三个平台、真实陈旧状态和未观测缺口。总览 API 没有 Provider 日桶，使用真实汇总柱，模型页另有真实模型日桶堆叠。图稿的第四平台/假正常不作为视觉要求。

**Findings**

- [P1] 首轮全年热力图低于首屏。实现 supporting 区 338px，活动区顶部 y=1004；图稿活动区顶部约 850。原因是重复状态行、表格每行金额小计及年度展开条占用纵向空间。修复：表格部分成本改成同行信息提示；范围质量与年度统计使用可点击 Popover；缩小摘要内边距；方格上限 16px。
- [P2] 首轮明细/采集区密度不齐。平台表额外 Tabs 与两行金额使两块被拉高。修复：减小顶部空间、保持行内金额状态与可查提示，按真实设备显示三行预览。

**Required Fidelity Surfaces**

- 字体：系统中文 sans serif、22px 页头、30px 摘要，层次成立；最终聚焦对照确认表格 13px 与辅助 12px 可读。
- 间距：216px 侧栏、24px 内容边距、16px 区块间距；最终摘要 y=122、趋势 y=253、明细 y=556、年度 y=830，年度底部约1049，进入1058px首屏。
- 颜色：浅蓝侧栏、白色内容、蓝色强调、低对比边界；真实陈旧用有限警示色，不能改成图稿虚构绿色状态。
- 图片/图标：目标无产品摄影/插画资产；品牌柱图及功能图标使用同系列 AntD 官方图标，图表/日历是数据可视化，无自画装饰替代图片。
- 文案：业务元数据来自接口；修正图稿的 ChatGPT 独立平台与平台充当来源，保留 unknown/partial/采集截至语义。

**Comparison History**

1. `comparison-1.png`：首轮 P1/P2 为热力图低于首屏、两行金额状态和多余统计条。
2. `comparison-2.png`：行内金额、Popover 和 16px 方格已落实；热力图仍从 y=922 开始，P1 仍阻塞。继续把明细切换放入标题、减小摘要/图表空间。
3. `comparison-3.png`：两块明细已对齐，活动 y=864；年度 246px 导致底部仍被裁，P2 仍阻塞。继续将质量证据放到趋势标题，全年固定14px，减小活动头和内边距。
4. `comparison-final.png`：所有首轮 P1/P2 已修复，全年完整 365 个日期、月/星期/图例在首屏内；聚焦 `comparison-final-header.png`、`comparison-final-tables.png`、`comparison-final-heatmap.png` 确认字体、基线、行高及日历几何。补齐首个可容纳的部分月份标签，短月片段不挤压下一月；侧栏父级选择层级恢复。没有新增待修 P0/P1/P2。

**Implementation Checklist**

- [x] 修复后同尺寸桌面截图与密度复查。
- [x] 聚焦侧栏/指标、明细/热力图的同图对照。
- [x] 模型、账号、价目、设备、项目会话主操作与 390px 窄屏检查。
- [x] 控制台与失败/未知/零语义复查。

**交互与响应式证据**

- 自定义日期实际打开双月日历，范围质量与年度统计 Popover 可见；模型真实日堆叠与成本切换、价目搜索/用量跳转可操作。旧 `/quota?view=usage&provider=codex&model=gpt-synthetic` 自动转入模型页并保留筛选。
- 账号原 ID、Credits 和订阅编辑按同邮箱两个账号分别读回；过期/陈旧窗主摘要“当前未知”，进度条为0个，原值和原时间保留。仅打开/取消编辑，没有修改订阅或签发新码。
- 项目/会话桌面左右两侧约410/796px，无详情弹窗；窄屏会话详情仍保留隐藏列表 DOM，缓存90.0%、输入1000/缓存900与原Session ID可读。
- Chrome 390×844：概览、账号、模型、价目、来源以及导航/会话详情可用，文档宽度375（15px滚动条），无页面横向溢出；宽表/全年日历在自己的容器内滚动。`mobile-review.png` 与 `session-detail-mobile.png` 为截图。
- Chrome captured console error 列表为空。不存在后端/数据库/原生App的新增实现证明；MySQL/三机/生产/CI仍Not Run。

**可接受差异与后续细节**

- 原图四平台堆叠、平台Logo和假新鲜来源不复制。使用三个既有平台、真实汇总柱与真实设备来源；模型日桶在模型页可查。不替换或猜测数据以接近图稿。
- 14px全年格子在宽桌面留出右侧空间，目标是与既有掘金几何对齐且不拉成灰墙；内容、月份和星期靠左对齐。该差异为产品明确选择。
- 图稿未定义失败/未知/可编辑订阅等状态，实现沿用现有语义。标准图标复用AntD，不增加头像或未支持设置功能。
- P3：未来有真实 Provider 日桶时可增加首屏平台堆叠；当前不能比例分摊。不是本次待修项。

final result: passed


---

以下保留此前原生页面的历史 QA，结论不代表本次 Web 验收。

# TOO-418 汇总页 Design QA

## 对照输入

- 视觉参考：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-aa78d4c3-74c6-4239-808e-d32e9a671812.png`
- 位置参考：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-9475f105-8ac6-4c8e-adec-5bf944f24756.png`
- 趋势反馈：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-6ea95c46-d854-4283-a738-14ab650da00a.png`
- 顶部信息精简参考：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-66994c12-8d6f-424f-a750-9876bf2be2c1.png`
- 堆叠柱边界反馈：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-328db841-986b-4727-b999-c5b7a075131c.png`
- 7 天柱图密度反馈：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-ffd4db5b-4b7e-45fe-be7f-75e8444f78ae.png`
- 年度指标条参考：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-6ed1c953-353d-448a-8a7d-d2f20fe6f824.png`
- 实现截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-top.png`
- 实现截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-cards.png`
- 堆叠趋势截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-stacked.png`
- 端点修复截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-stacked-7d.png`
- 端点修复截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-stacked-30d.png`
- 同屏对照：`.artifacts/too-418-dashboard-summary/reference-comparison.png`

## 验收环境

- 原生 macOS Development App，窗口 `1440 × 984 pt`
- 状态：汇总页、今天与近 7 天、全部客户端、Token 与费用口径
- 数据：真实 Codex Home；使用私有 runtime 和线上数据库副本，未修改生产数据库

## 对照记录

1. 初版：年度热力图位于 KPI 之前；趋势图遗漏原概览的数据点层；构成条偏细；分布图缺少明确的 pointer preview。
2. 修正：页面顺序改为 `KPI → 年度热力图 → Token 趋势 → 三张分布卡 → 额度`；覆盖度保留在 KPI 卡内，不再重复显示页级日期和 partial 提示；趋势图恢复既有 `AreaMark + LineMark + PointMark` 语义。
3. 修正：模型构成条增至 `28 pt`，按客户端堆叠柱按时间范围使用 `20–64 pt` 自适应固定宽度，其中近 7 天为 `64 pt`，柱体约占每日槽位四成；热力图、趋势图、构成条及两个环图均保留独立悬停命中层与详情反馈。
4. 修正：连续 `Date` 轴的自动 domain 以首末采样点为边界，固定宽度柱因以采样点居中而被裁掉一半。现将柱宽与对称绘图留白绑定，近 7 天与近 30 天的首末柱均完整位于绘图区内。
5. 修正：近 7 天柱宽从 `34 pt` 增至 `64 pt`，对称绘图留白同步从 `21 pt` 增至 `36 pt`；实机对照中 7 根柱的视觉密度更均衡，日期标签、图例与首末边界仍正常，近 30 天密度不变。
6. 修正：X 轴不再使用与真实采样点脱节的自动刻度，改为从趋势数据中选取首尾必含的真实 bucket，并用显式居中锚点渲染日期。实机检查近 7 天的 7 个日期逐柱对齐且显示末日 `9/4`；近 30 天保留 7 个均匀日期，首日 `8/6`、末日 `9/4` 与中间刻度均对齐。
7. 修正：Cursor Dashboard 成本改为逐事件定价。可定价事件形成 API 等价成本已知小计，并明确标记“部分可估算”；Cursor 官方上报费用只在 Cursor 客户端行以“实际上报”独立展示，不进入顶部 API 等价成本、工具费用分布或模型费用分布。
8. 修正：年度热力图上方增加近 365 天 Token、峰值日 Token、活跃天数、当前连续天数和最长连续天数；五列使用等权视觉层级和细分隔线，并随“全部客户端 / Codex / Cursor / Grok”范围复用同一年度数据口径。窄窗口回落为自适应网格，避免标签裁切。
9. 最终同屏检查：三张参考卡的层级、左右布局、环图与图例关系、Token/费用切换、横向构成条以及年度指标条均已复刻；颜色、圆角和材质继续遵循 Codex Pulse 现有原生设计语言。

## 结论

通过。参考图与实现中的实时数值、客户端数量和模型数量不同属于数据差异；未发现裁切、重叠、不可读标签或错误的卡片顺序。
