# 配置

使用独立 Viper 实例和严格解码，未知字段及错误类型导致启动失败。duration 使用 `5s`、`2h` 等字符串。环境变量以 APP_ 开头，点替换为下划线，如 `APP_SERVER_ADDRESS`、`APP_DATABASE_ENABLED`。

| 配置 | 含义 |
|---|---|
| debug | 是否开发模式，不改变生产鉴权要求 |
| contextTimeout | 启动和业务调用的默认截止时间 |
| server.address | host:port；无鉴权只允许环回 IP |
| server.readinessTimeout | 就绪检查总超时 |
| server.metrics | 是否注册 `/metrics`，与查询客户端独立 |
| server.corsOrigins | 精确 Origin 列表，不接受通配符 |
| log.output | stdout、file 或 both；默认 JSON 标准输出 |
| log.fileName / rotation limits | 文件日志及轮转，stdout 时不创建文件 |
| key.type | none / basic / key，以及生成时选入的 jwt |
| database/mongodb/redis/lark/prometheus.enabled | 启用相应组件；未生成的组件配置会被严格解码拒绝 |
| cron.on | 开启调度器；默认 false |

本次生成包含：MySQL=true，MongoDB=false，Redis=false，cron=false，Lark=false，Prometheus query=false，JWT=false，examples=false。

已包含但未启用的组件不要求连接信息、不建立连接、不参与就绪检查。已启用且必需的数据库或 Redis 连接失败时启动失败。

生产使用 config_docker.yml 或单独维护的配置；默认 AK 凭据为空，必须由部署方提供。禁止在仓库中保存真实密钥。
