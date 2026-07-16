# Scheduler Cron 定时唤醒验证

## 当前验证结果

- 记录时间：2026-07-15
- 执行目录：Codex Pulse 仓库根目录
- 对应 Issue：TOO-305
- 当前结论：`PASS（已合并并完成 post-merge verify）`；implementation 与 final scope review 均 `ZERO_FINDINGS`；PR #24 已合并为 `5adf42d`，main 完整门禁通过，Linear TOO-305 已读回 Done。
- 依赖版本：`github.com/robfig/cron/v3 v3.0.1`
- 分支：`suqing/too-305-robfig-cron-scheduler`
- 基线：`d6234b13b3f102ed5b6cf55e93468bbe51d898e7`

### 本次执行结果

| 验证面 | 结果 | 证据 |
| --- | --- | --- |
| TDD RED | 通过 | 新测试先因仓库未提供 `github.com/robfig/cron/v3` 直接依赖而编译失败 |
| SDK / schedule | 通过 | `go.mod` 直接依赖精确 v3.0.1；默认 entry 的下一次执行间隔为 1 秒 |
| overlap / panic token | 通过 | SDK `SkipIfStillRunning` wrapper 在首个 job 阻塞时立即跳过重叠 job，最大并发为 1；首个 job panic 后第二次仍能执行 |
| startup / trigger | 通过 | startup recovery 后立即 cycle；空队列启动后 fake cron trigger 可推进新 durable task |
| cancel / Stop | 通过 | cancel 先停止新 trigger，并等待在途 cron job 完成后返回 `context.Canceled` |
| retry due | 通过 | 首个 cycle 持久化 waiting retry；时钟推进到 `next_retry_at_ms` 后，cron trigger复用稳定 task并提交 succeeded cycle |
| fatal / panic fence | 通过 | system probe fatal 返回原始 typed error；cron job panic 只返回 `ErrSchedulerCronPanic`；首错先取消 job context，Stop 阶段排队 trigger不越过 fence |
| factory failure | 通过 | cron registration 失败发生在 recovery/target side effect 前，queued task 保持不变 |
| focused stability | 通过 | cron focused `count=50`；focused race `count=20` |
| scheduler regression | 通过 | `internal/scheduler count=20` 与 race 通过 |
| scheduler + app | 通过 | 两包 `count=20` 与 race 通过；app worker 关闭继续释放 owner lease |
| Pure-Go 核心 | 通过 | runtimeclock/retry/store/scheduler/lifecycle/bootstrap/liveindex `CGO_ENABLED=0 count=20` |
| 源码边界 | 通过 | 生产 scheduler 无 `time.NewTimer/NewTicker/Sleep/After`，无自写 cron parser；Pure-Go driver 与业务 raw SQL scan 无命中 |
| 全仓 Go 门禁 | 通过 | `go test ./...`、`go test -race ./...`、`go vet ./...`、`go mod tidy -diff`、`git diff --check` 通过 |
| package 门禁 | 通过 | harness/project/version、前端 typecheck/test/build、bindings 稳定性、macOS arm64 app/zip minOS 15 与 ad-hoc 签名读回通过 |
| implementation review | 通过 | Dewey 完整审查 working tree 与独立定向复验，`blocking_findings: 0`、`READY_FOR_CHANGELOG: YES` |
| final scope review | 通过 | Schrodinger 覆盖最终 tracked/untracked delta，`blocking_findings: 0`、`READY_TO_COMMIT: YES` |
| PR / merge / post-merge | 通过 | PR #24 已合并为 `5adf42d`；main full test/race/vet/tidy/source/harness/project/version/完整 `make verify` 通过，Linear TOO-305 已读回 Done |

## 验证目标

- 验证 robfig/cron v3.0.1 是生产 scheduler 的唯一周期 trigger，未保留自写 timer/ticker/sleep loop。
- 验证 cron 只唤醒：queue、claim、generation、lifecycle、retry due、ScanBudget、checkpoint 与 cycle transaction 继续以 typed Store 为权威。
- 验证启动立即 recovery/cycle，重叠 trigger 不并发执行 target，取消/fatal 可停止并 drain cron。
- 验证进程重启不依赖 cron Entry 内存，从 durable task/retry/lifecycle 恢复。
- 验证 TOO-260/TOO-261 的 owner、fairness、stable target、crash gap、bounded commit 和 retry due 语义无回归。

## 执行副作用

- `go test` 会写 Go build/test cache，并在 `t.TempDir()` 创建 Pure-Go SQLite/WAL 与合成 confirmed Home，测试结束自动清理。
- `go mod tidy` 只维护模块文件；`make verify` 会生成 bindings、`frontend/dist`、`bin/Codex Pulse.app` 与 zip，验证后清理可再生产物。
- 不访问真实 Codex Home、凭据、Linear 以外的外部业务系统或 GitHub Actions；不执行 release。

## 前置条件

1. 当前目录为仓库根目录，分支为 TOO-305 专用分支。
2. Go module cache 可读取 `github.com/robfig/cron/v3 v3.0.1`。
3. Wails package 验证时 `/tmp/codex-pulse-tools/bin/wails3` 可用。
4. GitHub Actions 保持用户明确停用状态。

## Focused TDD 与稳定性

```bash
go test ./internal/scheduler \
  -run 'TestDefaultCronRunner|TestServiceRun.*Cron' -count=50

go test -race ./internal/scheduler \
  -run 'TestDefaultCronRunner|TestServiceRun.*Cron' -count=20

go test ./internal/scheduler ./internal/app -count=20
go test -race ./internal/scheduler ./internal/app -count=1
```

预期结果：全部通过；overlap 最大并发为 1，Stop 等待在途 job，fatal 与 panic 使用稳定错误语义。

## Pure-Go 与源码边界

```bash
test "$(go list -m -f '{{.Version}}' github.com/robfig/cron/v3)" = "v3.0.1"

if rg -n 'time\.(NewTicker|NewTimer|Sleep|After)\(' \
  internal/scheduler --glob '*.go' --glob '!**/*_test.go'; then
  exit 1
fi

if rg -n 'cron\.(NewParser|ParseStandard)' \
  internal/scheduler --glob '*.go' --glob '!**/*_test.go'; then
  exit 1
fi

CGO_ENABLED=0 go test \
  ./internal/runtimeclock ./internal/retry ./internal/store ./internal/scheduler \
  ./internal/lifecycle ./internal/bootstrap ./internal/liveindex -count=20

if CGO_ENABLED=0 go list -deps ./internal/scheduler ./internal/store \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi

if rg -n '\.(Raw|Exec)\(' internal/scheduler internal/store/lifecycle_*.go \
  --glob '*.go' --glob '!**/*_test.go'; then
  exit 1
fi
```

预期结果：依赖版本精确匹配；三个源码/依赖扫描无输出；Pure-Go 核心包通过。

## 全仓与 package 门禁

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
make harness-verify
PATH="/tmp/codex-pulse-tools/bin:$PATH" make project-check
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
git diff --check
PATH="/tmp/codex-pulse-tools/bin:$PATH" make verify
```

预期结果：全部通过；版本日志检查无 finding；bindings 与 module 稳定；macOS app/zip 可读回验证。

## 清理与读回

```bash
rm -rf bin frontend/dist/assets frontend/dist/index.html
git status --short --branch
git diff --check
```

预期结果：仅保留 TOO-305 的实现、测试、设计、runbook 与 review 后 CHANGELOG；不包含构建产物。

## 当前残余风险

- robfig/cron v3.0.1 的 interval 最低为 1 秒，旧 250 ms idle/yield 等待统一变为下一秒 tick；这是显式 cadence 取舍，不以第二套 timer 补偿。
- 已知 macOS 本机构建可能输出 deployment-target linker warning；只要 app/zip 架构、minOS 与签名读回通过，不视为本卡新增失败。
- GitHub Actions 按用户要求停用，本卡只采用本地 gate 与 PR/main readback。
