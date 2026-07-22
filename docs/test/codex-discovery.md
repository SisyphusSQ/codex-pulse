# Codex 文件发现与来源对账 Runbook

## 当前验证结果

- 记录时间：2026-07-14（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-251 filesystem discovery / fingerprint / pure reconcile contract
- 当前结论：`PASS`；PR #12 已合并为 `b36533b`，TOO-251 已完成 main post-merge verify 与 Linear Done。
- 自动化入口：`internal/codex/logs/*_test.go`
- 对应 issue：TOO-251
- 结果说明：不同 final scope reviewer 发现的 identity-first 组合匹配、递归子目录 ENOENT 和 confirmed-home/allowlist 输入校验 3 个 blocking Medium 已完成代码与测试修复；原 implementation reviewer 复审返回 `ZERO_FINDINGS / blocking_findings: 0 / READY_FOR_CHANGELOG`。按 mandatory skills 写入唯一 `[TOO-251]` `Unreleased -> feature` 条目后，focused 20 次、race、83.0% coverage、全仓 test/race/vet、Pure Go Store guard、harness/project/version/diff、CHANGELOG/敏感扫描与 exact Wails `make verify` 全部通过；不同 final reviewer 复审返回 `ZERO_FINDINGS / blocking_findings: 0 / READY_TO_COMMIT`。PR #12 合并后在 main 重跑必要门禁并通过，临时产物已清理。

### 本次执行结果

- 执行时间：2026-07-14
- 执行目录：仓库根目录
- 本次结论：`本地验证通过`
- 影响范围：Go/Node/工具缓存、测试进程自动管理的临时目录、lockfile frontend dependencies 和临时 Wails arm64 app/archive；未读取真实 Codex Home。
- 清理结果：`t.TempDir()` fixture 自动清理；临时 Wails CLI、frontend dependencies、`bin`、`dist` app 产物均已清理，tracked `.gitkeep` 保留；仓库未保留 fixture 或原始 JSONL。
- 敏感信息处理：fixture 只使用 synthetic 内容；fingerprint 结果只保留 SHA-256，不写真实路径、凭据、token、cookie、原始 JSONL 或机器本地临时目录。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| 前置检查 | 通过 | 目标为 macOS；包在 `CGO_ENABLED=0` 下构建。 |
| focused 主路径 | 通过 | `-count=20`；新增 move + old-path-reuse、递归目录消失、outside/root arbitrary/kind mismatch、越界 issue 与 nil receiver。 |
| focused race | 通过 | `CGO_ENABLED=0 go test -race ./internal/codex/logs -count=1`。 |
| focused coverage | 通过 | 83.0%。 |
| Pure Go Store guard | 通过 | `CGO_ENABLED=0 go test ./internal/store/... -count=1`；production dependency path 不含两个禁用驱动。 |
| 全仓 test/race/vet | 通过 | `go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`。 |
| harness/project/version/diff | 通过 | harness、project check、release policy dry-run 与 whitespace gate 通过。 |
| 完整 `make verify` | 通过 | exact Wails `v3.0.0-alpha2.117`；前端、bindings、macOS arm64/minOS 15 app、ad-hoc 签名与 ZIP 复验通过。 |
| implementation re-review | 通过 | 原 reviewer 返回 `ZERO_FINDINGS / blocking_findings: 0 / READY_FOR_CHANGELOG`。 |
| post-integration verify | 通过 | 含 CHANGELOG 的完整 10 文件 diff 通过 focused/race/full/Store/harness/project/version/diff/exact Wails 权威矩阵。 |
| final scope review | 通过 | 不同 subagent `/root/too_251_final_scope_review` 返回 `ZERO_FINDINGS / blocking_findings: 0 / READY_TO_COMMIT`；三项原 finding、scope 与 CHANGELOG gate 均关闭。 |
| post-merge | 通过 | PR #12 合并为 `b36533b`；main 上必要 test/race/vet、harness/project/version/diff 与 exact Wails 验证通过，Linear 已读回 Done。 |
| 清理 | 通过 | fixture、临时 CLI、frontend dependencies、dist 和 package 生成物均已清理；`.gitkeep` 保留，tracked module/lockfile/bindings 无漂移。 |

## 目标

- 验证目标：证明 discovery 只读 allowlisted Codex JSONL，fingerprint 不保存原文，reconcile 对文件变化和不确定状态给出确定、可恢复的 plan。
- 成功标准：功能/安全矩阵全部通过；同一输入 plan 稳定；partial scan 不误报 deleted；包在 Pure Go 与 race 下通过；不触达真实用户数据。
- 本 runbook 可由 agent 或工程师直接执行，不是泛化 QA 说明。

## 执行副作用

- 可能写入的本地文件：Go build/test cache；Go test 自动创建的临时 fixture。
- 可能访问的服务 / 数据库 / 外部系统：无。
- 可能创建的临时数据：synthetic `sessions/`、`archived_sessions/`、`session_index.jsonl`、symlink、hardlink 和 named pipe fixture。
- 明确不会触达的范围：真实 `~/.codex` / `CODEX_HOME`、SQLite 用户数据库、网络、GitHub Actions、release、用户原始 JSONL。
- 执行前必须说明上述副作用和影响范围；如果命令被改为指向真实 Codex Home，立即停止。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：待验证的 TOO-251 分支或包含其变更的主分支。
3. 必需命令：Go 1.25 toolchain、`make`、`git`。
4. 必需配置：无；不得设置测试去扫描真实 `CODEX_HOME`。
5. 必需测试环境：macOS；本卡的 no-follow adapter 使用 Darwin `openat`。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

git status --short --branch
go version
```

预期结果：

- 当前目录解析为待验证仓库。
- 用户已有改动已识别并受到保护。
- 命令不打印或读取任何凭据。

## 主路径

### 1. Focused 功能矩阵

```bash
CGO_ENABLED=0 go test ./internal/codex/logs -count=1
CGO_ENABLED=0 go test ./internal/codex/logs -count=10
```

预期结果：

- allowlisted 三类来源、稳定 SourceFileID/fingerprint、filter 和空目录通过。
- confirmed home replacement、父 symlink retarget、目录 symlink 竞态、文件 symlink、hardlink duplicate identity、named pipe、permission、扫描变化和 cancel 均 fail closed。
- added / unchanged / grown / truncated / moved / replaced / deleted / unreadable 决策通过；identity pass 先于 path pass，move + old-path-reuse 不重复消费 previous。
- 同 size/mtime 内容变化识别为 replaced；短文件 append 只有 previous-prefix proof 一致时识别为 grown，改写后增长或 proof 缺失时为 replaced。
- planner 与 `DiscoverAgainst` 拒绝 confirmed home 外、其它根级 JSONL、kind/path mismatch 和越界 issue。

### 2. Race 与全仓回归

```bash
CGO_ENABLED=0 go test -race ./internal/codex/logs -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
```

预期结果：

- discovery 包在 Pure Go race 下通过。
- 全仓测试、race 和 vet 通过；macOS linker deployment-target warning 可记录，但不能把真实失败降级为 warning。

### 3. Harness 与 diff gate

```bash
make verify-architecture
git diff --check
```

预期结果：

- harness/project/version gate 通过且 version findings 为空。
- diff 没有 whitespace error。
- 本卡不创建 release 版本段。

### 4. 完整项目验证

前置条件：`wails3 v3.0.0-alpha2.117` 已在当前 `PATH`，frontend dependencies 已按 lockfile 安装。提交版命令不固定机器本地工具路径。

```bash
make verify
```

预期结果：

- project checks、Go test/vet、frontend typecheck/test/build、bindings stability 全部通过。
- 生成并验证 macOS arm64、minOS 15、ad-hoc 签名的 app 与 archive。
- 本卡不执行正式发布。

### 5. 清理

```bash
git status --short --branch
```

预期结果：

- Go test 的 TempDir fixture 已自动清理。
- 除本卡预期源码、测试、文档和 CHANGELOG 外没有新增 tracked 或 untracked 产物。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| home/path safety | symlink 被跟随、包外内容被读取 | blocking finding；只记录 synthetic path 与 issue code | 修复 no-follow/allowlist 后从 focused test 重跑 |
| fingerprint | 返回原始前缀、同 size 内容变化未识别、无旧前缀证明仍判 grown | blocking finding；不得保存原文证据 | 修复 digest/comparison proof 后重跑功能矩阵 |
| partial scan | permission/subtree failure 被误报 deleted | blocking finding | 修复 issue scope 后重跑 reconcile matrix |
| race / full test | 任一真实失败 | 记录失败命令与脱敏摘要 | 在同一分支修复后完整重跑 |
| 清理 | fixture 或构建产物残留 | 停止 closeout | 只清理确认由本 runbook 生成的路径，再读回状态 |

## 结果回写

- 每轮完成后更新本文顶部结果和步骤状态。
- 只提交脱敏摘要；原始命令输出留在本地 run 记录或 Linear comment。
- GitHub Actions 处于 `actions_disabled_by_user` 时不触发、不等待，也不把空检查伪装成 CI 通过。
- 普通 Execution 不发布；TOO-251 只更新 `CHANGELOG.md -> Unreleased`。
