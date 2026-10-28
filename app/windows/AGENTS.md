# Windows 客户端

- WinUI 只负责展示、平台交互与 Helper 托管；所有业务事实经生成 CoreService client 获取，不直接读取 SQLite、JSONL 或 Agent 凭据。
- `../../api/codexpulse/core/v1/core.proto` 是 Go/Swift/C# 唯一 IPC 合同；生成代码由固定版本 Grpc.Tools 写入 `obj/`，禁止手写或修改生成类型。
- 依赖只使用 NuGet 官方源并固定版本，提交 `packages.lock.json`；正式构建使用 locked restore。
- Windows Helper 使用受保护 Named Pipe，不监听 TCP；一次性启动凭证只经专用继承管道传递。
- 真实验收显式传入真实 CODEX_HOME 和 ACL 私有 runtime，读回实际 Home 物理身份；测试使用 synthetic Home。
- `scripts/windows/build.ps1` 提供客户端构建入口；不要求 Swift，不自动启用 Actions 或公开发布。
