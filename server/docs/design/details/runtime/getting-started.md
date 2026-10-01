# 快速开始

需要 Go 1.27.1。默认启动不需要数据库、Redis、Python 或 Docker。

```sh
go mod tidy
make run
```

开发配置 `config/config.yml` 监听 `127.0.0.1:8080`，只在 debug 模式允许无鉴权。使用 `go run ./app/main.go http -c /绝对路径/config.yml` 指定配置。

生成时选择只决定代码是否包含，运行时 `enabled` 决定是否连接。组件开启前准备隔离服务并填写配置，参见 [组件集成](integrations.md)。

使用容器前先执行 `go mod tidy` 生成 go.sum，设置自己的 `APP_KEY_AK_ACCESSKEY` 和 `APP_KEY_AK_SECRETKEY`，然后 `docker compose up --build app`。Compose 端口只绑定本机；容器读取 config_docker.yml，默认要求 AK 鉴权。凭据通过环境或挂载配置提供，不写入仓库。

`--examples` 生成的工程包含独立 User CRUD 参考；运行前启用相应数据库，MySQL 表结构由使用方按自己的业务模型准备。默认配置仍不连接外部服务。示例不包含默认账号或种子密码。


基底不提供 examples 建表 SQL；SQL 存放和按版本交付遵循 [统一规范](../../../sqls/README.md)。
