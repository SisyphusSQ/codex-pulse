# Codex Pulse Release Policy

## 目录

1. 产品与版本边界
2. 发布等级
3. 当前 stable blockers
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

Stable 必须满足：

- clean release commit，且发布 commit 已冻结；
- `make verify` 通过；
- production Bundle identifier、显示名、版本和 build number 正确；
- App 内所有可执行代码按 inside-out 顺序使用 Developer ID 签名；
- Hardened Runtime、secure timestamp、notarization 和 stapling 通过；
- 解压后的最终资产通过 `codesign`、`spctl` 和 `stapler`；
- 持久 runtime、Codex Home 首次确认和重启读回闭环；
- 在全新 macOS 用户环境完成首次安装、打开、索引和再次启动验证；
- Git tag、GitHub Release、资产 SHA-256 和发布状态远端读回一致。

### Preview

Preview 使用 prerelease SemVer，例如 `v0.1.0-beta.1`，并在 GitHub 标记
Prerelease。未签名、未公证资产只有在用户明确授权后才能发布；Release
Notes 必须写清风险和 Gatekeeper 打开步骤。

Preview 不能被描述为 stable，也不能把 isolated smoke 当作最终用户验收。

## 3. 当前 stable blockers

检查实际仓库，不要把本节当作永久事实。当前已知 gate 包括：

- `scripts/macos/Info.plist` 是 development identifier 和 development copy；
- `scripts/macos/build-dev-app.sh` 明确只组装 unsigned development App；
- 尚无项目所有的 `scripts/macos/build-release-app.sh`；
- Helper 的 Makefile 构建版本和 App 默认 client version 仍为 `dev`；
- App 默认 runtime 仍使用随机 `/private/tmp/cp-app-*`；
- 首次 Codex Home 确认、持久偏好与普通用户重启读回需要完成产品验收。

只要任一项仍成立，skill 必须输出 `stable_release_ready=false`。
Release Notes 不能代替产品实现或签名公证 gate。

`render-notes --channel stable` 还必须显式选择
`--distribution signed-notarized`。该参数只是防止误操作的分类，不是
签名或公证证据；执行者仍须保存并报告真实 readback。Preview 按最终产物
选择 `unsigned` 或 `signed-notarized`，不能由 channel 推断签名状态。

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
├── Codex-Pulse-<tag>-macos-arm64.zip
├── SHA256SUMS
└── release-notes.md
```

GitHub 自动生成的 `Source code (zip)` 与 `Source code (tar.gz)` 不是 App
安装包，Release Notes 必须明确区分。

## 5. 验证矩阵

| 证据面 | 最低入口 |
| --- | --- |
| 项目完整验证 | `make verify` |
| 真实 Home 产品验收 | `make verify-live` 或等价显式真实 Home 启动 |
| Bundle metadata | `plutil` 读回最终 App 的 Info.plist |
| 嵌套签名 | `codesign --verify --deep --strict --verbose=2` |
| Gatekeeper | `spctl --assess --type execute --verbose=4` |
| 公证票据 | `xcrun stapler validate` |
| ZIP 完整性 | 解压后重跑 Bundle、签名、公证检查 |
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
- 未签名 preview 的打开方式与正式 signed/notarized 版本不同。

不得指导用户关闭 Gatekeeper、执行 `spctl --master-disable`，或批量移除
系统隔离属性。
