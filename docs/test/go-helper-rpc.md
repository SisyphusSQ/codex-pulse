# Go Helper RPC 迁移验证摘要

## 验证范围

- 日期：2026-07-20
- 分支：`suqing/grpc-go-helper`
- 基线：`main@130f9dc`
- 本轮范围：Go Helper、Protobuf/gRPC contract、Unix Domain Socket、鉴权、业务 adapter、失效订阅、迁移恢复、优雅退出与旧桌面运行面清理
- 明确排除：Swift client、Xcode/`.app`、签名、公证、live E2E、push、PR、merge、release

## Contract 与安全断言

- `codexpulse.core.v1.CoreService` 固定 34 个 RPC，Updater、Window、Tray、Popover 不在 allowlist。
- `NumericValue` 用 Proto presence 区分真实零与 unknown；partial/issues 和 content-free `ErrorDetail` 保持强类型。
- Helper 只监听安全 UDS；父目录必须是当前用户拥有的 `0700` 目录，socket 为 `0600`，拒绝 symlink、普通残留文件和超长路径。
- 一次性 token 从继承 pipe 读取；interceptor 同时覆盖 unary/stream，Authenticator 只保留 SHA-256 摘要。
- invalidation 使用有界、非阻塞队列；慢订阅者合并到最新 sequence，手动取消、context 取消和 broker 关闭都会释放订阅观察者。
- shutdown RPC、signal、父 pipe EOF 汇合到同一 first-writer-wins 停止信号；gRPC admission 与应用 drain 使用独立超时窗口。

## RED -> GREEN 证据

实现按 contract、core facade、broker、UDS/auth、RPC adapter、host runtime 分层推进。初始测试分别因缺少 Proto、`EncodeResponse`、`Service`、`InvalidationBroker`、UDS/auth 和 RPC handler 而失败，再实现到通过。完整验证期间还暴露并修复了：

1. invalidation stream 建立与首个通知之间的注册竞态，改为由 response header 明确 ready。
2. quota 前台测试读取早于 durable refresh commit，改为等待新的 `LastSuccessAtMS`。
3. 手动 unsubscribe 后 context observer 可能泄漏，新增释放完成断言并修复。
4. 删除旧 single-instance wake socket，避免 Swift owner 与 Go Helper 出现双重进程所有权。
5. gRPC graceful stop 和业务 drain 不再共用同一个已消耗的 timeout。
6. scheduler pause 若与队列选择后的 task claim 竞态，曾把正常 lifecycle fence 当成 fatal；现归一为可重试 cycle，并以定向 race 测试连续重放 10 次 Home switch 收口。

## 权威命令

```bash
make verify
```

该入口依次执行：

```text
make harness-verify
make verify-project
make verify-proto
go test -race ./...
go vet ./...
make verify-helper
```

工具链：Go `1.26.2 darwin/arm64`；仓库语言基线 Go `1.25`；`protoc 34.1`；`protoc-gen-go v1.36.11`；`protoc-gen-go-grpc v1.6.2`。

## 结果

- `make verify`：通过
- harness 与 project negative fixtures：通过
- Protobuf 临时重生成 drift check：通过
- 全仓 `go test -race ./...`：通过
- 全仓 `go vet ./...`：通过
- Helper build：通过，产物为 ignored `bin/codex-pulse`

本摘要不等价于 Swift 集成或桌面发布验收；这些验证需在后续 Swift 任务完成。
