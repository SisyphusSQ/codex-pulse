# GitHub Tag and Release Workflow

## 目录

1. 前置条件
2. Signed tag
3. Draft Release
4. 资产验证
5. 发布和读回
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
- Release Notes、App ZIP 和 `SHA256SUMS` 已完成；
- stable 的签名、公证和首启 gate 已通过。

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

## 3. Draft Release

Stable：

```bash
gh release create "$TAG" \
  "$RELEASE_DIR/Codex-Pulse-$TAG-macos-arm64.zip#Codex Pulse for macOS (Apple Silicon)" \
  "$RELEASE_DIR/SHA256SUMS" \
  --repo "$REPO" \
  --verify-tag \
  --draft \
  --title "Codex Pulse $TAG" \
  --notes-file "$RELEASE_DIR/release-notes.md"
```

Preview 额外添加：

```text
--prerelease
```

必须使用 `--verify-tag`。不得允许 `gh release create` 隐式创建 tag。

## 4. 资产验证

创建 Draft 后读回：

```bash
gh release view "$TAG" \
  --repo "$REPO" \
  --json tagName,targetCommitish,isDraft,isPrerelease,name,body,assets,url
```

把 GitHub 上的 ZIP 下载到新的临时目录，重新检查：

```bash
shasum -a 256 "Codex-Pulse-$TAG-macos-arm64.zip"
codesign --verify --deep --strict --verbose=2 "Codex Pulse.app"
spctl --assess --type execute --verbose=4 "Codex Pulse.app"
xcrun stapler validate "Codex Pulse.app"
```

只有配置 GitHub artifact attestations 时才把
`gh release verify-asset` 作为额外证据；它不能替代 SHA-256 和 macOS
签名、公证读回。

## 5. 发布和读回

确认 Draft 的正文、首次打开说明、资产和校验值后，取得发布授权，再执行：

```bash
gh release edit "$TAG" --repo "$REPO" --draft=false
```

再次运行 `gh release view`，确认：

- `isDraft=false`；
- stable 的 `isPrerelease=false`，preview 的值为 `true`；
- tag、标题、正文和资产名称正确；
- Release URL 可访问；
- 远端 tag peeled target 仍等于 `RELEASE_SHA`。

## 6. 禁止操作

- 不使用 `git tag -f` 移动已推送 tag。
- 不因 Draft 内容错误自动删除 Release 或远端 tag。
- 不覆盖同名资产来隐藏差异。
- 不把 GitHub 自动生成的 Source code archive 当作 App artifact。
- 不把“Draft 已创建”描述成“Release 已发布”。
- 删除、重打 tag、撤回或替换已发布资产必须单独取得明确授权。
