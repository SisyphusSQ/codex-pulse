# Wails Events 与 Vue Query Cache Runbook

## 当前验证结果

- 记录时间：2026-07-16（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-271 typed Wails invalidation event、post-commit通知、Vue Query key/cache/recovery契约
- 当前结论：`IMPLEMENTATION REVIEW PASS；CHANGELOG/POST-INTEGRATION FULL LOCAL GATES PASS；FINAL SCOPE REVIEW PASS`
- 自动化入口：`internal/app/query_invalidation_test.go`、`internal/scheduler/*_test.go`、`frontend/src/queries/business.test.ts`、`frontend/src/events/queryInvalidation.test.ts`、`frontend/src/App.test.ts`
- 对应计划 / issue：`.agents/plans/2026-07-16-too-271-wails-events-vue-query-cache.md` / TOO-271
- 结果说明：仅使用内存 fake、synthetic HTTP payload、`testing.T.TempDir()` Preferences/Pure-Go SQLite 和 generated bindings；未读取真实 Codex Home/auth/session，未启动 Wails 窗口，未查询或触发 GitHub Actions，未发布。

### 本次执行结果

- RED：Go 缺少 typed publisher/commit callback，前端缺少 business query catalog/event bridge，focused suites 按预期失败。
- GREEN：Go focused repeat 20 次与 race 10 次通过；覆盖四域有限 payload、JSON 仅 version/domain、非法 domain、序列化失败 no-emit + content-free health、emitter panic containment、scheduler/quota commit success/failure、settings commit/reconcile failure。
- Generated：Wails `v3.0.0-alpha2.117` 处理 319 packages、1 service、15 methods、32 enums、65 models、1 event；`eventdata.d.ts` 精确增强 `codex-pulse:query-invalidated -> QueryInvalidationEvent`，event name 与 version 均由 generated type 约束防漂移。
- Frontend：typecheck、5 files / 16 Vitest 与 production build 通过；覆盖13 binding exact key/delegation/stale/active interval/AbortSignal→CancellablePromise、真实QueryObserver周期重取、unsubscribe停止与Go调用取消、Quota每次fetch重取clock、100组duplicate storm、malformed/unknown/wake/runtime-ready/foreground full invalidate、bootstrap排除、禁止setQueryData、四订阅卸载与pending timer取消。
- Full Go：`go test ./... -count=1` 通过（Store 15.141s）；`go test -race ./... -count=1` 通过（Store 82.534s）；vet、tidy diff、`git diff --check` 均通过。
- Control/package：harness、project及其负向contract、generated success/failure preservation、version `findings=[]` 均通过；arm64/minOS 15 ad-hoc app 与ZIP验证通过。implementation review两个Medium（malformed runtime guard、lost-event active periodic refetch）均完成RED/GREEN，最终Critical/High/Medium/Low为0、`IMPLEMENTATION_REVIEW_PASS:YES`。
- Post-integration：`[TOO-271]`在`Unreleased -> feature`唯一；focused20（app 2.644s / scheduler 3.093s）、focused race10（app 11.318s / scheduler 18.055s）、full test（Store 15.129s）、full race（Store 77.433s）、tidy/diff/version/plan gate与完整`make verify`全部退出0；frontend为5 files / 16 tests，generated为32 enums/65 models/1 event，package仍为arm64/minOS15。
- Final scope review：不同subagent独立核对typed payload、commit顺序、failure health、generated防漂移、13 query/timing/cancel、storm/lost/malformed/recovery/unmount、CHANGELOG唯一性与scope guard；Critical/High/Medium/Low均为0，`remaining_findings=0`、`blocking_findings=0`、`FINAL_SCOPE_REVIEW_PASS:YES`。
- 清理状态：测试临时目录自动清理；closeout 前已将 ignored `.task`、`bin`、`frontend/node_modules` 与 frontend build output 移入系统废纸篓，仅保留 tracked `frontend/dist/.gitkeep`。
- 敏感信息处理：只记录固定 event/query key、稳定错误分类和脱敏计数；不记录真实路径、token、Preferences值、SQLite row、request/response正文或临时目录。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| typed event / post-commit | PASS | 四域有限 payload；scheduler/quota/settings commit边界；失败health |
| query catalog | PASS | 13业务binding的完整request key/delegation/stale+active interval/cancelOn；Quota动态clock |
| storm/recovery/unmount | PASS | 50ms去重、malformed runtime guard、active refetch、full recovery、4 unsubscribe/取消timer |
| generated/frontend | PASS | 1 typed event；typecheck、16 tests、production build |
| full repository | PASS（post-integration） | full test/race/vet/tidy、frontend/generated、harness/project/version、arm64 package |
| implementation review | PASS | 两个Medium已闭环；最终0 findings、blocking=0 |
| post-integration full gates | PASS | focused20/race10、full test/race、make verify、version/plan gate |
| final scope review | PASS | 不同subagent四级0 findings，remaining/blocking均0 |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

## 目标

- 证明 event 只在 authoritative facts/settings 提交后发出，且只携带有限 invalidation hint。
- 证明 13 个业务 query 的 key、request、stale/active interval、AbortSignal取消、Quota动态clock与 generated binding 保持严格契约。
- 证明 duplicate/storm/lost/disconnect/wake/foreground/unmount 都不会制造第二事实源、重复失效风暴或订阅泄漏。
- 证明 Go DTO 经 Wails generator 成为 TypeScript event 唯一类型真相。

## 执行副作用

- 可能写入：Go/npm build/test cache；Wails generation 同步 `frontend/bindings`；frontend build 写 ignored `frontend/dist`；完整 gate 可能写 ignored `.task` 与 `bin`。
- 临时数据：`testing.T.TempDir()` 中 synthetic Preferences、Pure-Go SQLite/WAL/SHM；内存 fake event/query client。
- 可能访问外部系统：依赖已安装时 focused/full tests 不访问网络；缺失依赖时 `npm ci` 或 Wails CLI bootstrap 可能访问公共 registry。
- 明确不触达：真实 Codex Home、auth/token、session正文、真实用户数据库、Wham live endpoint、Wails窗口、GitHub Actions、tag/release。
- 执行前必须说明以上副作用；若输入变成真实路径、credential、live endpoint 或发布动作，立即停止并重新冻结范围。

## 前置条件

1. 当前目录为仓库根目录，分支包含 TOO-271 改动。
2. Go/Node/npm 与 Wails CLI 使用仓库锁定版本；Wails 必须报告 `v3.0.0-alpha2.117`。
3. `frontend/node_modules` 可由 `npm ci` 重建；测试不得注入真实业务数据或credential。
4. GitHub Actions 保持用户停用；本 runbook 不查询、触发或等待 CI。

## 主路径

### 1. Event publisher 与 durable commit 边界

```bash
go test ./internal/app ./internal/scheduler \
  -run 'QueryInvalidation|CycleCommitted|RefreshCommitted|CommitsSettingsBeforeQuotaReconcile|ReturnsCommittedSettingsOnReconcileFailure' \
  -count=20
go test -race ./internal/app ./internal/scheduler \
  -run 'QueryInvalidation|CycleCommitted|RefreshCommitted|CommitsSettingsBeforeQuotaReconcile|ReturnsCommittedSettingsOnReconcileFailure' \
  -count=10
```

预期：payload 只有version/domain；invalid domain不emit；serializer/emitter失败记录content-free health；commit失败不通知；settings已提交即使reconcile失败仍发settings/quota hint；全部无race。

### 2. Query catalog、storm、recovery 与卸载

```bash
npm --prefix frontend run typecheck
npm --prefix frontend test -- --run \
  src/queries/business.test.ts \
  src/events/queryInvalidation.test.ts \
  src/App.test.ts
npm --prefix frontend test
npm --prefix frontend run build
```

预期：13个业务request key完整且绑定`cancelOn(signal)`；active query按域interval有界重取，observer卸载即停止并取消Wails/Go查询；Quota每次fetch使用新clock；storm每root每批一次；malformed/unknown/wake/runtime-ready/foreground全失效但不含bootstrap；不调用`setQueryData`；app unmount释放四个subscription并取消pending flush。

### 3. Generated event contract

```bash
make verify-generated
rg -n 'codex-pulse:query-invalidated|QueryInvalidationEvent' \
  frontend/bindings/github.com/wailsapp/wails/v3/internal/eventdata.d.ts \
  frontend/bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models.ts
```

预期：正常 regeneration 无 diff；只有一个 custom event，data type 精确为 generated `QueryInvalidationEvent`，enum只有四个非零domain。

### 4. 全仓门禁

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
git diff --check
make harness-verify
make project-check
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
make verify
```

预期：全部本地 gate 退出0；Actions不作为验证入口。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| emit顺序 | commit失败仍emit或commit前emit | 只记录domain/commit类别 | 修复callback位置后重跑步骤1～4 |
| payload/privacy | 出现业务事实、路径、ID、错误正文 | 记录字段名，不记录值 | 收敛DTO/生成物后重跑步骤1～4 |
| cache correctness | handler调用setQueryData、request不入key、malformed/unknown事件被忽略或抛错 | 记录query root | 修复catalog/bridge后重跑步骤2～4 |
| storm/cleanup | 重复invalidate、timer/subscription泄漏 | 记录稳定计数 | 修复batch/lifecycle后重跑步骤2～4 |
| generation/full gate | generated漂移或任一gate非零 | 记录gate名与脱敏摘要 | 修复后从失败gate重跑，最终完整执行步骤4 |

## 清理

```bash
git status --short
```

预期：测试临时目录自动清理；closeout 后将已知 ignored `.task`、`bin`、`frontend/node_modules` 和 frontend build output 移入系统废纸篓，保留 tracked `frontend/dist/.gitkeep`。无需 revoke credential、停止server或清理外部数据。

## 结果回写

执行后更新本文前部的结论、步骤表和脱敏结果。不得写真实token、路径内容、Preferences值、SQLite row、原始event/request/response或临时目录。Actions保持`actions_disabled_by_user`，不查询、不触发、不等待；普通Execution不发布。
