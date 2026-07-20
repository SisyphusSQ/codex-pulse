# Go Helper CI and Verification

## 权威入口

```bash
make verify
```

执行顺序：

1. `make harness-verify`
2. `make verify-project`
3. `make verify-proto`
4. `make verify-go`（`go test -race ./...` + `go vet ./...`）
5. `make verify-helper`（输出 `bin/codex-pulse`）

## 工具链

- Go `1.25.0`
- `protoc 34.1`
- `protoc-gen-go v1.36.11`
- `protoc-gen-go-grpc v1.6.2`

Proto 脚本在 `mktemp` 目录安装精确 Go generator，重生成后用 `cmp` 检查两个 Go stub；只有 `make generate-proto` 会覆盖工作区生成文件。

## CI 边界

GitHub Actions 固定 `macos-15`，checkout 与 setup-go action 使用 commit SHA，repository 权限仅 `contents: read`，dependency cache 关闭。workflow 不读取 token/secret，不签名、不公证、不发布，也不执行 Swift/.app/live E2E。

CI 完成后检查 tracked、staged 和非忽略 untracked 文件，确保验证没有留下漂移。当前未验证项为 Swift client、`.app` bundle、Developer ID、notarization、Helper 崩溃重启与真实睡眠/唤醒。
