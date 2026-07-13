# Codex Pulse

> A local-first macOS observability companion for Codex usage, quotas, sessions, and data health.

Codex Pulse 是一个 Codex-only、本机优先的 macOS 菜单栏应用。它计划从 Codex 在本机维护的数据中提取有限的结构化事实，帮助用户查看额度、Token 用量、Session、项目归因、索引进度和数据健康状态。

## 当前状态

项目已进入 M1 工程基线阶段。当前仓库包含可启动的 Go + Wails3 + Vue 3 空应用壳、generated bindings、前后端测试，以及 macOS 15+ arm64 binary、`.app` bundle、正式图标、ad-hoc 签名和单顶层 ZIP 打包入口；SQLite、Codex 索引、quota、Tray、Popover、Updater 和正式页面仍由后续 Execution Issue 实现。

## 设计文档

版本化设计资料以 [docs/design](docs/design/README.md) 为唯一入口：模块设计按职责归档在 `details/`，Pencil 源稿、页面预览和图标资产归档在 `front/`。

## v0.1 方向

- 仅支持 macOS，优先完成菜单栏、Popover 和本机工作台体验。
- 只读增量索引 Codex 本地 JSONL，不复制原始对话，不保存完整 prompt、response 或工具输出。
- 使用本地 SQLite 保存支持统计、可信状态和恢复所需的结构化数据。
- 展示 5 小时 / 本周额度、Token 用量、API 等价成本、Session、项目和数据健康。
- 在线 quota 与 reset credits 作为可关闭的可选能力，凭证只在内存中使用。
- 首版只提供简体中文界面，不提供用户数据导出。

## 当前工程栈

- Go + Wails3
- Vue 3 + TypeScript + Vite
- Tailwind CSS v4
- Vue Router
- Vue I18n
- TanStack Vue Query

Go module 已固定为 `github.com/SisyphusSQ/codex-pulse`。Wails CLI 与 Go module 精确使用 `v3.0.0-alpha2.117`，前端 runtime 精确使用 `3.0.0-alpha.97`；Go 和 npm lockfile 均已提交，不使用浮动的 `latest`。

## 开发环境

- macOS 15+、Apple Silicon arm64
- Go 1.25+
- Node.js 22.12+、npm 10+
- Wails CLI `v3.0.0-alpha2.117`

首次安装精确 Wails CLI 与前端依赖：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
wails3 version
cd frontend
npm ci
cd ..
```

`wails3 version` 必须输出 `v3.0.0-alpha2.117`。若命中其它版本，先修正当前 shell 的 `PATH`，不要用 `latest` 覆盖项目基线。

### 开发、测试与构建

```bash
# 启动 Wails + Vite 开发模式
wails3 task dev

# Go 测试
go test ./...

# 前端检查（从 frontend/ 执行）
cd frontend
npm run typecheck
npm test
npm run build
cd ..

# 运行 base harness
make harness-verify

# 构建 macOS 15 arm64 二进制到 bin/codex-pulse
wails3 task build ARCH=arm64

# 从 clean package output 构建、ad-hoc 签名并验证 bundle 与 ZIP
wails3 package GOOS=darwin

# 复验现有 bundle 与 ZIP
wails3 task package:verify

# 删除 bundle、ZIP 与临时 icns（保留 bin/codex-pulse）
wails3 task package:clean
```

`wails3 package GOOS=darwin` 默认生成 `bin/Codex Pulse.app` 与 `bin/Codex Pulse.app.zip`，版本为未发布基线 `0.0.0`、build number 为 `0`。需要验证版本注入时可显式传入三段数字版本和非负整数 build number，例如：

```bash
wails3 package GOOS=darwin APP_VERSION=1.2.3 BUILD_NUMBER=42
wails3 task package:verify APP_VERSION=1.2.3 BUILD_NUMBER=42
```

package 只接受 macOS/arm64 目标；图标从 `docs/design/front/assets/icons/` 的冻结资产生成，生成物全部位于 ignored `bin/`。当前签名是 `Signature=adhoc`，只用于本机开发和受控自用：它没有 Developer ID、没有 notarization，`spctl --assess` 拒绝是已知且预期的 Gatekeeper 边界。普通开发卡不会触发 tag、GitHub Release 或其它正式发布动作。

## 工程目录

| 路径 | 职责 |
| --- | --- |
| `main.go` | 嵌入 `frontend/dist` 并启动桌面应用 |
| `internal/app/` | Wails application composition、窗口生命周期与最小 `Bootstrap` binding |
| `frontend/src/` | Vue app assembly、Router、I18n、TanStack Query 与空应用壳 |
| `frontend/bindings/` | Wails CLI 生成的 Go/TypeScript contract，不手工修改 |
| `build/` | Wails dev/build/package task、应用 metadata 与 macOS 15+ arm64 bundle/signing 约束 |
| `docs/` | 设计、harness、runbook 与提交版执行真相 |

## 隐私原则

Codex Pulse 只保存产品功能所需的结构化 metadata 和统计结果。原始 JSONL 继续由 Codex 管理；应用不复制对话正文，不持久化 access token、refresh token、Authorization header、Cookie 或其他凭证。

## License

[MIT](LICENSE)
