# 模型价目、订阅设置与用量成本 v1

所有接口要求已配对的admin浏览器会话，Cookie/Origin/CSRF沿用统一中间件；collector不能访问这些路由。目录不访问Agent凭据或在线抓取网页。

## 公开参考目录

`GET /api/v1/catalog` 不接受query参数，返回 `models/plans/version`。模型包括计费平台、模型名、模式、币种、单位、input/cached/cache_write/output价格、版本、来源、核对日和可选历史生效边界。价格是nullable精确十进制字符串，未知不是0。核对日用UTC日界编码、UI按日期显示。`evidence=current/historical/observed` 区分公開参考、本机计算快照与已观测未匹配模型。最新公开目录与本机历史目录不同，目录更新不改写上报费率。可观测模型最多2000个，超预算413。

套餐参考包含价格、币种、计费周期、地区、包含额度描述、reset规则、来源和核对日。未知或合同价格为NULL，不把套餐费用推算成固定Token额度。Credits费率与USD API价格分开。

## 账号订阅补充

`GET /api/v1/accounts/:id/subscription` 返回确认账号的中心手动设置及自动/手动/最终套餐，`revision` 为十进制字符串，首次无记录为0。确认账号键使用小写64位十六进制，不以邮箱归并。不存在账号404，非法键400。

`POST` 相同路径为完整替换，必须携带 `expected_revision` 字符串，以及 nullable `alias/manual_plan/renewal_day/membership_date`、`date_kind` 和 `time_zone`。允许的date_kind：空（不设日期）、monthly_renewal（1至31，完整日期NULL）、membership_expiry（完整YYYY-MM-DD，续费日NULL）。时区为有效IANA，备注最多128字符，套餐最多64字符。未知字段/重复字段/错误类型400，正文上限8KiB。缺失/错误CSRF或Origin403；CAS冲突409，成功返回持久结果，修订号增加1。清除字段保留修订记录，不删除设备事实。MySQL首次插入使用唯一约束，不能以ON DUPLICATE KEY无操作和ClientFoundRows判断成功。

服务端复用本机civil-date规则：短月按月末、每月续费经过后滚动下月；完整到期日经过后needs_update。next_date/day_delta/date_state为服务端派生。订阅日期与额度reset、Credits到期独立。设置保存在pulse_account_settings，不接受采集上报写入，不回传Mac，不执行续费扣款。

## 独立用量查询

`GET /api/v1/statistics/usage` 沿用统计日期、时区、Provider、来源、模型/项目/搜索筛选及权限和事实预算，返回range/scope/totals/coverage/models/model_days/trend/providers/cursor_pools及模型/趋势舍入差额。模型日桶与范围总量在同一只读快照计算，最多20000个模型日桶，超限413而非截断。

费用复用pricing.CalculateExact和现有各Provider聚合：历史费率和版本随事实，NULL与0保留；Codex缓存是input子集且reasoning独立，Cursor缓存写入/读取分别计价，Grok完整reported cost独立。reported_charge_status=known/partial/unknown说明来源费用完整性；partial展示已知小计，不与估算相加。Cursor用量池按事件时刻的版本化分类派生，未知路由模型保留未归类。微美元级舍入差额独立返回，不创造Token事实。

用量不接受account_key筛选，不将Home历史挂到当前账号；日期是事实发生时间，来源筛选仍读取自身快照。新HTTP查询不扫描JSONL、不启动本机采集、不改变TPS或缓存命中率生命周期口径。

## 数据库升级

结构v2在v1基础上只增加pulse_account_settings。HTTP启动只检查。先备份并停写，执行 `db upgrade`：只接受已核对v1摘要，创建新表、检查所有字段，最后CAS提交v2摘要。SQLite事务升级；MySQL DDL隐式提交，失败须检查实际结构后重入，不声称DDL事务回滚。当前MySQL实机执行Not Run。

目录包含已接受用量的历史费率投影，以 provider/model/version/四项费率去重；没有来源与核对时间的上报证据不生成官方来源或生效时间。已观测模型没有当前参考价时仍另列未知参考项。历史与观测投影在同一只读数据库快照获取，分别最多 2,000 组，超过返回 413。Web 默认展示美元价格，可切换 Codex Credits。
