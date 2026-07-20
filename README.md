# Codex Pulse

Codex Pulse 是一个 local-first 的 Codex 使用量、额度、Session、项目归因与数据健康核心。当前仓库交付的是供后续 Swift 原生应用托管的 Go Helper；Go 负责数据、索引、调度和业务口径，原生客户端负责窗口、菜单栏、更新与 UI。

## 当前边界

- 唯一跨进程 contract：`api/codexpulse/core/v1/core.proto`
- Transport：`grpc-go` over Unix Domain Socket，不监听 TCP
- UDS 父目录/文件权限：`0700` / `0600`
- 鉴权：父进程通过继承 pipe 写入一次性 Bearer token；Helper 只保留 SHA-256 摘要
- 生命周期：shutdown RPC、`SIGINT`、`SIGTERM` 或父 pipe EOF 均进入同一幂等 drain
- 数据：SQLite 与 Go query 层是业务事实和聚合口径的唯一真相

Wails、Vue、Go AppKit tray/popover/window 与 Sparkle adapter 已不属于本仓库当前运行时。SwiftUI/AppKit client、`.app` bundle、签名、公证和 live E2E 在后续 Swift 任务中实现。

## 启动协议

```text
codex-pulse --socket <absolute-uds-path> --auth-fd <inherited-fd>
```

父进程必须：

1. 创建当前用户拥有、权限为 `0700` 的短临时目录。
2. 创建 pipe，将读端作为 `--auth-fd` 继承给 Helper。
3. 向写端写入至少 32 字节的 URL-safe token 和换行，并在客户端生命周期内持有写端。
4. 使用 `authorization: Bearer <token>` metadata 发起每个 unary/stream RPC。
5. 退出时优先调用 `Shutdown`，并关闭 pipe；Helper 会停止 RPC admission 后逆序关闭后台组件和 SQLite。

## 工程目录

| 路径 | 职责 |
| --- | --- |
| `main.go` | Helper CLI 参数、signal context 和稳定错误日志 |
| `api/codexpulse/core/v1/` | Protobuf contract truth 与生成的 Go stub |
| `internal/helper/` | UDS、pipe token、gRPC interceptor/adapter、serve 与 shutdown |
| `internal/core/` | transport-neutral 业务 facade、typed mapping、失效通知 broker |
| `internal/app/` | SQLite、quota、lifecycle、health、metrics、retention 的运行时装配 |
| `internal/query/` / `internal/store/` | 查询、聚合与持久化真相 |
| `docs/` | 设计、harness、runbook 与提交版执行真相 |

## 开发与验证

要求 macOS 15+ / Apple Silicon、Go 1.25 和 `protoc 34.1`。Proto gate 会在临时目录安装精确版本的 Go generators：

- `protoc-gen-go v1.36.11`
- `protoc-gen-go-grpc v1.6.2`

```bash
# 完整验证：harness、project contract、Proto drift、race、vet、Helper build
make verify

# 单项入口
make harness-verify
make verify-project
make verify-proto
make verify-go
make verify-helper

# contract 修改后显式重生成
make generate-proto
```

`make verify-helper` 生成 ignored 文件 `bin/codex-pulse`。本轮没有可独立运行的桌面 `.app`；需要由符合上述协议的父进程提供 UDS 目录和 token pipe。

## 隐私原则

Helper 只保存产品功能所需的结构化 metadata 和统计结果。原始 JSONL 继续由 Codex 管理；应用不复制对话正文，不持久化 access token、refresh token、Authorization header、Cookie、RPC token 或其他凭证。公开错误、日志和 invalidation stream 只包含有限分类，不包含路径、原始 payload 或底层错误文本。

## License

[MIT](LICENSE)
