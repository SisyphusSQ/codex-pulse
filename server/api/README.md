# 中心网络 contract v1

上报白名单类型与验证位于仓库 `api/codexpulse/reporting/v1/`。它独立于本机 core.proto；不得将本机 Shutdown、Home 切换、恢复或任意运行操作暴露到网络。

| 路由 | 客户端与权限 |
| --- | --- |
| POST /api/v1/pair | 匿名的一次性码交换入口；服务端固定用途、限速、4 KiB、严格 JSON |
| GET /api/v1/session、POST /api/v1/logout | 管理浏览器自己的入口绑定会话 |
| POST /api/v1/pairings | 管理员签发 admin/collector 用途的短期码 |
| GET /api/v1/clients、POST /api/v1/clients/:id/revoke | 管理员查看/撤销客户端；历史不因撤销删除 |
| POST /api/v1/projects/associate | 管理员显式关联/解除选定中心项目；普通来源更新保留管理关系 |
| POST /api/v1/batches、GET /api/v1/sync | 采集设备自己的原子批次与确认；事务提交后确认，重复 body 返回原 Receipt；进度只属于自身 |

所有请求/响应使用明确类型；JSON 不接受未知和重复字段。batch version=1，未知版本明确不兼容；身份来自已验证凭证，不接受 body 自称 deviceId/role。Token 与微美元用十进制字符串，NULL 与真实零区分，时间为 UTC 毫秒。

每批最多 32 个完整会话快照、32 个账号/关系、1000 条额度观测、100 条 Credits、3 条设备状态，总正文 8 MiB。单快照最多 20000 条贡献与调用统计之和；预算不足应显示覆盖/积压错误，不丢弃或伪装完整。快照 revision 在来源持久状态中单调递增，不复用数据库 generation。完整替换支持会话增长、修订和移除旧贡献。

贡献 ID 由 Provider、原始 Session ID、结构化观测时间、Token 事实及相同事实出现序号生成，不依赖设备、文件 offset、数据库 generation、模型或价格版本。多机副本保留 provenance；可比较的前缀/完整快照合并，不相加；无法解释的差异显示冲突并保留证据。模型/价格修订仍需事实比较和明确选择规则，不能根据大小任取总数。

账号关系只由本机同一 confirmed context 提供；邮箱不唯一，HMAC-only 历史保留待关联。Quota 不上传本地 window_generation，中心依据真实窗口/reset/时间建立周期；Reset Credits 不含 raw credit ID 或响应。传输凭据只在 Header/Cookie，不进入统计 DTO。批次只在事务提交后确认，重试相同批次返回原确认。

仲裁、历史定价证据、纠正与来源预算的具体规则见 [共享上报协议](../../api/codexpulse/reporting/v1/README.md)。接收层以已鉴权 collector 为来源 owner，浏览器管理凭证不能上报或读取 collector-only 的同步进度。账号/额度/credits 先存储白名单事实与确认关系，中央周期/节奏由对应服务计算。

## 管理端统计查询

统计 API 的字段和口径见 [统计查询](statistics.md)，由已授权浏览器 Cookie 访问：

- `GET /api/v1/statistics/summary`：范围 KPI、Provider/模型/执行归属、自然日趋势、365 日热力图、星期/小时及工具/技能分布。
- `GET /api/v1/sessions`、`GET /api/v1/sessions/:id`：服务端搜索/排序/分页、原始会话身份与采集来源、范围用量、趋势及工具统计。
- `GET /api/v1/projects`、`GET /api/v1/projects/:id`：显式项目关联组、成员、范围统计及会话分页。
- `GET /api/v1/devices/status`：采集设备最后接收、各 Provider 采集截至、覆盖边界、队列、版本和有限状态，不返回授权摘要。

所有查询均默认鉴权；collector Bearer 不能读取这些中心数据。

## 中心配额

`GET /api/v1/quotas` 返回账号、可信当前值、来源证据、中央周期和 Credits 到期投影。身份、漂移、过期与预算详见 [配额 API](quotas.md)。

`GET /api/v1/quotas/pace` 返回本周期、上一周期、历史基线与预测；复用本机纯 Go 计算，未观测数据及不可用预测保持明确原因。

## 授权管理与原生设置

管理员 POST /api/v1/clients/:id/rename 的 body 仅允许 name（trim 后 1–128 字符），只更新显示名，不改变用途或凭证。GET /clients 最多 1,000 条，超出返回 413，不静默截断。POST /api/v1/pairings/revoke 仅允许 body.code，撤销未消费码；码不进入 URL/日志，已消费返回 409，应在客户端列表核对并撤销对应授权。与所有浏览器写接口一样要求入口匹配和 CSRF，collector 无管理权限。无需新增表或迁移。

Web 配对码只在弹窗内存显示；关闭/撤销即时卸载，过期清空码，浏览器管理码签发前明确权限确认。关闭显示不等于撤销未用码。撤销客户端保留事实；当前浏览器撤销沿用 logout 清 Cookie/会话缓存。

原生设置经既有 ReportingStatus/PairReporting/ConfigureReporting/SyncReportingNow RPC 操作 Helper；独立保存同步设置，默认关闭，配对成功仍关闭。短暂码操作开始即清空，配对/写入无自动重试；在途旧状态读回不能覆盖新配对。Helper 保存自身凭证，Swift 无设备凭证/原始数据/SQLite 访问。历史起点开始后固定；清理当前中心队列明确确认并关闭同步，其他中心队列和中心历史保留。退出 App 关闭所有者，下次启动增量补采。

## 中心版本

管理浏览器 GET `/api/v1/version` 返回 version、commit、built_at、reporting_protocol、throughput_capsule 和 schema。需要管理员授权，collector 不可读取；不返回环境、数据库、路径或仓库 URL。正常启动自动初始化/升级已登记结构并读回，HTTP 在成功之后监听；备份/恢复通过受控 db CLI，详见 [运行说明](../docs/test/operations.md)。

- [模型价目、订阅设置与用量成本](catalog-subscriptions.md)
