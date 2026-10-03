# 设计与开发文档

采用 architecture 保存跨模块长期约束，details 按主题保存实现契约。新增业务设计放 details/<domain>/，同时更新本索引；不把所有设计平铺在 docs 或单个大文档内。

| 入口 | 内容 |
|---|---|
| [architecture](architecture/README.md) | 分层、依赖方向、Fx 生命周期与扩展边界 |
| [模型约定](architecture/models.md) | DO / DTO / VO 的定义、目录和转换责任 |
| [details](details/README.md) | 开发、构建和运行的主题索引 |

阅读顺序：架构 → 模型约定 → 代码规范 → 受影响模块细节。设计记录目标、范围、不包含项、接口/数据、失败语义、验证入口和初始化要求；真实执行结果写入 docs/test，临时执行计划写入 .agents/plans。
