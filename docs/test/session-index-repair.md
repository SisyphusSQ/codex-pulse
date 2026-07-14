# Session Index Repair 验证 Runbook

## 当前验证结果

- 记录时间：2026-07-14（Asia/Shanghai）
- 对应 Issue：TOO-256
- 当前结论：implementation review 已清零，terminal success 已改为同事务 snapshot compare + state transition，CHANGELOG 集成后的 post-integration gate 已通过；final scope review 发现 upstream-valid unsupported latest 行与本地状态文档问题，当前处于 fail-closed Rework，尚未 final scope PASS。
- Actions：`actions_disabled_by_user`；本 runbook 不启用、查询、触发或等待 GitHub Actions。
- Release：不适用；普通 Execution Issue 不创建 tag、release 或正式发布产物。

## 目标

- 证明 dry-run、错误 confirmation、plan/index/Store 漂移和 unresolved conflict 都保持 repair 零写入。
- 证明只把 missing 或 Store timestamp 严格更新的 stale name 转成 append correction，保留正常 rename history。
- 证明确认执行先完成 Codex Pulse SQLite 与 index 双备份，再 append、`fsync` 并重新对账。
- 证明 backup failure、append 后中断、startup interrupt、重新 dry-run 和 succeeded replay 都有确定恢复语义。
- 证明生产路径使用 GORM-first Pure Go SQLite，不依赖 `gorm.io/driver/sqlite` 或 `github.com/mattn/go-sqlite3`。
- 证明不读取、复制或修改真实 `~/.codex` 以及 `sessions/`、`archived_sessions/` 原始 JSONL。

## 副作用与输出位置

- focused/live E2E 只在 `testing.T.TempDir()` 下创建 synthetic Codex Home、Codex Pulse SQLite、WAL/SHM、index、原始 Session sentinel 和双备份，测试结束自动清理。
- coverage profile 写到 `${TMPDIR:-/tmp}/codex-pulse-too256.cover`，验证后删除。
- `make verify` 会使用本机 Go/npm/Wails 缓存，可能生成 ignored 的 `frontend/node_modules/`、`frontend/dist/`、`.task/`、`bin/` 和 packaging 产物；closeout 只清理本轮生成物并保留 tracked `.gitkeep`。
- 不访问网络、真实 Codex Home、真实应用数据库、GitHub Actions 或 release 服务。

## Frozen Contract

| 场景 | 结果 |
| --- | --- |
| index 缺少 expected Session ID | plan 追加 `missing` correction |
| 名称不同且 Store field timestamp 严格更新 | plan 追加 `stale` correction |
| index 更新/相等/时间无效 | `index_newer_or_unknown` conflict，Execute 零写入 |
| 同 ID 多个有效行 | 记录 history count；最后有效 append 行获胜，不 compact |
| malformed / duplicate JSON key | content-free line diagnostic；不把原文写入 plan/DB |
| 缺失/null 必填字符串或 invalid UUID | 上游也无效；content-free diagnostic 后跳过 |
| 空白/>4096-byte 名称或 >1 MiB schema-valid 行 | 上游仍可能视为有效 latest；Analyze 整体 fail closed，不创建 plan/job/backup，不继续 correction |
| `updated_at="unknown"` / 非 RFC3339 | 仍按 upstream String contract 参与 latest view；只能同名对齐，异名 conflict，不作为 stale 排序证据 |
| plan DTO 或 preflight 已存在的 Store projection/index version 漂移 | confirmation 失效；audit/backup/index 零写入 |
| 双备份任一步失败 | job failed；index 不追加；已有 backup 保留 |
| append 后进程中断 | running job 由 startup recovery interrupt；重新 dry-run 不重复写 |
| succeeded plan replay | 重新 reconcile 后返回 replay report；不追加重复行 |
| Store 在 backup/append/reconcile/terminal 窗口变化 | 多阶段 digest 复核；最终 snapshot 比较与 succeeded transition 同一 writer transaction；job failed，不能 succeeded/replayed |
| Codex 在 final check 后并发 append | 写后 expected full SHA/size proof 失败并审计为 failed；保留所有 append bytes |
| correction 超过剩余容量 | Analyze/Verify 阶段拒绝；不创建 audit/backup |
| 新目录/backup/new index 掉电持久性 | 文件和父目录均 fsync；目录 sync failure 不进入 append/success |

## 验证命令

### 1. Focused、重复与 race

```bash
CGO_ENABLED=0 go test ./internal/codex/index ./internal/store \
  -run 'Test.*SessionIndex|Test(Parse|Analyze|IndexFile|Service|ValidateAppendSize)' -count=1
CGO_ENABLED=0 go test ./internal/codex/index ./internal/store \
  -run 'Test.*SessionIndex|Test(Parse|Analyze|IndexFile|Service|ValidateAppendSize)' -count=20
CGO_ENABLED=0 go test -race ./internal/codex/index ./internal/store \
  -run 'Test.*SessionIndex|Test(Parse|Analyze|IndexFile|Service|ValidateAppendSize)' -count=1
```

成功判据：latest-wins、missing/stale/conflict、duplicate key、immutable plan、Store/index drift、双备份、replay/recovery、symlink/size/末尾换行与原始 Session sentinel 全部稳定通过。

### 2. 隔离 live E2E

```bash
CGO_ENABLED=0 go test ./internal/codex/index \
  -run '^TestSessionIndexRepairLiveE2E' -count=1 -v
```

成功判据：真实 temp filesystem + Pure Go SQLite 完成 Analyze -> exact confirmation -> DB backup -> exact index backup -> append -> reconcile -> succeeded replay；原始 Session JSONL bytes、mode、size、mtime 不变。不得把命令改为指向真实 `~/.codex`。

### 3. Coverage、Pure Go 与 GORM boundary

```bash
COVER_FILE="${TMPDIR:-/tmp}/codex-pulse-too256.cover"
CGO_ENABLED=0 go test ./internal/codex/index ./internal/store \
  -coverprofile="$COVER_FILE" -count=1
go tool cover -func="$COVER_FILE"
rm -f "$COVER_FILE"

if CGO_ENABLED=0 go list -deps ./internal/codex/index ./internal/store | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
if rg -n '\.(Raw|Exec)\(' internal/codex/index internal/store/session_index.go \
  --glob '*.go' --glob '!**/*_test.go'; then
  exit 1
fi
```

成功判据：coverage 命令退出码为 0；实际编译依赖不含禁用 driver；本卡 production 路径不使用 GORM `Raw`/`Exec`。

### 4. 全仓本地门禁

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make harness-verify
make project-check
git diff --check
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
make verify
```

成功判据：全部退出码为 0；普通 feature 只允许 `CHANGELOG.md -> Unreleased`，不得生成 release/version/tag。Actions 不属于本轮 gate。

## 当前实测证据

| Gate | 当前结果 |
| --- | --- |
| RED/GREEN | PASS：重复 key、upstream-valid unsupported latest fail-closed、required-string presence、self-consistent invalid plan、diagnostic raw、conflict zero-write、unterminated append、pre-write size、Store preflight/backup/post-append/terminal/replay drift、post-check concurrent append、>4096 corrections/remaining capacity、directory sync、dense empty-line allocation 均保留先失败后修复证据 |
| focused | PASS：`internal/codex/index` 与 Store expectation query 当前矩阵通过 |
| Pure Go race | PASS：focused race 通过 |
| coverage | PASS：final-scope fail-closed rework 后全包运行 index 74.4%、store 80.1% |
| isolated live E2E | PASS：真实 temp filesystem + Pure Go SQLite 完成双备份、append、reconcile 与 replay；Session sentinel 未变化 |
| dependency / GORM boundary | PASS：CGO0 production deps 未命中 official driver/mattn；本卡 production 文件无 `Raw`/`Exec` |
| full repo / harness / project / version / `make verify` | PASS：CHANGELOG 集成后全仓 test/race/vet、harness、project、`findings=[]`、diff、bindings stability、frontend 和 macOS arm64/minOS 15 ad-hoc app/ZIP 已完整复跑通过 |
| version / release boundary | PASS：分类脚本因 `go.mod` 的 direct dependency 声明机械归为 `version-bump`，但未修改应用版本；普通 Execution 只写 `Unreleased`，不执行 release |
| implementation / final scope review | PASS：implementation reviewer 与 final scope reviewer 最终均 `blocking_findings: 0`；两轮 Rework、同 reviewer 再审和完整门禁均已闭环 |
| PR / merge / post-merge | PENDING |

## 失败处理与恢复

| 失败点 | 停止条件 | 恢复方式 |
| --- | --- | --- |
| dry-run/confirmation 偷写 | 出现 job、backup 或 index byte 变化 | 停止交付，增加 before/after fixture 后从 focused 重跑 |
| Store/index 漂移 | 漂移后仍创建 audit 或追加 | 修正 preflight snapshot/version gate；不得用旧 plan 重试 |
| backup | DB committed audit 未出现在 backup，或 index backup 非 exact bytes | 保留失败 job/已有 backup，修复后重新 dry-run |
| append/reconcile | history 被重写、correction 拼接坏行、写后 latest 不一致仍成功 | 不 restore 覆盖并发 append；修复 append/reconcile 后重新 dry-run |
| interruption/replay | 重启或 replay 重复追加 | 保留 interrupted job，以新 plan 验证 current latest 后恢复 |
| privacy | diagnostic/DB/doc 出现 synthetic raw marker，或真实 Codex Home 被访问 | 立即停止并清理测试产物，只保留 allowlisted诊断 |
| local gate | test/race/vet/harness/project/version/verify 任一失败 | 同分支修复并完整重跑；不得用 disabled Actions 替代 |

## 结果回写

- 每轮执行后更新“当前验证结果”和“当前实测证据”，保留已完成的脱敏摘要，不删回空模板。
- 原始长输出只保留在本地 `.agents/runs/TOO-256-session-index-repair.md`；提交版不记录机器绝对 temp path、原始 JSONL 或凭据。
- implementation review 清零后才按 `changelog-style` 写唯一 `[TOO-256]` `Unreleased` 条目；post-integration 与不同 subagent final scope review 通过后才 commit/PR/merge。
