# Codex Pulse Icons

正式方向：03“深空控制台”。可编辑源是上两级目录的 `codex-pulse-liquid-glass.pen`；本目录 PNG 是从该源稿冻结的产品评审与代码生成输入，不使用外部版权资产。

## 应用图标

- `codex-pulse-app-icon-1024.png`
- `codex-pulse-app-icon-64.png`
- `codex-pulse-app-icon-32.png`
- `codex-pulse-app-icon-16.png`

大尺寸使用完整的光学镜片、额度轨道、终端光标和运行信标；小尺寸使用主动简化后的终端光标版本，避免直接缩放造成糊连。

## 状态栏模板

- `codex-pulse-tray-template-19.png`
- `codex-pulse-tray-template-19@2x.png`

Pencil 导出的模板 PNG 保留轻微中性蓝灰抗锯齿（每个可见像素 RGB 通道差不超过 8）；package 流程按固定 luminance 公式确定性转换为严格 `R = G = B`、保留 alpha 的 19×19 / 38×38 bundle template，再由 macOS 按菜单栏明暗状态着色。不得把彩色应用图标直接缩小替代模板图标。

## 实现说明

### 导出与校验流程

1. 只在 Pencil 的 `09 · Codex Pulse Icon System` 画板、选中组 `03 · 深空控制台 · Selected`（node `wS4GP`）确认品牌轮廓与光学安全区。文件映射固定如下；全部使用透明背景 PNG，目标像素必须精确匹配文件名，不给画板背景、状态条或 compact meters 一起导出：

   | 冻结文件 | Pencil node | 导出规则 |
   | --- | --- | --- |
   | `codex-pulse-app-icon-1024.png` | `03 · 深空控制台 App Icon 1024`（`pwNDQ`） | 直接导出为 1024×1024 |
   | `codex-pulse-app-icon-64.png` | `03 · 会话轨道 64px Icon`（`KaRlZ`） | 直接导出为 64×64，不从 1024 缩小 |
   | `codex-pulse-app-icon-32.png` | `03 · 会话轨道 32px Icon`（`dnqqD`） | 直接导出为 32×32，不从 1024 缩小 |
   | `codex-pulse-app-icon-16.png` | `03 · 会话轨道 16px Icon`（`B1lmvV`） | 直接导出为 16×16，不从 1024 缩小 |
   | `codex-pulse-tray-template-19.png` | `03 Light Template Glyph`（`euBFb`） | 只导出 glyph，透明背景，1x 为 19×19 |
   | `codex-pulse-tray-template-19@2x.png` | 同一 `euBFb` | 同一 glyph 以 2x 导出为 38×38 |

   `03 Dark Template Glyph`（`u3BYG`）只用于深色菜单栏对照，不是第二套源资产。导出后覆盖本目录对应冻结 PNG，不手修派生 `.icns` 或 bundle 文件。
2. 执行 `go run ./build/darwin source docs/design/front/assets/icons`，读回六张 PNG 的固定尺寸、可见内容、透明边缘，以及两张 Tray 源图的中性色边界。
3. `wails3 package GOOS=darwin` 从 1024/64/32/16 PNG 生成十种 AppIcon representation，并把 Tray 源图确定性转成严格灰阶资源。
4. `wails3 task package:verify` 从 app 与 ZIP 读回 `icons.icns`、`codex-pulse-tray-template.png` 和 `codex-pulse-tray-template@2x.png`；任何缺失、尺寸、alpha 或颜色漂移都会失败。

AppIcon 与 Tray 派生物只写入 ignored `bin/`。当前签名仍为 ad-hoc，本流程不创建版本、Developer ID、公证或正式发布产物。
