# API 与订阅

## 目标与边界

“API 与订阅”用于展示没有本地 Agent Session 语义、但与用户 AI 服务消费直接相关的官方账户信息。它是独立功能页，不是 `AgentProvider`：

- 不进入 Codex、Cursor、Grok 客户端选择器。
- 不产生 Session、Project、Token、Tool 或成本归因数据。
- 不参与现有 Agent 用量、Quota 仲裁和 provider-scoped refresh。
- DeepSeek 与 OpenCode Go 各自配置、各自刷新、各自保留最后成功值；一个来源失败不影响另一个来源。

当前只接入两个窄能力：

| 来源 | 官方接口 | 展示内容 | 明确不展示 |
| --- | --- | --- | --- |
| DeepSeek | `GET https://api.deepseek.com/user/balance` | 账户可用状态、逐币种余额、今天/本周/本月内余额变化、余额趋势，以及基于成功采样估算的总充值和总消耗 | 请求历史、Token、Session、Project、模型拆分 |
| OpenCode Go | `GET https://opencode.ai/zen/go/v1/usage` | 5 小时、周、月三个窗口的已用比例、剩余额度与重置时间 | OpenCode Session、Token、Project、Tool、Zen 其他计费统计 |

接口地址、HTTP 方法和认证方式由 Helper 固定。用户不能把凭据配置成任意 URL，也不能借此把 Helper 变成通用 HTTP 客户端。

## 运行时职责

Go Helper 是接口访问、响应校验、自然周期、观察记录和最后成功值的业务真相。Swift App 只负责：

1. 在 Settings 中接收两个独立 API Key，并通过已鉴权的 Unix Domain Socket RPC 发起保存或删除。
2. 通过 `CoreService.APICredentialStatus` 读取不含密钥的配置状态。
3. 通过 `CoreService.APISubscriptionsCurrent` 获取结构化快照。
4. 在独立页面聚合展示两个来源。

Helper 对两个来源并行刷新，单个请求默认超时 10 秒，不跟随重定向，并限制响应体大小。响应必须完整满足对应 schema：DeepSeek 金额保留官方十进制字符串，余额差值用精确十进制运算；OpenCode Go 必须同时返回 rolling、weekly、monthly 三个合法窗口。

## 凭据与隐私

两个 API Key 保存在独立的 `credentials.db`，与业务数据使用的 `codex-pulse.db` 分离。只有 Go Helper 打开并读写该文件；Swift App 不直接访问 SQLite，也不能读取已保存的密钥。凭据目录必须是 `0700`，数据库文件必须是普通 `0600` 文件，权限不满足时 Helper 拒绝启动。

`credentials.db` 是明文 SQLite，不提供 Keychain 或文件级静态加密：同一 macOS 账户下获得文件读取权限的进程仍可读取其中内容。它解决的是避免每次 App 启动触发系统 Keychain 授权，以及把凭据限制在 Helper 的私有存储边界内，不应描述为对同账户恶意进程的防护。

凭据链路固定为：

```text
Swift SecureField -> 已鉴权 UDS mutation -> Helper -> credentials.db + Helper 内存 -> Authorization header
```

凭据不得出现在：

- 命令行参数、环境变量、启动鉴权 pipe 或主业务数据库 `codex-pulse.db`。
- 偏好文件、日志、诊断产物或错误消息。
- RPC 响应、UI 状态或最后成功快照。

保存 RPC 的请求体是唯一允许在 UDS 上传递密钥的 protobuf；UDS 位于私有 runtime 目录，并使用启动 pipe 下发的 bearer token 鉴权。Helper 复制请求数据完成写入后清理临时缓冲区；退出时清理内存凭据。保存或删除凭据后，App 重启 Helper，使采样服务和最后成功值切换到新配置。

Helper 每次保存凭据时生成并持久化一个不含密钥内容的随机凭据代次。业务数据库只记录该代次，用于隔离密钥更换前后的账户数据，不保存密钥或密钥哈希。

## 状态与失败语义

每个来源独立返回以下状态：

- `current`：本次刷新成功，展示新值。
- `stale`：本次刷新失败，但存在相同凭据代次的最后成功值；展示旧值并标记失败类别和最后成功时间。
- `unavailable`：已配置凭据，但本次失败且没有最后成功值。
- `unconfigured`：未配置该来源；不发起网络请求，也不展示历史值。

DeepSeek 成功余额写入 SQLite `api_subscription_balance_observations`，并可在 Helper 重启后恢复相同凭据代次的最后成功值。OpenCode Go 成功额度写入 SQLite `api_subscription_quota_observations`，用于按本地自然日汇总 5 小时窗口。两张表都按凭据代次隔离。周期趋势只读取当前凭据代次和所选自然周期内的成功观察；Helper 保留首条、余额发生变化的观察和最后一条，以减少连续相同采样的传输量，同时保持余额变化和最后观察时间。

DeepSeek 的“总充值”是相邻成功采样之间 `topped_up_balance` 正向变化之和；“总消耗”是 `topped_up_balance` 与 `granted_balance` 负向变化绝对值之和。它们是采样估算，不是官方交易流水：两次采样间同时发生的充值与消耗可能互相抵消，App 停止期间的变化也不能被完整还原。HTTP 失败对外只暴露稳定类别（网络、超时、认证、禁止访问、限流、服务端或协议错误），不透传可能包含敏感信息的原始响应正文；SQLite 失败作为本地错误上抛，不伪装成远端网络失败。

## UI 与刷新

侧边栏以独立分组呈现“API 与订阅”。页面包含：

- 统一活动热力图：一个过去 365 个本地自然日的日历同时承载 DeepSeek 与 OpenCode Go。方块只使用与 Codex 年度活动一致的蓝色五档；DeepSeek 按所选币种的每日总消耗采样估算、OpenCode Go 按当日 5 小时窗口最高已用比例，各自使用同一套 25%/50%/75% 分位档位，一个方块取两个来源中较高档。热力图不显示净变化或 Token；悬停日期时，上方分别展示 DeepSeek 总充值/总消耗和 OpenCode Go 5 小时峰值已用/最新剩余。
- DeepSeek 卡片：账户可用状态、逐币种余额，以及默认“本月”、可切“今天/本周/本月”的余额阶梯趋势、余额变化、总充值和总消耗。趋势线固定使用系统蓝色，连接周期内全部保留采样，不把 App 停止或网络失败期间的空档推断成余额变化；悬停点展示观察时间和原始十进制余额。
- OpenCode Go 卡片：固定的 5 小时、周、月三个额度窗口。
- 各来源自己的配置状态、数据状态、最后成功时间和可读失败提示。

两个来源各占一整行。DeepSeek 周期起点使用本地时区的自然日、周一和月初；如果周期边界没有记录，就使用该周期内第一条成功记录，并在页面展示其实际时间。余额变化仍是期初与当前记录的差值；总充值和总消耗明确标为采样估算，不能解释为完整流水。

进入页面、手动刷新、全局刷新和应用生命周期刷新都会触发查询。Helper 启动时还会立即采样一次，此后在 App 运行期间每 15 分钟采样一次；后台采样串行执行，并在 SQLite 关闭前停止。页面标题和数据不随 Agent Provider 选择器变化。

## 验证边界

单元测试、contract test 和 App deterministic test 使用 `t.TempDir()` 下的独立凭据库、内存凭据、`httptest` 或 fixture，验证权限、重启持久化、请求契约、严格解码、并行独立失败、last-known-good、RPC 映射和 UI 边界。自动测试不得读取真实 `credentials.db`，不得使用真实 API Key，也不得调用 DeepSeek 或 OpenCode Go 线上接口。

真实账户验收需要用户另行提供或录入已轮换凭据，并单独记录为 live evidence；synthetic 测试通过不能替代该结论。
