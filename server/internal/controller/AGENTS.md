# Controller 子包约束

编辑前阅读 docs/design/architecture/packages.md 和 docs/design/details/development/add-module.md（路径从仓库根目录计算）。

- 根包仅保留 module.go 的 Fx 装配；业务 handler 必须放 `<domain>_controller/`，单文件业务也分包。
- controller 只处理 HTTP 解析、校验、可信身份、service 调用和统一响应；业务 VO 使用 `models/vo/<domain>_vo/`。
- 子包不得反向导入负责注册自身的 controller 根包，不直接访问数据库。
