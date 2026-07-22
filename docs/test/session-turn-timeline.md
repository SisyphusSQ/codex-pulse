# Session Turn Usage/Cost 时间线 Runbook

## 当前验证结果

- Issue：TOO-307
- 分支：`suqing/too-307-session-turn-timeline-contract`
- 本轮任务性质：生成 + 执行 + 回写。
- 当前结论：实现、两层 review、CHANGELOG、PR #40、自合并与 `main` post-merge verify 均已完成；TOO-307 已回写 Linear Done，merge commit 为 `4448b8716d1a0c2ca3ae542630301ed8bd41b760`。
- GitHub Actions：`actions_disabled_by_user`，不查询、不触发、不等待 CI。
- Release：普通 Execution Issue，不创建 tag、GitHub Release、appcast 或正式发布产物。

### Pre-review gate 结果

| 验证面 | 结果 | 证据摘要 |
| --- | --- | --- |
| focused repeat | PASS | Store turn count10、usagecost全包count10、app count10；app仅既有macOS linker warning |
| focused race | PASS | Store turn race count3、usagecost race count3，无race report |
| Pure-Go | PASS | CGO-off SQLite/Store/usagecost通过；生产编译闭包无official sqlite/mattn |
| frontend/generated | PASS | 16 files/50 tests、typecheck/build；319 packages/1 service/15 methods/34 enums/66 models/1 event，二次生成稳定 |
| full repository | PASS | Go test/race/vet/tidy、harness/project、version `findings=[]`、diff均通过 |
| package | PASS | macOS arm64、minOS 15、ad-hoc app与单顶层ZIP验证通过 |

### Implementation review 与 post-integration 结果

- 独立 reviewer 首轮发现 4 项 blocking：公开 checksum cursor 可解码/重签名、aggregate/page 未对账、Linear lifecycle UNKNOWN 文案漂移、Store restart 未直接覆盖。
- cursor confidentiality/forgery 与 aggregate drift 均先补 RED；GREEN 后使用 process-key AES-GCM，并对完整首屏精确比较 rollup/pricing version set/unpriced reasons，对截断/后续页执行 lower-bound + membership。
- Linear、plan、product/front/architecture 已统一为 persisted lifecycle active/complete，unknown 只用于 usage/cost/time；Store 回归用第一页 typed cursor 跨同一 SQLite close/reopen 取得第二页。
- 原 reviewer 两轮 closure 后确认 `remaining_findings=0`、`blocking_findings=0`、`IMPLEMENTATION_REVIEW_PASS:YES`。
- 主线程完整重读 `changelog-style` skill，在 `Unreleased -> feature` 写入唯一 `[TOO-307]` 条目；未创建版本段。
- post-integration full test/race（Store 87.623s）、vet/tidy、CGO-off、frontend 16 files/50 tests、harness/project/version `findings=[]`、generated 15 methods/34 enums/66 models/1 event、arm64/minOS15 package与完整 `make verify` 全部通过。
- different-subagent final scope review复核Linear修订真相、最终diff、plan/runbook/CHANGELOG与scope guard，无P0-P3 finding；`remaining_findings=0`、`blocking_findings=0`、`FINAL_SCOPE_REVIEW_PASS:YES`。Residual为process-key cursor在app重启后按设计失效，TOO-274必须清旧分页cache；性能index只在真实query-plan证据后独立拆卡。

## 目标

- 证明既有 `SessionDetail` 可在同一只读 snapshot 返回 Session aggregate 与 bounded Turn usage/cost page，不新增 Wails method。
- 证明 Turn 按 `startedAt DESC + turn identity DESC` 稳定分页，cursor 由 process-key AEAD 认证加密并绑定 Session/domain，wire 不含 raw identity，篡改、公开 checksum 重签名或跨 Session 复用均 fail closed。
- 证明 DTO 只包含不可逆 timeline key、active/complete、安全 model、时间、integer usage 与 pricing evidence，不泄露 raw identity、正文、tool、路径、offset、generation、SQL 或 driver cause。
- 证明缺 active cost generation 时 usage/lifecycle 仍可读，而 cost 为 unknown + partial；明确 unpriced 与 unknown 不混淆，真实零不转成 unknown。
- 证明 Store 继续使用 GORM-first 与 Pure-Go SQLite，生产路径不新增 `.Raw`、`.Exec`、schema、index 或写入。

## 执行副作用

- Go tests 会写入 build/test cache；Store integration tests 在 `testing.T.TempDir()` 创建临时 Pure-Go SQLite、WAL/SHM 和 synthetic rows，并由测试框架清理。
- `npm ci/test/typecheck` 会写入 npm cache与 ignored `frontend/node_modules/`；Wails generation 会更新 tracked generated TypeScript，完整 verify/package 还可能创建 ignored `frontend/dist/`、`.task/` 与 `bin/`。
- focused 与 full gate 不启动 server、不绑定端口，不读取真实 Codex Home、JSONL、用户 SQLite、credential、Session 内容或外部业务系统。
- 不触达 GitHub Actions 或 release；外部写入仅在 closeout 阶段按 Root Goal 回写 Linear、GitHub branch/PR/merge。

## 前置条件

1. 当前目录为仓库根目录，当前分支包含 TOO-307 变更。
2. Go 1.25、Node/npm、`make`、`rg` 可用；Wails CLI 精确为 `v3.0.0-alpha2.117`。
3. module 版本继续包含 `gorm.io/gorm v1.31.2`、`github.com/libtnb/sqlite v1.2.0`、`modernc.org/sqlite v1.53.0`；`CGO_ENABLED=0` 的目标生产编译闭包不得包含 `gorm.io/driver/sqlite` 或 `mattn/go-sqlite3`。`go.mod/go.sum` 可保留 GORM 自身测试依赖记录，不能据此误判为运行时依赖。
4. 所有 fixtures 使用 synthetic identity 与 safe attribution；命令输出不得记录 opaque cursor payload、完整本机路径或真实用户事实。

## Acceptance matrix

| 验收面 | 入口 | 预期 |
| --- | --- | --- |
| Store snapshot | `internal/store/analytics_query_repository_test.go` | 同一 View 返回 aggregate + limit+1 Turn page；stable DESC、next cursor、safe attribution |
| generation semantics | Store/query tests | active generation 可 priced/unpriced；fallback usage 可用但 cost unknown + partial |
| page validation | `internal/query/usagecost/session_test.go` | default 20/max 50；非法 limit、tamper、cross-session cursor 拒绝 |
| reconciliation | `internal/query/usagecost/session_test.go` | 完整首屏与 aggregate 精确一致；截断/后续页不超过 aggregate 下界，pricing version/reason 属于 Session evidence |
| null/zero/lifecycle | Store/query tests | active/complete、absent/zero usage、observed/start/complete 顺序和 JS safe integer fail closed |
| privacy | DTO/JSON 与 deny-list | 不含 raw Turn/Session identity、内容、tool、path、offset、generation、SQL/driver cause |
| generated contract | frontend bindings tests | 15 methods 不变；request/response/enum reachable shape 可编译，generation second-run stable |
| persistence | CGO-off 与 source guard | GORM + Pure-Go SQLite 通过；无 Raw/Exec、schema/index/write |

## 主路径

### 1. Focused Store / Query / App

```bash
go test ./internal/store -run 'TestSessionAnalytics.*Turn' -count=10
go test -race ./internal/store -run 'TestSessionAnalytics.*Turn' -count=3
go test ./internal/query/usagecost -count=10
go test -race ./internal/query/usagecost -count=3
go test ./internal/app -count=10
CGO_ENABLED=0 go test ./internal/store/sqlite ./internal/store ./internal/query/usagecost -count=1
```

预期：全部退出 0，无 race report；fallback、known-empty、priced/unpriced、AEAD cursor、aggregate/page reconciliation、Store cursor 跨 SQLite reopen 与 malformed stored shape 均有自动化断言。

### 2. Generated TypeScript

```bash
npm --prefix frontend test -- --run src/bindings/contracts.test.ts src/queries/business.test.ts
npm --prefix frontend run typecheck
PATH="/tmp/codex-pulse-tools/bin:$PATH" wails3 task generate:bindings
git diff --check
```

预期：2 个 test files / 7 tests 与 typecheck 通过；generation 显示 319 packages、1 service、15 methods、34 enums、66 models、1 event，第二次运行不改变 bindings diff。

### 3. GORM / dependency / privacy guards

```bash
if rg -n '\.Raw\(|\.Exec\(' internal/store/analytics_query_session.go internal/query/usagecost; then
  exit 1
fi
if CGO_ENABLED=0 go list -deps ./internal/store ./internal/query/usagecost | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
go list -m all | rg 'gorm.io/gorm v1.31.2|github.com/libtnb/sqlite v1.2.0|modernc.org/sqlite v1.53.0'
go vet ./internal/store ./internal/query/usagecost ./internal/app
gofmt -d internal/store/analytics_query_records.go internal/store/analytics_query_session.go internal/query/usagecost
git diff --check
```

预期：两个 deny-list 无输出；三项版本精确命中；vet/gofmt/diff 退出 0。所有 projection/join/order 是固定常量，client value 只通过参数绑定进入 GORM。

### 4. Full repository

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
make verify-architecture
PATH="/tmp/codex-pulse-tools/bin:$PATH" make verify-architecture
PATH="/tmp/codex-pulse-tools/bin:$PATH" make verify
git diff --check
```

预期：Go、race、vet、tidy、harness、project/version、frontend、generated 与 package 全部通过；允许仓库既有 macOS linker warning，但不得掩盖新增 error。

## 失败处理

| 失败点 | 停止条件 | 恢复方式 |
| --- | --- | --- |
| snapshot/page | 跨 generation、重复/缺口、错误 next cursor、读取写入 | 保留 RED，修复 fixed GORM projection/keyset 后重跑步骤 1 |
| unknown/pricing | unknown/0/unpriced 混淆、aggregate 被 page 重算 | 修复 mapper/reconciliation 后重跑步骤 1 |
| privacy | raw identity/content/path/generation 进入 DTO/error | 停止 closeout，补 RED 后删除暴露面并重跑步骤 1～3 |
| generated | method count变化、手写 generated、second-run drift | 回到 Go reachable contract，使用正式生成器重跑步骤 2 |
| full/review | 任一 gate 非零或 blocking finding 未关闭 | 不写提交/PR结果；RED/GREEN 修复并从失败 gate 恢复 |

## Review 与 closeout

1. pre-review gate 通过后将 Linear TOO-307 移到 Codex Review，由独立 subagent 执行 findings-first implementation review。
2. blocking finding 必须由主线程补 RED、完成 GREEN，并交回同一 reviewer 关闭。
3. 完整重读 `changelog-style` skill，只在 `CHANGELOG.md -> Unreleased` 写 `[TOO-307]` 已完成事实。
4. post-integration gate 后由不同 subagent 执行 final scope review；通过才可 commit/push/创建中文 PR/自合并。
5. 最终 `main` 做 post-merge smoke，回写 runbook/plan/Root/Linear 后把 TOO-307 置为 Done，并恢复 TOO-274。

## 结果回写

完成后只写真实命令、数量、时长和脱敏结论；不得预写 review、PR、merge 或 post-merge 为通过。保留本文已写验证事实，不删回空模板。Actions 始终保持 `actions_disabled_by_user`，普通 Execution 不发布。

## 最终 closeout

- PR #40 已由 Codex 自行创建并 squash merge，`main` merge commit 为 `4448b8716d1a0c2ca3ae542630301ed8bd41b760`。
- post-merge 验证与协作结果回写均完成，TOO-307 已置为 Done；GitHub Actions 保持停用，未执行正式发布。
