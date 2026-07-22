# Wails Bindings 与 TypeScript Models Runbook

## 当前验证结果

- 记录时间：2026-07-16（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-270 单一 Wails query façade、generated TypeScript DTO/error contract、取消传播与失败生成保护
- 当前结论：`DONE`；TOO-270 已由 PR #35 self-merge 为 `bd95ceb3d1987f55a574aa375be4dc32fa1da544`，post-merge verify 与 Linear Done 已完成；当前作为 TOO-236 Master closeout 的集成证据。
- M6 Master 集成验证（2026-07-17）：`query/... + store + app + scheduler` focused count10 全部通过（Store 102.690s、app 19.047s、scheduler 34.816s），race count3 全部通过（Store 218.458s、app 41.893s、scheduler 78.954s）；完整 `make verify`、全仓 race（Store 82.855s）、vet/tidy/diff、harness/project/version、frontend 5 files/16 tests、generated 319 packages/1 service/15 methods/32 enums/65 models/1 event、arm64/minOS 15 app/ZIP 全部通过。release classification=`issue-only`、version `findings=[]`，最终 diff 仅含 M6 五份 runbook。
- M6 Master final review：独立 subagent 首轮发现本地 ignored Master plan skeleton Medium，补齐后又发现并关闭 `internal/app/service.go` / `Service` 命名真相 Low；最终 Critical/High/Medium/Low 均为 none，`remaining_findings=0`、`blocking_findings=0`、`MASTER_FINAL_REVIEW_PASS:YES`。
- 自动化入口：`internal/app/*_test.go`、`frontend/src/bindings/contracts.test.ts`、`scripts/project-checks/check_binding_generation_failure.sh`、`scripts/project-checks/check_generated_test.sh`
- 对应 issue：TOO-270
- 结果说明：当前只用 synthetic stub、`testing.T.TempDir()` 中的 Pure-Go SQLite 和 generated bindings；未读取真实 Codex Home/Preferences、用户数据库或 credential，未启动 Wails 窗口，未触发 Actions 或 release。

### 本次执行结果

- 执行时间：2026-07-16
- 本次结论：implementation review 的 3 个 Medium 均已完成 RED/GREEN；rework focused20/race10、full test/race/vet/tidy、harness/project/version、frontend、generated、arm64 package 与完整 `make verify` 均通过；原 reviewer closure 为 `blocking_findings=0`、`IMPLEMENTATION_REVIEW_PASS:YES`。
- 已验证 surface：`QueryService` 精确暴露 15 个只读方法，`commandMethods=[]`；Repository、SQLite、Preferences persistence model 与 lifecycle mutator 不在 exported method set。
- 已验证跨端契约：15 个公开方法逐一锚定精确 TypeScript `Parameters` / `CancellablePromise<Return>`；usage/session/project/quota/source/job/health/settings DTO、有限枚举和 `query.ErrorEnvelope` 均由 Go method signature 生成。
- 已验证失败语义：Go façade 保留 `errors.Is(context.Canceled)` 并 recover 依赖 panic；真实 Wails `BoundMethod.Call` 子进程证明 cancel/panic 都成为固定 content-free RuntimeError + envelope。Wails framework TypeError 不属于固定 message 契约，前端禁止展示其 message。
- 已验证真实 composition：同一个 façade 读回 empty active usage ledger、empty quota current、source known-empty，并在 compose 后观察同一 Preferences FileStore 的非默认 online 设置。
- 失败生成证据：注入非法 models 文件名并读回专用失败标识后，17 个 tracked/untracked generated files 的 SHA-256 集合前后相同；guard 自测覆盖 generic failure、失败但改文件和错误 Wails 版本必须报 `BINDING-001`。
- Full 证据：`go test ./... -count=1` 退出 0（Store 16.323s）；`go test -race ./... -count=1` 退出 0（Store 84.383s）；version check `findings=[]`；frontend 3 files/7 tests、generated 319 packages/1 service/15 methods/30 enums/64 models、arm64/minOS 15 ad-hoc app/ZIP 均通过。
- Rework full 证据：app focused20 1.622s、race10 18.522s；full test Store 21.353s、full race Store 80.778s；vet/tidy/diff、harness/project/version `findings=[]`、frontend 3 files/7 tests、generated 17-file preservation/stability、arm64/minOS 15 app/ZIP 与完整 `make verify` 均退出 0。
- Post-integration 证据：`CHANGELOG.md -> Unreleased` 中 `[TOO-270]` 唯一；app focused20 2.257s、race10 24.931s；full test Store 16.411s、full race Store 77.849s；vet/tidy/diff、harness/project/version `findings=[]`、frontend 3 files/7 tests、17-file generated failure-preservation/stability、arm64/minOS 15 app/ZIP 与完整 `make verify` 均退出 0。
- 副作用：Go/npm build/test cache，以及正常 binding generation 对 `frontend/bindings` 的预期同步；失败注入只在生成器自己的临时目录尝试生成。
- 清理结果：测试临时 SQLite 与 generator temp output 已自动清理；post-integration 后已将 ignored `.task`、`frontend/node_modules`、dist assets/index 与 `bin` 移入系统废纸篓，tracked `frontend/dist/.gitkeep` 保留；不存在后台 Wails 进程或外部数据。
- 敏感信息处理：只记录固定 method/type/error code；不记录真实路径内容、Preferences 值、SQLite row、credential、driver cause 或临时目录。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| exact façade / delegation | PASS | reflected exported method set 与 15-method allowlist 精确相等，全部 query delegate 到真实业务 service interface |
| real composition | PASS（rework focused） | 临时 Pure-Go SQLite + Repository + Quota Current + compose 后共享 Preferences 写入读回 |
| cancel / typed error / privacy | PASS（rework focused） | direct + 真实 BoundMethod subprocess；cancel、panic、validation/internal envelope 与 secret canary |
| generated TypeScript contract | PASS（rework focused） | 15 methods 全部精确 request/response 与 CancellablePromise |
| generation atomicity | PASS（rework focused） | 专用注入标识 + byte readback；generic/version/mutation 自测均 fail closed |
| full repository / package | PASS（rework） | full test/race/vet/tidy、harness/version、frontend/generated、arm64/minOS 15 package、完整 `make verify` |
| implementation review | PASS | 首轮 3 Medium 全部闭环；closure `blocking_findings=0`、`IMPLEMENTATION_REVIEW_PASS:YES` |
| final scope review | PASS | 不同 subagent 独立对账；`remaining_findings=0`、`blocking_findings=0`、`FINAL_SCOPE_REVIEW_PASS:YES` |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

## 目标

- 证明 Wails 只注册一个业务 façade，精确暴露批准的 M6 只读 query，不暴露 Store、SQLite、文件、shell、网络、credential 或私有 lifecycle mutator。
- 证明 façade 由真实 Repository、Quota Current 和共享 Preferences loader 装配，不用静态 placeholder。
- 证明 Go request/response/enum/error 是 `frontend/bindings` 的唯一类型源，TypeScript compile 能发现签名漂移。
- 证明业务 query 可取消，业务 error/recovered panic 的跨界 `RuntimeError` 只有固定 message 与版本化 content-free envelope；framework `TypeError.message` 不进入展示。
- 证明绑定生成失败不会覆盖或部分更新上一版 generated files。

## 执行副作用

- 可能写入：Go/npm build/test cache；成功生成会同步 `frontend/bindings`；完整 `make verify` 可能创建 ignored `frontend/node_modules`、`frontend/dist`、`.task` 与 `bin`。
- 临时数据：`testing.T.TempDir()` 内 synthetic Preferences 和 Pure-Go SQLite/WAL/SHM；Wails generator 自有 sibling temp output。
- 可能访问外部系统：focused Go/TypeScript/guard 不访问业务网络；依赖缺失时 npm 或 Wails CLI bootstrap 可能访问公共 registry。
- 明确不触达：真实 Codex Home/Preferences/SQLite、auth/token、Wham、Wails UI/window、GitHub Actions、tag/release。
- 执行前必须说明以上副作用；若路径、credential、endpoint 或发布范围发生变化，立即停止并重新界定范围。

## 前置条件

1. 当前目录为仓库根目录，分支包含待验证的 TOO-270 改动。
2. 使用项目锁定 Go/Node 依赖及 Wails `v3.0.0-alpha2.117` CLI。
3. focused tests 只使用 synthetic fixture，不注入真实用户路径、Preferences 或 credential。
4. GitHub Actions 保持用户停用；本 runbook 不查询、触发或等待 CI。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

test -f internal/app/service.go
test -f frontend/src/bindings/contracts.test.ts
test -x scripts/project-checks/check_binding_generation_failure.sh
test "$(wails3 version 2>&1)" = "v3.0.0-alpha2.117"
```

预期结果：四项检查退出 0；不读取或打印真实用户数据与 credential。

## 主路径

### 1. Go façade、真实 composition、取消与 error contract

```bash
go test ./internal/app -run 'Binding|Service|ApplicationOptions' -count=20
go test -race ./internal/app -run 'Binding|Service|ApplicationOptions' -count=10
```

预期结果：全部退出 0；exact method allowlist、全部 delegation、real usage/quota/source/shared Preferences composition、shared service registration、真实 BoundMethod cancel/panic/error/privacy canary 可重复且无 race。

### 2. Generated TypeScript surface

```bash
npm --prefix frontend run typecheck
npm --prefix frontend test -- --run src/bindings/contracts.test.ts
```

预期结果：TypeScript 只从 generated modules 导入业务类型；15 个 method 精确匹配，返回值为 `CancellablePromise`，`ErrorEnvelope`/`ErrorCode` 可达且严格类型化。

### 3. 成功生成稳定性与失败生成保护

```bash
bash scripts/project-checks/check_binding_generation_failure.sh
bash scripts/project-checks/check_generated.sh
bash scripts/project-checks/check_generated_test.sh
```

预期结果：

- 非法 `-models INVALID` 注入必须命中专用 uppercase validation 标识并非零退出，原 bindings 的路径与 SHA-256 前后相同；任意 generic failure 不得算作有效注入。
- 正常 Wails generation 与当前提交内容无 diff。
- guard contract test 能识别失败生成期间的内容变更并以 `BINDING-001` 拒绝。

### 4. denylist 与全仓门禁

```bash
if rg --pcre2 -n \
  '^(?!\s*\*)(?=.*(?:Repository|SQLite|GORM|RawSQL|Preferences|SwitchPlan|SourceRefreshSchedule))' \
  frontend/bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service.ts; then
  exit 1
fi

go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
git diff --check
make verify-architecture
make verify
```

预期结果：denylist 无输出；全部本地 gate 退出 0。Actions 不是验证入口。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| façade / composition | method 越权、placeholder、两份 Preferences loader、依赖泄漏 | 记录固定 method/dependency 类别 | 收敛 façade/composition 后重跑步骤 1～4 |
| cancel / error | cancel 丢失、raw cause/marker 进入 message/cause | 只记录稳定 error code | 修复 context/error marshaler 后重跑步骤 1～4 |
| TS contract | shadow type、any/unknown、非 CancellablePromise 或生成漂移 | 记录 generated module/type 名 | 修复 Go signature/generation 后重跑步骤 2～4 |
| generation failure | 非零失败仍改变任一旧文件 | 记录 `BINDING-001` 与文件类别，不记录机器临时路径 | 先保留当前 git 状态证据，修复 temp/sync guard，再正常生成并重跑步骤 3～4 |
| full/package | 任一本地 gate 非零 | 记录 gate 名与脱敏摘要 | 修复后从失败 gate 重跑，最终完整执行步骤 4 |

## 清理

```bash
git status --short
```

预期结果：测试临时目录自动清理；full gate 生成的 ignored dependencies/build/package 产物按仓库既有 scoped cleanup 移入系统废纸篓并保留 `frontend/dist/.gitkeep`。无需 revoke credential、停止 server 或清理外部数据。

## 结果回写

每次执行后更新本文前部的结论、步骤表和清理结果，只写脱敏摘要。不得写真实 token、路径内容、Preferences 值、SQLite row/identity、生成器临时目录、driver cause 或原始响应。Actions 保持 `actions_disabled_by_user`，不查询、不触发、不等待；普通 Execution 不发布。
