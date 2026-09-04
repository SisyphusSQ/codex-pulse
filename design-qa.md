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
