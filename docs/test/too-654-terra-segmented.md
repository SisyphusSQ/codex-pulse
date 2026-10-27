# TOO-654：动态选择器与 GPT-6.1 Terra 验证

## 范围

对应 GitHub #209 和 Linear TOO-654。分支 `suqing/fix-209-terra61`；仅本地开发与聚焦验证，不提交、不部署、不发版。原始崩溃证据来自 #209：SwiftUI 更新 NSSegmentedCell 时报旧索引 2 超过当前 0 项。额度窗口是吻合候选，原始系统异常尚未单独稳定复现。

## 改动

- 动态额度窗口与 Provider 相关的概览时间范围使用 SwiftUI 按钮组，移除这两处 NSSegmentedControl 更新路径。渲染先按稳定 ID 选择有效项，空集合无选择，重排保留选择，失效选择使用首项；随后规范化绑定。额度窗口隐藏时也规范化状态，避免重新出现后恢复失效选择。
- 长标题在固定高度独立行内横向滚动，支持左右键、按钮焦点与选中无障碍标记；不拦截 Objective-C 异常，不引入延时或重试。其他 segmented 选项为固定枚举/固定范围。
- Terra 展示名、归因与检查点恢复；第九版不可变历史价 `openai-api-2026-10-26`，input/cached/output 每百万 Token `$1/$0.05/$5`。旧八版不变。
- DSH 按请求时间定价，Mac/中心保持 Provider 与贡献身份；补价通过既有 revision 机制。中心增补公开快照、官方发布日和 16 组模式/上下文/缓存保留参考条件；当前价不重算历史，Web 默认 Standard 短上下文 30 分钟写缓存，Batch/Flex 在附加条件展开。
- 无 schema、Core RPC、Reporting contract 修改；不改 Cursor/Grok 价格或订阅目录核对日期。

官方来源：模型页 https://developers.openai.com/api/docs/models/gpt-6.1-terra ，发布日 https://developers.openai.com/api/docs/changelog （2026-10-26），总价目 https://developers.openai.com/api/docs/pricing 。发布日使用 UTC 日界，不能当成精确开放时刻。核对时刻与价格生效边界分别保存。

## 聚焦验证

原始输出放在忽略目录 `.artifacts/too-654/`，不提交个人数据。

- Go：`go test ./internal/pricing ./internal/attribution ./internal/lightindex ./internal/store ./internal/query/pricingcatalog ./internal/reporting ./internal/dshprovider -run 'GPT61|Terra|Current|TestDSH|TestReportingExport|TestTokenScannerRestores|TestNormalizeModel'` 通过。包含旧版本不可变、精确边界、未知型号、模型切换与恢复、既有 Token 不重扫补价、会话/项目/概览 `$1.56` 对账、DSH 互斥 Token 分量、导出及排队 revision。
- Server：`go test ./internal/service/catalog_srv ./internal/service/statistics_srv ./internal/http -run 'Catalog|Terra|DSHCodex|DSHReporting'` 通过。包含当前/历史/DSH 目录、独立发布日期、权限及三设备费用修订去重；目标为隔离 SQLite fixture，不代表真实三机或正式 MySQL 验收。
- Swift：`swift run --package-path app/macos codex-pulse-app-tests --quota-window-only`、`--pricing-only` 通过。挂载 NSHostingView，20 轮 × 6 种集合更新（空、1项、2项、全替换、重排、恢复），验证绑定、无 NSSegmentedControl、360px 长标签宽度。它是确定性控件回归，不是原始四天崩溃的稳定复现。
- Web：`npm test -- --run src/pages/Pricing.test.tsx --testTimeout=15000`，9 项通过。`npm run build`（包含类型检查）通过；受影响组件 AntD lint 无问题。首次并行构建时一项旧测试触发 5 秒超时，本次运行放宽到 15 秒后通过，未改全局超时配置。
- `make format-go-imports` 已执行，`git diff --check` 通过。未运行全仓长测或 CI。

## 验收边界

真实 Home 开发 App 已完成启动读回：偏好 canonical path/device/inode 与预期一致，App/Helper 的显式 CODEX_HOME 和 Helper 私有 runtime 参数一致，runtime mode 0700。额度页可见 `gpt-6.1-terra` 的 `$1/$0.05/$5` 与 `openai-api-2026-10-26`；刷新及概览往返正常。当前账号只返回一个额度窗口，因此本次真实界面没有触发多窗口缩减场景。退出后 App/Helper 均停止，socket 已移除，Home 物理身份保持一致。原始输出仅存忽略目录；无账户、会话正文或真实路径进入本摘要。

开发 App 使用仓库 `build-dev-app.sh` 构建在 `build/dev/` 下，未安装。默认 Command Line Tools 缺少 SwiftUI State 宏插件，改用命令局部 `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer` 后构建成功；未改全局 xcode-select，也未修改项目运行入口。Server/Web 构建成功，无正式签名、公证或发布。

长期稳定性、原始崩溃的完整触发链、动态按钮键盘操作与 VoiceOver 人工听读、正式 MySQL、生产与真实三机 E2E 不由上述测试证明。左右键选择的纯逻辑和挂载状态已验证，但不能替代这些人工项目。

## 二次 Review：目录继承与分批升级补价

发现并修复一项 P1：`enrichable` 原先对两份 known 贡献逐字段比较，新增 Terra 的完整目录继承了 Sol 费率，却让同一 Sol 事实的 `pricing_version` 从 `openai-api-2026-09-29` 变为 `openai-api-2026-10-26`。旧机器的 Sol known/Terra unpriced 与新机器的 Sol known/Terra known 被误判为已知价格冲突，阻止新增价格证据成为 canonical。纯仲裁回归在修复前对 `codex_model_sum` 和 `event_cost` 两种模式均失败。

修复仅在等价性比较的局部副本中忽略价格版本标签，其他字段必须完全相同；保存的来源快照和 canonical 的版本仍保留。模型或实际费率/金额变化继续构成冲突。

新增中心回归覆盖 Codex/DSH 混合 Sol/Terra 会话、三设备分批升级、旧机器继续发布更高本地 revision、重复批次：Token 恒为 2000，补价后费用恒为 15000 microUSD，单会话且无假冲突。`reporting_srv`、`statistics_srv`、`catalog_srv` 三个受影响包测试通过。Store 升级回归新增真实 ReportingPage 前后导出比较，验证 Terra unpriced → known、贡献 ID 不变、费率与价格版本正确；通过。

动态选择复查未发现新的可报告缺陷；补充不重置 binding 的连续缩减、重排、清空、恢复回归，检查有效选择保留且失效选择不复活。验证输出在 `.artifacts/too-654/review/`。本轮没有重新运行真实 Home App、Web、完整 CI、生产或真实三机验收；保留上轮对应证据及边界。

## v0.16.5 / build 72 发布授权

用户已明确授权本次提交并合入 main、先升级中心，再发布 Mac 和更新 appcast；覆盖上文开发阶段的“不提交、不发版”边界。提交与发版收尾按约定不重复执行测试，复用上述已有输出；发行构建、Bundle/签名/资产完整性、部署与远端读回单独记录，不冒充测试。最终发布与现场回执放在忽略目录 `.artifacts/releases/v0.16.5/`。无新增 schema 或 Core/Reporting contract 变更，保留旧发行、数据库备份、配对及队列。
