# 隔离集成验证

普通 make test 不连接数据库、Redis 或外部 SDK。集成测试只在 APP_TEST_INTEGRATION=1 时读取指定配置，目标必须是本次创建的隔离实例。

1. 使用独立 Compose project name 启动所选组件，例如 `docker compose -p starter-check --profile mysql --profile redis up -d mysql redis`。
2. 用不提交的配置文件指向这些容器；只开启实际需要的组件，设置自己的临时凭据。
3. 设置 APP_TEST_INTEGRATION=1 和 APP_TEST_CONFIG=/绝对路径/test.yml，执行 make integration。
4. MySQL 创建独立测试表并验证事务回滚、原值更新和清理；MongoDB/Redis 覆盖连接、真实读写及资源关闭；生成 JWT 时同时验证 Redis 令牌生效与撤销。
5. 结束后运行 `docker compose -p starter-check down -v`，仅清理本次 project 的容器与卷。

未启动容器、未运行 integration 或未提供环境时必须记为 Not Run，不能用普通单测代替。飞书发送及业务环境访问不属于默认集成测试。

生成工程的 CI 使用临时随机凭据，执行非 root 镜像启动、探针与鉴权检查，以及已包含数据库/Redis 的隔离联调，最终清理本次 Compose project。CI 配置存在不代表已执行成功，应以实际运行记录为准。
