# Session、Turn、Usage 数据模型验证 Runbook

## 当前验证结果

- 记录时间：2026-07-14 06:52 CST
- 记录目录：仓库根目录
- 本轮任务性质：TOO-247 核心事实、投影 Schema 与 typed repository 验证
- 当前结论：`通过`
- 自动化入口：focused Go tests、race detector、`make verify`、diff 与版本策略检查
- 对应计划 / issue：TOO-247 / `2026-07-14-too-247-session-turn-usage-schema.md`
- 结果说明：八轮独立 review 已确认全部 findings 关闭；包含 completion/usage ordering 原子性的 50 轮语义矩阵、10 轮受影响包 race、全仓 test/vet/race、harness/project gate 以及写入 `CHANGELOG.md -> Unreleased` 后的完整 `make verify` 全部通过。

### 本次执行结果

- 执行时间：2026-07-14 06:37～06:52 CST
- 执行目录：仓库根目录
- 本次结论：`通过`
- 影响范围：仅临时 SQLite 数据库、Go 构建/测试缓存，以及 `frontend/node_modules`、`frontend/dist`、`bin`、`.task` 本地生成物。
- 清理结果：测试临时数据库已由 `TempDir` 自动清理；ignored 前端依赖、构建、package 与 task 产物已清理，仓库状态只包含 TOO-247 预期源码、文档和 CHANGELOG。
- 敏感信息处理：不读取真实 Codex JSONL，不写入 prompt、response、tool output、凭据、token、真实用户路径或原始测试输出。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| 前置检查 | 通过 | Go 1.26.2，darwin/arm64，CGO 已启用；当前为 TOO-247 专用分支。 |
| Schema / repository 主路径 | 通过 | 第六轮 Rework 的 completion/usage greater/equal/lower 原子合并已完成 RED→GREEN；受影响包全集通过。 |
| 重放与 race 验证 | 通过 | 包含 completion/usage ordering 原子性的幂等、回滚、generation replacement、跨 Session、completed fact、字段级投影与冲突语义重复 50 轮通过；受影响包 race 重复 10 轮通过。 |
| 全仓与 harness gate | 通过 | 全仓 test/vet/race、`harness-verify`、显式临时 Wails PATH 的 `project-check` 和 `diff --check` 均通过。 |
| CHANGELOG 与完整集成验证 | 通过 | `[TOO-247]` 唯一条目位于 `Unreleased -> feature`；Node/Wails 精确工具链下 `make verify` 的 Go、前端、generated 与 macOS arm64 package gate 全部通过。 |
| 清理 | 通过 | 仅保留 TOO-247 预期源码、设计文档、验证摘要与 CHANGELOG；没有新增生成物。 |

## 目标

- 验证六张 `STRICT` 表、主外键、唯一来源位置、查询索引和 canonical bootstrap contract。
- 验证重复 upsert、整批回滚、generation 乱序重放、projection 单调性以及 unknown/null 与真实零语义。
- 验证应用在 Wails 启动前完成 schema bootstrap，既有不兼容 schema 会拒绝启动。
- 成功标准：下列命令全部成功，测试临时目录由 `testing.T.TempDir` 清理，仓库无意外生成物。

## 执行副作用

- 可能写入的本地文件：系统临时目录中的测试 SQLite 文件、Go build/test cache、`frontend/node_modules`、`frontend/dist`、`bin` 与 `.task`。
- 可能访问的服务 / 数据库 / 外部系统：无；全部数据库均为测试进程创建的本地临时 SQLite。
- 可能创建的临时数据：合成 project/session/turn/usage 记录，不包含真实用户数据。
- 明确不会触达的范围：真实 Codex Home、真实应用数据库、Linear、GitHub、release、签名和更新渠道。

## 前置条件

1. 当前工作目录：Codex Pulse 仓库根目录。
2. 当前分支或版本：待验证的 TOO-247 专用分支或其集成 commit。
3. 必需命令：`go`、`make`、`git`；SQLite store/repository 使用 Pure Go driver，必须额外通过 `CGO_ENABLED=0`；Wails app 与全仓 macOS 验证使用默认 CGO。
4. 必需配置：无真实凭据或外部服务配置。
5. 必需测试环境：macOS arm64；仓库依赖已可由 Go module cache 解析。

## 主路径

### 1. 前置检查

```bash
git status --short --branch
go version
go env GOOS GOARCH CGO_ENABLED
```

预期结果：

- 位于目标分支，只有当前 Issue 预期改动。
- `GOOS=darwin`、`GOARCH=arm64`；SQLite 门禁显式关闭 CGO，Wails app 门禁使用 macOS 默认 CGO。

### 2. Schema 与 repository 聚焦验证

```bash
CGO_ENABLED=0 go test ./internal/store/sqlite ./internal/store -count=1
go test ./internal/app -count=1
```

预期结果：

- canonical schema、唯一索引、外键、隐私列排除和 app bootstrap tests 通过。
- typed upsert/query、rollback、null/zero、source generation 和过滤查询 tests 通过。

### 3. 重放压力与 race 验证

```bash
CGO_ENABLED=0 go test ./internal/store \
  -run 'TestRepository(UpsertFactsIsIdempotent|UpsertFactsRollsBackEntireBatch|CompletedTurnAdvancesOnlyWithFullHigherGeneration|CompletedTurnRejectsOrIgnoresProvisionalFieldReplay|CompletedTurnRequiresFinalUsageAcrossGenerations|TurnOnlyCompletionRemovesProvisionalUsage|CompletionUsageOrderingIsAtomic|UsageSourceGenerationSupersedesLowerOffsets|RejectsUsageAheadOfStoredTurnGeneration|TurnGenerationReplacesCurrentUsage|RejectsCrossSessionBatchAtomically|SessionCurrentPreservesThreadNameByFieldTimestamp|RejectsConflictingPayloadAtSameOrderingKey|StableMetadataRejectsRegressionAndConflicts)' \
  -count=50
CGO_ENABLED=0 go test -race ./internal/store ./internal/store/sqlite -count=10
go test -race ./internal/app -count=10
```

预期结果：

- 重复重放、失败回滚和 generation 单调性在重复执行下稳定通过。
- race detector 不报告 data race。

### 4. 全仓与 base harness gate

```bash
go test ./... -count=1
go vet ./...
go test -race ./...
make harness-verify
PATH="/tmp/codex-pulse-tools/bin:$PATH" make project-check
git diff --check
```

预期结果：

- 全部 Go package、vet、race、harness、项目机械约束和 diff whitespace gate 通过。
- 当前本机 Wails CLI 位于受控临时工具目录，`project-check` 必须显式补入该 PATH；未补 PATH 时 gate 会在执行前以 `TOOLCHAIN-001` 停止，补齐后通过。
- Wails macOS CGO 链接输出可能包含 SDK deployment target warning，但所有相关命令必须退出码为 0；SQLite `CGO_ENABLED=0` 命令不得依赖该链路。

### 5. CHANGELOG 后完整集成验证

写入 CHANGELOG 后先执行完整集成验证：

```bash
npm ci --prefix frontend
PATH="/tmp/codex-pulse-tools/bin:$PATH" make verify
```

预期结果：

- Node lockfile 依赖安装、Go test/vet、前端 typecheck/test/build、generated stability、macOS arm64 Bundle、ad-hoc 签名和 ZIP package 验证全部通过。
- 不创建 release archive、tag 或外部发布 artifact。

### 6. 清理

```bash
git status --short
```

预期结果：

- 测试 SQLite 文件随 `TempDir` 自动删除。
- 清理 ignored 的 `frontend/node_modules`、`frontend/dist` 构建文件、`bin` 与 `.task` 后，除当前 Issue 预期源码、文档和 CHANGELOG 外没有新增生成物。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| 前置检查失败 | 停止 | 记录不匹配的分支或工具链摘要 | 恢复目标分支/工具链后从步骤 1 重跑 |
| Schema contract 失败 | 停止 | 记录失败对象名和 sentinel，不复制本机数据库内容 | 修复 canonical definition 或 fixture 后从步骤 2 重跑 |
| Repository / race 失败 | 停止 | 记录测试名和脱敏断言摘要 | 按 RED-GREEN-REFACTOR 修复后从步骤 2 重跑 |
| Harness / diff 失败 | 停止 | 记录失败 gate | 修复后重跑步骤 4 和全部受影响步骤 |
| 清理失败 | 停止并升级 | 记录残留类型，不写绝对临时路径 | 清理生成物后重新核对 `git status` |

## 结果回写

执行完成后，使用真实结果更新本文顶部的时间、结论、步骤状态和清理摘要。不得把未执行步骤写成通过，也不得提交本机绝对路径、临时文件名或原始命令输出。
