# Updates and Release

## 产品原则

借鉴 Codex Runway 的交互：启动后后台检查，此后默认每小时检查；只自动检查，不自动下载或安装。发现版本后由用户选择“立即更新”“稍后”或“跳过此版本”。

- 自动检查默认开启，设置页可关闭，并提供“立即检查”。
- 自动下载默认关闭；应用不在用户不知情时退出或重启。
- 提示展示当前/新版本、包大小、更新摘要，以及是否包含数据库 migration 或索引重建。
- 持久化 `skipped_version` 和 `snooze_until`，避免重复打扰。
- 下载、签名验证、解压、安全排空、安装、重启和结果分别展示状态。
- 更新检查是最低优先级网络任务，不与 quota 前台刷新争抢，也不影响业务数据 freshness。

自动检查开关、通道、跳过版本、稍后时间和最后检查时间保存到独立 preferences，不能只依赖主 SQLite。更新 attempt/result 可以写入 SQLite。

## 安全重启屏障

```text
用户确认安装并重启
    -> updater 请求 Scheduler 进入 draining
    -> 停止接收新的后台任务
    -> 当前增量解析在安全边界完成或取消
    -> 提交结构化事实和 parsed_offset
    -> 关闭 SQLite 和单实例锁
    -> 安装并重启
    -> 执行 migration / 健康检查
    -> 恢复后台调度
```

等待期间显示“正在安全结束后台索引，完成后安装并重启”。超时任务可以在不推进游标的前提下取消；不能直接终止尚未提交的事务。选择“稍后”后允许下载包保持 ready，下一次安全点再安装。

## 数据库 migration

- 使用 `PRAGMA user_version` 与 append-only version/name/checksum ledger 管理 schema；缺号、drift、状态分叉和 newer schema 都 fail closed。
- `MigrateApplicationSchema` 只在 Store 打开后、暴露给 runtime reader/writer 前的 bootstrap 阶段执行；未来运行期 maintenance migration 必须先实现任务排空与 Store 独占。
- 所有 pending migration 在一个 single-writer GORM transaction 中执行，完整成功后才推进版本；不使用无版本 `AutoMigrate` 代替 migration。
- 有 pending 且已有用户 schema 时，先做空间检查，再使用 modernc Pure Go SQLite Backup API 创建包含 committed WAL 的私有备份；fresh/current 数据库跳过。
- 恢复使用独立 `NewRestore` 文件原语生成新数据库，验证后再由上层安全重启流程决定切换；不得运行中覆盖当前 Store。
- migration 失败进入只读安全模式，保留检查更新、查看错误和恢复备份。
- 二进制回滚不能自动解决 schema 回滚；迁移和恢复路径必须独立验证。

## 发布可信链

```text
版本 tag
    -> CI 测试
    -> 目标平台 / 架构构建
    -> 平台代码签名与 notarization
    -> 独立更新私钥签名产物并生成 manifest
    -> 发布 Release
    -> 客户端以内置公钥验证后安装
```

更新签名私钥只放 CI secrets，缺失时发布必须失败；客户端不保存私钥。更新包签名不能代替平台代码签名和 notarization。正式 macOS 分发应同时具备 Developer ID 签名、Apple notarization 和更新包签名。

## 尚未决定

当前只确定统一 updater 状态机、安全重启、migration 和可信发布链，未决定具体 adapter：

- 如果 v0.1 macOS-only，可评估 Sparkle 与 Wails3 打包集成。
- 如果首版需要跨平台，保留统一状态机，分别接入平台安装器。

v0.1 是否只发布 macOS、跨平台进入哪个阶段，后续单独确认。
