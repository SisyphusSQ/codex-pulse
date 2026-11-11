# TOO-702 Credits 余额与消耗验收

## v0.18.1 整数显示修正

余额及历史记录仅在展示层省略小数，保留千位分隔且不向上取整；Helper、契约、存储和上报仍使用原始精确字符串。本次开发阶段 Web 余额组件 2 项测试、Swift `--credits-only` 测试通过；后台 Storybook 已查看带小数夹具的余额与展开历史，均显示整数。Windows 同步源码及断言，本轮不构建、不发布、不替换。以下为 v0.18.0 开发阶段的历史证据，不能当作本补丁重新执行的结果。

2026-11-11，本机开发分支 `suqing/too-702-credits-balance`。范围为官方计费 Credits 余额、观测历史及实际消耗的可用状态。设计见[配额信任模型](../design/details/quota/README.md)，网络字段见[上报合同](../../api/codexpulse/reporting/v1/README.md)及[中心 API](../../server/api/quotas.md)。未提交、推送、发版或部署生产。

## 数据语义

- 余额来源为真实 Codex Home 下的公开 `account/rateLimits/read`，精确字符串贯穿 Helper、Proto、上报、中心和 UI。观察到真实余额随使用变化，测试不硬编码截图中的 62,500。
- `0`、缺失、无限额、陈旧及同时间来源冲突分别处理；同账号多设备余额不相加。历史为近 30 天每天最后有效 UTC 观测，采集空档不补值。
- CLI 已从 0.156.1 升级到核对时 npm 最新稳定版 0.167.1。导出的公开 schema 提供 `estimatedUsageCreditsMicros` 估算字段，没有可验证的账号实际扣费流水。实际消耗合同为 `source_unavailable`，今日/7天/30天显示未知，余额变化不等同消耗；不能把本轮验收称为“实际消耗统计已完成”。
- Reset Credits、Token 和美元成本沿用各自口径，本轮没有充值、购买、换算或告警。

## 聚焦验证结果

| 范围 | 结果 |
| --- | --- |
| Go Helper 受影响包 | 精确小数/零/非法字段、配额隔离、重放不可变、账号 fence、历史清理及 Core 映射通过 |
| 中心 SQLite | 多机不相加、设备筛选、改写拒绝、绑定隔离、v6→v7 升级重入与旧事实保留通过 |
| Swift | Credits 格式化和后台专项启动参数测试通过；原生开发 App 构建成功 |
| Web | TypeScript 通过；Credits 与账号页 19 项测试通过；AntD lint 0 issues、0 skipped |
| Proto / 本地化 | protoc 34.1 重新生成 Go/Swift，Go 生成一致性检查通过；两套本地化 plist 与重复键检查通过 |
| 架构检查 | 未通过：仓库 HEAD 缺少 `.github/workflows/ci.yml`，入口以 CI-001 停止；未扩大本轮范围修补 |

工具 protoc 34.1 仅下载到被忽略的 `.artifacts/tools/`，没有替换系统 protoc 36.2。构建的 actool 检查触发现有 icns fallback，开发 App 正常组装；不作为签名、公证或发布证据。

## 真实 Home 专项 live E2E

使用显式真实 `${CODEX_HOME:-$HOME/.codex}` 和 mode 0700 的独立临时 runtime。首次运行前复制现有偏好到私有 runtime，未覆盖安装版偏好。启动后读回 preferences canonical path/inode、App/Helper 的真实 Home 环境、runtime 参数和 Helper 数据库位置；断言均通过，原始环境不进入提交版记录。

1. 通过真实账号官方来源采集余额，经账号 fence 原子保存到本机 SQLite。
2. 开发 App `--credits-smoke` 在后台启动托管 Helper，用正式 Swift CoreClient 经继承 pipe 鉴权的 UDS 完成握手、Bootstrap 和 QuotaCurrent；读取真实余额与非空历史，渲染实际 `PaidCreditsCard`。输出 `credits_balance=known credits_history=1 credits_render=captured credits_consumption=source_unavailable shutdown=clean`。原生卡片截图检查通过。
3. 独立 Helper probe 复用相同私有 runtime（顺序运行，不同时占用 socket），通过真实 PairReporting/ConfigureReporting/SyncReportingNow 将该余额上传至仅监听 loopback 的临时中心；读回成功 receipt 后关闭上报。临时中心 SQLite 中存在真实 Credits 事实。
4. 官方后台应用内浏览器 `iab` 登录临时中心账号页：实际余额、观测时间、未知消耗与余额历史可见。展开历史，检查桌面及 390px 窄屏截图；窄屏文档宽度与 scrollWidth 都为 390，没有水平溢出。结束后恢复浏览器 viewport。
5. 专项 App/Helper 干净退出，私有 socket 被清理；未替换 `/Applications` 中的安装版。临时中心在验收结束后停止，保留私有证据供本机复核。

通用 `make verify-live` 的等价全页面入口尝试两次，均在 90 秒超时，未获得全页面通过结论，原因未确认；专项入口只证明上述 Credits RPC 与原生卡片，不覆盖完整 AppRuntime/Overview、所有页面或菜单栏手工交互。一次并发占用测试 socket 的人工探针误操作产生 Unauthenticated，改为顺序执行后重验；该失败不作为产品认证回归结论。

本机原始日志、脱敏环境断言、截图及临时中心数据均位于被忽略的 `.artifacts/credits/`。不提交真实账号 ID、Home 路径、凭据、JSONL、原始会话内容或完整日志。

## 可复用入口

开发时按受影响测试筛选运行，例如：

```sh
go test ./internal/creditbalance ./internal/store ./internal/core ./internal/reporting ./internal/codex/appserver -run 'PaidCredits|DecodeOfficialBalance|ViewsUsesLatest'
(cd server && go test ./internal/service/quota_srv ./internal/repository/mysql/schema_repo -run 'PaidCredits|V6PaidCredits')
swift run --package-path app/macos codex-pulse-app-tests --credits-only
(cd server/web && npx tsc -b && npx vitest run src/components/PaidCreditsCard.test.tsx src/pages/Quota.test.tsx)
```

真实验收前必须按仓库真实 Home 规则准备、确认私有 runtime 与偏好，不使用 synthetic Home 替代。构建开发包后，显式设置真实 `CODEX_HOME` 和私有 `CODEX_PULSE_APP_RUNTIME`，传 `--credits-smoke --runtime-directory "$CODEX_PULSE_APP_RUNTIME" --skip-live-lifecycle` 给开发 App 可重复专项验收；该入口读取已采集余额，尚无有效余额时会失败而非制造成功。输出截图位于私有 runtime 的 `credits-smoke.png`。同一 runtime 的 App/Helper 必须顺序启动。

中心完整重验沿用既有设备配对及上报设置，选择独立临时开发数据库、loopback 地址；先确认上报目的地与范围，再开启同步。不要使用本轮私有一次性配对码或复用生产数据库作为回归夹具。

## 未验证

未运行全仓 `make verify`、race 长测或 CI。Windows 代码已接入同一 Proto，当前 Mac 无 dotnet/WinUI 环境，未构建或实机验收；独立 MySQL v7、跨机器同步、生产升级、签名、公证和正式发布均未执行。恢复/降级需匹配版本的一致数据库备份，不用旧库覆盖更新后事实。
