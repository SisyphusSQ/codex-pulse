# Cursor Dashboard 用量对账验证（2026-10-10）

关联：TOO-569。范围：完整 Dashboard 事实的修订仲裁、已接收来源重建、真实中心读回。

## 原因与修复

旧 Helper 使用本地会话 metadata 的覆盖率设置 Dashboard 的 `Complete`。
缺少本地会话记录时，即使 Dashboard 完整分页成功也会标记为部分来源。
Cursor 后续修正 Token 会改变贡献 ID；中心因此保留旧 canonical，导致大量少计。

Helper 按已原子提交的 Dashboard 账期事实设置完整性。中心兼容旧版本来源，
允许同一来源的权威修订替换旧事实，保留跨设备冲突与 correction fence。
历史对账命令复用实时接收的仲裁和投影路径，按会话事务处理，可预览和续跑。

## 开发验证

PASS：

- `go test ./internal/store -run TestReportingCursor -count=1`
- 在 `server/` 执行 `go test ./internal/service/reporting_srv ./app/cmd ./internal/service/statistics_srv`
- Web 类型检查及构建、中心安装包构建。
- 回归包含：旧 `Complete=false` Dashboard 非重叠贡献修订、多机副本去重、
  Token 减少修订、跨账期保留历史、其他部分来源的保护、预览不写、失败回滚、
  续跑分页、重复对账无变更、原始来源及回执不变、其他 Provider 不变。

NOT_RUN：全仓长测、完整 race、CI、公开发版、三机桌面 App 安装。
本轮中心兼容修复可直接处理现有桌面客户端，无须等待 App 升级。

## 真实中心对账

安装标识：`too-569-dbcfe0a-89c6c353`，包含未提交的当前分支改动。
中心二进制 SHA-256：`e8dbae5e746434b81c4e7f84bb7889fcc3641b1d11a6a18cdc0ca1c9697cb46a`。
正式中心短暂停止后，在私有目录保存 Cursor 来源和投影备份及摘要，再执行对账。

| 项目 | 结果 |
| --- | --- |
| 预览/执行扫描 | 1,465 个 Cursor 会话 |
| 实际修复 | 198 个会话 |
| 执行后再次预览 | 变更 0 |
| 原始来源 payload/digest/revision | 逐行比较不变 |
| 批次回执、项目关联、其他 Provider 用量 | 对账停机窗口内不变 |
| 独立来源事实核对 | 1,465 个会话，差异 0，身份冲突 0，canonical 冲突 0 |
| 恢复后的用量事实 | 1,789 条 |
| 恢复后的累计 Token | 1,294,664,726 |
| 当天 Token（Asia/Shanghai） | 282,719,256 |
| 关键故障会话 | revision 1 → 283；1 → 101 条事实；累计 584,047,199 Token |
| 生产运行 | 修复版本实际进程监听；HTTP 200；三台采集设备均有重启后的接收记录 |

上述数字是对账时快照，后续上报可继续增长。恢复后的累计和当天数字均独立从
canonical 所属设备的 Dashboard 账期事实核对，多机副本未相加。
原始来源、备份与完整操作日志仅保存在私有运行目录，不提交。

PASS：真实网页选择 Cursor + 今天，显示 2.8 亿 Token、API 等价成本 $390.26、
54 个活跃会话，页脚显示修复安装标识。截图仅保存在忽略的本机证据目录。
本机后续上报成功，Cursor 未确认 revision 为 0，发送队列为 0；
总体 `partial` 仍反映独立来源的覆盖状态，不等于 Cursor Dashboard 上报失败。

恢复后的新增修订也已进入正式投影：后续读回当天总量增长到 283,124,214 Token，
逐模型与来源独立汇总一致（grok-4.7-xhigh：279,685,581；
grok-bot-automation：3,176,883；grok-bot-default：261,750）。
多机采集未同时刷新时可短暂出现真实副本差异，仲裁继续保留冲突标记。
