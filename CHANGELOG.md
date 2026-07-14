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

#### bugFix:
1. [TOO-242] 修正 Wails3 版本探针未捕获 stderr 且未保留 CLI 退出状态的断言，避免 post-merge 验证稳定失败或误报成功

#### note:
1. [TOO-242] 固定 Wails3 `v3.0.0-alpha2.117` 与 macOS arm64 工具链能力基线，补充可复现 runbook、平台 adapter 边界和依赖升级准入规则

#### script:
1. [TOO-245] 新增本地与 GitHub PR CI 共用的统一验证入口、项目约束负向检查和 macOS 15 arm64 clean-state gate
