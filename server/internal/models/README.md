# 模型目录

本目录始终保留，与是否生成 examples、是否启用数据库无关。

- [DO](do/README.md)：数据库记录与存储映射。
- [DTO](dto/README.md)：跨服务、消息和外部系统边界的数据。
- [VO](vo/README.md)：HTTP 请求、校验和对外响应。

完整约束见 [模型分层](../../docs/design/architecture/models.md)。新增业务先定义边界，再选择类型；不能为了字段相同让三个边界共用一个结构体。
