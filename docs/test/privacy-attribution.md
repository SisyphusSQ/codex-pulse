# Session、Project、Model 隐私归因验证 Runbook

## 当前验证结果

- 记录时间：2026-07-14 CST
- 对应 Issue：TOO-254
- 当前结论：focused/count=20、Pure Go race、全仓 test/vet/race、harness/project/version/diff 与三轮完整 `make verify` 均已通过；implementation 与 final scope review 的 findings 均按 TDD 修复并复核清零；PR/merge 尚未完成。
- Actions：`actions_disabled_by_user`，本 runbook 不启用、查询、触发或等待 GitHub Actions。
- Release：不适用；普通 Execution Issue 不创建 tag、release 或签名产物。

## 目标

- 证明 raw cwd/model 只保留在本机 canonical Store facts，安全 attribution projection 不含任何绝对路径字段或 raw model。
- 证明 Project identity 不依赖 basename，路径匹配是 segment-aware 最长唯一 root；缺失、非法和同等级冲突 fail closed。
- 证明 Model alias 得到稳定 normalized key/display，可疑输入不会回显。
- 证明 application schema v4、自动 refresh、全量重算、事务 rollback 和 parser→indexer→Store→restart 链路可靠。
- 证明归因 Store CRUD 使用 GORM，SQLite 编译链在 `CGO_ENABLED=0` 下只进入 libtnb + modernc。

## 副作用与输出位置

- Go tests 只在 `testing.T.TempDir()` 下生成 synthetic JSONL、SQLite、WAL/SHM 与 migration backup，正常结束由测试清理。
- 命令使用本机 Go/npm/Wails 缓存；`make verify` 可能生成 ignored 的 `frontend/node_modules/`、`frontend/dist/`、`.task/` 和 `bin/`，closeout 只清理本轮产物。
- 不读取真实 `~/.codex`、真实应用数据库、真实项目仓库内容或凭据，不访问外部服务。

## 固定规则

| 维度 | 输入 | 安全结果 |
| --- | --- | --- |
| Session title | 非空 Session ID | `Session <8 位大写摘要>`、`high/session_id_fallback`，不读取对话内容 |
| Project registered root | cwd 唯一命中最长 segment-aware root | stable project ID/display、`high/registered_root` |
| Project path digest | absolute cwd 无登记 root | `local-path-v1:<sha256>`、受限 basename display、`medium/cwd_path_digest` |
| Project missing/invalid | 缺失或非绝对/非法路径 | nil identity、`unknown/missing|invalid_path` |
| Project peer conflict | 同长度 root 或 Session current/latest turn 指向不同 identity | nil identity、`low/conflict` |
| Model canonical/alias | allowlisted token 或 provider prefix/大小写 alias | normalized key/display、`high/model_canonical|model_alias` |
| Model unsafe | 路径形、内容形、非法字符、超过 128 bytes | nil key/display、`unknown/invalid_model` |

路径移动会生成新的本机 Project identity；同名 basename 不合并。v0.1 不读取 `.git`、remote、仓库正文或跨机器信息。后续 M6 DTO/Wails 只能消费 `SessionAttributionSnapshot` / `TurnAttributionSnapshot`，不能直接序列化 core Store records。

## Fixture Matrix

全部 fixture 使用 synthetic `/Users/alice/...` marker，只用于否定泄漏断言：

1. canonical 与 `OpenAI/GPT-5.2-Codex` alias、未来安全 token、缺失、路径形、内容形、超长 model。
2. 不同绝对路径下同名目录、路径移动、嵌套 root、路径 segment prefix、同长度冲突、缺失/相对/root cwd。
3. Session current 与最新 turn 一致、缺失和同等级 project/model 冲突。
4. 带既有 Session/Turn 的 v3→v4 migration 同事务 backfill、backfill 失败整体回滚、fresh schema、STRICT/FK/index/canonical readback、ID/display 单边 `NULL` 拒绝。
5. 派生值被破坏后的全量重算、canonical facts 前后相等、归因写失败时同批 canonical 写入 rollback。
6. synthetic rollout 经 parser/indexer 写入，关闭并重开 SQLite 后 safe attribution 保持一致。
7. 同一 WriteUnit 内多个 Fact 只把 Session 标脏一次；callback 成功后、commit 前仅刷新一次该 Session 的全部 Turn attribution。

## 验证命令

### 1. 纯规则与 Store/Indexer 链路

```bash
CGO_ENABLED=0 go test ./internal/attribution ./internal/store ./internal/indexer -count=1
CGO_ENABLED=0 go test ./internal/attribution -run 'Test(Normalize|Resolve|Arbitrate|SessionTitle)' -count=20
CGO_ENABLED=0 go test ./internal/store -run 'Test.*Attribution' -count=20
CGO_ENABLED=0 go test ./internal/indexer -run 'TestIngesterPersistsRestartSafePrivacyAttribution' -count=20
```

成功判据：规则、migration、refresh/recompute、fault injection、JSON leak negative assertions 和 restart readback 全部稳定通过。

### 2. Race 与 Pure Go 实际编译依赖

```bash
CGO_ENABLED=0 go test -race ./internal/attribution ./internal/store ./internal/indexer -count=1
if CGO_ENABLED=0 go list -deps ./internal/attribution ./internal/store ./internal/indexer | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
if { rg -n '\.(Raw|Exec)\(' internal/attribution --glob '!**/*_test.go'; \
     rg -n '\.(Raw|Exec)\(' internal/store --glob 'attribution_*.go' --glob '!**/*_test.go'; }; then
  exit 1
fi
```

成功判据：race 通过；实际编译图只进入 `github.com/libtnb/sqlite` 与 `modernc.org/sqlite`，不进入 official driver/mattn；production attribution files 没有 GORM `Raw`/`Exec`。`gorm.io/gorm` 上游模块的测试依赖元数据不等于本项目运行依赖，以 `go list -deps` 为准。

### 3. 全仓本地门禁

```bash
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

成功判据：所有命令退出码为 0；Wails 链接可出现仓库已记录的 macOS SDK deployment-target warning，但不得有 test/vet/race/gate 失败。Actions 不属于本轮 gate。

## 当前实测证据

| Gate | 当前结果 |
| --- | --- |
| indexer attribution focused | PASS：synthetic parser→ingester→Store→reopen 链路通过 |
| attribution/store/indexer Pure Go full | PASS：三个包 `CGO_ENABLED=0`、`-count=1` 通过 |
| partial tuple adversarial test | PASS：测试先证明旧 CHECK 会放过单边 `NULL`，收紧 schema 后 8 种 ID/key/display 单边 `NULL` 均被拒绝 |
| v3 history backfill | PASS：预置 v3 Session/Turn 升级后可立即查询；注入 path project conflict 时 v4 DDL/history/user_version 整体回滚 |
| invalid explanation | PASS：Session relative cwd/unsafe model 保留 `invalid_path`/`invalid_model`；合法与非法同级为 `low/conflict` |
| dirty Session refresh | PASS：audit trigger 先复现 3 Facts × 3 Turns = 9 次更新；WriteUnit 聚合后固定为一次 3-turn refresh，fault rollback 保持成立 |
| 实际依赖链 | PASS：`go list -deps` 输出含 libtnb/modernc，不含 official driver/mattn；全仓 Wails 包在 `CGO_ENABLED=0` 下受 macOS adapter build tag 限制，因此 Pure Go gate限定为三个目标包 |
| count=20 / Pure Go race | PASS：规则、Store attribution 和 restart E2E 各 20 次通过；三个目标包 race 通过 |
| full repo test/vet/race | PASS：仅出现已登记的 macOS deployment-target linker warning |
| harness/project/version/diff | PASS：精确临时 Wails CLI `v3.0.0-alpha2.117`；version `findings=[]`；diff 无错误 |
| `make verify` | PASS：Go/vet、前端 typecheck/test/build、generated stability、thin arm64/minOS 15、ad-hoc app/ZIP 全部通过 |
| implementation subagent review | PASS：首轮 High backfill、Medium invalid→missing 已按 RED/GREEN 修复；复核 `blocking_findings: 0` |
| post-integration | PASS：count=20、Pure Go race/coverage、全仓 test/vet/race、依赖/Raw SQL/gofmt/diff、harness/project/version 与完整 `make verify` 全部复跑通过 |
| final scope review | PASS：不同 subagent 首轮 Medium dirty refresh、Low 文档状态已修复；复核 `blocking_findings: 0`、READY_TO_COMMIT_PUSH_PR |
| PR / merge / post-merge | PENDING |

## 失败处理

| 失败 | 停止条件 | 恢复方式 |
| --- | --- | --- |
| 归因错误或泄漏 | identity/display/confidence/source/reason 不符，或 JSON 含 synthetic private marker/raw field | 先增加最小失败 fixture，再修纯规则或 safe mapping；重跑步骤 1 |
| migration/schema | v4 history/checksum、STRICT/FK/index/tuple contract 不符 | 不改写 v1-v3 history；修 v4 后重跑 migration focused 与 Store full |
| stale derived rows | 重算后派生不一致或 canonical facts 改变 | 保留失败数据库于测试临时目录用于本轮诊断；修事务边界后重跑 fault/recompute/full |
| race/dependency/GORM gate | race 非零、实际依赖命中 CGO driver、production attribution 出现 `Raw`/`Exec` | 修复后从步骤 2 开始并最终重跑步骤 3 |
| 全仓门禁 | 任一 test/vet/harness/project/version/verify 非零 | 记录最早失败命令与脱敏摘要，修复后从受影响 gate 重跑 |

## 清理与回写

- 只清理本轮产生的 ignored build/package artifacts，不删除用户已有 `.agents/state/`、`.agents/runs/`、`.agents/plans/` 或其他未跟踪内容。
- closeout 前把本节 PENDING 更新为真实结果；未执行的 gate 不得写成 PASS。
- Issue/PR 记录 `actions_disabled_by_user`，普通 Execution Issue 不触发 release。
