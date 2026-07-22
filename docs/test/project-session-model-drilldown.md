# Project Session/Model Drill-down Test Runbook

## 当前验证结果

- 记录时间：2026-07-17
- 记录目录：仓库根目录
- 本轮任务性质：Project provider 查询契约扩展
- 当前结论：`通过`
- 自动化入口：Go focused/full tests、frontend contract tests、harness/project/version gates、`make verify`
- 对应 issue：TOO-308
- 结果说明：独立 review 返工后，Store/Query/app/frontend focused 与全量回归、race、CGO-off、GORM/privacy/dependency 扫描、harness/project/version、generated stability 和 macOS arm64 package gate 已全部重跑并通过。GitHub Actions 依用户要求停用，本轮未查询、触发或等待 CI。

### 本次执行结果

- 执行时间：2026-07-17 12:36～12:41 CST
- 执行目录：仓库根目录
- 本次结论：`通过`
- 影响范围：测试只读查询、测试临时 Pure Go SQLite、生成绑定与本地构建产物
- 清理结果：已删除可重建 `.task`、`bin`、app bundle/ZIP 与 `frontend/dist` 生成内容，保留 tracked `frontend/dist/.gitkeep`；工作区只剩本卡预期源码、测试、生成绑定和文档差异。
- 敏感信息处理：未写入真实凭据、token、cookie、数据库主机、连接串、行主键、临时目录、完整下载 URL、原始响应或其它机器本地痕迹。

### 实际验证证据

- review 返工后的 `go test ./... -count=1`、`go vet ./...`、`go mod tidy -diff` 通过。
- `CGO_ENABLED=0 go test ./internal/store ./internal/query/usagecost -count=1` 通过；`go list -deps` 的实际 CGO-off 编译链不包含 `gorm.io/driver/sqlite` 或 `github.com/mattn/go-sqlite3`。
- review 返工后的 `go test -race ./... -count=1` 通过；macOS linker 仍输出已知 object min-version warning，未影响退出码或 package 的 minOS=15 读回。
- frontend 25 个 test files / 70 个 tests、typecheck 与 production build 通过；只有已知 ECharts chunk 大于 500 kB 的非阻断 warning。
- GORM 扫描确认 `internal/store/analytics_query_project.go` 无 `.Raw`/`.Exec`；Project DTO/mapping 无 path/remote/source-generation/offset 禁止字段。
- `make verify-architecture`、`make verify-architecture`、project-version-release check 和 `make verify` 通过。Wails 生成稳定读回为 319 packages / 1 service / 15 methods / 34 enums / 68 models / 1 event。
- macOS package/ZIP 读回为 thin arm64、minOS 15.0.0、ad-hoc 签名有效；这不表示 Developer ID、notarization 或正式发布通过。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| 前置检查 | 通过 | 分支/差异/GORM/privacy/dependency 边界符合范围 |
| Store focused tests | 通过 | exact SessionCount、31→30 trend suffix、有界单行双分组对账、scoped attribution completeness、equal/NULL/unknown 双 keyset 分页、generation rollover、restart、drift/cancel 覆盖通过 |
| Query/app/frontend contract tests | 通过 | 双 page DTO、AEAD tamper/replay、active-generation binding、首屏精确 cardinality、shape/order/privacy、generated contract 通过 |
| 全量与 race | 通过 | Go full/vet/tidy/race、frontend full/typecheck/build、CGO-off 通过 |
| harness / build | 通过 | harness/project/version/generated/package/ZIP 通过，Actions 未执行 |
| 清理 | 通过 | 可重建产物已清理，tracked `.gitkeep` 已保留 |

## 目标

- 验证目标：证明 Project list/detail 的 SessionCount、30 日趋势、Project contribution Session page、Project×Model page、AEAD cursor 和 strict reconciliation 在 Pure Go SQLite/GORM 路径下正确、稳定且不泄露本机事实。
- 成功标准：focused/full/race/CGO-off/frontend/harness/build全部通过；两类分页无重复/跳项且拒绝篡改/replay；known-empty与unavailable可区分；生成绑定仍为1 service / 15 methods / 1 event；文档只保留脱敏结果。
- 本 runbook 是给 agent 或工程师直接执行的步骤文档，不是泛化 QA 说明。

## 执行副作用

- 可能写入的本地文件：Go/npm缓存；`frontend/node_modules`；生成的 frontend binding、`frontend/dist`；`.task`、`bin`和本地构建包；测试临时SQLite/WAL/SHM。
- 可能访问的服务 / 数据库 / 外部系统：只访问测试进程创建的Pure Go SQLite；依赖未缓存时包管理器可能访问公开依赖源。
- 可能创建的临时数据：`go test` 的 `t.TempDir`数据库、前端测试缓存、应用构建/签名临时产物。
- 明确不会触达的范围：真实Codex Home/JSONL、真实用户数据库、凭据/token/cookie、GitHub Actions、外部写API、正式release/version。
- 执行前必须先说明上述副作用和影响范围；如果实际命令需要扩张范围，先停止并更新scope。

## 前置条件

1. 当前工作目录：仓库根目录。
2. 当前分支或版本：TOO-308 独立分支，且工作区无无关tracked修改。
3. 必需命令：`git`、Go、Node/npm、`make`、`rg`。
4. 必需配置：不需要真实凭据；`CGO_ENABLED=0`验证Pure Go路径。
5. 必需测试环境：macOS用于完整app bundle；Store/Query focused测试可在支持Go的macOS/Linux运行。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
PATH="/usr/local/go/bin:$PATH"

cd "$REPO_ROOT"
git status --short --branch
go version
node --version
npm --version
```

预期结果：

- 初始化命令成功退出，当前分支和版本符合本卡范围。
- 输出不包含敏感值或机器私有数据。

## 主路径

### 1. 前置与机械边界检查

```bash
set -euo pipefail

test "$(git rev-parse --show-toplevel)" = "$PWD"

if rg -n '\.Raw\(|\.Exec\(' internal/store/analytics_query_project.go; then
  echo "Project analytics query must remain GORM-first" >&2
  exit 1
fi

if rg -n 'root_path|initial_cwd|current_cwd|git_remote|source_generation|start_offset|complete_offset' \
  internal/query/usagecost/project.go internal/query/usagecost/types.go; then
  echo "Project DTO/query contains forbidden local facts" >&2
  exit 1
fi
```

预期结果：

- Project query没有 `.Raw`/`.Exec`。
- Project跨端DTO/mapping没有路径、Git remote或parser位置/generation字段。

### 2. Store focused tests

```bash
set -euo pipefail

go test ./internal/store -run 'TestProjectAnalytics' -count=1
CGO_ENABLED=0 go test ./internal/store -run 'TestProjectAnalytics' -count=1
```

预期结果：

- exact SessionCount、31→30日trend suffix、Session/Model equal/NULL/unknown双keyset page、scoped attribution completeness、active-generation rollover、取消、restart与reconciliation drift测试全部通过。
- CGO关闭时仍使用Pure Go SQLite路径通过。

### 3. Query、app 与 frontend contract tests

```bash
set -euo pipefail

go test ./internal/query/usagecost -run 'Test(ListProjects|ProjectDetail|Project.*Cursor)' -count=1
go test ./internal/app -count=1

(
  cd frontend
  npm test -- --run src/bindings/contracts.test.ts src/queries/business.test.ts
  npm run typecheck
  npm run build
)
```

预期结果：

- 双page默认值20、上下限与空cursor的reader前拒绝、AEAD round-trip、篡改、跨Project/range/endpoint replay、active-generation rollover、首屏精确cardinality、错误映射、partial与privacy测试通过。
- Wails contract仍是1 service / 15 methods / 1 event；完整request继续进入query key并支持AbortSignal cancellation。
- TypeScript类型检查与生产构建通过；若只有既有bundle-size warning，记录为非阻断观察。

### 4. 全量 Go、race、module 与控制面验证

```bash
set -euo pipefail

go test ./... -count=1
go vet ./...
go mod tidy -diff
CGO_ENABLED=0 go test ./internal/store ./internal/query/usagecost -count=1
go test -race ./... -count=1

make verify-architecture
make verify
```

预期结果：

- 全量test/vet/tidy/race/CGO-off通过。
- harness/project/version gate无finding。
- app生成、构建、签名和bundle verification通过；不执行GitHub Actions或正式发布。

### 5. 生成稳定性与最终diff检查

```bash
set -euo pipefail

git diff --check
git status --short

go test ./internal/app -run 'TestBinding' -count=1
(
  cd frontend
  npm test -- --run src/bindings/contracts.test.ts
)
```

预期结果：

- 无whitespace错误、无意外schema/workflow/release文件、生成绑定与Go contract一致。
- exact exported method/event gate保持不变。

### 6. 清理

```bash
set -euo pipefail

rm -rf .task bin
find frontend/dist -mindepth 1 ! -name .gitkeep -delete 2>/dev/null || true
git status --short
```

预期结果：

- 只删除可重建本地构建产物，保留tracked `.gitkeep`和源文件。
- 工作区只剩本卡预期tracked diff与允许的repo-local ignored state/run。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| 前置/机械边界失败 | 发现无关diff、raw query、隐私字段或工具缺失 | 更新本地recovery point并记录脱敏摘要 | 收窄diff/修复边界后从步骤1重跑 |
| focused test失败 | 功能、分页、对账、cursor或CGO-off任一失败 | 先补/保留能复现的红测，记录测试名和错误类别 | 最小修复后从对应focused命令重跑 |
| frontend/binding失败 | 类型、method/event allowlist或生成资产漂移 | 记录contract diff，不接受手改生成文件掩盖问题 | 修复Go source-of-truth并重新生成/测试 |
| full/race/harness/build失败 | 任一必需gate非零退出 | 记录命令和脱敏finding | 修复后从失败gate开始，再重跑全部收口gate |
| 清理失败 | 可重建产物残留或误触tracked文件 | 停止commit并记录残留范围 | 只清理已确认生成物，重新读回status |

## 结果回写

执行完成后，回写本文前部的 `当前验证结果`、`本次执行结果` 和 `当前步骤状态`。

固定规则：

- 已执行的步骤写真实结果。
- 未执行的步骤显式写 `未执行` 或 `blocker`，不得写成 `通过`。
- 提交版文档只保留脱敏摘要，不写真实凭据、token、cookie、数据库主机、连接串、行主键、cursor payload、临时目录、完整下载URL、原始响应或机器本地痕迹。
- 原始命令输出只留在受控本地run/issue comment，不提交。
- 后续同步或closeout不得把已脱敏的历史结果摘要删成空模板；有新结果时追加或替换为更新的真实摘要。
