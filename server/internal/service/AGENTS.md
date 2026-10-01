# Service 子包约束

编辑前阅读 docs/design/architecture/packages.md、docs/design/architecture/models.md 和 docs/design/details/development/code-style.md（路径从仓库根目录计算）。

- 根包仅保留 module.go 的 Fx 装配；业务实现必须放 `<domain>_srv/`，单文件业务也分包。
- DTO/DO/VO 放对应 models 业务子包；service 只保留实现、依赖容器和规范允许的局部算法状态。
- 子包不得反向导入负责注册自身的 service 根包；事务和资源授权沿用现有业务责任层。
