# Quota Trust Model

## 问题

Codex Runway 实际运行中观察到：网络不佳时，5 小时和周窗口可能显示成剩余 100%，容易把未知状态误认为额度已经重置。Codex Pulse 不允许失败响应、默认值或可疑降级覆盖最后可信配额。

数据库统一保存来源原始语义 `used_percent`；UI 的 `remaining_percent = 100 - used_percent` 只在展示层计算。每条 observation 必须携带来源、观测时间、窗口长度、reset 时间、有效性和 request。

## v0.1 来源

- `local_jsonl`：始终启用，只读 `event_msg.token_count.rate_limits`。无额外网络行为，但只在 Codex 产生新活动时更新。
- `wham`：v0.1 默认开启，请求 `https://chatgpt.com/backend-api/wham/usage`，用户可随时关闭。更实时，但可能受到网络、认证和内部接口变化影响。

不启动或复用 `codex app-server`，也不作为兜底。v0.1 只支持当前单账号，固定 `account_scope = default`；运行期间切换 Codex 账号不属于支持场景。

在线 quota 或 reset credits 启用时，只从 Preferences 当前 confirmed Codex Home 下的固定 `auth.json` 将 access token 读入调用期内存。应用不保存 token、refresh token、Authorization header 或 auth 文件内容；不使用 refresh token 主动刷新，也不修改 `auth.json`。401/403 后标记 `auth_required` 并停止自动重试，等待 Codex 恢复登录或用户手动重试。用户关闭对应能力后立即停止该能力的在线调度；已有非敏感 observation history 保留。

## Observation

两个来源先归一化为：

```text
account_scope
source                  local_jsonl / wham
limit_id
window_kind             primary / secondary / additional:{limit_id}
used_percent
window_minutes
resets_at_ms
plan_type
observed_at_ms
validity                accepted / suspicious / rejected
rejection_reason
```

### 本地 JSONL 观测边界（TOO-262）

TOO-262 只交付 `local_jsonl` 的结构化观测与持久化，不提前实现 `wham` 请求、跨来源仲裁、`quota_current`、刷新调度或 UI。`event_msg.token_count.info` 与 `rate_limits` 是两个彼此独立的输入：`info=null` 仍可生成配额 observation，`rate_limits` 缺失也不影响既有 token usage。解析器只消费：

- snapshot：`limit_id`、`primary`、`secondary`、`plan_type`；
- window：`used_percent`、`window_minutes`、`resets_at`；
- envelope：记录时间与 JSONL 行的 source offset。

`limit_name`、credits、future fields 和其它未知字段全部忽略，不进入 parser event、diagnostic、checkpoint 或数据库。`remaining_percent` 不持久化，也不从 token 用量猜测；它只能由后续展示层从 `100 - used_percent` 派生。

本地窗口逐项处理：

- `used_percent` 必须在 `0..100`，`window_minutes` 必须在 `1..525600`，`resets_at` 必须是可安全转换为 epoch milliseconds 的非负 Unix seconds；`0` 表示 Unix epoch，可保留为 `suspicious/reset_not_future`，负数或溢出才是非法 window；
- primary / secondary 独立解析。一个 window 非法时只产生 content-free `invalid_quota_window` diagnostic，另一个合法 window 仍可落 observation；两个 window 均缺失或不可用时产生 `invalid_quota_snapshot`，不写伪造 observation；
- 缺失 `limit_id`、secondary 缺失合法 primary、未知字符串枚举 `plan_type`、或 reset 不晚于观测时间时保留结构化值，但标为 `suspicious`，reason 分别固定为 `missing_limit_id`、`missing_primary_window`、`unknown_plan_type`、`reset_not_future`；未知 plan 字符串只归一为 `unknown`，不保留原字符串；对象、数组、数字、布尔值或超长字符串 plan 属于结构漂移，只产生 `invalid_quota_snapshot`；
- schema/type/range 不合法时 fail closed：只记录固定 diagnostic，不写 `used_percent=0`，也不沿用默认值冒充新观测。

Indexer 使用 `(source_file_id, session_id, source_generation, line_start_offset, window_kind)` 生成稳定、不含路径或内容的 SHA-256 observation ID；物理文件替换即使从 generation 0 和相同行位置重新开始，也不会复用旧 ID。parser events、diagnostics、完整 checkpoint、quota facts 与 committed offset 进入现有同一个 `CommitIngestBatch` writer transaction；ingest 还会校验 observation 的 `source_file_id` 与 batch source 一致，事务失败不得推进 offset。application schema v9 以 `STRICT` `quota_observations` 表保存 physical source、first/last observed time、sample count、first/current generation 与 offset，并以 `quota_observation_receipts` 保存每个原始 sample ID、所属 segment 和 content-free SHA-256；普通访问均使用 GORM typed CRUD/query，raw SQL 只用于 `STRICT/CHECK/index` migration 与 schema 验证。

同一 physical source、Session、generation、limit、window、值、reset、plan、validity/reason 的后续连续 sample 只推进末次时间/offset 与 `sample_count`；语义变化、新 generation 或新 physical source 创建新 segment。每个被折叠 sample 仍写入 replay receipt，因此经过 `A→B→A` 分段后重放旧 A sample 也是可验证的 no-op；同 ID 不同 digest、位置倒退、同位置冲突、时间倒退或跨 source/generation/checkpoint 的 ingest fact 整批失败。Session aggregate 因 rebuild 删除时，历史 observation 的 `session_id` 通过 `ON DELETE SET NULL` 保留，durable `source_file_id` 与 receipt 继续提供审计 lineage；不删除 observation history。

可复用的 synthetic-only 验证入口见 [`docs/test/local-jsonl-quota.md`](../../../test/local-jsonl-quota.md)。

### 在线 Wham 观测边界（TOO-263）

TOO-263 交付 `CredentialProvider -> Wham client -> validated observation / typed failure -> atomic recorder`，但不读取真实 `auth.json`，不实现 refresh token、app-server、周期调度、窗口仲裁、`quota_current`、Reset Credits 或 UI。调用方只能把当前 access token 注入 `MemoryCredentialProvider`；provider 在 callback 期间提供独立副本，并在 callback、Replace 或 Close 后清零可写 buffer。客户端只向固定 Wham HTTPS endpoint 发 GET，请求完成后删除临时 Authorization header；token、header、response body 和底层 error text 都不得进入 Result、SQLite、日志或文档。

每次 HTTP attempt 使用独立 timeout context，response body 有硬上限并始终关闭。401/403、429 和 schema failure 不做请求内重试；网络、timeout 与 5xx 使用 `internal/retry.Policy` 做最多三次短退避。429 不占用调用 goroutine 等待服务端窗口，只把合法 `Retry-After` 秒值、HTTP-date 或 `X-RateLimit-Reset` 安全转换为 `retry_at_ms`，供后续 durable scheduler 使用。取消优先于网络错误；在请求前已经取消或缺少凭证时 `attempt_count = 0`，仍记录一条无内容的 typed attempt。

Wham decoder 先在有界内存中递归拒绝任意层级 duplicate JSON key，再读取 `plan_type` 与 `rate_limit.primary_window/secondary_window`。未知附加字段忽略；未知但类型正确的未来 plan 归一为 `unknown + suspicious/unknown_plan_type`，不会误报 schema failure或生成零值。primary 必须合法，secondary 可以缺失；secondary 损坏时保留合法 primary 并同时返回 `schema_incompatible`，primary 缺失但 secondary 合法时保留 secondary 为 `suspicious/missing_primary_window`。window 字段继续遵守 `used_percent=0..100`、最大 525600 分钟、epoch overflow 和 reset 可信度规则。

application schema v10 通过 GORM Migrator 为 `source_state` 和 `source_attempts` 追加 exact failure code、attempt count、response byte count 与 retry time；v1～v9 的 schema/checksum 保持冻结。`RecordQuotaFetch` 在现有单 writer transaction 中同时写 observation、append-only attempt 和 source freshness：成功清空失败状态；partial/failure 保留 last-known-good 并进入 stale/unavailable；cancelled 不累计连续失败；较旧请求晚到只补历史，不回退较新的 source state。exact request replay 是 no-op，同 request 不同结构整笔 fail closed。

可复用的 synthetic-only 验证入口见 [`docs/test/wham.md`](../../../test/wham.md)。

### 窗口代际、校验与可信仲裁（TOO-264）

窗口 identity 使用 `(window_kind, limit_id, window_minutes, resets_at_ms)`；逻辑 current key 使用 `(account_scope, window_kind, limit_id)`。通过校验的 `resets_at_ms` 同时作为稳定 `window_generation`：projection 从完整 observation history 重算时不会因输入顺序或补入旧历史而重编号。同一代际内同一来源的 used 只能保持或上升；进入可解释的新代际后才允许从低值重新开始。

application schema v11 新增 `quota_current` 与 `quota_arbitration_evidence`。`quota_observations` 继续是不可变来源事实，parser 的 `validity/rejection_reason` 不会被 arbiter 改写；连续性、时钟、旧代际、来源冲突等派生判断写入 evidence。Local ingest、Wham fetch，以及对 exact quota Wham state 的通用 `UpsertSourceState`，都会在同一个 GORM writer transaction 内重算受影响窗口，写后完整读回 current/evidence；任一步失败连同 observation/attempt/source state 一起 rollback。Local ingest、`RecordQuotaFetch`、exact quota Wham state 更新和 maintenance rebuild 都使用 repository 单次注入的可信 wall clock，v10→v11 migration 使用 migration runner 同一次注入的应用时钟；observation/attempt 时间只作为待校验事实，不能自行抬高或降低 `evaluated_at_ms`，也不能绕过 future-clock 校验。migration backfill 与 evidence 写入按固定安全批次执行，4096 条以上 history 不会越过 SQLite bind-variable 上限；无 observation 时保持空表，原 observation 与 receipt 不删。

逐 window 校验：

- `used_percent` 必须在 `0..100`；
- window duration 和 reset 时间必须合理；
- primary 必须存在，字段类型和 observation 时间不能明显倒退；默认允许的系统时钟偏差为 2 分钟，规则版本为 `quota-arbiter-v1`；
- secondary 暂时缺失不能删除上一条 weekly；
- partial response 只更新通过校验的 window。

校验结果：

- `accepted`：允许参与 current 选择；
- `suspicious`：结构可解析，但违反窗口连续性或与可信历史冲突；
- `rejected`：字段缺失、类型错误或范围非法。

## Current 状态

每个窗口独立维护：

```text
never_loaded
    -> fresh                 成功取得并通过校验
    -> stale                 刷新失败，但最后可信窗口尚未 reset
    -> expired_unknown       已越过 reset，仍未确认新窗口
    -> suspicious            新响应违反连续性，隔离新值
    -> fresh                 确认新窗口或取得可信观测
```

刷新失败只写 `source_attempts`，不新增伪造 observation；随后从原 observation 和 source state 重算 current。选中 Wham 的窗口在较新请求失败后进入 stale 并保留 last-known-good；Local 仍提供更新可信值时不被 Wham 故障连带降级。一个窗口未知或 partial 缺失不能把其他窗口一起清空。

freshness 与 conflict 分开：current 可以同时是 `fresh + conflict` 或 `stale + conflict`。accepted observation 在 10 分钟内且 reset 未到是 fresh；超过 10 分钟但仍在同一窗口是 stale；reset 已过且没有可信新代际是 expired_unknown；较新异常 observation 被隔离时保留 last-known-good 并标记 suspicious；从未取得 accepted observation 是 never_loaded。typed reader 每次按调用方提供的 evaluation time、`fresh_until_ms` 与 `resets_at_ms` 只做单向降级，因此即使没有周期写入也不会让落盘的 fresh 永久有效。读回会在显式 GORM read transaction 的同一 SQLite snapshot 中加载该逻辑窗口的完整 raw candidates 与 Wham source state，按 stored rule/evaluation 重新仲裁，再逐字段对账 current 与完整 evidence 集合；并发 writer commit 只能让 reader 看到完整旧版或新版。组合自洽的 freshness/explanation 或 disposition/reason/explanation 篡改、evidence 缺行/多行、逻辑键、来源、used、duration、reset、generation、validity 任一漂移都 fail closed，不能作为官方 current/evidence 返回。

## 仲裁规则

不采用“wham 永远覆盖本地”的固定优先级：

1. 同一代际内，按来源分别检查 used 单调性；相同或上升才参与 current，下降写 `suspicious/used_regression` evidence，不覆盖 current。
2. 同代际有多个 accepted 来源时，取最大的 `used_percent`，即采用最保守的 remaining。
3. reset 向后推进、observation 已越过上一 reset（允许 2 分钟 clock skew）、新 reset 晚于 observation 且不超过 `window_minutes + skew` 时接受为新 generation，允许 used 重新从低值开始；中间未观测到的窗口可以跳过。generation 先做基础有效性分类，再做代际排序；若新代际零值被更晚 Local 旧窗口否定，会隔离该零值并重新分类旧窗口候选，确保首次观测也不会暴露 false-zero，已有历史时选更新的 Local last-known-good 而不是更旧值。
4. reset 向过去移动、跨度异常或来源代际无法解释时保留 last-known-good；旧 reset 到期后进入 expired_unknown。
5. 本地 JSONL 到来只重新仲裁，不触发在线请求；较新的本地高值可以更新旧 wham，较新的 wham 低值不能覆盖同代际本地高值。旧 generation 晚到只保留 `reset_regression` evidence，不能回退 current。

例：同一 reset 下本地已用 45%、在线已用 41%，current 采用 45%，UI 显示“剩余最多 55%”；41% 保留为 conflict evidence。之后在线返回 47% 时，47% 成为 current。

## 100% 防误判

只有满足以下条件，才接受 remaining 100% 对应的 `used_percent = 0`：

- `resets_at` 已推进到可解释的新窗口；
- 当前时间越过上一窗口 reset，窗口长度合理且结构完整；
- 没有更晚的本地 rate-limit 快照与其冲突。

若新零值 generation 已通过基本时间校验，但随后出现仍指向上一 reset 的更新 Local snapshot，零值以 `default_fallback` evidence 隔离并回退上一代 last-known-good；不会用到达顺序把 100% 强行设为 current。

以下响应标为 suspicious，保留上一条 accepted observation：

- reset 未变化，但 used 从非零突然降为 0；
- 5h 和 weekly 同时归零，但代际都未推进；
- reset 为 0、window duration 缺失或异常；
- HTTP 成功但关键字段像默认 fallback；
- 不同来源在同一窗口明显冲突。

离线且仍在同一窗口时，旧值只能作为边界，例如“5h 剩余最多 62% · 15 分钟前 · 离线”；越过 reset 后显示“当前未知”，附上上次可信值和原 reset 时间，不能自动显示 100%。

## 调度和失败

- 在线 quota 与 reset credits 的新安装默认值均为开启；升级时尊重已有用户偏好，不反向覆盖已关闭状态。
- 正常在线刷新：5 分钟。
- remaining 不高于 20%，或距离 reset 不超过 10 分钟：2 分钟。
- 到达 reset：`reset + 3 秒`尝试一次。
- 手动刷新：立即请求，绕过退避但不绕过校验。
- 系统唤醒：只刷新 stale 来源。
- 网络失败：5、10、20、30 分钟带 jitter 退避。

失败分类：`network_unavailable`、`timeout`、`auth_required`、`http_429`、`server_error`、`schema_incompatible`、`cancelled`。

网络、timeout、5xx 保留 last-known-good；401/403 停止自动请求；429 使用 `Retry-After` 或退避；schema 不兼容时停止接受在线响应。本地来源继续可用，任何失败都不能生成 `used_percent = 0`。

### Reset Credits 与持久刷新计划（TOO-265）

Reset Credits inventory 来自独立的只读 `GET /backend-api/wham/rate-limit-reset-credits`，不能从 quota 百分比或 reset 时间猜测。客户端与 `wham/usage` 共用内存 credential lease、固定 HTTPS endpoint、redirect 禁止、逐 attempt timeout、response body 上限、duplicate JSON key 拒绝和 typed failure 分类；不调用 consume endpoint。响应只接受有界 `available_count + credits[]`，credit 的 `id` 进入 Store 前转成 SHA-256，`title`、`description`、`profile_user_id`、未知字段、token、header、body 和 raw error 全部丢弃。status 只接受 `available/redeemed/expired/used`，reset type 归一为 `codex_rate_limits/unknown`，时间必须是合法且自洽的 RFC3339；available count 与 items 不一致时整次响应按 `schema_incompatible` fail closed。

application schema v12 新增 `reset_credit_snapshots`、`reset_credits`、`source_refresh_schedules` 和 append-only `source_refresh_claims`。一次成功 Reset Credits 请求把 append-only source attempt、snapshot 与 hashed items 放在同一个 GORM writer transaction；失败/取消只写 attempt/source state，不生成 snapshot。exact request replay 是 no-op，同 request 不同事实拒绝；summary 在调用方 evaluation time 重新计算实际可用数、总数、已兑换数、所有未过期 available credit 的累计剩余毫秒和最近到期时间，因此未加载、真实 0 与已自然过期不会混淆。reader 在同一 SQLite read snapshot 内对账 item、server count 与 source attempt provenance，篡改或跨来源 request 引用 fail closed。已有 last-known-good 在网络失败后保留。

`CalculateQuotaResetSummary` 从所有 `quota_current` 窗口计算最近可信 reset、剩余毫秒和可信窗口数；只有带 selected observation 且 freshness 为 `fresh/stale`、reset 仍在未来的窗口参与。Reset Credits inventory 与 quota reset summary 是两个独立事实，后续 query/UI 只组合，不互相推导。

quota 与 reset credits 各有一行 durable refresh schedule，保存 `next_due_at_ms`、固定 reason、last manual time、claim lease、revision 和更新时间；每次领取同时追加一行 claim fence，完成标记 `completed`，确认没有 durable attempt 的过期 claim 标记 `abandoned`。cron 只扫描 due row；领取、完成和过期恢复都使用 Store CAS，陈旧 completion 不能覆盖新 revision。attempt 写入与 claim finalize 共用串行 writer：attempt 先提交时恢复按成功/失败事实计算正常周期或 Retry-After；release 先提交时，迟到 attempt 被 fence 拒绝，不能更新 source state 或越过服务端限流。正常 quota 使用 preference 周期；remaining 不高于 20% 或 reset 不超过 10 分钟时使用 2 分钟，若 `reset + 3 秒` 更早则精确选它。reset credits 使用独立 preference 周期。网络/timeout/5xx 跨请求按 5/10/20/30 分钟 capped exponential 加 jitter；429 取本地退避与合法 Retry-After 中更晚者；401/403 与 schema incompatible 的 next due 为空。manual 可以越过普通退避，但 Store 与 policy 双层保证 60 秒最小间隔，且不能越过未来 Retry-After；foreground 只在上次成功超过 60 秒时立即刷新，wake 只刷新 stale 来源。cancelled 不增加 failure count，coordinator 使用 detached bounded context 释放 claim 并写下一计划；durability unknown 则保留 claim，待 lease 到期后重新校验当前时钟与事实。

settings commit 调用 `ReconcilePreferences`：关闭能力立即把 next due 置空并保留历史，重新开启 never-loaded 来源会安排启动请求。进程启动与每个 cron cycle 都回收已经到期的遗留 claim；即使 claim 在启动检查之后才到期，也不会永久卡住。周期 trigger 固定复用 `github.com/robfig/cron/v3 v3.0.1` 的 `@every 1s`、`SkipIfStillRunning` 和 `Recover`，生产代码不新增 ticker/timer/sleep loop。可复用 synthetic-only 验证入口见 [`docs/test/reset-credits-quota.md`](../../../test/reset-credits-quota.md)。

### 生产应用装配与凭据边界（TOO-306）

`startApplicationLifecycleRuntime` 是在线来源的 production composition root。它与 bootstrap/live scheduler 共享同一个 GORM Pure-Go Repository 和 Preferences loader，构造调用期 credential provider、Quota/Reset Credits clients 与 recorders、`QuotaRefreshCoordinator` 和 `QuotaRefreshRunner`。真实 PreferencesStore 启动时先让 quota runtime 保持 suspended：runner 与 generation admission 都不开放，组合 HomeRuntime 先恢复 `pending_resume` / `pending_switch`，只在 rollback 或 finalize 得到无 journal 的权威 snapshot 后 Resume 最终 generation；bootstrap status unknown/error 保留 journal、停止启动且不发 HTTP。随后 runner 从 Store 恢复 durable claim/schedule 并执行一次 due cycle，再由 robfig cron 唤醒；进程重启不复制尚未到期的请求，也不从 cron 内存恢复业务状态。开关关闭时只把对应 next due 置空，既有 observation、attempt、last-known-good 和 Reset Credits history 不删除。

credential provider 不缓存 token。每次 lease 都重新读取当前 confirmed Home，从文件系统根目录逐段以 no-follow directory FD 打开 Home，核对保存的 device/inode，再以 no-follow 方式打开固定 `auth.json`；只接受 1 MiB 内的普通文件，并在读取前后同时核对打开 FD 和目录 entry 的 device、inode、mode、size、mtime、ctime。打开后读中 rename/replace 会因打开 FD 与当前目录 entry 不一致而 fail closed，确定性 barrier test 同时证明 callback 不执行且错误不含新旧 token marker。内容使用有深度上限的 duplicate-key 检查，所有 JSON value 保持为可清零的 byte/`RawMessage`；只复制 `tokens.access_token` 给一次 callback，未知字段和 refresh token 不进入 domain result。callback 前再次读回 Preferences 并核对 `CodexHome`，因此 Home 在读取窗口切换时 fail closed，不会把旧 Home token 租给新来源。文件内容、RawMessage 和 token lease 在返回前清零；错误统一归一为 content-free `credential unavailable` 或 context 取消，不携带路径、字段值或底层正文。

缺失、畸形或被替换的 credential 会形成 `auth_required` attempt 和空 next due，不阻止 local-only 应用启动。后续同一 confirmed Home 恢复有效 credential 后，manual refresh 可以重新进入既有 claim/policy；settings application contract 使用真实 `Preferences.Service` 先完成 CAS，再调用 quota reconcile。CAS 已提交而 reconcile 失败时返回包含 committed snapshot 的 typed post-commit error，不能把持久成功谎报为未提交。manual hook 只传来源给同一个 coordinator，继续受 Store 的 60 秒节流、Retry-After、revision 和 active claim fence 约束；M6 只负责 Wails binding，不重写这些语义。

Wails sleep/wake/foreground callback 仍只向内存 FIFO 追加枚举。后台 adapter 先处理本地 lifecycle，再对两个在线来源发 wake 或 foreground request，由 policy 判断是否 stale、是否处于 backoff、是否需要真正发起 HTTP。Home switch 的 `pending_resume` guard 持久化后，组合 HomeRuntime 先让 local lifecycle 写入 blocked/draining fence 并调用 scheduler `Drain(all)` 等待 active live/backfill slice，再封闭 quota generation admission、取消 cron/request、等待所有旧 token lease/recorder，最后 drain bootstrap generation；只有全部旧 writer 退出后 Preferences 才能提交 target generation。resolution 完成后 lifecycle `HomeChanged` CAS durable generation、保留用户 pause/system sleep 并 reconcile 新 Home，quota runner 最后对已提交 generation 幂等 Resume。旧 generation queued task保留审计但不再 runnable，新 generation task 才能进入选择。

settings 固定先在 application control admission 内完成 Preferences CAS、释放 Service mutex，再进入 quota reconcile admission；这样 Home Confirm 的 Service mutex -> generation drain 不会与 settings 的反向锁顺序形成环。generation Drain/Resume 使用可取消的串行 transition，Resume 同 generation 幂等；runner fatal 会同步 seal/cancel 当前 generation，使 fatal 前已登记操作也能退出并被 drain。

shutdown 先停止接收 lifecycle，并封闭 application settings/manual/Home-switch admission；已接纳控制操作退出后，再取消并等待 quota cron、generation admission 与在途 request。取消 attempt 和 claim completion 仍使用既有 bounded detached persistence。Quota 完全 drain 后才关闭 bootstrap scheduler/coordinator，最外层 `Run` 最后关闭 SQLite。可复用 synthetic app integration 入口见 [`docs/test/quota-runtime.md`](../../../test/quota-runtime.md)。

### Quota Current 只读查询合同（TOO-266）

`quota-current-v1` 是 M5 的只读 domain contract，不是 Wails binding 或 M6 通用查询 envelope。调用方提供 evaluation time，query 固定查询 `account_scope = default`，返回 `version/accountScope/evaluatedAtMs`、按 primary、secondary 稳定排序的 windows、固定顺序的 Local/Wham source summaries、nearest trusted reset、Reset Credits 动态汇总，以及 quota/reset-credits refresh status。`remainingPercent` 只从选中事实的 `100 - usedPercent` 派生；真实 `usedPercent = 0` 返回真实 `remainingPercent = 100`，从未加载才返回 null 和固定 `never_loaded` reason。过期、stale、suspicious 或 conflict 仍保留 last-known-good 普通百分比；只有 freshness 为 fresh/stale 且 reset 晚于 evaluation time 时才返回 `resetRemainingMs` 或参与 nearest reset，suspicious/expired reset 只保留事实时间，不暴露可信倒计时。

Repository 在一个显式 GORM read transaction、同一个 SQLite snapshot 内读取全部 `quota_current` logical keys、对应 raw observations、完整 arbitration evidence、Wham source state、两个 durable refresh schedule 与 Reset Credits summary。并发 writer 只能让整份 response 看到完整旧版本或完整新版本，不能混出跨表时序。observation/current/evidence logical key 集必须完全一致；projection 缺行、多行、identity/provenance/shape 漂移时 fail closed，query 返回可恢复的 projection unavailable，不在查询路径写库。恢复只能由已有的显式 maintenance `RebuildQuotaProjection` 执行，调用方不得把查询失败当作 rebuild 授权。

每个 window 同时返回选中事实和按 observation ID 稳定排序的 explanation；explanation 只包含结构化 source、used/remaining、window/reset/generation、observed time、validity、disposition、reason 与固定 explanation code。source summary 只暴露 freshness、last success/attempt、固定 failure code 与 selected/conflict window count；Local freshness 只由 accepted observation 计算，在 10 分钟 freshness/reset 边界内为 current，之后为 stale，只有 suspicious/rejected observation 时仍是 unknown。refresh status 可以暴露 state、due/reason、manual/claim 时间和 trigger，但不暴露 claim ID/revision；Reset Credits 只暴露动态 count/duration/freshness，不暴露 snapshot ID、credit hash；任何 response 都不包含 token、Authorization/Cookie、原始 HTTP body、raw error、JSONL path、request ID 或用户文案。

Local-only、Wham-only、双源一致、双源冲突、expired last-known-good、429 backoff、primary/secondary 跨窗口、Reset Credits 真实零/自然到期、空仓库与并发 snapshot 都使用 synthetic fixture 验证。可复用入口见 [`docs/test/quota-current.md`](../../../test/quota-current.md)。

### Wails 手动刷新与详情页合同（TOO-276）

`RequestQuotaRefresh` 是 `wails-bindings-v1` 唯一 command，只接受有限 `quota` / `reset_credits` source。Wails façade 在应用进入 event loop 前把既有 `applicationLifecycleRuntime` 单次绑定到私有 command slot；command 不重写 coordinator，不绕过 60 秒 durable 手动间隔、Retry-After、active claim、generation fence 或调用期 credential provider。返回 receipt 只包含 source、next due、固定 reason 与 last manual time，不暴露 source instance、scope、claim ID、revision、token 或底层 cause；非法 source 与依赖失败继续映射为 content-free `query-v1` error envelope。

`/quota` 只消费 generated `QuotaCurrentResponse` 和该 command。页面按 DTO 原样展示真实 windows、Local/Wham source summary、固定 explanation/failure code、Reset Credits 和两类 refresh status；observation ID 仅作 vnode key，limit/account/schedule identity 不进入 DOM。unknown 使用 `--`，真实 `0` 保留数值与 progress semantics；query 或 command 失败时继续显示 last-known-good，不用空态或伪 100% 覆盖。1 秒 clock 只更新可见倒计时且在组件卸载时释放，不能触发网络、参与仲裁或成为 scheduler。手动动作同时请求两个有限 source，任一失败时显示固定文案，并在 settle 后仅失效 authoritative quota query；后续 durable commit event 仍负责再次通知可见 query。

## UI 语义

```text
fresh:           5h 剩余 62% · 2 分钟前 · 在线
stale:           5h 剩余 62%
expired_unknown: 5h 剩余 62%
conflict:        5h 剩余 55%
```

Tray 和 Popover 始终使用普通百分比，不用 `≤`、`?`、状态胶囊或说明文案向用户区分 fresh、stale、expired_unknown 与 conflict。存在 last-known-good 时继续显示当前选定值；从未取得 accepted observation 时才显示 `--`。Quota 详情页仍可展示各来源 observation、时间、generation、原始 validity 与派生 disposition/reason/explanation，供诊断 current 的选择依据。`trusted`、`stale`、`expired_unknown`、`suspicious_candidate`、`source_conflict`、`unavailable` 是固定、无上游正文的 explanation code；UI 文案在后续卡映射，不读取数据库外的日志文本。

## 验收场景

- 网络失败不把 38% 变成 0%。
- 同 reset 下 used 降低不覆盖 current。
- reset 推进后的低 used 能进入新 generation。
- reset 已过且刷新失败时保留 last-known-good；无历史值才显示 `--`。
- 本地新高值可更新在线旧值，在线新低值不能覆盖本地高值。
- 401 后停止重复请求。
- partial response 不删除 weekly。
- JSON 解析失败不产生 observation。
- 手动刷新绕过退避但不绕过校验。
- Reset Credits 原始 ID/文案不落库，动态汇总在自然到期后不保留伪 available。
- 进程退出、请求取消或 claim 过期后都能从 durable schedule 恢复，且不会产生重叠请求。

可复用的 synthetic-only 验证入口见 [`docs/test/window-generation-observation.md`](../../../test/window-generation-observation.md)。
