# 原生主要页面与共享数据闭环验证记录

## 当前结论

- 记录日期：2026-07-22（Asia/Shanghai）
- Execution Issue：Linear `TOO-314`，Parent `TOO-311`，前置 `TOO-313`
- 分支：`suqing/too-313-native-app-shell-overview`，HEAD 基线 `5db90ad`，stacked on `suqing/too-312-swift-transport-spike`
- 当前状态：七个主要页面、共享 runtime、Home-switch quota/reset credits 恢复、轻量索引 API 等价成本与按模型统计、确定性测试、隔离 App/Helper regression smoke、真实 Home 多页面 live E2E 和完整 `make verify` 均已通过；当前停在未提交 pre-commit ready 收尾阶段。
- 本轮不执行：commit、push、PR、merge、release、签名、公证、完整 Xcode/XCTest/XCUITest、真实 OS sleep/wake。

## 2026-07-22 真实 Home 多页面结果

- 用户明确授权直接使用真实 Codex Home，并允许 App Server 正常状态库、WAL、锁和日志写入；Codex Pulse 不复制原始 JSONL，派生索引继续写已配置私有 runtime SQLite。
- 正式 Home-switch preferences 已提交 generation `2`，但第一次 post-commit light-index 发布未完成，SQLite 仍 fence 在旧隔离 Home，导致后续 Helper 冷启动 `core_unavailable`。使用现有 `StartHomeSwitch` 对同一 runtime 做 identity-fenced 派生 metadata 恢复后，真实 App/Helper E2E 通过。该恢复缺口未伪装成正常成功路径。
- App Server metadata provider 返回 `925` 个唯一 thread，重复 `0`、diagnostic `0`；提交版不记录 thread 名称、项目名、cwd、rollout 路径或原始 payload。
- 稳定结果：Overview session 与 7 日趋势非零；Sessions/Projects 首屏各返回非零数据；Sources 非零；四类详情读取成功；Settings receipt/readback/conflict/restore 完整；七页 render；`unavailable=none`；Shutdown clean。

```text
app smoke passed: overview=loaded quota_windows=1 sessions=5 trend_points=7 health=healthy primary_pages=loaded sessions=20 projects=20 sources=6 jobs=0 health_events=1 usage_trend=7 usage_models=5 usage_cost=known quota_windows=1 details_read=4 settings=receipt+readback+conflict+restored unavailable=none ui_pages=7 native_surfaces=window+status_item+popover lifecycle=not_executed shutdown=clean
```

后续本机 development App / 主要页面验证默认执行：

```bash
CODEX_PULSE_APP_RUNTIME=<existing-configured-private-runtime> \
  make verify-swift-primary-pages
```

该命令不创建新 runtime，要求 confirmed Home 已是当前真实 Codex Home，允许标准 housekeeping，并对非零 Sessions/Projects/Usage、详情、七页 render 和 clean Shutdown 做稳定断言。CI 和 `make verify` 使用 `make verify-swift-app-smoke-isolated`，只承担不依赖用户数据的确定性 regression。

## 2026-07-22 额度与重置额度恢复修复

- 初始真实 App 读回：quota window `0`，reset credits `never_loaded`；两个 Wham source 均为 `unavailable/auth_required`，持久 schedule 均无 next due，且失败记录发生在切换到真实 Home 之前。
- 当前真实 Home 的 auth 文件只做结构和权限校验，未输出或复制任何 token；使用生产 credential provider 的只读探针通过。随后在 App 内分别触发 quota 与 reset credits 两个只读刷新，均成功完成。
- 手动刷新后的脱敏事实：quota observation/current 各 `1`；reset credits snapshot `1`、available/total `3/3`；两个 source 均为 `current`、连续失败 `0`、无 failure code、next due present、active claim none。页面显示可信剩余比例、重置时间和 `3/3`，未记录原始响应或真实路径。
- 根因：原先隔离 Home 缺少 credential，调度器把两类来源持久化为 `auth_required` 且无限期暂停；正式 Home switch 只恢复 quota runner generation，没有解除属于旧 Home 的 credential pause。Swift 页面、generated client、Helper query、Wham 两个 endpoint 和 pagination 均不是根因。
- 修复：Home switch/rollback 恢复 quota generation 后，对当前 preferences 中已启用的在线来源写入一次立即到期的 `recovery` schedule，并通过既有 durable claim/attempt/completion 链刷新；禁用来源保持 `disabled`，不伪装成 manual refresh。
- TDD 证据：第一条回归先在现有实现上稳定失败（切换后两秒内没有 transport request）；对抗复审新增的“失败回滚只重探一次”回归也先捕获到双请求，再收敛为单一外层 rearm。两条测试在 race 下重复 `20` 次通过，相关 `internal/app`、`internal/scheduler`、`internal/codex/quota` 包完整测试通过。
- 更新后真实 Home live smoke：`quota_windows=1`、Sessions/Projects/Usage 非零、四类详情成功、`unavailable=none`、七页 render、shutdown clean。最终常驻 App 读回 quota `fresh/current`、可信重置时间和 reset credits `3/3`。
- 最终 `make verify` 通过，覆盖全仓 race/vet、Proto、Swift transport/App tests 和隔离 smoke。此前两次完整 gate 分别命中两条既有 Home-switch race 用例的 `unavailable`，但精确 race 分别重复 `10`/`20` 次无法复现，且最终完整 gate 从头通过；该并行压力抖动保留为 bounded observation，不写成已修复的新缺陷。

真实手动刷新会访问两个既有只读 Wham GET endpoint，并在当前私有 runtime DB 中写入 source attempt/state、schedule、quota snapshot 与 reset credits snapshot；不会 consume credit，不会写真实 Codex Home 内容。自动化 gate 继续使用 synthetic credential/fake transport，不依赖真实用户数据。

## 2026-07-22 API 等价成本与按模型统计修复

- 根因不在 Swift 卡片：development App 的 production runtime 选择轻量 metadata/index 路径，明确跳过 legacy full-history bootstrap；生产代码也没有调用 `RebuildCostLedger`。`UsageCostRange` 检测到轻量 Session 后会直接读取 `light_token_timed`，但 parser v1 没有保存模型，因此既无法 exact-match pricing catalog，也没有全局模型 breakdown。
- parser v2 在原有分块 scanner 内只解析安全的 `turn_context.model` normalized key/source，并把当前模型 checkpoint 与 timed token delta 一起持久化。它不保存 prompt、response、tool output 或 raw model；非法/超长模型继续归入 unknown。
- Helper 在同一只读 snapshot 中按日期、模型和生效 pricing version 聚合 token/cost，新增 `UsageModelItem`；Swift 只展示 generated response。没有 exact price 的模型保留 unpriced/partial，不会伪装成零或完整。
- 成本公式修正为 `(input-cached)×input_rate + cached×cached_rate + (output+reasoning)×output_rate`。Codex 的 cached input 是 input 子集，旧公式会重复计费；`cached > input` 现在 fail closed。
- 新增 append-only Schema v17 和不可变 `openai-api-2026-07-22` catalog；旧版本及 checksum 不改写。合成手算 fixture 对 `1,000,000 input`（其中 `200,000 cached`）、`100,000 output`、`50,000 reasoning` 的 `gpt-5.4-mini` 得到 `$1.29`，同时断言模型 breakdown 与总成本一致。
- 真实 Home live gate 复用既有私有 runtime，返回 `usage_models=5`、`usage_cost=known`、`primary_pages=loaded`、`unavailable=none`。退出后只读聚合检查为 Schema v17、`quick_check=ok`、pricing version `2`；第二次短生命周期 smoke 结束时 parser v2 已从 `10` 推进到 `55`。随后同一 development App 按正常常驻场景继续后台工作，并发只读快照最终读回当前 `933/933` 条 scan 全部为 parser v2、active/complete，历史安全模型维度共 `7` 个。
- 对抗式复审另发现 unchanged rollout 的 fast path 原先早于 parser-version 判断，会让真正未变化的 v1 行永远不重建。新增 runtime 回归先稳定失败，再将 fast path 限制为“已是当前 parser version”；修复后同文件也会进入现有 identity/prefix-fenced rebuild。
- 隔离 empty Home smoke 仍必须返回 `usage_models=0 usage_cost=unknown`，证明 CI regression 没有借用真实用户数据制造成本。

## 实现与事实边界

| 页面 / 能力 | 真实数据入口 | 客户端责任 | 不在 Swift 执行的责任 |
| --- | --- | --- | --- |
| Overview | `UsageCost`、`QuotaCurrent`、`ListSessions`、`HealthProjection` | 全局摘要、导航、共享刷新与错误呈现 | 额度、用量、健康结论重算 |
| Sessions | `ListSessions`、`SessionDetail` | 筛选、选择、opaque cursor、content-free turn timeline | JSONL/SQLite 读取、session 聚合 |
| Projects | `ListProjects`、`ProjectDetail` | date range、双独立 cursor、详情呈现 | project/model 归因与统计重算 |
| Quota / Usage | `QuotaCurrent`、`UsageCost`、`RequestQuotaRefresh` | zero/unknown/partial、新鲜度、receipt 后刷新 | 窗口仲裁、成本与趋势计算 |
| Local Status / Health | `HealthProjection`、`DataHealth`、`ListHealth`、`Health`、`RunRuntimeAction` | degraded/unavailable/stale 展示、诊断选择、确认式全局调度控制 | 阈值判断、transport 状态转业务状态；provider recovery command 执行 |
| Sources / Jobs | `ListSources`、`Source`、`ListJobs`、`Job` | 稳定分页、筛选、详情、provider recovery action | 来源/任务业务状态推导 |
| Settings | `Settings`、`UpdateSettings` | editable metadata、revision CAS、receipt/readback/conflict | Home switch、repair、migration、release |

所有页面使用生成的 `CoreService` Swift 类型和同一个 `AppRuntime` / `AppModel`。Swift source gate 禁止 SQLite、JSONL、loopback TCP 与 shadow transport；`RunRuntimeAction` 仅允许 `pause_backfill`、`pause_all`、`resume`、`reconcile`，执行前必须确认。`runtime.source.retry` 等 provider recovery command key 属于诊断命名空间，当前 contract 没有对应执行 RPC，界面只读展示，绝不映射成调度控制或高风险 repair/Home 动作。

## 执行副作用与测试边界

- `make verify-swift-app-smoke-isolated` 会构建 Swift/Go 产物，在 `/private/tmp/cp-app-smoke.*` 下组装 unsigned development App 并启动一个临时 App/Helper。
- smoke seed 通过 Go preferences store 在隔离目录创建有效配置；confirmed Home 是同一临时 root 内的空目录。
- Helper 启动 App Server 时显式覆盖 `CODEX_HOME` 为 confirmed Home；不继承真实用户值。
- token 仍只通过继承 pipe 传递；通信只使用私有 UDS；输出只保留稳定状态与计数。
- smoke trap 删除临时 App/runtime；SwiftPM、Go cache 和 ignored `bin/` 可保留。
- 以上条目描述 CI-safe 隔离 regression；真实 Home live gate 复用已配置 runtime，不创建或删除 runtime，并允许 App Server 标准 housekeeping。

## 已执行 focused 验证

### Swift deterministic build/tests

```bash
make verify-swift-app
```

结果：PASS。

覆盖：

- canonical Gregorian / IANA timezone 与半开日期范围；page limit 被限制在 `1...100`，opaque cursor 原样透传。
- Sessions 刷新 generation、稳定去重、load-more、选择/detail；旧请求不能覆盖新筛选结果。
- Projects 的 sessions/models 双 cursor 独立推进；一个子分页完成后不会被另一个子分页的响应重置。
- `complete`、`partial`、`empty`、`stale`、`cancelled`、`unavailable` 保留上一份有界事实和稳定 issue code；未知 response status fail closed。
- typed invalidation 会使相关 cache/generation 失效并重载当前可见页；lifecycle invalidation 只取消读请求，不取消正在等待 receipt/readback 的 mutation。
- cursor 只在成功响应后消费；transient load-more 失败可用同一 opaque cursor 重试，真正重复 cursor 会终止分页并显示稳定 contract notice。
- Settings 非 editable 字段保留权威值；写请求携带 authoritative revision；receipt 后读回，不一致进入 conflict；刷新或保存期间 UI 禁用且模型保留并发产生的新草稿。
- quota mutation 与 runtime action 都使用 single-flight；后者只暴露独立的 `RunRuntimeAction` allowlist，`repair` 不会被接受。
- non-retryable contract incompatibility 禁用 Toolbar/Popover refresh/restart，不进入无意义的重连循环。
- App start/active/sleep/wake/recovery/restart/shutdown、Helper exit、stream failure、cancel/replacement 的 generation/lifecycle 状态机。

### 隔离 development App / Helper 多页面 smoke（历史与 CI regression）

```bash
make verify-swift-app-smoke-isolated
```

结果：PASS。

脱敏读回：

```text
app smoke passed: overview=loaded quota_windows=0 sessions=0 trend_points=0 health=empty primary_pages=partial sessions=0 projects=0 sources=0 jobs=0 health_events=0 usage_trend=0 usage_models=0 usage_cost=unknown quota_windows=0 details_read=0 settings=receipt+readback+conflict+restored unavailable=projects_unavailable ui_pages=7 native_surfaces=window+status_item+popover lifecycle=not_executed shutdown=clean
app smoke cleanup passed: isolated_runtime=yes user_codex_home=no rollout_data=no
```

解释：

- `sessions=0` 是 confirmed empty Home 的权威空结果，也是“不读取真实用户 Home”的负向证据；不是静态 mock。
- `projects_unavailable` 是空索引下 provider 返回的有界业务 unavailable，不被写成 complete/normal。
- `usage_cost=unknown` 与 `usage_models=0` 是 empty Home 的权威空/unknown contract，不会被客户端改写为 `$0` 或 synthetic model。
- `primary_pages=partial` 与上述 unavailable 一致；`ui_pages=7` 表示真实 RootView 依次消费了七个共享 route 并实例化对应原生页面，不代表 XCUITest/视觉断言。
- Settings smoke 使用隔离 preferences 执行安全字段 mutation、receipt/readback、旧 revision conflict，再恢复原值。
- 没有可供读取的 list item，因此 `details_read=0`；detail 的 generated client、selection 与分页由确定性测试覆盖，不能声称空数据 smoke 已执行 detail live query。
- `lifecycle=not_executed` 表示未把真实 OS lifecycle 写成通过。

### Go 数据隔离 focused gate

```bash
go test ./internal/codex/appserver ./internal/lightindex ./internal/app
```

结果：PASS。

关键回归：App Server 子进程环境移除所有继承的 `CODEX_HOME`，只注入 canonical confirmed Home；unit test 使用私有标记值证明继承项已被删除，且不会打印该值。

## 整体 gate、review 与对抗式复验

```bash
make verify-project
make harness-verify
make verify
git diff --check
make harness-review-gate PLAN=.agents/plans/2026-07-21-too-314-native-primary-pages.md
```

结果：`make verify-project`、`make harness-verify`、最终 `make verify` 均 PASS；`git diff --check HEAD` 与 plan review gate 在最终文档写回后复跑。

最终 `make verify` 覆盖：

- project 正/负向 contract fixtures、harness、Proto 生成一致性；
- `go test -race ./...`、`go vet ./...`；
- Swift Core deterministic tests、认证 UDS transport spike、App deterministic tests 与 build；
- unsigned development App 的七页 route/render、隔离 empty Home 下的真实 Helper/UDS 数据链、Settings CAS smoke、受控 Shutdown 与隔离清理；真实 Home 产品数据链由单独的 live gate 覆盖。

独立 reviewer 在第一次完整 gate 后审查基线 `5db90ad` 到全部 tracked/untracked diff，初次报告 8 个 blocking findings。主线程统一修复 invalidation/restart、terminal generation、Settings 草稿与 single-flight、unavailable fail-closed、pagination cursor、provider recovery 命名空间和真实多页面 render smoke；随后对抗式审查继续修复 UDS `bind -> chmod(0600)` 瞬态等待、lifecycle mutation 取消、Settings 并发编辑、transient cursor 重试及不可重试 contract 重连。相同 reviewer 最终读回：`critical_findings=0`、`blocking_findings=0`、`status=PASS`。

## Toolchain blocker / 未执行项

- 当前 `xcode-select` 指向 `/Library/Developer/CommandLineTools`；`xcodebuild`、XCTest、XCUITest 不可用。
- 未获授权安装或切换完整 Xcode；未使用签名凭据。
- 已执行真实用户 Codex Home E2E；真实 OS sleep/wake/foreground、VoiceOver/XCUITest、签名、公证、archive、正式 migration/release仍未执行。
- 因而当前证据是 SwiftPM + AppKit/SwiftUI executable + unsigned development App 的最接近真实 integration E2E，不等价于正式发布验收。

## 2026-07-22 Sessions 视觉回看修正

- 修复 `RuntimeAwarePage` / `FeatureStateView` 未占满详情区导致内容垂直居中的问题，页面内容固定从顶端开始布局。
- Sessions 筛选区改为 adaptive grid；默认窗口宽度为三列加两列的稳定两行布局，较宽窗口可自然扩展，避免单行横向裁切。
- 空列表不再强制展示空列表与错误详情双栏；没有选择会话时保持详情 `.idle`，index invalidation 不再把它伪装成 `content_invalidated` 错误。
- partial / stale 信息仍可访问，但移除整条粉橙色背景；全局与页面级提示不再重复。
- 原生窗口在安装最终 content view 后设置默认内容尺寸并居中，避免启动时只剩部分窗口可见。

本轮执行：

```bash
make verify-swift-app
git diff --check HEAD
```

结果：PASS。随后在 `/private/tmp/cp-app-smoke.preview.*` 隔离运行目录重建 unsigned development App，真实 Helper 仍通过 mode `0600` 的认证 UDS 连接；使用 App 可访问性树与当前窗口截图读回 `会话` route、五个筛选控件、`应用筛选`、紧凑 partial 状态和单一空结果状态。当时曾将 App 留在 Sessions 页面供人工回看，后续已切换为真实 Home 常驻场景。

该段记录的是当时的视觉回看现场；2026-07-22 已在后续真实 Home live gate 后重跑完整 `make verify` 并 PASS。XCUITest / VoiceOver 仍未执行，不写成通过。

## 失败与恢复

| 失败面 | 处理 |
| --- | --- |
| Swift compile/state test | 修复后重跑 `make verify-swift-app` |
| App/Helper smoke timeout或 forced shutdown | 视为失败；trap 终止本轮进程并删除唯一临时 root |
| empty Home 出现非零 session 或 rollout JSONL | 视为隐私阻塞，不继续 review |
| 真实 Home preferences 与 light-index identity 不一致 | 视为 post-commit recovery 阻塞；不得用 audit `completed` 冒充 App 可冷启动，先完成有 fence 的派生索引恢复 |
| Settings 未恢复原 revision/value | 视为 mutation 阻塞，保留隔离证据后修复 |
| 完整 gate 失败 | 不派 review；先修复并重跑 focused + full gate |
| 独立 review blocking finding | 主线程修复，重跑受影响 gate 与完整 `make verify` |
