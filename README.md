# Codex Pulse

**看清 Codex 在本机如何消耗、额度还剩多少，以及当前数据是否可用。**

Codex Pulse 是一款 local-first 的原生 macOS 应用：把 Codex 分散在本机会话、用量记录和额度窗口中的信息，整理成菜单栏状态与可下钻的分析界面，同时说明数据的新鲜度、完整性与健康状态。

![Codex Pulse 概览中的额度、年度活动热力图与项目消耗，动态数据已脱敏](docs/assets/codex-pulse-overview-redacted.png)

*基于真实 Codex Home 的界面截取；侧栏和通用产品文案保留，账号、项目、Session、模型、数值、日期和运行时明细均已不可逆脱敏，图表的形态、颜色与布局保留。*

## 主要功能

- **菜单栏**：查看额度、重置时间和健康提醒。
- **用量分析**：在概览、会话和项目页面查看 Token、模型、API 等价成本与活动分布。
- **数据状态**：查看额度来源、本机索引和后台任务的状态，了解统计结果是否完整。

## 功能概览

| 区域 | 你可以看到什么 |
| --- | --- |
| 菜单栏 | 额度剩余、累计 Token、重置时间和健康提醒 |
| 概览、会话与项目 | 趋势和热力图、模型与成本拆分、高消耗 Session，以及项目关联的 Session |
| 状态与设置 | 额度周期和来源、索引进度和新鲜度、后台任务、本机存储和设置 |

主窗口包括概览、会话、项目和配额页面；运行诊断、数据来源和设置位于系统区域。菜单栏用于查看当前状态，主窗口用于查看用量明细和数据状态。

## 产品界面

以下界面均基于真实 Codex Home 截取，并使用相同的脱敏规则：固定导航和产品文案可读，项目、会话、模型、数值、成本与时间等动态数据均已不可逆处理。

### 概览：趋势与活动分布

![Codex Pulse 概览中的 Token 趋势、活动分布与高消耗会话，动态数据已脱敏](docs/assets/codex-pulse-activity-redacted.png)

### 菜单栏

<p align="center">
  <img src="docs/assets/codex-pulse-popover-redacted.png" alt="Codex Pulse 菜单栏 Popover，动态数据已脱敏" width="420">
</p>

### 项目列表与详情

![Codex Pulse 项目页面中的列表、趋势、模型与会话下钻，动态数据已脱敏](docs/assets/codex-pulse-projects-redacted.png)

## 数值不确定时

额度和用量工具最容易产生误导的地方，不是没有数据，而是把获取失败后的默认值当成真实结果。Codex Pulse 使用以下显示规则：

- `0%` 只表示已经确认耗尽；从未取得、尚未计算或当前不适用时显示 `--`；
- 在线刷新失败但已有上次成功获取的数据时，继续展示 last-known-good，而不是突然变成 100%；
- 时间范围尚未索引完整时标记为“部分数据”，不把局部结果冒充完整统计；
- 额度名称与周期来自当前数据，例如按真实 `window_minutes` 生成周期标签，不硬编码“5 小时额度”；
- 金额始终标为“API 等价成本”，用于理解 Token 对应的公开 API 价格量级，不代表真实账单或实际扣费。

## Local-first 与隐私

所有分析都在本机完成：

- 只读发现和增量索引本地 Session，结构化结果只保存在本机 SQLite；不复制完整对话正文，也不持久化 token、Authorization header 或 RPC token。
- 在线 quota 与 Reset credits 可以关闭，凭证仅在请求期间进入内存；不提供云同步或公网访问。
- Swift App 与 Go Helper 只通过私有 Unix Domain Socket 通信；日志、错误和 UI 返回值不包含原始 payload、完整路径或底层错误。

Codex 原始文件仍由 Codex 自己管理。Codex Pulse 只保存产品功能所需的索引、统计和运行状态，不修改原始 Session 内容。

首次启动时，Go Helper 会对 `${CODEX_HOME:-$HOME/.codex}` 做不读取会话正文的 metadata-only 安全探测，并保存稳定身份；目录不存在或探测失败时保持未配置、不开始索引。之后更换 Codex Home 仍需在设置中显式确认。

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

[`api/codexpulse/core/v1/core.proto`](api/codexpulse/core/v1/core.proto) 定义了跨进程接口。Go Helper 负责读取、索引和汇总数据；Swift App 通过 generated CoreService 调用 Helper，不直接读取 SQLite 或 JSONL，也不在 UI 层重新汇总数据。

## 从源码运行

环境要求：

- macOS 15+
- Apple Silicon
- Go 1.25
- `protoc 34.1`

本地运行使用真实 `${CODEX_HOME:-$HOME/.codex}`。下面的命令会只读 Session / JSONL，并可能在私有 App runtime 中写入 SQLite、偏好、运行日志和 App Server 的常规 housekeeping；不会修改原始 Session 内容：

```bash
make verify-live
```

`make verify-live` 会构建 development App、复用已确认的私有 runtime，并使用真实 Home 启动应用。CI、单元测试和确定性 smoke 使用 synthetic / empty Home，避免读取个人数据。

## 开发与验证

日常开发优先运行受影响的 Go package 或 Swift executable tests。常用命令如下：

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
  --build-number 4 \
  --sparkle-feed-url \
    https://github.com/SisyphusSQ/codex-pulse/releases/download/updates/appcast.xml \
  --sparkle-public-key-file \
    /secure/path/codex-pulse-sparkle-public.key

# 修改 Proto 后重新生成 Go / Swift 代码
make generate-proto
```

发行候选写入 `.artifacts/releases/<tag>/`，包含首次安装 DMG、内嵌 Sparkle
的 Apple Silicon App ZIP 与覆盖两项资产的 `SHA256SUMS`。DMG 提供
`Codex Pulse.app` 到 `/Applications` 的标准拖拽入口；appcast 仍只指向
exact ZIP。公钥文件可公开，但必须与通过 stdin 用于 appcast 签名的私钥配对。
stable 与 preview 是产品发布渠道，macOS 信任等级另行记录为 `unsigned` 或
`signed-notarized`。stable 默认沿用 `unsigned stable`：发行资产采用 ad-hoc
签名，Release Notes 必须披露未完成 Developer ID 签名、未公证、非 Gatekeeper trusted，
以及首次打开时在“系统设置 → 隐私与安全性”中“仍要打开”的操作；远端发布还
必须经过 tag、Release、资产摘要、固定 appcast 和首次打开流程的独立读回。
`signed-notarized` 仅显式 opt-in，只有现场签名、公证、Gatekeeper 和最终资产
readback 全部通过时才允许。preview 仍使用 prerelease SemVer，并按实际资产
选择 unsigned 或 signed-notarized。

主要目录：

| 路径 | 职责 |
| --- | --- |
| [`app/macos/`](app/macos/) | 原生 SwiftUI / AppKit 应用、Core client 与 executable tests |
| [`api/codexpulse/core/v1/`](api/codexpulse/core/v1/) | Protobuf 接口定义与生成代码 |
| [`internal/`](internal/) | Go Helper 的索引、查询、调度、持久化和运行时实现 |
| [`docs/design/`](docs/design/) | 产品、架构、数据、额度、调度与可观测性设计 |
| [`docs/test/`](docs/test/) | 测试说明和脱敏结果摘要 |

更多细节从以下文档开始：

- [产品设计](docs/design/details/product/README.md)
- [系统架构](docs/design/details/architecture/README.md)
- [数据模型](docs/design/details/data-model/README.md)
- [额度数据说明](docs/design/details/quota/README.md)
- [调度与首次索引](docs/design/details/scheduling-and-bootstrap/README.md)

## License

[MIT](LICENSE)
