# Windows 原生客户端

Windows 使用 C# / WinUI 3 承担原生 UI 和进程管理，共享 Go Helper 承担业务事实。完整产品范围见 Linear TOO-657；本目录记录版本化实现合同。

## 工具链与构建

固定 .NET SDK 10.0.403、Windows App SDK 2.6.4、Windows SDK BuildTools 10.0.28000.2705、Google.Protobuf 3.36.2、Grpc.Core.Api / Grpc.Net.Client / Grpc.StatusProto 2.84.0 和 Grpc.Tools 2.85.0。NuGet 官方源及包锁随工程提交；根 Go module 保持 Go 1.26.2 最低版本，本机使用已安装的 Go 1.27.1。Named Pipe 固定使用 Microsoft/go-winio 0.6.4，其依赖要求 x/sys 从 0.44.0 更新到 0.49.0，并在 go.sum 同步 x/mod、x/sync、x/tools 的工具依赖图。工具与 SDK 只用于构建，产品不依赖用户安装 Go、Swift 或 Visual Studio。

```powershell
./scripts/windows/build.ps1 -LockedRestore
```

脚本构建 Windows x64 Helper 与客户端并把 Helper 复制到 App 输出目录，不要求 Swift。业务页面与完整生命周期由后续开发卡交付，当前不能把窗口构建当作完整客户端。

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
