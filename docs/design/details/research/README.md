# Research Notes

这是一份调研快照，不代表相关项目的当前最新状态。进入实现前，容易变化的版本、许可证、接口和发布能力需要重新读回验证。

## 调研范围

- 2026-06-29：GitHub 检索 Codex、Claude Code、OpenCode、Gemini CLI、Copilot 和通用 coding agent tracker/monitor/session browser，并抽样阅读 README 与源码。
- 2026-07-03：阅读 Codex Runway README、截图和 Swift 源码结构，判断可复用的产品能力与原生 macOS 交互方向。
- 2026-07-11：结合 sqmc04 上 Codex Runway `0.0.16` 的运行表现与偏好，补充 quota 可信性、刷新和更新设计。

## 市场形态

现有 agent tracker 大体分为：

1. 用量账本：解析本地 JSONL/SQLite，按日期、Session、模型聚合 token 和 cost。
2. live 运行态：扫描进程、打开文件、端口、Git 和最近活动。
3. 菜单栏/浮窗：展示额度、reset 和 burn rate。
4. Session 浏览/搜索/归档：把 transcript 归一化到 SQLite，支持搜索、导出和同步。
5. 远程/移动控制：手机查看、远程输入、终端切换和审批。

早期调研建议做通用“本机 agent 观测中心”；正式 v0.1 已收敛为 Codex-only，但 schema 和 provider 边界保留未来扩展空间。

## 代表项目

| 项目 | 定位 | 技术 / 数据源 | 借鉴点 |
| --- | --- | --- | --- |
| [ccusage](https://github.com/ccusage/ccusage) | 多 agent token/cost CLI | Rust/TS，本地 JSONL | 多 provider、周期/Session 聚合、JSON、5h block |
| [agentsview](https://github.com/kenn-io/agentsview) | 本地 Session 搜索分析 | Go、SQLite、多 agent roots | Provider、FTS5、SSE、Session browser |
| [abtop](https://github.com/graykode/abtop) | agents htop | Rust、进程/lsof/JSONL/Git | live Session、端口、子进程、quota |
| [codex-usage-tracker](https://github.com/douglasmonsky/codex-usage-tracker) | Codex 用量诊断 | Python、JSONL、SQLite | aggregate-only 隐私、按需上下文 |
| [CodexMonitor](https://github.com/Dimillian/CodexMonitor) | Codex command center | Tauri/React/Rust、app-server | thread/workspace/worktree/daemon/iOS |
| [ai-limit](https://github.com/zhuchenxi113/ai-limit) | Claude + Codex quota | Python、cookie/app-server/JSONL | 区分只读接口和有副作用 fallback |
| [codex-reset-watcher](https://github.com/jordan-edai/codex-reset-watcher) | Codex reset 菜单栏 | Swift、auth + backend | 不读 cookie、多账号 snapshot、派生字段 |
| [codex-runway](https://github.com/Licoy/codex-runway) | Codex quota/成本/Session 菜单栏 | SwiftUI、auth/wham/JSONL | 最接近目标的产品样本；AGPL-3.0，不作代码底座 |
| [splitrail](https://github.com/Piebald-AI/splitrail) | 跨 agent usage/cost | Rust、多 agent sources | CLI/VSCode/Cloud/MCP 和 provider 覆盖 |
| [VibePulse](https://github.com/wesm/vibepulse) | macOS spend tracker | Swift，调用 agentsview | UI 复用成熟采集器 |
| [CCOwl](https://github.com/sivchari/ccowl) | Claude status bar | Go，调用 ccusage | Go 桌面层的轻量路径 |
| [claude-code-monitor](https://github.com/onikan27/claude-code-monitor) | Claude TUI + 手机控制 | TS、hooks/WebSocket | QR、远程消息、终端聚焦、token auth |

## 数据源模式

### 本地 transcript / usage

- Codex：`~/.codex/sessions/`、`~/.codex/archived_sessions/` 和 `session_index.jsonl`。
- Claude Code：`~/.claude/projects/`，部分实现支持 `CLAUDE_CONFIG_DIR`。
- OpenCode：`~/.local/share/opencode/`，可能是 JSON 或 SQLite。
- Gemini/Qwen/Copilot：目录不同，可参考 ccusage/agentsview 的 provider 实现。

Codex JSONL 常见事件包括 `session_meta`、`turn_context`、`event_msg.token_count`、user/agent message、task lifecycle 和 function calls。Codex Pulse 只选择 session、turn、usage、quota、project/model 等结构化字段，不保存正文。

### 进程与运行态

abtop 的模式可作为后续阶段参考：

- `ps` 查找 agent 进程；
- macOS 用 `lsof` 把 PID 映射到打开的 rollout JSONL；
- Linux 扫 `/proc/{pid}/fd`，Windows 退化为最近文件；
- `lsof -i` 查找 agent 派生端口；
- transcript mtime 可作 freshness，但 v0.1 不用它推断 Waiting/Done/Error。

### 配额接口

Codex 常见三条路径：

1. JSONL `token_count.rate_limits`：无额外副作用，但可能不实时。
2. `chatgpt.com/backend-api/codex/usage` 或 `wham/usage`：内部只读接口，需要 auth/cookie，可能变化。
3. `codex app-server` 的 `account/rateLimits/read`：可实时读，但已有实现提示 initialize 可能影响 5h window。

因此 v0.1 只采用 1 + 可由用户关闭的 `wham/usage`，明确排除 app-server。

## Codex Runway 快照

调研时确认的能力：

- 状态栏展示 5h、weekly、additional windows 和 reset。
- 从 `auth.json` 读取 bearer，调用 `wham/usage`、reset credits 和 daily workspace usage。
- 扫描 sessions/archived_sessions，按模型、项目和日期聚合 token，折算 API 等价成本。
- 读取 session index，并抽样 JSONL 头尾获得 title、cwd、更新时间、状态和 token。
- 可修复 session index，写前备份。
- 有设置、语言/外观、刷新间隔、JSON 导出、更新检测、通知和自检。

可借鉴：quota/reset credits、版本化 API 等价成本、最近 Session、显式 session-index repair、轻量 Tray、自动检查但由用户决定安装的更新体验。

需要重新设计：

- 窄 Popover 只适合快速查看，分析应进入原生主窗口。
- 内部接口必须显示来源、时间和失败状态。
- 网络失败或可疑响应不能覆盖 last-known-good，也不能显示伪造 100%。
- token/auth 不落库、不写日志。
- 不复刻其 Session 状态猜测，只保留 turn lifecycle 和 freshness。
- 成本必须可按日期、模型、项目和 Session 下钻。

Codex Runway 是 Swift/SwiftUI 且采用 AGPL-3.0。Codex Pulse 只把它当能力和交互样本，不复制源码、图标或高度相似 UI。

## 对 Codex Pulse 的结论

- Go Helper 保留本地数据、索引、调度和 SQLite ownership；Swift native client 负责窗口、菜单栏、更新与进程托管，双方只通过版本化 Protobuf/gRPC over UDS 交互。
- local-first JSONL + SQLite 是最稳定的底座；online quota 是默认开启、可关闭的增强层。
- Tray 只承担摘要，主窗口承载可信解释、筛选、表格和下钻。
- 采集、调度、存储和 UI 必须分层，避免每次打开窗口重新扫描源文件。
- provider 抽象值得保留，但首版不为了多 agent 提前增加实现复杂度。

## 参考链接

- 其余项目链接见上方代表项目表。
