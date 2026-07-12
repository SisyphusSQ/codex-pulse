# Codex Pulse Design

本目录是 Codex Pulse 的版本化设计真相。产品与技术模块按职责归档在 [details/](details/README.md)，Pencil 源稿、页面预览和正式图标资产归档在 [front/](front/README.md)。

## 文档地图

| 区域 | 内容 |
| --- | --- |
| [details/product](details/product/README.md) | 产品目标、页面信息架构、用量与成本口径、v0.1 范围和实施阶段 |
| [details/architecture](details/architecture/README.md) | Go + Wails3 分层、provider 边界、隐私安全和本机数据目录 |
| [details/data-model](details/data-model/README.md) | JSONL 增量索引、SQLite schema、幂等事务、日聚合与保留策略 |
| [details/quota](details/quota/README.md) | 配额来源、可信状态、仲裁、失败降级和验收场景 |
| [details/scheduling-and-bootstrap](details/scheduling-and-bootstrap/README.md) | 数据源刷新、前后台预算、首次启动和错误恢复 |
| [details/updates-and-release](details/updates-and-release/README.md) | 自动更新、安全重启、数据库 migration 和发布可信链 |
| [details/observability](details/observability/README.md) | 资源、队列、故障、健康分级与 Data Health |
| [details/research](details/research/README.md) | 调研结论、数据源模式、参考项目和链接 |
| [front](front/README.md) | Liquid Glass 视觉方向、Pencil 源文件、预览与图标资产 |

项目的过程记录和长期决策继续维护在个人 agent-memory 项目记录中，不作为本仓库的版本化设计正文。
