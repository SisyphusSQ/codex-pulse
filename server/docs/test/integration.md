# 隔离数据库集成与正式验收

`make integration` 只有 `APP_TEST_INTEGRATION=1` 才读取 `APP_TEST_CONFIG=/绝对路径/isolated.yml`。目标必须是专用隔离数据库，中心启动时自动准备结构；需要独立组件验证时可用同一配置的 `db init`/`db check` 运维入口。Web 已内嵌，直接 Go 命令前先生成构建资源。该入口检查 /ready、真实连接、创建唯一探针表、事务回滚、原值更新匹配行与关闭/清理；不连接 Redis/Mongo、不生成 JWT。

当前 SQLite 开发证据与 MySQL/三机正式验收区分，整体步骤见 [运行说明](operations.md)。可选 Compose 只有明确开启的 mysql profile，使用独立 project name 和私有密码；不自动启动、启用 CI 或清理已有环境。只有本次明确创建的可丢弃测试库/卷才能在验收后清理，不能用 `down -v` 清理已有业务数据。

DEV 已通过 SeekDB 1.2 的 MySQL 协议自动初始化、Tailscale 三机 HTTP 上传/去重和本机真实 Home 原生上传，见[联调记录](../../../docs/test/multi-machine-reporting.md)。这与 `make integration` 入口、三台原生 App 全矩阵、独立 MySQL 8.4、备份恢复和生产部署验收分别记录；这些仍未运行。确定性测试使用合成资料，原始证据只留 ignored artifacts。构建或配置存在不代表 CI、部署或正式验收通过。
