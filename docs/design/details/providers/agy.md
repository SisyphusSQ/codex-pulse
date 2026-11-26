# Antigravity (AGY) 客户端

Antigravity 是独立的第五个 Agent Provider（代码、协议、数据库与上报身份为 `agy`，UI 显示名 `Antigravity`，极窄状态栏场景可用 `AGY`）。本文件记录 AGY 里程碑（Master `TOO-729`）的底层契约、配置版本、SQLite Schema、Provider 三层控制面与分阶段边界；当前由 `TOO-730 (AGY-E01)` 建立底层地基，本地轨迹采集与定价（`AGY-E02`）、本机 LS RPC 配额/账号（`AGY-E03`）、macOS / Windows / 中心适配（`AGY-E04`～`E07`）按子卡依次落地。

## 客户端身份与能力边界

- `internal/agentprovider` 注册 `AGY = "agy"`；空 `ProviderScope` 仍归一为 `codex`，未知非空值必须校验失败，禁止默认回落为 Codex。
- `AGYContext()` 声明的数据来源为：
  - `agy.conversation_summaries`
  - `agy.conversation_steps`
  - `agy.transcript_fallback`
  - `agy.language_server`
- 能力集固定为 `account`、`quota`、`sessions`、`projects`、`models`、`tokens`、`estimated_cost`。
- **明确排除项**：不提供调用统计（`tools` / `InvocationUsage`），不建 `agy_tool_events` 表，不在 `agy_sessions` 建 `tool_call_count` 列；不做加密 `.pb` 的本机 LS RPC 轨迹兜底；不逆向提取 OAuth 凭据，不接管账号登录/切换或强杀宿主进程；不按文本长度估算生图 Token；不采信 `.deep_stats_cache.json`。

## Preferences v6 与 Core Proto

- Preferences schema 从 `v5` 升级至 `v6`：
  - `providers.agy`：默认 `auto`（支持 `auto` / `enabled` / `disabled`）；从 v1～v5 迁移时原样保留既有 4 客户端（`codex`、`cursor`、`grok`、`dsh`）意图与子开关，补齐 `providers.agy = auto` 与 `online.agy_online_enabled = true`。
  - `online.agy_online_enabled`：默认 `true`，独立控制本机 LS RPC 账号与配额读取；关闭后保留历史非敏感 last-known 且不影响纯本地 SQLite 统计。
- `api/codexpulse/core/v1/core.proto`：
  - `SettingsOnlineSnapshot` 新增 `bool agy_online_enabled = 6`。
  - `SettingsOnlineUpdate` 新增 `optional bool agy_online_enabled = 6`；旧客户端未携带该字段时，Go Helper 保留当前持久值，避免旧端保存设置时静默关闭 AGY 在线能力。
  - `UpdateSettings` 客户端意图数组接受 3 / 4 / 5 项，缺省 `agy` 时沿用当前持久化意图。
  - 握手版本保持 `core-rpc-v9`，控制面保持 `provider-control-v1`。

## Metadata-Only 探测与三层控制面

`internal/providercontrol` 对 AGY 执行严格 **metadata-only** 探测，严禁打开 SQLite 内容、读取 JSONL 正文或发起任何网络请求：

- 根目录解析优先级：`CODEX_PULSE_AGY_HOME`（测试与 CI 隔离根目录，替代 `.gemini` 层）→ `ANTIGRAVITY_GEMINI_DIR` → `$HOME/.gemini`。环境变量必须为规范绝对路径，否则返回 `invalid / unsafe_path`。
- 探测四个产品子目录：`antigravity`、`antigravity-ide`、`antigravity-cli`、`antigravity-idea`。
- 仅执行 `lstat` 元数据检查：拒绝符号链接（`unsafe_path`）与错误类型（`invalid_type`）；产品目录本身安全且至少存在一个关键数据路径——普通文件 `conversation_summaries.db`，或目录 `conversations/`、`brain/`——即判定为 `available`。
- 四个产品目录通过 `firstAvailable` 合并：任一可用为 `available`，全部缺失为 `missing / not_found`，否则优先返回 `inaccessible / permission_denied`，再返回 `invalid`。
- 关闭顺序沿用统一控制面：`拒绝新 admission → 取消 operation context → drain 已接纳任务与提交租约`。显式 `disabled` 在重启、唤醒、数据源重新出现后必须保持关闭；直接查询稳定返回 `provider_disabled`（`retryable=false`）。
- `internal/providerrefresh` 全局刷新顺序扩展为五路：`codex → cursor → grok → dsh → agy`，AGY 组件固定为 `local_trajectories` 与 `online_quota`。关闭项回执 `skipped_disabled`；在 `AGY-E02` / `E03` 接入真实刷新器前，未绑定刷新器回执 `skipped_unavailable`。AGY `online_quota` 失效通知路由至通用 `quota` 域，不误触 `quota_codex`。

## 本地 SQLite Schema v40（`agy-local-facts`）

本地应用 SQLite schema 从 `v39` 升级至 `v40`（`agy-local-facts`），严格保持 v1～v39 历史迁移与校验和不变：

- 重建 `agent_provider_snapshots` 与 `agent_provider_sources`，将 `provider` CHECK 白名单扩展为 `('codex','cursor','grok','dsh','agy')`，迁移期间无损保留既有事实。
- 新增 5 张独立 `agy_*` 事实表与 4 个索引，不混写 Codex / Cursor / Grok / DSH 表：
  - `agy_sessions`：会话摘要、`display_title`（≤512）、`title_source`（`agy_annotation`/`agy_summary`/`fallback`）、可空 `parent_external_session_id`、`session_role`（`main`/`subagent`/`swarm_worker`/`side_question`/`unknown`）、`project_key`、`project_display_name`、`project_source`（`workspace`/`inherited_parent`/`unassigned`）、时间与模型、`request_count`、`lineage_conflict`、`coverage_state`、`throughput` JSON；`UNIQUE(provider, external_session_id)`。
  - `agy_session_lineage`：跨四个产品目录的脱敏来源血缘与内容摘要（`ON DELETE CASCADE`）。
  - `agy_usage_events`：64 位 hex `event_id`、单步 `step_index`、原样 Token 桶（`uncached_input_tokens`、`cache_read_tokens`、`output_tokens`、`reasoning_tokens`、`total_tokens`）及 `*_known` 标记、步骤起止时间（用于 TPS）与 `provenance`（`agy_step_metadata`/`agy_transcript_jsonl`）；费用不落库，查询时按版本化公价计算。
  - `agy_quota_snapshots`：按 `(account_scope, limit_id, window_kind)` 存储多配额池双窗快照；`account_scope` 强制为 64 位 hex 摘要，**禁止存邮箱原文**。
  - `agy_quota_observations`：按 `(account_scope, limit_id, window_kind, observed_at_ms)` 有界保存真实配额观测点，供 `QuotaPace` 计算节奏。
- 索引：`idx_agy_sessions_activity`、`idx_agy_sessions_project`、`idx_agy_usage_time`、`idx_agy_quota_observations_window`。

## E01 阶段查询路由行为

在 `AGY-E02` / `AGY-E03` 绑定真实采集与配额后端前：

- `agentrouter.Service`、`agentrouter.QuotaRouter`、`pricingcatalog.Service` 均对 `agentprovider.AGY` 设立显式分支：显式关闭返回 `provider_disabled`（不可重试），未绑定后端返回 `unavailable`（可重试），绝不误报参数校验错误。
- `AccountSnapshot` 在 E01 阶段对 `agy` 返回空快照（同 `dsh`），待 E03 接入本机 LS RPC 账号读取。
