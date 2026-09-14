# TOO-442 Codex 当前账号额度隔离实施计划

> **执行要求：** Cursor 必须逐任务执行并在每个任务完成后核对验收项。若环境提供 `superpowers:executing-plans`，使用它按任务推进；不得使用 Orca，不得创建 `superpowers` worktree，不得跳过失败测试继续堆叠改动。

**Goal:** 在单机、单个已确认 Codex Home、账号顺序切换的前提下，让 Codex 在线额度、Reset Credits、刷新调度、缓存和账号展示严格绑定当前 ChatGPT 账号；本地 Session、Token、项目、趋势和成本继续保持 Home 聚合口径。

**Architecture:** 以 Codex CLI 0.154.0 稳定版公开的 App Server `account/rateLimits/read` 为唯一在线额度入口。响应中的 `accountId` 只在内存中短暂存在，本地用安装级随机密钥派生 HMAC scope；SQLite 持久化 scope、binding state 和 binding generation，不保存原始账号 ID。所有调度 claim、在线写入、查询响应和 Swift 展示都携带同一 `(account_scope, binding_generation)`，切换时先进入 `pending`、封闭旧代次、取消并排空旧请求，再发布新代次。`account/read` 的邮箱和套餐通过同一 App Server 会话中的 account ID 前后夹读来绑定。

**Tech Stack:** Go 1.25、SQLite/GORM、Codex App Server JSON-RPC v2、Protocol Buffers/gRPC、Swift 6/macOS、现有 cron 与 query invalidation 基础设施。

**Spec:** Linear TOO-442；仓库内以本文“产品契约”和“验收矩阵”为完整实施口径。

**Global constraints:** 不使用 Orca；不修改登录态、不执行登录/登出/账号切换；不持久化、不记录、不通过 Proto 暴露原始 `accountId`、token、JWT 或 Reset Credit 原始 ID；不把 `default` 历史归给任何账号；不改变本地 Home 聚合统计口径；未确认账号时显示 unknown，不伪造 `0%`、`100%` 或沿用上一账号数据；提交、push、PR、Linear 回写均需另行授权。

---

## 0. 结论与产品契约

### 0.1 0.154.0 上游证据

- 本计划基于本机已回读的 `codex-cli 0.154.0` 稳定版，不基于 alpha 或 `main` 的未发布行为。
- 官方 `Account` 协议仍只提供 `type`、`email`、`planType`，不能单独作为稳定账号身份：<https://github.com/openai/codex/blob/rust-v0.154.0/codex-rs/app-server-protocol/schema/typescript/v2/Account.ts>
- `codex app-server generate-json-schema` 的 0.154.0 产物确认 `GetAccountRateLimitsResponse` 包含：
  - 可空 `accountId`；
  - `rateLimits` 与可空 `rateLimitsByLimitId`；
  - 可空 `rateLimitResetCredits`；
  - 可空 `ordinaryUsageAllowed`；
  - `account/rateLimits/read` 与 `account/rateLimits/updated` 已进入稳定二进制。
- 本机调研快照位于 `.artifacts/too-442/codex-app-server-0.154.0-schema/`，它是忽略产物，不得提交。
- 官方认证文档允许 `file`、`keyring`、`auto`、`ephemeral`；因此实现不得假设 `auth.json` 永远存在：<https://learn.chatgpt.com/docs/auth>

### 0.2 必须满足的口径

| 对象 | 切换账号后口径 |
| --- | --- |
| 在线 Quota 当前值、历史与 Pace | 只读取当前 confirmed scope |
| Reset Credits | 只读取当前 confirmed scope |
| 在线 source state、schedule、claim、attempt | 绑定 scope；schedule/claim/attempt 还绑定 generation |
| 账号邮箱与套餐 | 只有与同一 scope 夹读成功时才展示 |
| 本地 Session、Token、项目、趋势、成本 | 继续按已确认 Codex Home 聚合，不增加账号筛选 |
| 本地 JSONL quota 观察 | 保留 `default` 未归属口径，不参与账号 scope 的当前在线额度 |
| 旧 `default` 在线历史 | 原样保留为 legacy/unassigned，不回填给首个发现账号 |
| `accountId == null` | `identity_unavailable`，不写在线事实、不显示上一账号数据 |
| A→B→A | A、B 的 scope 稳定复用；generation 依次递增；回到 A 时恢复 A 的原始历史时间戳 |

### 0.3 明确不做

- 不支持多 Home、多机器或并发账号池。
- 不提供登录、登出、切换账号入口，不写 Codex credential store。
- 不做本地 Token/Session 的账号级归因、筛选或历史迁移。
- 不把邮箱、套餐或 Reset Credit ID 当账号主键。
- 不实现 Reset Credit consume；本卡只隔离当前库存和读取链路。
- 不改变 Cursor、Grok、API Subscription 的账号或额度实现。
- 不提交调研生成的完整 App Server schema。

---

## Task 1：锁定 App Server 0.154.0 额度契约

**Files:**

- Create: `internal/codex/appserver/rate_limits.go`
- Create: `internal/codex/appserver/rate_limits_test.go`
- Create: `internal/codex/appserver/testdata/rate_limits_v0154.json`
- Modify: `internal/codex/appserver/rpc.go`
- Modify: `internal/codex/appserver/rpc_test.go`
- Modify: `internal/codex/appserver/process.go`
- Modify: `internal/codex/appserver/process_test.go`

- [x] **Step 1：先写 0.154.0 正常响应测试**

测试 fixture 必须覆盖：单 bucket、多 bucket、`accountId`、Reset Credits 详情、`credits: null`、`ordinaryUsageAllowed: null`。fixture 中只能使用合成 ID，例如 `acct-test-a`。

核心类型固定为：

```go
type SensitiveAccountID []byte

type RateLimitWindow struct {
	UsedPercent       int32
	WindowDurationMins *int64
	ResetsAtSeconds    *int64
}

type RateLimitSnapshot struct {
	LimitID             *string
	LimitName           *string
	PlanType            *string
	Primary             *RateLimitWindow
	Secondary           *RateLimitWindow
	SpendControlReached *bool
}

type RateLimitResetCredit struct {
	ID               string
	Status           string
	ResetType        string
	GrantedAtSeconds int64
	ExpiresAtSeconds *int64
}

type RateLimitResetCreditsSummary struct {
	AvailableCount int64
	Credits        []RateLimitResetCredit
}

type AccountRateLimitsSnapshot struct {
	AccountID             SensitiveAccountID
	RateLimits            RateLimitSnapshot
	RateLimitsByLimitID   map[string]RateLimitSnapshot
	RateLimitResetCredits *RateLimitResetCreditsSummary
	OrdinaryUsageAllowed  *bool
}
```

`SensitiveAccountID` 不实现 `Stringer`、JSON marshal 或日志接口；任何错误只返回固定分类，不包含字段值。`Credits == nil` 表示 upstream null/absent，非 nil 的零长度 slice 表示明确空数组，归一化时必须保留这个差异。

- [x] **Step 2：运行失败测试**

Run: `go test ./internal/codex/appserver -run 'Test(ReadAccountRateLimits|NormalizeAccountRateLimits|RPCError)'`

Actual: FAIL（build failed，类型和 reader 尚不存在）。

- [x] **Step 3：实现公开 RPC reader 与严格归一化**

实现：

```go
func ReadLocalAccountRateLimits(
	ctx context.Context,
	confirmedHome ConfirmedHome,
	options ProcessOptions,
	excludeResetCreditDetails bool,
) (AccountRateLimitsSnapshot, error)
```

RPC params 必须编码为：

```go
type accountRateLimitsReadParams struct {
	ExcludeResetCreditDetails bool `json:"excludeResetCreditDetails"`
}
```

归一化规则：

- `accountId` 必须为 1–256 字节、UTF-8 有效、无首尾空白；否则视为 identity unavailable，不修剪后继续使用。
- `usedPercent` 只接受 `0..100`。
- `windowDurationMins`、`resetsAt`、credit 时间戳必须满足现有 store 时间范围。
- `rateLimitsByLimitId != nil` 时逐 bucket 解析，map key 与非空 `limitId` 不一致即 schema incompatible；为 nil 时回退 `rateLimits`。
- `availableCount` 是权威总数；`credits == null` 表示只有总数，不能把它解释为空列表。
- Reset Credit 原始 ID 只允许传给紧邻的哈希转换，不进入错误、日志或 Core contract。
- 忽略 `rateLimitUpsell` 的内容，不保存展示文案。

- [x] **Step 4：让 RPC 错误可分类但不可泄露**

在 `rpc.go` 增加：

```go
type RPCError struct {
	Code int64
}

func (err RPCError) Error() string {
	return fmt.Sprintf("App Server RPC error %d", err.Code)
}
```

只保留 code，不保留上游 message、data、body。`-32601` 映射为 capability unavailable；上下文取消保持 `context.Canceled` / `DeadlineExceeded`。

- [x] **Step 5：锁定实际执行的 Codex binary 能力**

额度 reader 必须解析并校验实际执行的 `codex --version`，最低稳定能力基线为 `0.154.0`。GUI 环境找不到 PATH 命令时，候选列表应先包含 `$HOME/.local/bin/codex`，再考虑应用包与 NVM；不得硬编码当前用户名或把 `0.154.0-alpha.*` 当作满足稳定版下限。选择结果至少在内部 runtime diagnostics 中可读回 path、version、capability state，错误中不包含 Home 内容。

当前机器候选版本是：活跃 `~/.local/bin/codex` 为 0.154.0、旧 NVM 副本为 0.150.1、ChatGPT.app 内置副本为 0.154.0-alpha.6.2。实现和 live 验收必须证明实际选择的是稳定 0.154.0，而不是凭 PATH 假设。

- [x] **Step 6：验证**

Run: `go test ./internal/codex/appserver -count=1 -timeout 120s`

Actual: PASS（`ok github.com/SisyphusSQ/codex-pulse/internal/codex/appserver 8.791s`）。错误路径不包含 `acct-test-a` / credit 原始 ID。

**Commit checkpoint（仅在用户另行授权 commit 时执行）：**

```bash
git add internal/codex/appserver
git commit -m "feat: 接入 Codex App Server 额度契约"
```

---

## Task 2：增加 v32 账号 binding 与 scope schema

**Files:**

- Create: `internal/store/account_binding_schema.go`
- Create: `internal/store/account_binding_models.go`
- Create: `internal/store/account_binding_migration_test.go`
- Modify: `internal/store/migration.go`
- Modify: `internal/store/quota_schema.go`
- Modify: `internal/store/quota_projection_schema.go`
- Modify: `internal/store/quota_schedule_schema.go`
- Modify: `internal/store/runtime_schema.go`
- Modify: `internal/store/models.go`
- Modify: `internal/store/runtime_records.go`
- Modify: `internal/store/quota_records.go`
- Modify: `internal/store/reset_credits_records.go`
- Modify: `internal/store/reset_credits_models.go`
- Modify: `internal/store/runtime_schema_test.go`
- Modify: `internal/store/migration_test.go`
- Modify: `internal/store/api_subscription_migration_test.go`
- Modify: `internal/store/scheduler_migration_test.go`
- Modify: `internal/store/quota_projection_migration_test.go`
- Modify: `internal/store/lifecycle_migration_test.go`
- Modify: `internal/store/health_migration_test.go`
- Modify: `internal/store/attribution_migration_test.go`
- Modify: `internal/store/cost_migration_test.go`
- Modify: `internal/store/quota_limit_name_migration_test.go`
- Modify: `internal/store/cursor_dashboard_quota_migration_test.go`
- Modify: `internal/store/quota_failure_migration_test.go`
- Modify: `internal/store/cursor_session_metadata_migration_test.go`
- Modify: `internal/store/quota_performance_migration_test.go`
- Modify: `internal/store/quota_migration_test.go`
- Modify: `internal/store/quota_schedule_migration_test.go`
- Modify: `internal/store/light_index_migration_test.go`
- Modify: `docs/test/migrations.md`

- [x] **Step 1：写 v31→v32 失败测试**

测试必须建立真实 v31 fixture，并验证：

1. 所有 legacy `account_scope='default'` observation/current/evidence/reset snapshot 数量和 checksum 保持不变；
2. v31 的 active claim 在迁移后变为 `abandoned`；旧 default schedule 变为 `disabled` 且 `next_due_at_ms=NULL`；
3. 新表、索引、FK、CHECK 与 STRICT 属性完整；
4. 迁移中途注入失败时整体 rollback；
5. reopen 后 schema version 为 32；
6. legacy default 不成为 `codex_account_binding.account_scope`。

Run: `go test ./internal/store -run 'TestAccountBindingMigration'`

Expected: FAIL，当前 schema version 为 31。

Actual: 先写 `TestAccountBindingMigration*`；实现 catalog 升到 32 后，`go test ./internal/store -run 'TestAccountBindingMigration' -count=1` 为 PASS（`ok` 1.146s）。v31 fixture、legacy default checksum、active claim → `abandoned`、default schedule → `disabled`/`next_due_at_ms=NULL`、rollback、reopen=32、binding 不吸收 `default` 均覆盖。

- [x] **Step 2：定义三个隐私安全表**

目标 schema：

```sql
CREATE TABLE codex_account_scope_key (
  singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
  key_bytes BLOB NOT NULL CHECK (length(key_bytes) = 32),
  created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0)
) STRICT;

CREATE TABLE codex_account_scopes (
  account_scope TEXT PRIMARY KEY CHECK (
    length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*'
  ),
  first_seen_at_ms INTEGER NOT NULL CHECK (first_seen_at_ms >= 0),
  last_seen_at_ms INTEGER NOT NULL CHECK (last_seen_at_ms >= first_seen_at_ms)
) STRICT;

CREATE TABLE codex_account_binding (
  singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
  state TEXT NOT NULL CHECK (state IN (
    'unknown', 'pending', 'confirmed', 'signed_out', 'identity_unavailable'
  )),
  account_scope TEXT REFERENCES codex_account_scopes(account_scope) ON DELETE RESTRICT,
  last_confirmed_scope TEXT REFERENCES codex_account_scopes(account_scope) ON DELETE RESTRICT,
  binding_generation INTEGER NOT NULL CHECK (binding_generation >= 0),
  observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
  reason TEXT NOT NULL CHECK (reason IN (
    'startup', 'stable', 'account_changed', 'signed_out', 'missing_account_id',
    'confirmation_failed', 'unsupported_app_server'
  )),
  CHECK ((state = 'confirmed') = (account_scope IS NOT NULL)),
  CHECK (state != 'confirmed' OR last_confirmed_scope IS NOT NULL),
  CHECK (state != 'confirmed' OR account_scope = last_confirmed_scope),
  CHECK (state != 'confirmed' OR binding_generation > 0)
) STRICT;
```

`last_confirmed_scope` 只用于状态机判断同账号恢复，不得进入 Core 或 Swift 响应。`pending` 的 `account_scope` 必须为空，查询不能使用 `last_confirmed_scope`。

Actual: 四张表（含 `source_refresh_global_fences`）已写入 `internal/store/account_binding_schema.go`，并进入 v32 checksum。

- [x] **Step 3：重建受旧 CHECK 限制的表**

下列表允许 legacy `default` 或 64 位 lowercase hex scope：

- `quota_observations`
- `quota_current`
- `quota_arbitration_evidence`
- `reset_credit_snapshots`

`quota_observations.source` 增加 `app_server`，旧 `wham` 数据原样保留。迁移使用 rename → create → ordered copy → FK check → drop old table 的既有模式，不能用 GORM AutoMigrate 代替。

重建必须覆盖完整 FK closure：`quota_observation_receipts`、`quota_current`、`quota_arbitration_evidence` 随 `quota_observations` 一起按父表后子表的顺序重建；`reset_credit_snapshots`、`reset_credits` 随 `source_attempts` 一起重建。不得留下指向 `*_v31` 临时表的 FK，迁移末尾显式执行 `PRAGMA foreign_key_check`。

同时重建 `reset_credits` 以适配 0.154.0 正式协议：`expires_at_ms` 改为 nullable；status CHECK 接受 legacy 的 `expired`/`used` 和 App Server 的 `available`/`redeeming`/`redeemed`/`unknown`；移除“redeemed 必须有 redeemed_at_ms”的旧 WHAM 假设。`reset_credit_snapshots.available_count` 上限从 100 调整为 1,000,000，详情行仍限制最多 100 条并由 service 层拒绝超限输入。

Actual: 未改写冻结的 v9/v11/v12 `quotaSchemaObjects*` 原文（改写会破坏旧 checksum）。v32 目标 SQL 放在 `quotaSchemaObjectsV32` 等新对象；`currentQuotaSchemaObjects()` 与 `verifyApplicationSchema` 指向 v32。迁移按 rename → create → copy → drop → `PRAGMA foreign_key_check`。

- [x] **Step 4：给调度审计增加 generation**

重建：

- `source_refresh_schedules.binding_generation INTEGER NOT NULL CHECK >= 0`
- `source_refresh_claims.binding_generation INTEGER NOT NULL CHECK >= 0`
- `source_attempts.binding_generation INTEGER NOT NULL CHECK >= 0`

legacy 行回填 `0`。新 account-scoped App Server 行必须 `>0`。新增 schedule reason `inactive_account`，不得复用 `disabled` 表示账号切换。

Actual: schedule/claim/attempt 已重建并回填 `binding_generation=0`；reason allowlist 含 `inactive_account`。

- [x] **Step 5：增加跨账号全局刷新闸门**

```sql
CREATE TABLE source_refresh_global_fences (
  source_group TEXT PRIMARY KEY CHECK (source_group = 'codex_app_server_rate_limits'),
  not_before_ms INTEGER NOT NULL CHECK (not_before_ms >= 0),
  reason TEXT NOT NULL CHECK (reason IN ('manual_interval', 'network_backoff')),
  updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
) STRICT;
```

这个 fence 跨 scope、跨 quota/reset logical source 生效，防止通过 A→B 切换绕过人工刷新最小间隔。

Actual: `source_refresh_global_fences` 已建表，`source_group` 仅允许 `codex_app_server_rate_limits`。

- [x] **Step 6：更新 migration catalog 与 schema contract**

新增：

```go
const applicationSchemaV32Version = 32
const applicationSchemaVersion = applicationSchemaV32Version
```

迁移名固定为 `codex-account-binding`。checksum 必须覆盖所有建表、重建、索引、数据修复语句。

所有把 31 当作 current/target version 的 catalog、backup 和 upgrade 测试都追加 32；专门冻结 `applicationSchemaV31Checksum` 或验证 v31 本身行为的测试继续保留原断言，再单独增加 v32 checksum freeze 测试，禁止改写旧 migration checksum。

Actual: catalog 追加 `codex-account-binding`；v32 checksum 冻结为 `0aedee7a707f37b27a1249886b260cad7cbe1da35ea4ad302b45f27c4d430f52`；v31 checksum 仍为 `2ad86dfc7e17ca34217875545af500d9cd0607bfd4da36859f7b5e184757bc63`。历史 v14–v18 verifier 改为 `runtimeSchemaObjectsThroughV14()`，避免把 v32 的 `source_attempts.binding_generation` 误当成中间版本合同。

- [x] **Step 7：验证**

Run: `go test ./internal/store -run 'Test(AccountBindingMigration|RuntimeSchema|MigrationCatalog)'`

Expected: PASS；`PRAGMA foreign_key_check` 无结果。

Actual: `go test ./internal/store -run 'Test(AccountBindingMigration|ApplicationSchemaV32Checksum|RuntimeSchema|MigrationCatalog)' -count=1` PASS（`ok` 3.362s）。补充 `go test ./internal/store -run 'TestApplication' -count=1` PASS（`ok` 19.457s）。

**Commit checkpoint（仅授权后）：**

```bash
git add internal/store docs/test/migrations.md
git commit -m "feat: 增加 Codex 账号绑定存储模型"
```

---

## Task 3：实现 scope 派生与 binding 状态机

**Files:**

- Create: `internal/codex/accountbinding/scope.go`
- Create: `internal/codex/accountbinding/scope_test.go`
- Create: `internal/store/account_binding_records.go`
- Create: `internal/store/account_binding_repository.go`
- Create: `internal/store/account_binding_repository_test.go`
- Modify: `internal/store/repository_validate.go`

- [x] **Step 1：写 scope 派生和 A→B→A 状态测试**

覆盖：相同 key+ID 得相同 scope；不同 ID 或不同安装 key 得不同 scope；scope 恰为 64 位 lowercase hex；错误不包含原始 ID；并发首次建 key 只保留一个值。

状态序列必须断言：

| 输入 | 结果 state | scope | generation |
| --- | --- | --- | --- |
| 首次确认 A | confirmed | A | 1 |
| A token 刷新后仍为 A | confirmed | A | 1 |
| 检测切换、尚未确认 | pending | empty | 2 |
| 确认 B | confirmed | B | 2 |
| B 登出 | signed_out | empty | 3 |
| 再确认 A | confirmed | A | 3 |

Actual: 先写 `TestDeriveScope*` 与 `TestCodexAccountBinding*`，编译失败（`DeriveScope` / repository API 不存在）。

- [x] **Step 2：实现安装级 HMAC scope**

固定接口：

```go
const ScopeDomain = "codex-pulse/account-scope/v1\x00"

func DeriveScope(key [32]byte, accountID []byte) (string, error)
```

算法为 `hex(HMAC-SHA256(key, ScopeDomain || accountID))`。禁止使用裸 SHA256、邮箱、套餐或 Reset Credit ID。调用完成后清空持有 account ID 的可变 byte slice。

Actual: `internal/codex/accountbinding.DeriveScope` 按固定 domain 派生；非法 ID 只返回 `account identity unavailable`；返回前清零调用方 buffer。

- [x] **Step 3：实现 repository API**

```go
type CodexAccountBinding struct {
	State             CodexAccountBindingState
	AccountScope      *string
	BindingGeneration int64
	ObservedAtMS      int64
	Reason            CodexAccountBindingReason
}

func (repository *Repository) EnsureCodexAccountScopeKey(
	ctx context.Context,
	candidate [32]byte,
	createdAtMS int64,
) ([32]byte, error)

func (repository *Repository) MarkCodexAccountBindingPending(
	ctx context.Context,
	observedAtMS int64,
	reason CodexAccountBindingReason,
) (CodexAccountBinding, error)

func (repository *Repository) ConfirmCodexAccountBinding(
	ctx context.Context,
	accountScope string,
	observedAtMS int64,
	reason CodexAccountBindingReason,
) (CodexAccountBinding, bool, error)

func (repository *Repository) SetCodexAccountBindingUnavailable(
	ctx context.Context,
	state CodexAccountBindingState,
	observedAtMS int64,
	reason CodexAccountBindingReason,
) (CodexAccountBinding, bool, error)

func (repository *Repository) CodexAccountBinding(
	ctx context.Context,
) (CodexAccountBinding, error)
```

返回的 bool 表示外部可观察的 `(state, scope, generation)` 是否变化。相同 confirmed scope 只更新 `last_seen_at_ms` / `observed_at_ms`，不递增 generation。离开 confirmed 进入 pending/signed_out/identity_unavailable 时先递增 generation，以永久封死旧请求；从 pending 或 unavailable 再确认时复用该新 generation。初始 generation 为 0，第一次确认变为 1。

Actual: repository API 已落地；`default` scope 被拒绝；A→B→A 恢复 A 的 scope 行并使用新 generation。

- [x] **Step 4：增加事务内 writer fence**

```go
var ErrCodexAccountBindingChanged = errors.New("Codex account binding changed")

func requireCodexAccountFence(
	ctx context.Context,
	transaction *gorm.DB,
	accountScope string,
	bindingGeneration int64,
) error
```

只接受数据库当前 `state=confirmed` 且 scope、generation 完全相等。错误不得返回当前 scope 或期望 scope。

Actual: `requireCodexAccountFence` 在写事务内重读 binding；pending 后旧 generation 返回 `ErrCodexAccountBindingChanged`，错误不含 scope。

- [x] **Step 5：验证**

Run: `go test ./internal/codex/accountbinding ./internal/store -run 'Test(DeriveScope|CodexAccountBinding)'`

Expected: PASS，并通过 `go test -race` 的并发首次初始化用例。

Actual: `go test ./internal/codex/accountbinding ./internal/store -run 'Test(DeriveScope|CodexAccountBinding)' -count=1` PASS。`go test -race ./internal/codex/accountbinding ./internal/store -run 'Test(DeriveScope|CodexAccountBinding)' -count=1` PASS（含并发首次建 key）。

**Commit checkpoint（仅授权后）：**

```bash
git add internal/codex/accountbinding internal/store
git commit -m "feat: 实现 Codex 账号 scope 与代次状态机"
```

---

## Task 4：用 App Server 替换直连 WHAM 客户端

**Files:**

- Modify: `internal/codex/quota/types.go`
- Modify: `internal/codex/quota/client.go`
- Modify: `internal/codex/quota/client_test.go`
- Modify: `internal/codex/quota/reset_credits_client.go`
- Modify: `internal/codex/quota/reset_credits_client_test.go`
- Modify: `internal/codex/quota/decode.go`
- Delete: `internal/codex/quota/credentials.go`
- Delete after replacement: `internal/app/quota_credentials_darwin.go`
- Delete after replacement: `internal/app/quota_credentials_test.go`

- [x] **Step 1：先把测试改成 App Server transport contract**

测试已改为 fake `AccountRateLimitsReader`，覆盖 confirmed A、mismatch B、`accountId=null`、`rateLimitsByLimitId`、null credits、partial details、`expiresAt=null`、cancel/capability/schema 分类，以及 retry 跨账号 fail closed。

- [x] **Step 2：改成显式 BoundRefreshRequest**

Quota Fetch 与 Reset Credits Fetch 均接收 `BoundRefreshRequest`；每次 App Server 响应先派生 scope 再与 admission fence 比较。

- [x] **Step 3：转换 App Server typed snapshot**

`QuotaSourceAppServer = "app_server"`；observation/snapshot ID 包含 scope、generation、request ID 与观察时间。Reset Credits 支持 unavailable/partial/complete；complete 按 **available 状态条数 vs 权威 AvailableCount** 判定（含 redeemed 行时总行数可以大于 count）。`expiresAt=null` 映射为 nullable `ExpiresAtMS`。

- [x] **Step 4：保留有界 retry，但禁止跨账号 retry**

scope mismatch 或 missing account ID 立即停止。App Server 失败不伪造 HTTP 200。`schema_incompatible` 暂停自动重试（`NextDueAtMS == nil`）；startup/manual 仍可探测。

- [x] **Step 5：删除直连凭据和私有 endpoint**

已删除 `internal/codex/quota/credentials.go`、`internal/app/quota_credentials_darwin.go`、`internal/app/quota_credentials_test.go`。Codex 路径不再出现 `WhamUsageEndpoint`、`WhamResetCreditsEndpoint`、`WithAccessToken`、`ChatGPT-Account-Id`。

- [x] **Step 6：验证**

实际命令与结果：

```text
go test ./internal/codex/quota -count=1
# ok

go test ./internal/codex/appserver -run 'Test(SensitiveAccountID|NormalizeAccountRateLimits|ReadAccountRateLimits|RPCError)' -count=1
# ok

go test ./internal/store -run 'TestRecordQuotaFetch|TestRecordResetCreditsFetch|TestResetCreditsSummary' -count=1
# ok

go test ./internal/app -run 'TestApplicationQuota|TestApplicationLifecycleRuntime' -count=1
# ok

go test ./internal/app -count=1
# ok
```

计划原文 `go test ... -run 'Test(Quota|ResetCredits|AccountRateLimits)'` 过窄，未匹配 `TestClient*`、`TestApplicationQuota*`、`TestNormalizeAccountRateLimits*`，因此用上面的 targeted 集合作为 Task 4 验收。

`rg -n 'WhamUsageEndpoint|WhamResetCreditsEndpoint|WithAccessToken|Authorization.*Bearer|ChatGPT-Account-Id' internal`：Codex WHAM 串已清零。`Authorization.*Bearer` 仍匹配 Cursor / Grok / API Subscription 生产代码和 `internal/privacy/policy_test.go` 夹具；按产品约束不得删除。

事实冲突与最小 store 合同修正：

1. Task 4 文件列表未含 store writer，但 App Server observation 无 HTTP status、HMAC `account_scope` 与 record `ScopeKey=default` 并存，必须改 `quota_fetch.go` / `reset_credits_repository.go`。
2. v32 `reset_credit_snapshots` 无 `details_status` 列；DetailsStatus 在内存，读回按 available 条数启发式重建。
3. `server_error` 允许 `HTTPStatus == nil`（App Server RPC）或 5xx（既有测试夹具）。

**Commit checkpoint（仅授权后）：**

```bash
git add internal/codex/quota internal/codex/appserver internal/app
git commit -m "refactor: 改用 App Server 读取 Codex 额度"
```

---

## Task 5：实现账号发现、确认与进程内代次切换

**Files:**

- Create: `internal/app/account_binding_runtime.go`
- Create: `internal/app/account_binding_runtime_test.go`
- Modify: `internal/app/quota_runtime.go`
- Modify: `internal/app/quota_runtime_test.go`
- Modify: `internal/app/lifecycle_runtime.go`
- Modify: `internal/app/account_runtime_test.go`

- [x] **Step 1：写确定性并发测试**

用 channel barrier 覆盖：

1. A 请求阻塞时发现 B：先 seal，A context 被 cancel，等待 A drain 后才发布 B；
2. A detached recorder 在 B 发布后到达：writer fence 拒绝；
3. 发现 B 后第二次确认失败：binding 停在 pending，查询为空，不恢复 A；
4. B 确认成功后只启动 B generation；
5. A→B→A 恢复相同 A scope、不同 generation；
6. Home generation 变化与 account generation 变化同时发生时，先处理 Home fence，再处理 account fence，无死锁。

- [x] **Step 2：实现 runtime contract**

```go
type activeCodexAccount struct {
	Scope      string
	Generation int64
}

type accountBindingRuntime struct {
	mu        sync.Mutex
	active    *activeCodexAccount
	accepting bool
	cancel    context.CancelFunc
	inflight  sync.WaitGroup
}
```

锁顺序固定为：Home lifecycle lock → account transition lock → quota admission lock → repository transaction。禁止持有 runtime mutex 调用 SQLite、等待 WaitGroup 或执行 App Server RPC。

- [x] **Step 3：实现两次确认切换协议**

首次启动或当前无 confirmed binding：

1. Helper 启动恢复出旧 confirmed binding 时，先写 pending 并递增 generation；在确认完成前不开放 quota runtime、不发布旧账号 Overview；
2. 调一次 `account/rateLimits/read(excludeResetCreditDetails=true)` 只发现 identity；
3. account ID 缺失则写 `identity_unavailable`；
4. 派生 scope 并写 confirmed generation；
5. 丢弃 discovery 响应中的额度，不绕过 durable claim；
6. 激活 scope schedule，并立即发起正常 claimed refresh。

已确认 A 的 refresh 若读到 B：

1. 不解码、不记录该响应；
2. 事务写 pending 并先递增 generation，使旧 writer fence 立即失效；
3. seal A admission、cancel A context、drain A；
4. 独立第二次 read 确认 B；
5. 两次 B scope 相同才发布 B generation；
6. 激活 B schedule、暂停 A schedule、发 account + quota invalidation；
7. 第二次失败则保持 pending，等待 foreground/wake/manual/下个 scheduled probe。

- [x] **Step 4：定义检测触发点**

必须触发账号确认：startup、foreground、wake、manual refresh、Codex `AccountSnapshot`、每次 scheduled fetch 返回 scope mismatch。不得新增 1 秒在线轮询；复用现有生命周期与刷新节奏。

本卡不新增 auth 文件 watcher，不读取 keyring，也不订阅不可靠的跨进程通知；所有 storage mode 都只依赖 App Server。账号切换检测延迟以最近一次 startup/foreground/wake/manual/account/scheduled trigger 为上界，任何一次检测开始后 UI 先隐藏旧账号展示，直到一致性确认完成。

- [x] **Step 5：实现 account/read 夹读**

在同一 `withInitializedLocalRPC` 会话执行：

```text
account/rateLimits/read -> account/read -> account/rateLimits/read
```

前后 `accountId` 派生 scope 必须一致，并且与最终 repository binding `(scope,generation)` 一致，才返回邮箱/套餐。否则返回无 identity 的 pending binding；不得用第一次或中间结果拼接。

夹读属于一个有界 identity operation：只在 startup、foreground、wake、binding 变化或当前 generation 尚无账号展示缓存时执行。普通 Overview 重绘复用同 generation 的已确认账号展示，不能每次都额外发两次在线 read；一个夹读操作内部允许连续两次 rate-limit read，完成后再更新全局 probe 时间，防止 UI 刷新绕过调度节奏。

- [x] **Step 6：验证**

实际命令与结果：

```text
go test ./internal/app -run 'Test(AccountBinding|ApplicationQuota|ConfirmedApplicationAccount)' -count=1
# ok

go test ./internal/codex/appserver -run 'TestReadAccountSandwich' -count=1
# ok

go test ./internal/app -count=1
# ok

go test -race ./internal/app ./internal/codex/appserver -run 'Test(AccountBinding|ApplicationQuota|ConfirmedApplicationAccount)' -count=1
# ok  internal/app
# ok  internal/codex/appserver  [no tests matched the race filter; sandwich covered above]
```

锁顺序：Home drain 先封闭 quota admission；account transition 使用独立 channel，禁止持有 runtime mutex 调 SQLite / WaitGroup / RPC。mismatch 的 `HandleObservedScopeChange` 在 Fetch 栈外异步执行，避免 drain 重入 admission。manual refresh 的身份探测走本次 Fetch 的 `accountId` fence，而不是在 `beginControlAdmission` 前再发一次 Read——后者会让 Home drain 看不到 in-flight identity RPC。

confirmed 账号的 foreground/wake probe 若只是瞬时 Read 失败，保持 confirmed，不进入 pending。missing `accountId` 仍 fail closed 为 `identity_unavailable`。

**Commit checkpoint（仅授权后）：**

```bash
git add internal/app internal/codex/appserver
git commit -m "feat: 增加 Codex 账号切换代次围栏"
```

---

## Task 6：让 durable scheduler 按 scope/generation 运行

**Files:**

- Modify: `internal/store/source_refresh_schedule_models.go`
- Modify: `internal/store/runtime_schema.go`
- Modify: `internal/store/models.go`
- Modify: `internal/store/runtime_records.go`
- Modify: `internal/store/source_refresh_schedule_records.go`
- Modify: `internal/store/source_refresh_schedule_repository.go`
- Create: `internal/store/source_refresh_schedule_repository_test.go`
- Modify: `internal/scheduler/quota_refresh.go`
- Modify: `internal/scheduler/quota_refresh_test.go`
- Modify: `internal/scheduler/integration_test.go`
- Modify: `internal/codex/quota/schedule.go`
- Modify: `internal/codex/quota/schedule_test.go`

- [x] **Step 1：写 scope-aware schedule 失败测试**

覆盖：A/B source instance 不同；同 scope 新 generation 更新 schedule generation；旧 generation CAS claim 失败；切换暂停旧账号；返回 A 后历史 schedule 恢复并立即 replan；manual fence 在 A/B 间共享。

- [x] **Step 2：替换固定 default ID**

```go
func QuotaSourceInstanceAppServer(scope string) string {
	return "quota:app_server:" + scope
}

func ResetCreditsSourceInstanceAppServer(scope string) string {
	return "reset_credits:app_server:" + scope
}
```

新 source type 固定为 `app_server_rate_limits` 和 `app_server_reset_credits`。旧 `*WhamDefault` 常量只允许 migration/legacy query 使用，不得进入运行时 descriptor。

- [x] **Step 3：descriptor 必须从 confirmed binding 构造**

```go
type refreshSourceDescriptor struct {
	source            quota.RefreshSource
	sourceInstanceID  string
	sourceType        string
	scopeKey          string
	bindingGeneration int64
	enabled           bool
	intervalSeconds   int64
	fetcher           SourceRefreshFetcher
}
```

无 confirmed binding 时 `descriptors` 返回空；同时把当前已知 account-scoped schedules 置为 inactive，不创建 `default` 新 schedule。

- [x] **Step 4：claim/finalize 全程核对 generation**

claim CAS 条件增加 `binding_generation`；fetch request 携带 claim scope/generation；complete 之前再次读取 binding。过期或切换后的 claim 只能 `abandoned`，不能更新 source state、next due 或成功时间。

- [x] **Step 5：实现跨账号 global fence**

manual refresh 成功 admission 时写 `now + 60s`；网络 backoff 写 max(existing, computed)。quota 和 reset logical source 都在 claim 前检查同一 `codex_app_server_rate_limits` fence。

- [x] **Step 6：验证**

Run: `go test -race ./internal/scheduler ./internal/store ./internal/codex/quota -run 'Test.*(Account|Binding|Scope|Generation|Manual)' -count=1`

Actual: PASS（scheduler 22.048s，store 202.818s，quota 2.088s）。

补充全包：`go test ./internal/app ./internal/store ./internal/codex/quota ./internal/scheduler -count=1` PASS。

事实冲突与锁顺序：

1. 同一用户动作会顺序 `RequestRefresh(quota)` 再 `RequestRefresh(reset)`。manual fence 在 A/B 间共享，但同 scope 的另一个 logical source 必须仍能在同一 `atMS` 认领；否则 reset 会被 quota 刚写入的 `now+60s` fence 饿死。实现为 `manual_interval` fence 对同 `scope_key` 的 companion source 放行，跨账号仍拦截。
2. 共享 `network_backoff` fence 若在 due cycle 第一次 Complete 时 raise，会挡住同 cycle 的另一个 source。backoff 改到 due cycle 全部 execute 之后。
3. Home 切换 `RearmAfterHomeChange` 必须清掉上一 Home 的 global fence。Clock 冻结时旧 backoff 的 `not_before` 仍大于 now，否则新 Home 探测会被 claim 挡掉。
4. fetch request 改为 `BoundRefreshRequest`，`executeClaim` 把 descriptor 的 `(account_scope, binding_generation)` 传给 `FetchBound`，不再用进程内 `Fetch(requestID)` 隐式绑定。

锁顺序保持：Home generation drain → account transition → quota admission → repository 写事务。

**Commit checkpoint（仅授权后）：**

```bash
git add internal/scheduler internal/store internal/codex/quota
git commit -m "feat: 按 Codex 账号调度在线额度刷新"
```

---

## Task 7：给记录事务增加最终 writer fence

**Files:**

- Modify: `internal/codex/quota/service.go`
- Modify: `internal/codex/quota/service_test.go`
- Modify: `internal/codex/quota/reset_credits_service.go`
- Modify: `internal/codex/quota/reset_credits_service_test.go`
- Modify: `internal/store/quota_fetch.go`
- Modify: `internal/store/quota_records.go`
- Modify: `internal/store/reset_credits_records.go`
- Modify: `internal/store/quota_repository_test.go`
- Modify: `internal/store/quota_projection_test.go`

- [x] **Step 1：写 late reply 污染测试**

启动 A record transaction 前切换到 B，断言：

- 不写 `source_attempts`；
- 不写 observation/reset snapshot；
- 不更新 A 或 B source state/current/evidence；
- claim 只能在 scheduler 侧标记 abandoned；
- service 返回 typed `ErrCodexAccountBindingChanged`，不包装原始 scope。

- [x] **Step 2：所有 record DTO 携带 fence**

```go
type QuotaFetchRecord struct {
	AccountScope      string
	BindingGeneration int64
	SourceInstanceID  string
	SourceType        string
	Attempt            SourceAttempt
	Observations       []QuotaObservationSample
}

type ResetCreditsFetchRecord struct {
	AccountScope      string
	BindingGeneration int64
	SourceInstanceID  string
	SourceType        string
	Attempt            SourceAttempt
	Snapshot           *ResetCreditsSnapshot
}
```

- [x] **Step 3：在同一 SQLite transaction 首句检查 fence**

`RecordQuotaFetch` 与 `RecordResetCreditsFetch` 必须先 `requireCodexAccountFence`，再写 attempt/source state/事实/projection。不能在事务外先检查后写。

- [x] **Step 4：隔离 local JSONL 聚合事实**

local JSONL 继续写 `account_scope=default`，且 projection rebuild 不得把 default/local observation 复制到 account scope。App Server observation 只能写 64 位 scope。

- [x] **Step 5：验证**

Run: `go test -race ./internal/store ./internal/codex/quota -run 'Test.*(Late|Fence|Record|Projection)' -count=1`

Actual: PASS。

补充：`go test ./internal/store -count=1` PASS；`go test ./internal/codex/quota ./internal/scheduler ./internal/app -count=1` PASS。

SQLite `Write` 仍会给错误加上 `sqlite write callback:` 前缀，但不包含 scope；调用方用 `errors.Is(..., ErrCodexAccountBindingChanged)` 判定。进程内 `SetBinding` 不能代替事务内 `requireCodexAccountFence`。

**Commit checkpoint（仅授权后）：**

```bash
git add internal/codex/quota internal/store
git commit -m "fix: 阻止旧账号额度结果延迟写入"
```

---

## Task 8：查询只投影当前 confirmed binding

**Files:**

- Modify: `internal/store/quota_query_snapshot.go`
- Modify: `internal/store/quota_query_snapshot_test.go`
- Modify: `internal/codex/quota/current_query.go`
- Modify: `internal/codex/quota/current_query_test.go`
- Modify: `internal/codex/quota/pace_query.go`
- Modify: `internal/codex/quota/pace_query_test.go`

- [x] **Step 1：写 unknown 与 A→B→A 查询测试**

测试必须断言：

- unknown/pending/signed_out/identity_unavailable 返回空 online windows 和 Reset Credits；
- 返回明确 binding state/reason，不把空解释为 0%；
- B 激活时完全看不到 A quota/pace/reset；
- 回到 A 后恢复 A 的原 observation timestamp，不复制、不改写历史；
- A 历史已过 fresh window 时按现有动态规则显示 stale/expired_unknown；
- legacy `default` 永不成为当前账号数据。
- Reset Credits count-only/partial 响应保留 `availableCount`，但不伪造 `cumulativeRemainingMS`、`nextExpiresAtMS` 或缺失详情。

- [x] **Step 2：在一个 SQLite read transaction 读取 binding + facts**

```go
type QuotaCurrentSnapshot struct {
	Binding             CodexAccountBinding
	AccountScope        string
	BindingGeneration   int64
	EvaluatedAtMS       int64
	Windows             []QuotaCurrentWindowSnapshot
	OnlineSourceState   *SourceState
	QuotaRefresh        *SourceRefreshSchedule
	ResetCredits        ResetCreditsSummary
	ResetCreditsRefresh *SourceRefreshSchedule
}
```

外部 Query 不接受任意 Codex scope；repository 先读当前 binding，再按 scope 读全部事实。Cursor/Grok router 保持原接口。

- [x] **Step 3：pace 使用同一 binding**

Pace current、cycle、history band 全部只查询 active scope。绑定非 confirmed 时返回 `binding_unavailable`，而不是 fallback default。

- [x] **Step 4：验证**

Run: `go test ./internal/store ./internal/codex/quota -run 'Test.*(Current|Pace|Binding|ABA)' -count=1`

Actual: PASS。

补充：`go test ./internal/store ./internal/codex/quota -count=1` PASS。

**Commit checkpoint（仅授权后）：**

```bash
git add internal/store internal/codex/quota
git commit -m "feat: 按当前账号投影 Codex 在线额度"
```

---

## Task 9：扩展 Core/Proto 与 invalidation contract

**Files:**

- Modify: `api/codexpulse/core/v1/core.proto`
- Regenerate: `api/codexpulse/core/v1/core.pb.go`
- Regenerate: `api/codexpulse/core/v1/core_grpc.pb.go`
- Regenerate: `app/macos/Sources/CodexPulseProtocolGenerated/core.pb.swift`
- Regenerate: `app/macos/Sources/CodexPulseProtocolGenerated/core.grpc.swift`
- Modify: `api/codexpulse/core/v1/core_proto_contract_test.go`
- Modify: `internal/core/service.go`
- Modify: `internal/core/service_test.go`
- Modify: `internal/core/codec.go`
- Modify: `internal/core/codec_test.go`
- Modify: `internal/core/invalidation.go`
- Modify: `internal/core/invalidation_test.go`
- Modify: `internal/app/query_invalidation.go`
- Modify: `internal/app/query_invalidation_test.go`
- Modify: `app/macos/Sources/CodexPulseCoreClient/TransportContract.swift`

- [x] **Step 1：先写 additive Proto contract 测试**

固定新 message：

```proto
message CodexAccountBinding {
  string state = 1;
  optional string account_scope = 2;
  uint64 binding_generation = 3;
  int64 observed_at_ms = 4;
  optional string reason = 5;
}
```

字段追加规则：

- `AccountSnapshotResponse.binding = 2`
- `CurrentQuota.binding = 9`
- `CurrentQuotaPace.binding = 5`
- `CurrentQuotaPace.unknown_reason = 6`（Task 8 把 `binding_unavailable` 放在 pace envelope；空 windows 无法承载该 unknown。这是与 Task 9 原稿的事实冲突修正，additive。）
- `CurrentResetCredits.details_state = 12`
- `CurrentResetCreditItem.expires_at_ms` 保持 tag 4，但改为 `optional int64`

`account_scope` 是本机 HMAC scope，不是上游 `accountId`。非 Codex provider 的 binding 字段保持 absent。

- [x] **Step 2：实现 Core 语义**

Codex account snapshot 返回夹读绑定；quota/current/pace 返回同一 store snapshot 的 binding。pending/unavailable 时仍返回结构化响应，但 account absent、windows empty、unknown reason 明确。

- [x] **Step 3：新增 account invalidation 并升级版本**

```go
const InvalidationContractVersion = "query-invalidation-v3"
const InvalidationAccount InvalidationDomain = "account"
```

账号 state/scope/generation 变化时按顺序发 `account`、`quota`。单纯同 scope token 刷新不发 account invalidation。`account` domain 不触发 Cursor dashboard `InvalidateUsageAndQuota`。

- [x] **Step 4：生成代码**

Run: `make generate-proto`

Actual: `protobuf generation --write passed`。随后 `bash scripts/proto/generate-swift.sh --write` 生成 Swift stubs。未手改 generated 文件。Swift `ResetCreditItemPresentation.expiresAtMS` 改为 optional，以匹配 `optional int64 expires_at_ms`。

- [x] **Step 5：验证**

Run: `go test ./api/codexpulse/core/v1 ./internal/core ./internal/app -run 'Test.*(Proto|Codec|Invalidation|Account)'`

Actual: PASS。

**Commit checkpoint（仅授权后）：**

```bash
git add api/codexpulse/core/v1 app/macos/Sources/CodexPulseProtocolGenerated internal/core internal/app app/macos/Sources/CodexPulseCoreClient/TransportContract.swift
git commit -m "feat: 传递 Codex 账号绑定上下文"
```

---

## Task 10：Swift 端阻止账号与额度错配

**Files:**

- Modify: `app/macos/Sources/CodexPulseAppSupport/OverviewModels.swift`
- Modify: `app/macos/Sources/CodexPulseAppSupport/AppRuntime.swift`
- Modify: `app/macos/Sources/CodexPulseAppSupport/AppModel.swift`
- Modify: `app/macos/Sources/CodexPulseAppSupport/ProductCopy.swift`
- Modify: `app/macos/Sources/CodexPulseApp/RootView.swift`
- Modify: `app/macos/Sources/CodexPulseApp/QuotaHealthViews.swift`
- Modify: `app/macos/Sources/CodexPulseApp/StatusItemController.swift`
- Modify: `app/macos/Tests/CodexPulseAppTests/main.swift`
- Modify: `app/macos/Tests/CodexPulseCoreClientTests/main.swift`

- [x] **Step 1：定义不可混合的 context key**

```swift
struct CodexAccountContextKey: Equatable, Sendable {
    let scope: String
    let generation: UInt64
}
```

只有 `state == "confirmed"`、scope 非空、generation > 0 时才产生 key。quota、pace、account 三者的 key 必须相等。

- [x] **Step 2：先写乱序响应失败测试**

覆盖：

- Overview 已发布 A quota，B account 后到：丢弃 B account，并安排一次完整 Overview refresh；
- Overview 已发布 B quota，A account 后到：同样丢弃；
- 新 Overview 取得 B quota 时，构造阶段不能直接沿用 previousAccount=A；第一次 publish 就必须清空 A account；
- quota=B、pace=A 的并发读结果不能同时发布；保留 B quota、把 pace 显式降为 unknown 并安排一致性 refresh；
- pending account response 到达：立即移除旧 account，不复用 A email；
- `account` invalidation 到来时取消 account task 与 overview task，下一次只发布同 key 组合；
- A→B→A 时 cache key 包含 context key，不能命中旧 generation 的组合对象；
- Cursor/Grok 不受 Codex binding 逻辑影响。

- [x] **Step 3：修改 `finishAccountRefresh`**

当前的无条件 `replacingAccount` 改为：

```swift
guard let quotaKey = responses.codexAccountContextKey,
      let accountKey = response.codexAccountContextKey,
      quotaKey == accountKey
else {
    invalidateCodexAccountPresentation()
    scheduleAccountConsistentOverviewRefresh()
    return
}
```

禁止仅凭 Swift 本地 task generation 判断账号一致；task generation 与 server binding generation 是两层独立 fence。

构造 `OverviewResponses` 时先计算 quota key：`previousAccount` 只有 key 完全相等才可复用，否则传 nil；quotaPace 若携带 confirmed key 且与 quota 不同，替换为 unavailable pace 并触发下一次一致性 refresh。任何一次 `publishOverview` 前都运行同一个组合验证器，不能只在 account completion 分支检查。

- [x] **Step 4：修正 cache 与 invalidation**

Codex Overview cache key 增加可空 account context；收到 `account` invalidation 时清空 Codex provider cache。Invalidation stream domains 增加 `account`，transport version 改为 `query-invalidation-v3`。

- [x] **Step 5：明确 UI 文案边界**

Codex 在线卡片标题使用“当前账号额度 / Current account limits”；本地使用、Session、项目、成本区域使用“当前 Codex Home 已采集 / Collected from current Codex Home”。pending/unavailable 显示“账号确认中”或“当前账号额度暂不可用”，不显示旧百分比。

- [x] **Step 6：验证**

Run: `mkdir -p bin && go build -trimpath -o bin/codex-pulse-cancel-probe ./scripts/swift-cancel-probe && CODEX_PULSE_CANCEL_PROBE="$(pwd)/bin/codex-pulse-cancel-probe" swift run --package-path app/macos codex-pulse-core-client-tests`

Actual: PASS（`CodexPulseCoreClient deterministic tests passed`）。直接 `swift run` 会因缺少 `CODEX_PULSE_CANCEL_PROBE` 以 exit 133 失败，因此使用 Makefile `verify-swift-client` 的等价探测二进制。

Run: `swift run --package-path app/macos codex-pulse-app-tests`

Actual: PASS（`CodexPulseApp deterministic tests passed`）。乱序测试覆盖 “B 身份 + A 额度” 不可发布、首次 B quota 不得复用 A account、A pace 不得与 B quota 同发、`account` invalidation 取消 in-flight Overview，以及 A→B→A cache 不复用旧 generation。

**Commit checkpoint（仅授权后）：**

```bash
git add app/macos
git commit -m "fix: 阻止 macOS 端账号与额度错配"
```

---

## Task 11：补齐集成、恢复与隐私测试

**Files:**

- Modify: `internal/app/quota_runtime.go`（账号切换 `SealAndDrain` 后 `PublishBinding` 必须 `ResumeGeneration`）
- Create: `internal/app/account_binding_e2e_test.go`
- Modify: `internal/scheduler/integration_test.go`
- Modify: `internal/store/runtime_queries_test.go`
- Modify: `internal/store/runtime_schema_test.go`
- Modify: `internal/codex/appserver/rate_limits_test.go`
- Modify: `app/macos/Tests/CodexPulseAppTests/main.swift`
- Create: `docs/test/codex-account-switching.md`

`internal/store/migration_catalog_upgrade_e2e_test.go` 仍是 `upgradee2e` 构建标签下的 catalog 截断测试，不单独承载 v32 产品语义；v32 由 `account_binding_migration_test.go` 与 lifecycle catalog 覆盖。

- [x] **Step 1：构建 synthetic A-B-A E2E**

应用层 E2E 在 `internal/app/account_binding_e2e_test.go`：A 成功 → B 首次失败保持 pending/unknown → B 成功 → A 恢复原时间戳 + 新 generation。App Server 进程串行身份在 `TestReadAccountRateLimitsScriptedProcessReturnsSequentialIdentities`。Swift 组合在 `CodexAccountContext` / AppRuntime 测试。三层共用同一 `(scope, generation)` 口径，不是单测同时驱动真实 process + Swift。

事实修正：账号切换 `SealAndDrain` 会停掉 Home generation runner；`PublishBinding` 必须按当前 Home generation `ResumeGeneration`，否则 B 确认后 `RequestRefresh` 得到 `application quota runtime is unavailable`。

- [x] **Step 2：覆盖 crash/restart**

- crash pending：重启不显示旧 A。
- crash confirmed B 且尚无 B schedule：重启后 `ReconcilePreferences` 补同一 scope/generation 的 App Server schedule。
- crash 时 A active claim：重启列出过期 claim 并 Release，不能再写回。
- DB 中 key 已存在：重启复用同 key，scope 不漂移。
- 拷贝 v32 sqlite 文件恢复 binding 与 schedule。

- [x] **Step 3：隐私回归扫描**

合成 `acct-secret-a` / `acct-secret-b` / `token-secret-a` / `credit-secret-a` 后扫描 SQL-visible text/blob、WAL/主库文件、Core proto/JSON。原文不得出现；只允许 64 位 scope。

- [x] **Step 4：验证 targeted integration**

Run: `go test -race -timeout 40m -count=1 ./internal/app ./internal/scheduler ./internal/store ./internal/codex/...`

默认 10m 不够跑完 `./internal/store` race，因此显式加长 timeout；命令语义不变。

Actual（Go race）：PASS。`internal/app` 328.289s，`internal/scheduler` 318.642s，`internal/store` 1236.199s，`internal/codex/accountbinding` 2.496s，`internal/codex/appserver` 10.085s，`internal/codex/homeidentity` 2.933s，`internal/codex/index` 78.940s，`internal/codex/logs/parser` 5.314s，`internal/codex/logs/source` 4.874s，`internal/codex/quota` 133.688s。

Run: `swift run --package-path app/macos codex-pulse-app-tests`

Actual（Swift）：PASS（`CodexPulseApp deterministic tests passed`）。`cancelRefresh` 后过期 refresh 不得 `refreshTask = nil` 清掉替换任务，否则会把 replacement 打成 `providerMismatch`。

**Commit checkpoint（仅授权后）：**

```bash
git add internal app/macos docs/test/codex-account-switching.md
git commit -m "test: 覆盖 Codex 账号切换隔离链路"
```

---

## Task 12：同步设计、运行手册与入口文档

**Files:**

- Modify: `README.md`
- Modify: `docs/design/README.md`
- Modify: `docs/design/details/quota/README.md`
- Modify: `docs/design/details/data-model/README.md`
- Modify: `docs/design/details/native-macos-client/README.md`
- Modify: `docs/test/quota-runtime.md`
- Modify: `docs/test/quota-source-job-health-settings.md`
- Modify: `docs/test/runtime-schema.md`
- Modify: `docs/test/codex-account-switching.md`

- [x] **Step 1：更新架构真相**

文档必须明确：

- Codex 在线额度从 App Server 公共接口读取，不再直连 WHAM；
- 最低能力基线为包含 `account/rateLimits/read.accountId` 的 Codex CLI，当前验证版本 0.154.0；
- capability unavailable 的失败语义；
- scope/generation 与 legacy default 的含义；
- 本地 Home 聚合和当前账号在线数据是两条不同口径。

Actual: 已写入 `README.md`、`docs/design/README.md`、`docs/design/details/quota/README.md`、`docs/design/details/data-model/README.md`、`docs/design/details/native-macos-client/README.md`、`docs/test/quota-runtime.md`、`docs/test/quota-source-job-health-settings.md`、`docs/test/runtime-schema.md`。历史 WHAM / `auth.json` lease / `account_scope=default` 均标注 legacy。

- [x] **Step 2：写 live A-B-A runbook**

`docs/test/codex-account-switching.md` 必须包含：

1. 前置条件与真实 Home 授权边界；
2. A 登录状态读回、A quota/account key readback；
3. 用户自行切到 B，应用只能观察，不能代替用户登录；
4. B 首次失败时 unknown 验证；
5. B 成功后旧 A 不可见；
6. 用户切回 A 后历史时间戳恢复、generation 改变；
7. SQLite/日志/Proto 隐私扫描；
8. `PASS / FAIL / ERROR / NOT_RUN / INCOMPLETE` 结果模板；
9. 第二个账号不可用时标记 `NOT_RUN`，不得用 synthetic 测试冒充 live。

- [x] **Step 3：验证文档链接与术语**

Run: `rg -n 'MVP|最小改动|WhamUsageEndpoint|WhamResetCreditsEndpoint|account_scope.?=.?default' README.md docs/design docs/test`

Actual: 无 `MVP` / `最小改动` / `WhamUsageEndpoint` / `WhamResetCreditsEndpoint`。`account_scope=default` 命中均带 legacy/unassigned 说明。

**Commit checkpoint（仅授权后）：**

```bash
git add README.md docs/design docs/test
git commit -m "docs: 说明 Codex 当前账号额度隔离"
```

---

## Task 13：完整验证与对抗式审查

- [x] **Step 1：运行开发完成门禁**

Run: `make check`

Actual: PASS。BSD awk 按字节切 UTF-8 会把 `Localizable.strings` 误报成重复 key `高`；`scripts/project-checks/check.sh` 改为按完整 quoted key 查重。Cursor/Grok 的零值 `binding` 结构体不得编进 Proto，`CurrentResponse`/`PaceResponse.Binding` 改为指针 + omitempty。Swift `testRecoveryDuringDisconnectedStreamRefreshesAfterReconnectWithoutReplay` 在门禁负载下曾 flake 一次，重跑通过。

Run: `make verify`

Actual: INCOMPLETE。第一次在 `internal/store` race 约 602s 触达默认包超时失败；第二次按用户要求停止，未重跑全仓 race / `make verify` / `make verify-live`。这是 synthetic/isolated CI 证据缺口，不能写成 PASS。

- [x] **Step 2：执行真实 Home A-B-A 验收**

运行 `make verify-live` 前必须再次说明：会读取真实 Session/JSONL，并可能写私有 runtime、SQLite、preferences 与 App Server housekeeping。按 `docs/test/codex-account-switching.md` 执行，不记录 token、原始账号 ID、真实邮箱或原始 JSONL。

Actual: NOT_RUN。用户要求本轮只做短项、禁止长时间 verify；未启动 `make verify-live`，也没有两个真实账号的现场切换记录。不得用 Task 11 synthetic A-B-A 冒充 live。

- [x] **Step 3：执行 5 项对抗式审查**

必须逐项回答并修复发现的问题：

1. 是否存在任何不经过 App Server 的 Codex 在线请求或 credential 读取？
2. 是否存在任何 DB 写入只在事务外检查 binding？
3. 是否存在任意 fallback 把 pending/unavailable 映射为 default、0 或上一账号？
4. 是否存在 Swift 组合只检查 task generation、不检查 server context key？
5. 是否存在 legacy migration 把 default 数据归给首个账号，或日志/Proto 泄露 raw ID？

Actual:

1. 否。生产 Go 无 `chatgpt.com/backend-api/wham`、`WhamUsageEndpoint`、`WhamResetCreditsEndpoint`。Codex `quota.Client` 只通过 `AccountRateLimitsReader` 走 App Server `account/rateLimits/read`。`auth.json` 读取仅剩 Grok、Home metadata probe（只探文件身份，不读 token）和测试夹具。审查中把 `schedule.go` 启动探测注释从「auth.json 恢复」改成「App Server 登录态恢复」，避免暗示仍读凭据。
2. 否。`RecordQuotaFetch` / `RecordResetCreditsFetch` 在 `database.Write` 内调用 `requireOnlineFetchAccountFence`，由同一事务 `requireCodexAccountFence` 重读 confirmed `(scope, generation)`。`ClaimSourceRefresh` 在同一写事务里比较并 `UPDATE ... AND binding_generation = ?`。
3. 否。`QuotaCurrentSnapshot` 在同一 snapshot 读 binding；非 confirmed 返回空 windows，不投影 `default`。`mapCodexBoundCurrentResponse` 对 pending/unavailable 返回空 windows + `binding_unavailable`，不伪造 0%/100%。Swift `reusableAccount` 在 Codex 下 quota key 与 previous account key 不等则丢弃上一账号。`sealLegacyCodexSchedulesForV32` 只 disable `scope_key='default'`，不归属首个账号。
4. 否。`CodexAccountContext.validatePublishedOverview` 比较 quota/pace/account 的 server `(scope, generation)`；缺失 account 不当错配，实际 payload 不一致才 strip。`finishAccountRefresh` 除 task generation 外再比较 `quotaKey == accountKey`，错配则 `publishOverview` + `scheduleAccountConsistentOverviewRefresh`。
5. 否。v32 拷贝保留原 `account_scope` 列，不把 `default` 改写成派生 scope；`TestAccountBindingV32DoesNotAssignLegacyDefault` 断言无 binding 行被派给 legacy default。Proto 只有 `account_scope` / `binding_generation` / 展示用 `email`，无 raw `accountId`。Cursor/Grok 零值 binding 已改为指针 + omitempty，避免空 message 进响应。

- [x] **Step 4：最终静态检查**

Run: `git diff --check`

Actual: PASS。

Run: `rg -n 'TO[D]O|TB[D]|FIXM[E]|placeholde[r]|稍[后]补|后[续]实现' docs/superpowers/plans/2026-09-14-too-442-account-binding.md`

Actual: 无匹配。

- [x] **Step 5：交付报告**

分层结果见下。没有证据的层级写 `NOT_RUN` 或 `INCOMPLETE`，不能用单元测试替代 live 结论。

| 层级 | 结果 |
| --- | --- |
| Task 1–12 实现与 targeted tests | PASS（Go：`./internal/app ./internal/scheduler ./internal/store ./internal/codex/...` race；Swift App tests） |
| `make check` | PASS |
| `make verify` | INCOMPLETE（用户叫停全仓 race） |
| live A-B-A / `make verify-live` | NOT_RUN |
| 五项对抗式审查 | 完成；发现并修正一处误导性 `auth.json` 注释 |
| 未执行 | 全仓 `go test -race ./...`、`make verify`、`make verify-live`、真实账号切换、commit / push / PR / Linear |

提交、push、PR、Linear 状态更新不属于本计划的默认授权。若用户随后要求 commit/push/PR，遵循仓库规则：进入提交收尾后复用已有测试结论，不重复运行测试。

---

## 完成定义

以下条件全部成立，TOO-442 才可视为完成：

- App Server 0.154.0 公开契约是唯一 Codex 在线额度入口；
- SQLite 中没有原始账号 ID、token、JWT、Reset Credit 原始 ID；
- 当前账号无法确认时，所有在线 quota/reset/account 展示为 unknown；
- claim、attempt、writer、query、Proto、Swift 组合均有 scope+generation fence；
- A→B→A synthetic 全链路通过，且 A/B 数据从未交叉；
- migration 保留 legacy default 数据但不进行自动归属；
- 本地 Home 聚合使用量未被改成账号级；
- `make check` 与 `make verify` 通过；
- 有两个真实账号时完成 live A-B-A，否则明确 NOT_RUN；
- 文档、设计和 runbook 已同步；
- 未进行未授权的 commit、push、PR 或 Linear 回写。
