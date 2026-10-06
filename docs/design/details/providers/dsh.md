# DSH 客户端

DSH 是独立的第四个 Agent Provider（`dsh`）。目标是在现有 Pulse 的使用量、Session、项目、模型、缓存、活动、吞吐量、本地工具统计和可选中心上报中支持 DeepSeek Harness，不增加聊天、上下文查看/编辑、正文导出、模型调用或独立后台进程。

## 数据来源和归属

参考 [dsh-context](https://github.com/bowenliang123/dsh-context) 的目录发现方式；日志契约以 [DeepSeek Harness 官方源码](https://github.com/deepseek-ai/deepseek-harness) 为准。默认读取 `${DSH_HOME:-$HOME/.dsh}/sessions`，开发和 CI 可通过绝对路径 `CODEX_PULSE_DSH_HOME` 覆盖 Home，或通过 `CODEX_PULSE_DSH_SESSIONS_ROOT` 指定会话根目录。设置中的主开关支持自动、开启、关闭，不新增另一套 Home 切换流程。

目前支持官方 V3/V4 的 `<项目>/<会话>/session.vN.jsonl` 和 `session.vN.jsonl.zstd`。每个会话只读取最高编号的 canonical generation；不同压缩编码混用、未知版本、序号断裂、重复 JSON 字段、未完成尾行和损坏的压缩流均标为 partial。扫描拒绝符号链接；单文件压缩前后各限 256 MiB、单行限 16 MiB、单会话限十万条记录、一次扫描限两万个会话。

`assistant/message` 优先取顶层 usage，否则取流中最后一个 usage；`assistant/attempt` 中失败但有 usage 的尝试单独统计，不把同一个消息的流 usage 再加一次。V4 以最后一个 `session/end-seed {inherited:true}` 排除分叉继承记录；V3 使用 header 的 `seedLength`。继承段的模型路由可以带入后续记录，usage、工具和吞吐量不继承。无法确认 seed 边界时不导入该文件。

DSH 的 `inputTokens` 是未缓存输入，缓存读/写是互斥分量。Pulse 输入总量合并这三个分量；reasoning 是 output 的子集，不再加一次。缺少可选字段保留 unknown；只有精确 total 等于各分量之和时才确认未出现的缓存桶为零。缺少 usage 的尝试让会话及相关总量保持未知和 partial。Session 名称读取最新 `session/title` 的 `data.title`，兼容 DSH 自动命名、手动改名和分支 seed 继承；来源标记为 `dsh_title_event`。名称最多保留 512 个 Unicode 字符；无标题记录时显示“未命名会话”。不从正文推导标题，也不保存 `session/title-llm-request` 的消息或系统提示。项目保留路径 hash 和 basename，完整路径在写盘前丢弃。标题格式依据 [DSH 官方说明](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/session-title.md)。

Collector 在 Go Helper 内运行，按文件身份、大小和 mtime 缓存解析结果；相同内容摘要不重写事实。成功解析的会话在事务中更新。目录暂时不可用、文件移动、格式漂移或损坏不会删除已索引历史；来源状态单独更新，查询显示覆盖不足。关闭 DSH 会取消并 drain 当前 operation，旧 generation 不得继续写入或发布 invalidation。退出 App 停止 Helper；再次启动续采，不运行常驻守护进程。

同一 header Session 身份的相同文件副本只计一次；内容分歧不合并贡献，保留一个确定性视图并标记 lineage conflict/partial，不能作为完整快照清除中心历史。

## 美元模型公价估算

所有金额使用整数微美元计算，展示为 USD。费用是**按实际模型路由的 API 公价估算**；桌面账户路由 `deepseek-account` 的本地 usage 不能证明实际扣费，也不能证明账户余额或订阅额度。已有独立“API 与订阅”中的 DeepSeek 余额继续独立，不合并或再次记账。

价格来源为 [DeepSeek 官方价格页](https://api-docs.deepseek.com/quick_start/pricing/)，冻结版本 `deepseek-usd-2026-10-06`。Flash 生效时间来自 [2026-09-10 公告](https://api-docs.deepseek.com/news/news260910/)；Pro 使用仍然独立的现行费率，不执行已撤回的 Pro 重定向方案。

| 模型 | 时段 | 未缓存输入 USD / 百万 Token | 缓存读 USD / 百万 Token | 输出 USD / 百万 Token |
| --- | --- | ---: | ---: | ---: |
| `deepseek-flash` | 谷时 | 0.15 | 0.003 | 0.60 |
| `deepseek-flash` | 峰时 | 0.30 | 0.006 | 1.20 |
| `deepseek-v4-pro` | 谷时 | 0.66 | 0.022 | 1.98 |
| `deepseek-v4-pro` | 峰时 | 1.32 | 0.044 | 3.96 |

Flash 的官方别名 `deepseek-v4-flash`、`deepseek-v4-flash-vision-exp` 使用同一价格。仅已确认的 DeepSeek 官方路由适用；第三方同名模型、未知模型、缺少缓存分量或存在未定价的缓存写入均不猜价。

峰时为北京时间工作日 09:00–12:00、14:00–18:00（半开区间）；其余时间、周末和中国公共假期按谷时。2026 假期来自 [国务院假期安排](https://www.beijing.gov.cn/cs/gncs/zcwj/202603/t20260327_4568275.html)，包括调休休息日；周末补班仍遵循 DeepSeek 的周末谷价。未收录年份在可能峰时显示 unknown，避免猜测节假日。

根据已核验完整组合规则，当前历史定价窗口从 2026-09-10 04:00 UTC 开始。更早记录只保留 usage，不套用现价。以 `step/start` 的请求起始时间判定时段；一个 step 内重试缺少独立起始时间时保留 usage，费用 unknown。各互斥 Token 分量计算后统一舍入一次。中心贡献带 `:peak` / `:off_peak` 价格版本后缀；上报保留已计算的事件 USD，不用中心现价回算历史。

### Codex 订阅模型（TOO-534）

`openai-codex` 路由在 DSH 客户端内计量，客户端身份仍是 `dsh`。按 `step/start` 的独立请求时间选择已有 `BuiltinOpenAICatalog` 历史快照，再精确匹配模型 ID；`gpt-6.1-sol` 从目录的 2026-09-29 UTC 生效边界开始可估算。不执行 DeepSeek 的峰谷/假期判断，不根据模型前缀猜其他型号，也不重新核定或覆盖 OpenAI 历史费率。

DSH 的 `inputTokens` 已经是未缓存输入，计算为 `(inputTokens × 输入价 + cacheReadTokens × 缓存价 + outputTokens × 输出价) / 1,000,000`，各分量整数微美元先精确求和再 HALF-UP 一次。不能直接传给使用“缓存为 input 子集、reasoning 独立”的 Codex JSONL 计算入口，否则会重复减缓存或加推理。DSH `reasoningTokens` 是 output 子集，缺少该字段不影响已有完整输出的计价。缓存写入非零仍无对应费率；缺缓存读/写时只有 total 精确证明缺桶为零才可定价。重试缺独立起始时间继续未计价，不把首尝试或完成时间冒充起点；范围内任一请求未计价时，完整费用合计继续 unknown，并保留 priced/unpriced 计数与已知价格版本。

Mac 查询和中心导出共用 `DSHRateAt` / `EstimateDSHCost`；DeepSeek 价格版本带 `:peak` / `:off_peak`，OpenAI 保留原 `openai-api-*` 版本，不附加空时段或 DeepSeek 后缀。单一供应方范围返回对应官方 URL；混合供应方没有单一 URL，返回实际使用的全部版本。每轮详情和项目详情同样保留价格版本，未知请求不产生虚假的版本。

原生价目表合并 DeepSeek 四档和 OpenAI 精确模型参考价，标明 OpenAI 行并提供两家官方来源入口；中心 DSH 目录同时包含 OpenAI 参考快照与历史版本，费用名称统一为“模型 API 公价估算”。这些费用不是 Codex 订阅实际扣费、余额或剩余额度。未知模型、路由、时间或缺必要计数保持 unpriced，不套当前价或报免费。

费用在查询/导出时计算，不新增 SQLite/MySQL migration，也不改写源日志。既有同步每轮有界遍历已索引会话；升级后的旧记录无需增长或重扫日志，重新导出会产生不同快照摘要，沿用单调 revision 更新费用。贡献 ID 不含价格，因此 Token 与身份不变；已经入队的旧批次字节保持不可变，先续传再处理新修订。多设备仍可用旧的 unknown → known 价格证据仲裁，不相加。用户需要立即重读时可使用现有“全量补传”，不需要新后台任务或清空队列。

## Mac 和中心

Mac 的客户端选择器、汇总、菜单栏、设置、Session/项目/模型页面、价格目录和本地调用统计均支持 DSH。概览和菜单栏使用今天/近 7 天/近 30 天日历范围。按用户要求，状态栏的环形与仪表盘图形固定满格，仅作装饰；文字显示真实 Token 用量。它不表示官方剩余额度，数据契约不制造百分比或账号资料。Swift 只访问 generated CoreService client，不直接读取 DSH 日志或数据库。

中心接收独立的 `dsh` / `dsh_local` 来源；复用已有认证、设备码、分权限凭证、来源归属和批次幂等。上传内容只含允许的结构化事实、脱敏身份和缓存/吞吐量胶囊，不上报工具调用、原始日志、消息、思考、工具参数/结果、完整路径或凭据。Web 的概览、客户端筛选、用量和价格目录支持 DSH，费用注明公价估算。多设备重复会话沿用已有 canonical 去重和冲突处理。

Preferences v5 从 v4 保留三个旧客户端的意图，并给 DSH 默认 `auto`；旧三客户端设置请求继续被接受并保留现有 DSH 意图。Core Proto 无字段变动，握手仍为 `core-rpc-v9` / `provider-control-v1`。本地 SQLite v36 新增 DSH 事实表并扩展 Provider registry，v37 扩展会话标题长度和来源枚举，事务保留 lineage 与事实；历史 migration checksum 保持不变。升级解析器后，即使原日志摘要未变，也会补齐不同的标题元数据。现有上报契约携带标题，改名不改变会话/贡献身份或重复用量。Server 现有 provider 字符串与 capsule schema 可容纳 DSH，不需要新增 MySQL DDL。

## 隐私与维护

不读取 `.credentials.yaml`、账户 token、DSH profile 密钥或消息正文作为产品数据。解码只提取白名单，正文仅随源行在解析期间短暂存在，随后丢弃，不写入 Store、RPC、日志或上报。Zstd 使用固定版本的 Go 解码库，不运行参考项目的安装脚本。

价格、官方路由、日志版本或假期变更时，维护者需更新对应版本化规则及边界测试。已有历史价格版本不应被覆盖。验证入口和本次证据边界见 [DSH 验证记录](../../../test/dsh-provider.md)。
