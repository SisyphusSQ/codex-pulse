# Codex Pulse

**看清 Codex 在本机如何消耗、额度还剩多少，以及当前数据是否可信。**

Codex Pulse 是一款 local-first 的原生 macOS 应用。它把 Codex 分散在本机会话、用量记录和额度窗口中的信息整理成菜单栏状态与可下钻的分析界面，帮助你快速定位高消耗项目和 Session，同时持续说明数据的新鲜度、完整性与健康状态。

> 项目仍在积极开发中。目前面向 macOS 15+ / Apple Silicon，主要通过源码构建和本机验收，暂未提供正式签名、公证的发行版。

## 它能回答什么

- 当前额度还剩多少，何时重置，这个数值是否仍然可信；
- 今天或最近一段时间用了多少 Token，主要消耗在哪些模型、项目和 Session；
- 某个 Session 的使用量、API 等价成本和 Turn 时间线是什么；
- 本地历史是否索引完整，后台任务是否正常，哪些异常正在影响数据；
- 数据来自本地还是可选在线接口，失败后界面展示的是最新值、上次可信值还是未知状态。

## 产品体验

| 区域 | 你可以看到什么 |
| --- | --- |
| 菜单栏 | 额度剩余、同一额度周期内的累计 Token、重置时间与必要的健康提醒，无需打开主窗口 |
| 概览 | 周额度、今天、最近 7 天或 30 天的使用趋势，按模型拆分的 Token、API 等价成本和高消耗 Session |
| 会话 | Session 的项目、模型、活跃状态、Token、成本与不含对话正文的 Turn 用量时间线 |
| 项目 | 按项目聚合的用量与成本排行，并继续下钻到相关 Session |
| 配额 | 通用额度与模型专属额度的真实周期、剩余比例、重置倒计时、Reset credits 和来源状态 |
| 本机状态 | 数据完整性、历史补齐进度、索引新鲜度、后台任务、SQLite、存储与最近运行情况 |
| 设置 | Codex Home、在线额度能力、刷新周期、启动行为、概览范围和更新偏好 |

主窗口采用“概览 → 会话 → 项目 → 配额”的分析路径；运行诊断、数据来源和设置收拢在系统区域。菜单栏负责快速判断，主窗口负责解释消耗去了哪里，本机状态负责说明数据为什么可信或为什么暂时不可用。

## 不把未知伪装成正常

额度和用量工具最容易产生误导的地方，不是没有数据，而是把失败后的默认值当成真实结果。Codex Pulse 对此使用明确的展示语义：

- `0%` 只表示已经确认耗尽；从未取得、尚未计算或当前不适用时显示 `--`；
- 在线刷新失败但仍有可信历史观测时，继续展示 last-known-good，而不是突然变成 100%；
- 时间范围尚未索引完整时标记为“部分数据”，不把局部结果冒充完整统计；
- 额度名称与周期来自当前数据，例如按真实 `window_minutes` 生成周期标签，不硬编码“5 小时额度”；
- 金额始终标为“API 等价成本”，用于理解 Token 对应的公开 API 价格量级，不代表真实账单或实际扣费。

## Local-first 与隐私

Codex Pulse 的分析链路运行在本机：

- 只读发现和增量索引 Codex 本地 Session 数据，结构化结果保存在本机 SQLite；
- 不复制完整对话正文，不持久化工具输出、access token、refresh token、Authorization header 或 RPC token；
- 在线 quota 与 Reset credits 是可关闭的独立能力，凭证仅在请求期间进入内存；
- 不提供云同步或公网访问，Swift App 与 Go Helper 只通过私有 Unix Domain Socket 通信；
- 日志、公开错误和 UI contract 不返回原始 payload、完整路径或底层错误文本。

Codex 原始文件仍由 Codex 自己管理。Codex Pulse 只保存产品功能所需的索引、统计和运行状态，不修改原始 Session 内容。

首次启动且尚无应用偏好时，Go Helper 会自动选择
`${CODEX_HOME:-$HOME/.codex}`，先执行不读取会话正文的 metadata-only 安全探测，
再保存 canonical path、device 和 inode；这一默认来源不要求用户手工确认。
默认目录不存在或未通过安全探测时，应用保持未配置且不开始索引。之后更换为
其他 Codex Home 仍需在设置中显式确认。

## 工作原理

Codex Pulse 由两个本地进程组成：

```text
Codex 本地数据 / 可选在线额度
             │
             ▼
   Go Helper：发现、索引、聚合、调度、SQLite
             │  Protobuf / gRPC over UDS
             ▼
   Swift App：菜单栏、窗口、交互与 Helper 生命周期
```

[`api/codexpulse/core/v1/core.proto`](api/codexpulse/core/v1/core.proto) 是唯一跨进程 contract。Go Helper 负责数据和业务口径，Swift App 只消费 generated CoreService，不直接读取 SQLite 或 JSONL，也不在 UI 层重新计算业务事实。

## 从源码运行

环境要求：

- macOS 15+
- Apple Silicon
- Go 1.25
- `protoc 34.1`

本地产品验收使用真实 `${CODEX_HOME:-$HOME/.codex}`。下面的命令会只读 Session / JSONL，并可能在私有 App runtime 中写入 SQLite、偏好、运行日志和 App Server 的常规 housekeeping；不会修改原始 Session 内容：

```bash
make verify-live
```

`make verify-live` 会构建 development App、复用已确认的私有 runtime，并启动真实 Home 验收。CI、单元测试和确定性 smoke 则使用 synthetic / empty Home，避免读取个人数据。

## 开发与验证

日常开发优先运行受影响的 Go package 或 Swift executable tests。统一入口如下：

```bash
# Go / Swift 分项测试
make test-go
make test-swift

# 提交前产品检查
make check

# PR / CI 完整验证，使用隔离 Home
make verify

# 组装本地 unsigned preview 候选，不创建 tag 或 GitHub Release
scripts/macos/build-release-app.sh \
  --version 0.1.0-beta.1 \
  --build-number 4

# contract 修改后重新生成 Go / Swift 代码
make generate-proto
```

发行候选写入 `.artifacts/releases/<tag>/`，包含 Apple Silicon App ZIP 与
`SHA256SUMS`。未签名、未公证的 preview 不能当作 stable；远端发布还必须
经过 tag、Release、资产摘要和首次打开流程的独立读回。
preview 可在逐次明确授权后以 ad-hoc 签名的 GitHub prerelease 形式提供；
stable 发行必须具备 Developer ID 签名、公证和对应安装验证。

主要目录：

| 路径 | 职责 |
| --- | --- |
| [`app/macos/`](app/macos/) | 原生 SwiftUI / AppKit 应用、Core client 与 executable tests |
| [`api/codexpulse/core/v1/`](api/codexpulse/core/v1/) | Protobuf contract 与生成代码 |
| [`internal/`](internal/) | Go Helper 的索引、查询、调度、持久化和运行时实现 |
| [`docs/design/`](docs/design/) | 产品、架构、数据、额度、调度与可观测性设计 |
| [`docs/test/`](docs/test/) | 可复用验收 runbook 与脱敏结果摘要 |

更多细节从以下文档开始：

- [产品设计](docs/design/details/product/README.md)
- [系统架构](docs/design/details/architecture/README.md)
- [数据模型](docs/design/details/data-model/README.md)
- [额度可信模型](docs/design/details/quota/README.md)
- [调度与首次索引](docs/design/details/scheduling-and-bootstrap/README.md)

## License

[MIT](LICENSE)
