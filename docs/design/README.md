# Codex Pulse Design

本目录是 Codex Pulse 的版本化设计真相。产品、Go Helper、原生 macOS 客户端迁移和各技术模块按职责归档在 [details/](details/README.md)。

## 文档地图

| 区域 | 内容 |
| --- | --- |
| [details/product](details/product/README.md) | 产品目标、页面信息架构、用量与成本口径、v0.1 范围和实施阶段 |
| [details/architecture](details/architecture/README.md) | 当前 Go Helper 与目标 Swift native client 分层、RPC 边界与本机安全 |
| [details/native-macos-client](details/native-macos-client/README.md) | 原生 macOS 客户端与 Go Helper 重构决策、RPC contract、生命周期、迁移阶段和切换门槛 |
| [details/data-model](details/data-model/README.md) | JSONL 增量索引、SQLite schema、幂等事务、日聚合与保留策略 |
| [details/quota](details/quota/README.md) | 配额来源、可信状态、仲裁、失败降级和验收场景 |
| [details/scheduling-and-bootstrap](details/scheduling-and-bootstrap/README.md) | 数据源刷新、前后台预算、首次启动和错误恢复 |
| [details/updates-and-release](details/updates-and-release/README.md) | 自动更新、安全重启、数据库 migration 和发布可信链 |
| [details/observability](details/observability/README.md) | 资源、队列、故障、健康分级与 Data Health |
| [details/research](details/research/README.md) | 调研结论、数据源模式、参考项目和链接 |

项目的过程记录和长期决策继续维护在个人 agent-memory 项目记录中，不作为本仓库的版本化设计正文。
