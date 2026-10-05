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
