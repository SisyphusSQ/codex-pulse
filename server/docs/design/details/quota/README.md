# 中心配额与节奏

目标是汇总多机观测并保留身份、周期和可信边界，而非叠加三台机器的额度百分比。网络白名单事实由 reporting 域持久化，quota 域只读并重建周期/当前投影；没有额外的周期真相表或自动 schema 写入。

`quota_repo` 参数化查询，所有表读共享 Engine.ReadSnapshot；`quota_srv` 负责已验证管理员权限、身份分组、来源仲裁和 VO；`quota_controller` 负责严格参数解析和统一响应。DTO/VO 各自进入 quota 业务子包，没有新 DO，因为查询复用既有 reporting 表。

共享边界是根模块 `store.ComputeQuotaWindow`、`DefaultQuotaArbitrationRule`、reset 等价与退役 limit 判断。这些是纯结构化计算，不读 SQLite/JSONL、网络或凭据，不装配本机 App、Helper、Fx 或采集器。中心没有复制 reset/provisional/异常隔离算法，使用现行 arbiter 规则；不同账号/不可比窗口分开，未知 scope 不跨采集设备合并。

原始时间与数据来源独立于接收时间。只接受真实观测参与计算，不使用 Home、设备在线或接收时间替代采集 freshness。关联历史分开进入曲线，不被提升为当前账号证据；退出期间缺口保留 observed_only。相同时刻的不同设备数值矛盾明确显示 conflict；没有依据从冲突推可信倒计时。

周期是从已观测 reset 重新计算的关系，按版本化规则归一漂移，固定锚点不允许相邻链无限扩张。原始 reset、当前选中 reset、中央 canonical reset 分别保留；不使用各机器自己的 generation。源别或窗口时长不兼容时仍分开。

接口字段、身份和失败语义见 [quota API](../../../../api/quotas.md)。前端展示中心结果，不使用客户端公式推测缺失预测。

节奏入口已实现为 `quota.ComputePaceWindow`：只调用现有计算，不创建 Client/Service runtime、不访问数据库/网络。中心 adapter 使用已通过仲裁的中央 canonical reset 和已验证账号键，关联历史单独传入；原始 local generation 不参与。两种数据库仍复用同一个 quota_repo 只读快照，无新增持久表。

中心显式转换为 quota VO，排除本机 scope/generation/内部 Binding；当前曲线只保留实际采样时刻。前端不用重复计算耗尽、偏差或基线。预测在 512 个唯一时间点内执行两两组合；超过预算时完整曲线仍保留，返回 evidence_budget。这个资源限制也保护使用同一纯算法的本机调用。

节奏聚焦验证：server 的 `go test ./internal/service/quota_srv ./internal/http ./internal/architecture ./app/cmd`；根模块 `go test ./internal/codex/quota -run '^(TestComputePace|TestBuildPace|TestForecastPace|TestPace)' -count=1`。中心四点同范围输入对账到现有耗尽计算，linked history 能提供历史基线但不能改变预测，曲线不添加现在采样；稀疏/下降/陈旧/冲突、密集数据预算及匿名/collector/参数拒绝均已验证。

开发验证：server 的 `go test ./internal/service/quota_srv ./internal/http ./internal/architecture ./app/cmd`；根模块 `go test ./internal/store -run '^TestQuotaArbiter' -count=1`。覆盖三机副本不累加、同邮箱不同 raw ID、下降、迟到、reset 抖动/换代、过期 LKG、legacy 不刷新、未关联/空库、Credits 去重/到期/失败，以及 HTTP 权限和参数拒绝。SQLite 开发通过不代表真实 MySQL 或三机验收。
