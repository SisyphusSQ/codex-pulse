# Codex Pulse Release Policy

## 目录

1. 产品与版本边界
2. 发布等级
3. 当前 signed-notarized stable blockers
4. 版本与产物
5. 验证矩阵
6. CHANGELOG
7. 数据、隐私与首次使用

## 1. 产品与版本边界

Codex Pulse 是一个产品、一个仓库和一个发布单元：

- `api/codexpulse/core/v1/core.proto` 是 Swift App 与 Go Helper 的唯一
  跨进程 contract。
- Git tag、GitHub Release、App Bundle 和内嵌 Helper 必须来自同一 commit。
- Swift App 与 Go Helper 使用同一产品版本；contract version 独立演进，
  不使用产品 SemVer 替代 `core-rpc-v1`。
- SQLite schema、preferences schema、pricing 和 attribution rule version
  是内部数据版本，不随产品版本机械改写。

issue 完成只进入 `CHANGELOG.md -> Unreleased`。只有真实发布资产时才归档
Release 段和创建版本 tag。

## 2. 发布等级

### Stable

Stable 表示产品 SemVer 与 GitHub Release 已进入非 prerelease 渠道；macOS
发行信任等级另行记录为 `unsigned` 或 `signed-notarized`。

默认 stable 使用 `signed-notarized`，并满足：

- clean release commit，且发布 commit 已冻结；
- `make verify` 通过；
- production Bundle identifier、显示名、版本和 build number 正确；
- App 内所有可执行代码按 inside-out 顺序使用 Developer ID 签名；
- Hardened Runtime、secure timestamp、notarization 和 stapling 通过；
- 解压后的最终资产通过 `codesign`、`spctl` 和 `stapler`；
- 挂载后的首次安装 DMG 只包含 App 与 `/Applications` 链接，且其中 App
  通过 `codesign`、`spctl` 和 `stapler`；
- 持久 runtime、Codex Home 首次确认和重启读回闭环；
- 在全新 macOS 用户环境完成首次安装、打开、索引和再次启动验证；
- 使用正式 Sparkle Ed25519 公钥构建，私钥只经 stdin 参与 exact ZIP 签名；
- fixed HTTPS appcast 已完成 stable/prerelease 选路、远端下载和 N-1 替换重启；
- Git tag、GitHub Release、资产 SHA-256 和发布状态远端读回一致。

用户可以显式授权 `unsigned stable`。该例外路径仍必须满足 clean release
commit、统一 Bundle/Helper 版本、ad-hoc inside-out 完整签名、DMG/ZIP/SHA-256、
Sparkle Ed25519、signed tag 与 GitHub readback，但允许跳过 Developer ID、
Hardened Runtime、公证、stapling 和全新用户验收。Release Notes 必须明确：

- 这是正式功能版本，但不是 macOS 已认证的可信分发；
- 首次打开仍需通过“系统设置 → 隐私与安全性 → 仍要打开”；
- 不得声称 Developer ID、Apple 公证或 Gatekeeper acceptance 已通过；
- 后续取得可信分发能力时使用新 patch 版本，不覆盖既有 tag 或资产。

### Preview

Preview 使用 prerelease SemVer，例如 `v0.1.0-beta.1`，并在 GitHub 标记
Prerelease。未签名、未公证资产只有在用户明确授权后才能发布；Release
Notes 必须写清风险和 Gatekeeper 打开步骤。

Preview 不能被描述为 stable，也不能把 isolated smoke 当作最终用户验收。

## 3. 当前 signed-notarized stable blockers

检查实际仓库，不要把本节当作永久事实。当前已知 gate 包括：

- `scripts/macos/build-dev-app.sh` 明确只组装 unsigned development App；
- `scripts/macos/build-release-app.sh` 当前只执行完整 ad-hoc inside-out 签名，
  不执行 Developer ID、Hardened Runtime、公证或 stapling；
- 正式 Sparkle key pair、固定生产 appcast 与远端更新资产尚需逐次授权启用；
- 已安装 N-1 App 的 stable/prerelease 真实下载、验签、Helper clean shutdown、
  替换重启与 migration/recovery 矩阵需要针对发行候选读回；
- 首次 Codex Home 确认、持久偏好与普通用户重启仍需完成产品验收。

只要任一项仍成立，skill 必须输出 `stable_release_ready=false`，表示
`signed-notarized stable` 尚未就绪。这个字段不能由 Release Notes 代替。
用户显式授权的 `unsigned stable` 可以在 source preflight、ad-hoc 发行资产、
Sparkle key、Gatekeeper 披露和远端读回成立后继续，但不得把该例外解释为
可信分发 gate 已通过。

`render-notes --channel stable` 仍必须显式选择真实 distribution。
`signed-notarized` 只是防止误操作的分类，不是签名或公证证据；执行者仍须保存
并报告真实 readback。stable 与 Preview 都不能只由 channel 推断签名状态。

## 4. 版本与产物

使用以下映射：

| 表面 | 示例 |
| --- | --- |
| Git tag / GitHub Release | `v0.1.0-beta.1` |
| 产品 SemVer | `0.1.0-beta.1` |
| `CFBundleShortVersionString` | `0.1.0` |
| `CFBundleVersion` | 显式递增的正整数，例如 `42` |
| RPC contract | 保持 `core-rpc-v1`，除非 contract 本身升级 |

Apple Bundle short version 只使用三段数字。Prerelease channel 留在 tag、
Release 和产品展示版本中，不写入 `CFBundleShortVersionString`。

默认产物：

```text
.artifacts/releases/<tag>/
├── Codex-Pulse-<tag>-macos-arm64.dmg
├── Codex-Pulse-<tag>-macos-arm64.zip
├── SHA256SUMS
├── release-notes.md
└── appcast.xml              # 待发布到固定 feed 的候选，不上传到版本 Release
```

DMG 是用户首次安装的推荐资产，包含 `Codex Pulse.app` 与指向
`/Applications` 的拖拽入口；ZIP 是 Sparkle appcast 指向的 exact 更新资产。
GitHub 自动生成的 `Source code (zip)` 与 `Source code (tar.gz)` 都不是 App
安装包，Release Notes 必须明确区分。

stable 与 prerelease 共用一个 appcast。stable item 不写
`sparkle:channel`，prerelease item 写 `prerelease`；两个 channel 的
`CFBundleVersion` 必须使用同一个严格递增序列。版本 Release 托管首次安装
DMG、exact ZIP、校验和与 notes；固定 feed 必须在版本 Release 公开且 ZIP
可下载后最后更新，避免客户端看到尚不可用的 enclosure。

生成 appcast 时使用：

```bash
scripts/sparkle/generate_appcast.sh \
  --version 0.1.0-beta.7 \
  --build-number 7 \
  --channel prerelease \
  --archive .artifacts/releases/v0.1.0-beta.7/Codex-Pulse-v0.1.0-beta.7-macos-arm64.zip \
  --archive-url https://github.com/SisyphusSQ/codex-pulse/releases/download/v0.1.0-beta.7/Codex-Pulse-v0.1.0-beta.7-macos-arm64.zip \
  --existing .artifacts/appcast-current.xml \
  --output .artifacts/releases/v0.1.0-beta.7/appcast.xml
```

Sparkle Ed25519 私钥必须从该命令的 stdin 输入。脚本使用官方
`sign_update` 签 exact ZIP，并以 ZIP 内的 `SUPublicEDKey` 再次验签；私钥
不得进入 argv、环境、日志、manifest、仓库或发布产物。

## 5. 验证矩阵

| 证据面 | 最低入口 |
| --- | --- |
| 项目完整验证 | `make verify` |
| 真实 Home 产品验收 | `make verify-live` 或等价显式真实 Home 启动 |
| Bundle metadata | `plutil` 读回最终 App 的 Info.plist |
| 嵌套签名 | `codesign --verify --deep --strict --verbose=2` |
| Gatekeeper | signed-notarized 要求 acceptance；unsigned 必须记录预期拒绝并在 notes 披露 |
| 公证票据 | signed-notarized 使用 `xcrun stapler validate`；unsigned 明确为未执行 |
| ZIP 完整性 | 解压后重跑 Bundle、签名、公证检查 |
| DMG 完整性 | `hdiutil verify`，只读挂载后检查 App、`/Applications` 链接、签名与公证 |
| Sparkle Bundle | `SUFeedURL`、`SUPublicEDKey`、Framework 与 rpath 读回 |
| Sparkle appcast | XML、channel、全局 build number、exact asset URL 与 Ed25519 验签 |
| N-1 更新 | 已安装旧 App 真实检查、下载、Helper drain、替换、重启与版本/schema 读回 |
| SHA-256 | 本地生成、GitHub 下载后重新比对 |
| tag | 远端 tag object 与 peeled commit readback |
| Release | `gh release view --json ...` |
| 首次使用 | 全新 macOS 用户安装、首次打开、Codex Home 确认和重启 |

真实 Home 验收会读取 Session/JSONL，并可能写入私有 runtime、SQLite、
preferences 和标准 housekeeping。执行前按根级 `AGENTS.md` 说明副作用。

## 6. CHANGELOG

保留 `## Unreleased`。功能、修复、文档和脚本变更先写入对应分类。

真实发布时归档为：

```markdown
## Unreleased

#### feature:

#### optimization:

#### bugFix:

#### note:

#### script:

## v0.1.0 - 2026-07-24

#### feature:
1. ...
```

禁止向历史 Release 段追加新 issue。归档前必须显式给出版本和日期，并先
运行 dry-run。

Prerelease 不归档 stable CHANGELOG 段；beta、rc 的变更继续保留在
`Unreleased`，等 stable 发布时一次归档。对应 prerelease 的变更范围写入
GitHub Release Notes。

## 7. 数据、隐私与首次使用

Release Notes 应说明：

- Codex Pulse 读取用户确认的 Codex Home；
- 索引数据库和 preferences 保存在本机私有应用目录；
- UI 不展示完整提示词或回复正文；
- 启用在线额度能力时可能访问相应上游接口；
- 首次索引时长与 Home 规模相关；
- 未签名发行版的打开方式与 signed-notarized 可信分发版本不同。

不得指导用户关闭 Gatekeeper、执行 `spctl --master-disable`，或批量移除
系统隔离属性。
