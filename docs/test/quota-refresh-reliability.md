# 额度刷新可靠性验收

对应 TOO-474；开发分支 `suqing/too-474-quota-refresh-reliability`。目标是菜单栏周剩余量的失败可追溯、来源互不拖垮、恢复安全和 stale 状态一致。

## 故障证据边界

2026-10-01 已安装版本的额度成功记录停滞，Reset Credits claim 已到期而未收口，手动操作未追加 attempt。独立 Codex 查询仍成功。旧版本没有持久化原始错误，因此无法断言首次异常是 HTTP/API 故障。

确定性复现证明：`credits:null` 被 clone 成已知空数组；available 状态但已过期的 credit 进入 writer；无关字段畸形使另一个来源解码失败；静默 RPC deadline 未能中断读取。这些是已修复缺陷，不等同于恢复了历史现场。

## 自动化入口与副作用

以下测试只使用 synthetic/empty Home、临时数据库与脚本化 CLI；生成和构建写入 generated files、构建缓存、bin/build，原始证据只留忽略的 `.artifacts/`。

```bash
go test ./internal/codex/appserver ./internal/codex/quota ./internal/diagnostics ./internal/core ./internal/scheduler -count=1
go test -race ./internal/app ./internal/scheduler ./internal/codex/appserver ./internal/codex/quota ./internal/diagnostics ./internal/core -run 'QuotaRuntime|QuotaRefresh|QuotaFreshness|HealthQuotaRuntime|RefreshFailure|RefreshDiagnostics|JSONLineRPC|RPCError|AppServerSilent|AppServerStderr|ReadAccountRateLimitsIsolates|ResetCreditsService' -count=1
swift run --package-path app/macos codex-pulse-app-tests --quota-refresh-only
bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check
bash scripts/macos/build-dev-app.sh
```

不主动运行全仓长测。提交/发版收尾遵守 AGENTS 的不重复测试约定。

## 场景

| 场景 | 预期 |
| --- | --- |
| quota / reset 无关字段畸形 | 仅相关来源失败，共同账号身份仍严格验证 |
| credits null、数量 0/非零 | 空库存 / 明细不可用成功入库，nil 不变已知空数组 |
| 过期 available 或库存约束异常 | 有 `reset_snapshot` 固定原因，失败 attempt，无异常库存覆盖 |
| 静默 initialize / RPC | deadline 中断阻塞 IO，子进程被回收，有耗时和 timeout 证据 |
| RPC error | 保存数字 code；不保存 message/data；错误链保持兼容 |
| 来源失败手动回执 | 实际执行失败返回错误，skipped 仍有 fetched=false |
| 暂态 runner 退出 | runtime recoverable；手动可重启，durable claim 后 recovered |
| 永久故障 / sleep | 不通过手动恢复绕过保护，无无限重启 |
| FreshUntil / reset / credit 到期 | Helper 仅触发查询失效，last-good 保留且状态重算 |
| HealthProjection | worker 停止可见，存储 blocked 仍优先，不修改共享快照 |
| 日志权限/轮转/保留 | 0700/0600、四份各 5 MiB、24 小时轮转，首个未恢复故障跨重启保留 |
| 日志失败 / privacy marker | 业务独立，丢失计数可见，正文/凭据/路径/ID 不落盘 |
| 中文资源目录 | `zh-Hans.lproj` 正常加载，菜单栏与新提示中英文正确 |

## 真实 Home 验收

执行前说明：读取真实 `${CODEX_HOME:-$HOME/.codex}` 的 Session/JSONL、公开 App Server 账号/额度接口；可能写入 mode 0700 私有 runtime 的 SQLite/preferences/日志和 App Server 标准 housekeeping。不读取凭据内容，不向真实 Home、系统网络或用户数据注入故障。

1. 使用 `make verify-live`，或等价的显式真实 CODEX_HOME + 已确认 0700 runtime 启动 development App。启动后读回 preferences 的 canonical path/device/inode，与 App/Helper 环境/参数核对。
2. 确认本轮 quota 与 Reset Credits 真实请求形成新成功 attempt，claim 收口、下次 due 存在；日志 trace/source/trigger/stage 连贯，无 dropped 或原始敏感字段。
3. 菜单栏打开额度视图，手动刷新后确认状态和真实百分比；再次读取缓存不能记作新上游成功。
4. 在正常下一次 due 后检查新 attempt 与日志。自然网络故障需用实际诊断复核，不能以 synthetic 测试声称已完成真实断网或睡眠验收。
5. 关闭开发 App，确认 Helper 与 socket 清理；正式安装版本不被开发包替换。

## 2026-10-01 结果

| 验证 | 结果 | 证据范围 |
| --- | --- | --- |
| 受影响 Go 包 | Pass | synthetic，真实 Store/客户端/调度契约 |
| runtime/health/Proto/freshness/诊断聚焦 | Pass | synthetic |
| 聚焦 race | Pass | 六个受影响包；另含手动日志上下文与 sleep/恢复回归，不等同于全仓 race |
| Swift 聚焦 | Pass | 本次菜单栏/额度/恢复提示/账号一致性；中文资源目录先失败后修复 |
| Swift 全量 | Pass | `CodexPulseApp deterministic tests passed`；修正旧侧栏 fixture 遗漏已交付的账号额度页，页面业务代码未改 |
| 开发 App 构建 | Pass | 未签名开发包 |
| 架构检查 | Pass | 修正 `build/dev` 缓存误判、CLI 环境包装和 11 页 smoke 的过期静态匹配；不放宽真实 Home 隔离检查 |
| 标准 protoc | Pass | 34.1 从校验源码重编译链接当前 abseil，正常路径可用 |
| 真实 Home 读取 / 持续刷新 / GUI | Pass | 新 runtime 身份回读匹配；quota/reset 启动成功、真实手动成功、观察一次 5 分钟到期的 scheduled 成功；最终 manual trace 含 persist/claim/结果，claim 全部收口 |
| 标准真实 Home 原生界面 smoke | Fail | 11 个页面已加载，数据非空、health healthy、shutdown clean；原生弹窗键盘焦点链 `keyboard_focus_chain` 未通过 |
| CI / 正式签名 / 公证 / 发布 | Not Run | 本轮未授权 |

原始本机日志、备份和下载仅在 `.artifacts/`，提交摘要不包含真实路径或响应正文。

真实故障注入、真实 sleep/wake 与双账号/Home 切换为 `Not Run`；相关取消、generation/fence 与睡眠保护仅有 synthetic/race 证据。自查未新增认证或 TCP surface，日志使用有限白名单，未放宽账号 fence 或数据库保护。

完整 smoke 的原生键盘失败不降为 Pass；本次人工额度页与手动请求、正常到期持续刷新仅作为对应范围的 Pass。
