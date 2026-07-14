# SQLite GORM Migration、Backup 与 WriteUnit 验证 Runbook

## 当前验证结果

- 记录时间：2026-07-14 23:28 CST
- 对应 Issue：TOO-249、TOO-250、TOO-253、TOO-254、TOO-255
- 当前结论：`PASS`；Pure Go Store、v2 retention、v3 durable ingest、v4 attribution 与 v5 pricing/cost schema 均已按 append-only migration 合并并完成 main post-merge 验证。
- 已验证：Pure Go GORM Store/Repository、fresh/legacy/v1/v2/v3/v4/current-v5 migration、v1～v5 checksum freeze、history drift/newer/divergence、全 pending rollback、modernc backup/restore、目录持久化发布、typed WriteUnit、durable ingest、attribution 与 pricing/cost STRICT/FK/index contract、默认应用 bootstrap、Pure Go race、全仓 test/vet/race、harness/project/version 与完整 `make verify`。
- 后续要求：新增 migration 必须继续 append-only 并冻结各版本 checksum；不得重算 v1～v5 history。

## 目标

- 证明 `internal/store/sqlite` 与 `internal/store` 在 `CGO_ENABLED=0` 下只使用 libtnb + modernc 的 Pure Go SQLite 编译链。
- 证明 GORM persistence models 与 domain types 分离，普通 CRUD/query/关联检查不依赖裸 SQL。
- 证明 migration catalog、v1～v5 append-only checksum ledger、`PRAGMA user_version` 和 canonical schema 一致，且调用契约限定为 Store 暴露给 runtime reader/writer 前的启动期 bootstrap。
- 证明已有用户 schema 在 migration 前创建可恢复 backup，fresh/current 跳过，失败不推进版本。
- 证明 `WriteUnit` 可跨 core/runtime typed operations 原子提交，并保留 error/panic/cancel rollback。

## 副作用与输出位置

- focused tests 只在 `testing.T.TempDir()` 下创建 SQLite、WAL/SHM、`backups/*.db`、restore 目标和 `.partial-*`；正常结束由 Go 自动清理。
- Go test/vet/race 使用本机默认 build/test cache。
- `make verify` 可能生成 ignored 的 `frontend/node_modules/`、`frontend/dist/`、`.task/`、`bin/`；closeout 前按仓库既有规则清理。
- 不读取真实 Codex JSONL、真实应用数据库、凭据或用户目录，不访问外部服务，不创建 release/tag。

## 前置条件

1. 工作目录是 Codex Pulse 仓库根目录，目标分支只包含当前 Issue 预期改动。
2. Go toolchain 满足 `go.mod`；全仓 Wails 验证要求 macOS 15+ arm64 与默认 CGO。
3. SQLite Pure Go门禁只覆盖 `internal/store/sqlite`、`internal/store` 及其实际编译依赖；Wails macOS adapter 自身需要 CGO。
4. 固定版本 `gorm.io/gorm v1.31.2` 上游 `go.mod` 带有 official SQLite driver/mattn 间接元数据；验收看 `go list -deps` 的实际编译链，不把上游元数据误报成运行依赖。
5. 仓库固定版本的 `wails3` 必须已在当前 shell 的 `PATH`；提交版命令不记录机器本地安装目录。

## 验证命令

### 1. Pure Go driver、GORM callback 与 Repository compatibility

```bash
CGO_ENABLED=0 go test ./internal/store/sqlite ./internal/store -count=1
if CGO_ENABLED=0 go list -deps ./internal/store | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
```

成功判据：Store lifecycle、TOO-246～248 既有 repository contract 和 GORM callback tests 全部通过；实际编译依赖没有 official driver/mattn。

### 2. Migration 状态机

```bash
CGO_ENABLED=0 go test ./internal/store \
  -run 'Test(Migration|EnsureApplicationSchema)' -count=1
```

成功判据：

- fresh v0→v4 在同一事务写入 v1/v2/v3/v4 四条稳定 history，重复启动 no-op；
- legacy v0 先 backup，再按顺序应用 v1～v4；已有 v1/v2/v3 先 backup 再只追加 pending versions；current v4 不 backup；
- v1/v2/v3/v4 checksum 各自由独立版本常量与固定值测试冻结，未来 v5 不得重算既有 history；
- catalog 缺号/空名/无 apply/非法 checksum、history checksum drift、ledger/user_version 分叉和 newer schema 都 fail closed；
- 后续 pending apply 失败时，先前 pending objects、ledger 与 `user_version` 全部 rollback；
- `MigrationFailure` 提供稳定 stage/code/current/target/failed version/backup path，并保留 wrapped cause。

### 3. Backup 与 restore

```bash
CGO_ENABLED=0 go test ./internal/store/sqlite \
  -run 'Test(Backup|Restore)' -count=1
CGO_ENABLED=0 go test ./internal/store \
  -run 'TestApplicationMigrationCreatesRestorablePreMigrationBackupForLegacyDatabase' -count=1
```

成功判据：modernc `NewBackup` / `NewRestore` 按页 `Step`，进度单调且最终 remaining=0；backup 包含 committed WAL rows；目录/文件分别为 `0700/0600`；Link 发布目标与删除临时名后分别同步父目录，任一同步失败均撤销目标并 fail closed；并发目标不得被覆盖；取消或失败不残留 `.partial-*`；legacy backup 保持迁移前 version/history。

### 4. Typed Unit of Work

```bash
CGO_ENABLED=0 go test ./internal/store -run 'TestWriteUnit' -count=1
```

成功判据：FactBatch 与 SourceFile cursor 同 commit；第二步 validation、callback error、panic、cancel 都整体 rollback；callback 返回后捕获的 `WriteUnit` 只能返回 `ErrWriteUnitClosed`，无法继续拥有 transaction。

### 5. Race、应用 bootstrap 与全仓门禁

```bash
CGO_ENABLED=0 go test -race ./internal/store/sqlite ./internal/store -count=1
go test ./internal/app -count=1
go test ./... -count=1
go vet ./...
go test -race ./...
make harness-verify
make project-check
git diff --check
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
make verify
```

成功判据：所有命令退出码为 0；Wails 链接可出现既有 macOS SDK deployment target warning，但不得有 test/vet/race/gate 失败；Actions 已按用户要求停用，本 runbook 不等待或触发 CI。

## 本次执行摘要

| Gate | 结果 |
| --- | --- |
| Pure Go focused/full | PASS：`CGO_ENABLED=0` 的 SQLite/store focused、全量和 race 均通过 |
| 实际依赖链 | PASS：`go list -deps ./internal/store` 不含 official driver/mattn；上游模块元数据差异已记录 |
| migration/backup/restore/UoW | PASS：真实临时 SQLite、WAL backup、restore、失败注入和 rollback 矩阵通过 |
| current v4 attribution | PASS：带既有 facts 的 v3→v4 同事务 backfill、失败整体 rollback、backup/history、STRICT/FK/index、ID/display tuple 与显式重算通过；PR #15 / `1df28c3` 已完成 post-merge |
| current v5 pricing/cost | PASS：v4→v5 append-only schema、pricing metadata、cost generation、turn/session/global/project/model rollup 与 v1～v5 checksum freeze 通过；PR #16 / `7c464af` 已完成 post-merge |
| app/full test/vet/race | PASS：默认 macOS CGO 下 app 与全仓通过；仅有既有 deployment-target linker warning |
| harness/project/version/diff | PASS：project constraints 通过，版本 check `findings=[]`，`git diff --check` 无输出 |
| `make verify` | PASS：Go、vet、前端 typecheck/test/build、generated stability、arm64/minOS 15/ad-hoc app/zip 全部通过 |
| TOO-254 独立实现 review | PASS：首轮 High backfill、Medium invalid→missing 已按 TDD 修复；同 reviewer 复核 `blocking_findings: 0` |
| 历史 Final scope / merge | PASS：TOO-249 PR #9 / `238eaa2`、TOO-250 PR #10 / `090b5ee`、TOO-254 PR #15 / `1df28c3`、TOO-255 PR #16 / `7c464af` 均完成同口径 post-merge 验证 |
| Actions | `actions_disabled_by_user`：未触发、未等待、未把 CI 作为 gate |
| closeout 清理 | PASS：post-merge 复验后的 `.task/`、`bin/`、`frontend/node_modules/` 与构建产物已清理，保留 tracked `.gitkeep` |

## 失败处理

| 失败阶段 | 停止条件 | 保留证据 | 恢复方式 |
| --- | --- | --- | --- |
| catalog/inspect | 任意 descriptor/history/version 不一致 | stable stage/code/version，不复制数据库正文 | 修复 catalog 或使用已验证恢复流程，重新从步骤 2 开始 |
| space/backup | 空间不足、Backup API、权限、取消失败 | required/available 摘要、是否存在成功 backup path | 释放空间或修复路径；确认无 partial 后重跑 |
| apply/verify | 任一 pending DDL、ledger、canonical readback 失败 | failed version、sentinel、成功 backup path | 确认版本仍未推进，再修复并完整重跑 |
| WriteUnit | 部分写入、panic/cancel 未 rollback | 失败测试名与行计数摘要 | 修复事务体复用，重跑步骤 4 和全量 repository |
| Wails/机械门禁 | app、vet、race、harness/project/version/make verify 非零 | 最早失败命令与脱敏摘要 | 修复后从受影响 focused gate 开始并最终重跑步骤 5 |

## 清理与结果回写

- focused fixtures 由 `TempDir` 清理；异常退出时只删除对应测试临时目录，不触碰默认应用数据库。
- 清理本轮生成的 ignored 前端/package/task 产物，但不得删除用户已有 ignored plan/state/run 文件。
- 执行完成后更新本文顶部的真实命令、结果、时间与清理摘要；未执行的 gate 不得写成通过。
- Issue closeout 记录 `actions_disabled_by_user`，不把未运行的 GitHub Actions 当成失败或通过。
- 已合并版本保留既有结果；后续只追加新 migration 与真实验证结果。普通维护不追加历史 release 段，也不触发版本发布。
