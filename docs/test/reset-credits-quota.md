# Reset Credits 与 Quota 调度退避 Runbook

## 当前验证结果

- 记录时间：2026-07-16（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-265 Reset Credits read-only provider、动态汇总、quota/reset-credits durable refresh schedule、退避与 robfig cron runner
- 当前结论：`PASS（已合并并完成 post-merge verify）`；两项 final scope P1 均已按 TDD 修复并复审关闭，`blocking_findings=0`；PR #28 已合并为 `f953f74`，main post-merge 门禁通过，Linear TOO-265 已读回 Done。
- 自动化入口：`internal/codex/quota/reset_credits_*_test.go`、`internal/codex/quota/schedule_test.go`、`internal/store/reset_credits_test.go`、`internal/store/source_refresh_schedule_test.go`、`internal/store/quota_schedule_migration_test.go`、`internal/scheduler/quota_refresh_test.go`
- 对应计划 / issue：`.agents/plans/2026-07-15-too-265-reset-credits-quota-schedule.md` / TOO-265
- 结果说明：Reset Credits payload、quota current、source attempt/state、durable schedule/claim fence 与 cron lifecycle 都使用 synthetic fixture。已证明固定只读 endpoint、content-free facts、动态到期、v11→v12、exact replay、last-known-good、cadence、5/10/20/30 分钟退避、独立且取较晚值的 Retry-After fence、60 秒 manual throttle、foreground/wake 错误退避、startup/recovery/reconcile 保留 durable due、取消、preferences disable、启动后 lease 到期恢复、attempt-first/release-first crash gap 与迟到 attempt 隔离；未读取真实 auth，未发真实网络请求。

### 本次执行结果

- 执行时间：2026-07-16
- 执行目录：仓库根目录
- 本次结论：`PASS（含 main post-merge verify）`；CHANGELOG 集成后的 focused repeat/race、全仓 race、静态/版本守卫与完整 `make verify` 均退出 0。
- 影响范围：Go build/test cache、`testing.T.TempDir()` 下的 Pure-Go SQLite/WAL/SHM，以及完整验证生成的 ignored frontend dependencies/dist、`.task/`、`bin/` package 产物。
- 清理结果：测试临时数据库由 Go 自动清理；本轮 ignored `frontend/node_modules`、dist build、`.task/` 与 `bin/` 已删除，tracked `.gitkeep` 保留；未生成用户数据库、网络抓包、auth 副本或 response transcript。
- 敏感信息处理：测试只注入 synthetic token/credit/request/source 值；client 结果和 SQLite 只保留 credit ID SHA-256，不保存 token、Authorization、Cookie、原始 response body、title、description、profile user 或本机临时路径。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| focused domain/store/scheduler | PASS | Reset Credits、reset calculator、schedule/claim/policy/coordinator/cron |
| schema v12 / migration | PASS | fresh schema、v11→v12、失败原子回滚、冻结 checksum |
| 全仓 `go test ./... -count=1` | PASS | 仅有既有 macOS deployment linker warning |
| `go vet ./...` | PASS | 无输出，退出 0 |
| focused repeat | PASS | post-integration `-count=20`：quota 2.623s、Store 11.971s、scheduler 10.383s |
| focused race | PASS | post-integration `-race -count=10`：quota 11.737s、Store 45.048s、scheduler 34.359s |
| 全仓 `go test -race ./...` | PASS | quota 9.094s、Store 78.747s、scheduler 32.877s；仅既有 macOS linker warning |
| Pure-Go / GORM / cron guards | PASS | compiled deps 无禁用 driver；新增业务文件无 Raw/Exec；runner 无 timer/ticker/sleep；robfig v3.0.1 + AddJob 读回 |
| generation fence rework | PASS | late 429、attempt-first/release-first、success/Retry-After crash gap、CAS conflict、runtime/Initialize recoverable error；Store race 66.212s、scheduler race 23.104s，recovery repeat 20 轮通过 |
| final scope P1 rework | PASS | Retry-After 早于本地 backoff 时 manual 仍被 server fence 阻止；fence 到期后可越过普通 backoff；foreground/wake 尊重普通错误退避；focused20：1.590s/12.642s/11.954s，race10：12.466s/46.948s/38.666s，全仓 test/race PASS |
| durable restart rework | PASS | startup/recovery/reconcile 不 rebasing 尚未到期的 network/retry-after due，过期 recovery 立即 fetch，两个 Retry-After fence 取较晚者；focused20：2.929s/12.459s/12.619s，race10：11.180s/48.055s/42.346s，全仓 test/race PASS |
| final scope re-review | PASS | 两个 P1 均 CLOSED；无 P0-P3 findings，`blocking_findings=0`，`FINAL_SCOPE_PASS:YES`；reviewer 精确回归 quota 0.639s / scheduler 0.647s |
| harness / make verify | PASS | final scope rework 后重新执行；classification `changelog-only`、version findings 空；harness/project、Go、前端 5 tests/build、generated、arm64 minOS 15 app/ZIP 全部通过 |
| PR / merge / post-merge | PASS | PR #28 已合并为 `f953f74`；main focused20/race10、全仓 test/race、Pure-Go/GORM/cron、harness/version 与完整 `make verify` 通过，Linear TOO-265 已读回 Done |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

## 目标

- 证明 Reset Credits 只读 response 被严格归一为不含用户文案/凭证的 typed snapshot/items，非法 schema 不产生伪事实。
- 证明 available/total/redeemed/cumulative remaining/nearest expiry 会按 evaluation time 重算，失败保留 last-known-good，真实 0 与 never-loaded 不混淆。
- 证明所有可信 quota 窗口共同计算 nearest reset/remaining，suspicious/expired/never-loaded 不参与。
- 证明 quota/reset-credits cadence、429、network、auth、schema、manual、foreground、wake、cancel、disable 和 restart 都产生持久 next due/reason，且 CAS claim 阻止重叠请求。
- 证明周期唤醒只使用 `github.com/robfig/cron/v3 v3.0.1`，不新增生产 timer/ticker/sleep loop。
- 本 runbook 只验证 synthetic contract，不是 live Wham/auth E2E。

## 执行副作用

- 可能写入的本地文件：Go build/test cache；完整 `make verify` 可能生成仓库已忽略的 frontend/build/package 产物。
- 可能访问的服务 / 数据库 / 外部系统：功能测试不访问外部服务，HTTP 使用内存 fake transport、SQLite 位于测试临时目录；若本机缺少 lockfile dependencies，完整 `make verify` 的 `npm ci` 可能访问 npm registry。
- 可能创建的临时数据：synthetic Reset Credits snapshot/items、quota observations/current、source attempt/state、refresh schedule/claim、v11/v12 migration 与 backup fixture。
- 明确不会触达的范围：真实 Codex Home、`auth.json`、Wham 网络、用户 SQLite、consume endpoint、GitHub Actions、release/tag。
- 如果测试被改为真实 endpoint、真实 auth 路径或默认应用数据库，立即停止并重新确认范围。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：待验证的 TOO-265 分支或包含其变更的主分支。
3. 必需命令：项目指定 Go toolchain、`make`、`git`、`rg`、`python3`、精确版本 `wails3 v3.0.0-alpha2.117` 和按 lockfile 恢复的前端依赖。
4. 必需配置：无；禁止注入真实 credential 或用户数据库路径。
5. 必需测试环境：focused Store 使用 `CGO_ENABLED=0`；完整 Wails gate 使用 macOS 默认 CGO。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

git status --short --branch
go version
```

预期结果：目录和分支正确；命令不读取或打印 credential、auth 文件、HTTP body 或用户数据库。

## 主路径

### 1. Reset Credits、schedule、migration 重复验证

```bash
CGO_ENABLED=0 go test ./internal/codex/quota ./internal/store ./internal/scheduler \
  -run 'ResetCredits|QuotaResetSummary|RefreshPolicy|SourceRefresh|QuotaRefresh|ApplicationSchemaV12|ApplicationMigration.*V12|ListQuotaCurrent|AbandonedSourceRefreshClaim' \
  -count=20
```

预期结果：

- endpoint/method/header cleanup、bounded/duplicate-key decoder、status/time/count 校验和隐私 marker 全部稳定。
- snapshot/items/source attempt 同事务，exact replay、冲突、跨来源 provenance 篡改和失败保留 last-known-good 稳定。
- quota normal/low/near-reset/reset+3s、reset credits interval、429/network/auth/schema/cancel/disable/recovery 的 due/reason 精确。
- due 前不 claim、同 revision 只 claim 一次、陈旧 completion/release 返回可恢复 conflict、manual 60 秒双层节流、启动后才到期的 claim 可恢复；claim fence 保证 attempt-first 完成原计划、release-first 拒绝迟到 attempt。
- v12 checksum 冻结；v11→v12 先备份再原子升级，失败完整留在 v11。

### 2. focused race 与 Pure-Go 依赖

```bash
CGO_ENABLED=0 go test -race ./internal/codex/quota ./internal/store ./internal/scheduler \
  -run 'ResetCredits|QuotaResetSummary|RefreshPolicy|SourceRefresh|QuotaRefresh|ApplicationSchemaV12|ApplicationMigration.*V12|ListQuotaCurrent|AbandonedSourceRefreshClaim' \
  -count=10

if CGO_ENABLED=0 go list -deps ./internal/store ./internal/codex/quota ./internal/scheduler \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
```

预期结果：race/repeat 均退出 0；实际编译依赖使用 `github.com/libtnb/sqlite` + `modernc.org/sqlite` Pure-Go 路径，不包含禁用 driver。

### 3. GORM-first、cron SDK 与敏感信息 guard

```bash
if rg -n '\.(Raw|Exec)\(' \
  internal/store/reset_credits_repository.go \
  internal/store/source_refresh_schedule_repository.go \
  internal/codex/quota/reset_credits_client.go \
  internal/codex/quota/reset_credits_service.go \
  internal/codex/quota/schedule.go \
  internal/scheduler/quota_refresh.go \
  internal/scheduler/quota_refresh_runner.go; then
  exit 1
fi

if rg -n 'time\.(NewTicker|Tick|Sleep|After|NewTimer)' \
  internal/scheduler/quota_refresh.go \
  internal/scheduler/quota_refresh_runner.go; then
  exit 1
fi

rg -n '^\s*github.com/robfig/cron/v3 v3.0.1$' go.mod

if rg -n 'Authorization:|Bearer [A-Za-z0-9+/=_-]{12,}|http://127\.0\.0\.1:[0-9]+|/Users/' \
  docs/test/reset-credits-quota.md \
  docs/design/details/quota/README.md \
  docs/design/details/scheduling-and-bootstrap/README.md | rg -v ':(if )?rg -n '; then
  exit 1
fi
```

预期结果：业务读写没有 raw SQL；v12 `STRICT/CHECK/index` DDL 仅在 migration schema adapter；quota refresh runner 没有自写周期 loop；依赖精确锁定 robfig v3.0.1；提交版文档没有真实敏感值或机器路径。

### 4. 全仓与控制面门禁

```bash
go test ./...
go test -race ./...
go vet ./...
go mod tidy -diff
git diff --check
make harness-verify
python3 .agents/skills/project-version-release/scripts/project_version_release.py check --repo "$PWD" --json
make verify
```

预期结果：全部退出 0；允许记录既有 macOS deployment linker warning，但不得把 warning 误写为失败或静默隐藏新的 error。GitHub Actions 仍按用户要求停用，不是本 runbook 的验证入口。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| decoder/privacy | 任一原始 ID、文案、token/body 泄漏或非法 payload 落库 | 记录固定测试名和 content-free failure code | 修复 decoder/record contract 后从步骤 1 重跑 |
| migration | checksum/schema drift、升级或 rollback 失败 | 记录版本和稳定 migration stage/code | 保留旧库，修复 v12 migration 后重跑 |
| schedule/claim | 重叠 claim、manual 提前、Retry-After 提前、遗留 claim 不恢复 | 记录 trigger/reason/revision，不记录 request 原文 | 修复 policy/Store CAS 后重跑步骤 1～2 |
| race/Pure-Go | race 或禁用 driver 进入编译链 | 记录 package/依赖名 | 修复并重跑步骤 2 和全仓 race |
| harness/build | 任一 gate 非零 | 记录 gate 名与脱敏摘要 | 修复后从步骤 4 完整重跑 |

## 清理

```bash
git status --short
```

预期结果：`testing.T.TempDir()` 自动清理 SQLite/WAL/SHM；若 `make verify` 生成 ignored build/frontend/package 产物，按仓库既有清理流程删除，保留 tracked `.gitkeep`。不需要 revoke token、停止 server 或清理外部数据，因为本 runbook 不创建这些资源。

## 结果回写

每次执行后更新本文前部的结论、步骤表和清理结果；只写脱敏摘要。不得写真实 token、Authorization/Cookie、原始 response、完整 credit ID、机器绝对路径、临时目录或内部 SQLite 行主键。Actions 保持 `actions_disabled_by_user`，不查询、不触发、不等待；普通 Execution 不发布。
