# DO：持久化模型

MySQL 模型放 mysql/<domain>，MongoDB 模型放 mongo/<domain>；仅选择相应数据库时才生成数据库专属目录。无数据库工程保留本入口，新增其他存储前先明确 schema 和 repository 契约。

DO 可包含 gorm/bson 标签、表名、索引或存储元数据。DO 不直接返回给 HTTP 客户端；service 将其转换为 VO 或 DTO，密码哈希等敏感字段不得出现在 VO。

规则见 [模型分层](../../../docs/design/architecture/models.md)。
