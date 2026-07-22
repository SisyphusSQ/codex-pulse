# M9-E6 AppKit Fallback、辅助功能与多显示器回归

## 范围

- Issue：`TOO-290`
- 分支：`suqing/too-290-appkit-fallback-regression`
- 仅补齐探针确认的最薄 AppKit platform observer、辅助功能播报与 Popover 恢复；不复制业务 query/command。
- 不包含 Intel/Universal、非 macOS、原生 NSPopover、真实凭据、Actions 或发布。

## 最终能力边界

| 能力 | 结果 | 证据 |
| --- | --- | --- |
| 多显示器锚点 | PASS | X 按当前屏 `visibleFrame` clamp，Y 从 Cocoa global Y-up 转为 Wails primary-top Y-down；上下错位/负 X 坐标有测试，屏幕过窄或过矮时 fail closed |
| Retina / 深浅色 | PASS | pt 布局不依赖 backing pixel；动态 system color 随 `effectiveAppearance` 重绘，探针在进程内切换 Aqua/DarkAqua 并观察 appearance event |
| VoiceOver | PASS | 原生 `AXMenuBarItem` role、实时 label、help 与 AXPress contract；外部 `AXObserver` 动态观察 title/announcement 通知 |
| 键盘与焦点 | PASS | WindowShow 后 nextTick 先聚焦首个可操作按钮、刷新不阻塞焦点；packaged replay 证明 Escape 由 WebView/native window 双层关闭 |
| wake / Space / display | PASS | 只使用公开 notification center；四种 typed event 均由隔离 probe 观察，并经非阻塞 custom event 交给前端隐藏 Popover |
| 资源释放 | PASS | native callback 在 AppKit main queue 只做非阻塞 event emit；Close 关闭 registration 后以 main-queue barrier 移除 observer、view block 和 status item，不在 main 等待 Wails Window API |
| fallback | PASS | platform event 或 anchor 失败时只隐藏 attached window，durable 主窗口仍可由菜单/Popover route 打开 |

## 可复现命令

```bash
go test -race ./internal/platform/tray ./internal/app
(cd frontend && npm test && npm run build)
go test ./...
go vet ./...
make verify-architecture
git diff --check
bash docs/test/m9-e6/replay-platform.sh
```

脚本使用隔离 `HOME/TMPDIR`，输出到 ignored `.artifacts/runs/too-290-platform-*`。它不会切换真实系统主题、Space、休眠或显示器配置；platform probe 仅在自己的 AppKit 进程内发布同名公共通知并切换自己的 `NSApp.appearance`。真实硬件屏幕数量、frame、visibleFrame 与 scale 只做脱敏 inventory；仅有一块可达屏幕时，不声称完成物理拖屏操作。

## 当前结果

聚焦 race/全量 Go、前端 54 个测试文件 / 165 个测试、typecheck/build、vet、harness、version policy、diff gate、四类 AppKit platform event probe、外部 AX observer 与 packaged accessibility/Escape/Cmd-Q replay 均已通过。两轮独立 subagent review、PR、自合并与 post-merge 结果只在实际完成后更新。
