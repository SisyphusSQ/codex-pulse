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
