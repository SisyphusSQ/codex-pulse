# Window Generation、Observation 校验与可信仲裁 Runbook

## 当前验证结果

- 记录时间：2026-07-15（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-264 窗口代际、连续性/时钟校验、Local/Wham 仲裁和可重建 current/evidence
- 当前结论：`PASS（已合并并完成 post-merge verify）`。`RecordQuotaFetch` 已在非 replay writer transaction 内只读取一次 repository 可信 wall clock，future/late attempt 不能再抬高或降低 projection evaluation clock；非法可信时钟会在任何 fetch 事实落盘前失败。implementation 与 final scope 两层 reviewer 均返回 `ZERO_FINDINGS`、`blocking_findings=0`，旧 P1/P2 均 CLOSED；PR #27 已合并为 `1bd7fb5`，main post-merge 门禁通过，Linear TOO-264 已读回 Done。
- 自动化入口：`internal/store/quota_arbiter_test.go`、`internal/store/quota_projection_test.go`、`internal/store/quota_projection_migration_test.go`
- 对应计划 / issue：`.agents/plans/2026-07-15-too-264-window-generation-observation.md` / TOO-264
- 结果说明：此前评审发现的 first-seen false-zero、observation 自抬 evaluation clock、4096 evidence bind overflow、typed readback 完整性与多次 SELECT 混合快照均已修复。两阶段 generation 重分类会选更新 Local last-known-good；Local、Wham fetch、通用 exact quota Wham state、maintenance 和 migration 分别使用其受信应用时钟；evidence 以 256 行分批写入。current/evidence reader 在显式 GORM read transaction 的同一 SQLite snapshot 内加载完整 raw candidates 与 Wham source state，按 stored rule/evaluation 重算并精确对账完整 projection。最新 final-scope P1 已通过 future/late/invalid-clock 回归修复；focused50、race10、Pure-Go Store20、全仓 test/race/vet/tidy、控制面与完整打包门禁均通过。未读取真实 Codex Home/auth，未发真实 Wham 请求。

### 本次执行结果

- 执行时间：2026-07-15
- 执行目录：仓库根目录
- 本次结论：`PASS（含 main post-merge verify）`
- 影响范围：Go build/test cache，以及 `testing.T.TempDir()` 下的 synthetic Pure-Go SQLite/WAL/SHM。
- 清理结果：测试临时数据库由 Go 自动清理；为完整验证按 lockfile 临时安装的 `frontend/node_modules` 及 frontend dist、`.task`、`bin` app/ZIP 已清理，tracked `.gitkeep` 保留；未生成用户数据库、网络抓包或 response fixture。
- 敏感信息处理：只使用 synthetic account/limit/request/source ID；未写入真实 credential、Authorization、Cookie、response body、用户内容、机器本地路径或临时目录。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| pure arbiter matrix | PASS | generation、clock、monotonic、conflict、zero defense、permutation |
| schema v11 / migration | PASS | 可信 migration clock、4096 evidence backfill、failed v11 rollback |
| repository integration | PASS | Wham fetch 改用 writer transaction 内单次 repository trusted clock；future/late/invalid-clock complete rollback 纳入完整重复门禁 |
| `internal/store` full | PASS | post-integration `CGO_ENABLED=0 go test ./internal/store/... -count=20`；Store 152.994s / SQLite 5.075s |
| focused repeat / race | PASS | post-integration focused50 56.811s；race10 161.219s；trusted-clock 专项 count20 通过 |
| full test / race / vet / tidy | PASS | post-integration 全仓 test 15.76s；race Store 65.584s / SQLite 4.827s；vet/tidy 退出 0；仅有既有 macOS deployment linker warning |
| harness / project / version / make verify | PASS | post-integration harness、RUNTIME/TOOLCHAIN/VERIFY/CI、version findings=[]、Wails arm64/minOS 15 ad-hoc app/ZIP；`make verify` 34.72s |
| implementation / final scope review | PASS / PASS | implementation ZERO；final ZERO、blocking=0、P1/P2 CLOSED、CHANGELOG_AUDIT=PASS、FINAL_SCOPE_READY_TO_COMMIT=YES |
| PR / merge / post-merge | PASS | PR #27 已合并为 `1bd7fb5`；targeted20、Pure-Go Store、全仓 race、guards、控制面与完整 `make verify` 通过，Linear TOO-264 已读回 Done |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待 |
| release | 不执行 | 普通 Execution 不发布 |

执行说明：post-integration 再次按 lockfile 执行 `npm ci --prefix frontend`，审计 0 vulnerabilities；完整 `make verify` 一次退出 0。npm 仅输出既有 `glob@10.5.0` deprecation warning，本卡不顺带升级依赖；验证完成后 ignored `node_modules`、dist、`.task`、app/ZIP 与打包目录均已清理，tracked `.gitkeep` 保留。

## 目标

- 证明窗口 identity 与 stable generation 可从不可变 observation 确定重建，旧 generation 晚到不能覆盖新 current。
- 证明同来源同代际 used 回退、时钟/reset/duration 异常和 false-zero 被隔离并保留 reason；合法新代际可以从低值恢复 trusted。
- 证明 Local/Wham 同代际不静默平均，选择最大 used 并保留双方 conflict evidence；freshness 与 conflict 正交。
- 证明 observation、current、evidence 在同一 writer transaction 内原子提交，v10 升级、取消、故障和重启不丢 raw facts 或 last-known-good。
- 本 runbook 只验证 synthetic contract，不是 live Wham/auth E2E。

## 执行副作用

- 可能写入的本地文件：Go build/test cache；完整 `make verify` 可能生成仓库已忽略的 frontend/build/package 产物。
- 可能访问的服务 / 数据库 / 外部系统：无；所有 SQLite 都位于测试临时目录，不连接用户应用库。
- 可能创建的临时数据：synthetic observation、source attempt/state、quota current/evidence、v10/v11 migration 与 backup fixture。
- 明确不会触达的范围：真实 Codex Home、`auth.json`、Wham 网络、用户 SQLite、GitHub Actions、release/tag。
- 如果测试被改为真实 endpoint、真实 auth 路径或默认应用数据库，立即停止并重新确认范围。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：待验证的 TOO-264 分支或包含其变更的主分支。
3. 必需命令：项目指定 Go toolchain、`make`、`git`、`rg`。
4. 必需配置：无；禁止注入真实 credential 或用户数据库路径。
5. 必需测试环境：focused Store 使用 `CGO_ENABLED=0`；完整 Wails gate 使用 macOS 默认 CGO。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

git status --short --branch
go version
```

预期结果：目录和分支正确；命令不读取或打印 credential、auth 文件、HTTP body 或用户数据库。

## 主路径

### 1. pure arbiter、schema 与 repository repeat

```bash
CGO_ENABLED=0 go test ./internal/store \
  -run 'QuotaArbiter|QuotaCurrent|QuotaProjection|ApplicationSchemaV11|ApplicationMigration.*V11' \
  -count=50
```

预期结果：

- 同 generation 单调、cross-source max/conflict、新 generation、zero defense、future/regressed clock、旧 generation 和输入 permutation 全部稳定。
- v11 checksum 冻结；v10→v11 在同一 migration transaction 内 backfill，失败完整留在 v10。
- 4096 条 migration/rebuild 与第 4097 条普通写入不受 SQLite bind-variable 或公开 list limit 影响；partial 不删除 sibling；fault/cancel/restart 保留旧 projection/raw facts。

### 2. focused race 与 Pure-Go 依赖

```bash
CGO_ENABLED=0 go test -race ./internal/store \
  -run 'QuotaArbiter|QuotaCurrent|QuotaProjection|ApplicationSchemaV11|ApplicationMigration.*V11' \
  -count=10
CGO_ENABLED=0 go test ./internal/store/... -count=20

if CGO_ENABLED=0 go list -deps ./internal/store \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
```

预期结果：race/repeat 均退出 0；实际编译依赖只有 `github.com/libtnb/sqlite` + `modernc.org/sqlite` Pure-Go 路径，不包含禁用 driver。

### 3. GORM-first 与提交版边界

```bash
if rg -n '\.(Raw|Exec)\(' internal/store \
  --glob 'quota_arbiter.go' --glob 'quota_projection.go' \
  --glob 'quota_projection_models.go'; then
  exit 1
fi

if rg -n 'Authorization:|Bearer [A-Za-z0-9+/=_-]{12,}|http://127\.0\.0\.1:[0-9]+' \
  docs/test/window-generation-observation.md \
  docs/design/details/quota/README.md \
  docs/design/details/data-model/README.md | rg -v ':(if )?rg -n '; then
  exit 1
fi
```

预期结果：业务 projection 代码没有 raw CRUD/query；敏感扫描仅允许命中规则说明本身，不得出现真实值、完整 URL 或机器痕迹。`quota_projection_schema.go` 的 `STRICT/CHECK/复合 FK/index` DDL 是显式例外。

### 4. 全仓与控制面门禁

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
git diff --check
make harness-verify
PATH="/tmp/codex-pulse-tools/bin:$PATH" make project-check
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
PATH="/tmp/codex-pulse-tools/bin:$PATH" make verify
```

预期结果：所有命令退出 0，version findings 为空，fixed Wails/macOS app/archive 门禁通过；不创建 release，不查询或触发 GitHub Actions。

### 5. 清理

```bash
git status --short --branch
```

预期结果：TempDir SQLite 已自动清理；除本卡源码、测试、设计、runbook 和 review 后 CHANGELOG 外无新增 tracked/ignored 构建产物。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| generation / zero defense | 旧代际覆盖新 current、同 reset 下降或伪零成为 trusted | 只记录 synthetic test 名与固定 reason | 修复 pure arbiter 后从步骤 1 重跑 |
| clock / freshness | overflow/future clock 被接纳、fresh 越过 fresh-until/reset 不降级 | 记录边界时间整数与状态，不记录原 payload | 修复 rule/read degradation 后重跑步骤 1、2 |
| conflict / partial | 静默平均、固定来源覆盖、partial 删除 sibling | 记录 synthetic source/used 与 disposition | 修复分组/affected-key 后重跑步骤 1 |
| atomic persistence | observation/current/evidence 部分提交，migration/fault/cancel 丢 LKG | 记录版本、stage 和 sentinel | 修复 transaction/readback 后重跑步骤 1、2 |
| evidence scale / typed readback | 4096 history 因 bind 上限回滚，或 current/evidence 与 raw observation 不一致仍可读 | 记录 synthetic 行数与固定错误类型 | 固定安全 batch，修复同事务语义核验后重跑步骤 1、2 |
| privacy / dependency | 出现真实 credential/body、禁用 SQLite driver 或业务 raw SQL | blocking finding，只记录命中文件 | 清理并修复 adapter 后完整重跑 |
| full gate | test/race/vet/harness/Wails 非零 | 记录首个失败 gate 的脱敏摘要 | 修复后从 affected gate 开始，最终完整重跑步骤 4 |

## 结果回写

- 每轮执行后更新本文顶部真实结论和步骤状态；未执行步骤不得写成通过。
- 长输出和本机路径只留在 `.agents/runs`，提交版只保留脱敏摘要。
- GitHub Actions 保持 `actions_disabled_by_user`：不查询、不触发、不等待，也不把未运行写成 CI 通过。
- 普通 Execution 不发布；本卡只在 review 通过后更新 `CHANGELOG.md -> Unreleased`。
