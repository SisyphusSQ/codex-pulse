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

窗口代际使用 `(window_kind, limit_id, window_minutes, resets_at_ms)` 识别。同一代际内 used 正常只能保持或上升；进入新代际后才允许从低值重新开始。

逐 window 校验：

- `used_percent` 必须在 `0..100`；
- window duration 和 reset 时间必须合理；
- primary 必须存在，字段类型和 observation 时间不能明显倒退；
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

刷新失败只写 `source_attempts`，不新增伪造 observation，也不覆盖 `quota_current`。一个窗口未知不能把其他窗口一起清空。

freshness 与 conflict 分开：current 可以同时是 `fresh + conflict` 或 `stale + conflict`。初始规则为：accepted observation 在 10 分钟内且 reset 未到是 fresh；超过 10 分钟但仍在同一窗口是 stale；reset 已过且没有可信新代际是 expired_unknown；从未取得 accepted observation 是 never_loaded。

## 仲裁规则

不采用“wham 永远覆盖本地”的固定优先级：

1. 同一代际内，新 used 相同或上升才接受；下降则 suspicious，不覆盖 current。
2. 同代际有多个 accepted 来源时，取最大的 `used_percent`，即采用最保守的 remaining。
3. reset 向后推进且时间关系合理时接受为新 generation，允许 used 重新从低值开始。
4. reset 向过去移动、跨度异常或来源代际无法解释时保留 last-known-good；旧 reset 到期后进入 expired_unknown。
5. 本地 JSONL 到来只重新仲裁，不触发在线请求；较新的本地高值可以更新旧 wham，较新的 wham 低值不能覆盖同代际本地高值。

例：同一 reset 下本地已用 45%、在线已用 41%，current 采用 45%，UI 显示“剩余最多 55%”；41% 保留为 conflict evidence。之后在线返回 47% 时，47% 成为 current。

## 100% 防误判

只有满足以下条件，才接受 remaining 100% 对应的 `used_percent = 0`：

- `resets_at` 已推进到可解释的新窗口；
- 当前时间越过上一窗口 reset，窗口长度合理且结构完整；
- 没有更晚的本地 rate-limit 快照与其冲突。

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

Tray 和 Popover 始终使用普通百分比，不用 `≤`、`?`、状态胶囊或说明文案向用户区分 fresh、stale、expired_unknown 与 conflict。存在 last-known-good 时继续显示当前选定值；从未取得 accepted observation 时才显示 `--`。Quota 详情页仍可展示各来源 observation、时间、generation 和 validity，供诊断 current 的选择依据。

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
