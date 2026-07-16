## Unreleased

#### feature:
1. [TOO-243] 完成 Go、Wails3 与 Vue 3 工程骨架初始化，固定前后端依赖、generated bindings 与 macOS 15 arm64 开发构建入口
2. [TOO-244] 新增 macOS 15+ thin arm64 应用 Bundle、冻结图标、ad-hoc 签名与单顶层 ZIP 打包验证闭环
3. [TOO-246] 新增 SQLite WAL 连接面、有界单写队列、独立只读池与应用 drain/close 生命周期，固定私有路径权限、authoritative cancellation 和可判定错误语义
4. [TOO-247] 建立 Session、Turn、Usage 六表 STRICT 事实与投影 Schema、原子 bootstrap 和 typed repository，固定来源 generation/offset 幂等、finality 与 unknown/null/真实零语义
5. [TOO-248] 建立 Source、Job、Health 与 Pricing 七表 STRICT 运行事实 Schema、原子 bootstrap 和 typed repository，固定 interrupted-only 恢复、健康生命周期与有限分类、不可变价格版本及敏感内容持久化边界
6. [TOO-249] 重构 SQLite 持久层为 Pure Go GORM adapter，新增版本化 migration、WAL 一致性备份与恢复、目录持久化发布及跨 core/runtime typed WriteUnit 原子事务
7. [TOO-250] 新增固定 24 小时运行数据保留、低优先级有界 maintenance 写入、append-only v2 查询索引与异常退出恢复集成矩阵，保证 GORM Pure Go Store 可分批清理并保留长期事实和恢复血缘
8. [TOO-251] 新增基于 confirmed root FD 的 Codex JSONL 文件发现、有限前缀 fingerprint 与 confirmed-home 范围内的两阶段来源对账，安全识别新增、增长、截断、移动、替换、删除和不可读状态
9. [TOO-252] 实现有界增量 JSONL Parser 与显式 Turn lifecycle，输出 allowlisted session/turn/nullable usage、content-free diagnostics 和完整恢复 checkpoint，支持坏行续读、乱序事件及跨重启确定性恢复
10. [TOO-253] 实现增量索引的原子 checkpoint、generation 重建与幂等事实提交，支持 append、retry、truncate、replace 和 crash 恢复，并保证 GORM Pure Go SQLite 下 offset 不越前、旧快照原子切换及跨 Session usage 隔离
11. [TOO-254] 新增可解释、可重算的 Session、Project、Model 隐私归因与 schema v4 历史回填，固定路径摘要身份、模型别名、冲突/非法语义和不含绝对路径的安全查询投影
12. [TOO-255] 新增有来源的版本化 exact-only API 价格目录、整数微美元 Turn 成本和可原子重算的 Session/全局/Project/Model 日聚合，固定 unknown/partial、时区日界、持久化后严格对账与旧 generation 回滚语义
13. [TOO-256] 新增 append-only Session Index Repair 的零写入 dry-run、精确确认、数据库与索引双备份、受控 correction 和写后对账，固定 Store/index 漂移与本地不支持行的 fail-closed、原子 terminal success、审计失败及重新 dry-run 恢复语义
14. [TOO-257] 新增确认前 metadata-only Codex Home 探测与版本化私有 Onboarding preferences，固定三来源路径规范化、physical identity 与结构竞态 fail-closed、显式隐私确认、并发取消、重启及 durability-unknown 恢复语义
15. [TOO-258] 新增版本化 typed Preferences、v1 原子迁移与单 Codex Home 切换协议，固定 private CAS/execution lease、显式数据策略、Home generation fence、并发 owner、bootstrap journal 与取消/崩溃恢复语义
16. [TOO-259] 新增 Discover、Fast Bootstrap、History Backfill 与多 pass Reconcile 四阶段可恢复首次索引，固定 schema v6 typed plan、confirmed Home 读取、权威 source checkpoint、generation admission/Drain 和 fresh reconcile 闭合语义
17. [TOO-260] 实现持久 Live/Backfill 双队列与协作式 ScanBudget，固定实时优先与历史公平、单重型 owner lease、真实 IO 预算、可观测 cycle 及跨崩溃 target 恢复语义
18. [TOO-261] 实现持久暂停续传、休眠唤醒、来源对账与故障恢复状态机，固定 Home generation fence、可取消有界退避、typed 用户动作、应用事件串行化及 target/cycle 原子恢复语义
19. [TOO-305] 重构 scheduler 周期唤醒为 robfig/cron v3.0.1 每秒 trigger，固定重叠跳过、首错 fence、panic 脱敏、Stop drain 与持久调度状态边界
20. [TOO-262] 新增本地 Codex JSONL quota observation 的严格解析、物理 source provenance、GORM Pure Go schema v9 持久化与 coalesced replay receipt，固定 partial window、schema drift、文件替换、checkpoint rollback 和隐私边界
21. [TOO-263] 新增受控 Wham quota 客户端、内存凭证租约与 GORM Pure Go schema v10 原子记录，固定 exact-key/null/partial 校验、禁止 redirect、七类失败、短重试和 last-known-good 语义
22. [TOO-264] 建立可重建的 quota 窗口代际校验与 Local/Wham 可信仲裁投影，隔离异常、旧代际和冲突 observation，保留 last-known-good 与完整 evidence 解释
23. [TOO-265] 新增只读 Reset Credits 事实与动态汇总、可信 quota reset 计算和 robfig cron 驱动的持久刷新退避，固定 Retry-After、60 秒手动节流、claim generation fence、崩溃恢复与迟到 attempt 隔离语义
24. [TOO-266] 新增单 SQLite snapshot 的 Quota Current 只读查询合同，稳定组合窗口、来源、可信 reset、Reset Credits 与刷新状态，固定 null/真实零、last-known-good、冲突解释、投影恢复和敏感 identity 隔离语义
25. [TOO-306] 装配在线 Quota 与 Reset Credits 应用运行时，新增 confirmed Home 调用期凭证租约、崩溃 journal 恢复、local/quota generation drain 与 lifecycle、settings、manual、shutdown 闭环
26. [TOO-267] 建立版本化 `query-v1` 公共查询契约，统一有界分页、allowlist 排序筛选、IANA/DST 本地日转 UTC、JavaScript 安全整数、unknown/真实零及 content-free 错误与 partial/unavailable 语义
27. [TOO-268] 新增 active 成本账本驱动的日周月 Usage/Cost 趋势与 Session/Project 有界查询，固定安全归因、opaque cursor、unknown/真实零、partial 降级及 pricing evidence 对账语义
28. [TOO-269] 新增 Quota、Source、Job、Health 与 Settings 只读查询，固定 GORM 单快照分页、敏感字段裁剪、来源单侧 partial、有限 recovery action 与 JavaScript 安全数值语义

#### bugFix:
1. [TOO-242] 修正 Wails3 版本探针未捕获 stderr 且未保留 CLI 退出状态的断言，避免 post-merge 验证稳定失败或误报成功

#### note:
1. [TOO-242] 固定 Wails3 `v3.0.0-alpha2.117` 与 macOS arm64 工具链能力基线，补充可复现 runbook、平台 adapter 边界和依赖升级准入规则

#### script:
1. [TOO-245] 新增本地与 GitHub PR CI 共用的统一验证入口、项目约束负向检查和 macOS 15 arm64 clean-state gate
