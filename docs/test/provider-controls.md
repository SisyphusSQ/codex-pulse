# Provider 启停与自动发现验收 Runbook

本 runbook 覆盖 TOO-445：Codex、Cursor、Grok 独立启停、metadata-only discovery、关闭 fence 与全关闭空态。提交版证据只保留脱敏摘要，不得包含真实 Home 路径、邮箱、原始 ID、Session/JSONL、token、Cookie、Authorization 或完整日志。

## 状态与合同

- Preferences schema：v3。v1/v2 迁移后三家 intent 为 `auto`，Cursor online 为 `true`。
- 三层状态：`intent`（auto/enabled/disabled）、`discovery`（unchecked/available/missing/inaccessible/invalid）、`effective`（enabled/disabled/unavailable/disabling）。
- 握手：`core-rpc-v5`；控制面：`provider-control-v1`。application SQLite schema 保持 v33。
- Helper 是启用状态和业务 gate 的唯一真相。Swift 只消费 Settings catalog。

## 聚焦自动化（synthetic / empty Home）

在仓库根目录执行：

```bash
go test ./internal/preferences ./internal/providercontrol -count=1
go test ./internal/providerrefresh ./internal/query/agentrouter ./internal/query/dashboardsummary -count=1
go test ./internal/cursorprovider ./internal/grokprovider -count=1
go test ./internal/app -run 'Test.*(Provider|Settings|DefaultCodexHome|Lifecycle|QuotaRuntime|ApplicationControls)' -count=1
go test ./api/codexpulse/core/v1 ./internal/core ./internal/helper \
  -run 'Test.*(Provider|Settings|Contract|Proto|DashboardSummary|QuotaRefresh|AccountSnapshot)' -count=1
bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check
swift run --package-path app/macos codex-pulse-core-client-tests
swift run --package-path app/macos codex-pulse-app-tests
swift build --package-path app/macos --product codex-pulse-app
```

聚焦测试全部通过后可运行 `make check`。该结果是本地产品检查，不等于 CI。

必须覆盖的并发与选择场景：

| 场景 | 预期 |
| --- | --- |
| collector 已进入但未 commit，随后 disable | 旧 generation commit lease 失败，无新 snapshot/invalidation |
| Settings CAS conflict | runtime intent 不变，Swift 保留 draft 并 readback |
| auto 源消失后又出现 | effective unavailable → 增量恢复，intent 仍 auto，历史不清空 |
| explicit disabled 源重新出现 | 仍 disabled，只更新 discovery |
| 三家全关闭 | 无 Provider Overview 请求；Summary known-empty；Settings 可达 |
| 关闭当前主窗口/Popover Provider | 两套选择独立 fallback，旧任务响应丢弃 |

这些命令使用 synthetic / empty Home，只能作为测试证据，不能冒充真实 Home 产品验收。

## 真实 Home 人工验收（独立 gate）

未执行前标记 `NOT_RUN`。运行前必须说明：会只读 Session/JSONL 和 Cursor/Grok 本地来源，并可能写入私有 runtime、SQLite、preferences、日志和标准 App Server housekeeping。使用 `make verify-live` 或等价显式真实 Home 启动，确认实际 `CODEX_HOME`。

检查项：

1. 无 Codex Home 或 Codex 关闭时，Cursor、Grok、Settings 和汇总仍可打开。
2. Settings 按客户端分组：主开关、发现状态、Codex/Cursor/Grok 子开关；关闭主开关后子开关置灰且值不变。
3. 显式关闭后重启、唤醒、前台恢复不会自动重开。
4. 主窗口与 Popover picker 只列出 enabled 客户端；全部关闭显示“尚未启用客户端”，可打开设置；菜单栏为 `Codex Pulse --`。
5. 汇总只统计 enabled 客户端；disabled 历史不进入当前 totals。
6. 界面、日志和 RPC 不出现真实路径、凭据或底层错误正文。

## 证据分层

| 层 | 含义 |
| --- | --- |
| 聚焦 Go / Proto / Swift / `make check` | 本地实现与合同 |
| CI | 仅在实际 CI 运行后填写 |
| 真实 Home / 人工 UI | 仅在 `verify-live` 或等价验收后填写 |
| 签名、公证、发布、Linear | 仅在对应任务明确授权并执行后填写 |

未执行的层级写 `NOT_RUN`，不得用其他层级替代。
