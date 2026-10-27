# 中心参考价目维护

当前模型入口为 `models-20261027.json`，在 2026-10-02 目录上增补 GPT-6.1 Terra；只有新增条目使用本次核对时刻，旧条目保留原始版本与核对日期。`models-20261002.json` 保留为旧快照。订阅入口仍为 `plans-20261002.json`，不因模型目录更新刷新订阅核对日。字段记录计费平台、模式、模态/单位、货币、nullable价格、来源和版本。来源分别为OpenAI API/Codex pricing、Cursor models-and-pricing、xAI developer pricing及Grok pricing/FAQ。完整来源URL留在每条记录，缺失价格保持NULL。

核对日期不等于费率生效时间：公开页面未提供精确边界时effective_from_ms保持NULL。API美元、Codex Credits及图片/视频/音频等单位不可混用。目录由代码审查后交付，页面请求不抓取外部URL。更新时重新核对官方来源、版本与计费条件，不只改核对日期。

服务额外列出本机历史目录、中心已接受用量的历史费率与没有当前公开价的观测模型。收到的历史费率没有官方来源和核对日期时保持空值，不为其补造证据。所有统计继续读取历史上报的版本/费率；此目录不参与重估旧消耗或推算订阅额度。维护入口和验证见`docs/test/multi-machine-reporting.md`及`server/api/catalog-subscriptions.md`（仓库根目录）。


`releases-20261027.json` 独立记录（旧 `releases-20261003.json` 保留）官方型号/API 发布日期和发布来源，与价格核对日、费率生效边界分开。日期按 UTC 自然日编码；同型号的不同计费模式/单位/历史费率共用发布事实。优先使用官方 release notes 与发布公告，已有明确别名通过现有型号匹配复用日期；未确认型号/日期保持 NULL，不根据版本号、目录核对日或价目生效时间猜测。目录按发布日期倒序，同日按模型名/稳定键排序，未知日期置后；近期使用情况仅作筛选，不改变默认排序。元数据随代码内嵌，不在 HTTP 请求时访问发布来源。历史价格 JSON 与本机计价目录保持。

TOO-654：Terra 公开参考包含 Standard/Batch/Flex/Fast × 长短上下文 × 30 分钟/12 小时缓存写入，共 16 条；长上下文条件为输入超过 272K 时整个请求 input/cache ×2、output ×1.5，地区处理另加 10%。不提供无证据的 Ultrafast/Credits 费率。默认展示 Standard 短上下文 30 分钟缓存写入，展开可见其他条件（含 Batch/Flex）。发布日 2026-10-26 来自官方 API Changelog，与价格核对时刻、目录生效边界分别保存。Mac/DSH 历史基础文本目录与中心公开参考继续独立。
