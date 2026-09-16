# Codex 账号与订阅

本页冻结 TOO-447 的产品、身份、存储、来源、日期、Core 和隐私合同。实现以 `api/codexpulse/core/v1/core.proto` 与 SQLite application schema v33 为准。

## 产品边界

Settings 顶部「Codex 账号与订阅」列出当前 Codex Home 内已识别与手动记录的账号。Popover 与 Codex「额度与用量」页头只展示当前 confirmed 账号的 resolved 套餐、邮箱和手动维护的每月续费日或会员到期日；额度页卡片点击后进入 Settings 管理，不提供虚假的账号切换能力。页头快照必须与当前额度的 binding scope 和 generation 一致，错配时隐藏旧账号事实并显示确认中或不可用。Cursor / Grok 页面保持既有展示，不消费 Codex 订阅列表。Home 聚合用量、成本、项目归因口径不变。

每月续费日与会员到期日本期固定为 manual-only。不得把 token expiry、quota `resetsAt` 或 Reset Credit `expiresAt` 映射成会员日期。官方 App Server 若日后提供稳定会员日期字段，需另开合同，不得 silently 改写本期语义。

## 身份

- 稳定身份是 detected 公开 UUID 与 manual 公开 UUID，不是邮箱。
- 邮箱只产生 `same_email` 关联候选；相同邮箱的两个 detected scope 仍是两条账号。
- 只有用户显式 Link 才建立一对一 link；Unlink 后恢复两条独立记录。
- 非当前账号可删除本地订阅记录；linked 账号同时删除 link 与对应 manual supplement。不得删除底层 account scope、Session、用量、成本或真实 Codex 账号。detected 账号后续再次登录时会重新出现。
- HMAC `account_scope`、binding generation 和 raw ChatGPT account ID 不进入 Proto、日志或 UI。

## 存储

application schema v33，migration 名 `codex-subscription-accounts`。三张专用表保存 detected accounts、manual entries 和显式 links。专用表可以持久化 detected/manual email；binding、quota、日志、Proto private identity 与原始证据仍不得泄露邮箱或 raw account ID。

List / Create / Update / Delete / Link / Unlink 只读写 SQLite，不启动 App Server，不采集非当前账号。Delete 必须在同一事务内复核 current binding 与各行 revision；当前账号拒绝删除。自动 profile 写入必须在同一 SQLite transaction 内核对当前 `(account_scope, binding_generation)`。

## 套餐来源

自动套餐与手动套餐分别保存。编辑器展示当前识别值，但未修改的自动值不复制到 manual supplement；用户明确修改后保存为 manual override。Go 按 `subscriptionaccounts.Resolve` 统一解析：manual 优先，没有 manual 时使用 automatic `known`，两者都没有则 unavailable。TOO-446 的 `subscriptiontier` resolver v1 不变；订阅列表复用同一夹读 plan evidence，不另做一套映射。

## 日期

日期按 `YYYY-MM-DD` Gregorian civil date 保存，并由 `date_kind` 组成判别联合：`next_renewal` 只使用其中的日（`DD`）作为每月循环续费日，年和月仅为兼容锚点；`membership_expiry` 使用完整年月日。每月不存在 29–31 日时按当月最后一天续费，日期经过后自动滚动到下个月，不进入 `needs_update`。会员到期的剩余天数由 Go 按 evaluation time 与 IANA time zone 计算 civil day delta；`0` 表示今天，负数表示需要更新。Swift 不得自算 day delta，也不得把 DatePicker 的绝对时间戳写入 Proto。

## Core

精确握手为 `core-rpc-v4`。`Contracts.codex_subscription_accounts_version=codex-subscription-accounts-v1`。`codex_pro_tier_version` 保持 v1。invalidation 仍为 `query-invalidation-v3`；账号资料变化使用既有 `account` domain。

新增 query `ListCodexSubscriptionAccounts` 与 command `Create/Update/Delete/Link/UnlinkCodexSubscriptionAccount`。`AccountSnapshotRequest` additive `evaluated_at_ms` / `time_zone`；Codex 响应 additive `subscription`。非 Codex provider 的 `subscription` 必须 absent。

mutation receipt 为 `applied` / `noop` / `conflict`。Swift 在 receipt 后必须做 authoritative List readback，不得 optimistic success。conflict 保留编辑草稿。

## 隐私

Popover 截图与剪贴板必须同时隐藏邮箱、备注、套餐、日期和剩余天数。剪贴板固定文案为「账号、套餐与订阅日期信息已隐藏」。失败路径不得回退复制原始账号订阅信息。
