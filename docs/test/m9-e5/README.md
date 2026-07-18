# M9-E5 原生右键菜单、窗口激活与页面跳转

## 范围与安全边界

- Issue：`TOO-289`
- 分支：`suqing/too-289-native-menu-window-routing`
- 范围：自有 `NSStatusItem` 的有限原生菜单、typed app command、主窗口 hide/minimise 恢复、有限 route 深链、权威刷新与安全 shutdown/drain。
- 不包含：Intel / Universal、非 macOS、跨显示器最终回归、真实凭据、GitHub Actions、tag 或正式发布。
- native replay 使用隔离 `HOME/TMPDIR`，不会读取真实 Codex Home、JSONL、凭据或用户 SQLite；只终止脚本启动的精确 PID。

## 验收矩阵

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 原生菜单 | PASS | AppKit 右键菜单只含打开概览、刷新、设置、关于、退出；生产实现不设置 `NSStatusBarButton.menu`，原有 `AXPress`/左键仍只切换 Popover |
| typed command | PASS | native callback 只映射五个 `MenuAction`，未知动作 fail closed，不存在任意 command registry |
| 窗口激活 | PASS | `WindowClosing` cancel+hide 保留 durable `main`；隐藏与最小化后依次 UnMinimise、Show、Focus，macOS Focus 负责应用激活 |
| 页面深链 | PASS | 菜单设置/概览与 Popover session 都经过有限 path normalize；未知、外部和 `/popover` 路径回退 `/overview` |
| 刷新 | PASS | 同时请求 quota/reset credits durable refresh，并失效 quota/index 权威查询；成功或有限失败后进程都保持可恢复 |
| 安全退出 | PASS | 先封闭并等待 lifecycle controls、quota worker、scheduler/coordinator；15 秒超时 fail closed、记录并保留应用，drain 完成后才 Quit |
| 配额兼容 | PASS | TOO-288 的动态窗口行为保持：primary 缺失时不显示“5 小时”或 `5 小时 --`，未来数据恢复后动态出现 |

## 可复现命令

```bash
go test -race ./internal/platform/tray ./internal/app
(cd frontend && npm test && npm run build)
go test ./...
go vet ./...
make harness-verify
git diff --check
bash docs/test/m9-e5/replay-native.sh
```

runbook 会重新 package macOS 15 arm64 ad-hoc `.app`，验证有限菜单构造、`Cmd-W` hide、原有 Popover 主动作保持、精确 PID 存活及无 native crash；typed 菜单映射、设置/概览路由、最小化恢复、双源 refresh、About 与 fail-closed drain 由聚焦 Go/前端测试覆盖。当前机器的状态项位于 camera notch 后方，因此脚本不会把不可达坐标右击伪装成真实菜单回放。AX/应用日志只写入 ignored `.agents/runs/too-289-native-*`。

## 当前结果

聚焦测试与 packaged native 探针已捕获并修复三个真实生命周期缺陷：后台 goroutine 直接调用非线程安全 `App.Show()` 的 `SIGTRAP`、`Cmd-W` 销毁窗口导致后续 Show 无效，以及常驻 `NSStatusBarButton.menu` 接管物理左键。最终实现只使用 Wails 主线程调度的 Window API，在 WindowClosing 阶段 cancel+hide，并仅通过显式右键弹出自有菜单。全量 gate、独立 implementation review 与不同 subagent Final Scope Review 均已通过；PR、自合并与 post-merge 结果只在实际完成后更新。
