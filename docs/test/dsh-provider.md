# DSH 验证与运行边界

关联需求：TOO-533。设计入口：[DSH](../design/details/providers/dsh.md)。验证日期：2026-10-06。以下自动测试使用合成日志/隔离数据库；真实 App 检查单独说明，不将两类证据混用。

## 聚焦自动验证

- Go DSH collector/pricing：明文和 Zstd、最高代次、分叉 seed、失败尝试与最终消息分别计量、损坏/未完成尾部保留 last-good、重复 JSON 与序号拒绝、可选计数未知传播。请求起点跨峰时边界按起点计费，重试缺少独立起始时间不猜价。
- 美元费率：峰时起止半开区间、周末、2026 中国假期、模型/路由/历史生效边界及缓存写价格缺失。缺证据不按零或当前价格补造。
- Store / Preferences / Provider / Core：SQLite fresh 和历史 migration、DSH 结构化事实及 nullable 字段、历史裁剪胶囊、来源不可用保留历史、四客户端控制面、旧设置兼容、停止 admission/drain 与汇总范围。
- 上报队列：合成官方日志经 collector → Store → Exporter → 持久队列；核对 USD、隐私白名单、重启后批次字节不变、重复扫描不重复入队、关闭 DSH 后禁止新导出但保留旧队列。同一 Session 的文件副本只计一次，内容分歧标记 partial/lineage conflict；项目范围及筛选不混入全局或生命周期请求数。
- Server：既有 reporting/statistics/catalog/quota 与 repository 聚焦测试；新增 DSH 真实服务与隔离 SQLite 数据库回归，验证三设备副本去重、USD 事件成本及原价格版本、缓存与生命周期吞吐量、Codex/DSH 筛选隔离。测试不是独立 MySQL 8.4 验收。
- Swift：App executable 确定性测试；DSH 独立菜单栏日历范围及 Token，缺少官方额度不产生剩余百分比；设置发送四个客户端并保留不可编辑字段。状态栏图形按用户要求固定满格，真实用量独立显示。开发 App / Helper 构建完成。
- Web：Usage / Overview / Pricing 三组共 25 个测试、TypeScript、AntD 检查及构建通过；本次没有新增依赖。Zstd 固定依赖 `github.com/klauspost/compress@v1.20.1`，OSV 按该版本查询未返回公告。

可复用入口（仅在开发新改动需要验证时运行；提交/发版收尾不重复测试）：

```sh
go test ./internal/dshprovider ./internal/pricing -count=1
go test ./internal/store -run 'DSH|EnsureApplicationSchema|Migration' -count=1
go test ./internal/query/runtimeinfo ./internal/core ./internal/providerrefresh -count=1
# server module
go test ./internal/service/statistics_srv -run DSH -count=1
# repository root
swift run --package-path app/macos codex-pulse-app-tests
# server/web
npm run typecheck
npm test -- --run src/pages/Usage.test.tsx src/pages/Overview.test.tsx src/pages/Pricing.test.tsx
```

自动 App smoke 显式绑定隔离 DSH Home 和 sessions root，避免使用个人 DSH 数据。真实 live smoke 接受启用范围中存在 DSH；不强行把缺少客户端的环境算成四个已启用客户端。

## 真实 Mac 数据检查

开发 App 使用显式真实 Codex Home 和 mode `0700` 私有 runtime。回读 preferences 的 canonical path/inode/device identity、App/Helper 环境和 Helper 参数：Home 身份与预期一致，Helper 仍通过私有 UDS 提供 CoreService。初次 DSH 默认目录扫描得到 10 个会话、923 条 usage，缓存分量完整；后续实际页面刷新可随本机 DSH 活动增加，这些计数不是静态验收基准。

已通过真实 App 查看 DSH 客户端选择、设置发现、近 7 天概览 Token/模型/项目/成本/活动、本地工具统计、会话列表及详情的缓存/吞吐量与四行 USD 峰谷价格目录。费用按 API 公价估算，不证明桌面账户实际扣费。本地 Store 上报导出校验得到 10 个 Session、1112 条 contribution 全部通过 v1 契约；仅输出脱敏计数，没有进行网络上传。退出开发 App 后确认进程停止、私有 socket 删除；保持 App 托管 Helper 的生命周期。

原始本机证据只保留在忽略的 `.artifacts/`，本文件不包含个人 Home、真实会话 ID、消息内容、凭据或完整日志。真实 App 检查没有配对生产中心或上传个人数据。

## 尚未执行的验证

本节是发版前的开发边界。v0.16.0 / v0.16.1 的发版与三机读回在后文。

本次未执行全仓长测、全仓 race、CI、独立 MySQL 8.4、真实生产中心端到端上传、签名、公证、发版或部署。Server 合成端到端回归不能替代这些结论。发布顺序为 Server 先升级，Mac 再升级；旧中心拒绝新客户端时应保留队列，不能清空或伪造 receipt。

架构检查入口 `scripts/project-checks/check.sh` 在仓库基线缺少 `.github/workflows/ci.yml` 时被 `[CI-001]` 阻止，未取得通过结论；本次没有扩大范围补建 CI。

更早价格和未知年份的假期尚缺完整证据，按设计保留 unpriced；更新这些规则需要独立核验官方来源与生效时间。

本次 diff 安全自查：复用既有认证与来源授权；文件访问限定在 DSH 根目录，日志和解压有界，正文/凭据不落库或上报；未新增服务端外部请求、动态执行或本机 TCP 监听。

## 用户验收调整

2026-10-06 按用户要求移除 Mac 客户端独立的“调用统计”页面，包括所有客户端侧栏、页面路由、概览跳转入口和独立页面加载任务。旧页面偏好通过现有未知页面恢复规则回到概览；概览的调用画像继续展示已有结构化汇总。原生页面 smoke 数量调整为 10。

调整后的 App 构建通过，真实 DSH 窗口已核对入口移除并留在前台。Swift executable 测试首轮在既有重连恢复断言出现时序失败，未改动该断言，重跑通过；两轮原始结果保留于 `.artifacts/`。本次没有新增数据传输或权限入口。

## DSH 会话名称补齐

2026-10-06 根据官方 `session/title` contract 补齐名称：最新事件优先，包含自动命名、手动改名及 V3/V4 seed 继承。DSH 自身 fallback 生成的标题同样属于 Provider 元数据；仅缺少标题记录时使用 Pulse 的“未命名会话”。辅助 `session/title-llm-request` 不作为标题来源，也不保存其正文。

聚焦 DSH collector / Store / reporting 测试通过，覆盖 Unicode 长度约束、损坏标题、相同日志摘要下的历史标题补齐、贡献身份不变和队列携带名称。受影响 Store schema / migration 回归通过；v36 checksum 保持不变，v37 保留 lineage、usage、工具事实并验证失败回滚及重试。开发 App / Helper 构建通过。

复用原私有 runtime 与真实 Home 启动，回读 schema v37、preferences 与 App/Helper 环境及私有 socket 参数。真实列表显示 11 个会话，其中 10 个具有官方标题记录；列表与选中会话详情的名称一致。客户端留在会话页供用户查看。原始证据只保留在 `.artifacts/`，不记录真实标题文本。本次未执行生产中心上传或部署。

本次标题 diff 安全自查：仅接入有界标题元数据，沿用现有会话身份、认证与上报权限；正文/凭据不新增持久化，未新增外部请求或执行入口。

## v0.16.0 发布与三机部署（2026-10-06）

用户确认 v0.16.0/build 67、Server 先升级后 Mac、三机更新，提交和发版收尾不重复测试。已上线 TOO-530 通知改动先通过 [PR #196](https://github.com/SisyphusSQ/codex-pulse/pull/196) 合入 main，DSH 与发行归档通过 [PR #197](https://github.com/SisyphusSQ/codex-pulse/pull/197) 合并。两台发行相关机器的干净 main 经 `pull --ff-only` 同步，源码冻结为 `3904b8ad8d1d5d9465e7ac01ea6e9e70aba730c0`；signed annotated tag `v0.16.0` 的远端 peeled commit 与其一致。

SQMC05 构建 Mac DMG、Sparkle 更新 ZIP 和内嵌 Web 的 Darwin arm64 Server 包。Mac 构建显式跳过 App 测试；Server 首次因未安装 Web 依赖缺少 tsc，按既有锁文件安装后构建成功。发行资产仍为完整 ad-hoc 签名，非 Developer ID/Apple 公证；DMG 只读挂载确认 App 与 Applications 链接、版本/build 和 codesign 完整性。Gatekeeper 预期拒绝（exit 3），stapler 无票据（exit 65），公开说明如实披露。

[GitHub stable Release v0.16.0](https://github.com/SisyphusSQ/codex-pulse/releases/tag/v0.16.0) 已公开、非 Draft、非 prerelease，含 DMG、ZIP、Server 包、两份摘要与两份说明共七个资产。SQMC04 的首轮大文件上传因 GitHub 408 失败，改由 SQMC05 上传相同资产成功。更新源曾在上传完成前推进，发现后立即恢复上一版；正式发行公开、七个资产经公开下载核对摘要后，再将固定 Sparkle feed 切至 0.16.0/build 67。最终 feed 与已签名候选字节一致，普通及避缓存 URL 均读回；临时签名 LaunchAgent 已卸载。

SQMC04 生产 Server 已先切至 `v0.16.0-3904b8a`，一致停服数据库备份和旧 `v0.15.3-too530-4131560` 发行保留，私有配置未变；LaunchAgent running、ready 200，schema v5 与 checksum 均未改变。DEV 服务未切换。三机客户端安装结果：

| 机器 | 安装版本 | 运行及迁移 | 原配置与备份 |
| --- | --- | --- | --- |
| SQMC04 | 0.16.0 / 67 | App/Helper 正常，SQLite v37、Preferences v5 | 真实 Home 物理身份、显式环境/Helper 参数及 0700 runtime 已读回；配对、启用、600 秒同步与补传范围保留；旧 App 和事实/队列/凭据库备份保留 |
| SQMC03 | 0.16.0 / 67 | 原本关闭，保持关闭；迁移留待下次启动 | 真实 Home 身份和原偏好/上报配置未变，旧 App 与数据库备份保留；未改动该机独立开发分支 |
| SQMC05 | 0.16.0 / 67 | App/Helper 正常，SQLite v37、Preferences v5 | 真实 Home 物理身份、显式环境/Helper 参数及 0700 runtime 已读回；原配对、启用、600 秒同步与补传范围保留；旧 App 和数据库备份保留 |

三个内嵌 Helper SHA-256 一致：`25346484a5873a897a15c3e2255d01a9514912c9cda01cc0cc254302a65d3bb2`。发行 ZIP：`6d56972a2648ad9062f5333b79b18809cde007b8d4f21505e4344f47b4649dea`；DMG：`4930933f4f0bfde2f9e8e7c5916f5de8b19705ff75c0b7abb5a9436cc0e0e47d`；Server 包：`9a3557aba79021c21b4d1e619f5fedc3154864ccef95ad135793ef94e756cb91`。

生产中心已自然接收 DSH 的 11 个会话及标题。原日志仍持续增长，本机最新事实与中心上次同步快照可随 600 秒同步间隔暂时不同；没有把这一运行读回描述成完整 Mac→Server→Web E2E 或长期稳定性验收。此前列出的全仓长测、CI、独立 MySQL 8.4、完整业务 E2E、Sparkle 更新 UI E2E、全新 macOS 用户首启、公证等未执行项仍保留。GitHub Actions 按已有仓库决定保持关闭。

发布安全自查：原认证、设备配对、权限和网络入口保留；发行包无私有配置、凭据或原始日志，私有备份/日志仅存放于各机忽略的发行证据目录。本次按约定未重复执行测试，构建和发行/安装结果读回不作为新增测试结论。


## TOO-534：DSH Codex 模型计价（2026-10-06）

开发分支 `suqing/too-534-dsh-codex-pricing`。在干净 main 上完成 `pull --ff-only`，当时本地与远端一致，再切分支。新增 `openai-codex` 精确模型历史价格匹配；本地查询与上报复用同一 DSH 互斥分量计算，reasoning 不重复计入 output。单一价格来源、混合版本、轮次/项目详情、原生和中心价目与费用说明已适配。没有 Proto、依赖、Preferences、数据库 schema 或凭据变更。

自动验证全部使用合成日志/隔离 SQLite：

- `go test ./internal/pricing ./internal/dshprovider ./internal/query/pricingcatalog ./internal/reporting -run DSH -count=1`：路由/型号/历史生效边界、旧价保持、OpenAI 不受峰谷/假期限制、缓存写价格缺失、真零、混合来源、unknown 重试、DSH collector → Store → 查询/导出一致、持久队列与关闭 gate。
- `go test ./internal/pricing ./internal/dshprovider ./internal/query/pricingcatalog ./internal/store -run 'DSH|TestCurrent' -count=1`：相关查询、Store 和原 DeepSeek 回归通过。
- Server 在 `make web-build` 后运行 `go test ./internal/service/catalog_srv ./internal/service/statistics_srv -run DSH -count=1`：参考/历史目录、已有型号不误标 unknown、旧费用修订、三设备副本和重复批次通过。测试验证 Token 与会话数不变，费用采用更完整证据，仍归 DSH。
- Web 类型检查与构建通过，`Usage.test.tsx` / `Pricing.test.tsx` 共 13 项通过。两个受影响组件的 AntD lint 均零问题。测试环境有 jsdom 的伪元素 `getComputedStyle` 未实现提示，不影响测试结果。
- `swift build --package-path app/macos --product codex-pulse-app` 通过；编译仍报告既有 RootView 的 weak/strong capture 警告，本次未改动该闭包。

历史不依赖原日志增长：直接从既有 Store 重读得到相同贡献与价格。模拟旧 unpriced 队列后补价格形成 revision 2，验证旧批次字节不变、贡献 ID 不变且同修订不重复入队；不需要新 schema、清空队列或全库重扫。以上为开发证据，不代表已更新真实个人历史或完成 Mac → 生产中心端到端验收。

未执行：新版 App 的真实 Home/UI 验收、个人数据上传、独立 MySQL、全仓长测/race、CI、签名、公证、commit/push、发版或生产部署。缺少独立起始时间的请求仍未计价；任何未计价请求都会使完整费用合计 unknown。部署升级后旧历史将在后续同步遍历或既有全量补传中更新，本次未操作生产数据。

本次 diff 安全自查：认证、CSRF、来源权限与幂等机制保持原规则；无新增服务端外部请求或执行入口，无凭据/正文/个人路径落库或上报；前端只展示 Go 计算结果，金额使用既有有界整数精度。

## v0.16.1 发布与三机部署（2026-10-06）

用户确认按 Server → SQMC04 试点 → 公开发行及更新源 → SQMC05/SQMC03 的流程执行。实现及 CHANGELOG 经 [PR #199](https://github.com/SisyphusSQ/codex-pulse/pull/199) 合并；SQMC04、SQMC05 的干净 main 分别完成 `pull --ff-only`，发行源码冻结为 `53bcfc952b1c4a61fc00461c6c01d5d954a72a30`。signed annotated tag `v0.16.1` 的签名及远端 peeled commit 均已读回。

SQMC05 从同一 commit 构建 Mac App/Helper 与内嵌 Web 的 Darwin arm64 Server 包。App 构建显式使用 `--skip-app-tests`；本次按约定未重复执行测试，复用上节的开发测试证据。最终 ZIP 和只读挂载的 DMG 均通过 Bundle/build、布局及 ad-hoc codesign 完整性读回。Gatekeeper 预期拒绝（exit 3）、stapler 无票据（exit 65）；未执行 Developer ID 签名或 Apple 公证，公开说明按实际信任等级披露。

[GitHub stable Release v0.16.1](https://github.com/SisyphusSQ/codex-pulse/releases/tag/v0.16.1) 已公开、非 Draft、非 prerelease，含 Mac DMG/ZIP、Server 包、两份摘要及两份说明共七个资产。七个资产通过匿名公开下载与候选 SHA-256 比对，下载后的 Mac 签名及 DMG 内容再次读回。之后才更新固定 Sparkle feed 至 0.16.1/build 68；普通和避缓存 URL 均与已签名候选字节一致。签名私钥只在 SQMC05 既有 Keychain 与 stdin 中参与操作，临时签名 LaunchAgent 已卸载。

SQMC04 生产 Server 已切至发行目录 `v0.16.1-53bcfc9`，实际 CLI 版本为 `v0.16.1`、Git commit `53bcfc9`，ready 200。数据库一致备份及旧发行目录保留，原私有配置未变；中心 schema v5 与 checksum 保持一致，DEV 未切换。三台 Mac 均安装 0.16.1/build 68，旧 App、Preferences、事实/队列/凭据数据库备份保留；原配对、600 秒同步间隔及补传范围未变。三台安装现场均为运行状态，更新后 App/Helper 正常；SQLite v37、Preferences v5、真实 Home 物理身份、显式环境/Helper 参数和 0700 runtime 已逐机读回。SQMC03 的独立源码分支未切换。

三机 Helper SHA-256 均为 `587998918f0b1d90bb1c80f548be0ae935e4d891e9fce4034df7da360144d59a`。ZIP 为 `1974c1cf17934fa60ca8a8f714372e38fecab87c5cfa6fab5cc4ab90e43e8d3b`，Server 包为 `72f21455e7bb7666c72b0f1d0c3aee05c55de1755797bab3e2fdc8d5992f69cd`；完整资产摘要保存在公开 Release。

SQMC04 试点读回 DSH 价格目录的 OpenAI 历史版本和完整会话的非零费用。通过既有“全量补传”重发当前范围，未改变起点、重扫日志或清空队列。固定的本机 220 条 Codex usage 全部保留且事实未变；中心原有 1,876 条 DSH 贡献全部保留，模型与 Token 数未变。其中原有 198 条 Codex 贡献，197 条补为已计价，1 条缺独立请求开始时间仍未计价。现场新记录继续增长；对读回时 244 条已计价 Codex 贡献逐条核对费用与 `openai-api-2026-09-29`，零价格差异。此为实际历史修订与定向 Mac → 中心证据，不代表三机完整业务 E2E 或长期稳定性。

用户询问概览空值时，读回 17 个会话中 16 个 exact、1 个 partial；后者记录 104 次请求但只有 103 条 usage。现有 `markMissingUsage` 故意将包含缺 usage 会话的完整 Token/费用合计保留为未知；年度摘要无法与完整合计对账时也不显示完整值，已知模型与趋势仍显示。费用方面，缺独立请求起点的重试同样保留未知；不把缺失值补零，也不承诺下一次同步能补齐。此次未改变这些既有规则。

全量补传在本轮读回时仍由既有队列后台推进；上述 DSH 历史费用已收到，未声称所有客户端历史补传已结束。退出 App 会暂停，下次启动继续。未执行全仓长测/race、CI、独立 MySQL 8.4、全新 macOS 用户首启、Sparkle 自动更新界面的完整 E2E 或长期稳定性验收。

上线安全自查：原认证、Origin/CSRF、配对和来源权限保持；发行包不含私有配置、凭据或源日志，备份和原始运行证据只保存在各机私有忽略目录。未关闭系统信任或网络安全控制。
