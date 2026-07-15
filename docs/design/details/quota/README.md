# Quota Trust Model

## 问题

Codex Runway 实际运行中观察到：网络不佳时，5 小时和周窗口可能显示成剩余 100%，容易把未知状态误认为额度已经重置。Codex Pulse 不允许失败响应、默认值或可疑降级覆盖最后可信配额。

数据库统一保存来源原始语义 `used_percent`；UI 的 `remaining_percent = 100 - used_percent` 只在展示层计算。每条 observation 必须携带来源、观测时间、窗口长度、reset 时间、有效性和 request。

## v0.1 来源

- `local_jsonl`：始终启用，只读 `event_msg.token_count.rate_limits`。无额外网络行为，但只在 Codex 产生新活动时更新。
- `wham`：v0.1 默认开启，请求 `https://chatgpt.com/backend-api/wham/usage`，用户可随时关闭。更实时，但可能受到网络、认证和内部接口变化影响。

不启动或复用 `codex app-server`，也不作为兜底。v0.1 只支持当前单账号，固定 `account_scope = default`；运行期间切换 Codex 账号不属于支持场景。

在线 quota 或 reset credits 启用时，只从 `~/.codex/auth.json` 将当前 access token 读入内存。应用不保存 token、refresh token、Authorization header 或 auth 文件内容；不使用 refresh token 主动刷新，也不修改 `auth.json`。401/403 后标记 `auth_required` 并停止自动重试，等待 Codex 恢复登录或用户手动重试。用户关闭对应能力后立即停止该能力的在线调度；已有非敏感 observation history 保留。

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

可复用的 synthetic-only 验证入口见 [`docs/test/window-generation-observation.md`](../../../test/window-generation-observation.md)。
