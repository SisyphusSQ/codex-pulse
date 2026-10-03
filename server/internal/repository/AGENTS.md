# Repository 子包约束

编辑前阅读 docs/design/architecture/packages.md、docs/design/architecture/models.md 和 docs/sqls/README.md（路径从仓库根目录计算）。

- 根包仅保留 module.go 的 Fx 装配；业务数据访问必须放 `<store>/<domain>_repo/`，单文件业务也分包。
- DO 放 `models/do/<store>/<domain>_do/`；MySQL DO 一表一文件，不将表模型定义在 repository 中。
- SQL 使用 engine.DB(ctx)；保留参数化查询和事务 context。子包不得反向导入 repository 装配根包。
