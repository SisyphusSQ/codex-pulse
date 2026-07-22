# M9-E4 冻结版 Popover 内容与交互

## 范围与安全边界

- Issue：`TOO-288`
- 分支：`suqing/too-288-frozen-popover`
- 范围：自有 `NSStatusItem` 左键/anchor、420×760 frameless Wails Popover、Quota / Reset Credits / 今日 API 等价成本 / 最近 5 Sessions、刷新与主窗口跳转。
- 不包含：右键菜单、多显示器最终门禁、Intel / Universal、Actions、tag 或正式发布。
- 视觉夹具只使用 synthetic DTO；不读取真实 `HOME`、Codex JSONL、凭据或用户 SQLite。

## 验收矩阵

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 动态额度窗口 | PASS | primary 缺席或 `remainingPercent = null` 时 DOM 均无“5 小时”及 `5 小时 --`；真实 `0%` 保留，primary 恢复有效值后自动出现 5 小时行 |
| 三个查询 region | PASS | Quota、UsageCost、Sessions 独立 query；失败仅降级对应 region；存在缓存时保留上次可信数据 |
| 关闭与恢复 | PASS | Wails `WindowHide` 取消三个 query root 的在途 binding；`WindowShow` 更新本地日 request 时钟并重新 invalidate，跨午夜不会沿用昨日范围，Vue Query cache 不清空 |
| 内容与隐私 | PASS | Reset Credits、估算声明、最多 5 个 safe Session item；无 path、正文、credential 或 opaque cursor 展示 |
| 导航与键盘 | PASS | Session、刷新、打开概览均为原生 button；刷新有显式辅助名称；主窗口 close button 隐藏、`Cmd-W` 转为 hide，named `main` 不被删除 |
| 窗口与 anchor | PASS | anchor 在 AppKit click action 主线程计算后随 callback 传回，不再持 Go mutex 同步等待 main queue；隔离 package 在 `Cmd-W` 后保持进程存活，真实 `AXPress` 产生 on-screen 419×759 Popover，再次触发后 on-screen window=0 |
| 420×760 visual | PASS | 两态 `clientWidth=scrollWidth=420`，console/page error=0；5 个可聚焦 action |

## 可复现命令

```bash
go test -race ./internal/platform/tray ./internal/app
(cd frontend && npm test && npm run build)
go test ./...
go vet ./...
make verify-architecture
git diff --check
```

浏览器视觉验证使用 ignored `.artifacts/runs/too-288-popover-qa/capture.mjs`，短暂启动只监听 `127.0.0.1:9245` 的 Vite server，并把固定 420×760 结果写入：

- [secondary-only.png](evidence/secondary-only.png)
- [primary-restored.png](evidence/primary-restored.png)

该 browser probe 只验证 Vue 内容、尺寸、焦点入口和动态窗口。隔离 native probe 使用 ignored HOME/TMP、package 内真实进程和状态项 `AXPress`，修复了 `NSStatusBarButton` 截获子 view `mouseDown` 导致真实点击不弹窗的问题；修复后由 button target/action 与 custom view VoiceOver press 共用同一有限 callback。可按以下入口重放完整 package、ready、`Cmd-W`、exact PID 存活、CGWindow geometry 与 show/hide 断言；它只终止本脚本启动的 exact child PID：

```bash
bash docs/test/m9-e4/replay-native.sh
```

真实 AppKit 多显示器几何、右键菜单及完整 VoiceOver 回归由 `TOO-290` 覆盖，不从单屏 evidence 外推。

## 当前结果

focused/full Go、race、vet、Popover component、frontend 54 files / 161 tests、production build、harness、version policy、clean arm64 package、synthetic visual 与 isolated native `Cmd-W`/click/show/hide 已通过。独立 implementation review 经两轮修复与复核后 `remaining_findings=0`、`blocking_findings=0`、PASS；不同 subagent 的 Final Scope Review 复核授权路径后同样为 `remaining_findings=0`、`blocking_findings=0`、PASS。PR、自合并和 post-merge 结果只在实际完成后更新为 PASS。
