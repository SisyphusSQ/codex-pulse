# Codex Pulse

Codex Pulse 是一个 local-first 的 Codex 使用量、额度、Session、项目归因与数据健康工具。Go Helper 负责数据、索引、调度和业务口径，原生 Swift macOS App 负责窗口、菜单栏与 UI。

## 当前边界

- 唯一跨进程 contract：`api/codexpulse/core/v1/core.proto`
- Transport：`grpc-go` over Unix Domain Socket，不监听 TCP
- UDS 父目录/文件权限：`0700` / `0600`
- 鉴权：父进程通过继承 pipe 写入一次性 Bearer token；Helper 只保留 SHA-256 摘要
- 生命周期：shutdown RPC、`SIGINT`、`SIGTERM` 或父 pipe EOF 均进入同一幂等 drain
- 数据：SQLite 与 Go query 层是业务事实和聚合口径的唯一真相
- 客户端：SwiftUI/AppKit 只通过 generated CoreService client 访问 Helper，不直读 SQLite 或 JSONL

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
| `app/macos/` | 原生 Swift App、Core client、Helper supervisor 与 executable tests |
| `docs/` | 设计、runbook 与提交版执行真相 |

## 开发与验证

要求 macOS 15+ / Apple Silicon、Go 1.25 和 `protoc 34.1`。Proto gate 会在临时目录安装精确版本的 Go generators：

- `protoc-gen-go v1.36.11`
- `protoc-gen-go-grpc v1.6.2`

日常开发优先运行受影响的 Go package 或 Swift executable test。仓库提供以下统一入口：

```bash
# 快速分项
make test-go
make test-swift

# 提交前产品检查：架构、Proto、Go、Swift
make check

# PR / CI 完整产品验证
make verify

# 显式真实 Home development App 验收，不属于默认验证
make verify-live

# contract 修改后显式重生成
make generate-proto
```

`make verify-helper` 生成 ignored 文件 `bin/codex-pulse`。`make verify` 使用 synthetic / empty Home；真实 Home 验收必须通过 `make verify-live` 显式执行。

## 隐私原则

Helper 只保存产品功能所需的结构化 metadata 和统计结果。原始 JSONL 继续由 Codex 管理；应用不复制对话正文，不持久化 access token、refresh token、Authorization header、Cookie、RPC token 或其他凭证。公开错误、日志和 invalidation stream 只包含有限分类，不包含路径、原始 payload 或底层错误文本。

## License

[MIT](LICENSE)
