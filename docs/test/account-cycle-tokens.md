# TOO-529 账号当前周期 Token 验证与交付

设计见 [当前账号额度周期已记录 Token](../design/details/account-cycle-tokens.md)。所有开发测试使用 synthetic fixture / 私有临时数据库，不读取真实凭据、会话或业务库。

2026-10-05 开发阶段通过：

- reporting contract：稳定 ID、重复 ordinal、计数溢出、不一致总量、周期边界、未知 scope 拒绝和空能力标记。
- Helper reporting：旧会话跨切换第一条 delta 丢弃、A→B→A、重启不补历史、新会话首条、不可变 outbox 重启重试与页面幂等；近期游标仅在整页进入持久队列后推进。
- Store 的受影响 Reporting 场景（含近期会话过滤与完整 ordinal 身份）：活动代 Home fence、已有事实身份、历史页和当前额度优先页保持。
- 中心 quota/reporting/schema 包：三机同事实去重、不同事实相加、来源筛选、账号隔离、换账号同事实冲突及事务回滚、旧周期排除、旧客户端 null 与新客户端 0、超过 int64 总量精度，v4→v5 追加升级及既有行保留、重入和漂移拒绝。
- Web Quota.test.tsx 16 场景、typecheck 及 AntD 用法检查（0 问题）：账号页本周期数值、万/亿格式、0/—，保留已有账号、额度、Credits、刷新和筛选行为。jsdom 既有伪元素 getComputedStyle 提示保留，不影响场景通过。

本轮初次测试中的重复事实数量断言、标签精确匹配、来源筛选 fixture 和旧 schema 版本断言已修正后通过；未把这些失败隐藏成首轮全部通过。

开发命令：`go test ./api/codexpulse/reporting/v1 ./internal/reporting ./internal/store -run 'AccountToken|AccountUsage|Reporting' -count=1`；Server 的 quota_srv/reporting_srv/schema_repo 聚焦包；Web `npm test -- src/pages/Quota.test.tsx`、`npm run typecheck`。

提交与发版收尾按约定不重复执行测试、lint、check、verify 或 smoke。不运行全仓长测。GitHub Actions 已在仓库设置关闭并移除 CI Workflow。

2026-10-05 发布与部署已完成：

- 功能 PR [#194](https://github.com/SisyphusSQ/codex-pulse/pull/194) 已合并。发行源码固定为 `1d5d5f2a9560565b745a40c27667418a7be71595`，signed annotated tag `v0.15.3` 的远端 peeled commit 与其一致。
- [GitHub stable Release v0.15.3](https://github.com/SisyphusSQ/codex-pulse/releases/tag/v0.15.3) 为公开、非 Draft、非 prerelease；DMG、ZIP、Server 包、两份校验清单与两份说明共七个资产已读回。SQMC05 从公开发行重新下载三种产物并核对 SHA-256，均与发行清单一致。
- 公开 DMG 完整性、只读挂载内容、App `0.15.3 / build 66` 和 ad-hoc codesign 完整性通过。Gatekeeper 返回拒绝，stapler 无公证票据；Release Notes 已说明未公证与首次打开方式，不将该版本描述为已公证发行。
- Sparkle 使用保留的 Ed25519 key 签名，公开 `updates/appcast.xml` 与已签名候选字节一致，最新条目为 `0.15.3 / build 66`，ZIP URL、长度与公开产物一致；固定公开 URL 另行下载读回一致。
- SQMC04 生产 Server 已先行部署 `v0.15.3-1d5d5f2`。停服后的数据库备份保留，schema v4→v5 已完成，三个账号 Token 表均存在；部署时 LaunchAgent 为 running、ready 返回 200，二进制 SHA-256 与发行 manifest 相同，旧发行和私有配置保留。
- SQMC03、SQMC04、SQMC05 客户端均已安装 `0.15.3 / build 66`。三台 App/Helper 父子进程正常，内嵌 Helper SHA-256 一致；分别读回真实 Home 物理身份、显式 App/Helper 环境与参数及私有 0700 runtime，已有偏好与 Home 绑定未改变。旧 App bundle 保留。
- 部署前两台远端 SSH 曾超时。当次目标路由均属于 Tailscale、系统与 VPN 会话 DNS 一致；默认发现探测有回应，但 TSMP/SSH 失败。用户授权后重建 SQMC03 Tailscale 会话，系统按需自动重新连接并更换网络扩展进程，SSH 恢复，所检查的 Tailscale 偏好与 DNS 未改变。未操作飞连；没有将恢复结果解释为飞连根因或长期稳定性证明。
- GitHub Actions 的仓库设置仍为 `enabled=false`，CI Workflow 已删除。提交与发版收尾未重复执行测试。

脱敏发行校验：ZIP `29b8f4f71cef74ad6530a0d2b6d0bc897fefbb9d909ee35dcf150b5a4e03d384`；DMG `10208147cae350417155d43811ed8213952be9bbcec20f5a0c8f9705a87ce035`；Server 包 `6d4c15e2a12ab097f8a321f2b8055f35bf2d4078e12d32723c34c0a71b6ce16e`。原始交付回执、备份和日志仅保存于各机忽略的 `.artifacts/too-529/`。

上述为发行完整性及部署运行读回。本文件不宣称真实账号切换 E2E、Sparkle 自动更新 E2E、独立 MySQL 8.4 验收、长期网络稳定性或 macOS 公证已完成。

## TOO-551 中心大批次入库与恢复验收

账号 Token 事实按全局事实 ID 排序，每 1,000 条批量插入并以锁定读核对既有值，来源关系同样分块保存。同一批次的账号绑定只解析一次，同一周期只写入最大真实采集时间。仍在原接收事务内处理：冲突、数据库失败或取消整批回滚；重复 batch 返回原确认，跨设备副本不重复累计。协议、表结构、请求期限和既有不可变 outbox 不变。

开发聚焦入口（`server/`）：

```sh
go test ./internal/service/reporting_srv ./internal/service/quota_srv ./internal/architecture ./internal/http
```

新增回归覆盖 5,132 条事实 / 15 个账号用量分组，在每次创建/读取注入 5 ms 延迟、请求预算 10 秒的场景中，创建/读取操作为 27 次；同时核对原确认重放、跨设备来源、最后一块事实冲突回滚、第二块来源写入失败/取消回滚、周期时间不倒退和绑定缓存不跨账号复用。夹具只使用 synthetic 数据与临时 SQLite，不能替代真实 MySQL 与积压恢复证据。

真实恢复验收：部署前只读保留待传 batch ID、正文 SHA-256、大小与最后成功时间；部署后核对服务版本、二进制 SHA-256、监听及 ready，再观察原 batch 被中心确认和本地 outbox 清空，最后等待下一次自动同步的成功时间与中心采集时间推进。不清队列、不重建凭据、不手动改写批次。ready 成功不等于积压已恢复；原始证据只留忽略且私有的 `.artifacts/too-551/`。

2026-10-10 首次中心修复结果（尚未完成 Session 用量对账）：

- Pass：reporting_srv、quota_srv、HTTP 包聚焦测试，新增大批次与失败语义回归通过；构建通过。架构包 Fail：主分支已有 `reporting_do/observation_digest.go` 辅助文件被检查器要求包含 `TableName()`，该文件本次未改动。没有运行全仓长测或 CI。
- Pass：由最新 main `dbf232e` 切出 `suqing/too-551-center-token-batch`，SQMC04 正式中心部署 `too-551-dbf232e-7d4234b0`，运行进程、ready 200、版本路径和二进制 SHA-256 与本机产物一致。该构建包含未提交修复，源码文件摘要和补丁另存本机证据；未创建公开发行或标签。旧版本与原私有配置保留，表结构和请求期限未变。
- 安装脚本首次 LaunchAgent bootstrap 返回错误 5；回读确认新文件/plist 完整且服务未加载，再使用既有 start 入口成功启动。未重复安装或覆盖发行目录。
- Pass：SQMC05 原 4 个待传批次（队首 5,132 条 Token 事实）均有中心持久化确认，中心批次摘要与部署前原正文 SHA-256 全部一致；10:59 自动重试后本地 outbox 清空，成功时间推进到 10:59:54，Codex 采集时间推进到 10:59:48、同步状态 ready。
- Pass：后续 11:04 自动同步成功，成功时间进一步推进到 11:04:55，中心 Codex 采集时间推进到 11:04:48、同步状态仍为 ready；中心接收批次数由 7,301 经 7,309 增至 7,314，本地 outbox 仍为 0。App/Helper 原进程保持运行。
- 现存边界：Grok/DSH 自动探测来源仍为 source_unavailable，导致客户端总体状态及旧全量同步任务未显示完全就绪；Codex 上传成功和原队列清空已独立确认。该阶段未修改其他 Provider 配置或客户端逻辑，不能据此宣称 Session 用量对账、旧全量同步任务或全 Provider 验收完成。Linear TOO-551 保持用户指定的 In Progress。

### TOO-551 Session 用量补充排查

用户反馈机器表仍为零后，补做同日 Session 用量对账。原 4 批账号事实已被确认，但不能据此宣称 Session 历史追赶完成：全量任务中 Grok/DSH 来源不可用会使全局重试退避到 5 分钟，即使 Codex 有下一页，仍每 5 分钟只导出 16 个 Session。机器状态仅按采集时间判断新鲜，不能作为事实完整性的证据。

Helper 调度改为：有可继续的分页且仅遇到来源不可用时，1 秒后继续追赶；真实传输、协议、存储、队列容量和事实预算失败继续退避；可用页耗尽后，不可用来源仍保留错误与全量未完成状态，不持续快速空轮询。

开发验证 `go test ./internal/reporting -count=1` 通过（16.306 秒）：覆盖来源不可用且重试已到 5 分钟时正常分页仍继续、页耗尽后恢复退避、全量任务不虚假完成，以及真实失败不能进入快速重试。SQMC05 私有 Helper 修复版本为 `0.16.1+too.551`；旧 App bundle 保留，使用原真实 Home、0700 runtime、配对和持久队列。没有创建公开发行、标签或提交。

后续验收须同时核对本机活动代的同日事实、中心 Session 来源与网页 source-usage；覆盖已扫描旧会话在今天新增事实的情况，并观察至少一次后续自动扫描及确认。不能只检查首次追赶后的最新 Session 数量，也不能用账号周期 Token 替代机器 Session Token。

2026-10-10 追加部署与验收结果：

- Pass：SQMC05 与 SQMC04 App 托管 Helper 私有修复部署为 `0.16.1+too.551`，两机旧 App 均保留；分别读回 App/Helper 环境、真实 Home inode 以及 0700 runtime。无独立后台采集进程，配对及既有 outbox 未清除。
- Pass：SQMC05 后续自动扫描于 11:41–11:42 完成，旧会话今天新增的事实补齐。固定对账范围为 Asia/Shanghai 当日 00:00 至 11:30，3 个会话的 2,375 条事实与中心计数完全一致：输入 391,273,770、缓存 376,020,992、输出 1,120,131、推理 348,479；130 个 Session 的规范化快照摘要逐一匹配已确认检查点，本地队列为 0。该固定范围避免把持续新增事实的采样时间差误判为丢数。
- Pass：原浏览器机器表读回 SQMC05 今日约 4 亿 Token、4 个活跃会话、采集截至 11:42、状态正常；中心同范围完整数值为 400,255,674 Token。全天会话数高于固定对账范围，源于 11:30 之后新增的活动事实。
- Pass：SQMC04 同一调度问题一并修复，378 个待扫描 Session 已追赶；586 个 Session 摘要逐一匹配中心确认，本地队列为 0，同日 11:30 前的 29 条事实精确一致；网页显示约 290.5 万 Token、1 个活跃会话。
- 边界：SQMC04 采集截至仍为 11:08、网页保留“采集陈旧”；未将用量补齐描述为其采集新鲜度已恢复。Grok/DSH 来源不可用提示及总体全量任务 running 状态仍保留，不宣称全 Provider 就绪。聚焦开发测试通过；既有架构检查失败仍保留。本轮代码尚未提交，TOO-551 保持 In Progress。
