# Windows 安装与更新

适用 Windows 11 24H2（build 26100）及以上 x64。产物为当前用户安装的自包含包，包含 WinUI 3 App、PRI、Go Helper、.NET、Windows App SDK 和独立更新执行器。安装/运行不要求 Go、Visual Studio、管理员权限或独立 Windows Service。当前不生成 MSIX，也不修改系统根证书信任。

## 安装、启动与卸载

将同一份产物的 `Setup.exe`、`release.json` 和 `codex-pulse-<build>-win-x64.zip` 放在一起，运行 `Setup.exe install` 并确认。默认程序目录为 `%LOCALAPPDATA%\Programs\CodexPulse`，默认 runtime 为 `%LOCALAPPDATA%\CodexPulse\runtime`；Home 使用显式参数或 `${CODEX_HOME:-$USERPROFILE/.codex}`。`--install-root`、`--runtime-directory`、`--codex-home` 可指定独立绝对路径，程序目录和两个数据目录不得互相包含。

安装在 HKCU 注册卸载项和当前用户开始菜单快捷方式。稳定入口为安装目录中的 `CodexPulse.exe`，通过经过核验的 `current.json` 启动当前版本；登录启动也使用稳定入口，不固定旧版本目录。运行 `Setup.exe uninstall` 或 Windows 的已安装应用卸载入口会确认、请求 App 安全退出，仅删除本安装拥有的程序目录、快捷方式和登录项。runtime、独立凭据库、偏好和 Codex Home 保留；需要删除数据时应另行明确处理。

程序位于 ACL 私有目录，安装所有权绑定包标识、公钥、用户 SID 和目录。所有源文件和目录使用不跟随 reparse 的 Windows 句柄，拒绝 junction、ADS、硬链接、越界路径和不在签名清单内的文件。快捷方式与登录项仅修改目标确属本安装的项。

## 打包与信任配置

开发机先按已固定的 prerequisites 配置环境。所有打包写入新的忽略目录，不覆盖旧产物；运行构建会生成 Helper、WinUI 资源和自包含运行时。

```powershell
dotnet run --project app/windows/CodexPulse.PackageTools -- dev-policy <private-policy.json> https://localhost:18731/
./scripts/windows/package.ps1 -Version 0.15.2-windows-dev.1004 -BuildNumber 1004 -Channel prerelease -PolicyPath <private-policy.json>
```

开发策略使用当前用户 CNG 中不可导出的 ECDSA P-256 签名密钥和独立 TLS 密钥；公开策略/证书只放私有 `.artifacts/`。开发 HTTPS fixture 仅监听 loopback，客户端仅在明确 Development 且 loopback 时使用策略限定的证书链信任，仍检查名称、有效期和 serverAuth；没有系统信任或全局 TLS 绕过。`PackageTools serve <private-feed-directory> <policy>` 提供有界、无请求正文日志的开发源。

生产策略由受控流程配置 `PublicKeySpki` 和固定 HTTPS `FeedBaseUri`，不得使用开发 TLS 证书。策略被编译进 App/更新执行器，不能从下载包自行改变信任根。正式打包还必须显式提供 `-CodeSigningCertificateThumbprint`；脚本用固定 SDK 的 signtool 对本项目二进制签名并独立验证 Authenticode。当前仓库默认策略未配置公钥/地址，普通未安装开发构建不会伪报更新可用。正式证书、公信任、时间戳/公网上线和公开 feed 必须在单独授权的发布流程验收，本轮未执行。

产物包含 `SHA256.json`。`release.json` 是 ECDSA/SHA-256 DER 签名的 exact JSON payload，包含递增 build、版本、架构、最低系统、Core contract、数据兼容声明和每个文件的大小/SHA-256。Helper 的 `--build-info` 只读取编译元数据，不打开 Home/runtime/SQLite；打包工具同时读取 App assembly metadata，拒绝 App/Helper 版本、协议或架构不一致。数据兼容声明组合实际 SQLite schema、偏好 schema 和固定凭据 DDL 指纹，不包含实际凭据的哈希。

## 更新、失败与恢复

设置中的自动检查/周期/通道来自 Go 权威偏好。自动检查不下载或安装。用户检查更新、确认版本后，从固定 HTTPS origin 获取当前通道 JSON 和固定格式资产名；不跟随重定向，不接受 URL 中的凭据。检查/下载有超时、取消、大小和文件数限制。包签名、ZIP 完整性、逐文件哈希、PE 架构及同包元数据全部通过后才请求退出。

App 先以 `client_restart` 排空 Helper，确认 SQLite 关闭、Helper 退出及 Job 归零。执行器必须收到相同 nonce/PID 的新鲜 clean 回执并确认原 PID 消失；uncertain/forced/超时、文件占用都不切换版本。跨进程 mutex 和文件锁保护安装；新包先进入私有 stage，原子发布 current 指针，保留 previous 和 journal。启动成功还须收到新 App 的 nonce/PID/版本匹配且已连接/恢复页回执，不能以 Process.Start 成功代替。

切换前失败且旧 App 已干净退出时，执行器仅在 current 仍为旧版本时重启旧 App。失败结果在 HKCU 私有 profile 元数据中供下一次设置页展示。切换后启动或迁移失败保留两版程序及 runtime，不能静默宣称成功。中断 journal 根据实际 current 验证并完成提交或保留旧指针；不隐式改数据库。

显式 `Setup.exe rollback` 仍须安全退出，并且两版签名包的数据兼容声明必须完全一致。声明变化时返回 `rollback_requires_data_recovery`，禁止把二进制回退当 schema 降级。数据库迁移失败走 Go 的恢复模式和已有备份 prepare/confirm/restart 协议；先完成数据恢复，不能直接用旧二进制打开未来 schema。`Setup.exe launch` 可重开仍被 current 指向的版本。

## 可复验开发矩阵与证据

隔离夹具：`dotnet run --project app/windows/CodexPulse.Tests -- --distribution <fresh-private-directory>`。可在该目录预置名为 `junction` 的实际 junction 并放置目标 `preserve.txt`，执行 no-follow 断言。`--update-network <policy> <fresh-private-directory>` 验证实际 TLS/feed/完整包下载，且无策略限定证书时必须失败。上述夹具使用合成数据，不能作为真实 Codex 结论。

2026-10-29 开发实测：36 项分发业务断言通过，另有 5 项基础 wire 断言；实际 HTTPS 完整自包含包下载/验签/文件核验及无系统证书信任拒绝共 3 项通过。覆盖 N-1 有效包、坏签名/不同根、占用、uncertain/活 PID/过期回执、签名前缺 PRI、journal 在指针切换前后中断、兼容回退和不兼容拒绝、卸载保留外部数据、中文特殊路径和 junction。Go 软件元数据聚焦测试通过。WinUI/PackageHost 自包含发布成功；正式签名/公开源没有执行，不记通过。

真实安装过程中发现缺 PRI 导致 native 启动崩溃，已加入发布强制复制和签名必需文件校验；又发现 AppContext 尾部目录分隔符导致合法更新执行器路径被拒绝，已修复并新增断言。分发矩阵累计 37 项不同业务断言通过，实际 HTTPS 另含下载中断的两项断言，未产生可交接 release。

实际执行：原生 1002 页检查离线/坏签名源均失败且禁用安装，当前进程/版本指针不变；修复后安装 1005，再从原生确认页经限定 HTTPS 下载、核验并升级 1006。旧 App/Helper clean 回执、新 App connected、执行器 ready 和 current=1006 一致。随后通过稳定 Setup 的自迁移执行器显式回退至 1005，返回 ready；实际卸载成功且真实 Home/runtime 保留，再安装 1006 返回成功和 connected。卸载前后 runtime 临时 WAL/SHM/锁文件数量会随正常 drain 改变，不把运行中全目录逐字节相同作为验收条件。重新安装后真实 Home 环境/参数/preferences/物理身份及私有 ACL 全部匹配，身份摘要与升级前一致。最终安装版本的 Codex/中心对账继续由 Master 集成验收记录收口。

原始产物/证据在 `.artifacts/windows/too-673/`，不提交个人路径、原始业务数据或凭据。受控 1006 包包含 538 个签名清单文件，ZIP SHA-256 为 `962eff1623f231b903c3084eddc298eb30d9e37d5594cfeda979a8e643e90f10`；这是开发包签名证据，Authenticode 公信任未执行。
