# macOS 15+ arm64 Bundle、图标与 ad-hoc 签名 Runbook

## Purpose

- 验证 `wails3 package GOOS=darwin` 能从 clean package output 生成 `bin/Codex Pulse.app` 与 `bin/Codex Pulse.app.zip`。
- 机械读回 Info.plist、thin arm64/minOS 15 Mach-O、正式 `.icns`、ad-hoc signature 与 ZIP 单顶层结构，并对解压后的 app 再验签。
- 明确当前能力只是本机开发/受控自用基线，不是 Developer ID、notarized、Gatekeeper trusted 或 Sparkle release。

## Scope

- 包含：输入校验、生产 binary、bundle、版本注入、冻结图标派生、ad-hoc sign、archive、live launch、clean/rebuild。
- 不包含：Intel/Universal、Developer ID、notarization、Keychain 写入、App Sandbox/hardened runtime、Sparkle/appcast、tag、GitHub Release。

## Preconditions

- macOS 15+ 与 Apple Silicon arm64。
- Go、Node.js、npm 和固定版本 Wails CLI `v3.0.0-alpha2.117` 可用。
- 系统工具 `iconutil`、`sips`、`plutil`、`vtool`、`lipo`、`codesign`、`ditto`、`unzip`、`spctl` 可用。
- 前端依赖可按 committed `package-lock.json` 安装；package 首次执行可能运行 `npm ci`。
- 工作区不要求预先存在 `frontend/dist`、bundle、ZIP 或 `.icns`。

## Test Data / Fixture Strategy

- 图标输入只使用 `docs/design/front/assets/icons/` 已冻结的 1024/64/32/16 PNG。
- 默认版本输入为 `APP_VERSION=0.0.0`、`BUILD_NUMBER=0`；版本注入用 `1.2.3/42` 做非发布测试。
- 所有 build output 位于 ignored `bin/`；临时 iconset、ZIP 解压目录和 probe 结果在退出时删除。
- 不读取或写入 Developer ID、notary profile、Keychain、用户数据或外部发布系统。

## Side Effects

- `wails3 package` 会执行 `go mod tidy`、按需 `npm ci`、生成 bindings/前端 dist，并替换 `bin/Codex Pulse.app`、ZIP 与 `bin/.packaging/`。
- live smoke 会短暂启动 `Codex Pulse` 进程；验证完成后必须终止并确认 9245 无 listener。
- `spctl --assess` 只读取当前 bundle 的 Gatekeeper 评估，不修改系统 trust 或安全设置。
- `package:clean` 只删除 bundle、ZIP 与临时图标产物，保留 `bin/codex-pulse`。

## Verification Steps

### 1. 工具链与 host gate

```bash
test "$(uname -s)" = Darwin
test "$(uname -m)" = arm64
wails3 version
for tool in iconutil sips plutil vtool lipo codesign ditto unzip spctl; do
  command -v "$tool"
done
```

Expected:

- host 为 `Darwin/arm64`。
- Wails 输出精确 `v3.0.0-alpha2.117`。
- 所有 Apple 原生工具均存在。

### 2. Clean package 主路径

```bash
wails3 task package:clean
wails3 package GOOS=darwin
wails3 task package:verify
```

Expected:

- package 先验证 `arch=arm64, version=0.0.0, build=0`，再重建所有 package output。
- 产生 `bin/Codex Pulse.app` 与 `bin/Codex Pulse.app.zip`。
- verify 明确输出 `arm64, minOS=15.0.0, ad-hoc`，并验证解压后的 app signature。

### 3. Info.plist、Mach-O 与签名读回

```bash
plist="bin/Codex Pulse.app/Contents/Info.plist"
binary="bin/Codex Pulse.app/Contents/MacOS/Codex Pulse"

plutil -extract CFBundleIdentifier raw -o - "$plist"
plutil -extract CFBundleShortVersionString raw -o - "$plist"
plutil -extract CFBundleVersion raw -o - "$plist"
plutil -extract LSMinimumSystemVersion raw -o - "$plist"
file "$binary"
lipo -archs "$binary"
vtool -show-build "$binary"
codesign --verify --deep --strict --verbose=4 "bin/Codex Pulse.app"
codesign -dv --verbose=4 "bin/Codex Pulse.app" 2>&1
```

Expected:

- identifier 为 `com.sisyphussq.codexpulse`，版本/build 为 `0.0.0/0`，plist minimum OS 为 `15.0.0`。
- `file`/`lipo` 只报告 arm64；`vtool` 报告 `platform MACOS`、`minos 15.0`。
- codesign 严格验证通过，details 包含 `Format=app bundle with Mach-O thin (arm64)`、`Signature=adhoc`、`TeamIdentifier=not set`。

### 4. 图标与 ZIP 结构

```bash
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

iconutil -c iconset \
  "bin/Codex Pulse.app/Contents/Resources/icons.icns" \
  -o "$tmp_dir/Verified.iconset"

for icon_file in \
  icon_16x16.png icon_16x16@2x.png \
  icon_32x32.png icon_32x32@2x.png \
  icon_128x128.png icon_128x128@2x.png \
  icon_256x256.png icon_256x256@2x.png \
  icon_512x512.png icon_512x512@2x.png; do
  test -s "$tmp_dir/Verified.iconset/$icon_file"
done

archive_entries=$(unzip -Z1 "bin/Codex Pulse.app.zip")
test "$(printf '%s\n' "$archive_entries" | awk -F/ 'NF {print $1}' | sort -u)" = "Codex Pulse.app"
test -z "$(printf '%s\n' "$archive_entries" | awk -F/ '$1 == "__MACOSX" || $NF ~ /^\._/ {print}')"
ditto -x -k "bin/Codex Pulse.app.zip" "$tmp_dir/extracted"
codesign --verify --deep --strict --verbose=4 "$tmp_dir/extracted/Codex Pulse.app"
```

Expected:

- `.icns` 可反解出 10 个标准 representation。
- ZIP 只有 `Codex Pulse.app` 一个顶层；任何层级都不得出现 `__MACOSX` 或 `._*` AppleDouble sidecar。
- 解压后的 app 严格验签仍通过。

### 5. 版本注入与输入负向 gate

```bash
build/darwin/validate_package_inputs.sh amd64 0.0.0 0
build/darwin/validate_package_inputs.sh arm64 "invalid version" 0
build/darwin/validate_package_inputs.sh arm64 0.0.0 -1
wails3 package GOOS=linux
```

Expected:

- 四条命令均非零退出，分别拒绝非 arm64、非法三段版本、负 build number 与非 darwin target。
- 输入 gate 在 `package:clean` 之前运行，失败不得删除已有 bundle/ZIP。

正向注入：

```bash
wails3 package GOOS=darwin APP_VERSION=1.2.3 BUILD_NUMBER=42
wails3 task package:verify APP_VERSION=1.2.3 BUILD_NUMBER=42
plutil -extract CFBundleShortVersionString raw -o - "bin/Codex Pulse.app/Contents/Info.plist"
plutil -extract CFBundleVersion raw -o - "bin/Codex Pulse.app/Contents/Info.plist"
wails3 package GOOS=darwin
```

Expected:

- 自定义读回为 `1.2.3/42`；最后一条恢复普通未发布基线 `0.0.0/0`。
- 这只是 metadata injection 测试，不创建版本段、tag 或 Release。

### 6. Gatekeeper 边界与 live launch

```bash
set +e
spctl --assess --type execute --verbose=4 "bin/Codex Pulse.app"
spctl_rc=$?
set -e
test "$spctl_rc" -ne 0

open -n "bin/Codex Pulse.app"
pgrep -x "Codex Pulse"
```

Expected:

- `spctl` 拒绝 ad-hoc/unnotarized bundle；2026-07-14 实测退出码为 3、结果为 `rejected`。这是本卡冻结边界，不是 package gate 失败。
- `Codex Pulse` 进程稳定运行；WindowServer 能读到标题 `Codex Pulse`、layer 0、alpha 1 的可见窗口。
- production bundle 不启动 Vite，9245 不得存在 listener。

完成后清理：

```bash
pkill -x "Codex Pulse" || true
test -z "$(pgrep -x 'Codex Pulse' || true)"
test -z "$(lsof -nP -iTCP:9245 -sTCP:LISTEN || true)"
```

### 7. 回归与 closeout gate

```bash
go test ./...
go vet ./...
(cd frontend && npm run typecheck && npm test && npm run build)
make harness-verify
git diff --check
python3 .agents/skills/project-version-release/scripts/project_version_release.py check --repo "$PWD" --json
```

Expected:

- Go、Vue、harness、diff 与版本策略全部通过。
- 普通 Execution 只允许 `CHANGELOG.md -> Unreleased`，不得创建版本 archive、tag 或正式 release artifact。

## Cleanup / Reset

```bash
pkill -x "Codex Pulse" || true
wails3 task package:clean
rm -rf frontend/node_modules frontend/dist/assets
```

- `frontend/dist/.gitkeep` 必须保留。
- 如需最终复验，clean 后重新执行 `wails3 package GOOS=darwin`；无需修改源码或用户数据。

## Current Result (2026-07-14)

| 检查 | 结果 |
| --- | --- |
| host / CLI | PASS：macOS 26.5.1 arm64；Wails `v3.0.0-alpha2.117` |
| baseline RED | PASS：实现前 `wails3 package GOOS=darwin` 退出 1，报告 `Task "package" does not exist` |
| default package | PASS：`Codex Pulse.app` 与无 AppleDouble sidecar 的单顶层 ZIP 生成并复验 |
| metadata | PASS：`com.sisyphussq.codexpulse`、`0.0.0/0`、minOS `15.0.0` |
| binary | PASS：Mach-O thin arm64；`vtool minos 15.0` |
| icon | PASS：冻结源文件 hash 前后不变，10 个标准 representation 可读回 |
| signature | PASS：strict verify；`Signature=adhoc`、无 TeamIdentifier |
| version injection | PASS：`1.2.3/42` 可注入/复验并恢复默认 |
| negative input | PASS：非 darwin、amd64、非法版本、负 build number 被拒绝 |
| Gatekeeper | EXPECTED REJECT：`spctl` exit 3，未使用 Developer ID/notarization |
| live E2E | PASS：进程稳定；CoreGraphics 读到 1097×705 可见窗口；无 9245 listener；清理后无残留进程 |

## Known Limits / Residual Risks

- ad-hoc signature 只证明本地 bundle 完整性，不建立发布者身份和 Gatekeeper trust；正式分发仍需后续 release Issue 的 Developer ID/notarization 专门门禁与用户授权。
- 当前不启用 App Sandbox、hardened runtime 或 entitlements；本卡没有声明这些尚未实现的安全能力。
- `System Events` 在当前执行环境返回 process visible 但 window count 0；CoreGraphics/WindowServer 能读到真实 layer-0 窗口，因此 live 证据以 CoreGraphics 为准。
- ZIP 结构为后续 Sparkle 的输入基线，但尚未验证 Sparkle EdDSA、appcast、N-1 更新或签名互操作。
- Wails3 仍为 Alpha；M1-E4 需要把稳定 package checks 接入统一 CI/本地 gate。

## Rollback Unit

- 回退 `Taskfile.yml`、`build/darwin/**`、README/runbook/constraint 变更。
- 删除 ignored `bin/Codex Pulse.app`、ZIP 与 `bin/.packaging`；不涉及 DB、用户目录、Keychain 或外部发布状态。
