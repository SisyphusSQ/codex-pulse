# Updates and Release Boundary

## 当前实现

Codex Pulse 的原生 macOS App 使用锁定版本的 Sparkle 2.9.4 完成应用内更新。
发布 Bundle 必须内嵌：

- `Contents/Frameworks/Sparkle.framework`；
- 使用 HTTPS 的 `SUFeedURL`；
- 32 字节 Ed25519 公钥对应的 `SUPublicEDKey`；
- 主程序用于加载 Framework 的 `@executable_path/../Frameworks` rpath。

App 启动后，Sparkle 从固定的 appcast URL 检查版本。GitHub Release 同时提供
首次安装 DMG 和 arm64 ZIP，但 appcast 中的 enclosure 只指向 exact ZIP；
Sparkle 下载后使用当前 App
内置公钥验证 Ed25519 签名，验证通过才进入替换与重启流程。“检查更新…”
菜单也走同一 Sparkle updater，不再查询 GitHub Releases API 后打开浏览器。

这意味着 GitHub Release 负责托管版本化 DMG 与 ZIP，但 App 感知新版本的入口
是固定 appcast，不是轮询 Release 列表。发布某个新版本时，顺序必须是：

1. 以全局递增的 `CFBundleVersion` 构建同一 App Bundle，并生成、验证 DMG 与 ZIP；
2. 上传 DMG、ZIP、校验和与 Release Notes，并将版本 Release 公开；
3. 从该 exact ZIP 生成 Ed25519 enclosure 签名；
4. 合并 stable/prerelease 历史，生成新的 appcast；
5. 最后原子更新固定 appcast 资产，并从远端读回内容。

在 ZIP 可公开下载前不得让 appcast 指向它；否则已经安装的客户端会先看到一个
不可下载的更新。

## 首次安装与后续更新

Sparkle 只能更新“已经包含 Sparkle、feed URL 和公钥”的 App。因此：

- `v0.1.0-beta.6` 不含本实现，不能凭空收到应用内更新；
- 第一个包含 updater 的版本仍需用户手工下载 DMG 并完成一次首次打开；
- 从该版本开始，后续版本由 App 检查、下载、验签、替换并重启。

首装 DMG 的卷根只包含 `Codex Pulse.app` 和指向 `/Applications` 的入口，
用户打开后可完成标准拖拽安装。DMG 改善首次安装交互，但不会替代 Developer ID
签名、公证，也不会让旧 App 自动获得 updater。未公证 preview 首次下载仍可能
需要在“隐私与安全性”中确认。Sparkle 可以消除后续手工下载、覆盖与打开 Release
页的动作，但 ad-hoc、未公证 preview 不能承诺 macOS 永远不再拦截；要可靠实现
“仅首次确认，后续更新不重复确认”，仍必须以 Developer ID、notarization、
stapling 和 Gatekeeper 读回作为发布门禁。

## stable 与 prerelease

stable 与 prerelease 共用一个 appcast，但通过 Sparkle channel 分流：

- stable item 不写 `sparkle:channel`，stable 客户端只接受默认 channel；
- prerelease item 写 `sparkle:channel=prerelease`；
- prerelease 客户端允许 `prerelease`，并继续接受后续 stable；
- 所有 channel 共用一个严格递增的 `CFBundleVersion` 序列。

tag、产品版本、GitHub Release 的 prerelease 标记和 appcast channel 必须一致。
例如 `0.2.0-beta.1` 只能进入 prerelease，`0.2.0` 只能进入 stable。新 stable
发布时保留必要的 prerelease 历史，不能为了更新 stable 而覆盖整个 feed。

Helper 持久化 `updates.autoCheckEnabled`、
`updates.autoDownloadEnabled`、`updates.checkIntervalSeconds` 与
`updates.channel`；Swift App 把这些权威偏好映射到 Sparkle。Helper 不获取
appcast、不下载更新包，也不接收密钥或平台安装命令。当前默认策略是定期检查、
不静默下载；用户确认、进度、错误、安装和重启 UI 由 Sparkle 提供。

## 安全退出与替换

Sparkle 准备安装时，App 必须先执行：

```text
Shutdown(reason = client_restart)
    -> Helper 停止接收新的 RPC
    -> 等待已接纳 RPC 返回
    -> scheduler admission fence
    -> lifecycle / metrics / SQLite drain
    -> SQLite close
    -> UDS cleanup
    -> Helper process exit
```

只有 Helper 返回 clean shutdown，App 才允许 Sparkle 继续退出、替换和重启。
forced 或 uncertain 会取消本次终止、保留 Sparkle 已计划的安装意图、重新启动
Helper，并向用户显示稳定错误；不能在 SQLite 仍可能提交时把安装伪装成成功。
下一次退出仍须重新执行同一安全门禁。只有 Sparkle 自身中止安装时才清除安装
意图，避免下一次普通退出被误判为更新安装。

普通退出继续使用 `Shutdown(reason = client_exit)`。父 pipe EOF 是异常托管
收口路径，不替代正常 Shutdown handshake。

## 签名与密钥

- 公钥写入 DMG 与 ZIP 共用 App Bundle 的 `SUPublicEDKey`，可公开。
- 私钥只通过 `scripts/sparkle/generate_appcast.sh` 的 stdin 输入，不进入
  argv、环境、日志、manifest 或仓库。
- appcast 生成器使用官方 Sparkle `sign_update` 对 exact ZIP 签名，并再次
  使用 ZIP 内 `SUPublicEDKey` 验证签名；密钥不匹配时 fail closed。
- appcast enclosure 只接受本仓库 exact GitHub Release HTTPS 资产 URL。
- stable 与 prerelease 都必须使用同一受控信任链；密钥轮换需要独立设计和
  N-1 验证，不能直接替换 Bundle 公钥。

## 验证边界

日常完整入口仍是：

```bash
make verify
```

它覆盖架构、Proto、Go race/vet、Swift transport、原生 App deterministic
tests 和隔离 development App smoke。发行候选还必须验证最终 Bundle、ZIP 与
DMG 的 metadata、Framework、rpath、签名、挂载结构和 SHA-256。

真正宣称应用内更新可用前，还需要针对已公开的同一资产完成 N-1 矩阵：stable
和 prerelease 选路、有效签名更新、坏签名、离线、Helper clean/forced/
uncertain、替换重启、migration/recovery，以及升级后版本与数据读回。本地测试
appcast、ad-hoc preview 构建或 `make verify` 都不能替代 Developer ID、
notarization、Gatekeeper、远端 feed 和真实 N-1 证据。

## Migration 与回滚边界

- `MigrateApplicationSchema` 只在 Store 暴露给 runtime reader/writer 前执行。
- pending migration 在 single-writer transaction 中完成并读回校验，成功后
  才推进版本。
- migration failure 不启动普通业务图，只暴露 Bootstrap、recovery 和退出。
- 二进制回滚不等于 schema 回滚；N-1 矩阵必须显式验证数据兼容与恢复。
