# 交互设计稿

此目录仅由`.storybook`加载；业务入口不导入。业务统计与账号均为合成样本，套餐和发布日期采用注明来源的公开参考；不请求真实中心API、不读取Home、不创建真实授权。

2026-10-03本版设计已获用户批准，正式业务页面已按真实API接入相同的信息层级；设计目录继续独立保留，不作为业务数据或持久化证据。业务开发与设计对照见仓库根`design-qa.md`及多机runbook。

- `Studio`：完整导航、九个页面、共享筛选与状态。
- `analytics`：概览、模型、来源、账号、Credits与节奏。
- `workspace`：项目/会话分屏、价目、设备与浏览器配对。
- `fixtures`：固定样本，真实NULL/零/未知概念分别保留。
- 三组stories：全站设计9条、关键状态8条、公共组件5条。
- `studio.css`：`ds-`命名空间，复用业务主题/单位格式/日历几何。

启动`npm --prefix server/web run storybook`，打开`http://127.0.0.1:6007/`。设计决定、公开参考与范围见[设计说明](../../../../docs/design/details/multi-machine-reporting/storybook-design.md)。

## TOO-523 追加预览

- `Subscriptions.stories`：6条价目与订阅样稿，保留“模型价格 / 订阅与额度”两个主标签；订阅内按 Codex/Cursor/Grok 与个人/团队分类，参考官方购买页的套餐卡片和档位选择。模型标签直接复用正式 Pricing 页面，默认相关模型，一行一个模型，保留发布时间排序、近30天用量与历史版本。尚未替换正式订阅页。
- `Changes.stories`：12条本轮优化故事（含改动目录），覆盖 Notification、年度汇总、模型柱状图、最后更新/恢复/未知/冲突额度、设备状态、全量补传、Prometheus说明、SVG图标、模型发布时间排序。
- `ReviewApp` 复用正式 `PulseApp` 与页面组件；`reviewApi` 将当前 iframe 的所有 `/api/` 请求拦在合成适配器，未配置路由返回501，不向后端透传。改名和订阅演示仅修改内存，卸载时清理合成会话与请求。公共价目 JSON 仅在构建时导入。
- `SyncDesign` 明确区分后端指标说明与原生App补传交互，不能把Storybook状态当成真实补传/抓取结果。

本轮旧6007已按用户要求停止；当前分支预览使用 `npm --prefix server/web run storybook -- --port 6008`。入口：`http://127.0.0.1:6008/?path=/story/subscription-plans--codex`；最新价目入口：`/?path=/story/subscription-plans--focused-model-prices`；改动目录：`/?path=/story/too-523-updates--index`。保留原22条设计供对照，新追加18条，合计40条，当前实现以“本轮优化”为准。

价目先在 Storybook 检查，再接入正式前端的同一 `ModelPriceCatalog` 组件。Codex 默认参考型号依据2026-10-03官方模型说明，Cursor/Grok保留现有文本模型参考；近30天实际使用和已观测未定价型号仍可查。普通API全目录、Batch/Flex及未使用的图片/音频/Embedding不铺在主表；Fast/长上下文、缓存写入、Credits和历史版本在模型内展开。仅改变查阅层，不更改公开费率或历史统计算法。
