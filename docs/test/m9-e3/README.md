# M9-E3 双行额度状态项验证

## 范围

- Quota Current window 到菜单栏 row 的一一映射。
- 当前 primary 缺席或 `remainingPercent = null` 时只显示“本周”，不显示“5 小时 --”；未来 primary 恢复有效值时自动扩展为双行，真实 `0%` 保留。
- trusted / stale / conflict / unavailable / exhausted 与 blocked / degraded 健康点相互独立。
- 读取失败保留最后一次实际存在的可信行，未知值不转换成真实 0。
- AppKit 创建、更新、截图和销毁统一在 main queue；刷新 burst 合并，模型未变化不重绘。
- 原生 update 复制参数后异步投递 main queue，不阻塞刷新 goroutine；Wails `OnShutdown` 在事件循环存活时先停止刷新并移除状态项，避免 in-flight update 与退出互等。

## 自动验证

```bash
go test ./internal/platform/tray ./internal/app ./cmd/traystatusprobe
go test -race ./internal/platform/tray ./internal/app
go test ./...
go vet ./...
make harness-verify
```

## macOS 合成 live probe

```bash
go run ./cmd/traystatusprobe --output docs/test/m9-e3/evidence
```

probe 只注入合成额度与健康状态，不读取真实 HOME、凭据或本地数据库。2026-07-18 在 macOS 26.5.2 arm64、Wails `v3.0.0-alpha2.117` 上得到：

| 场景 | 可访问文本 | 证据 |
| --- | --- | --- |
| 当前 secondary-only | `本周剩余 71%，数据可信`，无“5 小时” | [secondary-only.png](evidence/secondary-only.png) |
| 未来双窗口 + conflict + blocked | `5 小时剩余 55%，本周剩余 71%，数据冲突，健康受阻` | [dual-conflict-blocked.png](evidence/dual-conflict-blocked.png) |
| 无额度窗口 + blocked | `数据不可用，健康受阻`；仍显示品牌 glyph 与独立红点 | [unavailable-blocked.png](evidence/unavailable-blocked.png) |

PNG 为原生 custom view 自身的确定性 bitmap readback，不包含用户菜单栏或桌面内容。冻结稿的 252px Retina 宽度读回为 126pt 视图；实际高度由系统 status bar button 决定。

## 结果

- `secondary-only` 与 null-primary 都只渲染本周，满足当前产品事实；primary 恢复无需迁移，真实 `0%` 不会被隐藏。
- 额度 conflict 使用橙色进度语义，blocked 使用独立红点；辅助技术不依赖颜色识别。
- 原生层不读取 Wails 私有 handle，后续菜单/Popover 卡可以在同一 adapter 边界扩展交互。
