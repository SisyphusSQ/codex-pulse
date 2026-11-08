# TOO-701 六条 Server 查询与维护链路

2026-11-08；基线 `fef51fc`（v0.17.7）；[TOO-701](https://linear.app/sisyphus-sq/issue/TOO-701)。用户要求六项统一开发、对照、合并及正式上线。以下为开发证据，最终部署结果回写卡片。schema v7，无 DDL、新索引、依赖或客户端发布。

## 六项处理

| 链路 | 实现及准确性边界 |
| --- | --- |
| Credits | DENSE_RANK 只携带 ID/排名必要字段，单 SQL 主键回读；有效/异常分区、两档时间的全部并列记录、完整到期明细与顺序保留。 |
| 首页 usage/current | 同范围两卡均到期时一次读取事实并共享公共聚合；单卡独立加载。两份缓存独立失败保留旧值，模型日桶超限不停止 current，展示来源失败不污染 usage。 |
| 配额多窗口 | 每个账号窗口将可用性和有界整数商/余数汇总合为一条 SQL。保留机器来源、未知/零、大整数和 200000 事实上限。 |
| 版本回收 | 沿用删除 SQL，空闲成功后按 5/10/20/30 分钟退避；本机发布收紧到最多五分钟且不延后已有期限；错误一分钟重试、五秒单轮预算不变。 |
| Session/项目列表 | 完整候选聚合排序后，仅补齐当前 Session 页/项目详情页来源；总量、页数和排序不截断。项目总列表已只聚合必要分组，保持原路径。 |
| 年度/来源历史 | 全局宽来源查询按现有 Session 索引顺序关联正文分片，避免全局排序大正文；机器/Session/项目定向查询保留旧路径。年度保留完整历史、原 HASH 查询、精确价格与自然日口径。全部 Home 仍参与仲裁。 |

来源展示预算作用于实际读取的当前页，而非范围内所有不展示的来源；事实、Session 和机器仲裁预算未放宽。没有预计算第二套统计真相，没有裁掉历史、选择“最新 Home”或提前对事实分页。

## 开发期测试

以下五个受影响包通过（独立 SQLite fixture，不访问个人 Codex Home）：

```sh
go test ./internal/repository/mysql/statistics_repo ./internal/service/statistics_srv ./internal/service/reporting_srv ./internal/service/quota_srv ./internal/repository/mysql/quota_repo -count=1
```

五包 `go vet` 通过；Server 开发构建通过，复用未变更的已构建 Web 资源。最终来源定向路径调整后，来源仓库测试及包含来源仲裁的聚焦 race 通过：

```sh
go test ./internal/repository/mysql/statistics_repo -run 'TestSourceJoin|TestStreamSources' -count=1
go test -race ./internal/service/statistics_srv -run 'TestStatisticsCache|TestCacheWorker|TestPairedCache|TestSharedOverview|TestSourceUsage|TestStatisticsGlobalDedup' -count=1
```

新增测试覆盖 Credits 全字段/并列/组合筛选/NULL/零/2^53+1/MaxInt64、Token 汇总单 SQL 与不可用状态、共享与独立首页完整响应、分页来源补齐、模型预算与来源查询错误隔离、并发缓存等待者与单请求取消、GC 空闲退避/发布唤醒/连续发布不延期/失败重试、来源 JOIN 与原成员筛选一致。既有测试继续覆盖历史费率、DST、未知事实、严格分片校验、多 Home/多机仲裁、原子发布和回收删除前的租约/指针复核。

初次中间投影测试因延迟来源补齐失败，已调整为在补齐后比较完整结果；最终相关测试通过。序列化对照比较完整响应字节，避免将 JSON 空数组与 Go nil 切片的内部表示差异误判为业务差异。

## 真实数据库对照

现场为 SeekDB 1.2.0 / MySQL 兼容协议，不是独立 MySQL 验收。基线与候选在同一 REPEATABLE READ 只读事务、固定求值时间下执行；临时构建 overlay 仅固定时钟及复用外层事务，不更改查询或业务聚合。完整正文仅在内存比较，本机忽略目录保存哈希、数量、耗时和计划，不提交原始个人数据。

| 完整响应 | 旧 SQL 次数 | 新 SQL 次数 | 结果 |
| --- | ---: | ---: | --- |
| 今日 usage + current | 15 | 8 | 逐字节一致 |
| 单机 usage + current | 10 | 5 | 逐字节一致 |
| 今日 Session 页 | 9 | 9 | 逐字节一致，来源读取范围缩到当前页 |
| 项目总列表 | 7 | 7 | 逐字节一致，保留原路径 |
| 非空项目详情页 | 9 | 9 | 逐字节一致，来源仅补齐页内会话 |
| 配额摘要 | 9 | 7 | 逐字节一致 |
| 年度 | 7 | 7 | 日桶、总量与覆盖逐字节一致 |
| 全局年度范围来源 | 4 | 4 | 全部机器/来源结果逐字节一致 |
| 单机来源 | 4 | 4 | 逐字节一致；最终保留原定向 SQL |

首次同快照样本：首页 512→313 ms、Session 页 189→96 ms、配额摘要 1458→750 ms、全局来源 2634→1453 ms。后续样本存在明显缓存/共享主机负载波动，不能据此宣称稳定百分比或分位数；年度 SQL 没有改变，年度样本时差不是本轮性能收益。

Credits 真实完整记录对照覆盖全局、Provider 和空机器筛选，结果一致；实际生成 SQL 的计划为窄列 WINDOW FUNCTION 后 NESTED-LOOP + TABLE GET，历史到期 JSON 不随排名传递。Token 额外对照六条当前周期、全局/对应机器/空机器共 18 组，可用性和精确字符串总量一致。

来源旧→新→新→旧四轮同快照：3898 行、388809410 字节的逐字段 SHA-256 均一致。样本为旧 4778/1006 ms、新 1745/908 ms，冷缓存影响显著。最终全局生成 SQL 的计划使用现有 `idx_session_sources_session`，Session/Owner 点查、分片范围读取，排序标记 `prefix_pos(2)`，避免整批大正文全局排序；仍需读取所有参与仲裁的历史。单机来源候选没有可靠收益，最终保持旧查询形状。简单 DISTINCT/GROUP BY 的 GC 候选依旧全扫描，不采用。

## 自审、发布及边界

已检查本次 diff 的参数绑定、鉴权、资源域、错误传播、共享缓存并发/取消、事实一致性、分片释放与秘密输出。发现并修正共享 loader 被重复请求改写的并发风险，最终聚焦 race 通过。无认证或外部请求边界变更，不添加凭据或原始事实到仓库。

本次不重试前序连续超时的全库备份，没有新的完整数据库快照；保留同 schema 的旧发行包供代码回退。空闲派生垃圾的计划回收间隔最长半小时，不影响可达历史；长期资源增长与跨实例实际运行尚未做长时间压力验收。SQL 提示依赖当前 SeekDB 分布，未来数据分布变化应重新评估。

提交发版收尾按用户约定不重复测试，复用以上证据；仅构建 Server 补丁，不发布原生客户端、Sparkle 或 macOS/Windows 资产。上线后观察实际版本/二进制摘要/schema/配置、自然上报、配额及统计、年度和来源完整刷新周期，临时浏览授权结束撤销。发布与观察最终结果以 TOO-701 回写为准。
