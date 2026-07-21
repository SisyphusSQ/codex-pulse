# Swift gRPC/UDS Transport Spike 验证记录

## 目标与范围

本记录对应 Linear `TOO-312`。目标是在 AppKit/SwiftUI 页面开始前，验证 Swift 生成客户端与现有 Go Helper 的真实跨进程边界。

本轮覆盖：

- 同一份 `api/codexpulse/core/v1/core.proto` 生成 Swift message/client，checked-in 生成物支持漂移检查。
- Swift 使用认证 pipe 启动真实 Helper，只连接私有 Unix Domain Socket。
- `Handshake`、normal/recovery `Bootstrap`、invalidations stream、Task 取消、sleep/wake 重连、受控 Shutdown。
- 独立 grpc-go cancellation probe 读回 Swift Task 取消已抵达 Go stream context；Go broker 测试继续证明订阅者/observer 释放。
- Helper 异常退出后重新监督、重新生成 token、重新握手。
- zero/unknown/absent、partial/unavailable、结构化 `ErrorDetail`、recovery/`restart_required` 语义。
- Handshake/Bootstrap/recovery-state 只对 `unavailable` 提供有界只读重试；lifecycle、recovery retry、shutdown 等 mutation 不自动重放。
- 隔离数据路径；live E2E 不读取或写入默认 Application Support 数据。

本轮不覆盖：AppKit/SwiftUI shell、窗口/UI test、`.app` bundle、Sparkle、签名、公证、发布和真实用户 Codex Home。

## 环境读回

| 项目 | 读回结果 |
| --- | --- |
| macOS architecture | arm64 |
| Swift | Apple Swift 6.3.3 |
| protoc | 34.1 |
| grpc-swift-2 | 2.4.2 exact |
| grpc-swift-nio-transport | 2.9.0 exact |
| grpc-swift-protobuf | 2.4.1 exact |
| swift-protobuf | 1.38.1 exact |
| Xcode | 当前 `xcode-select` 指向 Command Line Tools，未配置完整 Xcode |

当前 Command Line Tools 不提供 `XCTest` 或 Swift `Testing` module。因此确定性 Swift contract tests 使用独立 test executable，由 SwiftPM 构建并以进程退出码判定。完整 Xcode/XCTest/UI gate 属于后续原生 App 阶段，不在本记录中伪装为已执行。

## 验证矩阵

| 验证面 | 命令 | 结果 |
| --- | --- | --- |
| Helper 隔离路径适配 | `go test ./internal/helper -run 'TestRunStartsWithExplicitIsolatedPaths|TestApplicationConfig' -v` | PASS；真实 `Run` 在 `/private/tmp` 私有目录创建 socket/DB 并可取消退出 |
| Swift contract / concurrency / security | `make verify-swift-client` | PASS；语义映射、stale stream generation 隔离、immediate-EOF 有界重连、启动中 stop、FD 继承收敛、0700/0600 owner/type/symlink 校验 |
| 跨语言取消 | `make verify-swift-client` | PASS；Swift 取消 gRPC stream 后，认证 grpc-go probe 在 stream context 取消时写入隔离 0600 marker；Go broker context/unsubscribe 测试随 `make verify-go` 执行 |
| Swift Proto drift | `scripts/proto/generate-swift.sh --check` | PASS；临时重新生成与 checked-in `core.pb.swift`/`core.grpc.swift` 一致 |
| 真实 transport E2E | `swift run --package-path app/macos codex-pulse-transport-spike --helper "$PWD/bin/codex-pulse"` | PASS；见下方 live 证据 |
| project mechanical gate | `make verify-project` | PASS；含 `SWIFT-001` 正向检查与依赖漂移负向 fixture |
| 完整仓库 gate | `make verify` | PASS；harness、project 正/负 gate、Go Proto、race、vet、Swift Proto/contract/live E2E 全部通过 |

## Live E2E 证据

最新一次隔离运行读回：

```text
transport spike passed: helper=dev contract=core-rpc-v1 mode=normal cancellation=passed recovery_restart=passed cold_handshake_ms=562.05 unary_p50_ms=0.91 unary_p95_ms=1.31 stream_reconnect_ms=0.91 abnormal_recovery_ms=72.23 idle_total_rss_bytes=57442304 swift_binary_bytes=54819264 helper_binary_bytes=31977282
```

指标解释：

- `cold_handshake_ms`：从启动 Helper 到首个认证 Handshake 完成；单次本机读数，不是发布基线。
- `unary_p50_ms` / `unary_p95_ms`：同一连接 20 次 Bootstrap 的本机样本。
- `stream_reconnect_ms`：controller 取消旧 stream、模拟 sleep/wake 后重新收到 headers 的耗时；隔离空白配置下关闭 lifecycle RPC 发送，只验证 transport 重连路径。
- `abnormal_recovery_ms`：`SIGKILL` Helper 后重新启动、重新生成 token、Handshake+Bootstrap 完成的耗时。
- `idle_total_rss_bytes`：采样时 Swift spike 与 Helper 的 RSS 合计。
- `swift_binary_bytes` / `helper_binary_bytes`：当前 debug executable 与 Helper 文件大小，不等于最终 `.app`/ZIP 发布体积。

真实 recovery 路径不是只测 Swift 映射：spike 先创建有效隔离数据库，关闭 Helper 后将 `user_version` 设置为 99；新 Helper 返回 recovery Bootstrap。随后只在该隔离数据库中恢复为 Helper 报告的 target version，调用 `MigrationRecoveryRetry`，读回 `restart_required`，再由 Swift 发送 `Shutdown(reason: client_restart)` 并确认旧 Helper 退出。

## 安全与事实边界

- auth token 只通过继承 pipe 传入，不进入 argv/env/输出；每次 Helper 启动重新生成。pipe 两端设置 `FD_CLOEXEC`，spawn 使用 `POSIX_SPAWN_CLOEXEC_DEFAULT` 并只显式继承 auth fd。
- runtime root 使用短 `/private/tmp/cp-*` 私有目录，避免 UDS 103-byte 路径上限；Swift 在启动/连接前用 `lstat` 校验目录与 socket 的 owner、mode、type 和 symlink 边界，Go listener 再做二次校验。
- `--database-path`、`--preferences-path` 只在显式传入时覆盖；零值继续使用生产默认。
- live 输出不包含 token、用户 Home、原始 RPC payload 或临时绝对路径。
- blank isolated preferences 不建立正常 lifecycle runtime，因此 live sleep/wake 使用 `sendLifecycle: false` 验证 stream 取消/重连；生产 controller 默认发送有限枚举 lifecycle RPC，Go adapter 的具体调度语义由现有 Go tests 覆盖。这里不声称已完成真实用户 Home 下的 lifecycle live E2E。
- 性能数据是当前机器单次证据，不是 SLA，不代表 release 构建或最终 App 体积。
- 独立 review 首轮提出 6 个 blocking findings；修复 stream/Supervisor actor 竞态、FD 继承、路径校验和 cancellation 证据后，二次复审读回 `blocking_findings: 0`、`status: approved`、`scope_guard: pass`。

## 后续 gate

进入阶段 C/E2 前必须配置完整 Xcode，再增加 XCTest、App lifecycle、UI smoke 和真实 `.app` bundle gate。未经单独授权，本轮不执行签名、公证、发布、commit、push 或 PR。
