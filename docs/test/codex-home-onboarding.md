# Codex Home 探测与隐私 Onboarding Runbook

> 当前主分支的普通首次启动会把 `${CODEX_HOME:-$HOME/.codex}` 作为受信默认候选，
> 在本 runbook 覆盖的 metadata-only probe、二次 physical identity 校验和原子
> `Confirm` 全部成功后自动完成配置，不要求用户点击确认。本文仍验证底层
> onboarding 状态机；用户选择其他 Home、Home 切换及破坏性重建继续要求显式确认。

## 当前验证结果

- 记录时间：2026-07-14（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-257 metadata-only Home probe / private preferences / onboarding state machine
- 当前结论：implementation review 与不同 subagent final scope review 均 `ZERO_FINDINGS`；CHANGELOG verdict `PASS`，`READY_TO_COMMIT=true`，包含 CHANGELOG 的 post-integration full verify 已通过。
- 自动化入口：`internal/codex/logs/home_probe_test.go`、`internal/preferences/file_store_test.go`、`internal/onboarding/service_test.go`
- 对应 issue：TOO-257
- 结果说明：确认前只枚举 allowlisted 目录和 stat metadata；mode `000` synthetic JSONL/auth fixture 仍可完成结构探测且 size/mode/mtime 不变。confirmation ID 不绑定动态 count/bytes，Confirm 重探测 physical identity；私有 snapshot 以 `0700/0600` 原子发布，支持幂等、并发首次确认、冲突保护和 durability-unknown 读回。未读取真实 Codex Home，未启动 indexing、SQLite、网络、Actions 或 release。

### 本次执行结果

- 执行时间：2026-07-14
- 执行目录：仓库根目录
- 本次结论：`本地完整验证通过`
- 影响范围：Go/Node/Wails build/test cache，Go tests 自动管理的临时 Home、symlink、named pipe 和 preferences fixture，以及临时 frontend dependencies、bindings 生成和 macOS app/ZIP 打包产物。
- 清理结果：`t.TempDir()` fixture 自动清理；临时 Wails CLI、frontend dependencies、frontend dist、macOS app/ZIP 和 packaging 目录已删除，tracked `frontend/dist/.gitkeep` 保留；bindings、Go module 与 lockfile 无漂移。
- 敏感信息处理：只使用 synthetic marker；提交版不记录真实路径、原始 JSONL/auth 内容、raw filesystem error、token、cookie、临时目录或完整命令输出。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| 前置检查 | 通过 | macOS arm64；新增包在 `CGO_ENABLED=0` 下验证。 |
| HomeProbe focused | 通过 | metadata-only、pre-observed root identity、逐组件 no-follow、真实目录/symlink replacement、entry 增删/替换、取消和 overflow，`count=20` + race。 |
| Preferences focused | 通过 | strict version/JSON、`0700/0600`、containing/private directory fsync、原子发布、并发/幂等/冲突、durability-unknown，`count=20` + race。 |
| Onboarding focused | 通过 | 输入 clean、候选顺序/去重、Confirm/Cancel 线性化、post-commit readback 四类结果、unknown→Resume not configured 清 latch、failure masking、source replacement、真实 adapter 集成，`count=20` + race。 |
| 全仓 test/race/vet | 通过 | 使用项目默认 CGO；Wails linker deployment-target warning 不影响 exit 0。 |
| harness/project/version/diff | 通过 | harness/project contract tests 通过，version findings 为空，diff 无 whitespace error。 |
| Pure Go GORM / SQL guard | 通过 | Store 在 `CGO_ENABLED=0` 下通过；production deps 无禁用 SQLite driver，本卡 scope 无 `Raw/Exec`。 |
| 完整 `make verify` | 通过 | Wails `v3.0.0-alpha2.117`、frontend、bindings stability、macOS arm64/minOS 15 app 与 ZIP 复验通过。 |
| 独立 review | 通过 | implementation review ZERO_FINDINGS；不同 subagent final scope review ZERO_FINDINGS、CHANGELOG PASS、READY_TO_COMMIT=true。 |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待。 |
| release | 不执行 | 普通 Execution 只更新 `CHANGELOG.md -> Unreleased`。 |

## 目标

- 验证目标：证明用户确认前不读取 Codex JSONL/auth 内容，只有精确确认的 physical Home identity 才能原子持久化 onboarding 与在线能力偏好。
- 成功标准：候选排序和 allowlisted failure 稳定；live append 不使确认失效；root replacement/unsafe structure 零写；cancel 零写；重启能恢复且 source replacement 不授予 indexing；私有 snapshot 权限和故障语义明确。
- 本 runbook 可由 agent 或工程师直接执行，不是泛化 QA 说明。

## 执行副作用

- 可能写入的本地文件：Go build/test cache；Go test 自动创建并清理的 synthetic fixture。
- 可能访问的服务 / 数据库 / 外部系统：无。
- 可能创建的临时数据：synthetic Codex Home、`sessions/`、`archived_sessions/`、root index/auth、symlink、named pipe、private preferences。
- 明确不会触达的范围：真实 `~/.codex` / `CODEX_HOME`、真实 Application Support、SQLite 用户数据库、网络、GitHub Actions、release、用户原始 JSONL/auth。
- 执行前必须说明上述副作用；如果测试参数被改为真实用户目录，立即停止。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：待验证的 TOO-257 分支或包含其变更的主分支。
3. 必需命令：项目指定 Go toolchain、`make`、`git`。
4. 必需配置：无；测试自行注入 synthetic `CODEX_HOME` 和 tracker DB display path。
5. 必需测试环境：macOS；HomeProbe 的 no-follow adapter 使用 Darwin `openat/fstatat`。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

git status --short --branch
go version
```

预期结果：

- 当前目录解析为待验证仓库，已有改动已识别并受到保护。
- 命令不打印或读取凭据和真实 Codex 内容。

## 主路径

### 1. Metadata-only HomeProbe

```bash
CGO_ENABLED=0 go test ./internal/codex/logs \
  -run 'TestHomeProbe|TestAddJSONLMetadata|TestOpenAbsoluteDirectoryNoFollow' -count=20
CGO_ENABLED=0 go test -race ./internal/codex/logs \
  -run 'TestHomeProbe|TestAddJSONLMetadata' -count=1
```

预期结果：

- 只统计 allowlisted JSONL 和 root index/auth 存在性；其它 root JSONL 不进入结果。
- mode `000` 内容文件不被打开，size/mode/mtime 保持不变。
- ancestor/final/nested symlink、真实祖先目录 rename、named pipe、目录伪装 `*.jsonl`、目录/entry replacement、entry 增删、顶层结构错误和 overflow fail closed。
- 空 Home 合法；取消返回 `context.Canceled` 和零 metadata。

### 2. Private Preferences 与 Onboarding 状态机

```bash
CGO_ENABLED=0 go test ./internal/preferences ./internal/onboarding -count=20
CGO_ENABLED=0 go test -race ./internal/preferences ./internal/onboarding -count=1
```

预期结果：

- snapshot 严格校验版本、未知/trailing JSON、绝对 clean Home identity 和私有权限。
- 新建 private 目录先 fsync containing directory；同目录文件 fsync 后不覆盖发布并 fsync private 目录；相同确认幂等，并发首次确认不残留 partial，不同确认拒绝覆盖。
- env/default/selected 输入先 clean、顺序稳定并按 canonical path 去重；只暴露 allowlisted failure reason。
- confirmation ID 不含 JSONL count/bytes；Confirm 重探测 path/device/inode，允许普通 append，拒绝 identity drift。
- privacy notice 明确 tracker SQLite、本地只读、不保存内容、在线 token 仅驻内存且两个开关独立。
- 提交前 Cancel 零写；Confirm/Cancel 以提交点线性化，late cancellation 使用不继承原取消信号的有界读回，并直接覆盖 matches/not-configured/conflict/unavailable 四类结果；无法证明零写时返回 durability-unknown，后续 Resume 明确未配置会清除 latch 并恢复 Detect/Cancel；source replacement 不返回 confirmed authorization。

### 3. 全仓回归

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
CGO_ENABLED=0 go test ./internal/store/... -count=1
```

预期结果：

- 全仓 test/race/vet 通过。
- 全仓不要设置 `CGO_ENABLED=0`：Wails macOS package 需要项目默认 CGO；Pure Go 要求由 focused 命令独立证明。
- `go list -deps ./...` 的生产构建闭包不得包含 `gorm.io/driver/sqlite` 或 `github.com/mattn/go-sqlite3`；`go.mod/go.sum` 可能保留 `gorm.io/gorm` 自身测试依赖记录，不能据此误判为运行时依赖。
- macOS linker deployment-target warning 可记录，但不能把真实失败降级为 warning。

### 4. Harness、版本与完整项目验证

```bash
make verify-architecture
git diff --check
make verify
```

预期结果：

- harness/project/version/diff 通过，version findings 为空。
- exact Wails、frontend、bindings stability、macOS arm64/minOS 15 app/archive 验证通过。
- 本卡不执行 release；生成产物按项目 closeout 规则清理。

### 5. 清理

```bash
git status --short --branch
```

预期结果：

- TempDir/symlink/named pipe/preferences fixture 已自动清理。
- 除本卡预期源码、测试、设计文档、runbook 和 CHANGELOG 外没有残留产物。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| pre-confirm privacy | JSONL/auth 内容被打开、读取或写入 | blocking finding；只记录 synthetic test 名 | 收紧 metadata adapter 后从 HomeProbe focused 重跑 |
| confirmation drift | count/bytes append 错误失效，或 root identity 改变后仍写配置 | blocking finding | 修复 token/重探测边界后重跑 onboarding focused |
| preferences atomicity | 宽权限、symlink 跟随、冲突覆盖、partial 残留或 durability 状态误报 | blocking finding | 修复 private publisher/Resume 后重跑 preferences + onboarding |
| raw error/content leak | State/error/runbook 出现 raw filesystem detail 或内容 marker | blocking finding；不得保留原文证据 | 改为 domain sentinel/allowlisted reason 后完整重跑 |
| race / full test | 任一真实失败 | 记录失败命令与脱敏摘要 | 同一分支修复后完整重跑 |
| 清理 | fixture 或构建产物残留 | 停止 closeout | 只清理确认由本 runbook 产生的路径，再读回状态 |

## 结果回写

- 每轮完成后更新本文顶部结果与步骤状态；未执行门禁保持“待执行”。
- 提交版只保留脱敏摘要；原始命令输出留在本地 run 记录或 Linear comment。
- GitHub Actions 为 `actions_disabled_by_user`：不查询、不触发、不等待，也不把空检查写成 CI 通过。
- 普通 Execution 不发布；TOO-257 只在 review 通过后更新唯一 `CHANGELOG.md -> Unreleased` 条目。
