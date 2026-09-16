# Codex 账号订阅列表验证

对应 Issue：TOO-447。对应计划：`docs/superpowers/plans/2026-09-15-too-447-subscription-accounts.md`。

本 runbook 分开记录 synthetic、isolated App smoke、真实 Home A→B→A、人工 UI 和隐私 readback。结果只允许 `PASS` / `FAIL` / `ERROR` / `NOT_RUN` / `INCOMPLETE`。真实邮箱、备注、日期、SQLite dump、截图和 App Server 响应只能留在忽略的 `.artifacts/`，不得提交。

## 聚焦命令

```bash
go test ./internal/codex/subscriptionaccounts -count=1

go test ./internal/store \
  -run 'Test.*CodexSubscription' \
  -count=1

go test ./internal/app \
  -run 'Test.*(CodexSubscription|AccountBinding|AccountSnapshot)' \
  -count=1

go test ./api/codexpulse/core/v1 ./internal/core ./internal/helper \
  -run 'Test.*(CodexSubscription|AccountSnapshot|Contract|Proto)' \
  -count=1

bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check

make verify-swift-client
swift run --package-path app/macos codex-pulse-app-tests
```

以上 targeted 通过后可运行一次 `make check`。不把 `make verify`、全仓 race 或 `make verify-live` 当作本 runbook 默认入口。

## Synthetic 结果

| 层级 | 验证点 | 结果 | 证据 |
| --- | --- | --- | --- |
| Domain | civil day delta、每月续费滚动与月末收敛、闰日、DST、automatic/manual 优先级 | `PASS` | `go test ./internal/codex/subscriptionaccounts -count=1` |
| Store | v32→v33、checksum、duplicate email 不合并、CAS conflict、非当前 composite 删除、current 删除拒绝、late A fence | `PASS` | `go test ./internal/store -run 'Test.*CodexSubscription' -count=1` |
| Runtime | A→B→A 两个公开 ID、List/mutations 不启动 App Server | `PASS` | `go test ./internal/app -run 'Test.*(CodexSubscription\|AccountBinding\|AccountSnapshot)' -count=1`；List/mutations 走 `codexSubscriptionRuntime` 只读 SQLite |
| Core/Helper | v4 handshake、DTO 无 private scope、UDS readback | `PASS` | proto `--check` 与 `./internal/core` `./internal/helper` 过滤测试 |
| Swift | mutation + authoritative readback、stale list 丢弃、时区重读、额度页当前账号 binding 对齐与 provider 切换清空 | `PASS` | `codex-pulse-app-tests` |
| Settings | add/edit/clear/link/unlink/delete 源结构、每月续费日/会员到期日分流、自动值带入且不复制、不改 `settingsDraft` | `PASS` | App tests 源结构 + mutation 用例 |
| Popover | current/needs-update/非当前拒绝、隐私 canary | `PASS` | App tests presentation + clipboard |
| Isolated App smoke | `make verify` / 隔离 Home | `NOT_RUN` | 本期未授权 |
| `make check` | 产品短门禁 | `PASS` | 收尾执行一次，architecture/proto/Go/Swift 全过 |

实施完成后由执行者把上表 `NOT_RUN` 改成真实结果；未跑项保持 `NOT_RUN`。

## 真实 Home

真实产品验收只能在用户另行要求后执行 `make verify-live` 或等价入口。执行前必须再次说明：会读取真实 Session/JSONL，并可能写 mode `0700` 私有 runtime、SQLite、preferences 与 App Server housekeeping。

| 场景 | 结果 | 备注 |
| --- | --- | --- |
| Settings 手动添加从未登录账号并重启 | `NOT_RUN` | 需真实 Home App |
| 用户登录 A：detected/current/profile | `NOT_RUN` | 用户亲自登录 |
| 用户切 B：B 新增、A 保留、B 不沿用 A | `NOT_RUN` | 用户亲自切换 |
| 用户切回 A：同一公开 detected ID、手动资料恢复 | `NOT_RUN` | |
| 同邮箱 candidate，显式 link/unlink | `NOT_RUN` | |
| 删除非当前账号，linked 手工补充一并删除 | `NOT_RUN` | 用户亲自确认破坏性操作；再次登录后可重新识别 |
| 跨本地午夜 day delta | `NOT_RUN` | 不得用 synthetic 代替 |
| Popover 截图与剪贴板隐藏订阅信息 | `NOT_RUN` | 人工 UI |

没有第二个真实账号、无法等待午夜或未执行人工步骤时，对应项保持 `NOT_RUN`。synthetic 结果不得替代 live 结论。
