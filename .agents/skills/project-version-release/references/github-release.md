# GitHub Tag and Release Workflow

## 目录

1. 前置条件
2. Signed tag
3. Public Release
4. 资产验证
5. 公开 Release 读回
6. 禁止操作

## 1. 前置条件

固定以下值，避免命令间 HEAD 漂移：

```bash
REPO=SisyphusSQ/codex-pulse
TAG=v0.1.0
RELEASE_SHA=<40-character-commit-sha>
RELEASE_DIR=".artifacts/releases/$TAG"
```

确认：

- `RELEASE_SHA` 是用户确认的发布 commit；
- 工作树干净，所需验证针对同一 commit；
- tag 和 Release 尚不存在；
- Release Notes、首次安装 DMG、Sparkle App ZIP 和 `SHA256SUMS` 已完成；
- 已明确实际 distribution。stable 默认是 `unsigned`；如果选择
  `signed-notarized`，签名、公证、Gatekeeper、资产和首启 gate 必须全部通过。
  unsigned 资产仍须完成 ad-hoc 完整性、发行构建、测试/验收规则、Sparkle、
  SHA-256 和对应公开读回，Release Notes 必须披露首次打开操作和 macOS 信任边界。

## 2. Signed tag

使用 signed annotated tag：

```bash
git tag -s "$TAG" "$RELEASE_SHA" -m "Codex Pulse $TAG"
git tag -v "$TAG"
git push origin "refs/tags/$TAG"
```

如果 tag signing 不可用，停止并报告。不要自动改用 lightweight tag 或
unsigned annotated tag。

推送后读回：

```bash
git ls-remote --tags origin \
  "refs/tags/$TAG" \
  "refs/tags/$TAG^{}"
```

annotated tag 第一行是 tag object；`^{}` 行的 commit 必须等于
`RELEASE_SHA`。

## 3. Public Release

GitHub Release 创建成功后立即公开，不存在 Draft 中间态。执行创建命令前，必须
确认前置条件中的 tag、Release Notes 和全部发行资产已经完成最终检查，并取得
本次远端发版授权。

Stable：

```bash
gh release create "$TAG" \
  "$RELEASE_DIR/Codex-Pulse-$TAG-macos-arm64.dmg#Codex Pulse 首次安装 DMG (Apple Silicon)" \
  "$RELEASE_DIR/Codex-Pulse-$TAG-macos-arm64.zip#Codex Pulse Sparkle 更新 ZIP (Apple Silicon)" \
  "$RELEASE_DIR/SHA256SUMS" \
  --repo "$REPO" \
  --verify-tag \
  --title "Codex Pulse $TAG" \
  --notes-file "$RELEASE_DIR/release-notes.md"
```

Preview 额外添加：

```text
--prerelease
```

必须使用 `--verify-tag`。不得允许 `gh release create` 隐式创建 tag。

## 4. 资产验证

创建公开 Release 后立即读回：

```bash
gh release view "$TAG" \
  --repo "$REPO" \
  --json tagName,targetCommitish,isDraft,isPrerelease,name,body,assets,url
```

把 GitHub 上的 DMG 与 ZIP 下载到新的临时目录，重新检查：

```bash
shasum -a 256 "Codex-Pulse-$TAG-macos-arm64.dmg"
shasum -a 256 "Codex-Pulse-$TAG-macos-arm64.zip"
hdiutil verify "Codex-Pulse-$TAG-macos-arm64.dmg"
# 将 DMG 只读挂载到新的临时目录后，检查根目录只有 App 与 Applications 链接，
# 再对挂载后的 App 执行与实际 distribution 匹配的 codesign/spctl/stapler 读回。
codesign --verify --deep --strict --verbose=2 "Codex Pulse.app"
spctl --assess --type execute --verbose=4 "Codex Pulse.app"
xcrun stapler validate "Codex Pulse.app"

```

对 `unsigned` 资产，`spctl` 的预期拒绝和没有 stapled ticket 必须如实记录，且
Release Notes 必须包含“不要移到废纸篓”及“系统设置 → 隐私与安全性 → 仍要打开”。
对 `signed-notarized` 资产，`spctl` 与 `xcrun stapler validate` 必须成功；任一
失败都不允许按可信分发发布。

只有配置 GitHub artifact attestations 时才把
`gh release verify-asset` 作为额外证据；它不能替代 SHA-256 和 macOS
签名、公证读回。

## 5. 公开 Release 读回

`gh release create` 成功后，不再执行 `gh release edit --draft=false`。完成资产
验证后，再次运行 `gh release view`，确认：

- `isDraft=false`；
- stable 的 `isPrerelease=false`，preview 的值为 `true`；
- tag、标题、正文和资产名称正确；
- Release URL 可访问；
- 远端 tag peeled target 仍等于 `RELEASE_SHA`。

## 6. 禁止操作

- 不使用 `git tag -f` 移动已推送 tag。
- 不因公开 Release 内容错误自动删除 Release 或远端 tag。
- 不覆盖同名资产来隐藏差异。
- 不把 GitHub 自动生成的 Source code archive 当作 App artifact。
- 不把 tag 或资产命令成功描述成 Release 已完成，必须完成公开状态、正文、资产和 tag 的读回。
- 删除、重打 tag、撤回或替换已发布资产必须单独取得明确授权。
