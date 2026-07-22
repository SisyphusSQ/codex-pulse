# Quota Current 查询与可信场景 Runbook

## 当前验证结果

- 记录时间：2026-07-16（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-266 Quota Current 单 snapshot 只读查询、稳定 domain DTO 与可信场景矩阵
- 当前结论：`PASS（已合并并完成 post-merge verify）`；两轮独立 review `blocking_findings=0`；PR #29 已合并为 `4fc76ce`，main post-merge 门禁通过，Linear TOO-266 已读回 Done。
- 自动化入口：`internal/codex/quota/current_query_test.go`、`internal/store/quota_query_snapshot_test.go`
- 对应 issue：TOO-266
- 结果说明：全部使用 synthetic observation/source state/schedule/Reset Credits 和 `testing.T.TempDir()` Pure-Go SQLite；未读取真实 Codex Home/auth，未请求 Wham，未注册 Wails service，未触发 Actions 或 release。

### 本次执行结果

- 执行时间：2026-07-16
- 当前结论：`PASS（含 main post-merge verify）`；final focused repeat/race、全仓 test/race/vet、harness、版本与完整 `make verify` 均退出 0。
- final focused repeat：`-count=20`，`internal/codex/quota 9.918s`；`internal/store 4.008s`
- final focused race：`-race -count=10`，`internal/codex/quota 65.956s`；`internal/store 15.475s`
- 副作用：Go build/test cache，以及临时目录中的 synthetic SQLite/WAL/SHM；完整 verify 恢复 lockfile 前端依赖并生成 dist/app/ZIP。
- 清理结果：临时数据库由 Go 自动清理；本轮 `frontend/node_modules`、dist assets/index、`.task/` 与 package 产物已删除，tracked `.gitkeep` 保留；generated bindings/module 无漂移。
- 敏感信息处理：response 只保留结构化配额/来源/调度状态；测试断言 claim/snapshot/credit/request/path/token/header/body marker 不可见。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| aggregate snapshot | PASS | current/observation/evidence/source/schedule/reset summary 同一 read transaction |
| trusted scenario matrix | PASS | Local-only、Wham-only、一致、冲突、expired LKG、429、跨窗口、Reset Credits |
| null / zero / privacy | PASS | never-loaded 为 null；真实零保留；私有 identity 不进入 DTO/error |
| concurrency / recovery | PASS | 并发 commit 只见完整旧/新 snapshot；缺失 projection 只读失败，显式 rebuild 可恢复 |
| affected package test/race | PASS | `CGO_ENABLED=0` focused20/race10 均退出 0；vet/tidy/Pure-Go/GORM guards 通过 |
| full Go test/race/vet | PASS | 全仓普通与 race 退出 0；race quota 13.284s、scheduler 33.468s、Store 78.005s |
| harness / version / diff | PASS | harness 通过，版本检查 `findings=[]`，CHANGELOG 唯一 `[TOO-266]`，diff check 通过 |
| complete make verify | PASS | project/negative/generated、Go、前端 5 tests/build、bindings、arm64 minOS 15 app/ZIP 全部通过 |
| implementation review | PASS | 首轮 3 blocking findings 修复后复审关闭；closure20/race3、完整 focused/race 与 guards 通过 |
| final scope review | PASS | 不同 subagent 独立审查，`FINAL_SCOPE_PASS:YES`、`blocking_findings=0` |
| PR / merge / post-merge | PASS | PR #29 已合并为 `4fc76ce`；main focused20/race10、全仓 test/race/vet、harness/version 与完整 `make verify` 通过，Linear TOO-266 已读回 Done |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

完整 verify 首次在当前 shell 未包含锁定 Wails CLI 时以 `TOOLCHAIN-001` 正确停止；显式补入已验证的 `v3.0.0-alpha2.117` 工具目录后进入前端，并因依赖未恢复在 `vue-tsc` 前停止。随后按 lockfile 执行 `npm ci`（199 packages，audit 0）并从完整入口重跑通过。既有 `glob@10.5.0` deprecated warning 和 macOS 26.0→11.0 linker warning 未被当作新增失败，本卡不顺带升级依赖或工具链。

### Implementation review rework

第一轮独立 implementation review 报告 3 个 blocking findings：suspicious window 仍暴露不可信 reset countdown；新 aggregate Query 缺少完整 close/reopen 证据；evidence/projection 畸形只泄出 Store error，未统一为可恢复 domain sentinel。另有非 blocking 文档误写 `additional` window，以及 Local source freshness 未区分 accepted/stale/rejected observation。

当前修复证据：只有 fresh/stale window 返回 reset countdown；同一路径 close/reopen 后完整 response 相等，新的 evaluation time 会继续动态推进 reset 与 credit expiry；两来源 evidence 缺一行时 Query 同时返回 `ErrQuotaCurrentUnavailable` 与 Store cause，显式 rebuild 后恢复；Local 只按 accepted observation 在 10 分钟/reset 边界内标 current，之后 stale，suspicious-only 保持 unknown；文档不再承诺不存在的 additional window。修复后 focused20/race10、全仓 test/race/vet 与完整 make verify 全部退出 0。原 implementation reviewer 复审无新增 P0–P3，closure20 为 quota 4.753s / Store 3.738s，closure race3 为 quota 7.362s / Store 5.900s，`IMPLEMENTATION_REVIEW_PASS:YES`、`blocking_findings=0`。

### Final scope review

第二位不同 subagent 对 Linear Goal/Included/Excluded/Acceptance、完整 tracked/untracked diff、单 snapshot/recovery/null-zero/freshness/场景矩阵/restart/privacy，以及 M6/Wails/UI/Actions/release 边界重新审查，无 P0–P3 findings，`FINAL_SCOPE_PASS:YES`、`blocking_findings=0`。reviewer 独立执行 focused5、race2、受影响包完整 test/vet、格式/diff、Pure-Go/GORM/raw SQL/Wails 越界与依赖版本 guards，均通过；其未重复的全仓 race/package 由主线程在提交前最终集成验证重跑。

### 提交前最终集成结果

在两轮 review 之后，主线程针对最终 diff 重新执行 focused20/race10、全仓 `go test ./...`、`go test -race ./...`、`go vet ./...`、harness、项目版本检查、Pure-Go/GORM/唯一 CHANGELOG/diff guards 与完整 `make verify`，全部退出 0；版本检查 `findings=[]`，前端 5 tests 通过，generated bindings/module 稳定，arm64/minOS 15 ad-hoc app 与单顶层 ZIP 验证通过。lockfile 依赖 audit 0；生成的 frontend dependencies/dist、`.task/` 与 package 产物已清理，tracked `.gitkeep` 保留。Actions 未查询、触发或等待，未执行 release。

## 目标

- 证明 `quota-current-v1` 在一次查询中返回一致的 windows、sources、nearest reset、Reset Credits 与 refresh status。
- 证明 unknown/null、真实零、last-known-good、freshness/conflict、可信 reset 和固定 explanation code 不混淆。
- 证明 Local/Wham 单源、双源一致/冲突、expired、429、primary/secondary 与 Reset Credits 到期均有可重复场景。
- 证明查询不写库；projection 不完整时 fail closed，并只允许显式 maintenance rebuild 恢复。
- 证明 DTO/error 不暴露 token、header/body、路径、request、claim、snapshot 或 credit identity。
- 本 runbook 是 synthetic contract 验证，不是 live Wham/auth E2E，也不验证 M6 Wails binding 或 Vue UI。

## 执行副作用

- 可能写入：Go build/test cache；完整 `make verify` 可能生成仓库已忽略的 frontend/build/package 产物。
- 临时数据：`testing.T.TempDir()` 内的 synthetic Pure-Go SQLite、WAL/SHM、quota observations/projection/evidence、source state/schedule 与 Reset Credits。
- 外部系统：focused/full Go tests 不访问真实网络或用户数据库；若本机缺少 lockfile 依赖，`make verify` 的 `npm ci` 可能访问 npm registry。
- 明确不触达：真实 `~/.codex`、`auth.json`、Wham、用户 SQLite、Wails runtime、GitHub Actions、tag/release。
- 若命令或 fixture 被改为真实 endpoint、auth 路径或用户数据库，立即停止并重新确认范围。

## 前置条件

1. 当前目录是仓库根目录，分支包含待验证的 TOO-266 变更。
2. 使用项目指定 Go toolchain；Store 路径必须能在 `CGO_ENABLED=0` 下编译测试。
3. 不注入真实 credential、Codex Home 或应用数据库路径。
4. GitHub Actions 保持停用；本 runbook 不查询、触发或等待 CI。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

git status --short --branch
go version
```

预期结果：目录与分支正确；命令不读取或打印 credential、auth 文件、HTTP body、用户数据库或本机路径内容。

## 主路径

### 1. 合同与可信场景重复验证

```bash
CGO_ENABLED=0 go test ./internal/codex/quota ./internal/store \
  -run 'TestCurrentQuery|TestQuotaCurrentSnapshot' \
  -count=20
```

预期结果：

- empty/never-loaded 返回稳定 null/reason，真实 0 保持为 0/100，不被当作 unknown。
- Local-only、Wham-only、双源一致/冲突、expired LKG、429、跨窗口与 Reset Credits 到期结果稳定。
- windows 固定排序，explanations 按 observation ID 稳定排序；nearest reset 只接受未来可信窗口。
- projection 缺失为可恢复只读失败；显式 rebuild 后恢复；查询不隐式修表。
- 关闭并重开同一 SQLite 后完整 aggregate/DTO 不漂移，随后使用新 evaluation time 仍会动态降级。
- suspicious/expired window 不返回可信 reset countdown；Local source freshness 不信任 suspicious/rejected-only observation。
- JSON/error 不出现 synthetic private path/request/claim/snapshot/credit marker。

### 2. 同 snapshot 与 race

```bash
CGO_ENABLED=0 go test -race ./internal/codex/quota ./internal/store \
  -run 'TestCurrentQuery|TestQuotaCurrentSnapshot' \
  -count=10
```

预期结果：race 退出 0；Store 并发 writer 在 current 读后提交新 observation/source/schedule/reset facts 时，聚合 reader 仍只返回完整旧 snapshot，下一次查询才看到完整新 snapshot。

### 3. Pure-Go、GORM-first 与隐私 guard

```bash
if CGO_ENABLED=0 go list -deps ./internal/store ./internal/codex/quota \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi

if rg -n '\.(Raw|Exec)\(' \
  internal/store/quota_query_snapshot.go \
  internal/codex/quota/current_query.go; then
  exit 1
fi

if rg -n 'Authorization:|Bearer [A-Za-z0-9+/=_-]{12,}|http://127\.0\.0\.1:[0-9]+|/Users/' \
  docs/test/quota-current.md \
  docs/design/details/quota/README.md | rg -v ':(if )?rg -n '; then
  exit 1
fi
```

预期结果：编译依赖不含禁用 SQLite driver；新增业务查询不使用 raw SQL；提交版文档不含真实敏感值或机器绝对路径。

### 4. 全仓本地门禁

```bash
go test ./...
go test -race ./...
go vet ./...
go mod tidy -diff
git diff --check
make verify-architecture
make verify
```

预期结果：全部退出 0。允许记录既有 macOS deployment linker warning，但不得隐藏新的 error。GitHub Actions 保持 `actions_disabled_by_user`，不是本 runbook 的验证入口。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| snapshot | response 混合跨提交 facts，或 key/evidence 缺失仍成功 | 记录固定测试名和逻辑 window，不记录行内容 | 修复 aggregate transaction/对账后重跑步骤 1～2 |
| contract | unknown 变 0、真实 0 变 null、排序或 explanation 漂移 | 记录字段名和稳定枚举 | 增加最小 fixture，修复 mapper 后重跑步骤 1 |
| privacy | identity/path/token/header/body marker 进入 DTO/error/docs | 只记录 marker 类别 | 删除泄漏面并重跑步骤 1、3 |
| race/Pure-Go | race 或禁用 driver 进入编译链 | 记录 package/依赖名 | 修复后重跑步骤 2～4 |
| harness/build | 任一本地 gate 非零 | 记录 gate 名与脱敏摘要 | 修复后从步骤 4 完整重跑 |

## 清理

```bash
git status --short
```

预期结果：测试临时数据库由 Go 自动清理；若 `make verify` 生成 ignored build/frontend/package 产物，按仓库既有清理流程删除并保留 tracked `.gitkeep`。无需 revoke credential、停止 server 或清理外部数据。

## 结果回写

每次执行后更新本文前部的结论、步骤表与清理结果，只写脱敏摘要。不得写真实 token、Authorization/Cookie、原始 response、路径、request/claim/snapshot/credit identity、临时目录或内部 SQLite 行主键。Actions 保持 `actions_disabled_by_user`，不查询、不触发、不等待；普通 Execution 不发布。
