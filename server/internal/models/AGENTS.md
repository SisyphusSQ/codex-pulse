# 模型层约束

编辑本目录前阅读 docs/design/architecture/models.md 与 docs/design/details/development/code-style.md（路径从仓库根目录计算）。

- DO 只描述持久化；DTO 描述跨服务/外部系统传输；VO 描述 HTTP 请求与响应。
- models 不得导入 service、controller、repository 或运行时连接组件。
- HTTP 响应必须使用明确 VO，禁止直接返回 DO；保持 service -> models 依赖方向。
- 不因删除 examples 再删除 do/dto/vo 的目录说明；README 是无业务工程的扩展入口。
