# TOO-447 Codex 账号订阅列表与会员日期管理实施计划

## 1. 目标与验收边界

在单机、单个已确认 Codex Home 内提供账号订阅管理闭环：

- Settings 同时展示真实识别账号和用户提前添加的手动账号。
- 支持备注、手动套餐、每月续费日或会员到期日、日期类型、显式关联和取消关联。
- 真实账号切换 A→B→A 后恢复同一公开账号及其手动补充资料，不串用 B 的信息。
- Popover 只展示当前 confirmed 账号的 resolved 套餐、邮箱和会员日期。
- Go Helper 是身份、持久化、套餐解析、日期差和并发语义的唯一真相；Swift 不读 SQLite、不复制业务规则。

完成标准是本计划中的 synthetic、contract 和 Swift 门禁通过。真实 Home、真实换号和人工 UI 验收必须单独记录，未执行时保持 `NOT_RUN`，不得由单元测试代替。

## 2. 基线与非目标

基线为 `main@792aea0`：已有 schema v32、稳定 HMAC `account_scope`、binding generation、App Server 账号夹读和 synthetic A→B→A。

本卡不做：

- 自动采集会员日期；token expiry、quota `resetsAt`、Reset Credit `expiresAt` 都不是会员日期。
- 非当前账号的远端采集、跨设备同步、真实 Codex 账号删除、历史 Session/用量删除、支付或续费。
- 改变 Home 聚合用量、成本和项目归因口径。
- 向 Proto、日志或 UI 暴露 HMAC scope、raw ChatGPT account ID。

自动日期能力固定为 `manual_only`。官方协议出现可靠字段时另开合同。

## 3. 核心设计

### 3.1 身份

- detected identity：由稳定私有 `account_scope` 定位，首次确认时生成公开 UUID。
- manual identity：由客户端生成并在编辑会话内复用的公开 UUID。
- 邮箱只用于显示和 `same_email` 关联候选，绝不是 stable key。
- 相同邮箱的多个 detected 账号保持独立；只有用户显式 Link 才形成一对一关系。
- Unlink 后恢复一个 detected 行和一个 standalone manual 行。

### 3.2 数据流

```text
App Server account sandwich
  -> binding fence 校验
  -> detected profile 写入 SQLite
  -> subscriptionaccounts 投影
  -> CoreService over UDS
  -> Swift AppModel authoritative readback
  -> Settings / Popover
```

List/Create/Update/Delete/Link/Unlink 只访问 SQLite，不启动 App Server。Delete 仅删除非当前账号的本地订阅记录；linked 账号整体删除 link、manual supplement 与 detected record，并保留底层 account scope。只有当前账号 discovery 可以写 automatic profile，且写入与 `(account_scope, binding_generation)` 校验处于同一事务。

### 3.3 SQLite v33

新增 migration `codex-subscription-accounts`：

- `codex_subscription_detected_accounts`：私有 scope、公开 ID、检测邮箱、automatic plan fact、观测时间和 revision。
- `codex_subscription_manual_entries`：公开 ID、手动邮箱/备注/套餐/日期、revision 和时间戳。
- `codex_subscription_links`：detected scope 与 manual ID 的一对一关系、revision 和时间戳。

数据库约束：

- public ID 为 canonical lowercase UUID；scope 只允许 64 位小写十六进制。
- 邮箱、备注长度受限；套餐、日期类型只允许固定枚举。
- 日期为真实有效的 Gregorian `YYYY-MM-DD`，日期与类型必须同时存在或同时缺失。
- revision 为正数；`updated_at_ms` 不早于创建/关联时间。
- 两个 email match 索引只用于候选查询，不设唯一约束。
- migration 完成前回读 schema，并执行 `PRAGMA foreign_key_check`；失败整体回滚。

### 3.4 套餐与日期

套餐保存 automatic 和 manual 两份事实：

1. 用户明确保存的 manual override 优先；
2. 没有 manual 时使用 automatic `known`；
3. 两者都无值时为 `unavailable`。

automatic plan 复用现有 `subscriptiontier` 夹读证据，不另做第二套 Pro 判定。

`membership_date` 与 `date_kind` 组成判别联合：`next_renewal` 只使用日期中的 `DD` 作为每月循环续费日，所选日期在短月份不存在时按当月最后一天处理，经过后自动滚动到下个月；`membership_expiry` 使用完整 Gregorian 年月日。Go 根据调用方的 `evaluated_at_ms` 与 IANA time zone 计算下一次续费或到期日的 `day_delta`，只有已过期的 `membership_expiry` 进入 `needs_update`。Swift 不计算日期差；续费日写入兼容锚点，到期日才使用完整日期选择器。

### 3.5 Mutation 语义

- Create：同一 manual ID 与归一化字段完全相同返回 `noop`；同 ID 不同字段返回 `request_id_reused`。
- Update：字段完全相同优先返回 `noop`；否则 revision 不匹配返回 `revision_changed`。
- detected 首次编辑：以稳定 `new_manual_entry_id` 原子创建 supplement 并 Link；重试遵循相同 ID 规则。
- Delete：仅允许非当前账号；linked 账号原子删除 link、manual supplement 与 detected record，保留底层 account scope；重复删除返回 `noop`，revision 变化或账号已成为 current 返回 conflict。
- Link：同一 pair 重试返回 `noop`；任一侧已关联其他对象返回 `already_linked`。
- Unlink：pair 已不存在返回 `noop`；对象变化返回 `link_target_changed`；manual 无邮箱时返回 `manual_email_required_before_unlink`。
- revision 溢出、非法时间和结构损坏返回错误，不伪装成成功。

Swift 不做 optimistic success。receipt 后必须 List 回读，且目标账号的身份、字段和 link 状态与预期完全一致，才能展示 `applied` 或 `noop`。旧列表和被 invalidation 抢先的 mutation 回读不得覆盖新状态。

## 4. Core 与运行时合同

### 4.1 Proto

- handshake：`core-rpc-v4`
- feature contract：`codex-subscription-accounts-v1`
- query：`ListCodexSubscriptionAccounts`
- commands：`Create/Update/Delete/Link/UnlinkCodexSubscriptionAccount`
- `AccountSnapshotRequest` 增加 evaluation time/time zone。
- Codex `AccountSnapshotResponse` 增加当前 subscription；非 Codex provider 必须 absent。

公开 DTO 只包含公开 detected/manual UUID，不包含 private scope。返回 automatic/manual/resolved 来源、日期状态和必要 revision，使 UI 可解释并能做 CAS。

### 4.2 账号切换与 invalidation

1. discovery 获取稳定夹读并确认当前 binding。
2. automatic profile 写入前执行 writer fence。
3. 可见 profile 事实变化才触发 `account` invalidation；仅观测时间推进不触发。
4. AccountSnapshot 的 display/profile/current-subscription 必须属于同一 fence。
5. 任一阶段发现 binding changed，清除 display 并只返回最新 binding，不组合旧账号资料。
6. Swift 收到 `account` invalidation 后取消旧 List、推进本地 generation 并重新读取。

## 5. Swift 交互

Settings 顶部新增「Codex 账号与订阅」：

- 行展示备注或邮箱、resolved 套餐、日期类型、日期、剩余天数，以及“当前/曾识别/手动/已关联”标签。
- 添加账号要求邮箱；编辑采用完整字段替换，允许清空可选字段。
- 相同邮箱只显示候选，Link 前明确提示“邮箱相同不代表同一身份”。
- conflict 保留草稿，并以 authoritative readback 的最新账号和 revision 重新基线化。
- Unlink 前 manual 必须有独立邮箱，否则引导先补全。
- create 和 detected 首次编辑的 UUID 在同一 sheet 生命周期内保持稳定。

Popover 仅在 Codex、binding confirmed、上下文匹配且 subscription 为 current 时消费新数据。无日期不显示空行；日期按当前 AppLocalization locale 展示。

## 6. 隐私

- 截图 capture 期间同时隐藏邮箱、备注、套餐、日期和剩余天数。
- 剪贴板固定输出“账号、套餐与订阅日期信息已隐藏”，失败路径不得回退复制原值。
- 原始邮箱、备注、日期、SQLite dump、App Server 响应和完整日志只能进入忽略的 `.artifacts/`。
- RPC 错误只返回稳定 code/message key/field，不包含输入值、scope 或 raw account ID。

## 7. 实施顺序

1. 增加 domain 类型、validator、套餐解析、civil-date 与 projection 测试。
2. 增加 schema v33、backfill、repository、CAS/link/idempotency 测试。
3. 将 detected profile 接入 account sandwich writer fence 和 `account` invalidation。
4. 增加 Core/Helper/Proto 合同与真实 UDS contract 测试。
5. 增加 Swift client、AppRuntime、AppModel generation/readback 逻辑。
6. 增加 Settings 编辑/link/unlink 和 Popover/隐私展示。
7. 同步设计、README、migration 与验证 runbook。

稳定产品合同见 `docs/design/details/codex-subscriptions/README.md`；可执行证据与 live 步骤见 `docs/test/codex-subscription-accounts.md`。

## 8. 验证矩阵

| 层级 | 必须证明 |
| --- | --- |
| Domain | plan precedence、日期真实有效性、闰日/DST/day delta、同邮箱不合并 |
| Store | v32→v33、checksum/rollback、public ID 恢复、CAS、幂等、late writer fence |
| Runtime | A→B→A 不串资料、List/mutation 不启动 App Server、invalidation 不自循环 |
| Core/Helper | v4 handshake、DTO 无 private scope、字段级错误、UDS create→list→update→link→unlink |
| Swift | stable request ID、conflict rebase、authoritative readback、stale reply 丢弃、时区重读 |
| Privacy | Settings/Popover copy、capture 与 clipboard canary |

聚焦入口：

```bash
go test ./internal/codex/subscriptionaccounts -count=1
go test ./internal/store -run 'Test.*CodexSubscription' -count=1
go test ./internal/app -run 'Test.*(CodexSubscription|AccountBinding|AccountSnapshot)' -count=1
go test ./api/codexpulse/core/v1 ./internal/core ./internal/helper \
  -run 'Test.*(CodexSubscription|AccountSnapshot|Contract|Proto)' -count=1
bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check
make verify-swift-client
swift run --package-path app/macos codex-pulse-app-tests
make check
```

本地默认不运行 `make verify`、全仓 race 或 `make verify-live`。真实 Home 验收需另行授权，并分别记录 Settings 持久化、A→B→A、same-email Link/Unlink、跨午夜日期和截图/剪贴板隐私结果。

## 9. 风险与停止条件

- 无法在同一事务证明 writer fence 时停止 profile 写入。
- 无法在 mutation 后获得 authoritative readback 时不得显示成功。
- migration schema/readback/checksum 任一不一致时停止升级并保留旧库。
- 发现真实账号数据进入日志、Proto 私有字段或可提交证据时停止交付并清理。
- 官方协议或当前 schema/contract 与本计划冲突时先更新设计，不以兼容分支掩盖。

交付说明必须分别列出：代码、targeted tests、`make check`、CI、live E2E、人工 UI、签名/发布；未实际执行的层级明确标为 `NOT_RUN`。
