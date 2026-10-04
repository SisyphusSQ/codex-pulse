# 统计查询 v1

与本机 CoreService 分离。所有路由要求 admin 浏览器会话，不使用请求参数中的身份授予权限。响应沿用 `code/message/request_id/data` envelope。来源与投影在同一只读快照中查询；MySQL 显式使用 REPEATABLE READ/READ ONLY，SQLite 保持同一读事务。TOO-526 的现场 SeekDB 查询与 HTTP 验收见 [性能验收](../docs/test/too-526-performance.md)，不代表其他 MySQL 部署已经验收。

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
| `model` | 精确模型名；`unknown` 表示无模型归因。工具/技能计数已停用 |
| `search` | 最多 256 字符；会话标题/raw Session ID/项目名的字面包含搜索；不接收正则或 SQL 片段 |
| `sort/direction` | activity/title/name/tokens/cost；asc/desc；默认 activity/desc，中心 ID 作为稳定次排序 |
| `page/limit` | 默认 1/25，page 最大 100000，limit 为 1–100；范围 totals 在分页前计算 |

每个查询最多读取 10 万会话/项目、1 万设备、20 万来源及 200 万事实；超预算返回 413，要求缩小筛选范围，不能截断后报告成功。共享请求 deadline 限制数据库读取与计算，来源/用量按行流式读取。

会话/项目 URL 的 `:id` 使用中心 ID。未关联 Cursor 用量的 `session_kind=unassigned_usage`，`session_id=null`；不能把合成分组键冒充原始会话。已知会话返回用户允许的标题和原始 Session ID。项目名称相同不会合并，`members` 是显式关联的中心项目成员。项目成员列表保留无当前活动的历史项目，范围 totals 为当前已收到事实。

项目详情的 `project/sessions/trend/models` 在同一只读快照中生成；`models` 为整个项目筛选范围的构成（含明确舍入差额），不随 `sessions.page` 截断，也不通过第二次浏览器请求拼接不同快照。会话详情保留全部采集来源、原始身份、趋势与 coverage，不返回正文或路径。

`summary.heatmap_range` 固定为截至当前自然日的连续 365 个自然日，使用同样的 Provider、模型、项目、设备和搜索筛选，但不使用 KPI 的日期范围；`heatmap_coverage` 与当前 `coverage` 独立。没有本日/星期小时事实的格子保留 nullable 计数，不能因其他日期有事实或 Provider ready 而补成零；明确零计数事实仍返回 `"0"`。自然日以 AddDate 推进，DST 的一天可能为 23 或 25 小时。`weekday_hours.weekday` 以 Sunday=0，hour 为 0–23。

`summary.heatmap_activity` 与年度日序列在同一只读快照中产生：`total_tokens` 使用年度范围已知小计；`peak_daily_tokens`、`active_days`、`longest_streak_days` 只概括已观测日，未知日不连接连续活动；`observed_days/unknown_days` 明确日级覆盖。当前连续天数允许今天明确为零时截至昨天；今天或连续段起点遇到未知日期则 `current_streak_days=null`，不能将未知日推定成中断或不活跃。前五项为 nullable 十进制字符串，全年未观测时峰值/天数为 NULL，明确观测零保留 `"0"`。Web 将不完整的活跃与最长连续天数标为“已观测”；格式化不改变 API 原始值。

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

工具/技能兼容字段为空，不返回调用参数、命令、结果或正文。API 不返回来源 payload、凭证摘要、Cookie、完整路径、原始 JSONL 或原始错误。数据库查询参数化，排序在服务端枚举选择，不拼接动态 SQL。

## 生命周期 TPS

会话列表/项目会话/详情的 throughput 使用 nullable 十进制字符串；average_output_milli_tps 的 average_unit=milli_tokens_per_second，active_duration_ms 的 duration_unit=milliseconds。basis=closed_turn_lifetime_output，均值不随日期/模型/分页变化。source_client_id 指向接受统计的采集来源；同贡献完整证据不一致显示 partial/source_conflict，不累加副本。旧客户端 not_reported、其他 Provider unsupported_provider 保持未知。

详情 throughput_limit=1..50 默认 20；throughput_turns 返回 items/nullable total/limit/truncated，最近项只含安全哈希键、起止与指标，不从子集重算平均。在统计同一只读事务中仅查询返回会话页，来源数和 payload 预算保持。无需新增 DDL。MySQL 实机尚未验证。

## 生命周期缓存命中率

会话列表、项目内会话与详情新增 cache_hit_rate：unit=basis_points，basis=lifetime_cached_input，basis_points/input_tokens/cached_input_tokens 为 nullable 十进制字符串。计算整段已索引 cached/input，缓存是输入子集；共享 Go 精确 HALF-UP 至万分比，Web 格式化一位小数。日期、模型、分页和最近 TPS 轮次不改变比例，定价未知不影响它。副本不相加，来源筛选用自身快照。

input>0/cache=0 返回真实零，input=cache>0 返回 10000；零输入 not_applicable、缺失/非法 unavailable、其他 Provider unsupported_provider、旧设备 not_reported。受限历史 history_filtered 不泄漏范围外总量。status=complete/partial/unavailable，reason 为有限枚举；source_client_id 与 conflict 保留已接受来源和完整证据冲突。沿原一致只读事务与页 ID 预算查询，无 DDL。

## 中心查询投影 v4

全局读取来源元数据，不传输完整 Session payload；Session 指标读取可重建 capsule，同事实来源冲突与定价口径不变。按采集机筛选仍保留完整来源仲裁。全年热力图只累计全年总量、每日明细及覆盖，避免同时生成未使用的全年模型/小时/工具分布。结构 v4 新增投影表，首次升级后台分批补建；缺失时使用原有读取逻辑。较大统计、Session、项目只读响应可 gzip 压缩，路径鉴权与响应契约不变。

## TOO-524 首页与机器用量

新增 `GET /api/v1/statistics/source-usage`，要求 admin；共用范围和筛选，返回 `range/scope/items`。scope 为 `collector_copies_may_overlap`；每项包含 machine(client_id/client_name)、totals、coverage、revoked_at_ms。只统计 Codex；其他平台筛选返回空项。每机器先仲裁自身同会话多个 Home；与其他机器副本可重叠，不可相加成全局。项目过滤以本机来源所属项目组判断，不能先用全局 canonical 项目排除来源。

summary 新增 activity_granularity（hour/day）、activity_timeline（start_at_ms/end_at_ms/tokens/sessions nullable 十进制字符串）、top_sessions（范围Token倒序Top5，Session VO，排除unassigned_usage）。单自然日按真实偏移的小时推进，保留23/25小时DST日期；未知桶的两个计数均为NULL。星期小时序列保持原有Sunday=0约定，增加 nullable 十进制字符串 session_count；无事实为NULL，不再用 legacy sessions=0 推断真实零。Top5与当前范围统计共用事实扫描和数据库快照。

项目VO新增 machines（client_id/client_name）；会话sources原字段继续使用中心名称表。设备状态仅返回未撤销设备；历史会话/项目仍解析撤销设备名称。客户端授权列表SQL过滤撤销项，鉴权撤销和历史行保留。

工具与技能不再统计：既有 tools/skills 兼容字段返回空数组，totals.invocations=0；Web 不显示，不读历史调用表，也不由 source payload 计算调用数。


## TOO-526 独立卡片与后台缓存

首页不再调用合并的 summary；各卡片独立请求、失败和刷新。旧 `GET /api/v1/statistics/summary` 保留，其范围与年度字段仍来自同一次只读快照。新增路由继续要求 admin：

| GET 路由 | 字段 / 范围 |
| --- | --- |
| `/api/v1/statistics/annual` | heatmap、heatmap_range、heatmap_coverage、heatmap_totals、heatmap_activity；忽略 KPI 日期范围，固定近 365 个自然日 |
| `/api/v1/statistics/totals` | range、totals、coverage；与 usage 共用精确计算 |
| `/api/v1/statistics/activity` | range、coverage、activity_granularity、activity_timeline、weekday_hours |
| `/api/v1/statistics/top-sessions` | range、coverage、top_sessions |
| `/api/v1/statistics/providers` | range、coverage、providers |
| `/api/v1/statistics/models` | range、coverage、models |

annual、usage/totals、当前范围构成/活动/高消耗会话、source-usage 和兼容 summary 使用进程内缓存。相同范围的四类当前卡片共享一次计算，各路由只返回相应卡片字段；年度缓存独立于 KPI 日期。每个计算内部保持数据库快照一致，不同卡片可来自不同计算时刻，不能据此声明跨卡片原子快照。

缓存键包含范围、时区及 Provider/模型/会话/设备/项目/搜索，先检查 admin 再读缓存；撤销授权不会被缓存绕过。首次未命中等待后台计算，共享同键并发冷请求；请求取消不取消其他等待者。一个后台工作者每分钟刷新近期访问的条目，超过五分钟未访问暂停刷新，下一次访问立即返回保留值并唤醒刷新。默认上海时区今天和年度卡片持续预热，即使无人浏览也保持刷新；其他筛选按访问活跃度刷新。重启清空缓存，冷查询仍需要真实计算。

响应新增 `cache`：`computed_at_ms` 是计算开始时刻，`refresh_after_ms=60000`、`age_ms` 为响应时的年龄，`state` 是 ready/refreshing/refresh_failed，超过两分钟 `stale=true`。正常目标为一至两分钟延迟；高负载或刷新失败时可更久，按 metadata 在页脚统一提示旧值与警告。失败保留最后成功值，初次计算失败仍返回错误，不能伪装空成功。Web 每分钟读取 metadata，页脚统一说明更新规则，不在卡片旁显示计算时间；手动刷新重新读取当前值，不强制在 HTTP 路径执行 SQL。

缓存上限为 32 个投影和 64 MiB 序列化结果；按最后访问淘汰已完成条目，不淘汰冷等待者。超预算返回 413，计算沿用 Server contextTimeout，停服取消计算并等待工作者退出。不新增外部缓存、DDL 或迁移。列表/详情、额度、目录与授权仍直接读取数据库。
