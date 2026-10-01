# 中心配置

Viper 严格解码，未知字段拒绝启动。环境变量以 `APP_` 开头，将点换成下划线，例如 `APP_DATABASE_PATH`、`APP_SERVER_ADDRESS`；duration 使用 `5s`、`2h`。

| 配置 | 语义 |
| --- | --- |
| server.address | 明确的 host:port。HTTP 模式必须绑定具体环回、LAN 或 Tailnet IP，拒绝无范围的公网监听 |
| server.origins | 精确中心入口 Origin，不带路径/查询/通配。可同时配置 HTTPS 与显式私网 HTTP |
| server.allowHTTP | 显式私网 HTTP 开关；不会因 HTTPS 失败降级，也不豁免鉴权 |
| server.trustedProxies | 仅可信 TLS 终止代理的精确 CIDR，默认空。普通 forwarded header 不影响入口身份 |
| server.corsOrigins | 精确跨域 Origin 白名单。官方 Web 使用同域 API；开发代理保留原始 Host（changeOrigin:false） |
| server.webDirectory | 空为 API-only；完整 Web 构建目录启用同域壳/hashed assets，无业务秘密；缺失拒绝启动 |
| server.maxBodyBytes | 当前上报请求上限 8 MiB；配对/管理请求另限 4 KiB，超预算明确拒绝 |
| database.enabled | 中心 HTTP 与 db CLI 要求数据库开启 |
| database.driver / path | sqlite 开发 dialect；父目录 0700、文件 0600，独立于本机库 |
| database.host / username / password / database | mysql 配置；秘密通过受保护环境或配置提供，仓库样例为空 |
| database.tls | mysql 明确 true 或 false；true 校验证书，false 只用于已确认私有数据库链路，不自动降级 |
| contextTimeout / server timeouts | 启动、数据库 I/O 与 HTTP 的有界等待 |
| log.output / rotation limits | 有限结构化日志，不写 payload、凭据、账号资料或完整 DSN |

没有 key.type/basic/AK/JWT 登录配置。统一设备码有效 10 分钟、仅消费一次，浏览器会话有效 14 天；设备凭证在撤销前持续有效。服务端只保存摘要，HTTP/HTTPS 权限相同。HTTPS 采用 __Host-pulse_session Cookie，私网 HTTP 采用 pulse_session；会话绑定入口，不跨协议自动共享。

首次运行顺序为 db init → 受控 db bootstrap → HTTP → 浏览器输入码 → 管理端签发设备码。首次码只在受信任终端显示；不要把终端截图、配对码或秘密放入提交证据。当前 config_docker.yml 是需要填入实际代理/数据库环境的部署样例，未进行生产部署或 MySQL 整体联调。
