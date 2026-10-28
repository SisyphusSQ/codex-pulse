# Windows 原生客户端

Windows 使用 C# / WinUI 3 承担原生 UI 和进程管理，共享 Go Helper 承担业务事实。完整产品范围见 Linear TOO-657；本目录记录版本化实现合同。

## 工具链与构建

固定 .NET SDK 10.0.403、Windows App SDK 2.6.4、Windows SDK BuildTools 10.0.28000.2705、Google.Protobuf 3.36.2、Grpc.Core.Api 2.84.0 和 Grpc.Tools 2.85.0。NuGet 官方源及包锁随工程提交；根 Go module 的 Go 1.26.2 最低版本和现有依赖不因移植升级，本机使用已安装的 Go 1.27.1。工具与 SDK 只用于构建，产品不依赖用户安装 Go、Swift 或 Visual Studio。

```powershell
./scripts/windows/build.ps1 -LockedRestore
```

脚本只构建 Windows 客户端，不要求 Swift；Go Helper 的平台、Home 和通信移植分别由后续开发卡交付，当前不能把空白窗口构建当作完整客户端。

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
