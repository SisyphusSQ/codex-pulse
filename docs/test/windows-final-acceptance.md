# Windows 客户端最终验收（2026-10-29）

对应 [TOO-657](https://linear.app/sisyphus-sq/issue/TOO-657) 与 TOO-658～TOO-673。范围、无人值守授权和允许的条件缺口以 Master 的用户 2026-10-29 约定为准。16 张开发卡的实现和修复均已进入 `suqing/windows`。下列 Not Run 与用户豁免均不记通过。

## 环境与最终产物

- Windows 11 Pro 24H2 build 26100、x64，一个显示器，实际窗口 DPI 120（125%）。ARM64 不在已验证支持矩阵内。
- .NET SDK 10.0.403 / runtime 10.0.12、Windows App SDK 2.6.4、Windows SDK BuildTools 10.0.28000.2705、Go 1.27.1；依赖固定并锁定。
- 应用代码提交 `8c95fdfb2bfe5980021ae5b91a8f9fa544b55b49`。安装版 `0.15.2-windows-dev.1011`、build 1011，App/Helper 版本一致，Core contract 为 `core-rpc-v9`。本记录的后续提交只改文档。
- 自包含包共 538 个文件、ZIP 160,976,566 bytes，SHA-256 `150ac605e76a92782b27a7c059b5b8dc731400a15513465bdda34f6a59e99712`。
- 数据兼容声明为 `sqlite38-prefs5-credentials-1c4b258db74421c75f78a3e2773f575b5a8efce398c4e853e289cf58b599728d`。
- 显式绑定真实默认 Codex Home 与独立 ACL 私有 runtime；preferences、App/Helper 环境、Helper 参数及物理身份全部匹配，身份摘要 `9c883cdeaa79cca0`。冷扫描、Home 切换和全历史补充验收使用另外的私有 runtime，不复制同步凭据。
- 原生程序来自 `OpenAI.Codex` MSIX 26.1023.6621.0；MSIX 内程序与当前独立原生 CLI 均为 `0.165.0-alpha.1.2`，分别经默认发现与显式 CLI 路径实际运行 App Server。
- 当前用户开发安装使用固定本地 HTTPS 源、CNG 开发包签名和策略限定证书。Authenticode 为 NotSigned，未声称正式公信任或公开分发。

## 最终矩阵

| 编号 | 结果 | 实际证据与边界 |
| --- | --- | --- |
| C01 安装与真实绑定 | Pass | 最终安装、connected/ready 回执、版本、上述 Home 身份与私有 ACL 全部读回。 |
| C02 程序与 CLI | Pass；其他入口 Not Run | MSIX 与独立原生 CLI 分别完成真实 Home、账号、额度和查询。npm/Node 只有开发覆盖，本机没有该真实安装入口。 |
| C03 初始与增量 | Pass（现存源）；缺失源保留 partial | 新私有 runtime 的真实冷扫描、既有 runtime、重启及 clear-and-rebuild 的固定日一致。代表活动 rollout durable offset 从 83,532,581 增至 88,590,895，保持同代次；固定日没有重复增长。一条 metadata 对应原始文件缺失，保留 pending/partial，不制造事实。 |
| C04 查询与页面 | Pass | Go、最终原生汇总/项目、Server 与实际 Web 的固定日统计一致；模型、区间缓存、生命周期和 TPS 按各自口径另行核对，见下表。 |
| C05 当前账号额度 | Pass | 当前 ChatGPT 账号、prolite 套餐、Known Pro tier，UI 为 Pro 5×；来源实际只提供 10080 分钟窗口，末次已用 24%、剩余 76%，reset=1793796404000。Reset Credits 当前库存 2；手动刷新 Fetched=true。没有制造 5 小时窗口。 |
| C06 隔离和历史 | Home 事实隔离 Pass；第二账号 Not Run | 受控 Home 取消、切换、空快照和返回真实 Home 已执行；A→B→A、历史账号清理和真实旧账号队列切换因缺少第二真实账号未执行，不记受阻。 |
| C07 失败恢复 | Pass；自然跨周期 Not Run | 只对受控 CLI/Helper 设置拒绝代理，实际刷新及账号请求失败，本地固定日不变；无法确认绑定时额度为空/unknown，无假 0%/100%。移除代理后刷新、绑定、额度及 Reset Credits 恢复。没有等待真实 reset 或库存消费。 |
| C08 原生体验 | 已执行项 Pass；条件缺口 Not Run | 最终安装版已打开汇总、概览、会话详情、项目、额度、账号、API 未配置、健康、来源任务及设置；中英双向、日期、深/浅/系统主题、键盘选择与刷新正常。托盘可打开、失焦收起及重开。仅有单屏 125%；工具不能独立寻址托盘工具窗口，内部复制/独立 Escape 输入未记真实通过。 |
| C09 生命周期 | 已执行项 Pass；实际睡眠 Not Run | 关闭/重新激活保持同一 App/Helper；单实例与 Helper 异常恢复已有真实证据。语言崩溃也实际收回 Helper。最终产品菜单退出后 Shutdown=clean，App/Helper/其 App Server 均为 0。Windows 把睡眠请求转为离开模式（Kernel-Power 187、59），未发生 sleep/wake；临时唤醒任务已删除。 |
| C10 Home 与恢复 | Pass | 受控 runtime 完成计划取消、确认空 Home、generation 变更、Token=0、返回真实 Home 重建、repair dry-run Noop/零冲突。迁移、未来 schema 和备份恢复另有隔离 runtime 的 C#→Go 开发矩阵，未破坏真实源。 |
| C11 中心闭环 | Pass | 安装 profile 独立配对默认关闭、启用、历史范围和续传；原生重复全量补传最终 24 个有效确认、pending=0。补充全历史设备 15 个会话、22 个有效确认、pending=0。Server 与实际 Web 均读回。 |
| C12 幂等与共存 | 真实联调及开发矩阵 Pass | 实际暂停/退出/重启/续传、重复全量、多 Windows 设备去重完成；全设备固定日不变，既有设备保留，与 Mac 来源共存。提交后丢响应、跨账号队列及 Mac 副本等精确故障引用受控开发证据，不冒充真实故障注入。 |
| C13 安装更新 | Pass（开发信任） | 原生有效更新 1005→1006、显式回退、卸载保留数据、重装及最终 1009→1011 的数据连续性已执行。坏签名、离线、下载中断有实际受控验证；占用、uncertain shutdown 和兼容 fence 有开发矩阵。正式信任、公开源及发行未执行。 |
| C14 兼容与隐私 | Pass（已列范围） | Windows Go/C#、IPC、ACL/路径/锁、恢复、上报及打包聚焦证据见各卡；sqmc04 私有临时源码以 synthetic Home 实际执行 macOS arm64 App Server、轻量 runtime、Store 查询回归。Core/Reporting Proto 未改。正式中心 PID 保持，开发配置 hash 与既有读回一致，未改正式中心。 |

## 固定范围与生命周期对账

Provider `codex`，Asia/Shanghai，2026-10-28 00:00（含）至 2026-10-29 00:00（不含）。原生包含结束日期为 2026-10-28。成本使用历史价格，是 API 等价估算，不是订阅账单。

| 指标 | Go / 原生页面 / Server / Web |
| --- | ---: |
| 输入 Token | 44,246,540 |
| 其中缓存输入 | 43,198,208 |
| 输出 Token | 175,926 |
| 独立 reasoning Token | 62,560 |
| 总 Token | 44,485,026 |
| 估算成本（microUSD） | 6,339,440 |
| 显示金额 | US$6.34 |
| 会话 / 本机项目 / 模型 | 10 / 4 / 2 |
| 范围缓存命中率 | 9,763 basis points |

GPT-6.1 Sol 为 18,268,141 Token、3,877,536 microUSD、9,826 basis points；GPT-6.1 Terra 为 26,216,885 Token、2,461,904 microUSD、9,720 basis points。Web 分别显示约 1826.8 万 / $3.88、2621.7 万 / $2.46。

代表闭合会话摘要 `a97183eb210f` 的完整生命周期为 21,719,022 Token、4,501,777 microUSD、5 个唯一轮次、缓存率 9,851 basis points；输出 112,991 Token / 活跃区间并集 4,089,428 ms = 27,630 milli-TPS。原生与 Web 显示 98.5%、27.63 TPS。其固定日贡献只有 18,268,141 Token，不能与生命周期混加。带起点的设备按合同返回 `history_filtered`；补充全历史设备才用于生命周期数值对账，不能拿区间缓存率代替生命周期缓存率。

安装 profile 设备为 `90fee774-480d-415f-bad7-e5674e95ae68`，补充全历史设备为 `1acaf61c-3ae2-4d5c-b753-82f5d40f2dcb`。重复补传前后及加入全历史设备后，全设备去重固定日 totals 均相同；原有六个设备 ID 全部保留，采集凭据与历史保留，临时 Web/管理会话已退出。

## 实际失败及修复

1. TOO-673 首次安装因 PRI 缺失启动失败；补齐生成、打包必需文件及签名清单后成功。有效更新另发现目录尾分隔符引起的路径拒绝，规范化及回归后原生 1005→1006 成功，两项均在 PR #229 内完成。
2. 新 runtime 曾只有 30,113,274 Token，旧 runtime 保留 43,198,983。真实 Codex 对三个现存文件返回扩展 DOS 路径，被误判 outside_home；PR #230 统一本地路径表示并保留 Home/真实路径校验后，冷扫描、既有库、重建均为 44,485,026。
3. 受控空 Home 曾错误回退旧深索引，返回 2,117,757 Token。PR #230 改以已发布轻量 metadata snapshot 为权威，实际空 Home=0，返回真实 Home 精确恢复。
4. 打开日历后切换语言发生 0xc000027b 原生崩溃。PR #231 在挂载前设置新页面语言，文化变化时替换预初始化日历，避免更新已初始化 Root 的继承语言；最终 1011 安装版在使用两个日历后完成中英双向切换，范围与统计不变。

托盘“清单未列出”的判断已纠正：自动化清单排除工具窗口，实际原安装版正常显示、收起和重开。候选修改全部撤回，未提交或创建 PR，不计产品缺陷或修复。

## 未执行及豁免

- **用户明确豁免**：Cursor、Grok、DSH 的真实采集、账号/服务、真实 UI 数据和中心联调。完整生产实现及开发验证已交付，豁免不是 Pass。
- **缺少条件**：第二真实 Codex 账号；多屏/其他 DPI；真实多页样本（代表会话只有 5 轮）；实际睡眠被系统离开模式替代；当前工具不能独立绑定托盘工具窗口，内部复制和独立 Escape 输入未执行。
- **尚未发生**：真实 reset、过期、Reset Credits 消费或自然跨周期；未改时钟或等待数日制造结果。
- **共享会话边界**：没有可恢复的隔离登录桌面，未执行真实 OS 登录/注销及 Explorer 重建；注册/移除、TaskbarCreated/会话结束路径有开发证据。
- **未配置或范围外**：DeepSeek/OpenCode Go 真实 Key/账户调用；npm/Node、另行下载的 App 形态、ARM64 的真实验收。实际中心为 SeekDB 1.2/MySQL 协议兼容环境，不能称独立 MySQL 8 验收。正式中心部署、正式公信任、公开分发、Tag/Release 和 Windows→main 均未执行。
- 原始缺失 rollout 及中心既有全年覆盖语义缺口保留，未补造内容或关闭遗留验收项。提交后丢响应、跨账号队列及跨平台副本的开发证据不冒充真实事件。

未启用停用的 Actions，没有 CI checks，未声明 CI 通过。提交、推送、合并收尾按约定未重复执行测试；本记录只引用已有开发输出和明确执行的最终验收。

## 开发交付与证据

| 开发卡 | 仅合入 Windows 的 PR |
| --- | --- |
| TOO-658 / E01 | #211 工程、生成、构建 |
| TOO-659 / E02 | #212 私有目录、持久化 |
| TOO-660 / E03 | #213、#230 Home、增量、空快照隔离 |
| TOO-661 / E04 | #214 IPC 认证及 Core client |
| TOO-662 / E05 | #215 程序发现、账号、额度 |
| TOO-663 / E06 | #225 Cursor |
| TOO-664 / E07 | #226 Grok、DSH |
| TOO-665 / E08 | #216、#231 框架、导航、语言 |
| TOO-666 / E09 | #217、#219 汇总、概览、unknown |
| TOO-667 / E10 | #218 会话、项目、缓存、TPS |
| TOO-668 / E11 | #220、#222、#228 额度、账号、托盘隐私 |
| TOO-669 / E12 | #227 API、订阅、独立凭据 |
| TOO-670 / E13 | #221 设置、Home、健康、恢复 |
| TOO-671 / E14 | #223 托盘、进程、生命周期 |
| TOO-672 / E15 | #224 配对、队列、有效确认 |
| TOO-673 / E16 | #229 安装、更新、工具链 |

PR 位于 [本仓库](https://github.com/SisyphusSQ/codex-pulse/pulls)，base 均为 `suqing/windows`。逐卡提交、开发验证和实际合并读回保留在 Linear；本文件不镜像 Issue 状态机。

忽略目录 `.artifacts/windows/final-acceptance/` 保存本机证据：

- `final-binding.json`、`full-history-binding.json`：真实 Home、私有 runtime、物理身份。
- `core-cli-offline-complete.jsonl`、`core-cli-recovered.jsonl`、`final-installed-core.jsonl`：失败、恢复及最终安装 Helper 查询。首轮未填 source 的 probe 参数错误另存，不能当产品故障或刷新通过。
- `core-pair-native.jsonl`、`core-full-history.jsonl`、`center-before-repeat.json`、`center-after-repeat.json`、`center-full-history-metrics.json`：配对、有效确认、口径及去重。
- `index-before-sleep.jsonl`、`index-final.jsonl`、`final-lifecycle-index-summary.json`：增量 offset、固定日不变、无 owned 遗留进程及睡眠条件。
- `native-1011-oct28-en.png`、`native-projects-oct28.png`、`web-models-oct28.png`：最终原生和实际 Web 画面。真实路径、用户内容、凭据及完整日志不提交。

聚焦开发结果见 [Windows 验证入口](windows-client.md)，安装更新矩阵见 [安装更新 runbook](windows-install-update.md)。最终 App 正常退出，安装和受保护数据保留，无独立后台采集进程；临时 HTTPS fixture、诊断项、唤醒任务及管理会话已清理，登录启动恢复为未配置。

安全自查：本次 Windows diff 的 IPC 认证/ACL、物理 Home/文件边界、固定外部端点、秘密/日志、结构化白名单、资源界限及进程/更新并发均有对应实现与证据；无尚未处理的本轮阻塞问题，未以开发信任或 fixture 证明未执行环境。
