# 模型层约束

编辑本目录前阅读 docs/design/architecture/models.md 与 docs/design/details/development/code-style.md（路径从仓库根目录计算）。

- DO 只描述持久化；DTO 描述跨服务/外部系统传输；VO 描述 HTTP 请求与响应。
- 编辑前同时阅读 docs/design/architecture/packages.md；业务 DO/DTO/VO 必须按业务域分子包，禁止业务模型平铺在 do/dto/vo 根包。
- MySQL DO 在 `mysql/<domain>_do/` 内一表一文件，DO、TableName() 与列映射同文件；禁止多表集中在 models.go/schema.go。
- models 不得导入 service、controller、repository 或运行时连接组件。
- HTTP 响应必须使用明确 VO，禁止直接返回 DO；保持 service -> models 依赖方向。
- 不因删除 examples 再删除 do/dto/vo 的目录说明；README 是无业务工程的扩展入口。
