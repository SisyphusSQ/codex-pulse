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

## E02 本地只读轨迹采集、版本化定价与统计查询

- `internal/pricing/agy_catalog.go` 冻结 Antigravity 公开 API 参考定价表（`AGYPriceVersion = "agy-api-2026-11-25"`），支持 `MODEL_PLACEHOLDER_M*` / `MODEL_GOOGLE_*` / `MODEL_CLAUDE_*` / `MODEL_OPENAI_*` 归一化与时间分段费率（`[from, until)` 左闭右开，如 Carbon/Argon 2026-12-01 与 Flash 3.6–3.8 2027-01-01 切换），整数微美元单次 HALF-UP 舍入；未知模型严格保持 `unpriced`（`missing_price_component`），不猜价。
- `internal/agyprovider/` 实现只读 + WAL 感知的本地轨迹采集与统计闭环：
  - 扫描 `CODEX_PULSE_AGY_HOME` → `ANTIGRAVITY_GEMINI_DIR` → `$HOME/.gemini` 下 4 个产品子目录（`antigravity`、`antigravity-ide`、`antigravity-cli`、`antigravity-idea`）；通过 `mode=ro` + `query_only(1)` + `busy_timeout(3000)` 只读事务与 Windows 句柄钉住读取 `conversation_summaries.db` 与 `conversations/<uuid>.db`。
  - 严格隐私白名单：绝不读取 `preview`、`conversations_fts` 或 `steps.step_payload`；仅从 `raw_summary` / `steps.metadata` / `gen_metadata` 白名单解码时间戳、模型、Token 与 `originCascadeId`，在解析边界丢弃正文、思考过程、工具参数/结果及本机绝对路径。
  - 克隆步骤去重（过滤 `originCascadeId != cascadeId` 与 `gen_metadata.cascade_id != cascadeId`）；子代理 / Swarm Worker / Side-Question 保留独立会话，仅在同库可解析父会话时继承父项目（`inherited_parent`），否则归入 `未归属项目`。
  - 一个生成记录（`gen_metadata` 的一行）关联多个步骤时，仅记一次请求和用量，优先归属到自身携带用量的非克隆步骤。只有生成级用量可用时仍计入 Token 和费用；若关联多个步骤、无法确定生成时长，则从 TPS 的分子和分母同时排除。不同生成记录独立计数。
  - Token 口径按 Uncached Input 语义映射：`input = uncached + cache_read`、`cached_input = cache_read`、`output = outputTokens`、`reasoning = thinkingOutputTokens`（输出子集，不重复计费）、`total = uncached + cache_read + output`；步骤级 TPS 使用纳秒级 `createdAt → finishedGeneratingAt`（非 `CLEARED` 步回退 `completedAt`），严格过滤 `< 0.5s` 短步。
  - `annotations/<uuid>.pbtxt` 仅解析顶层 `title`；`.db` 缺失或无用量时安全回退有界 `brain/<uuid>/.system_generated/logs/transcript.jsonl`（24 MB 上限，丢弃正文）；仅存在加密 `.pb` 时标记 `partial`，不伪造 Token。
  - 全部统计查询（`UsageCost`、`ListSessions`、`SessionDetail`、`ListProjects`、`ProjectDetail`）与 `pricingcatalog`、`dashboardsummary`、`providerrefresh`、`app` 装配完成。

## E03 本机 LS RPC 发现、账号夹心与多配额池

- **严格回环发现（`internal/agyprovider/ls_discovery*.go`）**：
  - 优先扫描 `~/.gemini/<product>/daemon/hub_*.json`（校验 `pid` 存活，按 `mtime` 降序提取 `httpPort` / `httpsPort` 与 `csrfToken`，忽略损坏文件）；未命中时回退跨平台进程枚举（macOS/Unix `ps` + `lsof`，Windows PowerShell `Win32_Process` + `Get-NetTCPConnection`）。
  - 严格限定回环地址（`127.0.0.1` / `localhost` / `::1`），禁止 HTTP 重定向；白名单仅允许 `GetUserStatus` 与 `RetrieveUserQuotaSummary`，拒绝 `GetAllCascadeTrajectories` / `GetCascadeTrajectory`。
- **账号夹心与账号胶囊（`ls_client.go` / `account.go`）**：
  - 依次执行 `GetUserStatus → RetrieveUserQuotaSummary({forceRefresh}) → GetUserStatus`，若前后账号不一致则立即丢弃脏观测，防止切号串数据；常规自动刷新最小间隔为 `5m` 且固定传 `forceRefresh: false`，不要求 LS 强制刷新；是否访问上游仍由 LS 自身实现决定，不能据此保证完全离线。仅用户显式手动刷新（`RequestQuotaRefresh` / 全局 `manual` 触发）绕过节流并传 `forceRefresh: true`。
  - `AccountSnapshot(provider=agy)` 返回 `type="agy"`、`email`（仅内存即时展示，不写盘）与标准化 `plan_type`（`Google AI Ultra 20×`、`Google AI Ultra 5×`、`Google AI Pro`、`Google AI Free`）；档位优先按 LS `userTier.id` 映射：`g1-ultra-lite-tier` 为 `Google AI Ultra 5×`，`g1-ultra-tier` 为 `Google AI Ultra 20×`；仅有通用 Ultra 名称时显示 `Google AI Ultra`，不推断倍数。`agy_quota_snapshots.account_scope` 仅存 64 位 SHA-256 摘要，CSRF token 与原始响应体永不落库或写日志。
- **多配额池归一化与离线语义（`quota_pools.go` / `quota_collector.go` / `quota_query.go`）**：
  - 兼容 Google AI Ultra 共享池、Google AI Pro 独立 Argon/Carbon 分组或嵌套周桶、`clientModelConfigs.quotaInfo` 差异推断，以及 Proto3 零值省略规则（仅当 `remainingFraction` 省略且 `resetTime > now` 时判为 `100%` 已用；已过期则不判耗尽）。
  - 映射为 `agy.gemini`、`agy.argon`、`agy.carbon`、`agy.claude_gpt`（未知池 `agy.pool.<slug>`）× `primary`（300m / 5h）与 `secondary`（10080m / weekly），写入 `agy_quota_snapshots` 与有界 `agy_quota_observations`（每窗最多 256 条），接入 `QuotaCurrent`、`QuotaPace`、`RequestQuotaRefresh(source=quota)`、`providerrefresh`（`online_quota`）与 `runtimeinfo`（`agy.ls_rpc`）。
  - 离线、LS 不可达或关闭 `agy_online_enabled` 时：停止全部 LS RPC 请求，对未过期窗口保留 last-known-good 并标 `stale`/`partial` 及 `data_as_of_ms`；跨过 `resets_at_ms` 后标为 `expired_unknown`（返回 unknown）。

## E04–E05 原生客户端（macOS SwiftUI / AppKit 与 Windows WinUI 3）

- **统一五路排序与能力边界**：`codex → cursor → grok → dsh → agy`（显示名 `Antigravity`，极窄状态栏紧凑名 `AGY`）；隐藏 AGY 调用统计入口并保证零 `InvocationUsage` RPC，不接入 Codex 专属 Reset Credits / Paid Credits / Account Quotas 视图。
- **菜单栏 / 托盘与多配额池展开**：
  - 菜单栏/托盘摘要展示**最紧缺未过期配额池**（同剩余%优先 `5h`，再按 `Gemini → Argon → Carbon → Claude & GPT → agy.pool.*` 确定性选取）；无有效额度时顶行 `--`、底行回退 `今日 <Token>`。
  - macOS Popover 的 AGY 账号与状态概览在同一轮读取完成后一起发布；后台刷新期间保留上一幅完整展示，避免先发布空账号再异步补回造成胶囊跳变。账号结果变更或退出时发布本轮真实结果，不沿用旧身份。
  - Popover / 托盘面板展示账号胶囊（含邮箱隐私脱敏与标准化套餐标签）、全部返回配额池（每池 5h + 周双窗、重置倒计时、stale 提示）、每日模型趋势与“近 7 天”Top 5 项目。
- **全页面与平台边界**：主窗口 `DashboardSummary`、`Overview`、`Quota`（按 `limit_id` 分组多池卡片 + 节奏多池切换 + 直接“刷新额度”按钮）、`Sessions`（缓存命中率、步骤级 TPS、轻量角色徽章 `subagent`/`swarm_worker`/`side_question`）、`Projects`、`Pricing`、`Sources`（`Antigravity` 来源分组）与 `Settings`（三态 + `agy_online_enabled` 开关）全量支持；Windows Go Helper 补充 `platform_windows_test.go` 校验无 reparse 句柄钉住与 PowerShell 进程/端口 JSON 解析。

## E06–E07 多机汇总中心（Reporting v1、Go Server 与 Center Web）

- **Reporting v1 与本地导出**：`api/codexpulse/reporting/v1` 放行 `provider="agy"`、`source_kind="agy_local"`、`quota.source="agy_ls_rpc"`、`DeviceStatus` 5 路上限及 `step_metadata` TPS 胶囊；本地导出支持 AGY 会话/贡献（含 `agy-api-2026-11-25` 版本化分阶段费用）、缓存/吞吐量胶囊、非敏感账号摘要（64 位 `account_scope`，无邮箱）与多配额池观测增量/全量补传，显式禁用遵循 `TOO-715` 语义不误报异常。
- **中心 Go Server 与零 DDL 迁移**：已核验 `server/docs/sqls/schema/` 下 MySQL / SQLite DDL 无 `provider` CHECK/ENUM 限制，零 DDL 迁移；`reporting_srv`、`quota_srv`、`statistics_srv`、`catalog_srv` 全量支持 AGY 多设备去重、多池配额不跨设备相加查询、统计/缓存/TPS 聚合与同口径版本化价目表。
- **中心 Web（`server/web/`）**：全页面支持五路筛选与 `Antigravity` 展示、零原始键泄漏、多配额池分组卡片与按池节奏切换、AGY 费用“模型 API 公价估算”标注、以及版本化分段参考价目表。
