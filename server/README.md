# server

Go 1.27.1 + Echo v5 + Uber Fx 服务基底。默认仅监听本机，外部组件默认关闭；不包含示例业务路由。

```sh
go mod tidy
make build
make run
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/ready
```

`/health` 表示进程可响应；`/ready` 表示当前可以接流量。生产配置必须启用 Basic、AK 或已包含的 JWT 鉴权。默认 `none` 仅允许 debug 和环回地址。

- [文档索引](docs/README.md)
- [配置与组件开关](docs/design/details/runtime/configuration.md)
- [分层和 Fx 装配](docs/design/architecture/README.md)
- [开发及验证](docs/design/details/development/README.md)

本工程由 go-web-starter 生成，模板内容摘要、生成选项、Harness 来源在 `.starter.json`。它记录来源，不授权覆盖业务修改。修改业务项目后不运行生成器覆盖已有目录。
