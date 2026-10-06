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
