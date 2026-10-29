# Windows 原生客户端

Windows 使用 C# / WinUI 3 承担原生 UI 和进程管理，共享 Go Helper 承担业务事实。完整产品范围见 Linear TOO-657；本目录记录版本化实现合同。

## 工具链与构建

固定 .NET SDK 10.0.403、Windows App SDK 2.6.4、Windows SDK BuildTools 10.0.28000.2705、Google.Protobuf 3.36.2、Grpc.Core.Api / Grpc.Net.Client / Grpc.StatusProto 2.84.0 和 Grpc.Tools 2.85.0。NuGet 官方源及包锁随工程提交；根 Go module 保持 Go 1.26.2 最低版本，本机使用已安装的 Go 1.27.1。Named Pipe 固定使用 Microsoft/go-winio 0.6.4，其依赖要求 x/sys 从 0.44.0 更新到 0.49.0，并在 go.sum 同步 x/mod、x/sync、x/tools 的工具依赖图。工具与 SDK 只用于构建，产品不依赖用户安装 Go、Swift 或 Visual Studio。

```powershell
./scripts/windows/build.ps1 -LockedRestore
```

脚本构建 Windows x64 Helper 与客户端并把 Helper 复制到 App 输出目录，不要求 Swift。业务页面与平台能力按下列工作包交付，不能把构建成功当作完整产品验收。

`CodexPulse.Protocol` 的 MSBuild `Protobuf` 项直接引用唯一 `api/codexpulse/core/v1/core.proto`，固定 Grpc.Tools 每次需要时在 `obj/` 生成 C# message 和 CoreService client。生成文件不提交、不手改；optional、int64、stream 由 IDL 决定。Go 和 Swift 仍沿原有生成入口，不改变其版本和生成结果。

## 支持矩阵

工程最低 API 合同为 Windows 10 build 19041，初始交付目标为 x64。开发环境为 Windows 11 24H2 build 26100 x64；实际构建和运行证据见 [Windows 开发验收](../../../test/windows-client.md)。Windows 10、ARM64、WSL2 均未验证，不宣称已支持。

## 平台与隐私边界

- macOS 保留私有 UDS；Windows 使用受用户/登录会话 ACL 保护的 Named Pipe。Helper 不监听 TCP。
- Windows runtime 使用本机 ACL 验证所有权和私有性，不能用 chmod 0700/0600 冒充；开发与安装版 runtime 分离。
- 每次启动 token 只通过专用继承管道进入 Helper；C# 不读取 SQLite、JSONL 或已保存凭据。
- 真实验收显式绑定真实 Home 和私有 runtime，读回 canonical path、卷/文件物理身份和最终进程配置。开发测试只使用 synthetic Home。

## 私有存储与恢复

`internal/platformfs` 负责 Windows owner/DACL、reparse/hard-link 检查、LockFileEx、可用磁盘空间及持久发布；Unix 保留 mode/no-follow/flock。Windows 默认数据与 preferences 放在 LocalAppData 下，避免 roaming profile；App 显式传入开发或安装版的私有 runtime。ACL 只允许当前用户与 SYSTEM，子目录/文件继承这组权限；已有自定义目录权限不安全时拒绝，不能用 chmod 读数代替。

Preferences 初始化保留 no-clobber，CAS 和 Home 执行锁保留 OS 跨进程锁及 revision/readback。Windows 文件发布使用 MoveFileEx WRITE_THROUGH，文件内容先 Flush；目录 FlushFileBuffers 不受支持，不冒充 Unix fsync 成功。SQLite URI 规范化为 Windows file URI，业务库、reporting.db 与 credentials.db 继续由 Go 单独持有。Backup/Restore 仍复制已提交的 WAL 一致快照，不能把普通文件复制当在线备份。

迁移恢复先关闭 SQLite、验证恢复副本并持久保留旧库，再以 ReplaceFileW 及同目录私有备份替换。已替换后清理失败显式返回 committed warning 并要求重启；Windows 明确的 1177 部分失败或启动时 canonical 缺失，通过唯一、同名关联的私有替换备份恢复；不安全/多份残留停止启动，不盲目新建空库。已有 canonical 不会被残留备份覆盖。macOS 原子 exchange 路径保留。

Helper 内嵌标准库 IANA tzdata，正式用户不需要安装 Go 获取时区。

## Codex Home 与来源身份

Windows 通过 FILE_ID_INFO 保留完整卷号和 128 位文件 ID，使用不透明 `DeviceID = winfile:<16 位卷号 hex>:<32 位文件 ID hex>`，`Inode = 1` 仅满足既有正值合同。全部身份位均在 DeviceID 中，不把文件 ID 截断为 int64 或散列后假称原始身份。macOS 的 `volume:<UUID>` 与 inode 格式保持原样，现有 Preferences、Fingerprint 与 SQLite 字符串字段可容纳两种格式，不更改业务 schema 或复制数据库。

`platformfs.ReadRoot/BoundFile` 将来源访问绑定到无 reparse 的目录组件和句柄，访问结束释放；支持 Unicode、空格和长路径，拒绝 junction/reparse 穿透、相对越界与 NTFS alternate data stream。来源使用其原有 ACL，不把 Pulse 私有目录权限写到 Codex Home。元数据探测只查类型、身份与大小；`auth.json` 仅确认存在性，不读取正文。确认后才按既有白名单读取会话，保留 prefix proof、4 MiB chunk 上限、取消和读取前后身份/大小/修改及变化时刻校验。

发现、SnapshotReader、parser、projector、持久 checkpoint、generation 和费用口径共享已有逻辑；Windows 仅实现文件系统原语。目录或文件变化返回 typed changed/unsafe/partial，不能用替代 Home 或零值掩盖。Session Index 的解析、版本证明、备份及 append-only repair 同样共用现有逻辑；Windows 新文件 WRITE_THROUGH、正文 FlushFileBuffers，显式修复仍需 dry-run/确认和私有备份，普通索引不修改源文件。

## 本机通信与启动

Windows Helper 只接受 `--pipe \\.\pipe\codex-pulse-<随机名>` 和显式私有 `--runtime-directory`；数据库与 Preferences 默认落入该 runtime。macOS 保留 `--socket` 与 `grpc+unix`，Windows 握手返回 `grpc+namedpipe`，业务 IDL 不变。

Named Pipe 的 protected DACL 只允许当前登录会话 SID，拒绝远端客户端，首个服务端实例占有端点。C# 使用 `SocketsHttpHandler.ConnectCallback` 在 NamedPipeClientStream 上传输 HTTP/2，地址仅为 gRPC 内部 authority，不创建 TCP 连接；每次连接通过 GetNamedPipeServerProcessId 匹配由 App 启动且仍持有进程句柄的 Helper。鉴权拦截器覆盖 unary 与 server stream，握手检查合同与 App/Helper bundle version，双向消息上限同为 16 MiB。

`NativeHelper` 直接 CreateProcessW，使用 STARTUPINFOEX 的 HANDLE_LIST 仅继承专用鉴权 reader 与 NUL，默认 stdin/stdout/stderr 均为 NUL。启动 token 每次重新随机生成，唯一传输是该继承管道中的单行；argv 只有数值 HANDLE，环境仅显式绑定 CODEX_HOME。父 writer 保持到关闭，EOF 是失联关闭兜底；正常关闭 RPC 后等待 Helper 完成数据库排空并以 exit 0 退出，超时终止明确标为 uncertain。正式 Job Object/单实例/托盘由生命周期工作包补齐。

`HelperSession` 区分 starting/connected/recovery/disconnected/stopped/uncertain；Bootstrap 的 recovery 不被当成正常业务空数据。事件订阅等待 ready header，每次重订阅清空依赖查询，global sequence 缺口再次触发权威查询；只对断流进行有上限退避重连。mutation 不自动重放，取消/超时/断连可能已提交，调用方必须权威读回。SettingsDraft 仅复制可编辑字段和权威 revision。错误使用生成 ErrorDetail，不向 UI 展示底层诊断或凭据。

## Windows Codex App Server

程序发现优先查询当前用户注册的 OpenAI.Codex MSIX 包，用 GetPackagesByPackageFamily / GetPackagePathByFullName 得到实际安装目录，再检查 `app/resources/codex.exe`；不遍历或猜测 WindowsApps 版本。其他候选包括 Codex 本机下载目录、PATH 的原生 codex.exe、WinGet 链接、npm 安装和用户显式绝对路径。`CODEX_PULSE_CODEX_BINARY` 与 `CODEX_PULSE_NODE_BINARY` 保留既有显式配置入口，UI 的平台启动配置由设置工作包接入。

npm 的 `.cmd` / `.ps1` shim 不被执行或解析为 Shell 命令；只映射到已确认 `@openai/codex` 的固定包路径，优先使用对应 Windows native vendor binary，否则以真实 node.exe 与独立 codex.js 参数启动。无法找到可靠模块或 Node 时返回明确 unavailable 原因。版本输出有时间/字节上限；版本号仅用于展示，实际 initialize/只读 RPC 决定能力，身份缺失不会回退到另一个安装掩盖当前账号失败。

Windows confirmed Home 使用完整卷号/128 位 ID 比对，读取全程持有 Home 与父目录的无 reparse、禁止 delete-sharing 句柄，阻止启动竞态中替换路径。子进程的 CODEX_HOME 与工作目录均绑定到该 Home，环境键与 PATH 按 Windows 大小写不敏感规则处理。所有 App Server/Node 进程以挂起状态创建、加入独立 kill-on-close Job Object 后才恢复；取消、正常查询结束和失败都会回收其后代，不弹控制台。账号夹读、scope/generation fence、Pro typed evidence、真实窗口、Reset Credits 和 LKG 沿用 Go 原逻辑；Pulse 不解析 auth.json、JWT 或 access token。

## 原生工作台与展示状态

WinUI NavigationView 复用 Mac 的信息架构：独立汇总、effective enabled 的客户端选择、客户端分析页，以及 API 与订阅/本机状态/来源与任务/设置。全部客户端关闭时仍可进入汇总和系统设置；账号额度仅属于 Codex。主窗口与托盘 Provider 分开保存。业务页面由各工作包向 PageCatalog 注册，壳层本身显示真实 Bootstrap/Settings 的服务状态，不填充演示用量。

采用 WinUI 原生控件、系统字体与 CJK 回退、ThemeResource 语义色和有限的 8/12/20/24 间距；导航宽 224，主体以 24 的内边距组织。标题、正文和辅助信息使用平台文字层级，浅/深/系统主题集中作用于根元素。窄窗口折叠导航，标题为菜单按钮保留空间，日期工具栏允许横向滚动，正文和导航分别纵向滚动；启动尺寸恢复为 DIP 并限制在当前工作区。语言使用 Go Settings 的权威 locale，日期控件与连接状态一同更新；统计请求使用明确 IANA 时区与半开日期输入，金额只是整数微美元的展示换算，不重新定价。

AppSupport 的 Workspace、QuerySlot 与 FeatureClient 不依赖 WinUI。缓存键包含 Provider、范围、IANA 时区、Home/账号 context 代次和 Protobuf 请求摘要，最多 64 项/32 MiB；返回独立 clone，不缓存 mutation。失效通知定向清理并阻止旧 in-flight 结果回填缓存；页面请求有取消与完成代次栅栏，背景失效合并，过期页面不能写新页面状态。loading/ready/empty/partial/stale/unavailable/cancelled/recovery-blocked 明确区分，unknown 与真实零保持独立，极小非零金额不显示成零。

业务偏好由 Go 独占。C# 只在当前用户 `HKCU\Software\CodexPulse\Windows\Profiles\<runtime 摘要>` 保存窗口主题、导航、Provider 和尺寸；不读 Go SQLite/JSONL/Preferences 或 Agent 凭据。开发 Home 由 CODEX_HOME 或显式 `--codex-home` 传入，runtime 由 CODEX_PULSE_APP_RUNTIME 或 `--runtime-directory` 传入；默认开发 runtime 位于 LocalAppData/CodexPulse/development。关闭窗口的基础实现先取消通知、排空 Helper 后结束，正式托盘/单实例/异常进程策略由生命周期工作包接入。

`scripts/windows/probe-binding` 只读回指定 App/Helper 的 CODEX_HOME、Helper runtime 参数、私有目录 ACL 和 Preferences 物理身份，stdout 仅输出匹配布尔值、PID 与身份摘要；不打印完整环境、路径或凭据。真实 UI 验收结合该读回，不能只凭启动命令判断已使用真实 Home。

## 汇总与概览

Windows 汇总直接消费 DashboardSummary 的 totals、coverage、provider slices、按客户端趋势、带 Provider 命名空间的模型 Top N 与各自额度，不在 C# 重算跨客户端业务合计。API 等价估算与 reported amount 始终独立。模型 Top N 的已知余量仅作“其他已知模型”的展示分组；未知构成不参与环图或堆叠图。

概览按额度、独立年度活动、当前范围用量/成本、模型趋势与构成、活动分布、会话/项目摘要和适用的调用画像组织。Cursor 另按 Cursor Models / Other Models / 未归类用量池回显官方金额和 Token fee，不与 API 估算混算；Cursor/Grok 不查询调用画像，DSH 不制造官方额度。会话排行使用 Helper 的最近活动筛选和生命周期累计值，明确区别于当前精确范围的项目贡献。下钻携带仅在内存中保存的 opaque identity 与精确 UTC 范围，完整详情由会话/项目工作包接入。

日期使用本地日半开区间和明确 IANA 时区。各页面独立保留范围；Codex 概览读取 Go 的 overview_range 默认值，周/月额度范围按真实 window_minutes/reset/evaluated_at 解析；无法取得时明确标注回退最近 7/30 天。年度查询固定最近 365 个本地自然日，其缓存与当前范围分离。年度、额度、用量与排行各有 QuerySlot；局部错误保留本区块 last-success，额度失效不重载年度日桶。

图表使用原生 WinUI 向量与控件，浮点仅用于像素坐标。Token/微美元维持整数，堆叠图、构成条、环图和零填充先用整数对账；模型日桶无法对账时只显示权威总量。年度完整覆盖可补零，partial 的缺失日保持未知，完全未知年度不生成零统计；已知本机子集与 totals 对齐时显示并标注本地口径。活动时间线和星期小时两组 Token 都与 totals 一致后才把缺格视为零，会话数量不跨桶相加。图表有精确值 Tooltip、明细 Expander 与文本辅助功能名称；日历和表格局部横向滚动，主体纵向滚动，窄窗口 KPI 换行、排行纵向排列。

## 会话与项目分析

两页使用原生虚拟列表、独立详情滚动区和可用鼠标/左右箭头调整的分隔条；窄窗口只显示列表或当前详情，“返回列表”保留位置和选择，再次点同一行也能打开。同一身份的后台更新只更新展示属性；Provider 与 Home/账号上下文隔离，筛选/范围变化清空选择和 cursor，已失效响应不能覆盖新实体。

Session 以最近活动时间筛选，列表和详情显示 Helper 的生命周期 Token/成本、Codex 缓存命中率及活跃输出 milli-TPS。缓存比率与 TPS 只换算单位，不在 C# 重新计算；覆盖、排除/未结束轮次、活跃时长和来源单独解释。完整会话趋势、最近轮次 TPS 与独立轮次 usage/cost 页不被加总成另一份生命周期总量。

Project 列表分别展示服务端 global/matched/page totals，详情仅展示所选区间贡献与已有 daily、模型/Session contribution；已归类和“其他”都使用列表返回的 dimension_key，不从显示名或 project_id 猜测。模型和 Session 贡献各有独立 cursor；索引失效时回到对应代次首页，typed cursor validation 只重置出错的分页。新索引或短暂失败不被伪装为空项目。

查询仅使用现有 Core 的精确项目/模型、活动、置信度、时间和排序；选项明确来自当前页，实际过滤/排序由服务端完成，不提供合同中不存在的标题全文搜索。列表默认每页 20 条，可选 10/50/100；详情默认 20。cursor 仅原样回传且只在当前 App 进程内保存。会话与项目互相下钻保留同一 Provider/精确范围；普通日期范围转为精确 UTC 时仍保留完整半开区间，ProjectDetail 的本地日期与 exact_range 互斥。

## 额度与订阅资料

Windows Cursor、Grok、DSH 的解析和业务真相仍由 Go 提供。Grok 与 DSH 的本地访问共享只读 SourceRoot，Windows 以句柄固定来源边界并拒绝 reparse/ADS；Grok 增量只推进完整行，读取失败保留上次一致的本地 generation 并返回 partial，在线 billing 独立。Grok 可关闭的 OIDC 续期沿用现有协议，跨进程锁期间重新比较当前凭据，发现 CLI 合法更新时采用其新值，避免过时网络响应覆盖。DSH 继续使用版本化 JSONL/Zstd、seed/retry/cache/TPS 和历史价格证据，不新增官方账号或额度。三者本轮真实验收由用户豁免，隔离开发证据见 Windows runbook。

独立额度页按 Go 返回的 limit/window_minutes/reset 展示当前窗口、来源可信状态、证据与重置库存。Codex 账号、额度和节奏的 binding scope/generation 必须一致；未确认时隐藏在线事实，Home 用量继续独立读取。节奏轨迹、上一周期、历史带和预测直接消费 Core，不在 C# 推算业务额度或会员剩余天数。定价参考独立于用量范围，按当前 Provider 的目录展示，Codex 隐藏旧模型家族与别名的规则与 Mac 一致。

额度页和托盘分别加载当前 Provider 的账号快照。Cursor/Grok 胶囊只展示匹配 account.type 的邮箱和各自套餐，不消费 Codex 手工订阅、Pro tier 或日期；读取失败隐藏旧身份。DSH 不提供官方账号或额度。托盘隐私复制对三家账号统一隐藏邮箱、套餐和日期。

账号额度列表只调用 `ListCodexAccountQuotas` 的原子快照，当前与历史共用动态窗口卡片，历史无在线刷新。订阅资料分区使用独立 Home 级内存协调器：自动资料只读，草稿仅保存手工字段；列表刷新或账号通知后的页面重建不覆盖草稿，真实 Home generation 变化后丢弃。新增/修改/删除、同邮箱显式关联与解除、legacy 历史关联均使用服务端 revision，receipt 后强制 List 读回。conflict 保留草稿并显式 rebase；未知回执不重发，只有最新列表匹配时才单独标记读回确认。保留策略与清理历史是两个独立动作，均有产品确认；清理后再次读取账号额度，不删 Home 用量。

## 设置、来源与恢复

通用设置只提交生成的 `UpdateSettingsRequest` 与权威 revision。独立协调器保留未保存字段，重读不会覆盖输入；显式 rebase 采用三方合并，只保留用户改动，未改字段使用最新快照。写入后的有界权威读回独立于页面请求取消，避免 settings invalidation 重建页面时把已提交值误报为未确认。现有 Core 对 stale CAS 的 `Unavailable` 通过读回 revision 变化识别为冲突，不改变共享错误合同，也不自动重发。

Home 切换与普通设置分开，先 `PlanHomeSwitch` 探测与预览，再 `ConfirmHomeSwitch`；路径、策略或 settings revision 变化使旧计划不可确认。确认/恢复后读取 Settings 的 generation、switch status 与 last outcome；不知道结果时保留恢复语义，只有显式恢复操作才调用 `RecoverHomeSwitch`。不在 C# 读取 Preferences、SQLite 或 Codex 源文件。Windows Codex/Node 显式程序路径是独立平台偏好，保存在当前用户 runtime profile 的 HKCU 项，应用时排空并重新连接 Helper；留空恢复自动发现，不解析或执行 npm shim。

来源与任务支持服务端筛选、排序、keyset 分页和结构化详情，opaque key 不显示、不持久化。只读 Data Health 位于本机状态之下，消费 `HealthProjection` 与独立时间的 `DataHealth`；资源、队列、覆盖、任务、事件、有限错误及恢复动作直接来自 Helper，缺失保持未知。健康事件另支持 `ListHealth/Health` 分页和关联来源/任务下钻。前台健康页每五秒重读，失去前台后停止该页轮询。资源图不补造缺失样本，也不把 Helper 的进程指标归因给外部 Codex 会话。

受控动作只接受固定 pause/resume/reconcile 与 recovery registry。reconcile 先展示影响并确认，repair 仅开放现有 Analyze-only dry-run；没有任意命令、SQL、日志导出或原始文件删除。迁移失败保持 recovery-only 图，只消费 `MigrationRecovery*`：准备备份后的确认 token 只在内存，取消和结果未知均读回，恢复成功必须结束旧 Helper 并启动新进程，不能热装配正常业务图。

## 托盘与生命周期（WIN-E14）

App 通过 Windows App SDK AppInstance 按 runtime 注册单实例，第二次启动重定向激活并退出，不创建第二个 Helper。普通启动遵循 Go 的 launch_behavior；--show 显式打开主窗口，--background 为登录启动保留托盘。关闭主窗口保留采集，应用菜单和托盘菜单的退出才执行 Shutdown。

NativeHelper 以 HANDLE_LIST 限制继承，使用 PROC_THREAD_ATTRIBUTE_JOB_LIST 在 CreateProcess 时原子绑定不可继承、KILL_ON_JOB_CLOSE 的根 Job，再恢复初始线程。正常退出仍先执行已鉴权 Shutdown、父 pipe EOF 和 Helper exit 等待，随后有界等候 Job 成员归零；必要的强制回收或不确定等待保留 uncertain，不伪装为 clean。退出码和固定原因保存在 runtime 对应的 HKCU 平台偏好中，供安装更新读回；更新仍须独立确认进程退出。异常 Helper/断流最多在十分钟内自动恢复三次，间隔 1/2/4 秒；每次新握手，不因握手成功重置预算，不自动重放 mutation。

托盘由 Shell_NotifyIcon v4 和独立 WinUI tool window 组成，处理 TaskbarCreated、Escape/失焦、右键固定菜单及工作区/DPI 定位。主窗口和托盘 Provider 分别保存；面板只查询呈现的额度、节奏、同周期 Home 用量及可选项目，隐藏即取消页面查询。账号/Home 失效清空旧数据；Codex account/Quota/Pace 必须同 scope/generation。复制入口重新渲染隐藏邮箱/套餐/备注/日期的视图，只向剪贴板写入脱敏 PNG 和固定安全文本；异步刷新期间同样维持脱敏，失败不回退原图或原文。

WM_POWERBROADCAST 映射现有三种 Lifecycle 事件。客户端只积累最新电源转换和一条前台通知，唤醒重新握手、Bootstrap、失效订阅和权威读取；连接变更串行化，不重放休眠期间的页面请求。Go 不增加平台业务口径或网络端点。登录启动使用当前用户、当前 runtime 独立的 Run 项，读取系统实际命令及可识别的可选 StartupApproved 状态；未知审批保持 unknown，应用不写审批元数据。测试使用替身存储，不改真实登录项。

平台依据：[AppInstance](https://learn.microsoft.com/en-us/windows/apps/windows-app-sdk/applifecycle/applifecycle-instancing)、[通知区域](https://learn.microsoft.com/en-us/windows/win32/shell/notification-area)、[创建时分配 Job](https://devblogs.microsoft.com/oldnewthing/20230209-00/?p=107812)。StartupApproved 为可选 OS 元数据，不视为公开稳定合同；实际登录仍由 Windows 管理。

## 中心配对与同步（WIN-E15）

设置中的独立同步分区使用现有 PairReporting、ConfigureReporting、ReportingStatus、SyncReportingNow、FullSyncReporting，不增加本机网络入口或协议字段。中心地址与显式私网 HTTP 许可只随配对提交；配对后默认保持关闭，启用、15～3600 秒间隔和历史起点另行保存。代码输入使用不可揭示的 PasswordBox，确认前清空；一次性码仅存于短期方法请求，finally 清空，不进入草稿、查询缓存、平台偏好或日志。

同步协调器以 runtime 为生命周期，页面重建或 Home 变更不把设备身份与同步草稿混入订阅状态。配对和配置各自保留独立的未保存字段；清队列强制关闭并使用权威历史起点/间隔，未保存的间隔继续留在草稿。所有命令仅发送一次，独立有界 ReportingStatus 回读不受页面取消影响；未知回执不自动重放，不以新成功时间推断某次补传已执行。

历史起点开始导出后锁定。普通关闭保留不可变批次，清队列需产品确认且只丢弃当前中心的本机待确认批次，不删除中心历史或其他中心分区。全量请求受理、后台 processing、publication_failed、协议拒绝、来源不可用与最终 completed 分别呈现；pending=0 或一次成功确认不能代替整个全量任务完成。仅前台展开分区轮询，进度更新保持同值草稿与原生控件，避免日期漂移和滚动跳动。

打开 Web 只使用已保存且无 userinfo/query/fragment 的根地址；HTTPS 与显式允许的 HTTP 仍由 Go 的既有地址/出口策略校验，客户端不降级、不持有采集凭证。Windows reporting.db 延用平台私有 ACL 和 Go 的分片、最终 receipt、账号 fence 与断点续传。Server module 的 x/sys 约束与共享 module 同步到 0.49.0，使锁定依赖下的中心构建可用；不改变中心业务 schema、计价或上传白名单。

## 安装与更新（WIN-E16）

当前用户自包含 bundle 配对 App、PRI、Helper 与独立 PackageHost；用户不依赖 Go、Visual Studio 或共享 .NET 安装。固定 HTTPS feed 和内置 ECDSA P-256 信任根独立于 macOS Sparkle，自动检查不下载，安装要求用户确认和完整包核验。程序目录、runtime 与 Home 分离，稳定启动器读取经核验的 current 指针；签名清单外文件、reparse/hard-link、占用、uncertain/活 PID 和陈旧退出回执均拒绝。

安装发布使用 current/previous/journal 与跨进程互斥，先排空 App/Helper 后切换，再等待 exact nonce/PID/version 的 connected/recovery 回执。失败结果可读回；切换前失败可安全重开旧版，二进制回退仅在实际 SQLite/偏好/凭据 DDL 兼容声明一致时允许，数据库不自动降级。卸载仅移除本安装程序和自有平台入口，保留 Home、runtime 与凭据。完整操作、信任配置和开发/真实证据见 [Windows 安装更新 runbook](../../../test/windows-install-update.md)；正式 Authenticode、公信任和公开更新源本轮未执行。
### Windows metadata 与空 Home 边界

App Server 的扩展 DOS rollout 路径与普通 Home 路径先统一表示，再验证真实路径与根边界；不接受 UNC/设备命名空间或 ADS 作为本机 metadata 路径。轻量分析以已发布的 Home metadata snapshot 选择读取模式，即便会话数为零也不回退旧深索引，避免 Home 切换后读到旧用量、会话或项目。保留的深索引只在没有已发布轻量快照的既有模式中使用。