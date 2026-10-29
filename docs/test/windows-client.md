# Windows 客户端开发与验收

开发机器为 Windows 11 24H2 build 26100 x64。开发与真实产品验收分别记录；后者必须显式绑定真实 Codex Home 和私有 runtime，不能由构建或 synthetic 数据替代。

## 工程基线

```powershell
./scripts/windows/build.ps1 -LockedRestore
./scripts/windows/generate-proto.ps1 -Check
dotnet run --project app/windows/CodexPulse.Tests
```

输出为各项目忽略的 `bin/obj`，包依赖使用官方 NuGet 源和版本锁。Protocol 从同一 core.proto 生成，重建后可比较 `obj/Debug/net10.0/api/codexpulse/core/v1/Core.cs` 和 `CoreGrpc.cs` 的 SHA-256 检查可重放性。macOS Swift 构建仍只在 macOS 验证。

此阶段尚未交付 Helper 平台移植、业务页面、安装包或真实 Codex 验收，不将空白窗口结果计入 Master 业务通过项。

2026-10-29 开发证据：Windows 11 build 26100 x64，`build.ps1 -LockedRestore` 成功（0 warnings / 0 errors）；`generate-proto.ps1 -Check` 的两个生成文件 SHA-256 一致；C# executable tests 5 项通过（optional absent、真实零、int64 最大值、invalidation stream、凭据 unary）。依赖还原未报告 NuGet 安全警告。未执行原生窗口运行、Codex 真实 Home、ARM64/Windows 10、macOS Swift 回归或 CI；Go/Swift 生成文件与 core.proto 未修改。提交/合并收尾复用上述证据，不重复测试。

## Windows 私有存储

2026-10-29，在 synthetic 目录/数据库执行并通过：

- `go test ./internal/platformfs ./internal/store/sqlite ./internal/diagnostics ./internal/apisubscriptions -run 'TestWindows|TestStore(WriteIsFIFO|SupportsConcurrentWAL|ViewEnforcesReadOnly|CloseDrains)|TestRefreshDiagnostics(ArePrivate|WriteFailure|RetainFault)|TestSQLiteCredentialStore' -count=1`：ACL、Everyone ACL 拒绝、hard link/junction 拒绝、LockFileEx 竞争、no-clobber、ReplaceFile/占用失败/中断恢复、WAL 备份恢复、owner lease 与取消、FIFO/事务回滚/只读/并发/关闭、有限诊断和凭据重启/代次/删除。
- `TestWindowsPinnedDirectoryCannotBeReplacedUntilReleased`：运行期目录不能移动，释放后可以移动。测试曾暴露 metadata-only handle 不能固定路径，已增加 FILE_LIST_DIRECTORY 并修复；最终通过。
- Preferences 存储独立文件集测试：`go test internal/preferences/file_store.go internal/preferences/types.go internal/preferences/clone.go internal/preferences/onboarding.go internal/preferences/validation.go internal/preferences/migration.go internal/preferences/json_shape.go internal/preferences/file_store_windows_test.go -run TestWindows -count=1`，覆盖初始化、重开、CAS 冲突、权威回读及可取消跨实例 Home lease。整包暂依赖后续 Codex Windows 来源移植，不把独立测试当整包已通过。
- `go test internal/store/migration_backup.go internal/store/migration_backup_windows_test.go -run TestWindows -count=1`：真实 Windows 可用空间查询下的不足分类和取消。
- `GOOS=darwin GOARCH=arm64 go test -c`：SQLite、Preferences 与 App runtime 测试二进制交叉编译成功；只证明编译，不是 macOS 运行回归。产物留忽略的 `.artifacts/windows/too-659/`。

尚未执行 Windows 完整 Helper/迁移 recovery UI、reporting 队列整包、真实 Codex Home/恢复和 macOS 运行测试，分别继续由 Home、通信、Provider、恢复及同步工作包/最终 Master 验收接通。无 CI 结果；提交/合并收尾不重复测试。安全自查：新权限与替换链路拒绝不安全对象，凭据和诊断边界保留，未新增网络端点或正文采集。

## Windows Codex Home 与索引

2026-10-29，Windows synthetic 来源的开发验证通过：

- `go test ./internal/platformfs ./internal/codex/homeidentity ./internal/codex/logs/source ./internal/codex/index ./internal/preferences -run 'TestWindows|TestHomeProbeReadsOnlyAllowlistedMetadata|TestOpenAbsoluteDirectoryNoFollow|TestDiscovererFindsAllowlistedJSONL|TestIndexFile(BacksUp|RepresentsMissing|AddsSeparator|DetectsConcurrent)|TestValidateAppendSize|TestSnapshotReader(ReadsExact|ReadLimited)' -count=1`。
- 覆盖完整 128 位身份与重开稳定性、中文/空格/长路径、Home 替换、初始/增量、读取中变化、取消、快照字节与预算、junction/越界/ADS 拒绝、Session Index 备份/追加/新建/并发漂移。Metadata fixture 以 ACL 明确拒绝 FILE_READ_DATA，并断言正文不可读取；HomeProbe 仍成功，证明该阶段不读会话或 auth 正文。
- `go test ./internal/indexer -run 'TestIngester(RestartsFromCommitted|SupersedesGrowing|RebuildsWhenParser|RestoresCounterEpoch|KeepsOldFacts|ReplacesNewPhysical)' -count=1`：在真实 Windows 临时文件和 SQLite 上验证半行一次提交、重启 checkpoint、building supersede、parser 重建、counter epoch 和物理替换前旧事实保留。
- Preferences 整包现在可在 Windows 编译，`TestWindowsPreferencesCASAndLease` 通过，补齐上一存储阶段的 package 编译前置；这仍不是完整 App/真实 Home 验收。
- Home identity、来源、Session Index 与 indexer 的 macOS arm64 测试二进制交叉编译成功，未在 macOS 运行；产物留 `.artifacts/windows/too-660/`。

POSIX FIFO fixture 仅在 Unix 执行；Windows 原生非普通对象边界以 junction、目录与类型校验覆盖，不将 POSIX fixture 标为 Windows 通过。尚未执行真实 Codex Home/原生程序/CLI、完整 Helper、页面或中心对账，继续在对应开发包及最终 Master 验收中执行。安全自查：平台层不读取 Agent 凭据、不增加网络调用，只有已确认白名单读取与原有显式 index repair 写入；Go/Swift/C# IDL 未改变。

## Named Pipe 与跨进程 CoreService

开发入口 `scripts/windows/test-transport.ps1` 在 `.artifacts/windows/too-661/fixture-<随机值>/` 初始化 synthetic Home、全部关闭的 Providers/在线请求，以及独立 normal/recovery 数据库。真实 Helper 与 C# 进程经生产 Named Pipe/gRPC 通道互通；专用 wire-peer 由 Helper 测试包编译，只使用假查询结果验证极端消息，不随 App 分发。它们都不能证明真实 Codex 产品验收。

2026-10-29 已执行通过：

- 实际 C#→Go Handshake、Bootstrap、全部合同查询、错误 token 的 unary/stream 拒绝、非预期服务端 PID 拒绝、合同与 bundle version 不匹配拒绝、typed ErrorDetail、deadline。
- Settings 修改 receipt/revision、权威读回、ready header 后的真实 invalidation、stream 取消、8 MiB 请求、16 MiB 请求上限、父 auth 管道 EOF 排空、Helper 新进程重连、显式 Shutdown exit 0。
- future schema 的真实 recovery Bootstrap、recovery state RPC、正常服务不可用和 recovery 关闭。
- wire-peer 跨进程 8 MiB 响应、17 MiB 响应拒绝、partial/zero/unknown 保留、有界 broker sequence gap 触发查询失效。
- `go test ./internal/helper -count=1 -timeout=60s -v` 全包通过；根包 Windows 参数测试通过。此前一次无 verbose Helper 测试未结束而被中止，随后有界全包复查通过，未将中止记录计为通过。
- Grok 为完整 Helper 编译而拆出平台文件访问；鉴权刷新/并发轮换/失败不覆盖凭据的聚焦测试通过，文件隐私断言使用 Windows ACL。两个文件 symlink 用例明确 skip：当前用户没有创建符号链接权限；未改变系统权限设置，后续 Provider 验证补充 Windows 原生边界。Cursor/Grok/DSH 真实验收仍按用户豁免。
- OSV 对固定 go-winio 0.6.4 与 x/sys 0.49.0 查询均未返回已知漏洞；这不是全仓安全保证。
- WinUI 全工程与 Helper 构建、随后 locked restore 构建均成功（0 warnings / 0 errors）。Helper、Grok 和根包的 macOS arm64 测试二进制交叉编译成功；未运行 macOS 测试。中文/空格的可执行文件路径与 runtime 下，跨进程大响应、值语义、sequence gap 和关闭测试通过。

首次创建 runtime 的 DirectoryInfo 缓存误判与 stream 正常结束后的无限循环风险已在开发中修复。尚未执行真实 Home、原生 Codex/CLI、页面、托盘/Job Object、安装更新和中心对账；无 CI checks，macOS 仅交叉编译不作为运行证据。安全自查：Named Pipe 按登录 SID 限制，逐连接验证服务端 PID，启动 token 不进入参数/环境/文件/日志，未新增 TCP、业务合同或 mutation 自动重放。

## Windows Codex 程序、Home 与账户采集

2026-10-29 开发证据：

- `go test ./internal/codex/appserver -count=1 -timeout=90s -v`：Windows 原生假执行程序验证中文/空格路径、实际子进程 Home/工作目录、account/rateLimits/read → account/read → account/rateLimits/read、Reset Credits、版本展示、未登录、账号漂移、缺身份不切换安装、能力回退、npm 固定模块和独立参数、Windows 环境键。通用 RPC/schema/隐私/取消/分页测试一并通过。原 Unix Shell 进程夹具移入仅 Unix 的测试文件，Windows 不依赖 Shell；后续 Windows 子集在清空 PATH 的 fixture 环境中再通过，防止误用真实 CLI。
- `TestWindowsCodexHomePinPreventsLaunchRace` 验证最后检查与进程启动之间目录无法替换；操作结束后可以移动，旧物理身份再启动被拒绝。
- `TestWindowsCodexCancellationReapsEntireJob` 启动真实父/后代测试进程，deadline 后确认回收并等待后代退出，不以单纯调用 Kill 当作无残留证据。
- `go test ./internal/codex/accountbinding ./internal/codex/quota -run 'TestDeriveScope|TestClient|TestCurrentQuery(Unconfirmed|AccountSwitch|StaleHistory)|TestResetCredits|TestRefreshPolicy' -count=1 -timeout=90s` 通过。
- `go test ./internal/app -run TestAccountBinding -count=1 -timeout=90s` 通过：ABA、历史时间戳、当前 scope/generation、旧请求封锁、崩溃重启/claim、备份/重开和 reporting 身份保留。初次执行发现 fixture 的 Unix 权限与未关闭 DB 问题，改为平台私有目录并注册关闭后通过；未放宽生产权限检查。
- App Server 的 macOS arm64 测试包交叉编译成功，未在 macOS 运行；本卡不改本机/中心 Proto 或账号/费用业务合同。

以上均为 synthetic Home/账号/响应开发验证。真实原生 Codex 与 CLI 版本、实际 Home/当前账号/窗口、真实刷新失败恢复和自然跨周期仍由最终 Master 集中验收。缺第二真实账号时 A→B→A 记条件不足，不以本节 synthetic ABA 替代。

安全自查：无登录/切号或凭据解析；所有程序使用绝对可执行文件与独立参数，未执行 npm shim；Windows Home 固定与 Job Object 约束、原有账号 fence 与只读 RPC 保留。

## WinUI 壳层与统一状态

2026-10-29：

- `dotnet run --project app/windows/CodexPulse.Tests` 通过。新增状态验证覆盖主窗/托盘 Provider 独立、全部关闭时系统导航、Codex 专属导航、旧请求不覆盖新上下文、同上下文 stale 与跨账号清空、recovery gate、缓存 clone/隔离/定向失效/in-flight fence，以及 mutation 未知不重放、凭据 mutation 不进入查询缓存、unknown/zero/int64/微小金额与英文呈现。
- WinUI/Helper locked build 成功，0 warnings / 0 errors。壳层不新增 UI 框架或网络依赖。
- 实际原生窗口显式绑定真实 Codex Home 和独立 ACL 私有 runtime；`probe-binding` 从 App/Helper 实际环境、Helper 参数和 Preferences 读回全部匹配，Home 物理身份摘要为 `9c883cdeaa79cca0`。目录仅当前用户与 SYSTEM 的显式 allow ACL。原始路径与偏好仍只在本机忽略目录。
- 实际查看中文浅色、中文/英文深色、系统主题选择器、原生菜单键盘 End/Enter/Space/Escape、英文设置 mutation 与 revision/locale 权威读回，以及 760×540 DIP 恢复后的窄窗口。连接状态/日期语言漏更新、窄窗菜单与标题重叠、无关索引通知重建服务页的问题已修复并查看修复画面。
- 已通过原生关闭操作结束开发 App，随后读回 App/Helper 均已退出；这不代替最终生命周期和异常退出矩阵。Computer Use 的 WinUI accessibility 快照偶有浮层残留，菜单收起以实际渲染图核实；未把残留树当成 UI 通过或失败证据。

本节只证明基础壳层、权威启动/Provider 状态及展示机制。汇总/分析/管理页面、托盘、完整恢复/故障和安装/中心闭环仍由后续卡与 Master 完成。独立 physical DPI/多屏组合、完整恢复窗口与 Helper 异常的原生矩阵尚未执行，不能据此声称全产品 UI 或 Master 已通过。

安全自查：C# 仅消费 CoreService 与窗口平台偏好，不读取业务 SQLite/JSONL/Agent 凭据；查询缓存有界且隔离，mutation 未知先读回，新增探针不输出完整环境或秘密。提交/合并阶段复用上述开发证据，不重复测试。

## 汇总与概览开发验证

2026-10-29：

- `dotnet run --project app/windows/CodexPulse.Tests --no-restore`：44 项断言通过。新增验证覆盖真实通用周额度边界、明确回退、周期溢出、25 小时 DST 日期、int64 图表对账、unknown 不补零、模型 bucket 不匹配时保留总量、年度完整/partial/全未知、昨日续接 streak、年度 totals 漂移、页面范围独立及小时横轴不误用 UTC offset。
- `go test ./internal/query/dashboardsummary ./internal/query/usagecost -count=1 -timeout=90s` 的两包分别通过。DashboardSummary 的 Codex/Cursor/Grok/DSH fixture 验证已知子集、年度局部失败、reported 与 estimated 独立、模型命名空间及 DSH 无官方额度。UsageCost 首轮暴露两个 Unix 目录权限夹具，替换为现有 platformfs 私有目录入口；DST 夹具的 rollout/Home 路径改为平台绝对路径后，该包通过。没有放宽生产权限或身份校验。
- WinUI/Helper locked build 和客户端 locked/no-restore build 成功，0 warnings / 0 errors。无新增依赖、Proto 或业务计价变化。
- 实际原生窗口使用真实 Home 和已有 ACL 私有开发 runtime；App/Helper 环境、父子关系、runtime 参数、Preferences 物理身份全部读回一致，脱敏身份摘要仍为 `9c883cdeaa79cca0`。已查看英文宽窗口、中文/英文 760×720 DIP 窄窗口，额度周期/今日范围、年度热力图、当前用量、模型环图/构成条、小时趋势、星期小时热力图、真实长会话名和项目排行。窄窗口排行纵向排列，图表 Tooltip 显示真实时间/offset/单位，未知日格不伪造零。
- 原生检查中发现并修复：范围标题截断、小时横轴错用时间戳 offset 后缀、日期切换抢走焦点、年度缓存受当前范围影响，以及未归类项目名称空白。最终仅保留安全项目名/聚合在 UI；无正文、账号凭据或原始日志进入提交版材料。

这些是本卡开发与局部真实窗口证据，未声明完整真实源事实→Helper→UI/Server 对账、最终索引完成或 Master 通过。当前 Windows 环境未执行 macOS 同视口实际窗口比较；仅对齐产品合同和 Mac 页面结构。多显示器/DPI 与最终业务矩阵留在 Master 集成验收。Cursor/Grok/DSH 本轮真实环境验收按用户明确豁免；fixture 不作为真实通过。安全自查：查询只走受鉴权 CoreService，客户端不读取业务文件/凭据、不重算账本，年度与当前覆盖分离，opaque 下钻状态只在内存。

## 会话与项目开发验证

2026-10-29：

- C# `dotnet run --project app/windows/CodexPulse.Tests --no-restore` 65 项断言通过。分析页新增覆盖 opaque cursor 原样回传/返回/重置、防重复下一页、Provider 与范围、服务端筛选/排序、精确毫秒区间 clone、ProjectDetail 范围互斥、普通日期下钻不截到 now、unknown confidence、缓存/TPS 真实零与未知、单位转换、跨 Provider 选择隔离及两类贡献 cursor 独立。typed RPC error 的 cursor field 也已回归。
- 最终 WinUI locked/no-restore 构建成功（0 warnings/errors）。本卡只改 C# 与文档，沿用 E09 已通过的 Go UsageCost/DashboardSummary 合同证据，不重复运行未修改的 Go 包；无 Proto/Go/Swift 生产合同变化。
- 原生窗口真实 Home＋ACL 私有 runtime 的进程/参数/preferences 身份读回全部一致，摘要 `9c883cdeaa79cca0`。实际查看中文 1400×960 DIP 分栏与 760×720 DIP 列表/详情布局、长标题、缓存与 TPS、键盘 Down 切会话、详情返回后重新打开同一项、真实未归类项目和关联项目/会话下钻。
- 真实列表切每页 10 条后执行下一页/上一页，第二页显示剩余结果且无虚构下一页。精确项目筛选从 13 条返回对应 1 条；Ctrl+R 重查期间保留尚未应用的筛选草稿。API 等价金额/Token 及 unknown 沿用 Helper，不把分页行重新聚合成总量。
- 实际失败与修复：关联项目曾因同时提交 local range 与 exact_range 被 Helper 拒绝，改用互斥请求构造后重新走真实路径，已归类项目详情恢复；普通列表下钻结束时间误截到 now 已修复并在原生读回中保留至下一日本地零点。还修复旧实体内容残留、筛选草稿被刷新覆盖、同项返回后无法再次打开及不可读的列表辅助功能名称，并补充空轮次说明。

未执行：已选真实代表样本没有返回超过 20 条的单个项目模型/Session contribution 页或会话轮次页，未穷举其他样本；相关 cursor 独立性和请求合同由开发断言覆盖，不能写成真实多页通过。同名不同项目以身份保持与展示规则处理，当前未取得真实重复名样本。最终源事实与中心同范围对账仍归 Master；本卡中文实际窗口不代替最终完整中英文/DPI/多屏矩阵。其余 Provider 的真实验收按用户豁免。

安全自查：仅调用已鉴权 Core RPC；原始正文、Agent 凭据、业务数据库和 JSONL 不进入 C#；使用静态原生模板的数据绑定，不拼接数据为 XAML；opaque identity/cursor 只在进程内，按上下文取消与隔离。提交/合并收尾复用本轮证据，不重复测试或宣称无 CI 的 PR 已通过 CI。

E11 合同复查发现 E09 共享额度摘要在 unknown 时仍创建 Value=0 的原生 ProgressBar。修复为只有已知、有限、0～100 的 remaining 才创建进度条；真实零继续保留，unknown 只显示“—”，避免辅助功能读取伪 0%。该修复使用独立 E09 修复分支/PR；C# 65 项既有值语义/隔离断言通过、客户端构建成功。原生 unknown 辅助功能故障矩阵留在最终验收，不把静态修正或构建当成该真实场景已执行。

## Windows 额度、账号额度与订阅管理

2026-10-29 开发证据：

- WinUI 独立额度页消费 `QuotaCurrent`、`QuotaPace`、`AccountSnapshot`、`UsageCost` 和 `PricingCatalogCurrent`。账号 scope/generation 只在内存中核对，错配隐藏在线事实；Home 用量独立。动态窗口、来源证据、可信状态、过期、重置库存、节奏轨迹/历史带、预测和明细沿用 Go DTO。未知不创建 Value=0 的进度条；刷新仅在 `fetched=true` 时说明已取得新数据。
- 账号额度页一次读取原子快照，当前与历史共用卡片；历史无在线刷新入口。Codex 设置分区支持独立手工资料、补充字段、套餐、月续费日/到期日、显式同邮箱关联、取消关联、legacy 历史关联、保留策略与清理。日期剩余天数仅使用 Go 结果。独立的 Home 级协调器在列表刷新/页面重建时保留草稿，Home 更换后清空。
- mutation 使用 revision、receipt 与强制 List 回读；conflict 保留草稿并提供显式 rebase；未知结果不自动重放。回执未识别、列表不匹配或无法读回均不报成功。错误保留安全的字段定位，未回显服务端内容。
- C# 可执行断言共 80 项通过（包含 E11 新增 15 项），覆盖 scope/generation、pending、unknown/zero、接近 0/100 的百分比、动态去重、自动资料不转换成手工覆盖、日期锚点、当前账号禁止删除、冲突、回执丢失、回读变化和字段校验。WinUI locked build 成功，0 warnings / 0 errors。
- Go `subscriptionaccounts`、Helper/Core 订阅映射与安全错误、Store 订阅 duplicate/replay/CAS/fence/link/unlink/delete 聚焦测试通过。首次 Store 测试因共享夹具只执行 Unix chmod 而失败，改为 `platformfs.Mkdir` 创建私有子目录后通过；生产权限校验未放宽。
- 真实 Home 与私有 runtime 的环境/参数/preferences/物理身份全部读回匹配，身份摘要仍为 `9c883cdeaa79cca0`。真实当前账号套餐、实际周额度、Reset Credits 库存与节奏数据已显示；手动额度刷新实际取得新数据并更新成功时间。发现 Reset Credits 使用 `current` freshness 的映射遗漏，已修正并增加断言。
- 中文 1400×960 DIP 下实际查看额度、账号卡、原生订阅编辑器和删除确认。用临时 `.invalid` 邮箱的手工资料实际完成新增、修改和删除，三次均经列表读回，最终资料已移除；Ctrl+R 重查时保留未保存草稿。这是 UI/本机资料 CRUD 证据，不是第二真实账号或在线切号证据。
- 修正后以 760×720 DIP 重新启动，实际 Home 绑定再次全部匹配；原生额度页与可访问文本已读回 Reset Credits 的“最新可信值”，未继续显示错误的 unavailable 状态。

未执行与边界：缺少第二真实 Codex 账号，真实 A→B→A、历史卡与跨账号在线隔离按条件不足记录，不能以 fixture 替代；自然跨 reset、真实服务失败恢复、显式关联/清理的全部产品场景和完整中英文/DPI 矩阵留给 Master 集中验收。Cursor/Grok/DSH 真实环境仍由用户豁免，未声称通过。完整设置和平台生命周期由后续卡承接；本卡不改变 Proto、Agent 登录或统计口径。

安全自查：只调用受鉴权的 Core RPC；账号 opaque ID 只在请求/内存中，凭据、正文和原始 JSONL 不进入 UI 数据层；删除/解除/清理有本机产品确认并受服务端 revision 与当前账号边界保护；未新增网络出口或 mutation 重放。提交与合并收尾复用以上证据，不重复测试；无 CI checks 不记 CI 通过。

## Windows 设置、来源、健康与恢复

2026-10-29 开发证据：

- 原生通用设置按 Helper 的 editable fields 展示 Provider intent/discovery/effective、在线来源、周期、界面与更新偏好；保存采用 revision 与权威回读，三方 rebase 保留用户改动并吸收未改字段的新值。Codex 订阅保持独立即时分区，Windows Codex/Node 路径作为独立平台偏好保存并通过正常排空重新连接应用。
- Home 独立两步探测确认，绑定路径/策略/设置 revision，确认与恢复后独立读取 generation/switch status/last outcome；未知结果不重放。来源、任务与健康事件消费服务端筛选、排序、分页和详情。Data Health 是本机状态下钻，健康评估与资源快照时间分开，资源缺失不补零，后台不运行该页五秒轮询。受控操作仅固定 pause/resume/reconcile 和 Analyze-only repair；没有 repair execute、SQL、命令或日志导出。
- 迁移恢复页只使用 recovery-only RPC，支持 retry、受保护备份列表、prepare/confirm/cancel、exit 与新 Helper 重启；token 仅在内存。补充同一 DTO 下的按钮状态显式刷新，避免准备后仍残留 disabled。
- C# 常规可执行断言 98 项通过，本卡新增 18 项，覆盖设置草稿/rebase/CAS/结果未知、页面刷新与 mutation 读回并发、旧 Core unavailable CAS 的权威 revision 冲突识别、Home 计划绑定、命令门禁、状态与小字节数展示。WinUI locked build 成功，0 warnings / 0 errors。
- `./scripts/windows/test-controls.ps1` 通过：独立 synthetic Home/私有 runtime、四 Provider 关闭，真实 C#→Go Named Pipe 验证 Settings receipt/readback/stale CAS、首次 Home 绑定、中文空格目录的独立库切换与回切、clear-and-rebuild 计划/确认、全关闭时 Sources/Jobs/Data Health 可读；future-schema recovery prepare/cancel/confirm、备份恢复后的 restart_required、旧进程业务门禁和新 Helper 正常连接。原始夹具只在 `.artifacts/windows/too-670/`，不作为真实 Codex 产品结论。
- Go application controls、Provider 关闭 admission/取消并排空账号读取，以及全部 migration recovery 聚焦测试通过。首轮旧 Unix chmod 夹具在 Windows 私有 ACL 校验失败，并使一个 hash 等待测试超时；改用私有子目录并为等待加界后通过。注入 rollback/companion cleanup failure 的夹具同样改为私有目录。Unix mode 负面用例在 Windows 明确 skip，由新增实际 DACL 文件/目录拒绝用例覆盖；symlink 用例因当前账号无创建权限而 skip，未修改系统权限。
- 真实 Home/私有 runtime 的 App、Helper 环境参数、preferences 与物理身份读回全部匹配，摘要 `9c883cdeaa79cca0`。中文 1400×960 DIP 原生页实际检查设置、来源详情、本机状态和 Data Health，资源与队列来自真实 Helper；来源对账经过预览确认并返回 `none / available / steady`，随后读回页面。
- 实际发现并修复：settings invalidation 重建页面曾取消 mutation 的共享读回，造成已提交值仍提示未确认；现采用独立有界权威读回，补充并发断言并重走真实保存，确认“已保存并权威读回”。额度周期测试值已恢复原 300 秒，未保存字段跨 Ctrl+R 保留。另修正本机来源 active/completed 映射、接近零的存储单位显示和操作结束文案。

未执行：真实当前样本无任务/健康事件，未把空态当成任务详情或事件恢复成功；真实来源少于单页容量，未执行真实来源多页。真实 Home UI 切换/故障恢复、原生 migration recovery 备份交互、完整中英文/DPI 和平台路径的双程序正式验收归 Master 集中矩阵；上述跨进程 synthetic 覆盖不冒充这些真实场景。安装更新和系统生命周期分别由后续工作包接入；其他 Provider 的真实验收按用户豁免。

安全自查：C# 不读取业务文件、SQLite 或 Agent 凭据；路径选择由 metadata-only Go Home 协议确认，受控动作有固定 registry 与产品确认；恢复 token 不进入缓存、日志、截图或持久配置。新增公开 ACL 仅施加在测试创建的私有临时目录内；未放宽生产权限或修改系统设置。提交/推送/合并收尾复用本轮证据，不重复测试，不声称无 checks 的 CI 已通过。
### TOO-668：订阅 mutation 独立读回修复（2026-10-29）

E13 真实设置保存复验发现页面失效刷新可取消共享查询槽，审查确认订阅协调器存在相同竞态。订阅命令现使用独立、有 deadline 的强制 List 读回；只在同一未销毁 Home 协调器发布，取消较旧页面响应。账号失效导致的 workspace generation 变化不会取消该读回。失败保留草稿和未知结果，不重复 mutation。

开发阶段 C# executable tests 共 102 项断言通过（新增并发刷新、旧响应、读回不可用、Home 销毁 4 项）。对抗自审确认权威数据不依赖页面取消令牌，Home 销毁后无晚到发布，冲突/未知回执仍不乐观成功。本修复不改变 Go/Core/上报合同，不新增敏感存储或网络入口；真实集成页面回归归 Master。提交与合并收尾不重复执行测试，无 CI checks 不记为通过。


### TOO-671：托盘、受控进程与生命周期（2026-10-29）

开发环境为 Windows 11 24H2 x64。C# executable tests 113 项断言通过，其中新增有限重启/通知合并、登录项替身、Provider/账号隔离和脱敏文本 11 项。scripts/windows/test-lifecycle.ps1 使用 synthetic Home、全部 Provider 关闭，实际 C#→Go Named Pipe 验证 sleep/wake/foreground 3 种回执、正常 drain、强杀父进程后无 Helper 孤儿及同库新 owner 恢复，共 6 项；最终进程实现上的该脚本通过。WinUI locked build 为 0 warnings/0 errors。

真实 development Home 绑定均经进程环境/Helper 参数/preferences/物理身份读回一致。观察到独立额度面板、实际额度/节奏/Home 用量；主窗口关闭后 Helper 继续，第二次启动唤回原 App，PID/Helper PID 保持。强制结束已核对归属的 Helper 后，App 自动新建 Helper、重新连接并读回相同 Home 身份。真实登录项为未配置，仅做读取。

真实退出暴露 Job 过早检查：Helper exit=0，但嵌套 Job 的异步回收尚未结算就强制终止，导致 uncertain。已修正为先有界等候成员归零，仍有成员才强制回收；复验通过产品菜单退出后读回 ExitCode=0/Reason=clean，App/Helper 均消失。未知退出仍保留 uncertain，不以强制回收冒充干净退出。

托盘 tool window 未作为独立目标暴露给当前自动化工具：已有面板画面和外部点击关闭观察，尚无托盘内复制、Escape、右键/单击切换的完整原生输入证据。复制隐私目前为代码/开发断言，未声称实际 PNG 已验收。物理睡眠/唤醒、Explorer 重建、多屏/各 DPI 及真实登录启动未执行；系统支持 S3，但未进行物理睡眠；真实登录项变更按本卡范围不作为自动测试。最终安装版本的集中验收和这些缺口归 Master，不把 RPC 通知测试冒充 OS 睡眠。

安全自查：无新增 TCP/任意命令入口；一次性 token 仍只经专用继承 pipe；Job 句柄不继承，退出和连接变更有界；剪贴板两种表示均脱敏；无 C# 业务文件读取或 Agent 凭据处理。合并收尾复用既有证据，无 CI checks 不记通过。

### TOO-672：Windows 中心同步（2026-10-29）

- C# executable tests 126 项断言通过，本卡新增 13 项：短期配对请求清空、默认关闭、独立草稿、清队列的权威范围/间隔、未知回执不重放、读回并发与旧响应栅栏、后台进度不重建日期控件、processing/publication_failed/unknown 不伪装成功、Web 地址与凭据边界。WinUI locked/no-restore build 0 warnings/0 errors。
- `go test ./internal/reporting ./api/codexpulse/reporting/v1` 通过，使用 synthetic 来源/账号/网络夹具；Windows reporting 队列 fixture 改用 platformfs 私有子目录及真实 ACL 检查，未放宽生产校验。覆盖既有不可变批次、分片/最终确认、全量、重启/重配分区、额度/账号、DSH 白名单和失败状态。它们不作为真实 Provider 验收。
- 真实原生 App 显式绑定真实 Home 与 ACL 私有 runtime，App/Helper 环境、参数、preferences、物理身份读回全部匹配，摘要 `9c883cdeaa79cca0`。实际完成独立 Windows 设备配对、关闭状态读回、启用/保存、暂停保留队列、退出重启后保留同一设备/起点/17 个批次，以及更新中心后的续传。一次性码没有进入截图、仓库或普通状态 DTO。
- 真实联调先发现开发中心版本落后并返回 protocol_rejected，客户端没有丢弃队列或剥除字段。已在 sqmc04 对独立开发数据库完成约 50.9 MB 的私有逻辑备份和 SHA-256 记录，再更新仅 development LaunchAgent 的中心/Web；原有 6 个设备均保留，配置哈希及正式中心的进程/二进制/plist 均未变。数据库实际为 SeekDB 1.2（MySQL 5.7 协议兼容标识），不能写成独立 MySQL 8.x 或生产验收。
- 同一 Windows 设备恢复后，原 17 个批次全部有效确认，pending=0/0 B；固定 Asia/Shanghai、2026-10-28 半开日范围的 Codex 汇总在重复全量导出前后保持一致（9 个会话，43,198,983 total tokens，6,154,928 微美元 API 等价金额，reported charge 仍 unknown）。这证明该范围没有因重复补传双计，尚不替代最终本机源事实/原生页面/Web 的三方精确对账。
- 原生全量确认/受理及进度已实际观察，Codex 来源为 ready、已排入快照获得确认；整个四 Provider 全量任务仍 running/source_unavailable，因为本机没有 Cursor/Grok/DSH 来源。没有把 pending=0 写成 completed，未寻找这些真实账户或数据；它们的真实环境验收由用户豁免。实际全量 completed 的开发语义由隔离测试覆盖，最终 Codex 专项验收继续在 Master 矩阵记录。
- 原生“打开中心 Web”实际在默认 Chrome 打开已保存的开发中心根地址，返回正常浏览器授权页面。完整浏览器授权、撤销/重配、真实异常恢复与最终 Windows→Server→Web 对账继续由 Master 集中执行；此处不把登录页当成 Web 业务对账通过。

对抗自审修复了进度轮询重建草稿造成的日期/视口漂移，以及清队列意外保存未提交间隔的问题。安全自查：只复用已鉴权 Core 与既有上报白名单；凭据归 Go 私有存储，一次性码不缓存，未知 mutation 不重放；没有 C# 业务文件读取、TLS 降级或正式中心变更。提交/推送/合并收尾复用以上证据，不重复测试，无 CI checks 不记为 CI 通过。

### TOO-663：Windows Cursor（2026-10-29）

已实现 Windows profile/APPDATA、自定义编辑器 user-data 根、发现与采集共用路径、只读 SQLite/WAL、transcript 访问及 Dashboard 内存凭据链路。没有访问真实 Cursor 文件、账号或服务。

- `go test ./internal/cursorprovider ./internal/cursorpaths ./internal/providercontrol -count=1 -timeout=90s` 三包通过；自审补充稳定遍历顺序与失败刷新间隔后，Cursor 整包再次通过。覆盖原有账期、模型池/unknown、真实零、分页、Grok Bot 独立周额度、网络/认证/协议失败与 query admission；新增隔离的默认路径→WAL 凭据→HTTP fixture、过期凭据不发请求、重定向不转发凭据。
- 新增 Windows 原生文件夹具通过：中文/空格/`# % &` 与超过 MAX_PATH 的路径；WAL 并发提交下当前事务不变、新事务可见、拒绝写入；句柄持有时禁止替换，释放后可移动；独占锁失败后恢复；实际 junction、SQLite companion reparse 与 ADS 拒绝。元数据发现 fixture 明确拒绝 FILE_READ_DATA，发现成功且正文打开失败，证明不是靠读取凭据探测。
- 真实临时 SQLite 的 Collector→Repository 验证锁定保留上一代 Session/事实与成功时间、保存有限失败时间/码、受刷新间隔约束，并在解锁后恢复。Store 的 Cursor 聚焦测试通过：缺失/schema/read failure 不清空 last-good，有效空快照恢复可清空旧事实，Dashboard/Grok Bot 继续独立；ApplicationControl 的禁用 admission、取消/排空账号读取聚焦测试通过。
- C# executable tests 128 项断言通过，新增 Cursor Models / Other Models / Grok Bot 窗口身份和账期、未知池与真实零的展示断言。Windows Helper/WinUI locked build 成功，0 warnings/errors；macOS arm64 Cursor/ProviderControl 测试包交叉编译成功。交叉编译只证明编译，未在 macOS 运行。

开发中首轮测试实际暴露 macOS 固定路径、Windows file URI 无法打开、元数据句柄不能阻止替换，以及旧三 Provider 夹具缺少 DSH intent；均已修复并回归。部分本地来源曾成功后失败时，保留整个跨来源 lineage generation，代价是本地其他来源等待下一次一致快照；在线 Dashboard/Grok Bot 不被一起阻塞，避免把混合代次或空替换当新事实。

安全自查：没有新增外部地址、依赖、权限放宽或 Agent 凭据持久化；发现仅元数据，Windows 数据读取拒绝 reparse/ADS，Dashboard 不跟随重定向，错误保持有限分类。真实 Cursor 验收：用户明确豁免，不记 Pass，不以 fixture 或构建代替。提交/推送/合并收尾按约定不重复测试，无 CI checks 不记 CI 通过。

### TOO-664：Windows Grok 与 DSH（2026-10-29）

两 Provider 的本地采集改用只读 SourceRoot：Windows 复用已固定的目录/文件句柄与 no-follow 边界，拒绝 reparse、ADS 和越界路径，不给 Agent 文件施加 Pulse 私有 ACL。Grok 保留 summary/updates 白名单；增量 offset 只提交完整换行，残行完成后续读，文件身份或同尺寸内容变更重建投影。目录/文件失败使用有限分类，已成功的本地 generation 保留，查询明确 partial，billing 单独更新。

- 隔离环境下 `go test ./internal/grokprovider ./internal/dshprovider ./internal/platformfs -count=1 -timeout=90s` 三包通过（3.963/2.044/0.456 秒）；`go test ./internal/store -run 'TestGrok|TestCursorLocal' -count=1 -timeout=90s` 通过（4.500 秒）。新增 Windows 长中文/空格/特殊字符路径的真实文件→SQLite→查询闭环，Grok 已报告费用 150 微美元与 120 Token 分离核对，独占锁失败保留/标记/解锁恢复，DSH plain 与 Zstd 都得到 70 total、47 input、35 cache-read Token，seed 不重复计入，1970 样本仍 unpriced。正文、原始项目路径不进入事实快照。
- Grok 认证 fixture 验证 OIDC 过程中 CLI 合法轮换凭据后采用最新凭据且不覆盖；跨进程测试实际启动两个 Windows 测试进程，持锁时拒绝重命名和第二进程获取，释放后可获取。续期仍受原有开关、超时、单次认证重试和 scope 约束，未知 JSON 字段及其他账户保留。Billing/Account HTTP 客户端不跟随重定向，原有账期、失败/LKG、用量/额度隔离 fixture 一并通过。
- Windows Helper 与 WinUI locked build 成功；macOS arm64 Grok、DSH 测试包交叉编译成功。交叉编译没有在 Mac 上执行测试，不作为运行结论。本卡不改 Proto、Schema、价格快照或依赖版本。

安全自查：原始会话保持只读；Grok 仅在既有授权开关下写认证文件，跨进程锁和原子替换保留其他合法更新；凭据不进入日志、RPC、仓库或上报。DSH 不读取凭据、不制造官方账户额度。真实 Grok/DSH 采集、账号/服务、UI 数据和中心联调：用户明确豁免，未执行，不记 Pass。提交/推送/合并收尾复用开发证据，不重复测试，无 CI checks 不记 CI 通过。
