# 分片续传与后台发布验证

## TOO-677 周期边界与显式恢复（2026-10-31）

账号周期消耗只采用观测时间已到达推算周期起点的额度；若 reset 减七天仍晚于观测，等待后续有效观测，不重写采集时间、不制造周期消耗，也不阻塞额度和其他事实。协议与数据库版本保持不变。

开发阶段 `go test ./internal/reporting ./api/codexpulse/reporting/v1 -count=1 -timeout=120s` 通过。新增回归覆盖起点晚 3106ms、仅等待时间不会使旧观测有效、边界及边界后观测正常入队、账号隔离，以及已持久化协议拒绝的显式重试、再次拒绝暂停、配对和不可变正文保留。

升级旧的 `protocol_rejected` 安装后，使用设置中的“立即同步”重试；不需要重新配对或清空队列。仍失败时继续保留错误和队列，先修复应用或中心。`FullSyncReporting` 不绕过协议拒绝，应先恢复普通同步。提交与发版收尾按约定不重复执行测试；本节的合成回归不代替真实 Home 或三机验收。

归属 TOO-582；本文件是可复用步骤，不声明全部步骤已通过。

## 聚焦入口

- 根模块：`go test ./internal/reporting ./api/codexpulse/reporting/v1`。
- server 模块：`go test ./internal/service/reporting_srv ./internal/repository/mysql/schema_repo ./internal/service/statistics_srv ./internal/http`。
- 并发：受影响 reporting 包的 `-race`；不运行全仓长测。
- Web：`make -C server web-build`；App 仅需受影响 Swift 构建/测试。

## 核心矩阵

1. 保留旧完整版本，按逆序与重复请求提交至少 22 片。收齐后先返回 processing，后台发布前统计仍为旧值；最终 receipt 成功后一次切换全部用量、metadata 与 capsule。
2. 在 usage 分批构建中取消 worker，重建处理器后续建。原 batch/revision、已保存分片和摘要保持不变，不重复计数、不提前 receipt。
3. 修改分片造成摘要错误、元数据冲突或稳定 ID 错误，必须保留旧投影并显示失败。多机副本、价格补齐、完整纠正与部分数据保护沿用仲裁回归。
4. 让一个 Session 持续返回 pending/500，同时更新账号额度并发送另一 Session。额度先到，其他 Session 继续，失败批次原文保留；再次启动不能丢失重试记录。
5. 模拟升级前已满普通队列，验证当前事实仍可进入 reserve；普通新入队受限，旧批次不删除。pending 队列存在时全量任务不得完成。
6. 从已登记 v5 升级 v6，保存并比对旧分片、来源与 receipt；再次启动幂等。未知摘要/结构漂移拒绝启动。
7. 版本回收不删除发布指针/来源/canonical 引用的 payload 与 usage；重启或过期租约重算不得覆盖更新来源。

## 真实链路

生产操作前备份数据库、偏好、配对和不可变队列摘要。先部署中心并读回 schema/ready/二进制摘要，再以真实 Codex Home 和 mode 0700 私有 runtime 启动修复 Helper。检查原积压批次 receipt、来源 revision、发布指针、精确 Token 合计及中心额度采样时间；不能仅以 outbox=0 或 HTTP 200 判定成功。统计缓存、浏览器页面与源采样时间分别记录。

MySQL 兼容协议实机、独立 MySQL 8.4、SQLite、CI、真实 UI 和发布各自记录 PASS/FAIL/NOT_RUN，不能互相替代。私有原始证据在 `.artifacts/too-582/`，不提交个人路径、邮箱、原始事件或凭据。
