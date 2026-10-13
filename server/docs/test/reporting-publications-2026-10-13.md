# TOO-582 分片续传与额度上报验收（2026-10-13）

## 交付

Server 改为持久接收分片、后台有界构建、短事务原子发布。Helper 优先发送当前额度/Credits，并按批次持久化重试与保留容量；原 batch、revision、正文和配对均保留。私有修复构建已部署到中心及当前 Mac，尚未 commit/push 或公开发版，Linear 按用户要求保持 In Progress。

Server 二进制 SHA-256：`bff654b42ec50f237c48d1ea3dfd7ebb13ea353464e772f4d8e42bab529999fc`。

本机已安装 Helper SHA-256（本地 ad-hoc 完整性签名后）：`ced322efd9a33a0fbbe74376f106b2b3cb0c739ab996873a0142ae93fbda4027`。

基底提交 `bf30d1f`，改动仍在 `suqing/too-582-chunk-publication` 工作树；私有 source-manifest 记录完整文件摘要，不把私有构建版本当成 Git tag。

## PASS

- 根 reporting / reporting v1 与中心 reporting、schema、statistics、HTTP 聚焦测试；受影响范围 race、vet 通过。
- Web 类型检查与构建、原生 Swift release 构建通过；未把构建当成全部 UI 验收。
- SQLite v5→v6 迁移保留旧分片与 receipt，重入成功；未知结构拒绝。生产 MySQL 兼容协议环境已完成 v5→v6 追加迁移，DDL 摘要读回一致。
- 最终构建在独立 MySQL 兼容协议测试库通过：21,621→21,625 条的 22 片增长；180,000 条 / 65,880,411 字节快照（约 62.8 MiB），单片接收最高 0.327 秒，精确 Token 合计 180,099；随后纠正为一条记录成功。同修订不同内容返回 409，最终 receipt 重放一致。
- 中断后台构建后旧投影不变，重建处理器可续建；损坏摘要保留旧值并失败；新版本只在完整构建后可见。当前统计与来源筛选均读取正确发布版本。
- 持续 pending Session 不阻塞新额度及另一 Session，原正文/revision 不变；旧普通队列已满时当前事实仍可使用独立 reserve。
- 真实积压 revision 747 的原 batch 已获得最终 receipt；21,625 条贡献已发布，发布快照 SHA-256 与原分片清单完全一致，贡献条数及已知 Token 小计与 usage 投影对账一致。未清队列、重新配对或重新采集历史。
- 当前账号额度新采样已到达中心：核对时已用 52%、剩余 48%；采集时刻与中心接收时刻分别保留。
- 本机 App/Helper 版本、父子关系、显式真实 Home、原 mode 0700 runtime、preferences 物理身份与 reporting schema v4 读回一致。核对时 outbox=0；其余来源 partial 状态继续保留，不宣称所有 Provider 完整验收。

## 部署故障与修复

首次切换时 launchctl bootstrap 返回 I/O 错误。系统日志证明 bootout 尚未完成，bootstrap 命中 `Operation already in progress`；原脚本未等旧服务退出。先用同配置修复二进制恢复可用性，随后确认临时进程退出并恢复原 GUI LaunchAgent，最终 state=running、PID、ready 与二进制摘要已读回，临时与隔离测试进程已停止。

macos-service.py 已改为等待 job 与原 PID 均退出，30 秒未完成则阻止部署；三个 Python 回归测试覆盖 job 消失但进程仍在、已卸载服务和退出超时。未修改 SSH、VPN、权限或另建永久守护进程。

## NOT_RUN / 边界

- 全仓长测、完整 CI、独立 MySQL 8.4 验收、完整浏览器/原生全部页面 E2E。
- 三机客户端统一升级、正式 Developer ID 签名、公证、GitHub 发布及提交推送。
- 数据库备份已成功，恢复演练未运行。SQLite/偏好/旧 App、中心一致性 dump 与回执均保留在私有证据目录；原始日志、凭据、个人标识与真实路径不进入本摘要。

可复用入口：[分片发布 runbook](../../../docs/test/reporting-publications.md)。
