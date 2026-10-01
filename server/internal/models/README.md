# 中心模型

遵循 [分层](../../docs/design/architecture/models.md) 和 [业务子包](../../docs/design/architecture/packages.md)。

DO 按 mysql/access_do、mysql/reporting_do、mysql/schema_do 分域且一表一文件；DTO/VO 按业务域分包。statistics 无自有表，不能为目录对称创建空 DO。VO 根包只保留 HTTP 公共响应、校验和通用变更结果，不汇总业务类型别名。
