# Credits：客户端、中心与版本上线流程

2026-11-11。适用 TOO-702 及本轮 Web/Storybook 调整。本文是待执行的上线流程，本轮仅完成开发产物构建，没有提交、打 tag、创建 Release、替换安装版或部署中心。文中尖括号参数和示例绝对路径均须按现场替换，不能原样执行。

## 已确认的当前状态

- 正式 `server/web/src/pages/Quota.tsx` 已把计费 Credits 放到节奏/用量图区块之后；Storybook 复用该页面，不维护另一份布局。
- `make -C server build` 已成功，`server/web/dist` 及内嵌 Web 的 `server/bin/codex-pulse-server` 已更新；版本为 `dev-d15da7f-dirty`，属于本机开发产物，不能冒充正式发布候选。
- 2026-11-11 通过 GitHub CLI 读回最新公开正式版为 [v0.17.2](https://github.com/SisyphusSQ/codex-pulse/releases/tag/v0.17.2)。本机安装版为 0.17.2 / macOS build 75，固定 Sparkle feed 当前最大 build 为 75；Windows 公开 ZIP 为 build 1014。发版当天必须重新读回，不能永久使用这些值。
- 当前源码位于 `suqing/too-702-credits-balance`，工作树未提交；尚不满足项目“clean main + 与 origin/main 一致 + 冻结 SHA”的发布条件。当前仓库没有 `.github/workflows/ci.yml`，打 tag 不会自动完成构建、Release 或部署。

## 发布单元与顺序

| 对象 | 替换内容 | 保留内容 |
| --- | --- | --- |
| macOS 客户端 | 整个 `Codex Pulse.app`，其中包含 Swift App、同版 Go Helper 与 Sparkle | 真实 Codex Home、私有 runtime、SQLite、偏好及上报队列 |
| Windows 客户端 | 同一个发行包中的 App、Helper、PackageHost 及自包含依赖 | 真实 Home、runtime、偏好、凭据和未确认队列 |
| Server | 对应目标 OS/CPU 的完整单体二进制；Web、API 和迁移 SQL 都在其中 | 当前私有配置、数据库、配对关系、HTTPS 入口和服务身份 |
| Storybook | 仅开发评审工具 | 不作为中心前端部署到生产 |

客户端与中心从同一个冻结 commit 构建，使用同一产品版本；它们的进程、数据库和安装方式独立。App 与 Helper 不拆开发版；Web 不单独拷贝到 Nginx 目录。

推荐顺序：完成源码及变更记录 → 冻结 commit → 构建并检查全部候选资产 → signed tag → 备份并升级 Server → 公开 GitHub Release → 更新固定 feed → 分批替换客户端。

公开客户端之前先让新中心运行就绪。当前 Helper 会探测 `/api/v1/sync` 的 `paid_credits` 能力，旧中心没有该能力时不发送新字段；这用于分批升级，不表示旧中心已支持余额。

## 一、确定版本并冻结源码

下一版可考虑 `v0.18.0`；以下版本/build 只作示例，未预留或发布。先读回远端 Releases 和全部 feed，确认 tag 未占用、macOS build 高于 stable/prerelease 全部历史。Windows build 按自己的更新源及发行历史递增，不能直接照抄 macOS build。

```sh
PULSE_REPO=SisyphusSQ/codex-pulse
PULSE_VERSION=0.18.0
PULSE_TAG="v$PULSE_VERSION"
PULSE_MAC_BUILD=76
```

1. 审阅本分支全部改动，将实际变更记录在 `CHANGELOG.md / Unreleased`；正式 stable 发布时才归档目标版本、真实发布日期和 SQL 交付档案。Preview 保留 Unreleased，不伪造 stable 记录。
2. 完成开发阶段必要验证并保存已有输出。按当前协作约定，进入提交/发版收尾后不重复运行单测、E2E、lint、check 或 verify；没有证据的项目写“未验证”，不能推断通过。
3. 在用户授权的提交/合并步骤后，将目标变更合入 `main` 并同步远端。发布只从 clean main 开始，不自动 stash、reset 或覆盖未提交改动。
4. 在 clean main 执行 `git pull --ff-only origin main`，确认 HEAD 等于 origin/main；冻结后任何源码变化都要重走该步骤。

```sh
git status --short
git branch --show-current
git pull --ff-only origin main
git rev-parse HEAD
git rev-parse origin/main
PULSE_RELEASE_SHA=$(git rev-parse HEAD)
PULSE_RELEASE_DIR="$PWD/.artifacts/releases/$PULSE_TAG"

python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --version "$PULSE_TAG" --channel stable --json
```

`check` 是发布来源检查，不是功能测试通过证明。CHANGELOG 或 SQL 归档若产生新提交，应先合并、同步，再冻结最终 SHA；不能拿归档前的 SHA 构建一部分资产。

## 二、构建发行资产

### macOS

沿用正式 feed 与已有 Sparkle 公钥，不重新生成信任根。公钥文件可公开；私钥不得放 argv、环境变量、仓库或日志。以下公钥路径为操作者替换的示例。

```sh
scripts/macos/build-release-app.sh \
  --version "$PULSE_VERSION" \
  --build-number "$PULSE_MAC_BUILD" \
  --sparkle-feed-url https://github.com/SisyphusSQ/codex-pulse/releases/download/updates/appcast.xml \
  --sparkle-public-key-file /absolute/public/codex-pulse-sparkle-public.key \
  --skip-app-tests
```

`--skip-app-tests` 使用脚本的正式跳过入口，符合提交/发版收尾不重复测试的约定。构建仍检查 Bundle、App/Helper 版本、架构、签名、ZIP 与 DMG；这不是完整产品验收。

输出在 `.artifacts/releases/<tag>/`：首次安装 DMG、Sparkle exact ZIP、`SHA256SUMS`。当前脚本提供 ad-hoc 签名，未做 Developer ID 签名和 Apple 公证，Release Notes 必须如实说明 unsigned 分发及“隐私与安全性 → 仍要打开”，不能声称 Gatekeeper 可信。不要将 `build/dev` 的开发 App 当成正式资产。

### Server

在目标架构相同的构建机生成完整包。当前机器为 Darwin arm64，以下命令适合相同架构的 macOS 中心：

```sh
umask 077
make -C server package-center \
  VERSION="$PULSE_VERSION" \
  PACKAGE_DIR="$PULSE_RELEASE_DIR/center-darwin-arm64"
tar -C "$PULSE_RELEASE_DIR" -czf \
  "$PULSE_RELEASE_DIR/Codex-Pulse-$PULSE_TAG-server-darwin-arm64.tar.gz" \
  center-darwin-arm64
```

目录必须全新；`package-center` 拒绝覆盖已有目录。包只包含二进制、公开配置模板和部署工具，不包含 `*.local.yml`、账密、数据库或证据。`build`、`release`、`package-center` 包含 Web 构建，不运行测试；不要使用捆绑测试的 `make all`。

Linux/其他 CPU 使用现成的 `release-linux-amd64`、`release-linux-arm64` 等目标，产物位于 `server/bin/<os>-<arch>/`；需要同样的部署目录结构时，将该目标二进制与公开 deploy/scripts/模板组包。必须核对目标平台，不把 Mac 包拷到 Linux 后当作可运行服务。首次部署的 service、HTTPS 域名及配置按 [部署说明](../../server/deploy/README.md) 设置。

若一并向 GitHub 交付中心包，将它的 SHA-256 加入最终 `SHA256SUMS`；发布前完成摘要，不在公开后改写同名资产。

### Windows（如本版一起发布）

在 Windows 11 x64 构建环境，使用同一个冻结 SHA、已有 package policy 和签名密钥，通过 `scripts/windows/package.ps1` 构建；不能在这台 Mac 上声称已构建验收。

```powershell
./scripts/windows/package.ps1 -Version <同一产品版本> -BuildNumber <Windows递增build> `
  -Channel stable -PolicyPath <已有私有policy.json> `
  -SigningKeyName <已有CNG包签名密钥名> -OutputDirectory <全新输出目录>
```

此处尖括号是待替换参数，不能原样执行。若使用生产 policy，脚本另要求 `-CodeSigningCertificateThumbprint`，且公信任要独立读回；不能为了打包临时换一把密钥导致旧安装无法接受包。

产物为 `Setup.exe`、`release.json`、`codex-pulse-<build>-win-x64.zip` 和摘要。当前公开 v0.17.2 的 Windows Notes 明确为开发密钥签名、未做 Authenticode，内置更新源仅供本地开发；现有公开用户应按手动下载整包更新，不能承诺 GitHub Release 发布后 Windows 会自动更新。正式 HTTPS JSON feed、公钥/证书和 Authenticode 如需上线，应按 [Windows 分发说明](windows-install-update.md) 单独完成，不能套用 Sparkle appcast。

## 三、准备 Notes 与 signed tag

Release Notes 完整说明 Credits 余额、图下位置、实际消耗仍未知、客户端/中心结构升级和回滚边界；列出真实验证范围。Notes 模板必须清除所有占位符，不把 Storybook 合成数据当成真实采集验收。

发布资产齐备、摘要一致、最终来源检查通过后，使用项目 release-plan 输出并审阅命令：

```sh
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  release-plan --repo "$PWD" --version "$PULSE_TAG" \
  --channel stable --release-sha "$PULSE_RELEASE_SHA" --json

git tag -s "$PULSE_TAG" "$PULSE_RELEASE_SHA" -m "发布 Codex Pulse $PULSE_TAG"
git tag -v "$PULSE_TAG"
git push origin "refs/tags/$PULSE_TAG"
git ls-remote --tags origin "refs/tags/$PULSE_TAG" "refs/tags/$PULSE_TAG^{}"
```

远端 peeled commit 必须等于冻结 SHA。签名不可用就停止，不降级为 lightweight/unsigned tag，不强推已有 tag。**这些是待授权执行的命令，本轮未执行。**

## 四、先升级 Server

### 升级前

保留当前二进制/版本目录、绝对路径私有配置及数据库一致备份，确认备份可恢复。服务器地址、架构和当前服务管理方式以现场为准；跨自有机器先用 `tailscale status --json` 读取目标地址，沿用 SSH 用户/密钥及严格主机校验。

MySQL 备份使用部署包中的工具，密码留在 0600 私有 client.cnf：

```sh
bash server/scripts/mysql-backup.sh backup \
  /absolute/private/mysql-client.cnf <当前中心数据库名> \
  /absolute/private/backups/<全新备份目录>
```

SQLite 开发中心使用当前运行版本及同一配置的 `db backup <全新目录>`。不能在运行中只复制 SQLite 主文件而漏掉 WAL，也不能在迁移 DDL 并行时做一致性承诺。不要清库或重复 `db init` 来“安装新版本”。

### macOS 中心替换

在目标机展开包后，用既有配置和相同 environment 安装到新 release-id：

```sh
python3 <新部署包>/scripts/macos-service.py install \
  --environment production \
  --package <新部署包绝对路径> \
  --config <现有私有production.local.yml绝对路径> \
  --release-id <产品tag-commit短SHA>
python3 <新部署包>/scripts/macos-service.py status --environment production
```

`install` 会复制到新版本目录、停止旧服务、等待旧进程退出、切换 current 并启动；不是原地覆盖正在运行的二进制，不需要卸载旧目录。已有部署若使用非默认 `--root`，各命令必须沿用原值。默认根目录为 `~/Library/Application Support/Codex Pulse Center`，用户 LaunchAgent 仅在该用户登录后运行。

Linux 沿用现有 systemd 服务：先正常停止旧服务，换到新版本目录/二进制，再启动。service 示例位于 `server/deploy/`；不要未经现场确认就照抄示例 `/opt`、用户或配置路径，更不重建已有数据库、换端口或改域名。

### 结构与读回

本轮中心 schema 为 **v8**，在 HTTP 监听前按已登记摘要从旧版本追加 `pulse_paid_credits` 及索引；保留旧 SQL 字节与已有事实。迁移失败不会开始监听。MySQL DDL 可能部分提交，不能假设整体回滚；修复实际故障后按迁移器重入，不手改版本标记。

正常升级保留配对关系，通常不用重新 bootstrap 或让所有客户端重配。需要读回：实际二进制版本/commit、`/health`、`/ready`、同配置 `db check`、管理页面 `/api/v1/version` 的 schema=8、既有账号/用量、Credits 在图下、原有上报是否续传。接口认证沿用现有浏览器/collector，不把凭据拼进 URL 或命令记录。

`/ready` 只证明依赖就绪，不能代替真实页面和余额验收。新的中心先运行，再发布客户端更新通知。

## 五、公开 GitHub Release

确认中心已读回且最终发行资产齐备，再直接创建公开 Release，不经过 Draft，不让 GitHub CLI 隐式创建 tag。以下列出 Mac 与同架构中心包；若本版不发布某个平台，删去对应参数并在 Notes 明说。

```sh
gh release create "$PULSE_TAG" \
  "$PULSE_RELEASE_DIR/Codex-Pulse-$PULSE_TAG-macos-arm64.dmg" \
  "$PULSE_RELEASE_DIR/Codex-Pulse-$PULSE_TAG-macos-arm64.zip" \
  "$PULSE_RELEASE_DIR/Codex-Pulse-$PULSE_TAG-server-darwin-arm64.tar.gz" \
  "$PULSE_RELEASE_DIR/SHA256SUMS" \
  "$PULSE_RELEASE_DIR/release-notes.md" \
  --repo "$PULSE_REPO" --verify-tag \
  --title "Codex Pulse $PULSE_TAG" \
  --notes-file "$PULSE_RELEASE_DIR/release-notes.md"
```

Windows 一起发时，将准备好的三项安装文件及摘要作为同一 Release 的资产明确列入；不要用通配符把私有目录或 appcast 候选误传。Preview 需要 prerelease SemVer，并额外传 `--prerelease`；它与稳定版不能混用。

立即读回公开状态、正文、资产名及 tag；另下载到全新目录比对 SHA-256、Bundle 与签名。只创建成功不等于发布验收通过。版本资产发布后不覆盖同名文件、不移动 tag；修复应出新版本。

```sh
gh release view "$PULSE_TAG" --repo "$PULSE_REPO" \
  --json tagName,targetCommitish,isDraft,isPrerelease,name,body,assets,url
```

## 六、最后更新 macOS 固定 appcast

GitHub Release 公开不等于已安装 Mac 会看到更新；Sparkle 读取的是固定 `updates/appcast.xml`。

先保存当前 feed，用同一 Sparkle 私钥从 stdin 签已公开的 exact ZIP，再合并原 stable/prerelease 历史生成新 feed：

```sh
gh release download updates --repo "$PULSE_REPO" --pattern appcast.xml \
  --output "$PULSE_RELEASE_DIR/appcast-before.xml"

scripts/sparkle/generate_appcast.sh \
  --version "$PULSE_VERSION" --build-number "$PULSE_MAC_BUILD" --channel stable \
  --archive "$PULSE_RELEASE_DIR/Codex-Pulse-$PULSE_TAG-macos-arm64.zip" \
  --archive-url "https://github.com/$PULSE_REPO/releases/download/$PULSE_TAG/Codex-Pulse-$PULSE_TAG-macos-arm64.zip" \
  --existing "$PULSE_RELEASE_DIR/appcast-before.xml" \
  --output "$PULSE_RELEASE_DIR/appcast.xml" \
  < /absolute/private/existing-sparkle-private.key
```

脚本会用 ZIP 内公钥再次验签。私钥路径是本机私有输入；不要展示内容、开启 shell trace 或把它复制到发布目录。候选 XML 与 ZIP/版本/channel/长度/签名确认后，才更新固定 feed。

当前 feed 托管为 GitHub Release 同名 asset；`gh release upload --clobber` 实际是**先删除再上传**，不是原子覆盖，失败可能丢失旧 asset。因此先保留并校验旧 feed，可另存一个不重名的远端备份，再明确替换固定 `appcast.xml` 并立即读回；失败用同一备份恢复固定入口。该变更对象只限可变的 `updates/appcast.xml`，不允许覆盖版本 Release 资产。若要求零 404 的严格原子发布，需要另行设计支持原子切换的固定 HTTPS 托管，不能把现有命令描述为已具备该能力。

```sh
# 仅在已明确授权替换固定更新源时执行。
gh release upload updates "$PULSE_RELEASE_DIR/appcast.xml" --repo "$PULSE_REPO" --clobber
```

从固定公开 URL 重新下载核对 XML 和 enclosure，确认 stable item 不带 prerelease channel，global build number 递增；保留 Preview 历史。固定 feed 必须最后更新，避免客户端拿到不可下载的包。Windows 的固定 JSON feed 是另一条发布链，不能上传 appcast 代替它。

## 七、替换客户端

### macOS：手动替换

1. 从该 Release 下载 DMG，核对摘要；不要下载 GitHub 的 `Source code`。
2. 用 App 正常退出菜单，等待托管 Helper clean shutdown。替换前保留旧 App 以及退出后私有 runtime 的一致备份，不读取/改写真实 Codex Home。
3. 将 DMG 中整个 `Codex Pulse.app` 拖入 `/Applications` 替换；不只替换内嵌 Helper，不覆盖仍在运行的 Bundle。
4. 启动 `/Applications` 中的新 App，沿用原 runtime、偏好、已确认 Home 和中心配对。客户端 SQLite 自动升级到 **v39**。
5. 读回 App/Helper 同版、确认 Home 物理身份、余额/历史和上报到中心的最后成功状态；中心账号页再确认 Credits 的图下布局；保留未确认队列，不因“页面为空”删除 runtime。

同一 signing/bundle/feed 信任链已包含 Sparkle 的旧 App，也可用“检查更新…”完成下载、验签、安全退出、整体替换和重启；但本轮没有重新完成旧版→候选版的真实升级验收，不能仅凭 feed 已上传宣称完成。unsigned 发行版可能需要系统首次打开确认，不关闭 Gatekeeper、不批量清除隔离属性。

### Windows：手动替换

将同一 Release 的 `Setup.exe`、`release.json`、`codex-pulse-<build>-win-x64.zip` 放到同一目录，运行该 Setup 的安装/更新入口；沿用原安装根与 runtime。不要手工覆盖单个 EXE、混用两版 Helper，或删除已有 current/previous/journal。程序目录与数据目录独立，正常更新保留用户数据。正式自动更新需独立 feed 和信任链支持，目前公开版本仍按手动更新说明交付。

## 八、失败与回滚

- **只有页面变化、结构兼容**时才考虑回切旧完整服务目录。本轮有本机 v39 / 中心 v8，不能笼统地说“换回旧 App/Server 就能回滚”。
- 中心结构不兼容：保留当前库和新产生事实，停止服务，用对应旧备份恢复到新的空库/路径，再与匹配旧二进制成对切换。恢复工具会撤销备份里的旧授权，需要重新配对；备份后的已确认事实要另行对账，不能用旧库覆盖新库冒充无损回退。
- 客户端结构不兼容：保留当前 runtime 和队列，使用匹配版本的备份及恢复流程；不让旧客户端直接打开未来 schema，也不删真实 Home。Windows 的显式 rollback 会核对数据兼容声明。
- Release/tag 已公开则不强制改写；需要修复时发布新版本。feed 出错先恢复已保存的旧 feed；不能绕过 ZIP/包签名或换公钥强装。

## 本轮交付与未验证项

已完成正式页面调整、最新 server 单体构建、上线手册和 Unreleased 记录。此前 Credits 专项真实 Home RPC/原生卡片/本地中心上报验收及 Web/Storybook 聚焦检查已记录；本轮按约定没有重复运行测试。

尚未完成：全页面 App smoke 曾超时；架构检查因 CI 文件缺失停止；Windows 新改动与独立 MySQL v8 实机、N-1 更新、签名公证、公开 feed 替换和生产上线均未验收。实际消耗仍为 `source_unavailable`，不能在 Release Notes 宣称已有精确扣费统计。

更多依据：[项目发布 Skill](../../.agents/skills/project-version-release/SKILL.md)、[中心运维](../../server/docs/test/operations.md)、[客户端更新设计](../design/details/updates-and-release/README.md)、[Credits 验收](paid-credits.md)。
