# 原生 App 壳与 Overview 真实纵向切片验证记录

## 当前验证结果

- 记录时间：2026-07-22 Asia/Shanghai
- 本轮任务性质：生成 + 执行 + 回写
- 对应 Issue：Linear `TOO-313`，Parent `TOO-311`
- 对应分支：`suqing/too-313-native-app-shell-overview`，stacked on `suqing/too-312-swift-transport-spike`
- 当前结论：`当前工具链自动化、隔离 CI smoke、真实 Home native UI live E2E 与独立 review 通过；完整 Xcode/真实 system lifecycle 保留 blocker`
- 自动化入口：`make verify-swift-app`、`make verify-swift-app-smoke-isolated`、`make verify-swift-app-live`
- 结果说明：SwiftPM 下的 AppKit/SwiftUI executable、确定性状态测试、隔离 regression smoke 和复用已配置 runtime 的真实 Home unsigned development App/Helper UI E2E 已通过；`xcodebuild`/XCTest/UI automation 与真实系统 lifecycle live gate 未执行。

### 本次执行结果

- 执行环境：macOS arm64；Apple Swift 6.3.3；`xcode-select` 指向 Command Line Tools。
- 本次结论：App 壳与 Overview 的当前工具链可执行主路径和统一仓库 gate PASS；独立 review 的 blocking findings 已全部修复并复审为 0；完整 Xcode gate 为 `toolchain_blocker`。
- 影响范围：只构建本仓库 Swift/Go 产物，在唯一私有临时目录组装 unsigned development `.app`，并使用隔离数据库与 preferences。
- 清理结果：smoke 退出后 Helper 已受控退出，UDS 不残留，临时 `.app` 与隔离 runtime root 已删除；Swift/Go build cache 和 ignored `bin/` 可保留。
- 敏感信息处理：未写入真实凭据、token、cookie、Codex Home、数据库主机、连接串、内部行主键、临时目录、完整 URL、原始 RPC payload 或日志。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| Toolchain 现场读回 | PASS / blocker | SwiftPM/AppKit 可用；`xcodebuild` 因完整 Xcode 缺失而不可用 |
| Swift App build | PASS | `codex-pulse-app` 在 Command Line Tools 下编译链接成功 |
| 确定性 App tests | PASS | request/state/connection/lifecycle/recovery/stale/error/shutdown |
| Development App 组装 | PASS | `Contents/MacOS` + `Contents/Helpers`；明确 unsigned、不可分发 |
| 真实 App/Helper UI smoke | PASS | window、status item、popover、Overview、auth UDS、Shutdown、cleanup |
| 真实 Home 多页面 live E2E | PASS / 有界恢复 | 非零 Sessions/Projects/Usage、四类详情、七页 render、`unavailable=none`；Home-switch post-commit light-index 不一致需先恢复 |
| 真实 system lifecycle | 未执行 | 隔离空白配置不建立 production lifecycle runtime；不得冒充通过 |
| `xcodebuild` / XCTest / UI automation | blocker | 当前仅有 Command Line Tools，且本卡无权安装/切换 Xcode |
| architecture/full verify | PASS | 架构约束、Proto、Go race/vet、Swift transport 与 App smoke 均通过 |
| 独立 review | PASS | blocking findings 修复后复审为 0；残余风险保持未执行边界 |

## 2026-07-22 真实 Home live E2E

- 用户明确授权本机 development App 直接使用真实 Codex Home，并允许 `codex app-server` 正常写入状态库、WAL、锁和日志。
- 本轮不创建新的验证目录，不复制 JSONL；复用既有私有 preview runtime，UDS、preferences 与 Codex Pulse 派生 SQLite 继续位于该 runtime。
- 正式 Home-switch 已把 preferences 推进到 generation `2`，但 post-commit light-index 状态仍停在旧隔离 Home，导致 Helper 冷启动在 UDS 创建前 fail closed。通过现有 `StartHomeSwitch` 以旧 identity 为 fence、真实 Home identity 为目标恢复派生 metadata 后，冷启动恢复正常。该现象保留为 post-commit recovery gap，不能把 preferences audit `completed` 单独当成完整切换成功。
- App Server metadata provider 脱敏读回 `925` 个唯一 thread、`925` 个合法 rollout、`0` 重复、`0` diagnostic；未记录名称、cwd、rollout 路径或原始 payload。
- 恢复后的真实 native smoke 读回：Overview 非零 session 和 7 日趋势；Sessions/Projects 首屏均非零；Usage 返回已知 API 等价成本和 5 个模型分组；四类详情读取完成；七个 route 全部 render；`unavailable=none`；Shutdown clean。

脱敏稳定输出：

```text
app smoke passed: overview=loaded quota_windows=1 sessions=5 trend_points=7 health=healthy primary_pages=loaded sessions=20 projects=20 sources=6 jobs=0 health_events=1 usage_trend=7 usage_models=5 usage_cost=known quota_windows=1 details_read=4 settings=receipt+readback+conflict+restored unavailable=none ui_pages=7 native_surfaces=window+status_item+popover lifecycle=not_executed shutdown=clean
```

真实 Home live gate 的标准入口为：

```bash
CODEX_PULSE_APP_RUNTIME=<existing-configured-private-runtime> \
  make verify-swift-app-live
```

该入口复用现有 runtime，不创建新 runtime、不清理真实 Home 或派生数据库；原始 App 输出只写 runtime 内 mode `0600` 的一次性文件并在退出时删除。CI 与 `make verify` 继续运行 `make verify-swift-app-smoke-isolated`，其证据只代表确定性 contract regression。

## 目标

- 验证真实 SwiftUI/AppKit application shell 复用现有 Swift transport/HelperSupervisor，经认证 UDS 和生成的 `CoreService` client 加载首个 Overview。
- 验证 UI state mapping 不把 unknown/partial/recovery/error 折成正常或真实零。
- 验证主窗口、基础导航、Toolbar、Menu Bar Item/Popover 与 App terminate path 使用真实 native API。
- 验证 development-only Helper 嵌入/定位、隔离 regression、真实 Home live 数据链、受控 Shutdown 和清理可重复。

## 成功标准

- App 只从生成 client 读取 `Bootstrap`、`UsageCost`、`QuotaCurrent`、`ListSessions`、`HealthProjection`；Swift 不直接读取 SQLite/JSONL。
- loading、normal、partial/unknown、recovery/restart-required、stale、unavailable/error、cancelled 有确定性证据。
- Helper 仍只使用 auth pipe + UDS；无 TCP listener，token 不进入 argv/env/log/file。
- UI smoke 从 Bundle 内 `Contents/Helpers/codex-pulse` 定位 Helper，真实创建 window/status item/popover，完成 Overview 后受控退出。
- 未执行的完整 Xcode、真实系统 lifecycle、签名/公证/发布明确保留为 blocker/范围外项。

## 执行副作用

- DB：隔离 regression 只在唯一 smoke root 创建临时 SQLite并清理；真实 Home live E2E 复用已配置私有 runtime SQLite，不清理或复制用户 JSONL。
- 服务：启动一个临时 Swift App 进程及其 Go Helper 子进程；只监听私有 UDS，无 TCP；结束时执行 `Shutdown` 并读回 Helper 退出。
- 文件：写入 SwiftPM `.build`、Go build cache、ignored `bin/codex-pulse`；临时 development `.app` 和 runtime root 在退出时删除。
- 外部系统：不调用生产 API、不使用签名/公证服务；真实 Home live E2E 会读取 session metadata/JSONL token facts，并允许 App Server 写标准 housekeeping，但不提交用户内容或真实路径。
- UI：`--ui-smoke` 在当前桌面会话短暂创建原生窗口、状态栏项和 Popover，读回后自动关闭。

## 前置条件

1. 当前目录为 Codex Pulse 仓库根目录。
2. 当前分支为 `suqing/too-313-native-app-shell-overview`，且基于 TOO-312 commit `5db90ad`。
3. `swift`、Go、`make`、`plutil` 可用；不要求完整 Xcode。
4. 不设置真实 Codex Home，不使用默认用户数据目录。
5. 执行前确认没有需要保护的并行 App/Helper 测试进程。

## 测试变量 / 初始化

脚本内部使用 `mktemp -d` 创建 `/private/tmp/cp-app-smoke.*`，并将权限设为 `0700`。真实路径只存在于本地运行过程，不写入提交版文档。

预期结果：

- runtime root 唯一、私有且路径足够短。
- App/Helper 数据库、preferences、socket 全部位于该 root。
- token 仅通过继承 pipe 传递。

## 主路径

### 1. Swift App build 与确定性状态测试

```bash
make verify-swift-app
```

当前结果：PASS。

覆盖：

- `OverviewRequestSet` 的 quota clock、按最近活动倒序的 bounded sessions page，以及由真实周额度窗口构造的精确 UTC 半开区间；测试覆盖不依赖 `primary/secondary` 位置、未知 reset 不伪造范围、Token 中文数量级格式化和 Session 未计算成本的明确文案。
- Popover 交互门禁覆盖 `44 pt` 最小命中目标、顶栏图标和紧凑文字按钮的 `28 pt` 视觉面、完整 `contentShape`、整卡 Button、中性 pressed 反馈、禁止自定义蓝色 hover、inactive control state 对比度、扁平子页返回导航和单一标题层级，以及刷新箭头对真实 `isOverviewRefreshing` 状态的原位旋转和无障碍文案绑定；确定性 App 测试同时验证手动刷新会立即进入忙碌态并在请求完成后恢复。静态门禁不能替代真实鼠标、键盘焦点与 VoiceOver 验收。
- 非 Gregorian 系统 Calendar 仍生成 canonical Gregorian local date，避免请求错误世纪。
- 真实零、unknown reason、partial issue、health state 的 presentation mapping。
- `starting -> handshaking -> loadingOverview -> normal`。
- App active、sleep、wake 到现有 lifecycle/stream controller 的确定性调用。
- active 不能越过 Handshake/Bootstrap；active-before-wake 与 startup-during-sleep 会在合法时序重放，recovery 不发 normal RPC。
- recovery Bootstrap 不调用 normal Overview RPC，recovery receipt 映射 `restart_required`。
- refresh error 有 last-known-good 时为 stale；startup error 无 snapshot 时为 unavailable。
- shutdown 发生在 startup `await` 期间时，旧 generation 不得在 `stopped` 后覆盖状态。
- 并发 start、refresh cancel/replacement、Helper process exit、stream terminal failure 不得由旧 generation 覆盖新状态；stale 离线状态可重新启动 Helper 恢复。
- clean Shutdown RPC 与 Helper exit readback；Shutdown RPC 卡住时 deadline 后强制终止且不得被 smoke 计为 PASS。

### 2. Development App 组装

```bash
bash scripts/macos/build-dev-app.sh --output <isolated-development-app-path>
```

当前结果：PASS。

读回：

```text
development app assembled: executable=present helper=present signed=no distribution=no
```

Bundle 形状：

```text
Codex Pulse.app/
└── Contents/
    ├── Info.plist
    ├── MacOS/Codex Pulse
    ├── Helpers/codex-pulse
    └── Resources/
```

这是 development-only unsigned App，不代表正式 Bundle、nested signing、archive 或公证完成。

### 3. 确定性隔离 App/Helper UI smoke（历史与 CI regression）

```bash
make verify-swift-app-smoke-isolated
```

当前结果：PASS。

脱敏输出：

```text
app smoke passed: overview=loaded quota_windows=0 sessions=0 trend_points=0 health=empty primary_pages=partial sessions=0 projects=0 sources=0 jobs=0 health_events=0 usage_trend=0 usage_models=0 usage_cost=unknown quota_windows=0 details_read=0 settings=receipt+readback+conflict+restored unavailable=projects_unavailable ui_pages=7 native_surfaces=window+status_item+popover lifecycle=not_executed shutdown=clean
app smoke cleanup passed: isolated_runtime=yes user_codex_home=no rollout_data=no
```

真实调用链：

```text
NSApplication executable
  -> Bundle Helper locator
  -> HelperSupervisor auth pipe + private UDS
  -> generated CoreService client
  -> Handshake / Bootstrap
  -> QuotaCurrent -> weekly exact UsageCost / ListSessions / HealthProjection
  -> Overview presentation
  -> NSWindow / NSStatusItem / NSPopover readback
  -> Shutdown / Helper exit / UDS cleanup
```

空白隔离数据的业务读回为 quota window `0` 条、session `0` 条；这些是权威空结果/不可用状态的界面输入，不是静态 fixture 页面。`TOO-314` 已将同一个 smoke 扩展到所有主要页面和安全 Settings mutation，细节见 [`native-primary-pages.md`](native-primary-pages.md)。

### 4. Architecture / full repository gate

```bash
make verify-architecture
make verify
git diff --check
```

当前结果：PASS。

- `make verify-architecture`：PASS，覆盖 `SWIFT-002`、direct SQLite/JSONL/TCP 禁止项和关键工具链约束。
- `make verify`：PASS，覆盖 Proto drift、`go test -race ./...`、`go vet ./...`、Swift transport 真实 Helper E2E、App deterministic tests、unsigned development App 组装与 native UI smoke。
- `git diff --check`：PASS（最终文档写回后仍须复跑）。

### 独立 review 与对抗式审查

- 独立只读 review agent 已审查基线 `5db90ad` 到当前全部 tracked/untracked diff；最终 `blocking_findings=0`。
- review 期间发现并已修复：startup active 越过 Bootstrap、旧 refresh generation 覆盖、Helper/stream terminal 未呈现、stale 无恢复入口、Shutdown 无内部 deadline、非 Gregorian 日期、startup sleep/wake 与 active-before-wake 丢事件。
- 主线程对抗式审查另行修复：并发 start、smoke 无外层 timeout、开发 Bundle 任意删除目标、smoke forced shutdown 误报 PASS、`/private/tmp` 存在状态引发的 canonicalization 误拒绝。
- 残余风险：真实 Helper crash/stream exhaustion 尚未做进程故障注入 live E2E；Handshake/Bootstrap/Overview RPC 尚无显式逐调用 deadline；当前仍是 unsigned development bundle。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| Swift build/test | 立即停止 | 记录编译/状态分支的脱敏摘要 | 修复后重跑 `make verify-swift-app` |
| Helper/UDS/Overview | 立即停止 | 记录稳定错误 code；不记录 token/path/payload | 确认隔离清理后重跑 smoke |
| native surfaces | 立即停止 | 标记 window/status/popover 哪项不可用 | 在有 GUI session 的同一工具链重跑 |
| Shutdown/cleanup | 阻塞 | 记录是否残留进程/socket，不写绝对路径 | 人工终止本轮测试进程并清理唯一 smoke root |
| full gate | 不进入 review | 回写失败命令和修正方向 | 修正后从 focused gate 到 full gate 重跑 |
| Xcode gate | blocker | `toolchain_blocker` | 仅在用户授权安装/切换完整 Xcode 后另行执行 |

## 清理

- `run-app-smoke.sh` 使用 30 秒外层 deadline 和 trap；超时先终止 App，随后删除唯一 smoke root 与临时 `.app`。
- `build-dev-app.sh` 只允许写入仓库 `build/dev` 或脚本自己的 `/private/tmp/cp-app-smoke.*` 隔离 root，拒绝跨目录组件，避免把开发组装入口变成任意 `.app` 删除器。
- App 先发 `Shutdown(reason=client_exit)`，等待 Helper exit，再由脚本确认 UDS 不存在。
- ignored Swift/Go cache 和 `bin/codex-pulse` 可以保留以缩短重复验证；不属于提交版产物。
- 不产生 token file、TCP listener、真实 Codex 数据或签名凭据。

## 未执行与范围外

- `xcodebuild test`、XCTest、XCUITest、正式 UI automation：`toolchain_blocker`。
- 真实 OS sleep/wake notification 到 production lifecycle runtime：未执行；当前 smoke 显式 `--skip-live-lifecycle`，只验证 AppKit wiring、Swift controller deterministic path 和既有 Go tests。
- VoiceOver 自动化、Reduce Transparency 视觉截图、键盘焦点巡检：未执行；视图使用系统组件与 accessibility label，但不能写成完整 accessibility gate 通过。
- Developer ID、nested signing、notarization、archive、Sparkle、release：不在本卡授权范围。
- 真实用户 Codex Home：2026-07-22 已执行直接 live E2E；仅保留脱敏计数和状态，不保存用户内容、真实路径或原始日志。

## 提交版信息边界

- 可记录：RPC 名称、contract/version 状态、normal/recovery/partial 等稳定状态、窗口/Popover readback、结果计数、清理结论。
- 不记录：bearer token、metadata、socket/runtime 绝对路径、用户 Home、SQLite 路径、原始 payload、完整日志或命令长输出。
