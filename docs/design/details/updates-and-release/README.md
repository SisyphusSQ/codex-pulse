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

### 检查编排与交互边界

- `internal/updater.Coordinator` 是启动、robfig cron 定时、系统唤醒和手动检查的唯一合并点；cron 每分钟只做到期判定，默认偏好仍是每 3600 秒检查一次，不创建第二套 timer loop。
- 同一时刻只允许一个 check/download/install 操作；checking/downloading/installing 会合并重复触发，可用更新仍允许用户手动复查。
- 离线、HTTP 429 或 appcast 解析失败进入 content-free typed fault；自动重试间隔加倍并封顶 24 小时，手动检查不受该退避限制。缺少 feed/key 或平台 adapter 不可用时 fail closed，且不写入虚假的最后检查时间。
- 检查成功发出的 native 状态变化只广播无内容 invalidation event，Vue 再从 allowlisted `UpdateState` 查询读取版本、摘要、大小、签名和进度；event payload 不承载更新事实。
- Settings 提供检查、取消、下载确认、跳过和稍后提醒。下载必须经过键盘可操作的确认对话框；release notes 作为纯文本渲染，不解释为 HTML。
- `readyToInstall` 只展示“更新包已准备”状态，不提供安装按钮。最终 install reply、safe drain、SQLite/单实例锁关闭仍由 TOO-294 负责。

## Sparkle 2 Adapter Contract

v0.1 macOS arm64 更新平台固定为 Sparkle 2.9.4。`internal/updater` 以 `SPUUpdater` 和自定义 `SPUUserDriver` 为唯一 native adapter，不使用 Sparkle 2 已废弃的 `SUUpdater`，也不把 Objective-C 类型暴露给 Wails/Vue。

- Go transient state 固定为 `idle/checking/available/downloading/installing/error`；下载完成但尚未允许重启时仍是 `available + readyToInstall`，不能提前进入 installing。
- `SPUUpdaterDelegate` 提供 valid update、no update、cancel、install 和 typed error；自定义 `SPUUserDriver` 提供真实 download bytes、expected length、extraction progress，以及 Sparkle 交付的 cancel/reply block。
- 检查、下载和最终安装是三个独立命令。检查和下载都具有显式可取消生命周期：check cancel 回到 `idle`，download cancel 回到保留 update 的 `available`；`Download` 只在 valid update 可用时调用 Sparkle 的用户选择 block，adapter 不暴露绕过安全重启屏障的立即安装入口。
- appcast version/OS/hardware selection、appcast signing status 和归档 EdDSA 校验以 Sparkle 为唯一平台真相。Go 只映射 Sparkle 结果：`SUSignatureError` / `SUValidationError` 归一为 `invalid_signature`，不再实现第二套验签器。
- updater、delegate、user driver 和 reply block 只在 AppKit main queue 创建/调用/释放。native holder 强持有 Sparkle 的 weak delegate；Go callback 使用 generation/closed guard，`Close` 后迟到回调不再进入状态机。
- `internal/app.Run` 在 application composition root 启动并持有 updater runtime，在 Wails shutdown 且 AppKit loop 仍存活时先关闭，defer 再做幂等错误读回。缺少 `SUFeedURL` / `SUPublicEDKey` 是可观察 configuration error，但不阻止本地账本应用启动。

Sparkle.framework 不提交到仓库。构建固定从官方 2.9.4 release 获取公开归档并校验 SHA-256，放入 ignored repo-local cache；Darwin package 将 framework 内嵌到 `Contents/Frameworks`，注入 `@executable_path/../Frameworks` rpath，并验证版本、arm64 slice、Mach-O dependency、深度 ad-hoc 签名和 ZIP 解包一致性。

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

## 当前交付边界

- TOO-291 已冻结并验证 Sparkle 2.9.4 adapter、typed state、main-thread lifecycle、pinned framework 与本地 bundle/package 链。
- TOO-292 负责自动检查偏好、每小时调度、Wails/Vue 状态与用户动作，不在 adapter 内重复保存偏好。
- TOO-293 负责 `SUFeedURL` / `SUPublicEDKey`、外部私钥注入、appcast/ZIP 签名与离线验证；仓库和客户端永不保存私钥。
- TOO-294 负责 safe drain 后才调用最终安装 reply；TOO-296 才能把真实 N-1 下载、签名拒绝、安装、重启和 migration 作为升级 E2E 证据。
- Developer ID、notarization、正式 tag/release、真实 appcast 与外部分发继续受独立发布门禁约束，不能由普通 Execution closeout 自动触发。
