# 原生 macOS 登录时启动设置验证

## 能力与边界

Settings 的“登录时启动 Codex Pulse”只管理主 App 登录项：

- 使用 `SMAppService.mainApp` 注册和取消注册，不创建后台 Agent、LaunchDaemon
  或登录辅助 Helper。
- 默认不注册；只有用户主动打开开关才调用注册。
- `SMAppService.Status` 是唯一真相，不写 Go Preferences、`CoreService`、SQLite
  或本地布尔偏好。
- App 再次激活或重新进入 Settings 时重新读取状态，以接收用户在系统设置中的外部
  修改。
- 自动化只使用可注入 fake service，不得注册、移除或打开当前用户的真实系统登录项。

## 状态展示

| 系统状态 | 开关 | 页面提示 | 用户动作 |
| --- | --- | --- | --- |
| `notRegistered` | 关 | 未启用 | 可主动打开 |
| `enabled` | 开 | 已启用 | 可主动关闭 |
| `requiresApproval` | 开 | 等待系统批准 | 可打开系统登录项设置，或关闭以取消注册 |
| `notFound` | 关（禁用） | 登录项不可用 | 从完整 App 安装包启动后重试 |

注册或取消后必须立即读回系统状态。若系统没有达到请求状态，开关恢复到读回值，并只
展示稳定的注册／取消失败提示；底层错误、路径、签名详情和平台原始文本不得进入界面。

## 自动化验证

受影响的 Swift 验证入口：

```bash
make verify-swift-app
```

它覆盖：

- 初始化只读取状态，不调用注册或取消；
- 注册成功、取消成功和 `requiresApproval`；
- 注册／取消失败后按系统读回恢复开关；
- `notFound` 与其他状态保持可区分；
- App 激活和 Settings `onAppear` 的刷新 wiring；
- 外部状态漂移覆盖旧页面状态；
- 错误展示不使用平台 `localizedDescription`；
- `CodexPulseApp` 目标可以编译并链接 ServiceManagement。

该命令会写入 SwiftPM `.build` 缓存；不会启动 App，也不会访问真实 Codex Home 或
修改当前用户的登录项。

收口入口：

```bash
make check
```

它还会运行架构、Proto、Go 和全量 Swift 检查，可能写入 SwiftPM/Go build cache
以及仓库 ignored `bin/` 构建产物；测试继续使用 synthetic 数据，不读取真实
Codex Home，不切换系统登录项。

## 系统级手工验收

真实开关验收必须在完整、已安装且满足 ServiceManagement 签名要求的
`Codex Pulse.app` 上由用户主动执行：

1. 打开 Settings，确认初始状态来自“系统设置 → 通用 → 登录项与扩展”。
2. 主动打开开关，确认 `enabled`；若为 `requiresApproval`，按页面按钮进入系统设置
   并批准，返回 App 后确认自动刷新为 `enabled`。
3. 在系统设置中外部关闭，再激活 App 或重新进入 Settings，确认开关恢复为关闭。
4. 主动关闭开关，确认当前 App 保持运行，但下次登录不再自动启动。

自动验收不得执行以上开关动作。正式 Developer ID、notarization、发布包和真实重新
登录验证不属于本次实现。

## 2026-07-29 验证结果

| 证据层级 | 结果 | 边界 |
| --- | --- | --- |
| `make verify-swift-app` | PASS | fake service 状态／错误／漂移测试与 ServiceManagement 编译链接通过 |
| `make check` | PASS | 架构、Proto、Go 非 race、Swift client/App 全部通过 |
| `make verify-swift-app-smoke-isolated` | PASS | unsigned development App、synthetic Home、七页 render、Settings Go 字段改写恢复与 clean shutdown |
| 原生窗口视觉／导航审查 | PASS（`notFound` 状态） | `Cmd+,` 进入 Settings；开关关闭且禁用，页头、即时提交说明、状态和恢复文案无截断或重叠 |

隔离 smoke 读回 `ui_pages=7`、`settings=receipt+readback+conflict+restored`、
`user_codex_home=no`、`rollout_data=no` 和 `shutdown=clean`。它没有触发平台登录项
Toggle；Popover keyboard smoke 只操作受控的项目／复制动作。

视觉审查使用未签名 development bundle，因此系统权威读回为 `notFound`，只能证明
该状态的实际布局与 `Cmd+,` 导航。`notRegistered`、`enabled`、
`requiresApproval`、注册／取消失败和外部状态漂移由注入 fake service 的确定性测试
覆盖；真实系统登录项注册、批准、取消和重新登录均未执行。
