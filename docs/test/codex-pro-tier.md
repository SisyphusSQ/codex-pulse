# Codex Pro 5×/20× 展示验证

对应 Issue：TOO-446。

## 验证范围

本 runbook 只验证只读档位解析与展示：

- `prolite → Pro 5×`
- `pro → Pro 20×`
- 缺失、未知、冲突证据 fail closed
- 非 Pro 套餐保持原展示
- account binding generation 隔离
- Go/Swift Proto 一致

不包含真实 Home probe、手动档位、SQLite migration、写 RPC、Settings mutation 或长测。

## 聚焦命令

```bash
go test ./internal/codex/subscriptiontier ./internal/codex/appserver -count=1

go test ./internal/app \
  -run 'Test(AccountBindingLoadDisplayRequiresMatchingSandwich|ConfirmedApplicationAccountUsesBindingDisplay|ConfirmedAccountSnapshotProbesAndTransitionsToNewAccount)' \
  -count=1

go test ./api/codexpulse/core/v1 ./internal/core ./internal/helper \
  -run 'Test.*(Proto|Contract|AccountSnapshot|AccountDisplay)' \
  -count=1

bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check

make verify-swift-client
swift build --package-path app/macos --product codex-pulse-app
swift build --package-path app/macos --product codex-pulse-app-tests
```

## 结果记录

结果只使用 `PASS`、`FAIL`、`ERROR`、`NOT_RUN`、`INCOMPLETE`。

| 层级 | 验证点 | 结果 |
| --- | --- | --- |
| resolver | 5×/20×、未知、冲突、非 Pro | `PASS` |
| sandwich | plan evidence 去重、冲突、敏感 ID 清理 | `PASS` |
| runtime | confirmed binding 投影、A→B 隔离 | `PASS` |
| Core/Helper | typed snapshot 和 v3 contract | `PASS` |
| Swift App | App 与 test executable 编译 | `PASS` |
| Swift presentation | 5×/20×/未知/非 Pro 用例执行 | `NOT_RUN`，按用户要求不运行 App 长测 |
| Swift CoreClient | v3 handshake contract executable | `PASS` |
| Proto drift | Go/Swift 生成文件一致 | `PASS` |
| 真实 Home / UI 人工验收 | 本轮不执行 | `NOT_RUN` |
| `make verify` / 全仓 race | 按仓库长测规则不执行 | `NOT_RUN` |

## 隐私检查

- 错误和 DTO 不得包含原始 `accountId`。
- 不记录 token、Cookie、Authorization、原始 JSON-RPC 或真实邮箱。
- resolver 输出只包含有限 state/tier/reason，不回显未知 plan token。
- 截图和剪贴板继续复用现有账号胶囊隐藏逻辑。
