# Codex Pulse Icons

正式方向：03“深空控制台”。Pencil 中的矢量与材质结构是源稿，本目录 PNG 用于产品评审和前端 / macOS 实现交接。

## 应用图标

- `codex-pulse-app-icon-1024.png`
- `codex-pulse-app-icon-64.png`
- `codex-pulse-app-icon-32.png`
- `codex-pulse-app-icon-16.png`

大尺寸使用完整的光学镜片、额度轨道、终端光标和运行信标；小尺寸使用主动简化后的终端光标版本，避免直接缩放造成糊连。

## 状态栏模板

- `codex-pulse-tray-template-19.png`
- `codex-pulse-tray-template-19@2x.png`

模板图标为单色资产，实际实现由 macOS 按菜单栏明暗状态着色。不得把彩色应用图标直接缩小替代模板图标。

## 实现说明

正式代码仓库初始化后，应从 Pencil 源稿生成 AppIcon / `.icns`，复核 Apple 安全区、圆角、透明边缘和不同缩放级别；不要把本目录 PNG 当作唯一可编辑源文件。
