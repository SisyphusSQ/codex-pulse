# 会话活跃期间平均 TPS 验证

## 目标与口径

TOO-492；分支 `suqing/too-492-session-average-tps`。Codex 会话列表及详情通过 Go Helper 返回同一生命周期指标：合格已结束轮次的输出增量之和，除以活跃区间并集秒数。轮内思考、工具及等待计时，轮间空闲不计时；`output_tokens` 已含源内 reasoning，不能再次叠加。平均值先按整数毫 TPS 四舍五入，UI 本地化为两位小数。

时长优先 `duration_ms`，其次毫秒日志起止时间，最后秒级起止时间。未知输出、缺少生命周期、零/冲突时长、歧义归因或超出数值范围均排除或不可用；未结束轮次不使用当前时刻补时长。可计算子集标 partial 并显示覆盖。显式分叉继承历史暂不可确认独立 TPS；旧 parser 待补齐、超出轮数支持范围及继承历史不显示虚假的零覆盖。真实零输出与字段缺失不同。

## 实现与数据边界

- schema v35 追加 `light_turn_events` STRICT 表；事件、既有 Token/调用事实和 durable offset 同事务提交。已知 TPS 输出必须与同一来源位置的 timed Token 增量对账，漂移拒绝整批。旧 migration checksum 不变。
- parser 为 `codex-token-model-invocation-throughput-v5`，复用现有分块、counter epoch、generation 与后台预算；待重建时旧 active Token/成本仍可读。
- 查询在同一只读 snapshot 中批量读取返回页的 active facts；最近轮次 TPS 默认 20 / 最大 50 条，独立于既有 cost timeline。Session 平均不随该页截断或 range 筛选重算。
- Proto 追加 `ThroughputStats/ThroughputTurn`；平均单位 `milli_tokens_per_second`，时长单位 `milliseconds`，basis 为 `closed_turn_lifetime_output`。Go/Swift bindings 由正式生成器生成。
- Swift 只格式化 Go 结果。Cursor/Grok 没有同口径事实时显示暂不支持。现有价格及成本口径不随本功能调整。
- allowlist 事实不含消息、工具参数/结果、父会话 ID 或原始 JSONL；公开 DTO 不含 raw Turn ID、文件位置、generation 或路径。UDS 与继承 pipe 鉴权保持原有边界。

## 聚焦验证入口

以下测试使用 synthetic 数据和临时 SQLite，不使用个人 Home。会写入构建缓存及临时测试目录；生成器 `--check` 写入临时输出后比较，开发构建写入 ignored `build/dev` 和 `bin`。

```bash
go test -race ./internal/throughput ./internal/lightindex ./internal/store ./internal/core \
  -run 'TestWeighted|TestCoverageAndZero|TestOverlap|TestMissingCounter|TestClosedTurnWithoutCounter|TestTimingEvidence|TestThroughput|TestLightSessionThroughput|TestApplicationSchemaV35|TestEnsureApplicationSchemaCreatesStrictRuntimeTables' -count=1
go test ./internal/query/usagecost -count=1
go vet ./internal/throughput ./internal/lightindex ./internal/store ./internal/query/usagecost ./internal/core
swift run --package-path app/macos codex-pulse-app-tests --tps-only
bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check
git diff --check
```

| 场景 | 验证要点 |
| --- | --- |
| 加权与空闲 | 1,000/10 秒与 1,000/100 秒合并为 18.182 TPS；两轮之间加一小时不改变结果 |
| 重叠 | 并行且输出明确归属时取区间并集；没有可靠归属时排除 |
| 增量与恢复 | 同轮多次调用、重复累计、跨 batch 续扫、counter reset，不重复 reasoning 或历史输出 |
| 缺失与生命周期 | 真零、字段缺失、无 usage 的整轮、未结束、中断、时间冲突/零时长和先 usage 后 start |
| 事务与代际 | duplicate source position 整批 rollback；pending rebuild 保留旧已知子集且标 partial；list/detail 同值 |
| 数据卫生 | 私密正文和父 ID canary 不出现在安全事件；重复 JSON key、跳过的超长行不进入完整可信 TPS |
| 合同与显示 | NumericValue 真实零与 unknown、毫 TPS 单位，中文/英文、可用/缺失展示 |
| 迁移 | v35 固定 checksum，STRICT columns/FK，既有版本升级及 schema readback |

## 真实 Home 原生验收入口

使用 `make verify-live` 或等价的显式启动：真实 `${CODEX_HOME:-$HOME/.codex}` canonical Home 与独立 `0700` runtime 同时设置到 App 环境及 `--runtime-directory`。启动会只读访问 Session/JSONL，写入该 runtime 的 SQLite、偏好和既有 App Server housekeeping；首次默认偏好可自动绑定真实 Home，不覆盖既有配置。

1. 读回 preferences 的 canonical path、稳定 device ID 与 inode，并检查 App/Helper 的 `CODEX_HOME`、runtime 环境和参数；不输出完整进程环境。
2. 等待后台索引追平，打开“会话”；查看列表 TPS，选择稳定已结束会话，核对详情 TPS、输出和时长。
3. 在原生详情展开最近轮次；检查文字没有裁切，coverage 与原因可读，列表/详情同值。切换列表时间范围后再次打开同一会话，确认生命周期平均不变。
4. 抽查 partial 和继承历史会话，确认显示已知子集或 `--`，不存在伪造零。独立读取安全输出及原生日志时长复算定点值；不复制原始日志或对话。
5. 脱敏原始结果留 `.artifacts/too-492/`，提交版只保留匿名数字和结论；保持开发 App 会话详情可见。

## 2026-10-01 实测结果

| 验证层级 | 状态 | 已执行证据 |
| --- | --- | --- |
| TPS / scanner / store / core 聚焦及 race | Pass | 上述受影响用例通过，包含迁移、rollback、加权、缺失和时间边界 |
| Query regression | Pass | `internal/query/usagecost` 全包通过 |
| 原有 scanner regression | Pass | `internal/lightindex` 全包通过 |
| Swift TPS 展示 | Pass | `--tps-only` 通过；开发 App 构建成功 |
| Proto / vet / diff | Pass | Go/Swift 正式生成检查无漂移，受影响包 vet 和 diff whitespace 检查通过 |
| 真实 Home 绑定 | Pass | App/Helper 环境和参数一致；preferences path/device/inode 匹配；runtime `0700` |
| 原生界面自验 | Pass | 列表/详情同值，最近轮次可展开；“近 7 天”→“全部”仍为同一生命周期值；partial/继承提示可见，最终构建重启后再次核对 |
| 全仓长测、全量 Swift、CI | Not Run | 本地按项目规则仅执行受影响验证 |
| 提交、PR、正式签名/公证、发布 | Not Run | 本轮授权范围为开发和自验 |

匿名真实样本：1 个已结束轮次、输出 16,327 Token、原生耗时 730,364 ms，`round(16,327 × 1,000,000 / 730,364) = 22,355` 毫 TPS，原生列表和详情均显示 `22.36 TPS`。另一个 partial 样本显示 `3.18 TPS`、2 轮参与 / 1 轮排除 / 0 轮未结束；继承历史样本显示 `--` 和独立 TPS 无法确认的原因。

最终 GUI 工具因多个 worktree 开发包共用 Bundle ID 发生窗口绑定歧义。验收副本仅在 ignored `build/dev` 使用独立 `com.sisyphussq.codex-pulse.development.too492` 标识，App/Helper 可执行文件 SHA-256 与标准构建一致，继续显式绑定同一真实 Home 和私有 runtime。该副本已再次打开 TPS 详情和展开最近轮次；仓库默认 Bundle ID、签名与其他开发包未改。

本机只读安全事实测量：46 个 active 会话、5,244 条事件，流式读取并计算 16 ms；40 个 complete、4 个 partial、2 个因继承历史 unavailable。它是当前样本的增量事实/计算成本，不代表全库覆盖或硬性能承诺。查询仅在返回页工作，常规无变化文件仍复用既有 identity/checkpoint fast path，没有新增全文读取或深索引触发。

首次运行受影响 Go 包时，Store 全包只有“expected tables”清单未加入新表而失败；已补齐表、column、FK 清单并通过该失败用例及相关迁移/TPS聚焦回归。未重复执行完整 Store 长测，不将该首次全包输出声明为最终全包 Pass。

自查修复：整轮缺少任何 counter 时，对下一条可能跨轮的累计增量设置缺口；迟到 start 不得吸收活跃区间之外的 usage；超长行跳过保留覆盖缺口；TPS 与 timed Token 的同源输出漂移整批拒绝。新增 scanner/store race 聚焦回归通过。安全自查未发现新增内容/凭据暴露、鉴权放宽、外部请求或无界事件结果；公开轮次列表及内部状态均有显式上限。
