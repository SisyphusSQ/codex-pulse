# 本地 JSONL 配额观测 Runbook

## 当前验证结果

- 记录时间：2026-07-15 14:25 CST
- 记录目录：仓库根目录
- 本轮任务性质：TOO-262 本地 Codex JSONL quota observation 解析、投影与 GORM 持久化
- 当前结论：`PASS（已合并并完成 post-merge verify）`；独立实现评审三项 finding 与最终 scope review 的 zero-reset finding 均已修复并复审清零；PR #25 已合并为 `91841c7`，main focused/race/full/Wails 门禁通过，Linear TOO-262 已读回 Done。
- 自动化入口：`internal/codex/logs`、`internal/indexer`、`internal/store`
- 对应 issue：TOO-262
- 结果说明：全部 fixture 为 synthetic upstream-shaped JSONL，SQLite 仅位于测试临时目录；三项评审回归重复 20 次、3,160,033 次 parser fuzz、受影响包与全仓 test/race/vet、Pure Go 依赖、harness/project/version/diff 与完整 `make verify` 均通过；未读取真实 Codex Home 或默认应用数据库。

### 本次执行结果

- 执行时间：2026-07-15 14:25 CST
- 执行目录：仓库根目录
- 本次结论：`PASS（含 main post-merge verify）`
- 影响范围：Go 编译/测试缓存，以及 `testing.T.TempDir` 下的 synthetic Codex Home 与 Pure Go SQLite/WAL/SHM。
- 清理结果：测试临时文件已由 Go 自动清理；本轮生成的 app/ZIP/binary/frontend dist/`.task` 产物已删除并保留 tracked `.gitkeep`；既有 frontend dependency cache 未改动。
- 敏感信息处理：不读取真实 `~/.codex`，不保存原始 JSONL、prompt、response、reasoning、tool output、limit name、credits、未知字段、token、cookie、Authorization 或本机临时路径。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| protocol / synthetic fixture | PASS | 对齐 `event_msg.token_count.rate_limits` 的 snapshot/window 结构；不读取真实 rollout |
| parser compatibility / privacy | PASS | quota-only、usage+quota、partial window、missing/expired/zero-reset/unknown string plan；非法类型 fail closed |
| projector / ingest transaction | PASS | observation ID 含 physical source lineage；replacement generation 0 可激活 |
| schema v9 / GORM repository | PASS | source provenance、coalescing receipt、A→B→A exact replay 与 rollback 回归通过 |
| affected package full tests | PASS | `internal/codex/logs`、`internal/indexer`、`internal/store` |
| 完整 race / vet / harness / Wails | PASS | zero-reset 修复后全仓 race 明确退出码 0；version findings 为空；arm64/macOS 15 ad-hoc app/ZIP 验证通过 |
| implementation review / CHANGELOG | PASS | 三项 finding 修复后 `blocking_findings: 0`、`READY_FOR_CHANGELOG: YES`；Unreleased feature 已写入 |
| final scope review | PASS | zero-reset finding 修复后 `blocking_findings: 0`、`READY_TO_COMMIT: YES` |
| PR / merge / post-merge | PASS | PR #25 已合并为 `91841c7`；main focused/race/full/Wails 门禁通过，Linear TOO-262 已读回 Done |

## 目标

- 证明 `event_msg.token_count.info` 与 `rate_limits` 独立解析，quota-only 记录不会被当成空事件，token usage 缺失也不会伪造计数器。
- 证明只持久化 allowlisted `limit_id/window/used/reset/plan/time/validity/provenance`，未知字段与内容型 payload 不出现在 event、diagnostic、checkpoint 或数据库。
- 证明 primary/secondary partial response、缺失字段、过期或 zero reset、未知 plan 和 schema drift 都有稳定语义；`resets_at=0` 作为 `suspicious/reset_not_future` 可贯穿持久化，负数/溢出等非法数据只产生 content-free diagnostic，不生成 `used_percent=0`。
- 证明 parser → projector → `FactBatch` → `CommitIngestBatch` → GORM repository 与 committed offset 位于同一个 writer transaction；physical source replacement 不碰撞，coalesced sample 有 durable exact-replay receipt，冲突与 source/generation/offset 漂移 fail closed。
- 证明 application schema v9 append-only，v1～v8 checksum 不变，fresh/v8 upgrade/失败回滚与 Pure Go SQLite contract 通过。

成功标准：下列 focused/repeat/race/full gates 全部退出码为 0；提交版文档不含真实用户数据或本机痕迹；Actions 保持 `actions_disabled_by_user`。

## 执行副作用

- 可能写入的本地文件：`testing.T.TempDir` 下的 synthetic rollout、SQLite/WAL/SHM；完整 `make verify` 可能生成仓库已忽略的 frontend/build/package 产物。
- 可能访问的服务 / 数据库 / 外部系统：无。所有数据库均为临时 Pure Go SQLite，不连接真实应用库或网络 API。
- 可能创建的临时数据：合成 Session、quota window、checkpoint、migration backup 与 schema rows。
- 明确不会触达的范围：真实 Codex Home、默认应用数据库、`auth.json`、Wham、GitHub Actions、release/tag、外部 API。
- 执行前必须向协作者说明以上副作用；closeout 后清理本轮 ignored build/package 产物，但不删除用户已有本地 state/run/plan。

## 前置条件

1. 当前目录为 Codex Pulse 仓库根目录，分支只包含 TOO-262 计划内修改。
2. Go toolchain 满足 `go.mod`；SQLite 相关命令显式使用 `CGO_ENABLED=0`。
3. 完整 Wails gate 使用仓库锁定工具链与默认 macOS CGO。
4. 不需要账号、token、真实 rollout 或网络。
5. GitHub Actions 已按用户要求停用；本 runbook 不查询、触发或等待 CI。

## 主路径

### 1. Parser、兼容性与隐私

```bash
CGO_ENABLED=0 go test ./internal/codex/logs -run 'Quota|RateLimit' -count=20
CGO_ENABLED=0 go test -race ./internal/codex/logs -run 'Quota|RateLimit' -count=10
```

预期结果：quota-only 与 usage+quota 独立输出；合法 partial window 保留，非法 window 只有固定 diagnostic；unknown plan 只输出 `unknown`；synthetic private marker 不可见。

### 2. Projector 与端到端 ingest

```bash
CGO_ENABLED=0 go test ./internal/indexer -run 'Quota' -count=20
CGO_ENABLED=0 go test -race ./internal/indexer -run 'Quota' -count=10
```

预期结果：observation ID 稳定且不含路径/内容，并纳入 durable `source_file_id`；Session、physical source、generation、line start offset、window kind 可读回；相同位置的新 inode 不碰撞；facts/checkpoint/committed offset 原子提交。

### 3. Schema v9 与 GORM repository

```bash
CGO_ENABLED=0 go test ./internal/store \
  -run 'QuotaObservation|SchemaV9|V8ToV9|FailedV9|RuntimeSchema|Migration' \
  -count=20
CGO_ENABLED=0 go test -race ./internal/store -run 'QuotaObservation|SchemaV9|V8ToV9|FailedV9' -count=10
if CGO_ENABLED=0 go list -deps ./internal/store | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
```

预期结果：v9 checksum 冻结，v8→v9 保留事实，失败整体回滚；普通 CRUD/query/coalescing/replay receipt 使用 GORM；A→B→A 后旧 sample exact replay no-op；实际编译依赖不含 official sqlite driver 或 mattn。

### 4. 受影响包与全仓门禁

```bash
CGO_ENABLED=0 go test ./internal/codex/logs ./internal/indexer ./internal/store -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make verify-architecture
git diff --check
make verify
```

预期结果：全部命令退出码为 0；version findings 为空；frontend/generated/macOS package gate 通过；不创建 release、tag 或 GitHub Actions run。

### 5. 提交版隐私与清理

```bash
local_path_pattern='/''Users/'
rg -n 'Authorization:[[:space:]]*Bearer|Bearer [A-Za-z0-9+/=_-]{12,}|http://127\.0\.0\.1:[0-9]+' \
  docs/test/local-jsonl-quota.md docs/design/details/quota/README.md \
  docs/design/details/data-model/README.md
rg -n "$local_path_pattern" docs/test/local-jsonl-quota.md \
  docs/design/details/quota/README.md docs/design/details/data-model/README.md
git status --short
```

预期结果：没有真实凭据、URL 或机器本地绝对路径；只保留 TOO-262 计划内 tracked 修改和仓库既有 ignored local state。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| parser/schema drift | 非法字段生成 observation、合法 partial 被整体丢弃、未知字段泄露 | 只记录固定 diagnostic/test 名，不复制 JSONL | 收窄 DTO/allowlist 后重跑步骤 1 |
| projector/provenance | ID 不稳定、generation/offset 错误、Session 漂移 | 记录 synthetic ID 与安全整数 | 修复投影后重跑步骤 2、3 |
| transaction/replay | offset 提前推进、部分写入、同位置冲突未失败 | 记录 checkpoint/generation/offset 与 sentinel | 从旧 durable checkpoint 重开并重跑步骤 2、3 |
| migration/schema | v1～v8 checksum 漂移、v9 rollback 不完整 | 记录版本与 schema object 名 | 恢复 append-only catalog 后重跑步骤 3 |
| privacy | marker、原始字段或本机痕迹进入提交面 | 只记录命中文件/字段 | 删除旁路字段，重跑步骤 1、5 |
| full gate | test/race/vet/harness/Wails 非零 | 记录首个失败 gate 的脱敏摘要 | 修复后从受影响 focused gate开始，最终完整重跑步骤 4 |

## 结果回写

- 执行后更新本文顶部的真实时间、结论、步骤状态和清理摘要；未执行项不得写成通过。
- 原始长输出与本机路径只留在 `.artifacts/runs`，不进入提交版。
- 普通 Execution 只更新 `CHANGELOG.md -> Unreleased`，不创建版本、tag、GitHub Release 或正式发布。
- Actions 保持 `actions_disabled_by_user`，不把未运行 CI 写成失败或通过。
