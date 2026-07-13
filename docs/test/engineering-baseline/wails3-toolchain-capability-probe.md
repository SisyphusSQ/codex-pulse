# Wails3 工具链与平台能力探针

## 当前验证结果

- 记录时间：2026-07-13（Asia/Shanghai）
- 记录目录：仓库根目录、隔离的本地工具目录与临时 spike 目录
- 本轮任务性质：`TOO-242` 工程基线与平台能力探针
- 当前结论：`通过，带 adapter-required 与版本锁定约束`
- 自动化入口：本 runbook 的“主路径”和 `make harness-verify`
- 对应计划 / issue：`TOO-242` / `.agents/plans/2026-07-13-too-242-wails3-toolchain-probe.md`
- 结果说明：Wails3 `v3.0.0-alpha2.117` 已在 macOS arm64 上完成精确版本读回、bindings、开发模式、darwin/arm64 build、ad-hoc package、production bundle 启动、系统托盘示例和 Wails AppCast digest-signature 测试。基础 tray 与附着窗口可直接使用；双行自定义状态项、Sparkle 2 签名互操作和原生 framework 仍需后续 adapter / interoperability 验证。

### 本次执行结果

- 执行时间：2026-07-13
- 执行目录：仓库根目录；临时工程位于已忽略的 `tmp/`；CLI 位于已忽略的 `.agents/runs/`
- 本次结论：`通过`
- 影响范围：下载并编译隔离的 Wails3 CLI，写入 Go/npm 构建缓存，创建临时 vanilla spike，短暂打开 setup 浏览器页、Vite 端口和 macOS 应用 / tray 进程
- 清理结果：所有开发、production bundle 与 tray 进程已退出，9245 端口已释放；临时工程与隔离 CLI 保留在忽略目录供当前 Issue 复核，均不进入提交
- 敏感信息处理：未写入真实凭据、token、cookie、签名身份、用户配置内容、完整本机目录清单或原始 setup 响应；`wails3 setup` 未保存 defaults，也未执行依赖安装、签名或 notarization

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| 环境与精确版本读回 | 通过 | Wails CLI 与 Go module 均为 `v3.0.0-alpha2.117` |
| `setup` / `doctor` | 通过 | desktop required dependencies ready；setup wizard 只检查依赖后关闭 |
| bindings 与 typed events | 通过 | 生成 1 个 Service、1 个 Method、1 个 Event |
| vanilla frontend / Go build | 通过 | 必须遵守 bindings → frontend dist → Go compile 顺序 |
| `wails3 task dev` | 通过 | Wails 进程、Vite 9245 和页面响应真实连通，随后干净退出 |
| darwin/arm64 package | 通过 | 生成 thin arm64 `.app`，ad-hoc 签名通过 strict verify |
| production bundle launch | 通过 | 打包产物独立启动、进程可见且不监听 9245，随后干净退出 |
| system tray / 附着窗口 | 通过 | 同 tag 官方 `systray-basic` 在本机以 accessory 进程运行 |
| 双行自定义 tray 状态项 | adapter-required | 默认 API 无 custom view 和公开 native tray handle |
| Wails AppCast / digest-signature | 通过 | focused integration test 完成下载内容 SHA-256 摘要的 Ed25519 校验 |
| Sparkle 2 `sign_update` 签名互操作 | 未验证 | Sparkle 签原始 archive bytes，与 Wails 当前 digest-signature 语义不同 |
| 原生 Sparkle 2 framework bridge | adapter-required | Wails AppCast provider 不是 Sparkle framework；本卡不交付 native shim |
| Developer ID / notarization | 未执行 | v0.1 本机自用探针不以正式分发为目标 |
| Intel / Universal / 其它平台 | 未执行 | 明确不在 `TOO-242` 范围 |

## 目标

- 验证 Wails3 `v3.0.0-alpha2.117` 是否足以启动后续正式工程骨架。
- 用真实命令区分直接支持、需要 adapter 和本卡未验证的能力。
- 固定可重复执行的版本、命令顺序、失败语义与升级准入规则。
- 成功标准：darwin/arm64 开发与 package 有真实证据，关键平台能力有 findings-first 结论，且临时产物不污染正式骨架。

## 执行副作用

- 写入 `.agents/runs/too-242-wails3-toolchain-probe/` 下的隔离 CLI。
- 写入 `tmp/too-242-wails3-probe/` 下的临时工程与 build 产物。
- 更新 Go module/build cache 与 npm cache，并在临时工程安装 `node_modules`。
- `wails3 setup` 会打开随机 loopback 地址的浏览器页面；`wails3 task dev` 会监听本机 9245 端口并打开应用窗口；tray 示例会创建一个系统托盘项。
- 不写正式应用骨架、业务模块、SQLite schema、Codex home、签名凭据或发布资产。

## 已验证环境

| 项目 | 本次读回 | 结论 |
| --- | --- | --- |
| Host | macOS 26.5.1 / arm64 | 满足 macOS 15+ arm64 目标的向上兼容探针环境 |
| Go | `go1.26.2 darwin/arm64` | 通过 |
| 临时 Go module directive | `1.25.0` | 与目标 Wails tag 的 module directive 一致 |
| Node.js | `v26.0.0` | 通过 |
| npm | `11.12.1` | 通过 |
| Xcode Command Line Tools | 已安装，macOS SDK 26.5 | Wails doctor 判定 desktop required dependency ready |
| Full Xcode | 未安装 / 未激活 | desktop build 不阻塞；iOS、Developer ID 与 notarization 不在本卡验证 |
| Wails3 CLI | `v3.0.0-alpha2.117` | 精确锁定，通过 binary build info 复核 |
| Wails Go module | `v3.0.0-alpha2.117` | 精确锁定 |
| `@wailsio/runtime` | `3.0.0-alpha.97` | 与该 Wails tag 内置 runtime 版本一致；临时 probe 在安装前精确 pin，正式骨架也不得保留 `latest` |

## 能力矩阵

状态定义：

- `supported`：已由同 tag 源码、测试或本机运行证据验证，可进入后续实现。
- `adapter-required`：框架提供部分基础能力，但冻结交互或原生集成需要自有平台 adapter。
- `unverified`：本卡明确未验证，不得从邻近证据推导为通过。

| 能力 | 状态 | 证据 | 后续约束 |
| --- | --- | --- | --- |
| Go service bindings | `supported` | `wails3 generate bindings -clean=true -ts -i` 成功生成 service bindings | 正式骨架提交生成物，并以 Go contract 为真相源 |
| Typed events | `supported` | vanilla 模板注册 `time` event，generator 生成 typed event module | 增量事件仍需后续卡定义业务语义 |
| macOS WebView 窗口 | `supported` | vanilla `dev`、生产 build/package 与无 Vite 的 bundle launch 均成功 | M1-E2 固定正式窗口配置与 frontend lockfile |
| 基础 system tray | `supported` | 同 tag `examples/systray-basic` 编译测试并在本机运行 | 使用 `ActivationPolicyAccessory` 和 template icon |
| tray 附着窗口 / Popover-like 行为 | `supported` | `AttachWindow`、`WindowOffset`、失焦隐藏与原生窗口定位均存在，官方示例真实运行 | 这是 Wails window，不等同于原生 `NSPopover`；交互细节由 M9 验收 |
| 双行额度状态项 | `adapter-required` | `SystemTray` 只公开 label/icon/menu/attach API；darwin 实现把文本写到 `NSStatusBarButton.title`，未公开 native tray handle/custom view | M9-E2/M9-E6 使用 AppKit `NSStatusItem` custom view 或经验证的预渲染 fallback，布局不得泄漏到业务层 |
| 窗口 native bridge | `supported` | `Window.NativeWindow()` 公开平台窗口指针，Wails 自身 darwin dialogs/tray 已使用 | 自有 bridge 必须封装在平台 adapter，Vue 不直接调用 AppKit |
| Wails built-in Updater | `supported` | `pkg/updater` 与 providers 测试通过，官方 updater example 可编译 | 是否采用仍受冻结更新架构约束 |
| Wails AppCast provider + Ed25519-over-SHA256 | `supported` | `TestIntegration_FeedToInstall` 对下载内容计算 SHA-256，并验证该摘要的 Ed25519 signature | 只证明 Wails 自有 AppCast dialect，不声称兼容 Sparkle 2 `sign_update` 产物 |
| Sparkle 2 `sparkle:edSignature` 互操作 | `unverified` | Sparkle 2 `sign_update` 对 archive 原始 bytes 签名，签名对象与 Wails digest-signature 不同 | M10 必须用官方 `sign_update` 真实产物做互操作测试；失败时由 native Sparkle adapter 接管 |
| 原生 Sparkle 2 framework | `adapter-required` | Wails tag 未包含 `SPUUpdater` / `SUUpdater` native framework bridge | M10-E1 交付 Objective-C/cgo shim、framework bundle、错误映射与真实更新 E2E 后才能升级为 supported |
| ad-hoc `.app` package | `supported` | `wails3 package GOOS=darwin ARCH=arm64` 生成 thin arm64 bundle，strict codesign verify 与 production launch 通过 | 仅用于本机自用；不是正式发布证据 |
| Developer ID / notarization | `unverified` | setup 未保存签名 defaults，也未发现本机 identity | 正式发布只能在 release Issue 与用户另行授权下执行 |
| Intel / Universal / Windows / Linux | `unverified` | 未执行 | v0.1 不纳入范围 |

## Alpha 与版本漂移风险

1. Wails3 当前 tag 是 pre-release，API、template 与 CLI 行为仍可能变化；不得使用 `master`、nightly 或无版本 `go install`。
2. vanilla template 的 Go dependency 精确指向 `v3.0.0-alpha2.117`，但 `frontend/package.json` 写的是 `"@wailsio/runtime": "latest"`。本次先读回该事实，再在安装前将临时 manifest 精确 pin 为 tag 内置的 `3.0.0-alpha.97`，避免 registry 漂移。
3. 正式骨架必须把 `@wailsio/runtime` 改成精确 `3.0.0-alpha.97` 并提交 npm lockfile；升级 Wails 时同时读回目标 tag 内置 runtime 版本。
4. template 的 `Info.plist`、CGO flags 与 `MACOSX_DEPLOYMENT_TARGET` 默认是 macOS 12.0；Codex Pulse 冻结为 macOS 15+，M1-E2 必须统一改为 15.0 并重新验证 binary/bundle。
5. Command Line Tools 26 编译 Wails 上游低 deployment target 测试时出现“object file was built for newer macOS version”的 linker warning，但测试、dev 与 package 均成功。正式骨架统一 deployment target 后需要确认 warning 不再来自项目自己的构建面。
6. `wails3 setup` 是实验性的交互式浏览器 wizard，没有稳定的 non-interactive flag。本卡仅验证入口和 required dependency readback；可重复自动检查优先使用 `wails3 doctor -json`。
7. Wails AppCast provider 当前对下载内容的 SHA-256 摘要做 Ed25519 校验；Sparkle 2 官方 `sign_update` 对 archive 原始 bytes 签名。两者不能仅凭同名 `sparkle:edSignature` 推导为互操作兼容。

## 前置条件

1. 当前工作目录为仓库根目录。
2. 目标主机为 macOS arm64，且只验证 desktop 路径。
3. 已安装 Go、Node.js/npm 和 Xcode Command Line Tools。
4. 不需要 Developer ID、notarization credential、Sparkle private key 或 Codex 用户数据。
5. 执行前确认 `.agents/runs/` 与 `tmp/` 仍被 Git 忽略。

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(pwd)}"
WAILS_VERSION="v3.0.0-alpha2.117"
WAILS_RUNTIME_VERSION="3.0.0-alpha.97"
TOOL_ROOT="$REPO_ROOT/.agents/runs/too-242-wails3-toolchain-probe"
SPIKE_ROOT="$REPO_ROOT/tmp/too-242-wails3-probe"
SPIKE_PROJECT="$SPIKE_ROOT/TOO242Probe"
DEV_HOST="${DEV_HOST:-127.0.0.1}"
DEV_PORT="${DEV_PORT:-9245}"

mkdir -p "$TOOL_ROOT/bin" "$SPIKE_ROOT"
export PATH="$TOOL_ROOT/bin:$PATH"

cd "$REPO_ROOT"
git check-ignore \
  .agents/runs/too-242-wails3-toolchain-probe/bin \
  tmp/too-242-wails3-probe
```

预期结果：

- 两个目录均命中 `.gitignore`。
- 变量不包含凭据或外部服务地址。

## 主路径

### 1. 环境与 CLI 精确版本

```bash
uname -m
sw_vers
go version
go env GOOS GOARCH
node --version
npm --version
xcode-select -p
xcrun --sdk macosx --show-sdk-version

env GOBIN="$TOOL_ROOT/bin" \
  go install "github.com/wailsapp/wails/v3/cmd/wails3@$WAILS_VERSION"

WAILS_VERSION_ACTUAL="$(wails3 version 2>&1)"
test "$WAILS_VERSION_ACTUAL" = "$WAILS_VERSION"
go version -m "$TOOL_ROOT/bin/wails3"
wails3 doctor -json
```

预期结果：

- host、Go 和 Wails binary 均为 darwin/arm64。
- 当前 CLI 把 `version` 文本写到 stderr；必须先通过独立赋值使用 `2>&1` 捕获文本并保留 CLI 退出状态，再比较精确版本。
- `go version -m` 显示 CLI module 精确为 `v3.0.0-alpha2.117`。
- doctor 的 required desktop dependencies ready，`diagnostics` 为空；Docker、full Xcode、签名身份不是本卡 desktop blocker。

### 2. 交互式 setup 入口

```bash
wails3 setup
```

操作与预期结果：

- 浏览器打开随机 `127.0.0.1` wizard。
- 只执行 dependency check，确认 Go、CLT 和 npm installed。
- 关闭 wizard，不保存 defaults，不安装 Docker / mobile dependencies，不配置签名或 notarization。
- CLI 以 setup wizard closed 正常退出。

### 3. 初始化临时工程与锁定读回

```bash
test "$SPIKE_PROJECT" = "$SPIKE_ROOT/TOO242Probe"
rm -rf "$SPIKE_PROJECT"

wails3 init \
  -n TOO242Probe \
  -t vanilla \
  -mod github.com/SisyphusSQ/codex-pulse-probe \
  -d "$SPIKE_ROOT" \
  -skipgomodtidy \
  -nocolour

cd "$SPIKE_PROJECT"
go mod tidy

test "$(awk '$1 == "go" { print $2; exit }' go.mod)" = "1.25.0"
test "$(node -p 'require("./frontend/package.json").dependencies["@wailsio/runtime"]')" = \
  "latest"

npm install \
  --prefix frontend \
  --save-exact \
  "@wailsio/runtime@$WAILS_RUNTIME_VERSION"

go list -m github.com/wailsapp/wails/v3
npm ls @wailsio/runtime --prefix frontend --json
test "$(node -p 'require("./frontend/package.json").dependencies["@wailsio/runtime"]')" = \
  "$WAILS_RUNTIME_VERSION"
test "$(node -p 'require("./frontend/package-lock.json").packages[""].dependencies["@wailsio/runtime"]')" = \
  "$WAILS_RUNTIME_VERSION"
test "$(node -p 'require("./frontend/node_modules/@wailsio/runtime/package.json").version')" = \
  "$WAILS_RUNTIME_VERSION"
```

预期结果：

- Go module 是 `v3.0.0-alpha2.117`，`go` directive 是 `1.25.0`。
- 先确认 template 原始 manifest 使用 `latest`，再于首次 npm install 前精确 pin。
- frontend manifest、lockfile 和实际安装 runtime 三者均为 `3.0.0-alpha.97`，与目标 tag 源码内置 runtime 一致。

### 4. bindings、frontend 与 Go 编译顺序

```bash
wails3 generate bindings -clean=true -ts -i
test -f frontend/bindings/github.com/wailsapp/wails/v3/internal/eventcreate.ts

npm run build --prefix frontend
test -f frontend/dist/index.html

go test ./...
```

预期结果：

- generator 成功处理 Service、Method 与 typed Event。
- Vite build 和随后执行的 Go tests 成功。
- 不得把 `go test ./...` 放到首次 bindings/frontend build 之前；见“已知失败与恢复”。

### 5. build、package 与签名读回

```bash
wails3 build GOOS=darwin ARCH=arm64
wails3 package GOOS=darwin ARCH=arm64

file bin/too242probe
file bin/too242probe.app/Contents/MacOS/too242probe
codesign --verify --deep --strict --verbose=2 bin/too242probe.app
codesign -dv --verbose=4 bin/too242probe.app 2>&1
/usr/libexec/PlistBuddy \
  -c 'Print :LSMinimumSystemVersion' \
  bin/too242probe.app/Contents/Info.plist
```

预期结果：

- binary 与 app executable 均为 thin arm64。
- bundle 是 ad-hoc signature，strict verify 成功。
- vanilla template 当前读回 12.0.0；这是 M1-E2 必须改成 15.0 的已知差异，不是最终产品配置。

production bundle 启动烟测：

```bash
bin/too242probe.app/Contents/MacOS/too242probe
```

另开终端验证：

```bash
pgrep -fl 'too242probe.app/Contents/MacOS/too242probe'
if lsof -nP -iTCP:"$DEV_PORT" -sTCP:LISTEN; then
  exit 1
fi
osascript -e \
  'tell application "System Events" to tell process "too242probe" to get {name, background only, visible}'
```

预期结果：

- production executable 进程真实存活，应用可见，且没有 Vite 9245 listener。
- 验证后在原终端发送 `Ctrl-C`，再次执行 `pgrep` 与 `lsof` 确认进程和端口均无残留。

### 6. 开发模式烟测

```bash
WAILS_VITE_PORT="$DEV_PORT" wails3 task dev
```

另开终端验证：

```bash
pgrep -fl "too242probe|wails3 dev|vite.*${DEV_PORT}"
lsof -nP -iTCP:"$DEV_PORT" -sTCP:LISTEN
curl --fail --silent --show-error --max-time 5 \
  "http://${DEV_HOST}:${DEV_PORT}/"
```

预期结果：

- Wails dev 进程、macOS app 进程和 Vite 监听进程同时存在。
- CLI 输出 connected to frontend dev server，HTTP 返回 vanilla 页面。
- 验证后在原终端发送 `Ctrl-C`；Wails 输出 graceful exit，相关进程与 9245 listener 均消失。

### 7. 同 tag 平台能力测试

```bash
WAILS_MODULE_DIR="$(go env GOMODCACHE)/github.com/wailsapp/wails/v3@$WAILS_VERSION"
cd "$WAILS_MODULE_DIR"

go test ./pkg/application ./pkg/updater/...
go test \
  ./examples/systray-basic \
  ./examples/binding \
  ./examples/events \
  ./examples/updater

go run ./examples/systray-basic
```

操作与预期结果：

- package tests 通过，四个示例完成编译检查。
- tray 示例以 macOS accessory app 运行，并创建 template tray item；验证后发送 `Ctrl-C` 清理。

Wails AppCast digest-signature focused test：

```bash
go test ./pkg/updater/providers/appcast \
  -run '^TestIntegration_FeedToInstall$' \
  -count=1 \
  -v
```

预期结果：

- 测试日志明确记录下载内容 SHA-256 摘要的 Ed25519 signature verified。
- 该结果不得写成 Sparkle 2 signature interoperability 通过；本卡没有使用官方 `sign_update` 产物。

### 8. base harness

```bash
cd "$REPO_ROOT"
make harness-verify
```

预期结果：

- base harness 全部通过。

## 已知失败与恢复

| 失败点 | 本次最小复现 | 根因 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| 首次 `go test ./...` 失败 | `pattern all:frontend/dist: no matching files found` | `main.go` 通过 `go:embed` 要求 frontend dist 已存在 | 先生成 bindings，再执行 frontend build，最后运行 Go test |
| `$(wails3 version)` 断言为空 | CLI exit 0 且终端可见版本，但命令替换结果为空 | 当前 CLI 把版本文本写到 stderr，命令替换默认只捕获 stdout | 先独立赋值 `WAILS_VERSION_ACTUAL="$(wails3 version 2>&1)"`，保留 CLI 退出状态后再比较精确版本 |
| 未生成 bindings 就执行 Vite build | typed events plugin 报 event bindings module not found | generator 尚未创建 `eventcreate.ts` | 先执行 `wails3 generate bindings -clean=true -ts -i` |
| `xcodebuild -version` 不可用 | active developer directory 指向 CommandLineTools | 未安装 / 未激活 full Xcode | desktop path 使用 CLT；只有进入签名、notarization 或 iOS 范围时才升级为 blocker |
| 上游 test linker warning | SDK 26 object 比测试的低 deployment target 新 | 上游 cgo target 与当前 SDK 的组合 warning | 保留 warning；M1-E2 统一项目 target 为 15.0 后重跑，不修改上游 module cache |
| 双行 tray 无法由默认 API表达 | 无 custom view / native tray handle | Wails `SystemTray` 抽象只覆盖 label/icon/menu/attach | 转入 M9 的 AppKit adapter，不在本卡修改框架源码 |
| 原生 Sparkle 2 bridge 不存在 | tag 内无 `SPUUpdater` / `SUUpdater` bridge | Wails AppCast provider 与原生 Sparkle framework 是不同实现 | 转入 M10-E1 native shim；正式发布前做真实更新 E2E |
| Sparkle 2 签名互操作未验证 | Wails focused test 签 SHA-256 摘要，Sparkle `sign_update` 签原始 archive bytes | 两个实现的 Ed25519 签名对象不同 | M10 使用官方 `sign_update` 产物做互操作测试；未通过前保持 `unverified` |

## 版本升级准入规则

只有专门的依赖升级 Execution Issue 可以调整本基线；普通功能卡和 release 卡不得顺手升级 Wails。

升级必须同时满足：

1. 使用不可变 tag，不使用 `latest`、master 或 nightly。
2. 读目标 tag release notes、Go module version 和 tag 内 `@wailsio/runtime` version。
3. 同一 PR 精确更新 Wails CLI、Go module、frontend runtime 与 lockfiles。
4. 重跑本 runbook 的 doctor/setup、bindings、tests、dev、darwin/arm64 package、production launch、codesign、tray 与 Wails AppCast focused test。
5. 对 system tray、window/events、Updater/Sparkle adapter 做 findings-first 独立 review。
6. 更新 capability matrix、已知失败和 `docs/harness/project-constraints.md`；任一 required 能力回退则不准入。
7. 升级验证不等于正式发布；release 仍只能在对应 release Issue 与用户明确授权下执行。

## 清理

完成当前 Issue 的 post-merge verify 后执行：

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(pwd)}"
SPIKE_ROOT="$REPO_ROOT/tmp/too-242-wails3-probe"
TOOL_ROOT="$REPO_ROOT/.agents/runs/too-242-wails3-toolchain-probe"
DEV_PORT="${DEV_PORT:-9245}"

cd "$REPO_ROOT"
test -n "$SPIKE_ROOT"
test -n "$TOOL_ROOT"
rm -rf "$SPIKE_ROOT" "$TOOL_ROOT"

if pgrep -fl "too242probe|systray-basic|wails3 dev|vite.*${DEV_PORT}"; then
  exit 1
fi

if lsof -nP -iTCP:"$DEV_PORT" -sTCP:LISTEN; then
  exit 1
fi
```

预期结果：

- 只删除两个已忽略且已用非空变量保护的临时目录。
- 无遗留 probe 进程或 9245 listener。
- 提交版 runbook 与结果摘要保留。

## 结果回写

- `TOO-242`：回写版本、能力矩阵、测试证据、PR、CI、Human Review、merge 与 post-merge verify。
- `TOO-231`：回写 M1 recovery point、adapter 风险与 M1-E2 准入结论。
- `TOO-243`：正式骨架精确锁定 Wails `v3.0.0-alpha2.117` 和 runtime `3.0.0-alpha.97`，提交 lockfiles，并将 deployment target 改为 macOS 15.0。
- `M9-E2/M9-E6`：承接 AppKit 双行状态项 adapter 与真实 tray 交互验收。
- `M10-E1`：承接原生 Sparkle 2 framework shim；不得把本卡 AppCast provider 证据写成 native Sparkle 已完成。
- `M10-E1`：使用官方 Sparkle 2 `sign_update` 生成的真实 archive/signature 做互操作测试；未通过前不得复用 Wails digest-signature 结果作为 Sparkle contract truth。
