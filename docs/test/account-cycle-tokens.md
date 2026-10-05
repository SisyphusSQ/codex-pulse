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

发布、三机客户端及 SQMC04 生产中心更新尚未完成，最终版本、结构、签名和运行读回将在实际执行后追加。本文件不宣称真实账号切换 E2E、Sparkle 自动更新 E2E 或独立 MySQL 8.4 验收已完成。
