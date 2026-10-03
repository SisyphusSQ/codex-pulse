# 中心会话缓存命中率

承接 v0.14.5 / TOO-508 的原生会话口径，属于 Master TOO-475；中心与 Web 适配为新的 Execution，不修改原生功能卡已完成状态。

Goal：Codex 会话列表、详情和项目内会话展示同一生命周期缓存命中率。cache 是 input 子集，公式为整段已索引 cached/input；不对轮次百分比求平均、不将 cache 再加入分母、不用日期/模型筛选或最近 TPS 轮次重算。

本机一致快照逐字段导出 version=1 的 cache_usage capsule，basis=lifetime_cached_input，nullable 十进制 input/cached 总量及有限缺失原因；不发送原始事件、generation/offset、正文、路径或凭据。历史上报起点非零时不给出起点之前的全生命周期总量，返回 history_filtered；未建立活动索引时保留 rollup_missing。已知部分索引统计仍可给出比例，保留来源 partial/冲突提示。

native 与中心共享纯 Go 精确整数四舍五入至 basis points（0..10000）；Web 只格式化一位小数。input>0/cache=0 为 0.0%，全命中为 100.0%；零输入 not_applicable，缺失/非法/缓存大于输入 unavailable，不 clamp。定价缺失不阻止比例。Cursor/Grok、旧客户端未上报保持明确未知。

capsule 沿现有 source/canonical 白名单 JSON 持久化，不新增 DDL/比例列；中心查询沿现有一致只读快照、返回页 ID 和来源预算读取已接受证据，多机副本不叠加。相同 Token 事实出现完整 counters 冲突时标记来源冲突并保留接受值；来源筛选用自身快照。已知 capsule counters 必须与完整贡献计数对账，版本未知返回 426，未知字段 400，网络/版本拒绝保留原队列。先更新中心再更新 App。

Implementation Scope：internal/cachehitrate、native usagecost mapper、reporting v1 contract、store 导出、中心 statistics DTO/查询、Web 会话组件/类型、聚焦测试及 API/设计/验证文档。无 schema/parser 迁移，不新增采集、排序、筛选、缓存趋势、中心连接 Agent、发布或真实 MySQL/三机验收。

开发完成条件：普通/零/全命中、零输入/缺失/非法、精确大计数与半值、价格独立、日期/模型/分页/多机/source/历史起点、持久重启及原生 mapper 对账有聚焦证据；桌面/窄屏列表详情显示一致；保留鉴权/白名单/XSS 和未知语义。使用 synthetic/empty Home、隔离 SQLite 与环回 HTTP；正式 MySQL/三机仍由 Master 验收。提交推送收尾不重复测试。
