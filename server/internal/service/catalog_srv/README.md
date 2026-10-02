# 中心参考价目维护

`models-20261002.json` 与 `plans-20261002.json` 是已核对的公开目录快照，核对日期2026-10-02。字段记录计费平台、模式、模态/单位、货币、nullable价格、来源和版本。来源分别为OpenAI API/Codex pricing、Cursor models-and-pricing、xAI developer pricing及Grok pricing/FAQ。完整来源URL留在每条记录，缺失价格保持NULL。

核对日期不等于费率生效时间：公开页面未提供精确边界时effective_from_ms保持NULL。API美元、Codex Credits及图片/视频/音频等单位不可混用。目录由代码审查后交付，页面请求不抓取外部URL。更新时重新核对官方来源、版本与计费条件，不只改核对日期。

服务额外列出本机历史目录、中心已接受用量的历史费率与没有当前公开价的观测模型。收到的历史费率没有官方来源和核对日期时保持空值，不为其补造证据。所有统计继续读取历史上报的版本/费率；此目录不参与重估旧消耗或推算订阅额度。维护入口和验证见`docs/test/multi-machine-reporting.md`及`server/api/catalog-subscriptions.md`（仓库根目录）。
