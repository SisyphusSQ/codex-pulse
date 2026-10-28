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
