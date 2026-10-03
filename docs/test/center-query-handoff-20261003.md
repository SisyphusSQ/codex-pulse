# 中心优化后续会话交接：2026-10-03

用户要求完成中心性能优化后，在同一项目新建使用 `gpt-6.1-sol` 的会话，不创建新的 WorkTree。本页承接已确认事实；新会话先读取本页、[设计](../design/details/multi-machine-reporting/center-query-performance.md)、[验证记录](center-query-retention-20261003.md)及相关就近 AGENTS，等用户的新指令。

## 已交付

- v0.15.1/build64 Mac 发行与三机升级已在之前完成；正式 tag、Sparkle 与 GitHub 发行资产保持原样，源版本 `1e12a95b9d4c28f5f4dd64a33d2bb940ca192013`。原资产只有 ad-hoc 签名，不能描述为 Developer ID 或公证发行。本次仅优化中心，不重新发 Mac 包。
- 后续中心改动源提交 `9dbb1c511281c5c272e93350f861d880dabf30cb`，[PR #180](https://github.com/SisyphusSQ/codex-pulse/pull/180) 已合并；构建名 `v0.15.1+center.9dbb1c5`，二进制 SHA-256 `b5c4c8a6c48d970e4c1827fd07a097915d1236248b10cbed0cb14e1c2300f0bf`。
- SQMC04 上 production 18090 / development 18089 使用同一构建，中心 schema v4，`server.quotaMaintenance=true`，ready 与投影缺失 0 已回读。中心服务保持既有 LaunchAgent 名称，配置在忽略的 `server/config/{production,development}.local.yml`。凭据仅在私有配置，不能输出或提交。
- 账号首屏目录/摘要，选中窗口节奏，展开后分页证据；四周期保留最后有效周期与最近三个其他已观测周期，结束周期连续相同状态保留首末，退役摘要防止原样补传复活。当前周期及无法可靠归入周期的异常事实保留；后端证据仍在选中窗口内投影后分页。
- 全局统计/Session 指标读取轻量投影，全年只算需要的总量/每日/覆盖，结构化事实直接扫描及只读 gzip；首次投影补建完成后停止扫描。单采集机过滤的统计仍使用完整来源仲裁。Storybook 已追加按需详情预览，用户停用的 6007 没有重启。
- 开发聚焦验证已通过，提交/部署收尾按用户约定不重复测试。真实 SeekDB 备份恢复演练、生产时序和账号读取结果见验证记录；不存在独立 MySQL 8.4 或长期周期 PASS。

## 私有运行上下文与边界

- 真实证据、升级前完整 SQL 备份、配置副本和截图在忽略目录 `.artifacts/center-query-optimization/`，权限为私有目录；不提交原始 SQL、日志、账号信息或凭据。恢复演练使用独立库，不覆盖生产；升级备份恢复会撤销旧授权，不能只替换 v3 二进制去读 v4。
- 原大 Session 20 MB 导致数据库 packet 预算不足已在之前解决；SeekDB global max_allowed_packet 为 128 MiB。其持久化配置不在本轮重新验收范围；不要把运行时值当作重启后永远保持的证明。
- SQMC03/SQMC04 生产上报继续运行，SQMC05 原来未配置中心上报，不能自行启用。跨机访问按当前 Tailscale 身份/IP、严格 SSH known_hosts；Mac 后续真正发版须在 SQMC05 执行已授权流程。
- 保留账号身份、linked_history、真实零/未知、冲突、最后采集时间与接收时间边界。API 参考费用不是订阅账单；不能根据套餐或订阅价格生成虚假额度。
- 同主机不同端口共用现有 HTTP Cookie 名；DEV/生产需独立 Cookie jar 或浏览器配置。此次已恢复预览浏览器生产授权，没有改 Chrome 原授权。该既有 Cookie 隔离问题是后续讨论候选，不自动扩大本轮代码范围。
- Mac 上 /metrics 未看到 process_resident_memory_bytes；已确认独立鉴权、Go runtime 与上传指标。不要宣称所有进程指标/长期采集均通过。
- TOO-523 为先前已完成优化卡；既有 Master 的独立 MySQL/完整三机/长期矩阵不自动完成。

## 下一步

当前实施与部署已完成。新会话仅承接上下文，简短报告已读和当前状态，等待用户验收反馈或新需求；不要新建 WorkTree，不主动重新测试、发版、改认证、删历史或启用其他机器。
