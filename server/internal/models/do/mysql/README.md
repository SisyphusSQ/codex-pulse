# MySQL DO

目标数据库的业务表模型位于 `<domain>_do` 子包，一表一文件。每个文件保留表 DO、TableName() 与列映射；关联表同样独立，不建立汇集多表的 models.go。

当前 SQLite 开发验证使用同一模型和参数化 repository，mysql 目录不改变存储策略，也不代表 MySQL 已验收。
