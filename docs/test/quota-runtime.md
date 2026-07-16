# 在线 Quota Application Runtime Runbook

## 当前验证结果

- 记录时间：2026-07-16（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-306 production quota composition、confirmed Home credential lease、lifecycle/settings/manual/shutdown 闭环
- 当前结论：`PASS（已合并并完成 post-merge verify）`；implementation/final reviewers 均 `blocking_findings=0`；PR #30 已合并为 `35df243`，main post-merge 全部门禁通过，Linear TOO-306 已读回 Done。
- 自动化入口：`internal/app/quota_credentials_test.go`、`internal/app/quota_runtime_test.go`、`internal/app/app_test.go`
- 对应计划 / issue：`.agents/plans/2026-07-16-too-306-quota-runtime.md` / TOO-306
- 结果说明：全部 HTTP、auth 和 Preferences 输入均为 synthetic fixture；SQLite 位于 `testing.T.TempDir()`。证据证明 local scheduler 在 target Preferences CAS 前持久化 fence，Drain 同时等待普通 slice 与 Recover/Retry preflight target writer；最终 generation CAS 保留 pause/sleep，旧 queued task 不再 runnable、新 generation task可进入，same-process 与 restart 均收敛。第五次同 reviewer 已确认全部 implementation findings 关闭；分支与 main post-merge 的 focused/full/race/harness/package 门禁均通过。未读取真实 Codex Home，未访问真实 Wham；Actions 保持停用。

### 本次执行结果

- 执行时间：2026-07-16
- 执行目录：仓库根目录
- 本次结论：`PASS（含 main post-merge verify）`
- 影响范围：Go/npm build/test cache、测试临时目录中的权限受控 auth fixture 与 Pure-Go SQLite/WAL/SHM；完整验证生成 ignored frontend、task 和 package 产物。
- 清理结果：临时测试文件由 Go test 自动清理；锁定 Wails CLI 仅保留在临时工具目录。已通过 scoped ignored clean 删除 frontend dependencies/dist、task 和 package 产物，保留 tracked `.gitkeep`；lockfiles、Go modules 与 generated bindings 无漂移。
- 敏感信息处理：只使用固定 synthetic marker；提交版不记录真实 token、Authorization/Cookie、auth 内容、原始 response、临时路径或 SQLite 内部行 ID。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| credential/path/privacy focused | PASS | no-follow、普通文件/大小、duplicate key、Home switch、取消、buffer 清零 |
| runtime startup/recovery/toggle/restart | PASS | synthetic app integration |
| lifecycle/settings/manual/shutdown | PASS | 真实 Preferences CAS、typed post-commit error、Home generation handoff、quota/application admission fence |
| focused repeat `count=20` | PASS | app 27.595s；quota 2.435s；scheduler 15.373s；Store 66.401s；app 仅既有 macOS deployment linker warning |
| focused race `count=10` | PASS | app 100.484s；quota 12.449s；scheduler 46.747s；Store 302.757s；无 race report |
| local generation handoff focused | PASS | lifecycle count20 9.875s；app count5 6.524s；两包 race2 lifecycle 8.727s/app 20.720s |
| scheduler preflight drain focused | PASS | 主线程 Recover/Retry barrier count20 2.548s、race count5 5.877s；reviewer 独立 count50 10.683s、race count10 16.720s |
| 首次 review 前 focused repeat | 历史 PASS | app 20 轮 9.021s；quota 1.639s；scheduler 12.377s；Store 63.398s |
| 首次 review 前 focused race | 历史 PASS | app 10 轮 54.104s；quota 15.510s；scheduler 48.254s；Store 281.194s |
| 全仓 test/race/vet/tidy | PASS | `go test ./... -count=1`、串行 `go test -race ./... -count=1`、`go vet ./...`、`go mod tidy -diff` 全部通过 |
| Pure-Go/GORM/cron/privacy guards | PASS | 禁用 driver 未进入编译链；新增生产文件无 raw SQL/自写周期 loop；robfig v3.0.1 |
| harness/project/version | PASS | harness、project contract、generated contract 通过；精确 Wails `v3.0.0-alpha2.117`；version `findings=[]` |
| 完整 `make verify` | PASS | 恢复 lockfile 冻结的 ignored npm dependencies 后通过 Go、前端 5 tests/build、bindings stability、arm64/minOS 15 app/ZIP；产物已 scoped clean |
| final scope review | PASS | 不同 subagent P0-P3 均 none，`blocking_findings=0`；独立 gofmt/diff、focused repeat/race、CHANGELOG/privacy/dependency guards PASS |
| PR / merge / post-merge | PASS | PR #30 已合并为 `35df243`；main 全仓 test/race、harness/project/version/CHANGELOG truth 与完整 `make verify` 通过，Linear TOO-306 已读回 Done |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

## 目标

- 证明 `app.Run -> startApplicationLifecycleRuntime` 真实构造并持有 Quota/Reset Credits runtime，不只存在 package-level library。
- 证明 credential 只来自当前 confirmed Home 的固定 `auth.json`，路径/文件替换和 Home 切换 fail closed，调用结束后可写副本清零。
- 证明 startup、disable/re-enable、auth recovery、foreground、manual、restart 与 shutdown 都复用 durable coordinator/Store truth。
- 证明 callback 不直接做文件、SQLite 或 HTTP，周期唤醒只使用 `github.com/robfig/cron/v3 v3.0.1`。
- 本 runbook 是 synthetic app integration，不是 live auth/Wham E2E。

## 执行副作用

- 可能写入的本地文件：Go build/test cache；完整 `make verify` 可能生成仓库已忽略的 frontend/build/package 产物。
- 可能访问的服务 / 数据库 / 外部系统：功能测试只使用内存 fake transport 和临时 Pure-Go SQLite；完整依赖恢复可能访问公共 package registry。
- 可能创建的临时数据：权限受控的 synthetic `auth.json`、Preferences snapshot、quota observations/current、Reset Credits、source attempt/state、refresh schedule/claim 与 SQLite sidecar。
- 明确不会触达的范围：真实 Codex Home、真实 credential、真实 Wham、用户数据库、GitHub Actions、release/tag。
- 如果测试路径改成用户 Home、默认应用数据库或真实 transport，立即停止并重新确认授权。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：待验证的 TOO-306 分支或包含其变更的主分支。
3. 必需命令：项目指定 Go toolchain、`make`、`git`、`rg`、`python3`；完整 gate 需要仓库锁定的 Wails CLI 与前端依赖。
4. 必需配置：无；禁止注入用户 credential、默认应用数据库或真实 endpoint transport。
5. 必需测试环境：`internal/app` 在 macOS 默认 CGO 下验证 Wails composition；quota/scheduler/store 使用 `CGO_ENABLED=0` 证明 SQLite Pure-Go 编译链。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

git status --short --branch
go version
```

预期结果：目录、分支和工具链正确；命令不读取或打印 credential、auth 文件、HTTP body 或用户数据库。

## 主路径

### 1. Application composition focused repeat

```bash
go test ./internal/app \
  -run 'Quota|ResetCredits|Credential|Lifecycle|Application' \
  -count=20
```

预期结果：production constructor、credential fail-closed/privacy、startup/off/recovery/restart、lifecycle/settings/manual/shutdown 在真实临时 Store 上稳定通过。`internal/app` 依赖 Wails mac package，不能用 `CGO_ENABLED=0` 编译；这不是 SQLite driver 降级。

### 2. Core Pure-Go repeat 与 race

```bash
CGO_ENABLED=0 go test ./internal/codex/quota ./internal/scheduler ./internal/store \
  -run 'Quota|ResetCredits|Credential|Lifecycle|Application' \
  -count=20

go test -race ./internal/app ./internal/codex/quota ./internal/scheduler ./internal/store \
  -run 'Quota|ResetCredits|Credential|Lifecycle|Application' \
  -count=10

if CGO_ENABLED=0 go list -deps ./internal/store ./internal/codex/quota ./internal/scheduler \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
```

预期结果：repeat/race 退出 0；Pure-Go 编译依赖不包含官方 GORM sqlite driver 或 mattn，继续使用 `github.com/libtnb/sqlite` 与 `modernc.org/sqlite`。

### 3. Composition、隐私、GORM 与 cron guard

```bash
rg -n 'startApplicationQuotaRuntime|NewQuotaRefreshRunner|ReconcileQuotaPreferences|RequestQuotaRefresh' \
  internal/app/lifecycle_runtime.go internal/app/quota_runtime.go

if rg -n '\.(Raw|Exec)\(' \
  internal/app/quota_credentials_darwin.go \
  internal/app/quota_runtime.go \
  internal/app/lifecycle_runtime.go; then
  exit 1
fi

if rg -n 'time\.(NewTicker|Tick|Sleep|After|NewTimer)' \
  internal/app/quota_credentials_darwin.go \
  internal/app/quota_runtime.go \
  internal/app/lifecycle_runtime.go; then
  exit 1
fi

rg -n '^\s*github.com/robfig/cron/v3 v3.0.1$' go.mod

if rg -n 'Bearer [A-Za-z0-9+/=_-]{12,}|http://127\.0\.0\.1:[0-9]+|/Users/' \
  docs/test/quota-runtime.md \
  docs/design/details/quota/README.md \
  docs/design/details/scheduling-and-bootstrap/README.md | rg -v ':(if )?rg -n '; then
  exit 1
fi
```

预期结果：production app 明确引用 quota runtime；新增业务文件无 raw SQL、无自写周期 timer/ticker/sleep；robfig 精确锁定 v3.0.1；提交版文档不含真实 token、临时端口或机器路径。

### 4. 全仓与控制面门禁

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
git diff --check
make harness-verify
PATH="/tmp/codex-pulse-tools/bin:$PATH" make verify-project
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
PATH="/tmp/codex-pulse-tools/bin:$PATH" make verify
```

预期结果：全部退出 0；允许记录既有 macOS deployment linker warning，但不得掩盖新 error。若精确 Wails CLI 不在本机，先按仓库 gate 恢复到临时工具目录，不用未锁定版本替代。Actions 仍不是验证入口。

本次执行说明：首次 `verify-project` 因当前 PATH 缺少 Wails CLI，以 `TOOLCHAIN-001` 在构建前正确停止；随后将仓库精确锁定的 `v3.0.0-alpha2.117` 恢复到临时工具目录，并通过版本输出与 binary module metadata 双重读回。`frontend/node_modules` 缺失时按 lockfile 执行 `npm ci`，199 packages、audit 0；既有 `glob@10.5.0` deprecated warning 与 macOS SDK deployment warning 不作为新增失败，也不在本卡升级依赖。完整验证后已清理所有 ignored 构建产物。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| credential/privacy | 任一 symlink/replace/Home-switch 绕过，或 token/auth marker 进入错误/Store | 记录固定测试名和 content-free failure class | 修复 provider 后重跑步骤 1～3 |
| startup/recovery | 缺 credential 阻止 app、重启重复请求、enable/disable 丢 history | 记录来源、trigger、reason，不记录 request/auth 内容 | 修复 composition/coordinator 接线后重跑步骤 1～2 |
| lifecycle/shutdown | callback 做 I/O、关闭后仍接纳、in-flight 不 drain | 记录稳定事件类型和 gate | 修复 admission/close order 后重跑步骤 1～2 |
| Pure-Go/GORM/cron | 禁用 driver、raw SQL 或自写周期 loop 进入新增代码 | 记录文件/依赖名 | 修复后重跑步骤 2～4 |
| harness/build | 任一 gate 非零 | 记录 gate 名与脱敏摘要 | 修复后从步骤 4 完整重跑 |

## 清理

```bash
git status --short
```

预期结果：`testing.T.TempDir()` 自动清理 auth fixture、SQLite/WAL/SHM；若 `make verify` 生成 ignored build/frontend/package 产物，按仓库既有清理流程删除，保留 tracked `.gitkeep`。本 runbook 不创建需要 revoke 的 token，不启动 server，也不产生外部数据。

## 结果回写

每次执行后更新本文前部的结论、步骤表和清理结果；只写脱敏摘要。不得写真实 token、Authorization/Cookie、auth JSON、原始 response、机器绝对路径、临时目录或内部 SQLite 行 ID。Actions 保持 `actions_disabled_by_user`，不查询、不触发、不等待；普通 Execution 不发布。
