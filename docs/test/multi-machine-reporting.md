# 多机中心开发验证与后续验收

总体方案：[多机汇总与 Web](../design/details/multi-machine-reporting/README.md)。开发证据来自隔离 SQLite 与 synthetic/empty Home，不能替代三机真实 Home 或 MySQL 验收。

## 聚焦开发入口

```sh
# 仓库根目录：本机同步与字段契约
 go test ./internal/reporting ./internal/core ./internal/helper ./api/codexpulse/core/v1 ./api/codexpulse/reporting/v1
 go test ./internal/store -run '^TestReporting' -count=1
 go test ./internal/app -run '^TestOptionalReporting' -count=1
 swift build --package-path app/macos --target CodexPulseCoreClient

# server/：结构、鉴权、HTTP 运行装配
 go test ./config ./internal/lib/gorm ./internal/repository ./internal/service ./internal/http ./internal/controller ./app/cmd
```

开发中按受影响范围选择命令，提交收尾不重复执行测试。本地不主动运行全仓 race/verify 长测。

## 已取得的隔离证据（2026-10-01）

- Pass：SQLite 显式初始化、重启读回、结构/版本拒绝、私有权限、事务回滚、批次唯一约束与资源关闭；Server 已构建。
- Pass：一次性用途固定的设备码、并发单次消费、过期/重放/撤销/权限拒绝、CSRF/Origin、HTTP 与实际 httptest TLS Cookie/证书语义。
- Pass：本机持久队列的匹配确认、确认丢失原 body 重试、进程重启、单调 revision、容量失败不推进 checkpoint、不同中心队列隔离及显式清理；损坏/未知字段的队列拒绝发送。
- Pass：启停取消在途上传、退出 join worker、撤销后暂停、Home fence 拒绝混合、首次补传范围不隐式删除中心历史；空 synthetic Home 的 App 开始时上报默认关闭并随 App 关闭。
- Pass：Codex reasoning/缓存增量/价格证据、索引重建稳定身份、工具统计白名单、路径不出 payload；Cursor 重复次数、缓存读写、reported/estimated 区分、未关联会话及账期切换 fence。
- Pass：CoreService RPC 白名单与生成 Swift protocol、CoreClient 编译；原生同步配置 UI 尚由设备配置执行卡继续实现。

- Pass：显式项目关联/解除、来源名称更新保留管理关系、中心事务接收、HTTP/实际 TLS 上报、相同请求原确认、batch/来源 revision 冲突、三来源复制/并发/增长/价格修订、部分修订保留、完整纠正与陈旧副本、来源 tombstone、整批回滚、Cursor 跨账期/复制、账号 scope 隔离/晚到确认/legacy 不提升、Credits 和 used 小数精度；网络身份/未知及重复字段/预算/撤销/自身进度权限。

以上为开发场景通过，不表示整个产品或 Master 已验收。后续各功能增量需补充其受影响验证。

## 本机恢复观察

同步库位于应用私有 runtime 的 `reporting.db`，父目录 0700、数据库/WAL/SHM 0600。它包含 Pulse 自有设备凭证、安装级分区盐、白名单持久队列、来源 revision 与确认进度；不与 App 主库或中心库共享 schema。不得将其上传到 Issue 或提交 Git。

`ReportingStatus / PairReporting / ConfigureReporting / SyncReportingNow` 是本机 UDS RPC。配对后仍关闭同步，需要明确启用。默认间隔 60 秒，可设置 15–3,600 秒；追赶队列/分页时按 1 秒调度，单轮最多发送 32 批、每 Provider 导出 16 个会话，每轮整体 60 秒。暂时失败有界退避至 5 分钟；撤销/协议拒绝停止自动重试。队列最多 2,048 批/256 MiB，超大单会话或磁盘失败保留有限失败状态。

停用取消在途工作并保留队列。退出 App 关闭同一个 Helper 所有者，下次启动从持久分页游标和确认进度继续；无独立守护进程。改变中心获得新的客户端凭证，旧中心队列保留且不会发往新中心。首次补传起点在开始导出后固定，重新配对可选择新范围；清理待发送队列是显式本机动作，不删除中心数据。

HTTP 必须在 App 显式允许，连接目标仅限环回/LAN/Tailscale 地址；域名每次建连验证全部解析结果，并直接连接已验证 IP。HTTPS 使用标准证书校验。所有重定向被拒绝，不使用代理或自动降级。真实中心地址与配对码不要放进测试证据。

## Master 待验收入口

- Not Run：三台机器真实 Home 分别配置、读回设备与 Home、初次补传/退出重启/断网恢复，以及实际 Web 页面闭环。
- Not Run：真实 MySQL 连接、整体 E2E、并发/事务/字符集/时区/备份恢复；环境由用户后续提供。
- Not Run：生产部署、签名、公证和正式发布。

实际运行前说明将读取 Session/JSONL、写入各机私有 runtime、主 SQLite/偏好及 App Server housekeeping；保持真实 CODEX_HOME 的物理身份读回，三台机器分别取证。完整日志、凭据、正文和本机路径只留受保护且忽略的本机 artifacts，提交摘要使用更窄白名单。
