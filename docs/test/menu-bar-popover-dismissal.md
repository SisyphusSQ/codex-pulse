# 菜单栏 Popover 外部点击关闭

本 runbook 承接菜单栏 Popover 从 `.transient` 改为 `.applicationDefined` 后的关闭生命周期验证：确定性真值表、monitor 配对、源码门禁，以及真实 Home 下无法自动化的外部点击/失活场景。

## 当前验证结果

- 记录时间：2026-08-24
- 记录目录：docs/test
- 本轮任务性质：实现 + 确定性验证
- 当前结论：`确定性通过；第 6、7 项真实 Home 人工验收通过`
- 自动化入口：`make verify-swift-app` 与 `make verify-architecture`
- 对应计划 / issue：TOO-351 状态栏二次点击应收起 Popover
- 结果说明：TOO-351 本轮补齐状态项 screen-frame 命中与 suppress-next-show：`make verify-swift-app` 确定性测试与 `codex-pulse-app` 构建通过。用户确认真实 Home 下第 6、7 项通过：再次点击状态栏收回且不重开，连点行为符合 toggle。其余 10 项外部点击/失活/VoiceOver 场景仍未执行。

### 本次执行结果

- 执行时间：2026-08-24
- 执行目录：仓库根目录
- 本次结论：`确定性通过；第 6、7 项真实 Home 人工验收通过`
- 影响范围：Swift App 菜单栏 Popover 关闭生命周期、确定性测试、架构门禁、设计契约与本 runbook
- 清理结果：未创建独立 runtime；SwiftPM `app/macos/.build` 缓存按工具链惯例保留
- 敏感信息处理：未写入真实凭据、token、cookie、数据库主机、连接串、行主键、临时目录、完整下载 URL、原始响应或其它机器本地痕迹。

### 当前步骤状态

| 步骤 | 结果 | 备注 |
| --- | --- | --- |
| 前置检查 | 通过 | 新文件与 StatusItemController 契约字符串已核对 |
| 主路径验证 | 通过 | TOO-351 本轮 `make verify-swift-app` PASS，覆盖 global 命中状态项 ignore 与 suppress-next-show 真值表；未跑 `make verify-architecture` 全量 |
| 真实 Home 人工验收 | 部分通过 | 第 6、7 项由用户确认通过；其余 10 项外部点击/失活/VoiceOver 未执行 |
| 清理 | 通过 | 无额外运行时产物 |

## 目标

- 验证目标：点击其它 App、桌面、其它菜单栏 extra 或系统菜单时 Popover 关闭；本 App 主菜单的嵌套 tracking 也能关闭，同时 Popover 内 Picker/上下文菜单不被误关；再次点击状态栏图标关闭且不重开；Popover 内部点击不关闭；Escape 关闭且不吞其它按键；Cmd-Tab / Spotlight / Mission Control / 切换 Space 关闭；modal alert 期间不关闭；monitor 安装与卸载配对，关闭后存活 token 为 0；任一 monitor 注册失败时原子回滚并关闭；不注册 keyboard global monitor。
- 成功标准：`PopoverDismissRule` 真值表全部命中，含 global 命中状态项为 ignore、状态项点击进行中的 dismiss 必须 suppress-next-show；fake monitor 的 show/close 配对与重复 show 不堆积 token，local/global 注册失败均保持 0 个 live token 且没有重复 remove；源码扫描确认 `.applicationDefined`、`popoverDidClose`、`NSMenu.didBeginTrackingNotification`、`sendAction(on: [.leftMouseUp])`、`consumeSuppressNextShow()` 以及 global mask 不含 `keyDown`；`make verify-swift-app` 通过。完整 `make verify-architecture` 的既有 localization blocker 与外部点击关闭的 12 项人工场景仍须分别解决或执行后才能记为通过。第 6、7 项为 TOO-351 必做手测。
- 本 runbook 是给 agent 或工程师直接执行的步骤文档，不是泛化 QA 说明。

## 执行副作用

- 可能写入的本地文件：SwiftPM `app/macos/.build` 缓存；本 runbook 的脱敏结果摘要。
- 可能访问的服务 / 数据库 / 外部系统：无。确定性检查不启动 Helper、不读真实 Codex Home。
- 可能创建的临时数据：SwiftPM 构建缓存。
- 明确不会触达的范围：真实 `${CODEX_HOME:-$HOME/.codex}` Session/JSONL、私有 runtime SQLite、签名/公证/发布、隔离或真实 Home App smoke、live E2E。
- 执行前必须先说明副作用和影响范围；如果影响范围不清楚，先停止确认。

若后续执行 `make verify-live`：会读取真实 Codex Home 下的 Session/JSONL；写入已配置的 mode `0700` 私有 runtime 内 SQLite、preferences 与 App Server 标准 housekeeping；在当前桌面短暂创建窗口、状态栏项与 Popover。本 develop 轮次不执行该入口。

## 前置条件

1. 当前工作目录：仓库根目录
2. 当前分支或版本：实现分支工作树
3. 必需命令：`make verify-architecture`、`make verify-swift-app`
4. 必需配置：macOS Command Line Tools / SwiftPM；不需要完整 Xcode
5. 必需测试环境：确定性测试使用 synthetic/empty 逻辑，不绑定真实 Home

## 测试变量 / 初始化

```bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-/Users/suqing/Coding/golang/00_self/codex-pulse}"
cd "$REPO_ROOT"
git status --short
```

预期结果：

- 工作目录为仓库根。
- 改动仅覆盖 Popover 关闭生命周期及其测试、门禁、文档。

## 主路径

### 1. 前置检查

```bash
test -f app/macos/Sources/CodexPulseAppSupport/PopoverDismissal.swift
rg -n "behavior = \.applicationDefined|popoverDidClose|NSMenu.didBeginTrackingNotification|sendAction\(on: \[\.leftMouseUp\]" \
  app/macos/Sources/CodexPulseApp/StatusItemController.swift
```

预期结果：

- `PopoverDismissal.swift` 存在。
- StatusItemController 使用 `.applicationDefined`、实现 `popoverDidClose` 与本 App 主菜单 tracking 关闭路径，状态项仍在 `leftMouseUp` toggle。

### 2. 执行验证

```bash
make verify-architecture
make verify-swift-app
```

预期结果：

- 架构门禁要求 `PopoverDismissal.swift`、`applicationDefined`，并拒绝 `behavior = .transient`。
- Swift 确定性测试包含规则真值表、modal/smoke 失活护栏、monitor 配对、注册失败原子回滚、禁止 keyboard global monitor、down/up 分工、delegate 关闭路径。
- 既有 Popover 源码扫描与 presentation 测试保持通过。
- 不启动真实 Home App，不跑 live E2E。

未自动化项（必须人工执行，不得写成通过）：

| # | 场景 | 期望 |
| --- | --- | --- |
| 1 | 点击其它 App 窗口 | Popover 关闭，被点 App 获得焦点 |
| 2 | 点击桌面 / Finder 空白处 | Popover 关闭 |
| 3 | 点击另一个菜单栏 extra | Popover 关闭，对方菜单正常展开 |
| 4 | 打开系统菜单栏菜单 | Popover 关闭 |
| 5 | 点击本 App 主窗口 | Popover 关闭，主窗口获得焦点 |
| 6 | 再次点击状态栏图标 | Popover 关闭且不重新打开 |
| 7 | 连续快速点击状态栏图标 5 次 | 终态与点击次数奇偶一致 |
| 8 | Popover 内点击 | Popover 保持打开，动作正常 |
| 9 | Escape | Popover 关闭 |
| 10 | Cmd-Tab / Spotlight / Mission Control / 切换 Space | Popover 关闭 |
| 11 | 截图失败 `.alert` 后点「好」 | 告警期间 Popover 不被 `didResignActive` 关闭 |
| 12 | VoiceOver 打开状态下重复开关 3 次 | 焦点可进入 Popover，关闭后不悬空 |

`nativeAcceptanceEnabled` 下跳过失活关闭：该分支只由纯值测试与人工项 10、11 覆盖，smoke 不覆盖。

### 3. 清理

```bash
# 确定性检查只使用 SwiftPM 缓存，不创建独立 runtime；无需额外清理。
git status --short
```

预期结果：

- 无新增运行时目录或真实 Home 副作用。
- 工作树仅保留本任务相关源码、测试、门禁与文档改动。

## 失败处理

| 失败点 | 停止条件 | 记录方式 | 恢复 / 重跑方式 |
| --- | --- | --- | --- |
| 前置检查失败 | 停止 | 记录 blocker | 修复源码契约后重跑 |
| 架构门禁失败 | 停止 | 记录缺失/禁止 pattern | 同步 check.sh 与源码后重跑 `make verify-architecture` |
| Swift 测试失败 | 停止 | 记录失败用例名，不写路径/token | 修复后重跑 `make verify-swift-app` |
| 人工验收失败 | 停止，不得记为通过 | 记录场景编号与可见结果 | 修复关闭路径后在真实桌面重做该场景 |
| 清理失败 | 停止并升级 | 记录残留范围 | 人工清理后再继续 |

## 结果回写

执行完成后，回写本文前部的 `当前验证结果`、`本次执行结果` 和 `当前步骤状态`。

固定规则：

- 已执行的步骤写真实结果。
- 未执行的步骤显式写 `未执行` 或 `blocker`，不得写成 `通过`。
- 提交版文档只保留脱敏摘要，不写真实凭据、token、cookie、数据库主机、连接串、行主键、临时目录、完整下载 URL、原始响应或其它机器本地痕迹。
- 原始命令输出、敏感信息和机器本地痕迹只放在受控的本地记录或 `.artifacts`，不写入提交版文档。
- 后续同步或 closeout 不得把已脱敏的历史结果摘要删成空模板；有新结果时，用新的脱敏摘要替换或追加。
