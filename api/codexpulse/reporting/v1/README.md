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

## 中心接收与来源仲裁

采集客户端、Batch 标记、来源更新和中心投影在一个事务内提交。重复 batch ID 的同一语义正文返回原接收时间，不同正文返回 409。来源的同 revision、不同内容拒绝；过旧 revision 只做幂等确认，不覆盖当前事实。

中心重算 Contribution/Invocation ID，不信任客户端随意指定的贡献身份。每会话保留最多 128 个来源分区、合计 64 MiB 白名单快照；合并投影最多 200,000 条事实/64 MiB，超预算整批拒绝。跨设备锁定会话 owner，按固定 session key 顺序取锁，MySQL 来源读取使用当前 locking read；账号 owner 同样按全局账号键排序，避免交叉更新。

可比较的包含关系选择覆盖更完整的事实集合；相同贡献不会相加。不可比较来源保留已接受集合并标记冲突。部分修订不能擦掉已接受事实；已接受来源的完整新修订可以纠正/移除自身贡献，建立中心 correction fence，陈旧副本不会恢复旧贡献。之后其他来源的差异持续显示冲突，直到事实一致；不会从更大的数量猜正确答案。被删除的所有来源将会话标记 deleted，历史证据不物理删除。

Cursor Dashboard 的同设备多个账期合并为历史集合，不与 cursor_local 再次相加；另一个设备复制账期只增加 provenance。原始 Session 无法确定时仍保持 unassigned_usage。

账号键来自 Provider + 原始 ID，邮箱不承担唯一关系。AccountBinding 只证明同设备/Provider/local scope 的关系；同 scope 不能改绑不同账号，其他设备同名 scope 不能关联本机历史。HMAC-only 观测在对应确认关系到达后可关联，原始时间不刷新；legacy_unassigned 不自动升级。传入的 AccountID 必须与该 scope 的已确认关系一致，整个 Home 的 Session/Token 没有账号字段。

used_percent 在两种数据库中统一保存到小数点后 6 位，MySQL 使用 DECIMAL(9,6)，SQLite 入库前同样规范化；它仍表达来源 used 语义，不是多机合计。其他精确 Token/微美元整数不舍弃精度。
