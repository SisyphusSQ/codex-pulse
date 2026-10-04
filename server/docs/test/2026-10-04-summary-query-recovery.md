# 首页 summary 年度查询超时修复

## 故障与定位

2026-10-04，正式首页 `/api/v1/statistics/summary` 在约 10 秒后返回 500。此时数据库可连接，`/ready` 正常；与此前数据库进程退出的 session 故障原因不同。

首页懒加载仍立即请求 summary，年度卡片保持独立置顶。summary 在同一只读快照中分别读取当前筛选范围和固定 365 天范围。SeekDB 1.2 对年度 canonical 用量查询选择 MERGE JOIN，约 51 万条事实的宽范围扫描超过应用 context 和数据库查询期限。只读重现中年度 Rows 查询约 8.96 秒，随后出现 MySQL 协议错误 4012，summary 约 10.25 秒失败。

## 修复与边界

只在年度热力图的 canonical StreamHeatmapUsage 查询加入固定 `USE_HASH(u)` hint，现场 EXPLAIN 确认 HASH JOIN。保留同一列集、参数化筛选、只读快照、Go 精确聚合、行数预算、取消和 Rows 释放。窄范围和指定机器的来源仲裁路径保持原实现。

未延长超时、截断年度数据或修改 schema。Web 懒加载、年度卡片、API contract、Mac App 与 Helper 不变。其他受支持数据库可忽略该 SQL 注释；SQLite 聚焦回归已覆盖实际执行。hint 语义参考 [OceanBase 官方文档](https://en.oceanbase.com/docs/enterprise-oceanbase-database-en-10000000000385219)。

## 开发阶段证据

在实际正式库执行 service 层只读查询，时间范围使用 Asia/Shanghai，结束日为 2026-10-05（不含）：

| 范围 | summary 耗时 | 年度返回 |
| --- | ---: | ---: |
| 今天 | 1.463 秒 | 365 天 |
| 7 天 | 2.460 秒 | 365 天 |
| 30 天 | 4.055 秒 | 365 天 |
| 90 天 | 8.744 秒 | 365 天 |

另按正式应用的 10 秒 context 同时请求 summary 和 usage：今天分别约 1.457 / 0.153 秒，90 天约 2.946 / 2.064 秒，均成功。不同缓存状态和持续上报会影响耗时；以上不是 P95、持续压力测试或浏览器网络计时。

已有 `TestHeatmapReadKeepsTotalsDaysAndCoverage`、活动、全局去重/机器来源以及轻量投影聚焦测试通过。新增 `TestHeatmapReadPreservesFilteredFacts` 覆盖 provider、model、项目、搜索、时间边界，以及跨机器副本、NULL、零值和超过 2^53 的精确数值；最终执行通过。测试使用隔离 SQLite，不作为 SeekDB 查询计划证据。原始诊断保存在忽略目录 `.artifacts/summary-20261004/`。

## 部署与验收边界

使用当前 main 代码构建包含 Web 的 Server 维护版本，依次替换 SQMC04 DEV 和正式环境的受管不可变 release，保留旧目录、配置与授权。正式部署前进行在线私有备份；不创建或覆盖正式 tag / Mac 发行资产。具体 commit、二进制版本、服务状态和发布读回另记 Linear TOO-525。

提交、合并和部署收尾复用开发证据，按约定不重复测试。当前没有可用的已登录中心浏览器上下文，未创建新的管理授权；service 层实际库成功不冒充已登录 HTTP 200 或用户页面验收。没有全仓长测、CI 或持续性能结论。

安全自查：hint 是服务端常量，不接收用户拼接；权限、参数化查询、资源限制与错误传播保持原机制，未新增外部请求或秘密输出。
