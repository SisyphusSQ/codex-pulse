# Codex 账号切换隔离 Runbook

本 runbook 验证：在单个已确认 Codex Home 内顺序切换 ChatGPT 账号时，当前在线额度、Reset Credits 和账号展示严格隔离；本地 Session、Token、项目、趋势和成本继续按 Home 聚合。

对应计划：`docs/superpowers/plans/2026-09-14-too-442-account-binding.md`。对应 Issue：TOO-442。

## 当前验证结果

- 记录时间：2026-09-14
- 记录目录：仓库根目录
- 本轮任务性质：TOO-442 账号绑定隔离
- 当前结论：`INCOMPLETE`（synthetic A-B-A PASS；真实 Home A-B-A 见下方 live 步骤，不得用 synthetic 冒充）
- 自动化入口：
  - `go test -race ./internal/app ./internal/scheduler ./internal/store ./internal/codex/...`
  - `swift run --package-path app/macos codex-pulse-app-tests`
- 对应计划 / issue：TOO-442
- 结果说明：单元与集成使用 `acct-test-*` / `acct-secret-*` 合成身份。真实账号切换必须由用户亲自完成，应用不得代登、代切或写入 Codex credential store。

### 本次执行结果

- 执行时间：
- 执行目录：`/Users/suqing/Coding/golang/00_self/codex-pulse`
- 本次结论：`INCOMPLETE`
- 影响范围：私有 runtime、SQLite、preferences、App Server housekeeping（仅 live 步骤）
- 清理结果：
- 敏感信息处理：不记录 token、原始 `accountId`、真实邮箱、Reset Credit 原始 ID 或原始 JSONL。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| 前置条件与授权边界 | 未执行 | live 前必须口头说明副作用 |
| A 登录态与 quota/account key 读回 | 未执行 | |
| 用户切到 B，应用只观察 | 未执行 | 无第二个真实账号则整表 `NOT_RUN` |
| B 首次失败显示 unknown/pending | 未执行 | |
| B 成功后旧 A 在线数据不可见 | 未执行 | |
| 用户切回 A：历史时间戳恢复、generation 改变 | 未执行 | |
| SQLite / 日志 / Proto 隐私扫描 | 未执行 | |
| synthetic A-B-A | PASS | `go test ./internal/app -run 'TestAccountBinding'` 与 Swift account-context 测试；完整 race 套件见计划 Task 11 |
| 第二个账号不可用 | | 标记 `NOT_RUN`，不得用 synthetic 冒充 live |

结论取值只能是 `PASS` / `FAIL` / `ERROR` / `NOT_RUN` / `INCOMPLETE`。

## 目标

- 当前 confirmed `(account_scope, binding_generation)` 是在线额度、Reset Credits、账号邮箱/套餐的唯一展示边界。
- 新账号第一次确认或刷新失败时显示 unknown/pending，不复用旧账号额度，不伪造 `0%` 或 `100%`。
- A→B→A 恢复 A 的历史观察时间戳，但使用新的 binding generation。
- legacy `account_scope=default` 保持 unassigned。
- 本地 Session / Token / 项目 / 趋势 / 成本仍按当前 Codex Home 聚合。
- Cursor、Grok、API Subscription 不受 Codex binding 改动影响。

## 执行副作用

- 可能读取：真实 Codex Home 下的 Session / JSONL、App Server `account/rateLimits/read` 与 `account/read`。
- 可能写入：mode `0700` 的私有 runtime、SQLite、`preferences.json`、App Server 标准 housekeeping。
- 明确不会：登录、登出、代用户切换账号、读取 access token / JWT / `auth.json` 内容 / Keychain、直连私有 WHAM endpoint。
- 执行 `make verify-live` 前必须再次说明上述副作用。

## 前置条件

1. 当前工作目录：`/Users/suqing/Coding/golang/00_self/codex-pulse`
2. 当前分支：`suqing/too-442-account-binding`
3. 活跃 Codex CLI 应满足稳定能力基线（含 `account/rateLimits/read.accountId`）。当前验证版本 0.154.0。GUI resolver 必须能找到 `$HOME/.local/bin/codex`，不得硬编码用户名或绝对路径。
4. 用户已授权本仓库本地 App 使用真实 Codex Home；每次执行脚本前仍须说明会读 Session/JSONL。
5. Live A-B-A 需要两个真实 ChatGPT 账号。只有一个账号时整条 live 标记 `NOT_RUN`。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

echo "REPO_ROOT=$REPO_ROOT"
echo "BRANCH=$(git branch --show-current)"
```

预期结果：工作目录和分支正确；输出不含真实邮箱、token 或 Home 文件内容。

## 主路径

### 1. 前置检查

```bash
git branch --show-current
git status --short
```

确认分支为 `suqing/too-442-account-binding`。说明即将读取真实 Session/JSONL，并可能写私有 runtime、SQLite、preferences 与 App Server housekeeping。

### 2. A 登录状态读回

由用户确认当前 ChatGPT 账号为 A。启动 development App（`make verify-live` 或等价真实 Home 启动）。读回：

- Helper 实际 Codex binary path / version / capability state
- 当前 `account_scope`（64 位 lowercase hex）与 `binding_generation`
- Overview 账号邮箱与在线额度百分比
- 至少一条 A 的额度历史时间戳（只记相对顺序，不记原始 JSONL）

应用只观察，不代替用户登录。

### 3. 用户自行切到 B

用户在 Codex / ChatGPT 客户端完成切号。应用不得注入凭证。观察 pending / 账号确认中，旧 A 百分比必须消失。

### 4. B 首次失败

若 B 第一次额度刷新失败：界面必须是 unknown/pending，不能出现 A 的百分比，也不能出现伪造的 `0%` 或 `100%`。

### 5. B 成功

B 成功后：

- quota / pace / account 的 context key 一致
- A 的在线额度、Reset Credits、邮箱不可见
- 本地 Session / 项目 / 趋势仍是同一 Codex Home 聚合，不出现账号筛选器

### 6. 用户切回 A

用户自行切回 A。读回：

- `account_scope` 与步骤 2 的 A scope 相同
- `binding_generation` 严格大于步骤 2 与步骤 5
- A 历史观察时间戳恢复
- 不得命中切换前旧 generation 的 Overview cache

### 7. 隐私扫描

在不导出真实 Home 的前提下，检查本机 runtime SQLite、结构化日志和 Core/Proto 读回：

- 不得出现原始 `accountId`、access token、JWT、Reset Credit 原始 ID、真实邮箱
- 只允许 64 位 hex `account_scope` 与 credit hash

`.artifacts/` 与真实 Home 数据不得提交。

### 8. 第二个账号不可用

若无法准备两个真实账号：本 runbook 的 live 各步全部记 `NOT_RUN`。不得把 `go test` / `make verify` / isolated Home 结果改写成 live `PASS`。

## 清理

关闭 development App。不删除本机 Codex CLI 安装。不提交 `.artifacts`、真实路径或原始 JSONL。
