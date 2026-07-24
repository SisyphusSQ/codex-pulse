# Codex Pulse App Icon

Codex Pulse 使用“A2 · Pulse Core”方向：中心表示 local-first 事实核心，非闭合轨道表示持续索引与动态额度窗口，右上折线表示 Pulse。图形不绑定固定额度时长、百分比或健康状态颜色。

## 源稿

- `CodexPulseDefault.svg`：当前手工 macOS bundle 的 ICNS 输入。
- `CodexPulseDark.svg`：供后续 Icon Composer / Xcode target 的 Dark 外观使用。
- `CodexPulseMono.svg`：供后续 Icon Composer / Xcode target 的 Mono 外观使用。
- `CodexPulse.icon`：Icon Composer 可编辑源稿；当前包含 1 个 `Pulse Core` 组和 6 个图层。
- `Layers/01-Background.svg`～`06-CoreDot.svg`：Icon Composer 的未预裁方形图层，按从后到前的顺序导入。
- `../StatusItem/CodexPulseStatusTemplate.svg`：菜单栏严格单色模板；当前动态额度状态栏继续由 `StatusBarQuotaContentView` 绘制，不替换为静态图标。

三个 AppIcon SVG 中的圆角 clip 仅服务于当前扁平 ICNS 兼容路径。正式接入 Icon Composer 时，应把背景、轨道、Pulse 和核心拆成未预裁圆角的独立方形图层，让系统应用平台 mask、Liquid Glass、阴影和高光。

Icon Composer 图层职责：

| 图层 | 预期材质 |
| --- | --- |
| `01-Background` | 不透明哑光背景，不应用折射 |
| `02-RearTrace` | 低透明度、弱镜面高光、低折射 |
| `03-MainOrbit` | 中等透明度与折射，保留清晰边缘 |
| `04-Pulse` | 较强镜面高光、低模糊，优先保证小尺寸识别 |
| `05-CoreLens` | 中等折射的凸透镜层 |
| `06-CoreDot` | 较高不透明度，保持视觉焦点 |

## 派生物

`CodexPulse.iconset` 由 Default SVG 的 1024×1024 渲染结果派生，包含 `iconutil` 要求的十张标准 representation。派生 PNG 和 bundle 内 `.icns` 不作为可编辑源稿。

开发 App 构建优先使用完整 Xcode 的 `actool` 编译 `CodexPulse.icon`，生成包含动态分层材质的 `Assets.car` 和兼容旧环境的 `CodexPulse.icns`。完整 Xcode 不可用时，构建仍从 `CodexPulse.iconset` 生成静态 ICNS 回退；需要把 Liquid Glass 设为硬门禁时使用：

```bash
scripts/macos/build-dev-app.sh --require-layered-icon
```

生成脚本使用 macOS `sips` 保留圆角外侧与菜单栏模板的透明通道，并通过 Swift 像素门禁检查尺寸、可见/透明像素、透明四角以及菜单栏模板严格灰阶。

重新生成并验证派生资产：

```bash
scripts/macos/render-icon-assets.sh
```
