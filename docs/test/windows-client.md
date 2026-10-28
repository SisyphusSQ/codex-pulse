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
