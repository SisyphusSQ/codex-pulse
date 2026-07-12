# Codex Pulse

> A local-first macOS observability companion for Codex usage, quotas, sessions, and data health.

Codex Pulse 是一个 Codex-only、本机优先的 macOS 菜单栏应用。它计划从 Codex 在本机维护的数据中提取有限的结构化事实，帮助用户查看额度、Token 用量、Session、项目归因、索引进度和数据健康状态。

## 当前状态

项目目前处于产品与技术设计阶段。仓库只包含基础说明、许可证和忽略规则，尚未生成 Wails、Go 或 Vue 应用代码。

## 设计文档

版本化设计资料以 [docs/design](docs/design/README.md) 为唯一入口：模块设计按职责归档在 `details/`，Pencil 源稿、页面预览和图标资产归档在 `front/`。

## v0.1 方向

- 仅支持 macOS，优先完成菜单栏、Popover 和本机工作台体验。
- 只读增量索引 Codex 本地 JSONL，不复制原始对话，不保存完整 prompt、response 或工具输出。
- 使用本地 SQLite 保存支持统计、可信状态和恢复所需的结构化数据。
- 展示 5 小时 / 本周额度、Token 用量、API 等价成本、Session、项目和数据健康。
- 在线 quota 与 reset credits 作为可关闭的可选能力，凭证只在内存中使用。
- 首版只提供简体中文界面，不提供用户数据导出。

## 计划技术栈

- Go + Wails3
- Vue 3 + TypeScript + Vite
- SQLite
- Tailwind CSS v4
- shadcn-vue + Reka UI
- Apache ECharts

后续 Go module 将使用 `github.com/SisyphusSQ/codex-pulse`。进入工程初始化阶段时会固定具体依赖版本并提交 lockfile；当前仓库不追随浮动的 Wails3 `latest`。

## 隐私原则

Codex Pulse 只保存产品功能所需的结构化 metadata 和统计结果。原始 JSONL 继续由 Codex 管理；应用不复制对话正文，不持久化 access token、refresh token、Authorization header、Cookie 或其他凭证。

## License

[MIT](LICENSE)
