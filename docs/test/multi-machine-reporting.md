# 多机中心开发验证与后续验收

总体方案：[多机汇总与 Web](../design/details/multi-machine-reporting/README.md)。确定性开发证据使用隔离 SQLite 与 synthetic/empty Home；2026-10-03 补充了本机真实 Home Live E2E，以及 DEV SeekDB/MySQL 协议与 Tailscale 三机 HTTP 联调，见文末。各证据不能互相替代，不代表三台原生 App 全矩阵或正式部署已验收。

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

2026-10-03 更新：本机一台真实 Home 的原生配对、上传、断网恢复、退出重启、设备撤销与新版 Web 首次授权已完成下述 Live E2E；三台机器分别运行及跨机去重/账号关联的完整矩阵仍为 Not Run。MySQL 本轮按用户要求排除。

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

## 首次授权与本机真实 Home Live E2E（2026-10-03）

用户授权补做现有环境可完成的 Live E2E，MySQL 明确排除。使用新构建的 Go Server、正式 Web 和原生 Development App；中心为本机环回 HTTP/独立 SQLite。原生 App 显式绑定真实 Codex Home，并使用从已核实私有 runtime 复制的独立 0700 runtime。每次启动均读回 App/Helper 环境、Helper 参数、preferences 的 canonical path/device/inode；没有覆盖日常 App 的偏好或停止日常 App。

| 场景 | 结果与证据范围 |
| --- | --- |
| 匿名首页与 API 边界 | Pass：新版浏览器能看到授权表单；无授权的业务/会话 API 为 401。 |
| CLI 首次授权与访问恢复 | Pass：同一服务配置的 `db bootstrap` 生成一次性码，浏览器配对进入、刷新恢复；退出回到授权页，已消费码重放被拒绝，再经 CLI 新码恢复。 |
| Origin/CSRF | Pass：真实 HTTP 请求缺少 CSRF 或使用其他 Origin 时写入为 403，已授权查询为 200。 |
| 原生配对与设置 | Pass：真实 UI 配对后仍关闭上报；显式允许环回 HTTP、保存启用及 600 秒间隔，Helper 和同步库读回一致。退出重启后间隔与开关保留；未连续等待一个完整 10 分钟自动周期。默认间隔仍为 60 秒。 |
| 实际上传与中心查询 | Pass：首轮队列排空，中心确认结构化会话、用量、额度和 Provider 状态；真实 Web 会话页加载，HTTP 列表/详情返回缓存命中率与 TPS。收到事实不等于全部历史或年度覆盖完整。 |
| 断网与采集隔离 | Pass：只停止本轮隔离中心，原生状态变为 `offline`，保留 3 个未确认批次；中心表计数不变，同时本机 active token scan 的输入、输出和更新时间继续推进。 |
| 断网恢复与幂等 | Pass：恢复中心后队列排空，3 个积压批次均找到唯一中心收据，digest 与离线队列原始正文 SHA-256 一致。 |
| 带积压退出与重启 | Pass：带 1 个未确认批次正常退出，App、Helper 和 UDS 均停止，持久队列保留；使用同一 runtime 重启后继续补传，原批次获得唯一且正文匹配的确认。 |
| 撤销采集设备 | Pass：撤销本轮测试设备后进入 `reconnect_required`，保留 2 个未确认批次；再次请求立即同步被拒绝，最近尝试时间与中心事实不再推进，本机采集仍继续更新。 |
| 设置页位置调整 | Pass：在上述 Live E2E 完成后，仅将“多机中心”移至“本机数据”之后、设置页最后；重新构建并在真实 Home App 中检查标题顺序和界面，配置逐字段与调整前一致。没有为顺序调整新增或重跑全量测试。 |

验收发现并修复真实启动缺陷：部署示例的 `corsOrigins: []` 会让 Echo v5 的 CORS 中间件在构造阶段 panic。空列表现在跳过 CORS 中间件，继续使用同源入口及原有 Cookie/Origin/CSRF 鉴权。新增回归测试验证静态授权页、匿名 API 拒绝、授权会话、缺失 CSRF 与其他 Origin 拒绝；既有非空精确白名单保持。

本轮聚焦命令：`go test ./internal/http -run '^(TestUnifiedPairingCSRFRevocationAndPermissions|TestHTTPSUsesSamePairingAndSecureSession|TestNativeCoreReportingPairUploadRevokeAndClose|TestSameOriginServerWithoutCORSRetainsAuthorization|TestStaticWebRootAssetsAndAPIAuthorization)$' -count=1`（server module），Pass。实际 HTTPS 测试使用确定性测试服务，不能代表生产代理/证书部署已验收。Go Server/Web 构建和 Development App 两次构建成功；UI 顺序修改之后只补做新构建的界面与配置读回。

原始数值观测、私有测试库、构建日志及截图仅保存在忽略提交且受保护的 `.artifacts/multi-machine/live-20261003/`，不提交真实 Home 路径、账号资料、正文、配对码或凭证。收尾关闭本轮测试上报、撤销测试设备与管理会话、退出 Development App、停止隔离中心；日常 App 和既有预览保持运行。

剩余 Not Run：三机分别绑定真实 Home 的完整 HTTP/Tailscale/HTTPS 与跨机去重、账号切换/历史关联、真实 MySQL（本轮排除）、生产反向代理/证书/备份恢复与正式部署。已有的合成边界测试不能升级为这些场景的 Live 证明；本轮没有执行 CI、签名、公证或发布，也未据此宣称 Master 整体通过。

安全 diff 自查：只移动 Swift Section，Go 空 CORS 列表保留同源鉴权及 CSRF，不新增匿名业务接口、网络暴露、凭据存储或上报字段；开发验收数据与凭据未进入提交内容。

## 2026-10-03 内嵌 Web 与单体部署

用户确认中心不采用前后端分离部署，CORS 默认 `*`。Web 正式产物通过 Go `embed` 编入 Server；运行时移除 `server.webDirectory`，一个二进制同源提供页面、hashed assets 和 API。Make 的本机/跨平台构建先生成 Web，打包不再附带外置 Web；Dockerfile 使用 Node → Go → 非 root 运行镜像的构建流程。README、配置和升级说明同步，旧配置须删除外置 Web 开关。

Pass：Web 类型检查/构建、Go 本机构建与部署包；受影响 config、HTTP、CLI 生命周期共 15 个顶层聚焦测试通过（含子场景）。覆盖省略配置时 CORS 默认 `*`、显式空列表、精确白名单、混合通配拒绝；通配响应不设置跨域凭据许可。同源浏览器配对、Cookie、Origin/CSRF、用途权限、撤销、HTTPS Cookie 与 native Core 上报 contract 回归通过。静态首页及所有内嵌资产可读取，HEAD 无正文，资产 immutable，隐藏文件/source map/穿越路径拒绝，匿名业务 API 仍为 401。

Pass：将部署包二进制复制到独立私有临时目录，仅保留二进制、配置与隔离 SQLite，无 Web/dist 目录。配置省略 corsOrigins 后实际启动：ready/index 为 200；首页引用的 30 个资源 GET/HEAD 成功；其他 Origin 的预检为 204/`Access-Control-Allow-Origin: *`，无 `Access-Control-Allow-Credentials`，匿名业务 API 为 401。Chrome 实际浏览器首次授权 → 刷新恢复 → 会话懒加载 → 退出回到授权表单通过，浏览器错误数 0。仅验证中心部署链路，本轮未启动原生 App、读取 Codex Home 或执行 MySQL。

Pass：macOS/Linux × amd64/arm64 构建和 Mach-O/ELF 架构读回。Fail：Windows amd64/arm64 交叉构建因既有共享配额算法间接依赖本机 Unix diagnostics/sqlite 包失败；本轮没有修改这些依赖或移除平台目标。Not Run：Docker 实构/容器运行（本机无 Docker）、MySQL、三机及生产 HTTPS 部署。官方 Node 构建镜像 tag 已在 registry 确认存在，不能作为完整容器构建证据。

原始本机证据在忽略目录 `.artifacts/multi-machine/embedded-20261003/`，包含构建输出、部署包、HTTP 结果和脱敏浏览器截图；隔离服务已停止，明文测试配对码已删除。原有应用与预览未重启。安全 diff 自查：静态资源只读内嵌、无外置路径访问；默认通配关闭跨域凭据许可，统一鉴权/入口/CSRF 保留；无新增凭据、上报字段、数据库迁移或公开业务接口。

## 2026-10-03 开发/正式配置与启动自动迁移

用户确认只有开发与正式两套环境，采用二进制部署，Dockerfile 可以保留但不安装/运行 Docker。新增 MySQL 开发/正式 `*.example.yml` 模板与环境说明；本机创建对应 `*.local.yml`（0600），`config/*.local.*` 被 Git 和镜像上下文排除。开发环回端口 18089、开发库与正式库分别配置，正式模板采用环回 18090 与受信任 HTTPS 代理。真实地址/账密只由用户填写本地副本，本轮没有读取或连接实际 MySQL。

中心 Fx 生命周期在 HTTP 监听之前自动执行版本化 Migrate：空库初始化到 v2、已确认 v1 自动升级到 v2、当前结构重复启动只检查；未知版本/摘要及已检测到的字段漂移拒绝启动。原 SQL 内容、checksum 与 schema 版本保持不变；不调用 ORM 猜测结构，不删除业务历史。MySQL 迁移使用同数据库摘要命名锁，锁和 DDL 固定同一连接，避免单连接池死锁；取消后有界释放，失败连接丢弃，不把残留连接级锁返还池中。迁移预算默认 2 分钟，普通请求 context 预算保持不变。

Pass：`go test ./config ./internal/lib/gorm ./internal/repository/mysql/schema_repo ./app/cmd -count=1`，覆盖自动初始化、已登记升级、重复执行、事实/收据保留、未知结构拒绝、SQLite 失败回滚、MySQL 锁成功/超时拒绝/迁移失败/取消清理/释放失败语义，以及 Fx 启动成功/拒绝。后续新增配置预算与显式 SQLite 夹具分别以 config/app-cmd 聚焦测试补验；固定连接的单连接池事务、失败丢弃和跨 Engine 拒绝在 gorm 包测试补验。SQL mock 不代表实际 MySQL 已执行。

Pass：真实开发二进制在独立私有 SQLite 中，不执行 db init/upgrade，空库直接启动得到 ready 和内嵌页面 200；重启不改写 schema 标记；模拟已确认 v1 后重启自动升级 v2，已有收据 digest/receivedAt 保留；模拟未来 v99 时启动失败且端口未监听，版本不被降级。原始证据在忽略目录 `.artifacts/multi-machine/automigrate-20261003/`。全部隔离进程已停止，不读取 Codex Home；实际 MySQL、生产部署仍 Not Run。

安全 diff 自查：私有配置 0600 且实际验证 Git 忽略；迁移 SQL 仅来自受控内嵌定义，锁参数化、连接与事务不跨 Engine，结构异常不伪装成功；不新增 HTTP 接口、匿名业务访问或上报字段。常规备份保留，MySQL DDL 部分提交语义在运行说明明确。


## 2026-10-03 DEV 数据库与 Tailscale 联调

用户提供已配置的 DEV 私有连接并授权最后经 Tailscale 验证。先实时确认三台机器身份及地址；在当前中心机器选择空闲端口，仅绑定自身具体 Tailscale IP，精确 Origin 与其对应。按用户确认将两套 Web/Server 默认端口统一为 DEV 18089、正式 18090，MySQL 连接端口保持实际实例值；同步公开示例、本地副本和 HTTPS 代理示例。现有服务、正式配置账密和正式数据库未变更。用户原配置中的 `database.database` 是 MySQL 实际库名；`database.name` 不替代它。

DEV 实际后端为 SeekDB 1.2.0.0，`VERSION()` 读回 MySQL 协议版本 `5.7.25-OceanBase seekdb-v1.2.0.0`，不能表述为独立 MySQL 8.4 验收。初次只读检查确认库内无表。现有数据库不支持 TLS，使用已确认的同机回环连接并仅将私有 DEV 配置显式设为 `tls: "false"`；正式模板仍开启证书校验。Server 使用当前内嵌 Web 二进制在独立工作目录启动，没有外置前端目录。

- Pass：不执行 `db init`/`upgrade` 或手工 SQL，空 DEV 库直接启动自动创建 16 张表，版本为 v2、ready 与网页为 200；重复启动不改写版本、checksum、初始化时间，已有会话/批次保留，旧批次仍返回同一收据。
- Pass：sqmc03、sqmc04、sqmc05 经实时 Tailscale IP 完成真实 HTTP 上传。同一合成会话在三台机器分别上报和重传后，总量仍为 110 Token、中心会话为 1、来源为 3。sqmc03 初次访问超时，用户调整后再次验证 ready 与上传均为 200；不把首次失败隐藏为通过。
- Pass：Chrome 通过 Tailscale 入口加载二进制内网页，首次管理员配对、刷新恢复、中心重启后恢复和退出授权闭环完成；合成会话可读。匿名业务 API 为 401；缺失 CSRF、外来 Origin、管理员写采集事实、collector 查询管理数据均被拒绝。通配 CORS 为 `*` 且不允许跨域凭据；浏览器 console error/warning 为空。
- Pass：本机 development App 使用真实 Codex Home 与新的 0700 runtime；preferences 的 canonical path/inode、App/Helper 环境、父子关系与 Helper runtime 参数分别读回。通过原生 UI 配对 Tailscale DEV、明确启用、保存 600 秒间隔并发起补传，中心真实接收 745 个跨 Provider 会话，队列排空。600 秒是配置与重启持久读回证据，本轮未等待完整的两次 10 分钟周期。
- Pass：只停止本轮 DEV Server 后，原生上报进入 offline，保留未确认批次；本机 64 个活跃 Codex Session 索引的更新时间继续推进。恢复 Server 后无需再次触发上传，自动退避重试恢复、队列归零，历史会话/收据与浏览器授权保留。
- Pass：撤销三个合成 collector 后其 sync 返回 401；撤销真实 Home 验证设备后原生进入 reconnect_required，未确认批次保留。退出 development App 时其 Helper 与 UDS 停止；同一 runtime 重启后仍是 600 秒、相同失效状态、相同待确认批次与 body SHA-256，真实 Home 身份不变。验证结束停止 development App，并撤销/退出全部本轮测试授权；保留 DEV 中心供用户检查，未安装常驻服务。
- Pass：两套端口配置加载的聚焦 config 测试；最终模板/私有副本均分别使用 18089/18090，Git 忽略及 0600 权限读回，文档链接和 diff 空白检查通过。

Not Run：三台原生 App 分别绑定真实 Home 的完整验收矩阵、独立 MySQL 8.4、真实数据库旧版本升级演练、备份恢复/回滚演练、正式 HTTPS/生产配置/常驻部署、CI、签名/公证/版本发布。本机 MySQL 客户端为 8.0 且未找到 GNU timeout，现有备份脚本的实际执行条件尚未满足；本轮不安装工具，不对活动 DEV 库做恢复或降级。

结论：DEV 的数据库、单体 Web、三机 Tailscale HTTP 与本机真实 Home 上报链路通过，可进入上线准备；这些结果不构成正式环境已经上线或 Master 全部通过。原始日志、数据库状态、HTTP 结果、队列摘要和合成网页截图保存于忽略的 `.artifacts/multi-machine/dev-tailscale-20261003/`，目录 0700、敏感文件 0600；真实地址、账密、Home 路径、账号资料与原始内容不提交。

安全 diff 自查：本轮配置不扩大公开监听，DEV 绑定具体 Tailscale IP；正式数据库 TLS/HTTPS 模板保持校验，通配 CORS 与凭据分离，Cookie/Origin/CSRF 及用途鉴权拒绝已读回，迁移和重启保留事实，凭据仅存私有配置/本地 runtime。没有新增匿名业务入口、原始内容上报或生产数据库操作。

部署包收尾：`make package-center` 实际构建通过，补入两套公开配置模板和独立启动 README，并同步 HTTPS upstream 18090。产物为本机 macOS arm64 单体二进制，读回 SHA-256 和文件清单，确认不包含任何 `*.local.*` 或外置 Web。最终 DEV 已切换到该部署包二进制，直接在包目录启动并读回 ready 200；未签名、公证或发布，构建输出及清单位于同一本轮 ignored artifacts。


## v0.15.0 投产入口补齐（2026-10-03）

macOS 常驻入口使用独立用户 LaunchAgent 与不可覆盖的发行目录，具体 root/config 为操作者绝对路径，配置 0600、运行目录 0700。正式与 DEV label 分离，版本清单包含二进制 SHA-256，更新保留旧发行目录与数据库。根 CI 实际纳入 Web 行为、内嵌构建、Server race/vet/build 和部署包；不将嵌套 starter workflow 当作已执行 CI。

Pass：MySQL 8.0.46 实际 dump 成功，修复不支持的 `--no-login-paths` 与 dump connect-timeout 参数，隔离隐式登录文件、关闭 column statistics，Python 有界执行超时返回 124。DEV 恢复到本轮新建的专用空库后，14 张业务表逐行比较一致（含收据/原确认时间），版本/摘要保持，旧客户端及未消费设备码均失效，db check 通过。隔离 macOS LaunchAgent install/stop/start/restart/ready 和 remove 流程通过；remove 保留文件/数据，不删除业务库。

本地开发补验完成后进入提交和发版收尾，按协作约定不重复执行测试；后续 CI 与正式部署结果分别读回。当前已确认采用中心 SQMC04 Tailscale 私网 HTTP、正式端口 18090，客户端在 SQMC05 由 clean main 发布 v0.15.0/build 63。私有配置迁入主仓库前先同步忽略规则；现有主仓库设计草稿备份后与已实现文档逐项对账，旧草稿不覆盖最终实现。

## 2026-10-03：TOO-523 七项优化开发验证

范围：统一操作 Notification、Prometheus 运维/同步指标、自然日柱状图、年度 Token/API 等价成本、同步阻塞修复、当前范围全量补传、SVG favicon。分支 suqing/too-523-center-web-sync-improvements；未提交、推送或部署。

只读诊断确认：某个 Codex Session 含 20,032 条 timed Token 事实和 16,691 条调用事实，超过旧单快照 20,000 上限，导出返回 source_budget_exceeded；旧调度在账号历史/设备状态导出前退出。本机有新鲜当前额度，但中心仅收到旧 linked_history；不把历史升级成当前额度。未写入真实 Home、上报队列或生产中心。

开发 Pass：

- Reporting contract / Helper reporting / Store reporting 的聚焦场景，Core/Helper 握手与 RPC 场景通过。新增完整大快照分片、重启后不可变正文、全部确认才推进 revision、全量重发保留队列/单调版本/任务进度、来源错误不阻塞最新额度与其他 Provider、来源不可用时不误报全量完成、完成状态及时上报。
- 中心 reporting/statistics/http/schema/config 受影响包通过。新增乱序与重复片段、完整摘要损坏拒绝且旧投影保持、完整调用数保留、独立指标 Token 只能读 /metrics、原生 CoreService/UDS/HTTP 的 full resend RPC/撤销/退出闭环（synthetic Home）、年度范围独立与历史金额、SQLite v1/v2 升级保留既有数据。分片接收只在收齐后读取全部正文；中途使用索引元数据和服务端字节计数，不反复加载之前所有片段。
- Web 7 个受影响文件 26 场景通过；设备同步状态展示调整后 2 文件 4 场景通过。构建通过，AntD lint 0 问题。NULL 日桶/真实零、模型堆叠、部分费用、年度独立值、通知成功/冲突后表单保留均有证据。
- Mac reporting-only 可执行测试通过；Mac App 编译通过；CoreClient 确定性测试按 make verify-swift-client 的取消探针入口通过。首次直接启动 CoreClient 测试缺少 CODEX_PULSE_CANCEL_PROBE，失败输出未当作通过证据。
- Chrome 独立 loopback 合成 SQLite 副本：右上 Notification 保存成功、内联成功提示 0；390×844 顶部通知、年度汇总无页面横向溢出（documentWidth 375）。年度 5792.6万/$24.01 切到 7 天保持，当前范围变成 581.1万/$2.30；模型柱可见。SVG 资源匿名 GET 为 200/image/svg+xml，构建 /assets 引用正确。Chrome error/warn 列表为空，临时视口已恢复。截图仅 ignored .artifacts/too-523/；合成预览已停止。

安全自查：指标独立只读鉴权、采集资源域和同客户端事务锁、白名单字段与稳定 ID、分片/快照/队列预算、原子投影及已确认历史口径保留；diff 未加入秘密或 raw error/正文，未移除既有 Origin/CSRF/权限校验。

Not Run：真实三机升级/账号恢复/全量补传、生产 Prometheus 抓取、MySQL v3 DDL/锁/备份恢复、真实 Home App GUI、CI、长测、签名、公证与发版。先升级 Server 后升级 App；SQLite / Chrome 合成证明仅为开发证据。


## 2026-10-03：TOO-523 账号最后更新展示补充

用户确认“账号订阅”指账号页的额度与用量，要求有效数据持续指向最后一次更新。只读核对当前 Chrome 页面与仲裁/VO/UI：FreshForMS 为 10 分钟，reset 后为 expired_unknown；后端保留有效 UsedPercent/ObservedAtMS，但 Web 隐藏主额度与曲线。Credits 的 current available 同样受 freshness 门槛影响；Pace 曾混用当前时间进度与旧额度。

实现：Web 主额度、进度条和观测周期曲线持续显示，常驻原更新时间；Credits 以最后观测库存为主。Server 用 snapshot_at_ms 对应的最后观测时刻复用既有 Pace 算法，周期进度、偏差、历史同进度对比和推算统一截止；reset 静态间隔、Credits 当次可用量/最近到期均由 Server 返回。Current freshness、原接收/采集时间、真实冲突、可疑和关联历史边界保留，Native 算法与持久化结构没有新增修改。无有效观测时保持未知；异常库存没有 valid last observation 时不作为主值发布。

开发 Pass：

- Server quota/http 聚焦 Quota/Pace/Unassigned 场景，以及新增无有效快照和无有效 Credits 观测场景通过。推进时钟到 10 分钟后、reset 后、reset 后 30 天，快照时间、曲线、进度/偏差、历史对比和耗尽推算保持一致；Current freshness 未升级。真实新观测推进快照；过期与失败保留库存和原时间。
- Web Quota.test.tsx 的 10 个场景通过，包含 stale/expired 刷新保留主额度、图、推算、精确库存与更新时间，未知/冲突/账号隔离/零/失败缓存边界。类型、最终构建及 AntD lint 0 问题；Server 嵌入构建通过。
- 独立 loopback 18103、原合成 SQLite 新副本升级到 v3：读取时为 10 月 3 日，两个窗口 reset 已过，仍显示最后观测剩余 30%/80%、70% 使用、80% 周期进度、-10.00 pp、2 个历史周期、原曲线/推算；全部截止 10 月 2 日 00:45。Credits 库存 3、当次可用 2、当次最近到期保持。刷新不改变原时间，详情静态 reset 间隔为 1 小时。390×844 documentWidth=375 无页面横向溢出；浏览器 error/warn 空，视口已恢复。截图 .artifacts/too-523/quota-last-update-desktop.jpg 与 quota-last-update-mobile.jpg。

安全自查：管理员鉴权与账号资源域保留，新增值由服务端有效观测计算，冲突/未知未伪装为新观测，未加入秘密、危险 HTML 或新的外部请求。生产中心未改动；生产部署、真实三机账号恢复/补传、MySQL/CI 与真实 Home App GUI 未执行。临时预览验证后停止。

## 2026-10-03：TOO-523 价目表发布时间排序补充

用户要求全部模型按发布时间从新到旧排列。Server 增加独立的官方型号/API 发布记录，当前 66 组、118 个明确型号/别名；为公开、历史和已观测目录统一附加 released_at_ms 与 release_source_url，按发布日期倒序，同日稳定排序，日期未确认置后。Web 移除近期已使用优先排序，新增发布时间列和可展开的发布来源，保留近30天已使用与平台/搜索/价格版本/单位/历史目录筛选。价格核对日、生效边界和历史计价不被发布时间覆盖；没有数据库或上报契约变更。

开发 Pass：

- Server catalog/http 的 Catalog/Subscription 聚焦场景通过，覆盖日期倒序、同日稳定排序、未知日期置后、明确别名复用和历史费率/价格日期独立。
- Web Pricing.test.tsx 的 3 个场景通过，新增完整目录保留服务端顺序、近期使用不改变排序、发布日期/来源展开、近30天筛选。类型检查和生产构建通过，AntD lint 0 问题；最终 Server 内嵌 Web 构建通过。
- Chrome 独立 loopback 18103 合成 SQLite 预览：完整美元目录 286 条，顶部为 gpt-6.1-sol（2026-09-29）、Claude Sonnet 5.5（2026-09-28）、Claude Opus 5.5（2026-09-22）。搜索 Grok 后 44 条保持 4.7（2026-09-21）、4.6（2026-08-12）顺序；清空搜索恢复完整目录。展开发布来源为官方 changelog，发布时间 2026-09-29 与价格核对日 2026-10-02 分开展示。浏览器 error/warn 空；桌面 viewport 1027、documentWidth 1012，无页面横向溢出。截图仅保存于 ignored .artifacts/too-523/pricing-release-order-desktop.jpg。

安全自查：管理员鉴权保持，发布来源使用现有 HTTPS 域名白名单与 noopener/noreferrer；发布元数据来自受控内嵌 JSON，不新增外部请求或秘密，不改写历史消耗。开发验证完成后停止临时合成预览。生产部署、CI、长测、真实三机与原生 App 验收未执行。

## 2026-10-03：TOO-523 Storybook 追加与订阅分类样稿

用户要求所有本轮改动追加进Storybook，订阅与额度参考官方购买页、先按平台与套餐类别做样稿；另明确要求停止旧6007。当前分支新增订阅4条、优化12条，保留原22条，静态index实际读回共38条。

本轮优化使用正式PulseApp与页面/图表/通知组件，合成适配器截获全部 `/api/`，未配置路径返回501。覆盖年度汇总、自然日模型柱状图、Notification、额度reset后保留最后有效值和原时间、最新额度恢复、未知/冲突、设备同步、价目发布时间排序；后端指标和原生补传另列说明与模拟交互，不冒充真实运行。订阅样稿按Codex/Cursor/Grok、个人/团队分组，档位选择、参考价格和能力层级取自官方购买页。Grok购买页与原中心目录不同的套餐单列其他来源；正式目录与订阅页未替换。

开发 Pass：最终TypeScript检查、Storybook静态构建；新增AntD用法lint为0问题。Chrome实际检查平台/个人团队切换、Cursor团队组、Codex Pro键盘选择$500后示例在用标签消失、失败Notification及重试入口、全量确认后进入进行中/暂停且计数保留、正式概览年度汇总与价目顶部日期、reset后额度30%/已用70%与Credits库存3/观测时可用2仍显示原10月2日09:00。390×844样稿documentWidth=375，没有页面横向溢出，视口已恢复。页面console error为空；Storybook manager有一条PopoverProvider的未来版本ariaLabel提示，构建另有既有大chunk与Node弃用提示，未表述为无警告。

当前6007读回无监听；6008由本分支的Storybook运行并留给用户查看。订阅入口 `/?path=/story/subscription-plans--grok`，改动目录 `/?path=/story/too-523-updates--index`。截图 `.artifacts/too-523/subscriptions-grok-desktop.jpg`、`subscriptions-grok-mobile.jpg` 和最终静态构建 `.artifacts/too-523/storybook/` 均ignored；没有读取真实Home、数据库或上报队列。

安全自查：正式鉴权与服务配置未改；预览API不透传，操作只改合成内存，公开购买页链接为固定HTTPS且使用noopener/noreferrer，不提交订单、不签发有效凭证。没有新增依赖、生产部署、CI、长测或真实三机/原生App验收。

### 订阅样稿保留模型价格入口修正

用户指出首版订阅样稿缺少模型价格入口。修正为同一价目表的“模型价格 / 订阅与额度”两个主标签，平台/个人团队分类只在订阅内。Storybook通过PricingReview的合成适配器与路由复用正式Pricing模型页，模型搜索、完整/已使用目录、版本、历史证据、USD/Credits、发布时间排序、发布来源和用量链接保留；业务页的默认模型入口与原订阅渲染保持。新增“05 模型价格与订阅切换”，当前合计39条。

Pass：Pricing既有3场景、TypeScript检查、最终Storybook构建；Pricing和design两次独立AntD lint均0问题。Chrome同一Grok订阅故事进入模型价格，搜索grok-4.7得到2条短/长上下文参考价格；进入订阅后Grok分类仍选中，再返回模型搜索仍为grok-4.7。新模型故事默认展示模型价格，USD合成目录284条，顶部gpt-6.1-sol及发布日期列可见，两个主标签同时在页面中。截图 `.artifacts/too-523/pricing-tabs-restored.jpg`。热更新阶段Storybook输出act环境提示，未据此宣称控制台无错误；页面内容和切换/搜索均已实际读回。

安全自查：业务API权限、字段和价格口径未改；订阅替换内容仅由Storybook传入React节点，业务入口不导入设计数据，合成请求不透传。6008保留供评审，生产部署与正式订阅页替换仍未执行。

### 价目精简、API参考折算与正式前端接入

用户批准价目围绕实际工具与模型收窄，并授权先更新Storybook，再同步正式前端。先检查独立的新价目组件预览，随后在正式`Pricing`模型标签接入同一`ModelPriceCatalog`；最终新增“06 相关模型与 API 参考折算”，40条故事已从静态index读回。

默认一平台内一个型号一行，基础文本美元参考价与发布时间常驻，Fast/长上下文、缓存写入、Credits、历史证据在模型内展开。Codex默认集合依据官方模型说明于2026-10-03核对；Cursor/Grok保留现有文本型号，普通API完整目录及Batch/Flex不再铺满主表。实际使用的旧型号、非文本型号与未知参考价保留；非文本型号明确自身计价单位。历史版本深链、原始Cursor Fast/500k用量型号、公开费率与历史费用计算未被改写。

Pass：Pricing 6个聚焦测试覆盖未知值/XSS、金额与来源白名单、发布时间顺序、模式合并与Standard主价、订阅标签切换、实际Fast型号跳转、历史版本深链和非Token计价单位。最终Web/Storybook构建通过，相关AntD用法检查0问题；正式构建不包含设计样本。初轮新增测试的英文展开按钮定位及Segmented原生input不可直接点击问题已修正后通过。

Chrome合成预览：全平台主表30个模型，Codex筛选9个（7个默认参考、1个已使用旧型号、1个未定价观测），日期倒排；gpt-6.1-sol主价$2/$0.10/$10，Fast与长上下文展开可见，Batch未铺入。搜索后切到订阅，再返回仍为单个搜索型号；历史计价可读openai-api-2026-09-29合成证据。390×844的documentWidth=375，表格920px内容在317px容器内局部滚动，视口已恢复。

截图`.artifacts/too-523/pricing-focused-desktop.jpg`和`pricing-focused-mobile.jpg`、构建日志与静态产物均ignored。旧草稿组件移除时曾有Vite热更新失效，重新加载后正式组件与切换恢复；Storybook保留既有act环境与PopoverProvider提示记录，构建有既有chunk/Node提示，不宣称控制台完全无警告。

安全自查：仅前端价目展示和合成预览变更，同源API、管理员鉴权、Origin/CSRF与来源HTTPS白名单保留；普通文本默认转义，无新增外部请求、依赖或秘密。6008预览保留供用户检查，中心服务部署、正式订阅样稿替换、CI、长测及真实账号验收未执行。

### TOO-523 真实 DEV 部署（2026-10-03）

用户明确授权部署 DEV 供人工检查。本轮仅更新已有 development LaunchAgent，发行目录为 `dev-too523-20261003-164949`；生产服务、配置、数据库和发行链接保持原状。此前各段“未部署”为各自开发阶段的证据边界，本段补充真实 DEV 结果。

Pass：`make package-center` 构建成功，部署包不含私有配置。先对 DEV v2 库备份，并在新建演练库中恢复、升级到 v3；演练 ready 200、db check 通过。实际切换前停止 DEV 并再做一致备份，随后升级原 DEV 库，结构版本 3、18 张表。升级前后、新浏览器配对前，15 张既有业务表的行数与 SHA-256 一致，包含原客户端撤销标记和配对消费状态；745 个会话及历史事实保留。运行数据库是原有 SeekDB 的 MySQL 协议入口，这不构成独立 MySQL 8.4 验收。

真实 DEV ready 200，Prometheus 独立 Token 鉴权为匿名 401、授权 200。Token 仅保存在本机 0600 私有文件，运行目录 0700。生产进程、配置摘要与发行链接读回未变。Chrome 正在被用户使用后，改用 Codex 侧栏独立浏览器完成正常配对，避免同主机不同端口共用 Cookie 覆盖生产登录；真实价目页显示“相关模型 / 近30天已使用 / 历史计价”、输入/缓存输入/输出参考价、发布时间倒排，以及新 DEV 版本与结构 3。页面保留供用户检查。

临时演练 Server 已停止，演练数据库、两次备份和旧 DEV 发行目录保留。截图、部署日志、数据指纹和运行读回仅保存在 ignored `.artifacts/too-523/dev-deploy-20261003-164949/`，消费后的配对码临时文件已移除。复用此前 Pricing 6 场景等开发测试证据，本轮未重复运行单元测试或长测。

安全自查：未扩大监听或权限，既有 Origin/CSRF/用途鉴权保留，指标仅允许独立凭证只读访问；秘密未进入源码、交付包或回写内容。未部署生产、未安装新版原生 App、未完成真实三机补传验收；订阅分类购买页样稿仍在 Storybook，正式订阅布局尚未替换，等待用户检查后续范围。

### v0.15.1 开发收尾与真实 Home 定向补传

用户批准 v0.15.1 / build 64 发版、先中心后 Mac、Mac 在 SQMC05 构建发布。正式订阅页新增平台及个人/团队/地区渠道分类，套餐档位复用已有目录，未知价、币种、年付、来源和核对日期保留，未把样稿价格或在用标签写入产品。Storybook 新增“07 正式订阅分类与目录证据”。

开发 Pass：Pricing 7 场景通过，新场景覆盖档位合并、年付切换、印度币种/地区、其他来源和未知价；Web 最终类型与构建通过，两个新增/受影响组件 AntD 用法检查 0 问题。初轮 Select 虚拟无障碍 option 及键盘模拟定位未更新真实选项，改为可见选项点击后通过；实际浏览器键盘切换读回 Business 年付 $20.00、每席位/月（年付）及相应目录证据。初轮类型检查的测试 locator 参数及故事 required prop 已修正。保留 jsdom pseudo-element 与既有构建告警，不宣称全无警告。

本机 DEV App 复用此前已确认的真实 Home 与 0700 runtime，启动后分别读回 preferences 物理路径/inode、App/Helper 环境、父子关系和 Helper 数据库参数。原 DEV 授权已在此前验收撤销；正常重配后需明确保存启用，600 秒与原全部已索引历史范围保持，旧授权分区的 1 个未确认批次保留。通过原生全量补传确认入口，747 个完整快照、786 个批次确认，三个 Provider 均 ready/completed；当前分区排空，旧分区未清除。中心会话由 745 变为 748（本轮新事实），分片暂存归零；收到新的 Codex app_server/confirmed/accepted 额度，Cursor 保持 pending_association 边界。此 runtime 最大已接收贡献/调用数为 1471/3561，不能替代超 20000 大会话的真实验收；该场景已有开发分片测试，后续正式客户端升级另读回实际大快照。

本轮开发 App 已停止，其 runtime/历史/授权保留；真实生产 App 与中心未因此停止。截图和日志位于 ignored `.artifacts/releases/v0.15.1/development/`。进入提交发版收尾后复用已有证据，不再重复测试；真实三机更新和生产部署证据后续逐项补充。

安全自查：正式订阅只读取同源管理员目录，默认文本转义与来源白名单保留；真实 Home 上报仍为既有 typed 白名单，配对码只在页面内存和原生安全输入中使用，未进入日志/截图/源码。
