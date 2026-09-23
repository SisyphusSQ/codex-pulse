# Codex CLI 与 Node 兼容性验收

本 runbook 对应 TOO-468。目标是验证 Codex Pulse 从图形界面启动时，可以用实际 App Server 契约识别当前账号，不因 CLI 版本号或预发布标记拒绝可用版本。

## 自动化验证

使用 synthetic Home 与脚本化 CLI，不能把结果记作真实账号验收：

```bash
go test ./internal/codex/appserver ./internal/codex/quota ./internal/app -count=1
```

聚焦场景包括：

- App 内置 CLI 优先；它缺少 `account/rateLimits/read` 时回退到同一 confirmed Home 下的独立 CLI。
- CLI 版本低于旧门槛、属于预发布版或版本格式变化时，版本号不阻止尝试；有效 `accountId` 和额度结构仍是必要条件。
- Finder 风格的精简 `PATH` 下，npm 包装脚本与 Node 分属不同目录时可启动；覆盖 NVM、显式 Node 路径、Node 缺失与原生 CLI 无需 Node。
- 方法缺失或响应不兼容时只尝试同一 confirmed Home 下的下一个候选；`accountId` 缺失时不跨 CLI 寻找其他登录态，不发布错误账号的额度。本机运行时缺失进入可恢复的刷新退避。

## 路径覆盖

自动发现优先尝试 Codex App 内置 CLI，再尝试独立 CLI；Node 按进程 `PATH`、CLI 同目录、Volta/asdf/mise/NVM/fnm 和常见 Homebrew/MacPorts 路径查找。特殊安装可以在启动 App 的环境中设置绝对路径：

- `CODEX_PULSE_CODEX_BINARY`：指定唯一 Codex CLI 候选。
- `CODEX_PULSE_NODE_BINARY`：为 `#!/usr/bin/env node` 包装脚本指定 Node。

覆盖路径只在本机进程环境使用；不得写入提交版证据或把它用于猜测其他机器的路径。版本输出仅作诊断，`InspectCodexBinary` 的 `unverified` 表示尚未完成 App Server 调用。

## 真实 Home 验收

运行前说明：会读取真实 Codex Home 的 Session/JSONL 和公开 App Server 账号/额度接口；可能写入 mode `0700` 的私有 runtime、SQLite、preferences 和 App Server 标准 housekeeping。不读取凭据内容，不代替用户登录或切换账号。

1. 确认 `${CODEX_HOME:-$HOME/.codex}` 的物理身份与既有私有 runtime 的 `preferences.json` 一致，且 runtime 未被其他 App 使用。
2. 显式设置 `CODEX_PULSE_APP_RUNTIME` 后执行 `make verify-live`；启动后读回 App/Helper 的 Home 与 runtime 参数、实际 CLI 候选、账号 binding、额度和失败代码。
3. App 内置 CLI 可用时，确认无需独立 npm CLI；若不可用，记录固定的能力/运行时分类并验证独立 CLI 回退。
4. 重启开发 App 再次读回；路径或 CLI 更新后应重新探测。账号无法确认或未取得新额度时标记 `INCOMPLETE`，不把 synthetic PASS 代替 live PASS。

本 runbook 不要求卸载任何 CLI，也不执行签名、公证或发版。

## 2026-09-23 本地结果

- Go 聚焦包 `internal/codex/appserver`、`internal/codex/quota`、`internal/app`：`PASS`。
- 使用完整 Xcode 构建开发版 Swift App：`PASS`。
- 已确认私有 runtime 的真实 Home live smoke：`PASS`；`ui_pages=11`、`codex_account_card=available`、`shutdown=clean`。退出后只读回查，账号 binding 为 `confirmed`，本轮 App Server quota 与 Reset Credits 来源均有成功记录且无失败代码。
- Codex App 内置 CLI 的公开接口探测：`initialize`、`account/rateLimits/read`、`account/read` 均可用，`accountId` 与额度结构存在；探测只输出布尔结果。
- `make verify-live`：`FAIL` 于 App 启动前的现有 Swift 测试断言 `weekly overview range copy must not expose an hourly boundary`。该失败与上面的独立 live smoke 结果分别记录，尚未作为本卡修复范围处理。
- 已安装的正式 App 未被本轮开发版替换；签名、公证、发版和安装后验收均为 `NOT_RUN`。
