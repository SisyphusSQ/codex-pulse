# 中心配额与周期 API

`GET /api/v1/quotas` 只允许已授权管理浏览器。可选 `provider=codex|cursor|grok`、`account_key=<中心账号键>`、`client_id=<采集客户端UUID>`；未知、重复或非法参数返回 400。collector Bearer 返回 403，匿名返回 401。

响应为标准 envelope 的 `data`：`evaluated_at_ms`、规则版本、账号资料、`windows`、`credits` 和 `coverage=observed_only`。没有数据时数组为空，不造 0 或完整历史。

- 账号是 Provider + 原始 ID；邮箱不唯一。窗口按账号/Provider/limit/window_kind/实际分钟数分组。未关联 scope 则按自己的 collector 分开，`identity_state=unassigned`，不提供可信倒计时。
- `current` 保留选中 used/remaining、原观测时间、来源设备、reset、freshness、冲突及有限原因。仅 confirmed/fresh/无冲突且 reset 未过时提供 `reset_remaining_ms`。过去 reset 保留绝对时间和最后可信值，状态 `expired_unknown`。
- 同一种来源的多机观测按原采集时间更新，合法 used 下降保留；不同来源沿用本机 arbiter 的冲突规则，不加百分比。同一时刻相同周期存在不同设备数值时显式 conflict，不提供倒计时；选中来源和全部证据均返回。
- `observations` 包含来源、原观测/接收时间、历史来源、有效性、仲裁 disposition/reason 及中央 canonical reset/cycle ID。只保留 metadata 和统计，不含原响应、本地 generation、路径、认证或 raw credit ID。
- `cycles` 来自原始时间/reset 的共享仲裁结果，处理固定锚点漂移、provisional reset、真实换代与迟到。Cycle ID 是中央窗口键 + 规范 reset 的可重算键；不是本机 generation，也不是未观测的周期计数。
- legacy/linked history 单独计算并只进入历史周期；不影响当前值、freshness 或预测。退役 Codex 专用 limit 遵循本机明确名单，不影响 Cursor/Grok。
- Credits 按账号选择最新可信原观测，三机库存不相加；失败记录保留旧库存和原时间并变 stale。`observed_inventory` 是源观测库存；只有 fresh、无冲突且完整到期分布时 `available_inventory` 扣除已知到期数量。`next_expires_at_ms` 与 `next_reset_at_ms` 独立，不从到期推 reset。库存采用十进制字符串。

一次读取的所有表共用只读 snapshot。最多 100,000 条 quota、100,000 条 Credits、10,000 个账号/客户端；超出返回 413，建议使用账号、Provider、来源筛选，不截断后伪装成功。两种数据库沿用同一 read adapter；真实 MySQL 运行尚待环境。

实现与验证入口：[中心配额设计](../docs/design/details/quota/README.md)、[开发验证](../../docs/test/multi-machine-reporting.md)。

## 节奏

`GET /api/v1/quotas/pace` 使用相同浏览器授权、筛选、预算和只读快照。返回 `evaluated_at_ms`、账号、`windows` 与 `coverage=observed_only`；窗口含同一可信 current、时间进度/偏差、本周期实际采样、上一周期、至多四个首尾覆盖的历史周期、5% 进度网格上的历史区间及耗尽预测。

中心调用纯 Go `quota.ComputePaceWindow`，与本机使用同一个曲线压缩、阶梯基线、中位区间与 recent Theil-Sen 算法；前端只展示结果。本周期曲线仅返回实际 `observed_at_ms`，不把本机显示延伸到现在的端点当成新采样。平台首末点和下降保留；基线网格是计算结果，不能冒充观测。

预测 `state=unavailable|on_track|at_risk|exhausted`，`method=none|recent_theil_sen`；缺少窗口、身份未确认、陈旧、冲突、稀疏、平台、无效证据及预算分别给有限 `unknown_reason`。at_risk 才提供耗尽时刻与距 reset 提前量。真实数据未采集的时段仍为缺口；没有耗尽时刻不能用前端公式猜一个。

复用现行证据要求：至少三条实际观测，跨度至少 30 分钟；lookback 是窗口时长的四分之一，限定在 30 分钟至 24 小时。同一时刻重复副本去重，已关联历史不参与预测/当前 freshness。Theil-Sen 至多 512 个唯一时间点，超出返回 `evidence_budget` 并保留完整曲线，避免二次组合耗尽资源。

历史 `complete` 表示该周期实际首末采样覆盖前 10% 与后 10%，并不声明中间每个时刻都有观测或完整历史上传。没有满足条件的历史基线保持 NULL/空数组；legacy 仅用于允许的曲线、上一周期与基线，不提升当前可信性。

## Web 展示约定

Web 按原始账号键筛选，不使用邮箱作唯一键；quota/pace 分别返回自己的评估时间，页面不得宣称两次请求同快照。30 秒轮询只读取已收到事实，不触发采集端刷新。reset_remaining_ms 以中心确认时快照展示，不能用旧 reset 在前端推测新周期或重置 used。实际采样只画离散点，历史 band 虚线注明计算网格；下降保留，不按缺口补线或追加当前时刻。
