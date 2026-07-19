## Unreleased

#### feature:
1. [TOO-243] 完成 Go、Wails3 与 Vue 3 工程骨架初始化，固定前后端依赖、generated bindings 与 macOS 15 arm64 开发构建入口
2. [TOO-244] 新增 macOS 15+ thin arm64 应用 Bundle、冻结图标、ad-hoc 签名与单顶层 ZIP 打包验证闭环
3. [TOO-246] 新增 SQLite WAL 连接面、有界单写队列、独立只读池与应用 drain/close 生命周期，固定私有路径权限、authoritative cancellation 和可判定错误语义
4. [TOO-247] 建立 Session、Turn、Usage 六表 STRICT 事实与投影 Schema、原子 bootstrap 和 typed repository，固定来源 generation/offset 幂等、finality 与 unknown/null/真实零语义
5. [TOO-248] 建立 Source、Job、Health 与 Pricing 七表 STRICT 运行事实 Schema、原子 bootstrap 和 typed repository，固定 interrupted-only 恢复、健康生命周期与有限分类、不可变价格版本及敏感内容持久化边界
6. [TOO-249] 重构 SQLite 持久层为 Pure Go GORM adapter，新增版本化 migration、WAL 一致性备份与恢复、目录持久化发布及跨 core/runtime typed WriteUnit 原子事务
7. [TOO-250] 新增固定 24 小时运行数据保留、低优先级有界 maintenance 写入、append-only v2 查询索引与异常退出恢复集成矩阵，保证 GORM Pure Go Store 可分批清理并保留长期事实和恢复血缘
8. [TOO-251] 新增基于 confirmed root FD 的 Codex JSONL 文件发现、有限前缀 fingerprint 与 confirmed-home 范围内的两阶段来源对账，安全识别新增、增长、截断、移动、替换、删除和不可读状态
9. [TOO-252] 实现有界增量 JSONL Parser 与显式 Turn lifecycle，输出 allowlisted session/turn/nullable usage、content-free diagnostics 和完整恢复 checkpoint，支持坏行续读、乱序事件及跨重启确定性恢复
10. [TOO-253] 实现增量索引的原子 checkpoint、generation 重建与幂等事实提交，支持 append、retry、truncate、replace 和 crash 恢复，并保证 GORM Pure Go SQLite 下 offset 不越前、旧快照原子切换及跨 Session usage 隔离
11. [TOO-254] 新增可解释、可重算的 Session、Project、Model 隐私归因与 schema v4 历史回填，固定路径摘要身份、模型别名、冲突/非法语义和不含绝对路径的安全查询投影
12. [TOO-255] 新增有来源的版本化 exact-only API 价格目录、整数微美元 Turn 成本和可原子重算的 Session/全局/Project/Model 日聚合，固定 unknown/partial、时区日界、持久化后严格对账与旧 generation 回滚语义
13. [TOO-256] 新增 append-only Session Index Repair 的零写入 dry-run、精确确认、数据库与索引双备份、受控 correction 和写后对账，固定 Store/index 漂移与本地不支持行的 fail-closed、原子 terminal success、审计失败及重新 dry-run 恢复语义
14. [TOO-257] 新增确认前 metadata-only Codex Home 探测与版本化私有 Onboarding preferences，固定三来源路径规范化、physical identity 与结构竞态 fail-closed、显式隐私确认、并发取消、重启及 durability-unknown 恢复语义
15. [TOO-258] 新增版本化 typed Preferences、v1 原子迁移与单 Codex Home 切换协议，固定 private CAS/execution lease、显式数据策略、Home generation fence、并发 owner、bootstrap journal 与取消/崩溃恢复语义
16. [TOO-259] 新增 Discover、Fast Bootstrap、History Backfill 与多 pass Reconcile 四阶段可恢复首次索引，固定 schema v6 typed plan、confirmed Home 读取、权威 source checkpoint、generation admission/Drain 和 fresh reconcile 闭合语义
17. [TOO-260] 实现持久 Live/Backfill 双队列与协作式 ScanBudget，固定实时优先与历史公平、单重型 owner lease、真实 IO 预算、可观测 cycle 及跨崩溃 target 恢复语义
18. [TOO-261] 实现持久暂停续传、休眠唤醒、来源对账与故障恢复状态机，固定 Home generation fence、可取消有界退避、typed 用户动作、应用事件串行化及 target/cycle 原子恢复语义
19. [TOO-305] 重构 scheduler 周期唤醒为 robfig/cron v3.0.1 每秒 trigger，固定重叠跳过、首错 fence、panic 脱敏、Stop drain 与持久调度状态边界
20. [TOO-262] 新增本地 Codex JSONL quota observation 的严格解析、物理 source provenance、GORM Pure Go schema v9 持久化与 coalesced replay receipt，固定 partial window、schema drift、文件替换、checkpoint rollback 和隐私边界
21. [TOO-263] 新增受控 Wham quota 客户端、内存凭证租约与 GORM Pure Go schema v10 原子记录，固定 exact-key/null/partial 校验、禁止 redirect、七类失败、短重试和 last-known-good 语义
22. [TOO-264] 建立可重建的 quota 窗口代际校验与 Local/Wham 可信仲裁投影，隔离异常、旧代际和冲突 observation，保留 last-known-good 与完整 evidence 解释
23. [TOO-265] 新增只读 Reset Credits 事实与动态汇总、可信 quota reset 计算和 robfig cron 驱动的持久刷新退避，固定 Retry-After、60 秒手动节流、claim generation fence、崩溃恢复与迟到 attempt 隔离语义
24. [TOO-266] 新增单 SQLite snapshot 的 Quota Current 只读查询合同，稳定组合窗口、来源、可信 reset、Reset Credits 与刷新状态，固定 null/真实零、last-known-good、冲突解释、投影恢复和敏感 identity 隔离语义
25. [TOO-306] 装配在线 Quota 与 Reset Credits 应用运行时，新增 confirmed Home 调用期凭证租约、崩溃 journal 恢复、local/quota generation drain 与 lifecycle、settings、manual、shutdown 闭环
26. [TOO-267] 建立版本化 `query-v1` 公共查询契约，统一有界分页、allowlist 排序筛选、IANA/DST 本地日转 UTC、JavaScript 安全整数、unknown/真实零及 content-free 错误与 partial/unavailable 语义
27. [TOO-268] 新增 active 成本账本驱动的日周月 Usage/Cost 趋势与 Session/Project 有界查询，固定安全归因、opaque cursor、unknown/真实零、partial 降级及 pricing evidence 对账语义
28. [TOO-269] 新增 Quota、Source、Job、Health 与 Settings 只读查询，固定 GORM 单快照分页、敏感字段裁剪、来源单侧 partial、有限 recovery action 与 JavaScript 安全数值语义
29. [TOO-270] 新增唯一 allowlist Wails 查询 façade 与生成式 TypeScript DTO/枚举/错误契约，装配真实 Usage、Quota、Runtime 与共享 Preferences 查询，固定可取消调用、panic 脱敏和失败生成不覆盖既有 bindings 的门禁
30. [TOO-271] 新增版本化 typed Wails 失效事件与 13 项 Vue Query 缓存契约，固定 durable commit 后通知、有限无事实 payload、事件风暴合并、丢失事件有界重取、唤醒全量恢复及卸载清理语义
31. [TOO-272] 建立 Liquid Glass 语义 token、可拖拽 Wails 应用壳、六项导航与 zh-CN i18n 基础，新增 Button/Card/Table/Empty/Error/Skeleton 共享组件及材质、对比度、动效和辅助技术降级语义
32. [TOO-273] 新增真实概览页面与异步 ECharts 趋势图，支持本地日范围、额度与 Reset Credits 新鲜度、Token 构成、API 等价成本、每日明细、近期活动及索引健康的独立 loading/empty/partial/stale/error 语义
33. [TOO-307] 新增 Session 详情的 content-free Turn usage/cost 有界时间线，固定 AEAD opaque cursor、安全归因、active/complete、unknown/真实零、priced/unpriced、Store 跨重启分页及完整页精确/截断页下界对账语义
34. [TOO-274] 实现 Sessions 的可恢复筛选与稳定分页、generated DTO 列表/详情及无正文 Turn 用量成本时间线，补齐安全错误恢复、隐私、键盘焦点、跨日查询和 macOS 原生视觉验证
35. [TOO-308] 新增 Project 精确 Session 数、30 日趋势及 Session/Model 贡献双页查询，固定 GORM 单快照对账、active generation 绑定 AEAD cursor、unknown/NULL 稳定分页与内容无关隐私边界
36. [TOO-275] 实现 Projects 聚合、置信度筛选、稳定分页与详情下钻，支持本地日范围、日级趋势及 Session/Model 贡献，并固定跨日 cursor 隔离、安全错误恢复和不暴露项目路径的隐私边界
37. [TOO-276] 实现 Quota 来源、仲裁与 Reset Credits 页面，展示可信窗口、Local/Wham evidence、冲突与失败状态，新增受节流的双来源手动刷新，并固定 unknown/真实零、不可信重置倒计时及敏感 identity 隔离语义
38. [TOO-277] 实现本机状态与 Settings 页面，新增有限运行控制、Home 两阶段切换和 Session Index Analyze-only 检查，固定局部 unavailable/stale、危险操作确认、权威缓存失效、并发 latest-plan 与敏感路径隔离语义
39. [TOO-278] 统一六页面的有限全局状态、route 错误恢复、缓存事件恢复与键盘焦点语义，补齐局部 partial/live region、危险操作 modal 隔离、平台辅助功能降级和可审计视觉回归证据
40. [TOO-279] 新增 Pure Go GORM runtime metrics 采集与单快照 Job、Scheduler、Source 低基数事实，使用 gopsutil 和 robfig cron 提供 30 秒/5 秒采样、24 小时完整序列、查询延迟与 dropped 恢复语义，并以持久 resume-consumed 标记防止 interrupted Job 在 retention 后错误复活
41. [TOO-280] 新增 typed Health Evaluator、schema v14 有限健康事件与 robfig cron 周期评估，固定持续窗口、暂停/休眠 lane 抑制、唯一优先级、精确事件所有权、原子 observe/resolve/reopen 和 stale projection 语义
42. [TOO-281] 实现 runtime、已解决健康事件、已完成 Job 与来源尝试的固定 24 小时 GORM 分批清理，新增低优先级 PASSIVE WAL checkpoint 和 robfig cron 启动补跑、小时周期、有限退避及运行投影，保证引用保护、取消重算、实时读写优先和应用关闭隔离
43. [TOO-282] 接入七组件 Health Projection 只读查询、主导航健康摘要与唯一全局 Banner，固定权威优先级、影响原因时间、有限恢复入口、缓存失效重取、上次可信和查询失败语义
44. [TOO-283] 新增最近 24 小时 Data Health 二级页面与只读查询，展示七组件健康、current/open 优先的任务事件、CPU/RSS/DB/WAL/磁盘/队列趋势，并固定有限安全恢复、独立评估时间、键盘焦点和敏感信息隔离语义
45. [TOO-287] 新增 macOS AppKit 原生动态额度状态项，按真实 Quota window 自动显示本周单行或 5 小时/本周双行，固定可信、陈旧、冲突、不可用、真实零与独立健康升级语义，并提供主线程安全更新、事件合并、最后可信降级和完整辅助功能标签
46. [TOO-288] 新增 420×760 冻结版 Popover，复用主线程安全的原生状态项 click/anchor 动态展示真实额度窗口、Reset Credits、今日 API 等价成本和最近会话，固定局部查询降级、隐藏取消、缓存保留、持久主窗口跳转及缺失 5 小时窗口不显示占位的语义
47. [TOO-289] 新增 AppKit 原生右键菜单与 typed 窗口命令，支持隐藏/最小化主窗口激活、有限页面深链、双来源权威刷新、原生 About 及 fail-closed shutdown/drain，并固定 `Cmd-W` cancel+hide 生命周期
48. [TOO-290] 新增 AppKit display/Space/wake/appearance typed observer 与有界 Popover 恢复，完善原生状态项实时辅助标签、键盘焦点、Escape 关闭、跨屏 point 坐标 clamp 和 observer/callback 释放，并提供隔离平台事件与 packaged accessibility 回放
49. [TOO-291] 新增 Sparkle 2.9.4 原生 Adapter、typed 更新状态机与应用生命周期装配，固定主线程回调、取消/进度/错误语义、受审 framework 供应链 pin，以及 arm64 Bundle/ZIP 的内嵌、rpath、版本与签名验证
50. [TOO-292] 实现默认每小时的 robfig cron 更新检查、连续失败有界退避与 Settings 更新交互，支持真实版本摘要、签名、进度、下载确认、取消、跳过和稍后，并固定 Sparkle choice、wake、事件合并及等待安全安装的边界
51. [TOO-294] 实现 Quit 与 Sparkle Install 共用的安全关闭状态机、scheduler/Wails admission fence、SQLite 有序关闭和单实例 wake/takeover，固定 timeout 后后台 drain、失败阶段可观察、final reply 恰好一次及崩溃锁回收语义
52. [TOO-295] 实现 migration startup gate 与只读恢复服务图，新增私有备份摘要冻结、可取消重试和二次确认恢复、content-free 审计告警及 SQLite 在线保全与原子文件交换，固定失败不推进版本、canonical path 不缺失和唯一副本不覆盖语义
53. [TOO-296] 建立显式本地发布流水线与真实 Sparkle N-1 升级矩阵，覆盖 Ed25519 签名、替换重启、schema 迁移、坏签名、离线、information-only、migration failure 回滚、恢复态安全安装及 HOME/PID 零残留门禁
54. [TOO-300] 建立统一隐私与敏感字段审计合同，以 synthetic canary 验证 parser、GORM Pure Go SQLite TEXT/BLOB、备份与公共投影，冻结 generated DTO、进程内缓存、启动日志、可编辑设计源、视觉证据和 packaged App regular/symlink 边界，并以独占 lease 保证只清理本轮产物

#### optimization:
1. [TOO-285] 完善 AppIcon、ICNS 与 Tray Template 资产闭环，新增冻结源校验、严格灰阶 1x/2x 派生、macOS bundle/ZIP 资源读回及可重复导出与 live smoke 证据
2. [TOO-298] 优化首次全量索引的 GORM 有界批量冻结、1MiB 读取与分阶段 quota 投影，使用增量 evidence 写入保持首屏后 quota 持续可查询；单次约 6.55GB 真实 Home 样本首屏约 36.4 秒、full bootstrap 约 18.08 分钟，正式阈值与后半程 arbitration 读放大优化留给 TOO-299
3. [TOO-299] 优化首次初始化为生产幂等入队、4MiB 读取、schema v15 quota 过滤与排序索引及增量投影读回，并在 robfig cron 的零 yield cycle 间连续推进且保留 live 抢占和 mutable Home final reconcile；约 6.56GB 真实只读 Home 三轮首屏 p95 约 38.4 秒、full bootstrap p95 约 15.3 分钟，资源、查询、隐私和清理门禁均通过

#### bugFix:
1. [TOO-242] 修正 Wails3 版本探针未捕获 stderr 且未保留 CLI 退出状态的断言，避免 post-merge 验证稳定失败或误报成功
2. [TOO-309] 修复 Tray 与 Popover 在 5 小时额度无有效值时仍显示占位行的问题，隐藏 null primary 且保留真实 `0%` 与后续动态恢复
3. [TOO-310] 修复更新跳过与稍后操作先消费 Sparkle choice、后写偏好造成的半成功窗口，以既有 skip/snooze 偏好承载 durable intent，新增不确定写入读回与启动/available 自动 reconcile，保证存储失败不丢失当前更新且 native failure 可跨重启恢复
4. [TOO-298] 修复真实 rollout 的 session-local turn ID、quota 事件时间回退与重叠 active turn 兼容性，保持 source offset、事实引用和 SessionCurrent 的严格一致性

#### note:
1. [TOO-242] 固定 Wails3 `v3.0.0-alpha2.117` 与 macOS arm64 工具链能力基线，补充可复现 runbook、平台 adapter 边界和依赖升级准入规则
2. [TOO-297] 冻结 v0.1 全链路验收矩阵、证据责任与失败重测规则，并新增逐场景完整性 gate 和六类负向回归

#### script:
1. [TOO-245] 新增本地与 GitHub PR CI 共用的统一验证入口、项目约束负向检查和 macOS 15 arm64 clean-state gate
2. [TOO-284] 新增 macOS arm64 资源开销与 synthetic 故障注入 harness，机械校验 application collector duty、查询延迟、RSS、WAL 及权限、磁盘、锁、坏行、网络、休眠和进程中断恢复矩阵
3. [TOO-286] 新增锁定 Wails3 版本的 Tray、附着窗口与 NSStatusItem 能力探针，真实验证 template、左/右键菜单、窗口生命周期及几何读回，并冻结 AppKit adapter 缺口与 fallback
4. [TOO-293] 新增 Sparkle 2.9.4 EdDSA 本地发布工具链，支持 stdin 私钥签名 arm64 ZIP、纯文本 release notes、appcast 与审计 manifest，并通过内核独占锁、原子目录交换、先验签后解压及失败注入固定秘密隔离和旧产物保留语义
5. [TOO-298] 新增显式 opt-in 的真实 Codex Home 只读验证器，使用隔离 Pure Go GORM SQLite 闭环验证 bootstrap、UTC 成本账本、公共查询、quota、Tray、隐私扫描与安全清理
