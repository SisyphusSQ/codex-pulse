# Runtime Schema 验证 Runbook

## 目的

验证 TOO-248 提供的 Source、Job Run、Health Event、Pricing Catalog/Version 运行事实 schema 与 typed repository，重点覆盖：

- 七张 SQLite STRICT 表、外键、CHECK、唯一键与十三个真实查询索引；
- application bootstrap 的原子创建、重复打开与不兼容 contract 失败；
- Source identity/generation/offset、attempt history 与 due query；
- Job 合法状态转换、startup interruption、typed generation/offset cursor 与 interrupted-only resume lineage；
- Health opaque SHA-256 fingerprint 去重、有限 domain/code allowlist、resolve/reopen ordering 与 class-only error persistence；
- Pricing immutable version、半开生效区间、币种隔离、模型规则与 NULL/zero；
- 重放、冲突、回滚、竞态与隐私边界。

## 前置条件

- macOS 15+、arm64。
- Go toolchain 满足 `go.mod`；SQLite store/repository 必须通过 `CGO_ENABLED=0`，Wails app 使用 macOS 默认 CGO。
- 仓库固定版本的 `wails3` 必须已在当前 shell 的 `PATH`；提交版命令不记录机器本地安装目录。
- 仓库工作目录为 Codex Pulse 根目录。
- 不需要网络、账号、token、真实 Codex Home 或默认应用数据库。

## 副作用与输出位置

- focused/integration tests 只在 `t.TempDir()` 中创建 synthetic SQLite、`-wal`、`-shm` 文件，测试结束由 Go 自动清理。
- Go build/test cache 使用本机 Go 默认缓存目录；runbook 不清空全局缓存。
- 完整 `make verify` 可能生成仓库内 ignored 的 `frontend/node_modules/`、`frontend/dist/`、`.task/`、`bin/`；closeout 前按仓库既有方式清理生成物并保留 tracked `.gitkeep`。
- 不访问 `~/Library/Application Support/Codex Pulse`，不读取真实 Session JSONL，不写外部 API。
- privacy test 使用 synthetic secret marker；提交版文档不得记录 marker、临时绝对路径或 raw command output。

## 验证命令

### 1. Schema 与应用 bootstrap

```bash
CGO_ENABLED=0 go test ./internal/store \
  -run 'RuntimeSchema|ApplicationSchema|Migration' -count=1
go test ./internal/app -run 'ApplicationSchema|ConfiguredStore' -count=1
```

### 2. Runtime repository 行为

```bash
CGO_ENABLED=0 go test ./internal/store \
  -run 'Source|Job|Health|Pricing|RuntimeError' -count=1
```

### 3. 幂等、冲突与恢复重复矩阵

```bash
CGO_ENABLED=0 go test ./internal/store \
  -run 'Source|Job|Health|Pricing|RuntimeError' -count=50
```

### 4. 受影响包 race 矩阵

```bash
CGO_ENABLED=0 go test -race ./internal/store ./internal/store/sqlite -count=10
go test -race ./internal/app -count=10
```

### 5. 全仓回归与机械门禁

```bash
go test ./... -count=1
go vet ./...
go test -race ./...
make verify-architecture
git diff --check
make verify
```

## 预期结果

- 新数据库一次创建 6 张 core 表和 7 张 runtime 表，全部 runtime 表为 STRICT；重复 bootstrap no-op。
- 不兼容 runtime object 返回 `store.ErrSchemaContract`，本轮 DDL 回滚，应用 opener 关闭 Store。
- Source/Job/Health/Pricing 四域独立写入、关联查询、历史与 null/zero round-trip 均通过。
- 非法 transition、stale generation/offset、same-key conflict、health time regression、pricing mutation 均被拒绝且无部分写入。
- exact replay 不增加 attempt/health/pricing 行或 occurrence；job resume 不改变旧 interrupted 行，公共 create 不能绕过 resume lineage。
- pricing as-of 使用 `[effective_from, next_effective_from)`，找不到 version/rule 返回 `ErrNotFound`，不返回虚假零价。
- 受控 code/digest/cursor/error 输入面拒绝 synthetic token/cookie/authorization/raw error/prompt/response/tool output marker，数据库 bytes 不出现这些被拒绝的 marker；cursor 只落两个整数，payload/fingerprint 只落 opaque SHA-256 digest，错误只落 allowlisted class。
- Source/Job identity、路径等语义 metadata 不属于通用 secret scanner 的职责；调用方不得把凭据放入这些字段。
- 全部命令退出码为 0；`git diff --check` 无输出；closeout 后 tracked/ignored 生成物符合仓库 clean-state 要求。

## 失败诊断

- Schema contract：先检查 `runtimeSchemaObjects` canonical DDL 与 `sqlite_schema` readback；不得通过 drop/rebuild 掩盖不兼容。
- Source：检查 stable identity、generation/offset/size ordering 与 same timestamp exact replay。
- Job：检查 expected state、phase/progress/time 单调性、terminal immutability 与 resume lineage。
- Health：检查 fingerprint identity、observation timestamp、occurrence count、resolved/reopened lifecycle 与 nullable FK。
- Pricing：检查 source/currency/effective ordering、version/rule immutability、integer units 与 match precedence。
- Privacy：若 synthetic marker 经受控 code/digest/cursor/error 输入面进入 DB bytes 或 typed snapshot，视为 blocking finding；删除旁路后重新执行完整矩阵。业务 identity/path metadata 需在调用方边界单独保证凭据卫生。
- Race/SQLite：保留最早失败命令与稳定 error chain，先用 `-count=1` 复现；不把 busy/IO/cancel 归因于业务状态机。

## 清理与回滚

- focused tests 的临时 DB 由 `t.TempDir()` 自动删除；失败后若测试进程异常退出，只删除对应测试临时目录，不触碰默认应用数据库。
- 仓库内生成物按项目既有 clean-state 规则清理；不得删除用户已有 ignored 计划、state/run 文件。
- 本卡回滚单元为 runtime schema、typed repository、application bootstrap 与同步文档；pricing 历史不得通过回滚脚本静默覆盖。
- 真实旧库 migration/backup/restore 使用 `docs/test/migrations.md`；本 runbook 仍要求不兼容 schema fail closed。

## 本次执行摘要

| 项目 | 结果 |
| --- | --- |
| 执行状态 | TOO-248 已由 PR #8 合并为 `917c57b` 并完成 post-merge verify；Linear Done |
| focused schema/bootstrap | PASS：七表/十三索引/13 表应用 bootstrap、不兼容 core/runtime fail-closed |
| focused repository | PASS：有限 HealthCode、domain/code pair、opaque SHA-256 与 DDL 旁路已复现 RED 并修复 GREEN；19 个允许组合逐一通过 Repository + DDL |
| replay/conflict `-count=50` | PASS（Rework #2 重跑） |
| affected race `-count=10` | PASS（Rework #2 重跑）：`internal/store`、`internal/store/sqlite`、`internal/app` |
| full test/vet/race | PASS（Rework #2 重跑）：`go test ./... -count=1`、`go vet ./...`、`go test -race ./...` |
| harness/project/diff/version | PASS（Rework #2 重跑）：version check 为 `findings=[]` |
| final `make verify` | PASS（Rework #2 重跑）：Go、vet、frontend typecheck/test/build、generated stability 与 Wails package；arm64/macOS 15.0/ad-hoc bundle 与 zip verify PASS |
| privacy audit | PASS：opaque SHA-256、有限 domain/code、typed cursor、DDL constraint、main/WAL/SHM bytes marker 与 50x/10x stability |
| live E2E | PASS（本卡范围）：真实临时 SQLite repository integration；未读取真实用户数据、未访问网络服务 |
| M2 aggregate | PASS：后续 TOO-249 migration/backup 与 TOO-250 retention/v2 index 已合并；`main@090b5ee` 完整 M2 post-merge gate 通过 |

执行说明：Wails CLI 使用仓库固定版本 `v3.0.0-alpha2.117`，并在执行前补齐当前 shell 的 PATH；frontend 使用 `npm --prefix frontend ci` 恢复 199 个锁定依赖，audit 0 vulnerabilities。Rework #2、CHANGELOG 集成后与 post-merge 均执行完整门禁，不沿用前序 PASS；version classification 为 `changelog-only`、check 为 `findings=[]`。验证结束已执行 package clean，并删除本轮 `frontend/node_modules/`、`frontend/dist/` 构建文件、`.task/` 与 `bin/`，保留 tracked `frontend/dist/.gitkeep`；`go.mod`、`go.sum` 与 generated bindings 无漂移。

已知非阻塞环境输出：macOS CGO 链接阶段持续提示 SDK deployment target warning，但全部相关命令退出码均为 0；这与既有 SQLite 测试环境一致。锁定依赖中的 `glob@10.5.0` 输出 deprecated warning，但 `npm audit` 为 0，本卡不顺手升级依赖。
