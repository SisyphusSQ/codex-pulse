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

### 账号与配额导出

Codex 原始账号 ID 仅从既有 `account/rateLimits/read` 的 typed 结果取得，经过原有前后账号一致、Home/generation fence 后记住 scope 与 ID 的关系；邮箱/套餐沿用同一已确认账号资料。上报不会另外查询配额，也不解析 auth.json/JWT。关系保存在私有 `reporting.db`，不进入 Swift、偏好或日志。账号 A 切至 B 时保留 A 的已确认关系和不可变旧队列，历史逐条按原 scope 关联；Session/Token 不被当前账号接管。

Cursor/Grok 当前受支持配额链路没有可验证的原始账号关系，保持设备来源下的待关联观测；不从邮箱或认证文件推断身份。Grok 旧库只有当前快照时导出其真实观测，与历史使用相同事实键，不补造关闭期间历史。

`observed_at_ms` 保留原始观测时间。Codex 合并样本只导出实际保存的首末端点；Cursor/Grok 保留实际 `window_start_at_ms`、时长和 reset，generation 只用于本机分页。`linked_history` 限于 Codex 原始 default 历史；`association_scope` 指向本机已经确认的 scope，中心验证同一 collector 的关系。解除显式关联更新归属，保持原事实、观测时间与首次 receipt；linked history 不升级为当前额度或 freshness。

Reset Credits 上传库存、详情状态、按到期时间汇总的 `expiry_schedule` 和 `next_expires_at_ms`；不含 raw credit ID、请求 ID、响应或 source file。到期时间与 `next_reset_at_ms` 分开，没有已知 reset 时保持 NULL；完整详情的可用数量必须与库存对账。

账号资料与事实各自计算 checkpoint，整页变更在同一事务中打包入队。分区包含 Home、已确认关系和显式历史关联 revision；前后分区不一致则不推进游标。同步库 schema 2 显式升级 schema 1，保留盐、revision、游标、凭证及原队列。

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

## 生命周期 TPS capsule

Codex light_index 快照可选 throughput version 1 / basis closed_turn_lifetime_output。整体输出与活跃并集来自完整生命周期；最近最多 50 轮只供展示，不决定整体均值。只传哈希轮次键，不传 raw Turn ID/offset/generation/内容。输出不能大于本来源安全贡献输出之和，最多 50,000 轮，历史裁剪仅返回 history_filtered 未知状态。TPS-only 变化产生新 revision 与持久批次；tombstone 不带指标。

严格验证 nullable/零、有限原因、预算；未来 capsule version 426。保存在既有 source/canonical payload，中心复用 Go 计算及仲裁。先升级 Server 后升级 App，旧 Server 未知字段 400 会暂停并保留队列；旧客户端无 capsule 显示未上报。
