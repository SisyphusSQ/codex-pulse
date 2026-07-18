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
- `readyToInstall` 展示“更新包已准备”并提供独立的键盘可操作安装确认。确认后先进入 shared safe drain；只有 lifecycle/SQLite/observer/instance lease 全部关闭成功，Go 才提交 Sparkle 最终 install reply。

## Sparkle 2 Adapter Contract

v0.1 macOS arm64 更新平台固定为 Sparkle 2.9.4。`internal/updater` 以 `SPUUpdater` 和自定义 `SPUUserDriver` 为唯一 native adapter，不使用 Sparkle 2 已废弃的 `SUUpdater`，也不把 Objective-C 类型暴露给 Wails/Vue。

- Go transient state 固定为 `idle/checking/available/downloading/installing/error`；下载完成但尚未允许重启时仍是 `available + readyToInstall`，不能提前进入 installing。
- `SPUUpdaterDelegate` 提供 valid update、no update、cancel、install 和 typed error；自定义 `SPUUserDriver` 提供真实 download bytes、expected length、extraction progress，以及 Sparkle 交付的 cancel/reply block。
- 检查、下载和最终安装是三个独立命令。检查和下载都具有显式可取消生命周期：check cancel 回到 `idle`，download cancel 回到保留 update 的 `available`；`Download` 只在 valid update 可用时调用 Sparkle 的下载选择 block，`Install` 只消费 `showReadyToInstallAndRelaunch` 的最终 reply，且 application runtime 在调用前强制通过安全重启屏障。
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

Install mutation 未结束期间，Settings 以 250ms 有界刷新读取 shared shutdown snapshot，展示真实 stage；settled 或组件卸载立即停止刷新。该刷新只读状态，不推进 shutdown，也不依赖 GitHub Actions。

### Shutdown 与单实例契约

- `internal/app.applicationShutdownCoordinator` 固定为 `running -> draining -> closed` 单向状态机。首次 Quit/Install 启动后台关闭；并发和重试调用只等待同一个结果，不会重复关闭组件。
- 关闭顺序固定为 instance wake admission、updater cron/admission、scheduler/Wails admission、Tray observer、retention、health、scheduler/lifecycle drain、metrics、SQLite writer/pool、instance lease。前置 admission fence 先拒绝新的 wake、更新操作、后台 job 和 Wails control；后续 drain 再等待已准入任务完成协作式 checkpoint。组件错误记录首个失败阶段，但仍继续关闭后续资源；任何错误都会阻止最终 install reply。
- caller timeout 只终止本次等待并报告当前 stage；后台关闭继续，状态不会从 draining 回到 running。这样不会在 Store 已开始 flush/close 后错误恢复 job admission，也不会把 context cancel 注入正在提交的事务。
- Tray Quit、Cmd+Q 和平台 terminate 的 `ShouldQuit` preflight 都进入同一个异步 coordinator；AppKit 主线程只取消本次原生 terminate 请求，不等待 closer。最早的 `OnShutdown` hook 还会同步封闭 instance wake admission，覆盖信号等绕过 preflight 的退出路径，随后由 defer 有序释放 SQLite 与 flock。
- Sparkle native controller 和 `installChoiceReply` 在 safe close 期间保持存活；updater cron 已暂停。safe close 成功后，整个 reply 提取与提交都异步排入 AppKit main queue，不在持有 Go coordinator/adapter lock 时同步等待 main；Wails shutdown hook 再幂等释放 Sparkle 对象。
- Install 在进入 shared drain 前独占 terminal intent；Tray Quit 与 Cmd+Q 在 native main-queue block 发出 `install_started` 前都被拒绝。该事件同步回写 intent readiness 后才提交 final reply 并允许 AppKit terminate，避免 updater Close 清空一个已接受但尚未消费的 reply。
- `internal/singleinstance` 使用私有目录中的永久 lock inode 与 non-blocking `flock`，并通过短路径 Unix socket 接收固定 wake frame。第二实例只在收到完整 ACK 后退出；owner 进入 shutdown fence 后不再 ACK，contender 会在覆盖 15 秒桌面关闭窗口的 20 秒期限内重试并接管。owner 关闭时先停止 socket、再解锁，进程崩溃则由内核自动释放。lock 文件不删除，避免 unlink/inode 竞态形成双 owner。
- 重启发现 interrupted scheduler job 时仍沿用既有 durable recovery/reconcile；本卡不新增第二套进程内 checkpoint。

## 数据库 migration

- 使用 `PRAGMA user_version` 与 append-only version/name/checksum ledger 管理 schema；缺号、drift、状态分叉和 newer schema 都 fail closed。
- `MigrateApplicationSchema` 只在 Store 打开后、暴露给 runtime reader/writer 前的 bootstrap 阶段执行；未来运行期 maintenance migration 必须先实现任务排空与 Store 独占。
- 所有 pending migration 在一个 single-writer GORM transaction 中执行，完整成功后才推进版本；不使用无版本 `AutoMigrate` 代替 migration。
- 有 pending 且已有用户 schema 时，先做空间检查，再使用 modernc Pure Go SQLite Backup API 创建包含 committed WAL 的私有备份；备份临时文件通过只读 `PRAGMA quick_check`、fsync 和原子发布后才允许运行 migration，fresh/current 数据库跳过。
- 恢复使用独立 `NewRestore` 文件原语生成新数据库，验证后再由上层安全重启流程决定切换；不得运行中覆盖当前 Store。
- migration 失败进入只读安全模式，只保留稳定诊断、恢复备份和退出；安全模式不检查更新。
- `internal/app.Run` 在注册业务 Wails service、scheduler、metrics、health、updater 与 retention 前执行 startup migration gate。成功才装配 normal graph；typed `MigrationFailure` 会先关闭 Store，再装配互斥的 recovery graph。recovery graph 不创建 preferences/updater，也不注册普通 query、settings mutation、quota、index 或 SQLite writer command。
- recovery contract 只暴露稳定 stage/code/version、备份 basename/size/mtime 与 `failed/running/awaiting_confirmation/restart_required`；底层 cause、SQL、数据库内容、路径正文和凭据不跨 Wails。重试成功只进入 `restart_required`，不在已运行进程内热装配 normal graph。
- 恢复必须先从私有 `0700` backup 目录选择非 symlink 的 regular `0600` SQLite 文件，经 `O_NOFOLLOW` 打开并冻结 SHA-256；确认时再次从独立 descriptor 复制、比对摘要，抵御同尺寸/同时间戳替换。冻结副本先 restore 到 working DB，执行 migration、schema readback 和 builtin pricing 验证，再通过 SQLite backup 固化为无 WAL 依赖的 ready DB。切换前用 SQLite Online Backup 把当前失败库及已提交 WAL 页保存为独立 `0600` 备份；Darwin 使用 `RENAME_SWAP` 原子交换 ready/canonical，目录同步失败会原子换回，因此 canonical path 始终存在，且不会覆盖唯一副本。
- retry/prepare/cancel/confirm/exit 写入私有 `0600` content-free JSONL audit，只记录时间、动作、结果、stage/code 与备份 basename；audit path 使用 `O_NOFOLLOW`，拒绝 symlink 或宽权限目录。测试只使用 synthetic `t.TempDir()` 数据库。
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

### 本地发布工具链

- `scripts/sparkle/prepare_release_tools.sh` 只从 SHA-256 固定的 Sparkle 2.9.4 官方 archive 提取 `generate_appcast`、`sign_update` 与 `generate_keys`，校验 Mach-O 与 arm64 slice；正常构建不执行 `generate_keys`。
- `task release:local` 只从 stdin 读取一行 Sparkle Ed25519 private seed，并通过官方 `generate_appcast --ed-key-file -` 签名。private seed 不允许进入 argv、环境、日志、manifest、release notes 或最终 `dist/update`。
- release bundle 在打包时注入公开的 `SUFeedURL` / `SUPublicEDKey`；公钥必须 base64 解码为 32 bytes，feed 和 download URL 必须是无 userinfo 的 HTTPS URL。
- 完整 release 通过 `/usr/bin/lockf` 在常驻 FD 上持有 `dist/.update.lock` 的内核独占锁；并发者立即失败，进程退出或崩溃由内核释放，不使用 PID/stale-owner 清理。所有产物先写 `dist/.update.staging.*`，通过后使用 macOS `renameatx_np(RENAME_SWAP)` 原子替换 ignored `dist/update`；失败或进程终止前不会产生目标目录缺口，commit point 后的旧目录清理不再反转成功结论。
- 签名生成以 Sparkle 官方工具为真相；`releaseverify` 只持公钥，先用 Go 标准库 Ed25519 对 archive bytes 独立验签，再检查唯一顶层 app、ZIP 路径/大小/大小写冲突与不越界的 framework 相对 symlink，最后才允许 `ditto` 解压。
- verifier 限定 release 目录只能包含 ZIP、同 basename 纯文本 release notes、appcast 与 manifest；release notes 必须与 appcast 内嵌内容逐字一致，并重新计算 ZIP/plist/appcast/notes/manifest 元数据，避免 verifier 再访问 private key 或 Keychain。
- `task release:verify` 是只读离线复验入口。它不会上传、创建 tag/GitHub Release、更新真实 appcast，也不会触发 Actions。

### Key 生成与导入边界

Sparkle 官方 `generate_keys` 会读写登录 Keychain。只有在 release Issue 范围内且用户另行明确授权后，人工 operator 才可按 Sparkle 2.9.4 官方说明执行：

```text
.cache/sparkle/2.9.4/tools/generate_keys --account <approved-account>
.cache/sparkle/2.9.4/tools/generate_keys --account <approved-account> -x <private-file>
.cache/sparkle/2.9.4/tools/generate_keys --account <approved-account> -f <private-file>
```

本仓库不提供自动执行上述命令的 Task，不接收 private file path，也不把 private seed 写入 `.env`。批准后的本机/CI 流程应由受控 secret manager 直接向 `task release:local` stdin 提供 secret；本轮 TOO-293 只用 `generate_keys --help` 核对官方参数，并只运行进程内合成临时测试 key，未执行 key 生成/导入/导出、未访问 Keychain，也未生成或使用正式更新 key。

## 当前交付边界

- TOO-291 已冻结并验证 Sparkle 2.9.4 adapter、typed state、main-thread lifecycle、pinned framework 与本地 bundle/package 链。
- TOO-292 负责自动检查偏好、每小时调度、Wails/Vue 状态与用户动作，不在 adapter 内重复保存偏好。
- TOO-293 负责 `SUFeedURL` / `SUPublicEDKey`、外部私钥注入、appcast/ZIP 签名与离线验证；仓库和客户端永不保存私钥。
- TOO-294 负责 safe drain 后才调用最终安装 reply；TOO-296 才能把真实 N-1 下载、签名拒绝、安装、重启和 migration 作为升级 E2E 证据。
- Developer ID、notarization、正式 tag/release、真实 appcast 与外部分发继续受独立发布门禁约束，不能由普通 Execution closeout 自动触发。
