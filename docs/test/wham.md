# Wham 在线配额客户端 Runbook

## 当前验证结果

- 记录时间：2026-07-15（Asia/Shanghai）
- 记录目录：仓库根目录
- 本轮任务性质：TOO-263 内存凭证、受控 Wham 客户端、typed failure 与原子持久化
- 当前结论：`PASS（已合并并完成 post-merge verify）`；implementation review 与 final scope review findings 均已按 TDD 修复并复审 `ZERO_FINDINGS`；PR #26 已合并为 `d507de6`，main post-merge 全部门禁通过，Linear TOO-263 已读回 Done。
- 自动化入口：`internal/codex/quota/*_test.go`、`internal/store/quota_fetch_test.go`、`internal/store/quota_failure_migration_test.go`
- 对应计划 / issue：`.agents/plans/2026-07-15-too-263-wham-quota-client.md` / TOO-263
- 结果说明：fake HTTP transport 覆盖成功、partial、结构漂移、失败分类、重试、取消、redirect 禁止和凭证释放；临时 Pure-Go SQLite 覆盖 schema v9→v10、migration rollback、原子 replay/conflict/rollback、last-known-good、suspicious-only、乱序结果和零次 HTTP 取消落库。implementation review 的四项 blocking 已修复并复审 `ZERO_FINDINGS`。不同 final reviewer 随后发现 response header 后过早 cancel、混合重试残留 HTTP status、推进时钟下 429 hint 早于 finished 三项 High；均已补 context-aware body/body I/O retry、Service→Repository 混合失败和 `Retry-After: 0` 边界 RED，并完成修复。rework 后 Pure-Go count=20、targeted race count=5、全仓 test/vet/race、tidy、harness/project/version/diff、依赖/raw-SQL guards 与 exact Wails `make verify` 通过，生成物已清理且 module/lock/bindings 无漂移；同一 final reviewer 复审 `ZERO_FINDINGS / READY_TO_COMMIT: YES`。未读取真实 Codex Home/auth，未发真实 Wham 请求。

### 本次执行结果

- 执行时间：2026-07-15
- 执行目录：仓库根目录
- 本次结论：`PASS（含 main post-merge verify）`
- 影响范围：Go build/test cache；Go tests 自动管理的 fake response 和临时 SQLite 数据库。
- 清理结果：`t.TempDir()` 数据库已自动清理；未生成用户数据库、网络抓包或 response fixture 文件。
- 敏感信息处理：只使用 synthetic marker；token 只存在于测试内存，提交版不记录真实 credential、Authorization 值、Cookie、response/error body、真实路径或临时目录。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| Client / Store focused | 通过 | 两包 `count=1`；包含真实 Service→Repository 取消落库。 |
| 全仓 test / vet | 通过 | `go test ./... -count=1`、`go vet ./...`；macOS linker deployment-target warning 不影响 exit 0。 |
| Pure-Go 依赖 | 通过 | `CGO_ENABLED=0` 下 quota/store 通过；依赖图只有 libtnb/modernc SQLite 路径。 |
| go.mod 一致性 | 通过 | `go mod tidy -diff` 无输出。 |
| focused repeat / race | 通过 | 初始功能/迁移 `count=50`、race `count=10`；final rework 后 Pure-Go `count=20`、targeted race `count=5`。 |
| full race | 通过 | `go test -race ./... -count=1`；所有包通过。 |
| harness / project / version / make verify | 通过 | 3项 final rework 后完整复跑；固定 Wails v3.0.0-alpha2.117、macOS arm64/minOS 15 app 与 ZIP 复验通过。 |
| 独立 review / CHANGELOG / final review | 通过 | implementation review 与 final scope review 均复审 `ZERO_FINDINGS`；CHANGELOG 已按 skill 写入一条完成事实；`READY_TO_COMMIT: YES`。 |
| PR / merge / post-merge | 通过 | PR #26 已合并为 `d507de6`；main post-merge 全部门禁通过，Linear TOO-263 已读回 Done。 |
| GitHub Actions | 不执行 | `actions_disabled_by_user`，不查询、不触发、不等待。 |
| release | 不执行 | 普通 Execution 不发布。 |

执行说明：首次直接运行 `make verify` 时，因 closeout 前已清理 `frontend/node_modules`，在 `vue-tsc` 入口以 127 停止；按仓库 README 与 lockfile 执行 `npm ci --prefix frontend`（199 个依赖、audit 0 vulnerabilities）后，从完整 `make verify` 重跑通过。验证结束已清理 `frontend/node_modules`、`frontend/dist` 构建文件、`.task` 与 `bin`，保留 tracked `.gitkeep`；go.mod、go.sum、lockfile 与 generated bindings 无漂移。npm 只输出既有 `glob@10.5.0` deprecation warning，本卡不顺带升级依赖。

## 目标

- 验证目标：证明 Wham 客户端只使用短生命周期内存凭证，严格、有界地把响应归一为 observation 或七类 typed failure，并将 observation、attempt、source state 原子持久化。
- 成功标准：失败不会生成 `used_percent=0` 或覆盖 last-known-good；partial 保留合法窗口；取消、timeout、重试和 429 hint 可判定；请求/响应正文与凭证不进入 durable state。
- 本 runbook 只验证 synthetic contract，不是 live Wham/auth E2E。

## 执行副作用

- 可能写入的本地文件：Go build/test cache；Go test 自动创建并清理的临时 SQLite 文件。
- 可能访问的服务 / 数据库 / 外部系统：无；HTTP 全部由 fake `http.RoundTripper` 截获。
- 可能创建的临时数据：synthetic response 内存、临时 v9/v10 SQLite 数据库和 migration backup fixture。
- 明确不会触达的范围：真实 `~/.codex/auth.json`、真实 access/refresh token、Wham 网络、用户 SQLite、GitHub Actions、release。
- 如果测试被改成真实 endpoint、真实 auth 路径或持久用户数据库，立即停止并重新确认范围。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：待验证的 TOO-263 分支或包含其变更的主分支。
3. 必需命令：项目指定 Go toolchain、`make`、`git`。
4. 必需配置：无；禁止注入真实 credential。
5. 必需测试环境：macOS 用于完整 Wails 门禁；focused quota/store 测试支持 `CGO_ENABLED=0`。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

git status --short --branch
go version
```

预期结果：

- 当前目录为待验证仓库，已有改动已识别并保护。
- 命令不读取或打印 credential、auth 文件、HTTP body 或用户数据库。

## 主路径

### 1. Client 与原子 Store focused matrix

```bash
CGO_ENABLED=0 go test ./internal/codex/quota ./internal/store \
  -run 'Wham|QuotaFetch|SourceFailure|Migration' -count=50
CGO_ENABLED=0 go test -race ./internal/codex/quota ./internal/store \
  -run 'Wham|QuotaFetch|SourceFailure|Migration' -count=10
```

预期结果：

- provider 复制、Replace、Close 与 callback lease 不互相泄漏；请求退出后 Authorization header 已删除。
- primary/secondary、secondary 缺失、partial、missing primary、future plan、past reset、duplicate key、wrong content type、oversized body 全部按固定语义归类。
- 网络、timeout、5xx 最多三次；401/403/429/schema 不做短重试；重试等待取消优先返回 cancelled。
- response+error body 总是关闭；时钟回拨不会生成 finished-before-started。
- Store v10 migration/rollback、exact replay/conflict、transaction rollback、last-known-good、partial 和 late result 均通过。

### 2. Pure-Go 与隐私边界

```bash
CGO_ENABLED=0 go test ./internal/codex/quota ./internal/store/... -count=20

if CGO_ENABLED=0 go list -deps ./internal/codex/quota ./internal/store \
  | rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi

if rg -n '\.(Raw|Exec)\(' internal/codex/quota internal/store \
  --glob '*quota*go' --glob '!**/*_test.go'; then
  exit 1
fi
```

预期结果：

- production SQLite 依赖为 `github.com/libtnb/sqlite` + `modernc.org/sqlite`，不包含禁用 driver。
- quota production scope 不含业务 CRUD raw SQL；schema migration/verifier 的既有例外不在此 glob 中。
- synthetic token/body 不出现在 Result、SQLite、文档结果或失败文本中。

### 3. 全仓回归

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod tidy -diff
```

预期结果：

- 全仓测试、race、vet 与 module diff 通过。
- 全仓不要设置 `CGO_ENABLED=0`：Wails macOS 平台包需要默认 CGO；Pure-Go Store 由 focused 命令独立证明。
- macOS linker deployment-target warning 可记录，但不能掩盖真实失败。

### 4. Harness、版本与完整项目验证

```bash
make harness-verify
make project-check
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --json
git diff --check
make verify
```

预期结果：

- harness/project/version/diff 门禁通过，version findings 为空。
- exact Wails、frontend、bindings stability、macOS app/archive 验证通过。
- 本卡不运行 GitHub Actions，不执行 release。

### 5. 清理

```bash
git status --short --branch
```

预期结果：

- TempDir SQLite 与 fake response 已自动清理。
- 除本卡预期源码、测试、设计、runbook 和 review 后 CHANGELOG 外无新增产物。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| credential lifecycle | token/header 超出 callback/request 生命周期或进入输出 | blocking finding；只记录 synthetic test 名 | 修复 lease/header 清理后从 focused 重跑 |
| schema / false zero | 无效响应生成零值、partial 丢合法窗口、unknown plan 被当真实值 | blocking finding；不得保存 body | 修复 decoder 后重跑 payload matrix |
| retry / cancel | 非允许错误被重试、取消误报网络、attempt 超过 3 | blocking finding | 修复分类/退避后重跑 retry + race |
| atomic persistence | attempt/observation/state 部分提交、replay 冲突未拒绝、late result 回退 state | blocking finding | 修复 writer transaction 后重跑 store fault matrix |
| privacy / dependency | 出现真实 credential/body、禁用 SQLite driver 或业务 raw SQL | blocking finding | 清理证据并修复依赖/adapter 后完整重跑 |
| 清理 | 临时数据库或构建产物残留 | 停止 closeout | 只清理本 runbook 产生的路径后读回状态 |

## 结果回写

- 每轮执行后更新本文顶部“当前验证结果”和步骤状态；未执行步骤不得写成通过。
- 只提交脱敏摘要；原始输出留在 `.agents/runs` 或 Linear comment。
- GitHub Actions 处于 `actions_disabled_by_user`：不查询、不触发、不等待，也不把空检查伪装成 CI 通过。
- 普通 Execution 不发布；TOO-263 只在 implementation review 通过后更新 `CHANGELOG.md -> Unreleased`。
