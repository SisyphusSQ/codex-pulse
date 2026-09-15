# TOO-446 Codex Pro 5×/20× 展示实施计划

## 1. 目标

在 Codex 当前账号的套餐展示位置，将 App Server 返回的明确套餐标识转换为用户可读档位：

- `prolite` → `Pro 5×`
- `pro` → `Pro 20×`

当证据缺失、未知或互相冲突时，不猜测档位，显示 `Pro · 档位未知`。Cursor、Grok 和非 Pro
套餐继续使用现有展示逻辑。

## 2. 范围

### 2.1 本次实现

- 从现有账号夹读中提取 `account/read.account.planType`。
- 同时提取夹读前后的 `account/rateLimits/read` planType，作为一致性证据。
- Go Helper 统一完成 Pro 档位解析。
- `AccountSnapshotResponse` 增加只读 `pro_tier`。
- Swift Popover/账号展示消费类型化档位，不复制 `planType` 映射。
- 保留当前账号 binding generation 隔离，避免账号切换时串用旧展示。
- 为 resolver、sandwich、runtime、Core/Helper 映射和 Swift presentation 增加聚焦测试。

### 2.2 明确不做

- 不提供手动选择、修改或清除档位。
- 不新增 SQLite 表、schema migration 或持久化记录。
- 不新增写 RPC、Settings picker 或 mutation/readback。
- 不新增真实 Home capability probe。
- 不根据额度百分比、Token、窗口时长、reset 时间或 Reset Credits 推断档位。
- 不修改账号登录、切换、多账号列表或订阅日期能力。
- 不修改本地 Session、Token、项目、趋势和成本的 Home 聚合口径。

## 3. 产品契约

### 3.1 输入

同一受控 App Server 会话按顺序读取：

```text
account/rateLimits/read
  → account/read
  → account/rateLimits/read
```

只消费以下字段：

- 前后 rate-limit response 的 `accountId`，仅用于派生并核对稳定 scope，随后清零内存字节。
- `account.planType`。
- 前后 response 的顶层及各 limit bucket `planType`。

不得把原始 `accountId`、token、Cookie、Authorization、完整 response 或上游错误正文写入
Proto、SQLite、日志、测试产物或文档。

### 3.2 解析规则

先对所有非空 planType 执行 trim 和 lowercase，再按以下规则解析：

| 证据 | 状态 | 档位 | UI |
| --- | --- | --- | --- |
| 所有非空值均为 `prolite` | `KNOWN` | `5X` | `Pro 5×` |
| 所有非空值均为 `pro` | `KNOWN` | `20X` | `Pro 20×` |
| 明确为 Free/Go/Plus/Team/Business/Enterprise/Edu | `NOT_APPLICABLE` | 无 | 沿用基础套餐 |
| 没有 planType | `PRO_UNKNOWN` | 无 | `Pro · 档位未知` |
| 出现未知 planType | `PRO_UNKNOWN` | 无 | `Pro · 档位未知` |
| 同一次夹读出现两个不同非空值 | `CONFLICT` | 无 | `Pro · 档位未知` |

冲突必须 fail closed，不能选择 `account/read` 或任意 bucket 作为优先值。

### 3.3 Core contract

`core-rpc-v3` 新增只读结构：

```proto
enum CodexProTier {
  CODEX_PRO_TIER_UNSPECIFIED = 0;
  CODEX_PRO_TIER_5X = 1;
  CODEX_PRO_TIER_20X = 2;
}

enum CodexProTierState {
  CODEX_PRO_TIER_STATE_UNSPECIFIED = 0;
  CODEX_PRO_TIER_STATE_KNOWN = 1;
  CODEX_PRO_TIER_STATE_PRO_UNKNOWN = 2;
  CODEX_PRO_TIER_STATE_CONFLICT = 3;
  CODEX_PRO_TIER_STATE_NOT_APPLICABLE = 4;
}

message CodexProTierSnapshot {
  CodexProTierState state = 1;
  optional CodexProTier tier = 2;
  string reason = 3;
}
```

`AccountSnapshotResponse.pro_tier` 使用 field 3。`Contracts.codex_pro_tier_version` 为
`codex-pro-tier-v1`。没有新增 Core command。

### 3.4 UI

- 仅 `type == chatgpt` 时消费 `pro_tier`。
- `KNOWN + 5X` 显示 `Pro 5×`。
- `KNOWN + 20X` 显示 `Pro 20×`。
- `PRO_UNKNOWN` 或 `CONFLICT` 显示 `Pro · 档位未知`。
- `NOT_APPLICABLE` 使用现有基础套餐名称。
- pending、signed out、identity unavailable 继续显示 `--`，并清除旧账号展示。
- accessibility 沿用现有账号套餐模板，不新增来源或手动状态文案。
- 截图和剪贴板继续复用现有账号胶囊隐藏机制。

## 4. 责任边界

| 层 | 责任 |
| --- | --- |
| App Server adapter | 收集夹读两侧有限 plan evidence；清理敏感 account ID |
| `subscriptiontier` | 归一化、冲突判断、只读映射和有限状态 |
| account binding runtime | 将 plan evidence 和当前 `scope + generation` 一起缓存 |
| application runtime | 解析 tier 并组装 `AccountSnapshot` |
| Core/Helper | 传输只读 typed snapshot 和 contract version |
| Swift presentation | 将 typed tier 转成三个用户文案 |

Swift 不得读取原始 plan token 后自行判断 5×/20×。

## 5. 文件改动

### 新增

- `internal/codex/subscriptiontier/model.go`
- `internal/codex/subscriptiontier/resolve.go`
- `internal/codex/subscriptiontier/resolve_test.go`
- `docs/test/codex-pro-tier.md`

### 修改

- `internal/codex/appserver/account.go`
- `internal/codex/appserver/account_test.go`
- `internal/app/account_binding_runtime.go`
- `internal/app/account_binding_runtime_test.go`
- `internal/app/lifecycle_runtime.go`
- `internal/app/account_runtime_test.go`
- `internal/core/service.go`
- `internal/core/service_test.go`
- `internal/helper/business_rpc.go`
- `internal/helper/server_test.go`
- `api/codexpulse/core/v1/core.proto`
- Go/Swift Proto 生成文件
- `app/macos/Sources/CodexPulseCoreClient/TransportContract.swift`
- `app/macos/Sources/CodexPulseAppSupport/OverviewModels.swift`
- Swift presentation 与 transport 聚焦测试
- README 和相关设计文档

## 6. 实施步骤

### Step 1：纯 resolver

1. 定义 `Tier`、`State`、`Evidence`、`Snapshot`。
2. 实现 plan token 归一化与去重。
3. 检测同侧 bucket 冲突及前后/账号字段冲突。
4. 实现 `prolite/pro` 固定映射和非 Pro allowlist。
5. 验证 `KNOWN` 必须有合法 tier，其余状态不得携带 tier。

### Step 2：夹读证据

1. 在 `AccountSandwich` 增加前后 planType 数组。
2. 从顶层 quota 和 `rateLimitsByLimitId` 收集、归一化、排序并去重。
3. 任一中间读取失败时清理已读取的 account ID。
4. 不返回额度数值或其它无关字段。

### Step 3：账号隔离与快照

1. 在现有 `accountDisplayCache` 中保存 plan evidence。
2. clone cache 时复制 slice，避免调用方修改缓存。
3. 继续依赖既有 sandwich scope 与 binding generation 核对。
4. `AccountSnapshot` 只在 confirmed binding 且 display 可用时返回 tier。

### Step 4：跨进程 contract

1. 新增 enum/message 和 `AccountSnapshotResponse.pro_tier`。
2. 更新 domain contract version 和精确握手版本。
3. 重新生成 Go/Swift Proto。
4. 不增加 command service、request 或 receipt。

### Step 5：Swift 展示

1. ChatGPT 账号读取 typed state/tier。
2. 渲染 `Pro 5×`、`Pro 20×`、`Pro · 档位未知`。
3. 非 Pro、Cursor、Grok 继续走原 `planDisplayName`。
4. 增加中英文字符串和聚焦 presentation 测试。

### Step 6：文档与验证

1. README 说明固定映射和禁止额度推断。
2. 架构文档说明 Go owner、只读 contract 和 Swift 边界。
3. 运行聚焦 Go/Swift 测试及 Proto drift 检查。
4. 按仓库约定不主动运行 `make verify` 或全仓 race 长测。

## 7. 聚焦验证

```bash
go test ./internal/codex/subscriptiontier ./internal/codex/appserver -count=1
go test ./internal/app -run 'Test(AccountBindingLoadDisplayRequiresMatchingSandwich|ConfirmedApplicationAccountUsesBindingDisplay|ConfirmedAccountSnapshotProbesAndTransitionsToNewAccount)' -count=1
go test ./api/codexpulse/core/v1 ./internal/core ./internal/helper -run 'Test.*(Proto|Contract|AccountSnapshot|AccountDisplay)' -count=1
bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check
make verify-swift-client
swift build --package-path app/macos --product codex-pulse-app
swift build --package-path app/macos --product codex-pulse-app-tests
```

本计划只编译 Swift App tests，确认新增 presentation 用例可编译；按仓库长测规则不执行完整
App deterministic executable。不运行真实 Home App、不写 `.artifacts`。

## 8. 验收标准

- `prolite` 一致证据返回并展示 `Pro 5×`。
- `pro` 一致证据返回并展示 `Pro 20×`。
- 缺失、未知或冲突证据不猜测档位。
- 非 Pro 套餐展示无回归。
- A→B 切换时旧账号 cache 不进入新 generation。
- Proto 中无手动字段、写 request、receipt 或 mutation RPC。
- Store schema 保持 v32，无 TOO-446 migration。
- Settings 无档位 picker。
- 仓库不包含 capability probe。
- 生成代码与 Proto 无 drift。
- 未执行的 CI、长测、真实 Home E2E 或人工 UI 验收不声明为通过。
