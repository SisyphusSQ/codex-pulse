# 中心上报协议 v1

这是独立的 HTTPS/私网 HTTP 白名单 contract。身份来自 Pulse 配对凭证，payload 不声明客户端 ID、角色或执行设备。CoreService 仍仅用于本机 Swift/Helper UDS，新增本机同步 RPC 后精确握手版本为 `core-rpc-v8`；中心上报保持独立协议 v1。

`POST /api/v1/pair` 以一次性采集码换取凭证，`POST /api/v1/batches` 发送持久批次；响应使用 Server 的 `code/data/request_id` envelope。Receipt 的版本、batch_id 和接收时间必须全部有效，本机才推进确认进度。设备凭证只在 Authorization Header 使用。

## 用量事实

SessionSnapshot 的 Provider + 原始 Session ID 是中心会话身份，HomeID 是安装级 HMAC 来源分区，不是全局账号。revision 在受保护的同步库内跨重启单调递增，与本机索引 generation 无关。每个快照完整替换同一设备、Provider、Home、Session 的来源事实；deleted 是明确的来源 tombstone。尚未就绪/部分失败的本机来源不能用于删除扫描。

ContributionID 使用 Provider、Session ID、UTC 观测时间、nullable Token 计数与相同事实的出现序号。它不包含设备、路径、offset、generation、模型或价格。完整事实序号先确定，再应用首次补传起点；索引重建与价格修订不因此产生新增消耗。相同事实真实出现多次仍保留多条贡献。

价格采用历史版本与整数微美元费率证据。`codex_model_sum` 在查询范围内按原有模型/价格版本分组、汇总计数后舍入；reasoning 是独立 output 类计数，不能漏计或当 output 子集。累计计数各自增量化，单条缓存 delta 可以大于 input delta，可分解性在分组后判定。`cursor_range_sum` 对查询范围内的可定价事件 numerator 求和后统一舍入，缓存读/写独立于 input。`event_cost` 保留本机已有事件金额。reported charge 与 API 等价成本分别保存。

Cursor Dashboard 的本机表只保留当前账期，因此 HomeID 同时包含账期来源分区。旧分区留作已观测历史，下一账期不会把旧账期当删除。无法关联 Session 的 Dashboard 事件使用 `session_kind=unassigned_usage`，内部来源键不冒充原始 Session ID。没有模型/时间/完整计数的数据保持 unknown/unpriced/partial。

Invocation 仅包含工具/技能名、真实时间、结果与允许的 duration，不含参数或输出。其稳定身份按 Provider、Session、类型、名称、时间和同事实序号确定。

## 配额与元数据

原始账号 ID、邮箱、套餐、项目名、会话标题、原始 Session ID、reset、真实窗口与结构化历史允许上报。AccountBinding 只关联已确认账号的本地 scope，不绑定整个 Home 的历史用量。原始 JSONL、正文、思考、工具内容、auth.json、Agent 凭据、原始请求响应和完整本机路径不在协议内。

设备覆盖与真实采集截至时间独立于接收时间；没有事实时保留 NULL。Version 是 Helper 版本。在线心跳不代表全历史完整。

## 预算

每请求最多 8 MiB；最多 32 个 Session 快照、32 个账号/关联、1,000 个配额观测、100 个 Reset Credits 记录、3 个 Provider 状态。每快照最多 20,000 条用量与调用事实之和。时间限制在 JavaScript 安全整数范围，Token/金额使用十进制字符串保留 int64 精度和 NULL/零差异。未知字段、重复字段与非法枚举由接收端拒绝，错误不回显内容。

本协议首次发布前仍与实现同时演进，当前版本没有已发布的跨版本兼容承诺；正式发布后改动须设计升级与版本拒绝语义。
