# Web 工作台整体重构

用户明确指出当前 Web 不可接受，授权使用 AntD skill 和官网组件完整重构。本项属于 Master TOO-477，包含所有现有页面与核心流程；保留现有数据、安全与运行契约，不将完整重构缩减为换色或只改首页。

问题：双层横向顶栏占据首屏，英文装饰标题/标识/口号抢占内容；大数字与卡片重复堆叠，额度证据和长说明迫使用户纵向寻找操作；项目/会话详情追加到列表下方，筛选分散且层级模糊。

Goal：形成清晰、紧凑、可操作的中文数据工作台，按 AntD 官方 Layout/Menu/Form/Table/Tabs/Descriptions/Drawer/Collapse/Progress/Statistic/Breadcrumb 的组件结构和 Design Token 组织。普通文本保留框架转义，不复制官网 demo 的 console/示例数据或编造业务信息。

Scope：

* 桌面左侧导航、紧凑页头与统一标题/操作区；窄屏可操作导航，不靠 hover 或隐藏入口。
* 登录为专用配对表单，去掉营销式口号和巨大装饰；保留单次提交、失败提示和短暂码清理。
* 总览指标用统一紧凑 Statistic，主要趋势与年度活动优先，构成/工具/覆盖证据分层；图表使用一致的可读轴、颜色、间距和真实数据，不造插值。
* 项目/会话以标准查询工具条和 Table 展示，详情通过 Drawer/Tabs/Descriptions 保持上下文；保留搜索、排序、分页、跨页选择、显式关联与确认流程，展示 TPS 和缓存命中率。
* 额度按账号/窗口清晰选择，Progress 仅表现已知观测比例，状态文字保留；当前/历史/来源/credits 按 Tabs 和折叠组织，区分 reset/到期与 stale/expired/unknown，不伪造倒计时/预测。
* 设备状态与授权管理层次清楚，签发码、改名、撤销流程可操作；码只在内存，关闭/撤销不遗留 DOM。
* 去除无业务意义英文眉题、装饰符号、夸大营销、巨型数字、重复卡片与过量整屏说明；关键不完整/错误提示保持可见，详细口径可展开。

Implementation Scope：server/web/src 公共壳/主题/样式、全部 pages、filters、RecordViews/QuotaViews/CoverageNotice/Throughput/CacheHitRate、聚焦交互测试、设计/README/验证摘要；不引入第二套 UI 框架、不为外观升级依赖或改变 API 的业务计算/授权。

开发完成条件：所有页面和详情完成结构重构；官方组件 API/示例查询和 AntD lint/typecheck/build；登录、筛选分页、关联/撤销、tabs/drawers、未知/零、恶意标题、码清除等行为测试；真实环回 HTTP 合成数据桌面及 390px 检查和截图。Web 只格式化服务端事实，不将 mock 结果冒充真实数据，不改 native macOS UI。本阶段开发验证不替代真实 MySQL/三机/生产验收；提交推送收尾不重复测试。

参考：[Layout](https://ant.design/components/layout-cn/)、[Table](https://ant.design/components/table-cn/)、[Tabs](https://ant.design/components/tabs-cn/)、[Descriptions](https://ant.design/components/descriptions-cn/)、[Progress](https://ant.design/components/progress-cn/)。

## 已落实

所有业务页面及登录均已结构重构，未增加框架或升级依赖。总览按下述 Mac 汇总布局组织年度活动、趋势及独立构成，明细与数据质量可展开；项目与会话使用嵌套抽屉和标签；额度使用账号窗口列表、所选状态/实际 reset、历史节奏与来源证据标签，Credits 独立；设备签发码抽屉和授权/上报状态标签保持完整权限确认。

Desktop 采用左侧 Menu，390px 使用导航 Drawer；路由变化回到顶部，TPS 轮次数量更新保留当前标签，窄列用单位标签避免数值重叠。重构只改变呈现与交互，不改变统计、账号、权限或配额算法。验证证据见多机 runbook；MySQL 与三机真实验收留 Master。

## 汇总与热力图对齐（2026-10-02）

汇总布局参考本项目 `app/macos/Sources/CodexPulseApp/DashboardSummaryView.swift`：三个范围指标、年度 Token 活动、全宽趋势、客户端与模型用量、分别独立的客户端/模型环图。额度入口保留，中心不把多账号窗口压成单一客户端百分比。

用户随后要求热力图直接对齐掘金。已读取实际 Dashboard 与官方 [ActivityHeatmap](https://github.com/juejin-cn/juejin-usage/blob/00f1f2f99159e81d896c596590c760d8a29171d2/packages/dashboard/src/components/ActivityHeatmap.tsx)、activity-heatmap.ts 和 index.css：整数像素方格（最小 14px）、3px 间距、3px 圆角、Sunday 起始的七行标签、同列月标、50/75/90 正值分位与实心蓝色色阶，图例在底部。中心继续展示完整 365 日，窄屏横向查看而不删除旧周，星期固定；未观测日为实心浅灰，真实零为最浅蓝。仅借鉴几何与配色，不复制掘金的缺失补零、来源比例推算或过滤业务。年度指标收进展开项，年度覆盖/陈旧提示保持可见。

Go 在现有一致只读统计快照内返回 heatmap_activity，不新增数据库表。年度总量、峰值与活跃/连续天数由 Server 计算；unknown 不充作 zero，当前连续段边界未知则保持 NULL，已观测活跃/最长连续天数是现有事实概括，不声明完整历史。页面只计算日历位置与视觉分位色阶。

Token 展示与中文 Mac 的 TokenQuantityFormatter.compactString 对齐：万/亿、最多一位小数、单位进位、NumberFormatter 的 half-even 显示舍入；API 等价成本保留两位小数。适用于总览、列表、详情、缓存/TPS 输出计数、环图图例/提示与坐标轴。会话/天数/轮次保持普通计数；API 和数据库继续保存完整十进制值，前端不以展示舍入值重算统计。
