# 暂停续传、休眠唤醒与故障恢复验证

## 当前验证结果

- 记录时间：2026-07-15
- 记录目录：Codex Pulse 仓库根目录
- 本轮任务性质：TOO-261 实现验证
- 当前结论：`通过：最终 scope review 的 2 个 P1 已关闭，第六轮 implementation review、CHANGELOG 集成门禁与不同 subagent 第二轮 final scope review 均通过，pre_commit_ready=YES`
- 自动化入口：Go package tests、race、repository harness
- 对应计划 / issue：TOO-261 / `.agents/plans/2026-07-15-too-261-pause-sleep-recovery.md`
- 结果说明：schema v8 lifecycle/retry、generation fence、暂停 drain、wake/source reconcile、持久退避、Wails event adapter 与真实 app runtime 装配已通过 focused/count/race 与全仓回归；第四轮补齐 Interrupt->Resume 时间余量、active/counter executor 前后双层边界、确定性 integration，并关闭内建 executor active 记账越界和 cycle commit 取消竞态；GitHub Actions 按 Root Goal 明确停用。

### 本次执行结果

- 执行时间：2026-07-15
- 执行目录：仓库根目录
- 本次结论：`阶段通过`
- 影响范围：`internal/store`、`internal/retry`、`internal/scheduler`、`internal/lifecycle`、`internal/app`、`internal/bootstrap`、`internal/liveindex`
- 清理结果：测试数据库与 confirmed Home fixture 均位于 `t.TempDir()` 并由 Go test 清理；没有创建常驻 worker 或外部资源。
- 敏感信息处理：未写入真实凭据、token、cookie、JSONL 原文、Home 绝对路径、临时目录或原始错误正文；测试只使用合成事实。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| schema v8 / repository | 通过 | GORM + Pure-Go SQLite；migration、checksum、CAS、exact replay、atomic failure/retry |
| pause / sleep / wake | 通过 | intent-first、lane-aware drain、generation fence、user pause 跨 wake 保留 |
| retry / recovery | 通过 | stable target retry、持久 due time、永久错误与耗尽 typed action |
| logical time boundary | 通过 | queued/running/terminal 分层余量；最大 slice 可先持久化再在下一 claim 前安全 terminal；不可重排 due 转 blocked |
| Wails event adapter | 通过 | sleep/wake/前台 source check callback 仅排队；后台 FIFO 串行调用 coordinator |
| app runtime 装配 | 通过 | configured Home 启动真实 bootstrap/live/scheduler/lifecycle；临时 JSONL 经 reconcile 入队并处理成功；物理 Home 失效时先 blocked 且不恢复 target；关闭释放 owner lease |
| 全量 test / race / harness | 通过 | rework 后重新执行全仓 test/race/vet、tidy、harness/project/version、`make verify` |
| 核心重复验证 | 通过 | 7 个 Pure-Go 核心包 `count=20`；默认 8 包（含 app）`count=20`；8 包 focused race |
| coverage | 通过 | store 78.1%、retry 92.6%、scheduler 79.1%、lifecycle 72.7%、app 69.2%、bootstrap 79.6%、liveindex 71.5% |
| Pure-Go / GORM 边界 | 通过 | CGO SQLite driver 与本卡业务 raw SQL 均无编译/源码匹配 |
| macOS package | 通过 | arm64、minOS 15、ad-hoc app/zip 均读回通过；仅有既有 linker warning |
| 独立 review / final scope review | 通过 | implementation reviewer 确认 ZERO/READY_FOR_CHANGELOG；不同 subagent 第二轮 final scope 确认 FINDINGS=0、FINAL_DELTA_PASS=YES、FINAL_FREEZE_PASS=YES、pre_commit_ready=YES |
| post-merge | 待执行 | main 权威 commit 验证后回写 |

## 目标

- 验证用户暂停、系统 sleep/wake、来源变化与启动恢复保持正交、持久且可精确重放。
- 验证 scheduler 只 claim active Home generation 且 lifecycle permit 允许的任务，暂停 intent 写入后不会接纳新 slice。
- 验证 transient failure 使用有界、可取消且持久化 due time 的指数退避，永久错误和耗尽状态给出 typed recovery action。
- 验证 retry/recovery 复用同一 scheduler task 与 stable target identity，failed cycle 和 retry state 原子提交。
- 验证 SQLite 路径保持 GORM-first + Pure Go，不引入 CGO SQLite 编译依赖。

## 执行副作用

- Go test 会写入本机 Go build/test cache。
- 测试在私有临时目录创建合成 confirmed Home 与 SQLite/WAL 文件，结束后自动清理。
- `make harness-verify` 与 `make project-check` 只读取仓库并可能写入忽略的本地构建缓存。
- 不访问真实 Codex Home、Linear、GitHub Actions 或其它外部系统；不执行 release。

## 前置条件

1. 当前工作目录为仓库根目录。
2. Go module 依赖已下载。
3. Pure-Go 核心路径显式使用 `CGO_ENABLED=0`；Wails app package 在 macOS 使用默认 CGO 构建约束验证。
4. GitHub Actions 保持停用，所有 gate 由本地命令提供证据。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$PWD}"
cd "$REPO_ROOT"
git status --short --branch
go version
```

预期结果：位于 TOO-261 专用分支；命令成功，输出不包含敏感值。

## 主路径

### 1. Pure-Go schema、retry 与 scheduler 核心路径

```bash
CGO_ENABLED=0 go test \
  ./internal/runtimeclock ./internal/store ./internal/retry ./internal/scheduler ./internal/lifecycle \
  ./internal/bootstrap ./internal/liveindex -count=20

if CGO_ENABLED=0 go list -deps \
  ./internal/runtimeclock ./internal/store ./internal/retry ./internal/scheduler ./internal/lifecycle \
  ./internal/bootstrap ./internal/liveindex \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
```

预期结果：测试重复通过；依赖扫描无匹配并以 `rg` 的无匹配状态退出，不包含两个 CGO SQLite driver。

### 2. Wails event adapter 与真实 app runtime 装配

```bash
go test ./internal/app -run 'TestLifecycleEventAdapter|TestApplicationLifecycleRuntime' -count=20
```

预期结果：sleep/wake/source-check FIFO、callback 无 IO、close/unregister 与错误通道测试重复通过；configured 临时 Home 的真实 runtime 完成 startup reconcile、live task 与 owner lease 释放；允许出现项目既有的 macOS linker warning，但命令必须成功退出。

### 3. race、全量回归与仓库 gate

```bash
go test -race \
  ./internal/runtimeclock ./internal/store ./internal/retry ./internal/scheduler ./internal/lifecycle \
  ./internal/app ./internal/bootstrap ./internal/liveindex -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
make harness-verify
make project-check
```

预期结果：全部成功退出；`go mod tidy -diff` 无输出。Wails linker 只允许既有 deployment target warning，不得出现新的构建失败。

### 4. GORM / raw SQL 边界审计

```bash
if rg -n '\.(Raw|Exec)\(' \
  internal/lifecycle internal/retry internal/scheduler \
  internal/store/lifecycle_*.go internal/app/lifecycle_*.go; then
  exit 1
fi
```

预期结果：无匹配。schema v8 的 canonical `STRICT` DDL 只允许集中在 migration adapter，并由 migration/checksum 测试覆盖。

### 5. 清理

```bash
git status --short
```

预期结果：没有测试生成的未跟踪数据库、WAL、日志或凭据文件；只保留本 Issue 的受控代码、文档与本地忽略的恢复记录。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| migration/checksum | 任一历史 checksum 或 v8 catalog 不符 | 记录测试名与有限错误 | 只追加/修正当前 migration，重跑 store |
| lifecycle fence | pause 后仍 claim、wake 清除 user pause、generation 漂移后开放 worker | 记录 typed lifecycle readback | 新增 RED 后修复并重复 20 次 |
| retry atomicity | failed cycle 无 retry state、重复创建逻辑 task、无限重试 | 记录 task/retry typed facts | 修复 transaction/rebind 后重跑 scheduler |
| Wails callback | callback 内发生 Store/IO、事件乱序或 close 泄漏 | 记录事件序列 | 修复 adapter 后重复运行 app tests |
| race | 任一 data race | 停止 review | 修复并重跑全部 race package |
| full gate | 非既有 Wails warning 的失败 | 停止 closeout | 按失败 package focused 修复 |

## 结果回写

完整本地 gate、implementation review、final scope review 与 post-merge verify 完成后更新本文顶部状态；提交版只保留脱敏摘要，不写真实路径、原始错误或命令全文输出。
