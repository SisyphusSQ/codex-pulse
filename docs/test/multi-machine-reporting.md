# 多机中心开发验证与后续验收

总体方案：[多机汇总与 Web](../design/details/multi-machine-reporting/README.md)。开发证据来自隔离 SQLite 与 synthetic/empty Home，不能替代三机真实 Home 或 MySQL 验收。

## 聚焦开发入口

```sh
# 仓库根目录：本机同步与字段契约
 go test ./internal/reporting ./internal/core ./internal/helper ./api/codexpulse/core/v1 ./api/codexpulse/reporting/v1
 go test ./internal/pricing
 go test ./internal/store -run '^TestReporting' -count=1
 go test ./internal/app -run '^TestOptionalReporting' -count=1
 swift build --package-path app/macos --target CodexPulseCoreClient

# server/：结构、鉴权、HTTP 运行装配
 go test ./internal/architecture
 go test ./config ./internal/lib/gorm ./internal/repository/... ./internal/service/... ./internal/http ./internal/controller/... ./app/cmd
```

开发中按受影响范围选择命令，提交收尾不重复执行测试。本地不主动运行全仓 race/verify 长测。

## 已取得的隔离证据（2026-10-01）

- Pass：SQLite 显式初始化、重启读回、结构/版本拒绝、私有权限、事务回滚、批次唯一约束与资源关闭；Server 已构建。
- Pass：一次性用途固定的设备码、并发单次消费、过期/重放/撤销/权限拒绝、CSRF/Origin、HTTP 与实际 httptest TLS Cookie/证书语义。
- Pass：本机持久队列的匹配确认、确认丢失原 body 重试、进程重启、单调 revision、容量失败不推进 checkpoint、不同中心队列隔离及显式清理；损坏/未知字段的队列拒绝发送。
- Pass：启停取消在途上传、退出 join worker、撤销后暂停、Home fence 拒绝混合、首次补传范围不隐式删除中心历史；空 synthetic Home 的 App 开始时上报默认关闭并随 App 关闭。
- Pass：Codex reasoning/缓存增量/价格证据、索引重建稳定身份、工具统计白名单、路径不出 payload；Cursor 重复次数、缓存读写、reported/estimated 区分、未关联会话及账期切换 fence。
- Pass：CoreService RPC 白名单与生成 Swift protocol、CoreClient 编译；原生同步配置 UI 尚由设备配置执行卡继续实现。

- Pass：显式项目关联/解除、来源名称更新保留管理关系、中心事务接收、HTTP/实际 TLS 上报、相同请求原确认、batch/来源 revision 冲突、三来源复制/并发/增长/价格修订、部分修订保留、完整纠正与陈旧副本、来源 tombstone、整批回滚、Cursor 跨账期/复制、账号 scope 隔离/晚到确认/legacy 不提升、Credits 和 used 小数精度；网络身份/未知及重复字段/预算/撤销/自身进度权限。

- Pass：范围与独立年度热力图、DST 自然日、跨来源真实设备筛选、全量搜索/排序/分页与项目关联详情、NULL/零/无时间事实、超过 int64 的十进制汇总、历史价格和缓存分解、Cursor 范围舍入/reported charge、会话原生舍入口径、工具/技能白名单与无模型推断；管理查询匿名/collector 拒绝、参数预算及快照隔离装配（sqlmock 验证事务装配，真实 MySQL 隔离行为尚未验证）。

- Pass：Starter v2.0.1 业务子包迁移、15 张表各自独立 DO、模型/装配依赖方向、Fx/CLI 装配、认证、接收仲裁和统计接口；当前工作树和不含配额在做改动的独立结构提交快照均通过 `go test ./internal/architecture ./internal/service/... ./internal/repository/... ./internal/http ./internal/controller/... ./internal/lib/gorm ./app/cmd`。

以上为开发场景通过，不表示整个产品或 Master 已验收。后续各功能增量需补充其受影响验证。

- Pass：Web 框架 8 个行为测试、类型/构建、AntD lint 和锁定依赖审计；同源 Cookie/内存 CSRF、配对/恢复/退出、撤销清缓存、旧请求晚到丢弃、网络/协议错误、名称默认转义、不持久凭据。真实浏览器使用环回 HTTP/隔离 SQLite/合成码验证配对、刷新恢复、退出后刷新仍未授权和 390px 窄屏，截图在忽略的 `.artifacts/multi-machine/web/`。业务看板和静态交付继续由其他执行卡实现。

- Pass：中心节奏与本机同范围四点耗尽时刻/提前量对账、legacy 历史基线不参与预测、稀疏/陈旧/冲突预测不可用、下降曲线与原观测端点保留、512 点预测预算不删完整曲线、未知窗口的明确 forecast，以及管理端权限/参数拒绝；使用 `go test ./internal/codex/quota -run '^(TestComputePace|TestBuildPace|TestForecastPace|TestPace)' -count=1` 和 server quota/http/architecture/app-cmd 聚焦验证。

- Pass：中心配额三来源复制不累加、reset 漂移/真实换代、合法下降/迟到、同邮箱不同账号、同时刻冲突、过期 LKG/无可信倒计时、关联历史不刷新当前；未关联/空库不猜身份。Credits 多机库存不相加、到期与 reset 区分、失败保留原库存/原时间。HTTP 匿名/collector 拒绝、严格参数与预算；Fx/业务子包装配通过。

- 配额导出聚焦入口：`go test ./internal/store -run '^TestReportingQuota' -count=1`、`go test ./internal/reporting ./api/codexpulse/reporting/v1`、`go test ./internal/app -run '^(TestAccountBinding|TestOptionalReporting)' -count=1`，以及 server 的 `go test ./internal/service/reporting_srv ./internal/repository/mysql/reporting_repo ./internal/architecture`。
- Pass：Codex 同一 confirmed context 的原始账号映射和 A→B 隔离；未知历史不推断账号；私有同步库 schema 1→2 保留队列/盐；整页原子打包、重启和容量失败不推进事实 checkpoint。
- Pass：实际观测首末时间、Cursor 真实周期历史、Grok 旧库当前快照、真实零、Credits 原始库存/详情/到期分布与 reset 分开；payload 不含原始 credit/request/file ID 或本地 generation。中心 linked history 验证自身 scope，拒绝其他设备证明，解除后保留原观测/receipt。

## 本机恢复观察

同步库位于应用私有 runtime 的 `reporting.db`，父目录 0700、数据库/WAL/SHM 0600。它包含 Pulse 自有设备凭证、安装级分区盐、白名单持久队列、来源 revision 与确认进度；不与 App 主库或中心库共享 schema。不得将其上传到 Issue 或提交 Git。

`ReportingStatus / PairReporting / ConfigureReporting / SyncReportingNow` 是本机 UDS RPC。配对后仍关闭同步，需要明确启用。默认间隔 60 秒，可设置 15–3,600 秒；追赶队列/分页时按 1 秒调度，单轮最多发送 32 批、每 Provider 导出 16 个会话，每轮整体 60 秒。暂时失败有界退避至 5 分钟；撤销/协议拒绝停止自动重试。队列最多 2,048 批/256 MiB，超大单会话或磁盘失败保留有限失败状态。

停用取消在途工作并保留队列。退出 App 关闭同一个 Helper 所有者，下次启动从持久分页游标和确认进度继续；无独立守护进程。改变中心获得新的客户端凭证，旧中心队列保留且不会发往新中心。首次补传起点在开始导出后固定，重新配对可选择新范围；清理待发送队列是显式本机动作，不删除中心数据。

HTTP 必须在 App 显式允许，连接目标仅限环回/LAN/Tailscale 地址；域名每次建连验证全部解析结果，并直接连接已验证 IP。HTTPS 使用标准证书校验。所有重定向被拒绝，不使用代理或自动降级。真实中心地址与配对码不要放进测试证据。

## Master 待验收入口

- Not Run：三台机器真实 Home 分别配置、读回设备与 Home、初次补传/退出重启/断网恢复，以及实际 Web 页面闭环。
- Not Run：真实 MySQL 连接、整体 E2E、并发/事务/字符集/时区/备份恢复；环境由用户后续提供。
- Not Run：生产部署、签名、公证和正式发布。

实际运行前说明将读取 Session/JSONL、写入各机私有 runtime、主 SQLite/偏好及 App Server housekeeping；保持真实 CODEX_HOME 的物理身份读回，三台机器分别取证。完整日志、凭据、正文和本机路径只留受保护且忽略的本机 artifacts，提交摘要使用更窄白名单。
# Web 总览开发证据（TOO-487，2026-10-01）

## 项目和会话开发证据（TOO-488）

服务端搜索/排序/分页、范围 totals、项目/模型/会话下钻和关联/解除接入。新增 2 个 Web 行为场景通过：跨页查询与搜索重置页码、范围 totals 保持、恶意标题普通文本与 raw Session ID 下钻；跨页保留成员、明确确认、CSRF 请求和成功清空选择。测试发现 AntD 表格卸载导致内部选中对象缺失，已改用页面保存的已选及当前页对象解析 keys。最新 typecheck、AntD lint 通过。

项目详情补齐 `models` 字段，与项目和分页会话同一读快照返回，避免并发更新期间拼接两次 API 的模型贡献；Go 聚焦项目关联/分页/模型对账通过。真实隔离 Server 浏览器验证：18 个独立同名项目；关联两个后变 17 个、目标 2 个成员及合并用量，范围 57,905,904 Token 保持；解除恢复 18 个。每页 10 条切第二页，范围 totals 及总条数保持。项目模型、会话及三份采集来源下钻可读，复用 API 普通文本转义。

`.artifacts/multi-machine/web/project-session.jpg`、`project-models.jpg` 为合成 UI 证据。仅隔离 SQLite/真实 HTTP 开发证明；真实 Home/三机/MySQL/CI/正式发布 Not Run。TPS 新增适配由 TOO-507 接续，不把当前会话页描述为 TPS 已完成。

使用独立 SQLite 中心与 loopback Go Server/Vite，无真实 Codex Home。经既有服务接收路径导入三份相同合成副本，12 个中心会话；浏览器真实配对并通过 HTTP 查询。全局 57,905,904 Token / $24.011668，Codex 筛选 9,650,968 / $4.001938；选择单一采集来源仍为本来源真实数值，未把三份副本累计。趋势/构成切换成本与折线，年度热力图和 KPI 范围分开；390px 布局可用。

开发聚焦证据：Web 11 个行为测试、typecheck 与 AntD lint 通过；statistics `TestStatistics` 聚焦组及新增明确零/未知历史测试通过。初次 GUI 发现无事实日被补零，已修复 Server 日/星期小时结果保留 NULL，明确零事实不变，并在真实隔离 API 图表读回。Web build 通过，有 500kB chunk 警告，构建拆分在 TOO-491 衔接；没有以提高警告阈值掩盖。

截图保存在 ignored `.artifacts/multi-machine/web/overview-desktop.jpg` 与 `overview-mobile.jpg`，只包含合成数据。原始浏览器/中心凭证不提交；测试库使用合成配对码，不用于真实账号资料。CI、真实三机、真实 Home、MySQL 与正式发布均 Not Run，未据此完成 Master 验收。
