# TOO-445 Codex、Cursor、Grok 独立启停与自动发现实施计划

## 1. Goal

为 `codex`、`cursor`、`grok` 建立由 Go Helper 统一掌握的 Provider 生命周期控制：

- 未有显式选择时，通过安全的 metadata-only discovery 决定是否默认启用。
- 用户显式关闭后，重启、唤醒、前台恢复、数据源重新出现和应用升级都不得自动重开。
- 关闭覆盖本地采集、在线请求、凭据续期、查询触发刷新、全局刷新和跨 Provider 汇总，而不是只隐藏 Swift UI。
- 关闭完成后，旧 generation 的在途任务不得发布新快照、缓存或 invalidation。
- 历史事实、配置、索引进度和子开关偏好继续保留；重新开启沿用现有增量、last-good 和节流机制。
- 主窗口与 Popover 只选择当前启用的 Provider；全部关闭时展示明确空态，并始终允许进入 Settings。

本计划完成的判定是：Preferences、Helper 控制面、所有实际入口、Core RPC、Swift 状态与 UI、文档和聚焦验证形成闭环。CI、真实 Home、人工 UI、签名、发布和 Linear 回写属于独立证据层，未实际执行时不得写成通过。

## 2. 事实源与当前基线

### 2.1 事实源

- Linear：`TOO-445`《支持 Codex、Cursor、Grok 独立启停与自动发现》。2026-09-16 回读状态为 `In Progress`，无 comment、relation、release 或外部 blocker。
- 仓库：`/Users/suqing/Coding/golang/00_self/codex-pulse`
- 实施分支：`suqing/too-445-provider-controls`
- 分支基线：`main@5fd105c739ad49a7859d3d9a4980a005cf813748`，与本地 `origin/main` 一致。
- 仓库规则：根目录 `AGENTS.md`；`api/codexpulse/core/v1/core.proto` 是唯一跨进程 contract，Go Helper 是业务真相，Swift 不读取 SQLite/JSONL，也不复制 Provider 启用判断。

### 2.2 当前实现事实

- `internal/preferences/types.go` 的 preferences schema 为 v2，`Snapshot.CodexHome` 必填；`validatePreferences` 要求 Home generation 非零。
- `internal/app/default_home.go` 只有探测到安全的默认 Codex Home 才首次写入 preferences。
- `internal/app/app.go:openNormalRuntime` 在公共 Core graph 后启动 `applicationLifecycleRuntime`，Core 的 `RuntimeControls`、Codex quota、deep index 和 account 都绑定到该 Codex 生命周期。
- `internal/providerrefresh/orchestrator.go` 固定按 `codex → cursor → grok` 并发刷新；已有 `skipped_disabled` 结果类型，但没有 Provider 主开关。
- `internal/cursorprovider/query.go` 与 `internal/grokprovider/query.go` 在查询已有快照后通过后台 goroutine 触发本地/在线刷新；首次无快照时会同步采集。
- `internal/query/agentrouter/service.go` 固定路由三家，空 scope 兼容为 Codex；没有启用状态 gate。
- `internal/query/dashboardsummary/service.go` 固定汇总三家，disabled Provider 的旧数据目前仍可能进入当前 totals/freshness。
- `SettingsOnlineSnapshot` 只有 Codex quota/reset 和 Grok quota/credential refresh；Cursor 没有在线子开关，也没有三家总开关。
- Core/Swift 精确握手版本为 `core-rpc-v4`，application SQLite schema 为 v33。本需求不需要新增业务表或提升 application schema。
- Swift `AgentProvider.allCases` 固定三家；`AppModel.selectedProvider/statusProvider` 非 optional、默认 Codex 并写入 UserDefaults；主窗口和 Popover picker 都直接列出三家。
- `AppRuntime.start()` 在 bootstrap normal 后立刻安排初始 Overview，然后才由 `AppModel` 条件性加载 Settings，存在 disabled Codex 仍先发查询的竞态。

## 3. Scope

### 3.1 本次实现

- preferences v3、v1/v2 迁移、首次无 Codex Home 初始化、严格 JSON shape、CAS/readback。
- Provider intent、discovery、effective/runtime state 和安全 reason code。
- Helper 常驻 Provider 控制器、operation admission、cancel/drain、generation 与 commit fence。
- Codex 公共控制面和 Codex Home worker 解耦。
- Codex/Cursor/Grok 本地与在线入口、账号读取、凭据刷新、生命周期刷新、手动刷新、查询触发采集和汇总 gate。
- Settings/Core/Helper/Swift contract 及 Go/Swift generated clients。
- Swift 启动前 Provider catalog、选择修复、任务取消、缓存失效、设置分组和全部关闭空态。
- README、Provider/Preferences/Scheduling/native 设计和可复现验收 runbook。

### 3.2 Out of Scope

- 新增第四个 Provider、通用插件框架或 `AgentProvider.all`。
- 删除历史、清空 Store、重写源 Session 文件或回填猜测数据。
- 登录管理、自动切号、多账号并行采集、会员日期或订阅列表扩展。
- 更改 Codex/Cursor/Grok 既有用量、额度、成本、Session/Project 业务口径。
- 生产部署、签名、公证、发版、提交、推送、PR、Linear 状态或评论回写。
- 用 isolated/synthetic Home 结果替代真实产品验收。

## 4. 冻结的产品与状态语义

### 4.1 三层状态

每个 Provider 固定包含三层状态：

| 层 | 值 | 语义 |
| --- | --- | --- |
| `intent` | `auto` / `enabled` / `disabled` | 持久化用户意图 |
| `discovery` | `unchecked` / `available` / `missing` / `inaccessible` / `invalid` | metadata-only 探测结果 |
| `effective` | `enabled` / `disabled` / `unavailable` / `disabling` | 当前是否允许进入业务链路 |

状态矩阵：

| intent | discovery | effective |
| --- | --- | --- |
| `auto` | `available` | `enabled` |
| `auto` | 非 `available` | `unavailable` |
| `enabled` | `available` | `enabled` |
| `enabled` | 非 `available` | `unavailable`，但保留显式开启意图 |
| `disabled` | 任意 | `disabled`；disable/drain 期间为 `disabling` |

`intent` 只有 Settings mutation 可以改变。Discovery 不得把显式 `disabled` 改回 `auto/enabled`；数据源消失也不得把显式 `enabled` 改成 `disabled`。

### 4.2 二态 UI 映射

- Settings 主开关关闭时写入 `disabled`。
- Settings 主开关开启时写入 `enabled`。
- `auto` 是迁移和首次默认状态，不增加三态 picker；UI 用“自动发现”状态标签解释。
- `auto + available` 的开关显示开启；`auto + missing/inaccessible/invalid` 显示关闭和对应发现状态。用户从该状态开启时写入显式 `enabled`。
- 主开关关闭或 Provider unavailable 时，子开关置灰但不改值。

### 4.3 Discovery 边界

Discovery 只允许路径解析、`lstat/stat`、类型/权限和已存在安全探针读取的物理身份信息：

- Codex：使用配置中的 confirmed Home；首次无配置时只探测 `${CODEX_HOME:-$HOME/.codex}`。安全 Home 才是 `available`。
- Cursor：使用 `cursorprovider.DefaultConfig()`，`ProjectsRoot` 或 `StateDatabase` 至少一个是可访问的预期类型时为 `available`；conversation/AI tracking 数据库缺失只影响 capability/coverage，不把整个 Provider 判为 missing。
- Grok：使用 `grokprovider.DefaultConfig()`；Home 和 `SessionsRoot` 为安全可访问目录时为 `available`。`auth.json` 缺失只让账号/billing 子能力 unavailable，不关闭本地 Provider。

Discovery 不得打开 Session/JSONL 正文、Cursor SQLite 内容、Grok `auth.json`，不得启动 App Server、发网络请求、刷新凭据或写任何源目录。Reason code 固定为 `available`、`not_found`、`permission_denied`、`unsafe_path`、`invalid_type`、`probe_failed`，不得携带真实路径或底层错误正文。

Discovery 触发点固定为启动、system wake、application foreground、Settings 页面 read/refresh 和现有五分钟 scheduled tick。显式 disabled Provider 仍可做 metadata-only discovery 以展示“已发现 · 已关闭”，但不得进入业务采集。

### 4.4 关闭、查询和历史语义

- 新 admission 先关闭，再取消 operation context，再 drain 已接纳任务。
- `applied` 只在 operation/commit lease 均排空且 runtime 稳定为 `disabled` 后返回。
- 若 preferences 已提交但 drain 未完成，返回既有 `applied_reconcile_required`；Settings 显示“正在停止”，不能显示普通成功。后台只允许继续收尾，不能重新开放 admission。
- 旧 generation 无法再取得 commit lease；已持有的短 commit lease 必须在 stable disabled 前完成并排空。
- direct query 命中显式 disabled 时返回新稳定错误 `provider_disabled`、`retryable=false`；unavailable 继续使用可恢复的 `unavailable`。
- 当前 Provider 页面不返回 disabled Provider 的历史快照冒充当前事实。历史仍保存在 Store，由 Sources/Jobs/以后专用历史入口解释。
- DashboardSummary 只聚合 effective enabled Provider；全部关闭时返回 complete 的已知空结果：provider count 为 0、可加总数值为 0、providers 为空，不返回 service unavailable。
- 全局手动刷新回执仍按固定三家顺序返回；disabled 项及其 components 使用 `skipped_disabled/disabled`，便于解释且不算失败。

### 4.5 子开关

- Codex：沿用 `quota_enabled`、`reset_credits_enabled`。
- Cursor：新增 `cursor_online_enabled`，统一控制 Dashboard 月额度、usage events 和 Grok Bot 在线请求；本地 snapshot 仍受 Cursor 主开关控制。
- Grok：沿用 `grok_quota_enabled`、`grok_auto_refresh_enabled`。
- Provider 主开关优先于子开关；主开关关闭期间不修改子开关持久值。

## 5. Preferences v3 与首次初始化

### 5.1 Domain 结构

修改：

- `internal/preferences/types.go`
- `internal/preferences/validation.go`
- `internal/preferences/json_shape.go`
- `internal/preferences/migration.go`
- `internal/preferences/file_store.go`
- `internal/preferences/service.go`
- 对应 `preferences_test.go`、`file_store_test.go`、`service_test.go`

冻结结构：

```go
type ProviderIntent string

const (
    ProviderIntentAuto     ProviderIntent = "auto"
    ProviderIntentEnabled  ProviderIntent = "enabled"
    ProviderIntentDisabled ProviderIntent = "disabled"
)

type ProviderPreference struct {
    Intent ProviderIntent `json:"intent"`
}

type ProviderPreferences struct {
    Codex  ProviderPreference `json:"codex"`
    Cursor ProviderPreference `json:"cursor"`
    Grok   ProviderPreference `json:"grok"`
}
```

- `Snapshot.CodexHome` 改为 `*CodexHomePreferences`，v3 JSON 允许省略 `codex_home`，但不接受显式 `null`。
- `Snapshot.Providers ProviderPreferences` 为 v3 必填。
- `OnlinePreferences` 增加 `CursorOnlineEnabled bool`，v3 必填。
- `SettingsUpdate` 增加完整的 `Providers`；`settingsEqual`、clone、validation 和 CAS 全部包含该字段。
- `Onboarding.Completed` 在 v3 表示应用 preferences 已初始化，不再表示 Codex Home 必然存在。

### 5.2 迁移规则

- 保留显式 `preferencesSchemaV2 = 2`，将 `CurrentPreferencesSchemaVersion` 提升到 3。
- legacy onboarding v1 直接迁移到 v3，Codex Home 保留，三家 intent 为 `auto`，Cursor online 默认 `true`。
- v2 → v3 保留 revision、Home、detached homes、pending switch/resume、last switch、所有 online/refresh/update/UI 字段；三家 intent 为 `auto`，Cursor online 为 `true`。
- v3 strict JSON 要求 `providers.codex/cursor/grok.intent` 精确存在，不允许未知 Provider、未知 intent、重复/空对象或 null。
- 迁移写回继续使用跨实例锁、原子替换和权威 readback；durability unknown 不伪装成功。

### 5.3 无 Home 初始化与 Home switch 兼容

- `FileStore` 新增只在文件不存在时发布完整 v3 snapshot 的 `InitializePreferences`；保持 `Confirm(OnboardingSnapshot)` 的兼容入口并让其复用新初始化路径。
- `ensureDefaultCodexHomeConfigured` 重构为“始终初始化 preferences，安全探测成功时附带 Codex Home”；默认 Home 缺失/无权限时写入 `CodexHome=nil`，而不是让整个 App unconfigured。
- 已有 preferences 永不因默认 Home 变化被覆盖。
- `Preferences.Service`、`SwitchPlan` 和 journal 校验支持从 nil Home 首次配置：初次 target generation 为 1，默认 data store key 为 `default`；已有 Home 的 switch/recovery 语义保持不变。
- Home switch RPC 的公开 receipt 继续只返回 generation/result，不暴露 nil Home 或路径。Codex 未配置时禁止 deep index、Codex quota/account 和 runtime repair，但 Settings、Cursor、Grok、API subscriptions、health 和 dashboard 仍可用。
- `SettingsHomeSnapshot` 在未配置时固定返回 `configured=false`、`generation="0"`、`switch_status=stable`；首次成功配置后的 generation 为 `1`。`redactedHomeSwitchReceipt` 和 Swift 不得把 generation `0` 解释为已配置。

## 6. Provider 控制器与并发协议

### 6.1 新包

新增：

- `internal/providercontrol/types.go`
- `internal/providercontrol/controller.go`
- `internal/providercontrol/discovery.go`
- `internal/providercontrol/controller_test.go`
- `internal/providercontrol/discovery_test.go`
- `internal/app/provider_control_runtime.go`
- `internal/app/provider_control_runtime_test.go`

主要公开接口：

```go
type StateReader interface {
    ProviderState(provider string) (Snapshot, error)
    EnabledProviders() []string
}

type Controller struct { /* private state */ }

func (c *Controller) Snapshot(provider string) (Snapshot, error)
func (c *Controller) RefreshDiscovery(ctx context.Context, trigger string) ([]Snapshot, error)
func (c *Controller) Begin(ctx context.Context, provider string) (Operation, error)
func (c *Controller) Apply(ctx context.Context, preferences.ProviderPreferences) (TransitionResult, error)

type Operation interface {
    Context() context.Context
    Generation() uint64
    BeginCommit() (finish func(), err error)
    Finish()
}
```

约束：

- `Begin` 只在 effective enabled 时成功，并返回与 Provider generation 绑定的 context。
- `BeginCommit` 必须再次校验 generation，并持有短 commit lease 到 Store commit、缓存替换和 invalidation 排队完成。
- disable 在同一锁域内关闭 admission、推进 generation、取消根 context；先等 commit lease，再等普通 operation。
- Controller 不持有数据库 transaction，不在持锁状态调用 collector、网络、preferences CAS 或 invalidation，避免反向锁顺序。
- `EnabledProviders` 始终按 Codex、Cursor、Grok 顺序返回。
- 非法 Provider fail closed；公开错误不得包含路径、URL、credential 或底层错误。

### 6.2 复用既有语义

- Codex quota generation admission/drain 的现有模式作为实现参考，不复制另一套互相冲突的调度状态机。
- `providerrefresh.StatusSkippedDisabled` 和 `ReasonDisabled` 继续使用。
- `ApplicationPreferencesPostCommitError` 继续表达“preferences 已提交、runtime reconcile 未闭合”。

## 7. 公共控制面与 Codex worker 解耦

修改：

- `internal/app/default_home.go`
- `internal/app/app.go`
- `internal/app/binding_composition.go`
- `internal/app/application_controls.go`
- `internal/app/lifecycle_runtime.go`
- `internal/app/lifecycle_events.go`
- `internal/app/lifecycle_provider_refresh.go`
- 相关 app tests

实施结构：

1. `openNormalRuntime` 在 preferences 初始化后创建常驻 Provider Controller。
2. `composeCoreGraph` 注入该 Controller，先装配 Cursor/Grok、router、summary、settings 和全局刷新。
3. 新增常驻 `applicationControlRuntime`，实现 Core 的 `RuntimeControls`、Provider 状态 readback、全局 lifecycle/discovery 和 optional Codex delegate。
4. `applicationLifecycleRuntime` 收敛为 Codex Home worker：Home identity、light/deep index、quota/reset、App Server、Codex account 和 scheduler。
5. Codex effective enabled 且 Home available 时创建/恢复 delegate；disabled/unavailable 时 delegate 不启动或被 drain 后释放。
6. Core dependencies 始终绑定 `applicationControlRuntime`，不再以 Codex worker 是否存在决定 Settings/Provider query 是否可用。
7. `Runtime.NotifyLifecycle` 始终可用：先刷新 discovery/Provider transitions，再按 effective state调用 Codex delegate和 enabled Provider global refresh。
8. shutdown 顺序固定为：停止 lifecycle admission → Provider controller seal/drain → optional Codex worker → sampling/invalidation/retention/health → metrics/credentials/SQLite。

Codex 重新启用时只恢复既有 Home generation、checkpoint、last-good 和调度；没有 Home 时保持 `enabled + unavailable`，不得自动创建虚假 Home 或清库。

## 8. 覆盖所有业务入口

### 8.1 全局刷新与生命周期

修改 `internal/providerrefresh/orchestrator.go`、`adapters.go`、tests：

- Orchestrator 注入 `providercontrol.StateReader`。
- `refreshOne` 在调用 adapter 前申请 Provider operation；disabled 返回全 components `skipped_disabled`，unavailable 返回 `skipped_unavailable`。
- startup、scheduled、wake、foreground、manual 都走同一入口。
- overlapping refresh 的 single-flight 仍保留；disable 时旧 flight 由 operation context 取消并受 commit fence 约束。

### 8.2 Cursor/Grok 本地与在线采集

修改：

- `internal/cursorprovider/collector.go`
- `internal/cursorprovider/query.go`
- Cursor Dashboard/Grok Bot collector 组合处及 tests
- `internal/grokprovider/collector.go`
- `internal/grokprovider/query.go`
- Grok billing/account/auth 组合处及 tests

要求：

- QueryService 不再自行创建脱离 Provider 生命周期的裸 `context.Background()`；后台任务从 Controller operation context 派生 timeout。
- 首次无 snapshot 的同步采集也必须先取得 operation。
- 本地 snapshot replace、Dashboard/Billing last-good commit、query cache replace 和 invalidation 都在相同 operation generation 下发布。
- Cursor online 子开关关闭时，Dashboard 与 Grok Bot 都返回 disabled；Cursor local 仍可运行。
- Grok master 关闭时 account/profile、billing 和 auth refresh 均不能读取凭据或访问网络；只关闭 auto-refresh 子开关时保留既有硬有效期 token 行为。
- Provider disable 不删除 repository 中已有 snapshot，也不重置 collector 的最小刷新时间和 checkpoint。

### 8.3 Codex

- Codex master 关闭时停止 light index trigger、deep index、bootstrap/reconcile、quota/reset cron、manual refresh、App Server account read 和 subscription automatic profile discovery。
- 已有 Home、binding、subscription records、quota observations、light/deep facts 和 schedule 审计全部保留。
- Codex Home switch/configuration仍可在 Settings 显式执行；master disabled 时确认新 Home 后不启动 worker，直到再次开启。
- 重新启用复用已提交 Home generation，继续现有 incremental/reconcile 规则；不得无条件 full rebuild。

### 8.4 Query、account、pricing 与 aggregation

修改：

- `internal/query/errors.go`
- `internal/query/agentrouter/service.go`
- `internal/query/dashboardsummary/service.go`
- `internal/query/runtimeinfo/settings.go` 与 `types.go`
- `internal/core/service.go`、`runtime_controls.go`、codec/tests
- `internal/helper/business_rpc.go`、server/tests

要求：

- Usage、Sessions、Projects、Invocation、Quota current/pace、Quota refresh、AccountSnapshot、deep index 都在路由层统一检查 Provider 状态。
- 新错误分类 `provider_disabled` 映射为稳定 `query.error.providerDisabled`、`retryable=false`；unavailable 保持现有语义。
- Pricing catalog 是静态参考数据，可继续读取；UI 不对 disabled Provider 发起该请求。
- DashboardSummary 构造时注入 enabled-provider reader；cache key 加入 Provider Controller generation，并在 settings/provider transition invalidation 时清除旧 cache。
- Sources/Jobs 保留历史行，但 Provider 主状态进入 Settings read model；不得把 disabled 的 source health 写成 fresh/running。

## 9. Core contract 与生成代码

修改 `api/codexpulse/core/v1/core.proto`：

```proto
enum ProviderIntent {
  PROVIDER_INTENT_UNSPECIFIED = 0;
  PROVIDER_INTENT_AUTO = 1;
  PROVIDER_INTENT_ENABLED = 2;
  PROVIDER_INTENT_DISABLED = 3;
}

enum ProviderDiscoveryState {
  PROVIDER_DISCOVERY_STATE_UNSPECIFIED = 0;
  PROVIDER_DISCOVERY_STATE_UNCHECKED = 1;
  PROVIDER_DISCOVERY_STATE_AVAILABLE = 2;
  PROVIDER_DISCOVERY_STATE_MISSING = 3;
  PROVIDER_DISCOVERY_STATE_INACCESSIBLE = 4;
  PROVIDER_DISCOVERY_STATE_INVALID = 5;
}

enum ProviderEffectiveState {
  PROVIDER_EFFECTIVE_STATE_UNSPECIFIED = 0;
  PROVIDER_EFFECTIVE_STATE_ENABLED = 1;
  PROVIDER_EFFECTIVE_STATE_DISABLED = 2;
  PROVIDER_EFFECTIVE_STATE_UNAVAILABLE = 3;
  PROVIDER_EFFECTIVE_STATE_DISABLING = 4;
}

message SettingsProviderSnapshot {
  string provider = 1;
  ProviderIntent intent = 2;
  ProviderDiscoveryState discovery_state = 3;
  ProviderEffectiveState effective_state = 4;
  string reason_code = 5;
  string generation = 6;
}

message SettingsProviderUpdate {
  string provider = 1;
  ProviderIntent intent = 2;
}
```

- `SettingsSnapshot.providers` 为 repeated field，Helper 必须精确返回 Codex、Cursor、Grok 各一次并保持固定顺序。
- `UpdateSettingsRequest.providers` 必须精确包含三家各一次；缺失、重复、未知 Provider 或 unspecified intent 返回 validation。
- `SettingsOnlineSnapshot/Update` 增加 `cursor_online_enabled`。
- `ContractsResponse` 增加 `provider_control_version = provider-control-v1`。
- 精确握手升级到 `core-rpc-v5`；Go/Swift tests 同步拒绝 v4/v5 混用。
- 不新增 invalidation domain，也不提升 `query-invalidation-v3`：intent、discovery 或 effective 状态变化统一发布既有 `settings` invalidation；影响业务事实时再同时发布既有 `index`、`quota` 或 `account` domain。Swift 收到任何 `settings` invalidation 都必须先重读 Provider catalog，即使当前不在 Settings 页面。
- 生成命令固定为：

```bash
make generate-proto
bash scripts/proto/generate-swift.sh --write
```

不得手改 `core.pb.go`、`core_grpc.pb.go`、`core.pb.swift` 或 `core.grpc.swift`。

## 10. Settings mutation 和 readback 顺序

`applicationControlRuntime.UpdateSettings` 固定执行：

1. 校验 request 的 revision、三家完整 intent、online/refresh/update/UI 字段。
2. 读取权威 preferences，保留只读字段。
3. 通过 `Preferences.Service.UpdateSettings` 完成一次完整 snapshot CAS。
4. CAS 失败或冲突时不改变 runtime state，返回权威 readback语义。
5. CAS 成功后先对变为 disabled/unavailable 的 Provider 关闭 admission、推进 generation、cancel/drain；再对变为 enabled 的 Provider 做 discovery 和增量启动。
6. Reconcile 闭合后发布 settings、对应 provider facts、quota/account/index 和 dashboard invalidation。
7. 全部闭合返回 `applied`；preferences 已提交但 runtime 未闭合返回 `applied_reconcile_required`。
8. Swift 对两个 receipt 都执行 `Settings` authoritative readback；只有 stable state 与 intent 一致时显示成功。

不能在持有 Preferences Service mutex 时等待 Provider drain，避免与 collector/Store/Home switch 形成锁环。

## 11. Swift 启动、状态和 UI

### 11.1 Model 与 runtime

修改：

- `app/macos/Sources/CodexPulseAppSupport/FeatureModels.swift`
- `FeatureRequests.swift`
- `AppRuntime.swift`
- `AppModel.swift`
- `OverviewModels.swift` 中与 Provider 空态相关的 presentation
- Core client contract/tests

要求：

- `SettingsDraft` 增加三家 intent 和 Cursor online 子开关；完整 request 保持三家精确一次。
- `AppRuntime.start()` 在 bootstrap normal 后、任何 Overview/Dashboard/Account 请求前先调用 `Settings`，构建 `ProviderCatalog`。
- AppRuntime 持有当前 catalog generation，并通过独立 sink 向 AppModel 发布 Provider states 和 effective selection。
- `selectedProvider`、`statusProvider` 改为 optional；picker、页面请求和缓存只能消费 effective enabled 值。
- 启动时优先保留 UserDefaults 中仍 enabled 的选择，否则按 Codex、Cursor、Grok 选择第一项并持久化 fallback。
- 当前选择被显式关闭或 discovery 转为 unavailable 时取消对应 Swift tasks、推进 request generation、清理可见 state/cache，再切换到第一项 enabled Provider。
- 全部关闭时删除当前 active selection，停止 Provider 请求；重新出现第一个 enabled Provider 时按固定顺序选中，但不自动跳回更早的旧选择。
- DashboardSummary 仍可在无单一 Provider 选择时显示已知空态；Settings、API subscriptions 和 system 页面始终可达。
- provider/settings invalidation 先重读 catalog，再决定是否刷新当前页面，不能先按旧选择发请求。

### 11.2 Settings 与导航

修改：

- `app/macos/Sources/CodexPulseApp/SourcesJobsSettingsViews.swift`
- `RootView.swift`
- `StatusItemController.swift`
- Provider 相关业务页面对 optional selection 的入口
- `Resources/zh-Hans.lproj/Localizable.strings`
- `Resources/en.lproj/Localizable.strings`
- `app/macos/Tests/CodexPulseAppTests/main.swift`

设置页按 Provider 分为三组：

- 标题、主开关、自动发现/已发现/未发现/无权限/已停用/正在停止状态。
- Codex 组内展示额度和 Reset Credits 子开关。
- Cursor 组内展示在线数据采集子开关。
- Grok 组内展示额度和凭据自动续期子开关。
- unavailable 不等于用户关闭；显式开启但源缺失时显示“已开启，等待客户端数据源”。
- 原因文案只消费 reason code，不显示路径或底层错误。

主窗口与 Popover：

- picker 数据源改为 `enabledProviders`，不直接使用 `AgentProvider.allCases`。
- 全部关闭时隐藏 Provider 专属导航/刷新动作，展示“尚未启用客户端”和“打开设置”。
- Settings 入口、Command-Comma 和 App menu 继续可用。
- 状态栏无 Provider 时显示 `Codex Pulse --`，不得沿用最后一个 Provider 的额度、账号或 Token。
- Privacy capture/clipboard 规则不因新状态文案而泄露路径、账号或配置细节。

## 12. 文档与测试材料

修改：

- `README.md`
- `README_CN.md`
- `docs/design/details/providers/README.md`
- `docs/design/details/scheduling-and-bootstrap/README.md`
- `docs/design/details/native-macos-client/README.md`
- `docs/design/details/product/README.md`
- `docs/design/details/README.md` 或既有索引中必要链接
- 新增 `docs/test/provider-controls.md`

必须说明：三层状态、migration 默认、metadata-only 边界、关闭/恢复、子开关、全关闭空态、历史保留、查询错误、`core-rpc-v5` 和证据分层。提交版 runbook 不能包含真实 Home 路径、邮箱、原始 ID、Session/JSONL、token、Cookie、Authorization 或完整日志。

## 13. 可独立审查的实施任务

### Task 1：Preferences v3

- 先写 migration/strict-shape/CAS/无 Home 初始化测试。
- 实现 v3 类型、v1/v2 迁移、InitializePreferences 和 nil Home 验证。
- 保持现有 Home switch、durability、跨实例锁测试通过。

完成条件：无 Home 也能得到 revision=1 的合法 v3 snapshot；v2 精确迁移；显式 intent round-trip 后重启不丢失。

### Task 2：Provider Controller

- 先写 intent/discovery/effective 矩阵、admission、cancel/drain、generation/commit fence 并发测试。
- 实现 Controller 和三家 metadata-only probes。
- 使用 spies 证明 discovery 不调用 collector/network/credential/session readers。

完成条件：旧 generation 无法在 stable disabled 后 commit 或发布 invalidation。

### Task 3：公共控制面解耦

- 让 app graph 在 `CodexHome=nil` 时启动 Settings、Cursor/Grok、health 和 CoreService。
- 引入 applicationControlRuntime，optional 组合 Codex worker。
- 覆盖 startup、wake、foreground、shutdown 和首次配置 Home。

完成条件：synthetic 无 Codex Home 启动不报 recovery/unavailable；Cursor/Grok 与 Settings 可用；Codex 操作明确 unavailable。

### Task 4：入口 gate 和 writer fence

- 接入 global refresh、Cursor/Grok query-triggered background refresh、online collectors、auth/account、Codex worker。
- 消除脱离 Provider operation 的后台刷新 context。
- 增加阻塞 collector → disable → release 的晚到提交测试。

完成条件：每个 Issue 列出的启动、定时、唤醒、前台、手动、查询和汇总入口都有自动化断言。

### Task 5：Query、Summary 与错误合同

- Router/Quota/Account/deep index gate。
- Summary 动态 enabled set、cache generation 和 all-disabled known-empty。
- `provider_disabled` 跨 Core/Helper 映射。

完成条件：disabled 历史不进入当前 totals/freshness，其他 Provider 不受影响。

### Task 6：Proto v5

- 修改 proto、domain/Core/Helper codec 与 strict validation。
- 生成 Go/Swift clients。
- 更新 handshake/Contracts 和 drift tests。

完成条件：v5 双端通过，v4/v5 精确不兼容；DTO 不含路径、credential 或底层错误。

### Task 7：Swift catalog、选择和 Settings UI

- 先处理 AppRuntime 启动顺序与 optional selection。
- 再处理 picker、页面 guard、task/cache cancellation、全关闭空态。
- 最后按 Provider 分组 Settings 和本地化。

完成条件：启动不会先查询 disabled Codex；主窗口/Popover 独立选择均只包含 enabled Provider；全关闭仍可进入 Settings。

### Task 8：文档和证据

- 同步设计与 README。
- 新增脱敏 runbook。
- 运行第 15 节聚焦验证并按层记录实际结果。

## 14. Acceptance Criteria

- [ ] 三家 intent 独立持久化，v1/v2 迁移保持原行为且默认 `auto`。
- [ ] 无 Codex Home 时 App 仍可启动并使用 Cursor/Grok/Settings。
- [ ] discovery 只做 metadata-only 探测，不读取正文、凭据或网络。
- [ ] 显式关闭在 restart/wake/foreground/rediscovery/upgrade 后保持。
- [ ] master disabled 覆盖所有本地、在线、账号、凭据和查询触发入口，子开关值保留。
- [ ] stable disabled 后，旧 operation 无 Store/cache/invalidation 发布。
- [ ] Cursor online 子开关只影响 Dashboard/Grok Bot 在线能力，不影响本地 snapshot。
- [ ] direct disabled query 返回 `provider_disabled`；unavailable 与 disabled 可区分。
- [ ] DashboardSummary 只汇总 enabled Provider；all-disabled 返回 complete known-empty。
- [ ] global refresh 保留三家稳定回执，disabled 为 `skipped_disabled`。
- [ ] disabled 不删除历史/进度；re-enable 继续增量和节流，不全量重置。
- [ ] 主窗口/Popover 只列 enabled Provider，选择失效时稳定 fallback，全关闭时明确空态且 Settings 可达。
- [ ] `core-rpc-v5`、`provider-control-v1`、Go/Swift generated clients 无 drift。
- [ ] 设置 CAS、post-commit reconcile、conflict 和 authoritative readback 语义保持。
- [ ] 文档、双语文案和脱敏 runbook 同步。

## 15. 验证矩阵与命令

### 15.1 聚焦 Go

```bash
go test ./internal/preferences ./internal/providercontrol -count=1
go test ./internal/providerrefresh ./internal/query/agentrouter ./internal/query/dashboardsummary -count=1
go test ./internal/cursorprovider ./internal/grokprovider -count=1
go test ./internal/app -run 'Test.*(Provider|Settings|DefaultCodexHome|Lifecycle|QuotaRuntime|ApplicationControls)' -count=1
go test ./api/codexpulse/core/v1 ./internal/core ./internal/helper \
  -run 'Test.*(Provider|Settings|Contract|Proto|DashboardSummary|QuotaRefresh|AccountSnapshot)' -count=1
```

### 15.2 Proto 与 Swift

```bash
bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check
swift run --package-path app/macos codex-pulse-core-client-tests
swift run --package-path app/macos codex-pulse-app-tests
swift build --package-path app/macos --product codex-pulse-app
```

### 15.3 产品检查与证据边界

- 聚焦测试全部通过后可运行 `make check`，记录为本地产品检查；它不等于 CI。
- 默认不运行 `make verify`、`go test -race ./...` 或其它长测；只有用户明确要求或 CI 执行时才运行。
- 本计划实施阶段不自动运行 `make verify-live`。真实 Home 验收是单独 gate，运行前必须说明：会只读 Session/JSONL 和 Cursor/Grok 本地来源，并可能写私有 runtime、SQLite、preferences、日志和标准 App Server housekeeping。
- CI、真实 Home、人工 UI、签名/公证、发布必须分别标为 `PASS/FAIL/ERROR/NOT_RUN/INCOMPLETE`，不得相互替代。

### 15.4 必须覆盖的并发场景

| 场景 | 预期 |
| --- | --- |
| collector 已进入但未 commit，随后 disable | 旧 generation commit lease 失败，无新 snapshot/invalidation |
| commit lease 已持有，随后 disable | `applied` 等待 lease 退出；未排空时只返回 reconcile required |
| Settings CAS conflict | runtime intent 不变，Swift 保留 draft 并 readback |
| auto source 消失 | effective unavailable，停止 admission，intent 仍 auto |
| auto source重新出现 | 增量恢复，不修改 intent，不清历史 |
| explicit disabled source重新出现 | 仍 disabled，只更新 discovery 展示 |
| 关闭当前主窗口/Popover Provider | 两套选择独立 fallback，旧任务响应丢弃 |
| 三家全关闭 | 无 Provider 请求；Summary known-empty；Settings 可达 |
| 关闭一家、另两家刷新 | 另两家状态、generation 和 totals 不受污染 |

## 16. 权限边界

实施授权仅覆盖本计划定义的业务代码、生成代码、测试和文档，以及第 15 节聚焦验证。

未授权：

- 重置、stash、覆盖或删除用户工作区改动。
- 新建 worktree 或使用 `superpowers` worktree。
- commit、push、PR、merge、tag、release、部署或 Linear 回写。
- 真实 Home/live E2E、签名、公证或发布。
- 为通过测试而删除历史、放松隐私校验、吞错、添加无依据重试或把 unavailable 伪装成成功。

## 17. 停止条件

出现以下任一情况时停止实施并报告，不自行扩大范围：

- 工作区出现无法安全归属的既有改动，或当前分支/基线与本计划不一致。
- Linear 的 Goal/Scope/Acceptance Criteria 被更新并与本计划冻结决定冲突。
- 无法让无 Codex Home 的公共控制面启动，且替代方案仍要求隐藏的 Codex Home 前置条件。
- 无法证明 disable 后的 operation/commit generation fence，或需要让旧任务在 stable disabled 后仍可写入。
- preferences migration、Home switch recovery 或 CAS readback 无法保持 fail closed。
- Provider discovery 需要读取 Session 正文、credential 或发网络请求才能判定 master availability。
- Proto/日志/文档/测试产物出现真实路径、账号、token、Cookie、Authorization、原始 Session/JSONL 或底层错误正文。
- 需要 application SQLite schema v34；本卡设计不需要新业务表，若实现证明必须升级，应先回到设计审查。

## 18. 最终回读与报告格式

实施结束必须回读：

```bash
pwd
git branch --show-current
git status --short --branch
git diff --stat
git diff --check
git diff -- api/codexpulse/core/v1/core.proto internal/preferences app/macos/Sources
```

最终报告按以下层级分别列出实际状态和证据：

1. 实现文件与冻结语义。
2. Preferences migration 与无 Home 启动。
3. Provider gate、drain、generation/late-writer 证据。
4. Query/Summary/Core/Helper contract。
5. Swift Settings、选择与全关闭空态。
6. 聚焦 Go、Proto drift、Swift tests/build、`make check`。
7. CI、真实 Home、人工 UI、签名、公证、发布、Linear 回写。
8. 当前 branch、HEAD、工作区变更和未解决事项。

任何未实际执行的层级明确写 `NOT_RUN`；失败或无法判定分别写 `FAIL`、`ERROR` 或 `INCOMPLETE`，不能用“看起来正常”替代证据。
