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

#### bugFix:
1. [TOO-242] 修正 Wails3 版本探针未捕获 stderr 且未保留 CLI 退出状态的断言，避免 post-merge 验证稳定失败或误报成功

#### note:
1. [TOO-242] 固定 Wails3 `v3.0.0-alpha2.117` 与 macOS arm64 工具链能力基线，补充可复现 runbook、平台 adapter 边界和依赖升级准入规则

#### script:
1. [TOO-245] 新增本地与 GitHub PR CI 共用的统一验证入口、项目约束负向检查和 macOS 15 arm64 clean-state gate
