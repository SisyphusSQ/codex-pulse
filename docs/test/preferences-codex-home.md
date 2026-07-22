# Preferences 与单 Codex Home 切换验证 Runbook

## 当前验证结果

- 记录时间：2026-07-14（Asia/Shanghai）
- 对应 Issue：TOO-258
- 当前结论：第四次 implementation review、唯一 `[TOO-258]` `Unreleased -> feature` 条目和完整 post-integration re-verify 已通过；final-scope 第二次 re-review 与 PASS 结果回写 delta-only 终审均 `ZERO_FINDINGS`，`FINAL_SCOPE_PASS=YES`、`FINAL_DELTA_PASS=YES`，已进入 commit/PR。
- Actions：`actions_disabled_by_user`；本 runbook 不启用、查询、触发或等待 GitHub Actions。
- Release：不适用；普通 Execution Issue 不创建 tag、release 或正式发布产物。

## 目标

- 证明 v1 onboarding snapshot 可原子迁移为严格 typed Preferences v2，失败保留原 bytes。
- 证明 private `0700/0600` 文件、cooperative cross-instance lock、revision CAS、原子 replace 和 durability-unknown 语义。
- 证明 Settings 更新只改变 online/refresh/update/UI，stale revision 与 pending switch 均 fail closed。
- 证明单 Home 切换严格执行 re-probe、drain、pending generation、幂等 bootstrap、finalize，并覆盖并发、取消和重启恢复。
- 证明两种策略的事实保留/重建边界明确，Home generation 与逐文件 generation 不混用。
- 证明生产依赖仍为 GORM-first Pure Go SQLite，不新增 schema、`Raw` 或 `Exec`。

## 副作用与输出位置

- focused/integration tests 只在 `testing.T.TempDir()` 下创建 synthetic Codex Home、private preferences、`.preferences.lock`、`.preferences.switch.lock`、symlink 和故障 fixture，测试结束自动清理。
- synthetic Home 中可创建 mode `000` 的 JSONL/auth sentinel；测试只核对 metadata 和最终 bytes，不读取真实用户内容。
- coverage profile 写到 `${TMPDIR:-/tmp}/codex-pulse-too258.cover`，读取后删除。
- `make verify` 使用本机 Go/npm/Wails cache，可能生成 ignored 的 `frontend/node_modules/`、`frontend/dist/`、`.task/`、`bin/` 和 packaging 产物；closeout 只清理本轮生成物并保留 tracked `.gitkeep`。
- 不访问真实 `~/.codex`、`CODEX_HOME`、Application Support、用户 SQLite、网络、GitHub Actions 或 release 服务。

## Frozen Contract

| 场景 | 结果 |
| --- | --- |
| 首次确认 | 直接写 v2 revision=1、Home generation=1 与 typed defaults |
| v1 load | private lock 内 strict decode、atomic replace、独立有界 readback；pre-rename failure 保留 v1，post-rename unknown/cancel 只接受权威 v2 |
| unknown/newer schema、unknown/大小写别名/null field、非法 enum/range/path | `ErrInvalidPreferences`，不修补或覆盖 |
| stale/parallel CAS | 只有一个 revision winner；loser 返回 conflict，旧/胜者快照保持完整 |
| switch execution lease | Confirm/Recover 跨实例串行；live owner 持租约时 contender/recovery 只等待并服从 context，进程退出后由 OS 释放 flock |
| unsafe lock/temp/target | symlink、非 regular 或宽权限拒绝；不触碰指向目标 |
| replace 前故障 | old bytes 权威；replace 后 fsync/cleanup 故障返回 durability unknown |
| Settings exact replay | 不增加 revision；Home、generation、data key、journal 原样保留 |
| preview/same Home/target drift | metadata-only；零 drain、零配置写、零 bootstrap |
| confirm order | acquire execution lease -> persist 带随机 `attempt_id` 的 resume guard -> 仅 owner drain old -> persist target/pending 并清 guard -> start exact bootstrap -> finalize audit -> release lease |
| independent database | 新 key；旧 Home 进入 detached；切回复用原 key，仍只有一个 active Home |
| clear and rebuild | 复用 current key；只授权后续 runtime 清理派生事实，不删 Codex 文件 |
| bootstrap not started / safe rollback | 持久回滚 previous，再 resume old generation |
| queued/running/succeeded/failed-needs-resume | finalize target；不启动第二个 bootstrap |
| status unknown/error | journal 保留并返回 recovery error |
| Resume failure | old Home active 但 `pending_resume` 保留；下次 Recover 幂等重试，成功后才清 marker/audit |
| cancel | confirm 前取消零写；pending 已提交后取消按 durable bootstrap status 完成或回滚 |
| concurrent switch | 不同目标或相同 plan 都只有一个 lease/attempt owner、一个 drain、一个 generation/一次 bootstrap；live contender 等待/超时，完成后的同 plan 重放幂等成功 |
| advisory lock 边界 | 约束 cooperating Codex Pulse writers；不声称能原子阻止同一用户绕过协议直接替换文件 |

## 验证命令

### 1. Focused、重复、race 与 coverage

```bash
CGO_ENABLED=0 go test ./internal/preferences ./internal/onboarding -count=20
CGO_ENABLED=0 go test -race ./internal/preferences ./internal/onboarding -count=1

COVER_FILE="${TMPDIR:-/tmp}/codex-pulse-too258.cover"
CGO_ENABLED=0 go test ./internal/preferences ./internal/onboarding \
  -coverprofile="$COVER_FILE" -count=1
go tool cover -func="$COVER_FILE"
rm -f "$COVER_FILE"
```

成功判据：typed validation/migration/CAS、lock/fault、Settings、两种 switch strategy、调用顺序、concurrency/restart/cancel 与 real metadata-only probe integration 全部通过；race 无报告。

### 2. Focused fault 与状态机复验

```bash
CGO_ENABLED=0 go test ./internal/preferences \
  -run 'Test(FileStore.*(Migration|CompareAndSwap|Replace|UnsafeLock)|Service.*(Switch|Bootstrap|Recover|Concurrent))' \
  -count=20
```

成功判据：迁移失败保留 legacy bytes；CAS 单 winner；replace 前后故障语义稳定；drain/bootstrap/status/resume 错误保留 sentinel 和操作上下文；pending journal 可恢复。

### 3. Pure Go 与本卡 SQLite 边界

```bash
if CGO_ENABLED=0 go list -deps \
  ./internal/preferences ./internal/onboarding ./internal/store | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
if rg -n '\.(Raw|Exec)\(' internal/preferences --glob '*.go' --glob '!**/*_test.go'; then
  exit 1
fi
go mod tidy -diff
```

成功判据：实际 production 编译闭包不含禁用 SQLite driver；Preferences production 路径无 GORM raw SQL；module 无漂移。`go.mod/go.sum` 的间接 metadata checksum 不等于 production dependency。

### 4. 全仓本地门禁

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make verify-architecture
git diff --check
make verify
```

成功判据：全部退出码为 0；全仓命令使用项目默认 CGO，Pure Go 由 focused gate 单独证明；普通 feature 只允许 review 后写 `CHANGELOG.md -> Unreleased`，不得生成 release/tag。Actions 不属于本轮 gate。

## 当前实测证据

| Gate | 当前结果 |
| --- | --- |
| RED/GREEN | PASS：final-scope Rework 中同一 Service 顺序/并发完成重放先稳定返回 plan-not-found，保留最近 plan 后均读回同一 durable audit，runtime 仅一次 drain/start |
| focused repeated | PASS（final-scope Rework）：同一 Service 重放 `count=20`；Preferences + Onboarding `count=20`；关键 lease/migration/CAS/switch/recovery/concurrency `count=50` |
| focused race | PASS（final-scope Rework）：Preferences + Onboarding Pure Go race |
| coverage | PASS（final-scope Rework 最终权威值）：Preferences 76.4%、Onboarding 82.1%，合计 77.4% statements |
| Pure Go / raw SQL / module | PASS（final-scope Rework）：安全无匹配命令退出 0；CGO0 production deps 未命中 official driver/mattn；Preferences production 无 `Raw`/`Exec`；module 无漂移 |
| full repo / harness / project / version / `make verify` | PASS（final-scope Rework）：全仓 test/race/vet、harness、project、`findings=[]`、diff、bindings stability、frontend 与 macOS arm64/minOS 15 ad-hoc app/ZIP；生成物已清理 |
| implementation / final scope review | implementation 第四次 `ZERO_FINDINGS`；final scope 两轮 Rework finding 均关闭；第二次 re-review 与 PASS 回写 delta audit 均 `ZERO_FINDINGS`，`FINAL_SCOPE_PASS=YES`、`FINAL_DELTA_PASS=YES` |
| post-integration verify | PASS：final-scope Rework 后 coverage 77.4%、focused/race/boundary、全仓与完整 package gates 全绿，生成物已清理 |
| PR / merge / post-merge | 待执行 |

## 失败处理与恢复

| 失败点 | 停止条件 | 恢复方式 |
| --- | --- | --- |
| migration/CAS | legacy 被部分覆盖、并发出现两个 winner、revision 跳跃 | 保留 fixture，修复 private publish/CAS 后重跑步骤 1～2 |
| privacy/path | 读取真实 Home 内容、跟随 symlink、宽权限锁仍写入 | 立即停止；只保留 synthetic test 名，收紧 adapter 后完整重跑 |
| switch order | bootstrap 早于 pending 持久化，或 drain 失败仍写新 generation | 修复 coordinator；从 order/fault focused 重跑 |
| recovery | unknown status 清除 journal、not-started 未 resume、started 状态重复 start | 保留 journal，修复 durable status 分支后重跑 restart matrix |
| concurrency | 双 generation winner、两次 bootstrap、Recover 抢占 live owner | 修复 execution lease/CAS/readback/fence；`count=50` + race 后再进入 review |
| local gate | test/race/vet/harness/project/version/verify 任一失败 | 同分支修复并完整重跑；不得用 disabled Actions 替代 |

## 结果回写

- 每轮执行后更新“当前验证结果”和“当前实测证据”，保留已完成的脱敏摘要，不删回空模板。
- 原始长输出只保留在本地 `.artifacts/runs/TOO-258-preferences-home-switch.md`；提交版不记录机器绝对 temp path、原始 JSONL/auth、凭据或 raw filesystem error。
- 已按 `changelog-style` 写入唯一 `[TOO-258]` `Unreleased -> feature` 条目；post-integration 与不同 subagent final scope review 通过后才 commit/PR/merge。
