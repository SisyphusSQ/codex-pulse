# 隔离数据库集成与正式验收

`make integration` 只有 `APP_TEST_INTEGRATION=1` 才读取 `APP_TEST_CONFIG=/绝对路径/isolated.yml`。目标必须是专用隔离数据库，先用同一配置执行 `db init` / `db check`，WebDirectory 留空或准备完整静态构建。该入口检查 /ready、真实连接、创建唯一探针表、事务回滚、原值更新匹配行与关闭/清理；不连接 Redis/Mongo、不生成 JWT。

当前 SQLite 开发证据与 MySQL/三机正式验收区分，整体步骤见 [运行说明](operations.md)。可选 Compose 只有明确开启的 mysql profile，使用独立 project name 和私有密码；不自动启动、启用 CI 或清理已有环境。只有本次明确创建的可丢弃测试库/卷才能在验收后清理，不能用 `down -v` 清理已有业务数据。

当前没有 MySQL 环境：真实 MySQL、容器与三机整体结果 Not Run；测试配置必须使用合成资料，原始证据只留 ignored artifacts。构建或配置存在不代表 CI、部署或正式验收通过。
