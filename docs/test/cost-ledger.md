# Pricing、Turn Cost 与 Daily Rollup 验证 Runbook

## 当前验证结果

- 记录时间：2026-07-14 CST
- 对应 Issue：TOO-255
- 当前结论：implementation review 的 2 High + 1 Medium 已按 TDD 修复并由原 subagent 复审清零；CHANGELOG 与 data-model 已同步，post-integration focused/count=20、Pure Go race/coverage、全仓 test/vet/race、harness/project/version/diff 和完整 `make verify` 全部通过；不同 subagent final scope review `blocking_findings=0`，进入 PR preparation。
- Actions：`actions_disabled_by_user`，本 runbook 不启用、查询、触发或等待 GitHub Actions。
- Release：不适用；普通 Execution Issue 不创建 tag、release 或签名产物。

## 目标

- 证明内置 price catalog 是有来源、带日期、不可变、exact-only 的整数快照。
- 证明 final turn usage 可以得到可手算的 API 等价微美元成本；provisional、unknown、缺失 token 和缺失价格不会伪装为零。
- 证明 session、global day、project day、model day 使用同一 generation 严格对账。
- 证明 IANA timezone、DST、nullable token、partial priced subtotal、同名 project 与安全 attribution 语义稳定。
- 证明 rebuild 的 replay、故障、取消、overflow 和重启不会暴露半成品或覆盖旧 active generation。
- 证明生产成本路径使用 GORM-first Pure Go SQLite，不依赖 `gorm.io/driver/sqlite` 或 `github.com/mattn/go-sqlite3`。

## 副作用与输出位置

- Go tests 只在 `testing.T.TempDir()` 下创建 synthetic SQLite、WAL/SHM 与 migration backup，正常结束由测试清理。
- 命令会使用本机 Go/npm/Wails 缓存；`make verify` 可能生成 ignored 的 `frontend/node_modules/`、`frontend/dist/`、`.task/` 和 `bin/`，closeout 只清理本轮产物。
- 不读取真实 `~/.codex`、真实应用数据库、真实项目路径内容或凭据，不访问价格 API。

## 价格快照与免责声明

内置版本固定为 `openai-api-2026-07-14`，币种 `USD`，价格单位 `microUSD / 1,000,000 tokens`，`effective_from_ms=0`，`verified_at_ms=1783987200000`。运行时只安装本地快照，不联网抓价。

| exact model key | input | cached input | output | 官方证据 |
| --- | ---: | ---: | ---: | --- |
| `gpt-5-codex` | 1,250,000 | 125,000 | 10,000,000 | [GPT-5 Codex](https://developers.openai.com/api/docs/models/gpt-5-codex) |
| `gpt-5.1-codex` | 1,250,000 | 125,000 | 10,000,000 | [GPT-5.1 Codex](https://developers.openai.com/api/docs/models/gpt-5.1-codex) |
| `gpt-5.1-codex-max` | 1,250,000 | 125,000 | 10,000,000 | [GPT-5.1 Codex Max](https://developers.openai.com/api/docs/models/gpt-5.1-codex-max) |
| `gpt-5.2-codex` | 1,750,000 | 175,000 | 14,000,000 | [GPT-5.2 Codex](https://developers.openai.com/api/docs/models/gpt-5.2-codex) |
| `gpt-5.3-codex` | 1,750,000 | 175,000 | 14,000,000 | [GPT-5.3 Codex](https://developers.openai.com/api/docs/models/gpt-5.3-codex) |
| `gpt-5.4` | 2,500,000 | 250,000 | 15,000,000 | [GPT-5.4](https://developers.openai.com/api/docs/models/gpt-5.4) |
| `gpt-5.5` | 5,000,000 | 500,000 | 30,000,000 | [GPT-5.5](https://developers.openai.com/api/docs/models/gpt-5.5) |
| `gpt-5.6` / `gpt-5.6-sol` | 5,000,000 | 500,000 | 30,000,000 | [GPT-5.6 Sol](https://developers.openai.com/api/docs/models/gpt-5.6-sol) |
| `gpt-5.6-terra` | 2,500,000 | 250,000 | 15,000,000 | [GPT-5.6 Terra](https://developers.openai.com/api/docs/models/gpt-5.6-terra) |
| `gpt-5.6-luna` | 1,000,000 | 100,000 | 6,000,000 | [GPT-5.6 Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna) |

目录 source URL 固定为 [OpenAI API Pricing](https://developers.openai.com/api/docs/pricing)。`gpt-5.2-codex-max`、`gpt-5.3-codex-spark`、Pro、日期 snapshot 和其它未逐项确认的 key 保持 `unpriced/model_not_listed`；不借 prefix/default 或相似名字猜价。长上下文、regional、cache-write、Batch/Flex/Priority 倍率不在当前 JSONL 事实 contract 中，也不参与估算。

所有金额只是公开 API 单价下的本地等价估算，不代表 OpenAI/Codex 实际账单、订阅配额或应付款。

## 固定公式与 unknown 语义

```text
numerator = input_tokens × input_rate
          + cached_input_tokens × cached_rate
          + output_tokens × output_rate
          + reasoning_tokens × output_rate

estimated_usd_micros = round_half_up(numerator / 1_000_000)
```

所有 component numerator 先精确求和，最后只 round 一次。Codex JSONL 的 reasoning 是独立于 output 的计数，两者使用同一个公开 output rate，不重复包含。

| 场景 | 结果 |
| --- | --- |
| 全部 token 已知且 rate 完整 | `priced`，金额允许真实 `0` |
| 任一 token 为 `NULL` | `unpriced/missing_token` |
| 正 token 类别缺少 rate | `unpriced/missing_price_component` |
| 没有生效 catalog | `unpriced/catalog_not_effective` |
| 版本存在但 exact key 未列出 | `unpriced/model_not_listed` |
| model attribution 缺失/冲突/非法 | `missing_model` / `conflict_model` / `invalid_model` |
| 没有 attribution row | `unpriced/missing_attribution` |

rollup 中任一成员缺失某 token component 时，该 component 与 `total_tokens` 为 `NULL`；其他完整 component 仍保持整数和。只要存在 priced turn，金额保存 priced subtotal 并同时返回 unpriced count；完全没有 priced turn 时金额为 `NULL`。

## Synthetic Fixture Matrix

1. 1M input + 1M cached + 1M output + 1M reasoning 的 `gpt-5.2-codex`，手算结果 `29,925,000 microUSD`。
2. all-zero、missing token、缺失 rate、negative 和 `int64` overflow；合并 numerator 的 half-up 边界。
3. provisional usage 排除；final 的 missing、conflict、invalid、future safe model 分别保留固定 reason。
4. catalog 生效前、v1 边界、v2 边界与只命中 prefix 的 model，证明 `[from,to)` 和 cost exact-only。
5. `Asia/Shanghai` 23:59:59.999/00:00，以及 `America/New_York` DST fall 日界。
6. 同 display name、不同 stable project ID；unknown/conflict/invalid dimension 不合并；typed JSON 不含 synthetic raw cwd/model marker。
7. nullable token、partial subtotal、session/global/project/model turn/priced/unpriced/token/cost 对账。
8. generation 一致 replay、冲突 replay、trigger fault、context cancel、aggregate overflow；精确校验 batch/state `RowsAffected`，并在切换前读回 turn cost 与四类 rollup 重新对账；失败后旧 active 保持且失败 generation 为零行。
9. canonical/alias 与同 stable project identity 的 provenance/display 差异采用确定性的保守 `mixed` 语义，不因合法归因演进中止 rebuild。
10. Store close/reopen 后 active generation、turn cost 和 daily rollup 完整读回。
11. fresh schema、v4→v5、STRICT/FK/partial unique index、SQLite `NULL` 三值逻辑对抗测试与 v1-v5 checksum freeze。

## 验证命令

### 1. Pricing、migration 与 cost focused

```bash
CGO_ENABLED=0 go test ./internal/pricing ./internal/store -count=1
CGO_ENABLED=0 go test ./internal/pricing -count=20
CGO_ENABLED=0 go test ./internal/store \
  -run 'Test.*(Pricing|Cost|Rollup)' -count=20
```

成功判据：catalog、手算、rounding、schema v5、unknown、timezone、rebuild、rollback、restart 和 strict reconciliation 全部稳定通过。

### 2. Race、coverage、Pure Go 与 GORM boundary

```bash
CGO_ENABLED=0 go test -race ./internal/pricing ./internal/store -count=1
CGO_ENABLED=0 go test -cover ./internal/pricing ./internal/store -count=1
if CGO_ENABLED=0 go list -deps ./internal/pricing ./internal/store | \
  rg '^(gorm.io/driver/sqlite|github.com/mattn/go-sqlite3)$'; then
  exit 1
fi
if rg -n '\.(Raw|Exec)\(' internal/pricing \
  --glob '*.go' --glob '!**/*_test.go'; then
  exit 1
fi
if rg -n '\.(Raw|Exec)\(' internal/store \
  --glob 'cost_*.go' --glob '!cost_*_test.go'; then
  exit 1
fi
```

成功判据：race/coverage 退出码为 0；实际编译依赖不含 official driver/mattn；production pricing/cost 文件不使用 GORM `Raw`/`Exec`。DDL/CHECK/index 与 test fault/schema introspection 是隔离例外。

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
| pricing/store focused | PASS：catalog、calculator、schema v5、rebuild/rollup/rollback/restart，以及 safe provenance merge/静默 persistence fault 测试通过 |
| SQLite nullable CHECK | PASS：missing component + forged total、complete components + missing total 均被拒绝；同 timezone 第二个 active 被拒绝 |
| production GORM boundary | PASS：`internal/pricing` 与 production `internal/store/cost_*.go` 没有 `Raw`/`Exec` |
| 实际依赖链 | PASS：`CGO_ENABLED=0 go list -deps` 未命中 official driver/mattn |
| count=20 / Pure Go race / coverage | PASS：cost/rollup focused 与 pricing 各 20 次；Pure Go race 通过；pricing 100%、store 80.0% statement coverage |
| full repo test/vet/race | PASS：全仓全部通过；仅出现已登记的 macOS deployment-target linker warning |
| harness/project/version/diff | PASS：`harness-verify`、精确 Wails `v3.0.0-alpha2.117` 下的 `project-check`、`findings=[]` 与 diff check 通过 |
| `make verify` | PASS：post-integration 重跑 Go/vet、前端 typecheck/test/build、generated stability、thin arm64/minOS 15、ad-hoc app/ZIP 全部通过 |
| implementation subagent review | PASS：原 2 High + 1 Medium 均 closed，`blocking_findings=0` |
| post-integration / final scope review | PASS：不同 subagent 终审无 finding，`blocking_findings=0`，scope/changelog/version/GORM/隐私边界通过 |
| PR / merge / post-merge | PENDING |

## 失败处理

| 失败 | 停止条件 | 恢复方式 |
| --- | --- | --- |
| catalog/手算不符 | 价格、version/source/date、公式或 rounding 任一不符 | 先核对官方 model page，再增加冻结 fixture；不得用近似模型猜价 |
| unknown 变成零 | unpriced reason 丢失，或无 priced turn 的 subtotal 变为 `0` | 先增加最小 nil/zero fixture，再修 calculator/mapper |
| rollup 漂移 | 任一 session/global/project/model count、token 或 cost 不等于 turn 集合 | 不切换 generation；修 accumulator/reconcile 后重跑步骤 1 |
| 半成品可见 | fault/cancel/overflow 后 active 改变或失败 generation 留行 | 修 transaction/switch 顺序，并重跑 rollback/restart fixture |
| 隐私泄漏 | typed snapshot 出现 synthetic raw cwd/model marker | 停止交付；只消费 safe attribution，重跑 privacy fixture |
| race/dependency/GORM gate | race 非零、实际依赖命中 CGO driver、production cost 出现 `Raw`/`Exec` | 修复后从步骤 2 开始并最终重跑步骤 3 |
| 全仓门禁 | 任一 test/vet/harness/project/version/verify 非零 | 记录最早失败命令与脱敏摘要，修复后从受影响 gate 重跑 |

## 清理与回写

- 只清理本轮产生的 ignored build/package artifacts，不删除用户已有 `.agents/state/`、`.agents/runs/`、`.agents/plans/` 或其他未跟踪内容。
- closeout 前把 PENDING 更新为真实结果；未执行的 gate 不得写成 PASS。
- Issue/PR 记录 `actions_disabled_by_user`，普通 Execution Issue 不触发 release。
