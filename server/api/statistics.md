# 统计查询 v1

与本机 CoreService 分离。所有路由要求 admin 浏览器会话，不使用请求参数中的身份授予权限。响应沿用 `code/message/request_id/data` envelope。来源与投影在同一只读快照中查询；MySQL 显式使用 REPEATABLE READ/READ ONLY，SQLite 保持同一读事务。实际 MySQL 验收尚未运行。

## 范围和筛选

除设备状态外，共用以下 query 参数：

| 参数 | 语义 |
| --- | --- |
| `start_at_ms/end_at_ms` | UTC 毫秒的 `[start,end)`；必须成对，不能与日期参数混用；非负且最多 3660 日 |
| `start_date/end_date_exclusive` | `YYYY-MM-DD`，指定时区中的自然日，末日不包含；默认最近 30 个自然日，包括今天 |
| `time_zone` | UTC 或 IANA，默认 Asia/Shanghai；嵌入 tzdata，拒绝依赖部署主机的 Local |
| `provider` | codex/cursor/grok；省略代表已知 Provider 合计 |
| `client_id` | 采集客户端 UUID；使用该来源自己的实际快照，跨账期沿用接收层仲裁；不把其他来源扩充的中心事实算给该机器 |
| `project_id` | 中心项目关联组的 64 位十六进制 ID；设备筛选同时按本设备项目身份匹配 |
| `model` | 精确模型名；`unknown` 表示无模型归因。调用没有可信模型归因，模型筛选时不返回推断的工具/技能计数 |
| `search` | 最多 256 字符；会话标题/raw Session ID/项目名的字面包含搜索；不接收正则或 SQL 片段 |
| `sort/direction` | activity/title/name/tokens/cost；asc/desc；默认 activity/desc，中心 ID 作为稳定次排序 |
| `page/limit` | 默认 1/25，page 最大 100000，limit 为 1–100；范围 totals 在分页前计算 |

每个查询最多读取 10 万会话/项目、1 万设备、20 万来源及 200 万事实；超预算返回 413，要求缩小筛选范围，不能截断后报告成功。共享请求 deadline 限制数据库读取与计算，来源/用量按行流式读取。

会话/项目 URL 的 `:id` 使用中心 ID。未关联 Cursor 用量的 `session_kind=unassigned_usage`，`session_id=null`；不能把合成分组键冒充原始会话。已知会话返回用户允许的标题和原始 Session ID。项目名称相同不会合并，`members` 是显式关联的中心项目成员。项目成员列表保留无当前活动的历史项目，范围 totals 为当前已收到事实。

`summary.heatmap_range` 固定为截至当前自然日的连续 365 个自然日，使用同样的 Provider、模型、项目、设备和搜索筛选，但不使用 KPI 的日期范围；`heatmap_coverage` 与当前 `coverage` 独立。没有本日/星期小时事实的格子保留 nullable 计数，不能因其他日期有事实或 Provider ready 而补成零；明确零计数事实仍返回 `"0"`。自然日以 AddDate 推进，DST 的一天可能为 23 或 25 小时。`weekday_hours.weekday` 以 Sunday=0，hour 为 0–23。

## 数值、成本与舍入

所有 Token、估算成本和 reported charge 为 nullable 十进制字符串。NULL 表示没有已知值；已观测范围内的真实零是 `"0"`。混合未知记录时返回已知小计并标明 coverage，不以零补全未知；从未采集的 Provider 返回 unknown。计算使用任意精度整数，前端不能用浮点重新计算业务 totals。

- Codex reasoning 是独立计数，历史价格由上报的 version/rates 提供；中心不会查当前价目覆盖历史。范围 KPI、趋势、项目按自然日/模型/版本合计后 HALF-UP，复用本机 `pricing.CalculateExact`。缓存增量可以暂时大于同条 input 增量，仅在合计后验证分解。
- 会话列表/详情按同一会话、整个请求范围、模型/版本定价，保留本机 Session 查询的舍入语义。因此每个 totals 返回 `cost_basis`；会话成本或各项目成本的和可能与跨会话 KPI 存在微美元级舍入差异，不宣称金额逐项严格相加。Token 对账不受此影响。
- Cursor input/cache read/cache write/output 分开计算，历史价率逐条累加 numerator 后在整个查询范围一次 HALF-UP。模型构成的单独舍入差额以 `rounding_adjustment` 明确返回，Token 为零且不计作模型用量；趋势与范围金额的差额由 `trend_cost_rounding_delta_micro_usd` 返回。
- `event_cost` 沿用本机记录的事件估算微美元合计；reported charge 独立返回，不混成估算成本或实际账单。
- `pricing_versions` 表示本范围的历史证据；`cost_status=known/partial/unknown` 区分已知、已知小计包含未定价记录和无已知金额。缺失价格/Token 分解不能变成免费用量。

Provider 构成和执行设备构成的 Token/成本与范围 totals 对账。采集设备属于多对多来源，不能用来源数量分摊消耗；`devices` 当前统一为 `execution_unknown`，`sources` 与设备状态单独展示采集关系。模型只用于 Token/成本分布，同一会话跨模型、日期可能重复出现，不能把分组 Session 数之和当作总会话数。

## 覆盖与新鲜度

`coverage.scope=received_facts` 明确查询中心已收到的事实。`ready` 是来源本机索引状态，不是全量分页上传完毕的证明；目前没有可验证全量导出清单，因此 `range_covered=false`，不声明完整覆盖。`state=partial` 保留这种边界，并列出未定价、未知计数、无时间事实、部分会话及冲突。

不能给无时间事实伪造日期；它们不进入有明确起止的 KPI/趋势，而由 `untimed_facts` 提示。`collected_at_ms` 与中心 receipt 分开；超过 15 分钟或无采集证据标 stale，任一已知 Provider 的旧状态不能被其他 Provider 新心跳掩盖。撤销设备仍保留合法历史。热力图空格、局部已知零和未采集状态由前端按 coverage 分开展示。

工具/技能仅返回白名单名称和计数，不返回参数、命令、结果或正文。API 不返回来源 payload、凭证摘要、Cookie、完整路径、原始 JSONL 或原始错误。数据库查询参数化，排序在服务端枚举选择，不拼接动态 SQL。
