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

## TPS 开发证据（TOO-507）

Pass：原生 61 轮完整并集与查询对账、加权/空闲排除、50 最近/61 总覆盖、待重建/继承/未知/真零、历史起点不泄露旧生命周期；TPS-only revision、持久重启原 body 与 tombstone；严格输出对账/预算/版本 426/raw Turn member 400；中心三份复制只计一次、日期范围与整体分离、来源选择/冲突、collector 查询拒绝。root store/reporting/contract 与 Server statistics/http 聚焦测试通过。

Web TPS 3 场景与 Records 2 场景共 5/5，通过 typecheck、AntD lint；Helper/Server 构建通过。真实隔离 HTTP 浏览器：合成 16,327 output / 730,364 ms 在列表/详情均 22.36 TPS；61 轮整体 1.84，20→50 截断与 30→7 日筛选不改平均；390px 可读。截图 .artifacts/multi-machine/web/tps-reference.jpg、tps-mobile.jpg 为合成资料。

复用现有 source/canonical JSON，无新增 DDL；均值用纯 Go 重算。真实 Home 原生 App、三机、MySQL、CI 与发布未运行，不构成 Master Pass。

## 额度 Web 开发证据（TOO-489，2026-10-02）

Pass：4 个聚焦行为场景覆盖真实零/未知、陈旧过期无倒计时、刷新失败保留原值/原时间、同邮箱原始账号独立、Provider/设备/账号 query、稀疏预测、精确 Credits 与到期/reset 分离、名称普通文本转义、下降采样与原端点/无插值。typecheck、AntD lint 与 Web 构建通过；chunk 警告由 TOO-491 继续处理。

真实环回 HTTP/隔离 SQLite 浏览器读回：原账号陈旧 45% 保留且无倒计时/预测；追加同邮箱另一账号后保持独立，主/次窗口 70%/20%，主窗口中心预测提前 15 分钟耗尽，次窗口实际采样不足，两个首尾覆盖历史周期/上一周期可切换。三份副本的 Credits 观测库存 3、已到期 1、可用 2，不相加，next expiry/reset 日期独立。quota/pace 各自评估时间、21/36 条来源证据与去重采样分开显示。390px 可读，截图 .artifacts/multi-machine/web/quota-pace.jpg、quota-mobile.jpg。

所有账号/历史/机器均为合成资料，未读取真实 Home；MySQL、三机正式验收、CI 和生产部署/发布 Not Run。

## 设备与原生设置开发证据（TOO-490，2026-10-02）

Pass：Server 新增未用码撤销/消费冲突、改名字段白名单/权限/CSRF、名称长度、重复名称更新和凭证/用途保持；真实 CoreService 经私有 UDS 配对、启用、增量上报到真实 HTTP/SQLite 中心并读回确认，管理端撤销后进入 reconnect_required/保留未确认队列，关闭 Native Runtime 后停止发送。测试全部使用空 synthetic Home/独立库；RPC 有整体超时，所有临时材料不进入证据。server/http/architecture/access-srv/app-cmd 聚焦检查通过。

Swift --reporting-only 通过，覆盖默认关闭、显式 HTTP、短暂码清空/失败不重试、历史起点及清理时关闭/保留实际范围、旧读回丢弃、失败不会被后台读取抹掉与 stop 清码。Swift App 和独立 Development bundle 构建通过。新增测试复用根模块已锁定的 gRPC/Native 依赖，由 server/go.mod/go.sum 记录。

Web 3 场景通过：管理码权限确认、内存码撤销且 DOM 清除、过期/缺失来源不伪造、失败撤销保留客户端/错误、恶意名称普通文本、各表单独立标签与改名 CSRF；最新 typecheck/AntD lint/build 通过。真实隔离浏览器签发合成采集码→撤销未用码、改名/读回/恢复原名、桌面和 390px 可用。发现并修复关闭 Modal 留码 DOM 与两个 Form 字段 ID 重复。

真实 Home Development App 本机界面读回：未配对，上报/HTTP 关闭、间隔 60 秒、队列 0、未配对操作禁用；App/Helper 环境均匹配真实 Home、prefs canonical path/inode 与 0700 runtime、Helper 参数/UDS正确；正常退出二者进程和 UDS 消失。未启用真实资料上报。原始证据仅本机 ignored artifacts；提交摘要无真实账号/路径/日志/码。三机真实上报、MySQL、CI、生产部署/签名/公证/发布 Not Run，正式验收留 Master。

## 运行交付开发证据（TOO-491，2026-10-02）

Pass：Server 同源壳/hashed assets GET/HEAD，业务 API 继续授权，隐藏文件/map/逃逸符号链接被拒绝；缺失静态产物拒绝启动；版本白名单仅管理员可读。SQLite 一致备份、schema/integrity/hash 检查、原子恢复到新路径、拒绝覆盖/损坏备份，恢复后旧授权与未用码失效而事实/receipts/reset/原观测时间保持。聚焦 http/app-cmd/config 与 Starter 架构检查通过，部署示例严格配置解码通过，MySQL 脚本 bash 语法通过。

独立 Server/Web 及 package-center 构建通过；Web typecheck、AntD lint、4 个浏览器会话场景通过。保留模块独立 Go 版本；Rolldown 分块后无旧大包告警。实际环回 HTTP 打包 Server 直接托管 SPA，同源配对后读回 57,922,841 Token/14 会话、额度/历史/节奏图表和中心协议版本；浏览器不依赖 Vite。证据 packaged-pace.png 为合成资料。

对运行中合成 SQLite 中心执行在线 CLI 备份、恢复到新库及 db check。离线只读对账 13 张事实/收据/结构表逐行相等：14 会话、42 来源、302 用量、61 配额观测、9 receipts；恢复库有效客户端和未消费码均为 0。比较原证据仅 ignored backups/restore-comparison.json；未上传或提交个人记录。

交付同域构建打包、HTTPS/显式 Tailnet HTTP、非 root 常驻、结构检查与升级/回滚、保留/撤销/删除区别、SQLite/MySQL 备份恢复和 Master 联调 runbook。容器模板按 monorepo/统一配对更新，未构建或启动 Docker。真实 MySQL、三机上报验收、HTTPS 生产代理、CI、签名/公证/发布 Not Run，留 Master。用户提供 MySQL 后按 runbook 整体验证，SQLite Pass 不替代它。

交付自查：本次新增接口经过统一管理员鉴权，静态根受限且无任意 API 放行，SQL 参数化/固定语句，恢复拒绝覆盖且撤销旧授权；未在代码/模板/日志/产物环境写入 Agent 凭据或原始内容。

## 缓存命中率开发证据（TOO-509，2026-10-02）

聚焦 Go contract、store 与本机 native Rollup 对账、持久队列重启/指标修订/删除、原生 mapper、中心 statistics 测试通过。覆盖整段输入与缓存、受限历史不泄漏、日期范围只命中部分贡献仍保留生命周期比例、三份副本/来源筛选、定价独立、零/全命中/缺失/非法/半值/大整数。Web 两个格式及缺失状态测试与 typecheck 通过。开发测试使用合成事实与隔离 SQLite，无 MySQL 或三机正式验收结论；整体 Web 浏览器证据与新版布局另在 TOO-510 记录。

## Web 整体重构开发证据（TOO-510，2026-10-02）

Pass：按 AntD 6.6.5 CLI info/demo 与官网 Layout/Table/Tabs 示例重构侧栏工作台、登录、全部业务页面、详情与窄屏导航。受影响的 7 个 Web 测试文件共 22 个行为场景通过（分聚焦运行）：授权与撤销、恶意名称转义、查询/分页与跨页关联、缓存/TPS 精确展示与未知、Credits/reset、失败保留数据、设备表单/确认/短暂码清理，以及最近轮次重新读取后保持 TPS 标签和生命周期缓存比例。typecheck/AntD lint/生产构建通过；新增 HTTP 缓存接收/重复确认、collector 禁止全局读取、精确大整数 DTO、未知字段 400/版本 426 通过，中心模型/范围/limit 筛选仍保持生命周期率通过。

实际环回 HTTP Server 直接托管新版 Web，隔离 SQLite：接收三份合成副本后总览读回 57,925,841 Token / 18 个会话，缓存 90.0%/0.0%/100.0%/零输入未知，项目内会话与详情一致；来源保留 3 份，消耗不相加。项目嵌套下钻/返回与模型标签、窗口切换/真实历史曲线、陈旧和过期不提供可信倒计时或预测、设备表单与上报状态均读回。

390px 的总览/项目/会话/额度/设备主页面布局和导航抽屉已检查，页面 documentWidth 为 390；会话详情保留原始 ID、90.0% 与输入 1,000/缓存 900，详情 Tabs 可切换或从官方溢出菜单进入。浏览器临时尺寸已恢复，截图仅 ignored .artifacts/multi-machine/web/refactor-*.jpg，其中总览桌面与会话窄屏为最终交付截图。测试与浏览器数据全部合成，未启动或上传真实 Home。

自查修正：TPS 请求变化导致标签重置、窄列单位拥挤、主导航滚动位置保留、构成前 12 项提示。运行中在 Vite build 替换 assets 后旧目录句柄仍绑定旧 inode，重启隔离预览 Server 恢复；部署文档明确使用新版本目录与重启，不在运行目录原地构建。最后的前 12 项提示为收尾自查恢复，收尾按约定不重复运行测试或 lint。

安全自查：沿用管理员 Cookie/内存 CSRF、后端授权和统计白名单；React 文本转义、ECharts 文本 tooltip、有限原因、NULL/零和 stale/partial 保留。没有本地存储凭证、新增匿名接口、正文或秘密上报。真实 MySQL、三机正式上报、生产 HTTPS/CI/部署/签名/发布 Not Run，Master 仍待相应环境验收。

## 汇总、掘金热力图与 Mac 单位对齐（TOO-510，2026-10-02）

Pass：布局对照 Mac DashboardSummaryView，热力图对照掘金实际 Dashboard 与官方组件。年度格子采用整数像素、3px 间距、日到六标签、同列月标、实心蓝色正值阶梯和底部图例；未知为实心浅灰，真零最浅蓝，不按参考实现补零或裁掉历史。客户端/模型独立环图含全部已知正值，全部明细保留 NULL、零和有符号差额；年度指标收进展开项，陈旧/覆盖提示可见。

Go 聚焦通过 TestStatisticsActivityObservedFacts、TestStatisticsActivityUnknownAndZero、TestStatisticsGlobalDedupAndActualCollectorScope：任意精度年度峰值/总量、未知与真实零、今天为零截至昨天、未知日期不声明精确当前连续天数、三份副本去重及独立年度范围。新增只读 heatmap_activity，无 DDL、上传字段或本机 runtime 变更。

Web 6 个受影响测试文件共 17 场景通过；点选触发修复后再次聚焦运行 ActivityHeatmap 的 3 场景通过。覆盖 Sunday/跨月/DST 对齐、50/75/90 视觉分位的大整数、未知/零、键盘移动与单次点选提示、服务端独立年度指标、失败保留数据、列表/详情/关联、缓存/TPS，以及中文 Mac 万/亿单位、一位小数、单位晋升、half-even 与微美元两位显示舍入。typecheck、AntD lint、Go/生产 Web 构建通过；Foundation 格式器小样本读回 1.25→1.2、1.35→1.4、5792.5841→5792.6。未运行原生 App 或读取真实 Home。

真实环回 HTTP + 原隔离 SQLite 合成事实：底层总量仍为 57,925,841 Token / 18 会话，页面为 5792.6万 / $24.01；趋势轴万/亿、环图和明细采用同一显示格式。390px documentWidth=390，格子14px、星期固定左侧、横向可访问全年；单次点选提示 2026-09-12 / 509.6万。1280px 为14px正方格，365个日期齐全。视口已恢复。最终截图仅在忽略目录 .artifacts/multi-machine/web/aligned-overview-top.jpg、aligned-overview-desktop.jpg、aligned-heatmap-mobile.jpg。

## 账号归属与左右分屏开发证据（TOO-510，2026-10-02）

Pass：Quota/Records 两个受影响 Web 测试文件共 11 个行为场景通过。包括同邮箱的不同账号不串额度或 Credits、只有 Credits 的账号、待关联记录、真实零/未知、陈旧过期无倒计时、刷新失败保留事实、账号/来源筛选；桌面列表与详情同时可见，无详情 Drawer/Modal，分页保留详情而范围变化清空；项目内会话原位切换与返回恢复页码/页签；跨页关联、全选入口、CSRF、标题文本转义及 TPS 标签/生命周期缓存比例。最终 Web 类型检查/生产构建和 AntD lint 通过。JSDOM 的 pseudo-element getComputedStyle 提示不代表真实浏览器视觉结论，布局另行检查。

实际 loopback HTTP Server 直接托管最终 Web，复用原独立 SQLite 合成库：同邮箱的两个原始账号分别显示 70%/30%、库存 3，以及 45%/55%、库存 2；陈旧库存可用数量保持未知，实际 reset 与到期分开。待关联 Cursor 观测独立列出，不归入确认账号。总筛选范围仍为 5792.6万 / $24.01 / 18 会话。

1280px 项目/会话两侧同时可见、无详情抽屉；实际拖动分隔线使左侧宽度由约 340px 改为 400px。项目内会话在右侧原位显示，实际可见会话点击前项目滚动位置为 330px，返回后恢复 330px。会话分屏读回生命周期 TPS 1.84、61 轮次和来源，缓存详情读回 90.0% 与输入 1,000/缓存 900。390px 会话与项目逐级进入/返回可用，详情模式保留列表 DOM；额度、项目、会话均无页面横向溢出。临时视口覆盖已恢复。

截图、测试/构建输出仅在忽略目录 `.artifacts/multi-machine/web/account-split-*`，包含 `account-split-projects.jpg`、`account-split-sessions.jpg`、`account-split-quota.jpg`、`account-split-session-mobile.jpg`。本轮仅 Web 呈现与状态调整，复用现有账号键、Server 查询与仲裁；没有 DDL、上报协议、Agent 凭据、原始内容或本机采集变更。真实 Home、三机、MySQL、生产/CI未运行，不作为本轮验收结论。提交推送收尾不重复测试。

自查：业务事实由 Go 查询快照计算，Web 只格式化与绘制；NULL、已知小计与观测缺口保留，未知边界不冒充连续天数。复用管理员权限、白名单及 React/文本图表转义，无新匿名路由、外部请求或秘密记录。MySQL/三机/生产验收与 CI Not Run；提交推送收尾不重复测试。


## 模型价目、用量成本与订阅设置开发证据（TOO-513，2026-10-02）

实现说明见[设计明细](../design/details/multi-machine-reporting/pricing-usage-subscriptions.md)与[中心接口](../../server/api/catalog-subscriptions.md)。本轮新增 catalog/subscription 域、模型日桶查询、账号设置表及显式 v1→v2 升级；本机采集、原始数据和认证文件不参与本轮开发预览。

Pass：订阅保存/清除、首次插入冲突与旧修订冲突、同邮箱账号隔离、设备资料更新不覆盖手动值、非法日期/时区/字段组合、每月31日短月/闰月/滚动与IANA自然日；目录当前/历史/未知模型、重复费率投影与官方证据空值；用量三来源去重、真实模型日桶和历史费率保持。Codex样本输入1,000,000、缓存500,000、输出100,000、独立reasoning10,000，历史费率2/0.1/10 USD每百万，得到1,110,000 Token及$2.15。匿名401/collector403、写入CSRF403、更新200/修订冲突409/未知秘密字段400通过实际HTTP装配验证。Starter业务包/每表一个DO的架构检查通过。

Pass：Web新增/受影响5个测试文件17个行为场景，后续额度与价目调整的2个文件8个场景通过；覆盖设置提交修订与CSRF、失败/冲突、模型文本转义、危险来源链接拒绝、估算与上报费用/部分金额区分、真实日桶与未知日期、既有额度/项目会话行为。类型检查、AntD lint与最终前后端构建通过。JSDOM pseudo-element getComputedStyle提示属于测试环境限制，视觉布局另外在浏览器检查；提交推送收尾不重复执行测试。

Pass：独立SQLite预览库升级前备份，显式v1→v2后旧15张表计数保持，18个会话仍在；启动检查不会代替升级，未知摘要拒绝。真实loopback Go Server托管最终Web。浏览器保存合成账号备注、手动套餐和每月31日续费，读回2026-10-31/29天及revision=1，刷新仍在；同邮箱另一个原始账号仍为自动套餐、未设置日期，窗口与Credits独立。全局用量5792.6万/$24.01保持，模型成本趋势切换与价目来源/版本展开可操作。

目录含296条公开参考费率及24条订阅套餐资料，另含本机历史和已上报结构化费率、未定价模型。公开目录核对日期为2026-10-02，不伪造精确生效时间；参考目录不覆盖历史成本。390px额度、用量、价目页面documentWidth=375，无页面横向溢出，宽表在容器内滚动；临时视口已恢复。截图仅保存于忽略目录`.artifacts/multi-machine/catalog-usage/`，包括quota-desktop.png、usage-preview.png、quota-mobile.png、usage-mobile.png、pricing-mobile.png，均为合成资料。

Not Run：真实MySQL升级/并发/事务/备份恢复、三台Mac真实上报与订阅同步、CI、生产HTTPS/部署/发布。中心设置没有Mac双向同步；不执行续费或扣款。正式环境验收由Master承接，SQLite开发证明不替代它们。

安全自查：新增接口复用admin权限、精确Origin与CSRF；白名单字段/正文预算、SQL参数化/修订CAS、React转义/官方HTTPS来源链接、模型与日桶预算已核对。未新增服务端网页抓取、匿名写入、秘密日志或原始JSONL/正文上报。


## 方案 1 Web 视觉与信息架构（TOO-516，2026-10-02）

按用户选定首张方案重构现有 AntD Web：用量概览/模型/采集来源集中，账号独立；紧凑筛选、成组指标、首屏自然日趋势、并排明细与真实来源、全年14px日历。质量与年度数据按钮打开可点击证据；环图、工具保留。模型趋势/选择提前，价目按近30天实际用量模型优先；旧用量深链保留参数。陈旧/过期/冲突主额度未知，最后可信百分比/原观测仍在，进度条只对当前可信比例显示。项目会话继续左右分屏，订阅/Credits/TPS/缓存未移除。

开发 Pass：首批9个受影响测试文件29场景通过、来源新测试修正既有NULL日期文案后1场景通过；新增范围/自定义与部分月标签、随后调整的3文件10场景通过。总计32个不同场景有通过证据，未用失败首轮充作Pass。覆盖范围请求/真实自定义日历、服务端统计/年度独立值、未知/零、失败保留数据、历史金额/上报金额分离、过期未知优先/无当前进度条、账号隔离/订阅、项目关联/分屏/授权、恶意文本、来源撤销与原时间、日历星期/月标/视觉分位/键盘。类型、AntD lint与最终生产构建通过。JSDOM不提供浮层布局，单测验证日历内容存在，实际可见位置由Chrome另证。

真实loopback HTTP/独立合成SQLite Pass：首页5792.6万/$24.01/18保持；1487×1058首屏含摘要、完整趋势、两块明细与完整365日期日历（14px）。Product Design原图与实现放在同一比较图，四轮修复密度/裁切，最终无待修P0/P1/P2，细节和历史见根`design-qa.md`；该文件保留此前原生QA记录，不能用原生结果代替本轮Web。图稿第四平台、假新鲜和平台充当设备来源已纠正；日汇总不伪造平台分摊，模型页仍是真实日桶。

Chrome检查自定义日历、年度/质量证据浮层、模型成本切换、价格搜索/跳转、旧深链、账号切换与订阅编辑打开/取消、项目/会话桌面分屏、设备入口。390×844概览/账号/模型/价目/来源、导航及会话缓存详情无页面横向溢出；文档宽375，宽表/全年日在局部容器滚动。捕获到的console error列表为空。截图与日志仅ignored`.artifacts/multi-machine/design-review-20261002/`，未读取真实Home或修改已有合成订阅。

交付自查：管理会话、同源CSRF与服务端权限继续有效；普通文本与图表文本转义、准确NULL/部分估算/原时间、来源与执行归属边界保留。无Go/SQL/网络contract/本机runtime变更，无新匿名接口、凭据存储或原始内容上报。MySQL/三机/生产/CI未执行，正式验收留Master；提交推送收尾不重复测试。


## 热力图置顶与宽度自适应开发证据（TOO-516，2026-10-02）

按最新用户要求，总览顺序修正为筛选、全年热力图、指标摘要、趋势与并排明细/采集来源。日历使用等宽正方格填满内容区，最小14px和3px间距；不删365日，年度范围/指标与上方统计日期独立。末列月份右对齐，避免文字造成多余滚动条；热力图独立于懒加载图表呈现。

Pass：Overview与ActivityHeatmap两文件9场景通过，包括新的活动→摘要→趋势顺序、独立年度服务端值、日期/大整数分位/未知与零、键盘选择与失败保留数据；边缘修正后ActivityHeatmap4场景再次通过。最终类型检查/生产构建与AntD lint通过，0问题。JSDOM的pseudo-element提示未当成浏览器布局结果。

Chrome真实loopback HTTP、原合成SQLite：1280×914方格14.828px，日历963px，容器clientWidth/scrollWidth同为969px；默认1971×914方格24.469px，日历1474px，容器同为1480px，正方形并填满。活动/摘要/趋势依次约y=122/431/562，365格齐全。390×844方格14px、局部横向滚动可通过End访问2026-10-02，星期固定；documentWidth375px无页面横向溢出。摘要5792.6万/$24.01/18保持，console error为空，视口已恢复。

QA见根design-qa.md，截图与日志仅在ignored `.artifacts/multi-machine/heatmap-top-20261002/`，旧原图底部/固定方格规则已明确被本轮要求覆盖。安全自查：仅Web布局，复用管理授权与文本转义，无新接口、外部请求、凭据或原始内容记录。未读真实Home，MySQL/三机/生产/CI仍Not Run，正式验收留Master；提交推送阶段不重复测试。


## 全站Storybook设计稿（TOO-517，2026-10-03）

本轮仅独立合成设计，正式业务页面未替换；范围与公开参考见[设计稿说明](../design/details/multi-machine-reporting/storybook-design.md)。入口`npm --prefix server/web run storybook`，127.0.0.1:6007，无需Server/数据库。

Pass：九页/八个关键状态/五个公共组件共22个可查看条目；聚焦类型、AntD lint（0问题）、最终Storybook静态构建通过，新增依赖后的原Web构建通过。实际Chrome检查范围/年度浮层、模型/成本、订阅保存取消/账号隔离、Credits说明、项目会话分屏返回、设备改名/模拟配对及三平台套餐；最新节奏按Mac四指标/四线/推算与对比组织，明细展开与上一周期键盘切换读回。密集记录隐藏圆点，普通采样实线跨缺口连接已知两端，NULL观测不转为事实。

Pass：390px九页、分屏详情与最终完整概览/节奏/陈旧账号检查，无全页横向溢出，日历与宽表局部滚动。首轮账号Grid溢出、工具条换行、星期背景与重复React key已修复；story热更新期间瞬态错误保留记录，稳定后最新检查无新console error。完整迭代/五类视觉面在根design-qa.md，截图和构建日志只在ignored`.artifacts/multi-machine/storybook-20261003/`。

Not Run：真实API对账、原生App/真实Home、三机/MySQL/生产/CI。设计数据与价格为固定代表样本；编辑、关联、配对与撤销只改变当前组件内存，不是业务持久化证据。没有为可逆图稿新增镜像实现单测；提交推送收尾不重复测试。

安全自查：无业务接口/权限改变，独立Vite无API代理，设计业务数据不持久化，无真实凭据/原始内容，React和richText输出保持文本。正式验收仍归Master，用户设计审核与业务实施待后续。

## 已批准全站设计接入正式Web（TOO-519–522，2026-10-03）

用户批准Storybook后已完成八个工作台页面及独立配对表单适配。业务继续读取真实中心API，设计样本不进入业务入口；18085托管新版，6007保留独立设计。概览保留置顶365日热力图、完整统计/分布/工具/来源与模型折线；紧凑筛选、账号内Credits/订阅、Mac节奏层级、记录分屏、完整价目与真实设备授权均已落地。范围缓存字段为兼容可选查询能力，不修改上报协议/数据库或会话生命周期口径。

开发Pass：44个不同Web场景分批通过（Overview5、Usage2、SourceTable1、ActivityHeatmap4、Quota7、SubscriptionPanel2、Records5、Pricing2、Devices3、CacheHitRate2、Throughput3、授权/client8）；后续仅复跑受影响场景，最终视觉细节四文件15场景、记录五场景通过。最终TypeScript、全src AntD lint 0问题、业务构建通过。Go statistics_srv聚焦Usage/RangeCache/CacheHit测试通过，含7类范围边界、大整数精度、来源去重、生命周期独立、NULL/真零/未知/权限；Go import格式化无额外改变。未把初轮失败输出当Pass，提交推送阶段不重复测试。

真实loopback HTTP/原合成SQLite Pass：概览5792.6万/$24.01/18和365日保留；Codex范围/模型缓存25.0%及63.3%、会话生命周期90.0%读自实际Server，混合/其他平台未知。陈旧窗口主值未知、当前曲线不画、预测暂停且原因可查，历史/理想线保留；Credits中心可用未知、原观测库存3和原时间可查，到期与reset分开。订阅备注真实修改、重载读回并恢复原备注，day31/套餐/时区保持；修订号仅在合成库增加。同邮箱账号隔离、桌面记录分屏和手机返回、搜索清除、价目/来源展开有操作证据。

八页桌面1487×1058和390×844检查：documentWidth分别1472/375，无整页横向溢出，日历/宽表局部滚动。首次自定义双月popup将窄屏撑至717，已改为上下月和内部滚动，最终popup宽320、document375。单桶曲线补小marker、模型配色/来源行/明细会话列/工具技能切换修正后有最终截图与受影响验证。浏览器最终console error/warning为空；临时视口已恢复。完整批准稿对照和五类视觉面见根`design-qa.md`；原始证据仅ignored `.artifacts/multi-machine/web-approved-20261003/`。

安全自查Pass（diff范围）：既有admin授权、Cookie/Origin/CSRF、collector隔离、参数化查询与预算、修订CAS、React文本/richText及官方HTTPS来源限制保持。缓存比值由Go完整计数计算，未知不伪装成零；未新增原始内容/秘密上报、日志或匿名接口。使用原浏览器授权，没有新签发管理凭证。

Not Run：本轮匿名配对界面单独浏览器验证、真实MySQL/升级并发/备份恢复、三台Mac真实上报、原生App/真实Home、CI、Docker、生产HTTPS/部署/发布。签发码/撤销真实客户端未再执行，沿用授权/client行为测试；SQLite开发证明不替代上述验收。四张Execution均指派用户、同一原milestone、归Master TOO-477；整体验收及后续MySQL复测仍在Master。
