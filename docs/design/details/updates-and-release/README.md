# Updates and Release Boundary

## 当前状态

当前仓库只交付 Go Helper，不交付可独立运行的 macOS `.app`、更新器或发布包。旧桌面运行时、平台更新 adapter、bundle/package 脚本和本地更新流水线已经删除；历史实现不能继续作为当前设计真相。

后续 Swift native client 负责：

- 窗口、菜单栏、应用激活与退出交互；
- Helper 进程创建、一次性 token pipe、UDS 目录和崩溃重启策略；
- 更新检查、下载、平台签名验证、安装、重启和用户提示；
- `.app` bundle、Helper 嵌入位置、Developer ID、notarization 和正式分发。

Go Helper 只负责：

- Core RPC、业务运行时、SQLite ownership 和 migration recovery；
- 接收有限 lifecycle 枚举；
- 在 `Shutdown` RPC、signal 或父 pipe EOF 后停止 RPC admission 并 drain 已接纳业务；
- 返回 content-free typed error，不接收更新包、签名材料或平台安装命令。

Updater、Window、Tray 和 Popover 不属于 `codexpulse.core.v1.CoreService`。未来客户端不得通过扩张 Core RPC 把平台职责重新塞回 Go。

## 安全退出与更新衔接

客户端准备退出或安装更新时，应先调用：

```text
Shutdown(reason = client_exit | client_restart)
    -> Helper 停止接收新的 RPC
    -> 等待已接纳 RPC 返回
    -> scheduler admission fence
    -> invalidation / retention / health / lifecycle / metrics drain
    -> SQLite close
    -> UDS cleanup
    -> Helper process exit
```

客户端只有在 Helper 已确认退出后才能替换应用或 Helper 二进制。若超过客户端定义的等待上限，客户端必须把结果视为不确定并向用户报告；不能在 SQLite 仍可能提交时把更新伪装成成功。父 pipe EOF 是异常托管收口路径，不替代正常 `Shutdown` handshake。

当前 Helper 不提供热更新、运行中二进制替换或 schema downgrade。migration failure 会进入 recovery-only RPC；成功恢复后返回 `restart_required`，由客户端显式重启 Helper，不在当前进程热装配 normal graph。

## Migration 与回滚边界

- `MigrateApplicationSchema` 只在 Store 暴露给 runtime reader/writer 前执行。
- 所有 pending migration 在 single-writer transaction 中完成并读回校验，成功后才推进版本。
- migration failure 不启动普通业务图，只暴露 Bootstrap、recovery 和退出能力。
- 恢复流程只暴露稳定 stage/code/version 与安全备份摘要；底层 SQL、数据库正文、绝对路径和凭据不跨 RPC。
- 二进制回滚不等于 schema 回滚。后续发布矩阵必须显式验证 N-1、migration failure、恢复、重启和数据兼容性。

## 后续发布门禁

Swift 客户端实现前，以下项目均保持 `planned`，不得引用旧本地脚本或历史测试结果冒充当前通过：

1. gRPC Swift client 生成与 contract drift。
2. Helper 在 `.app` 内的嵌入、权限、架构和签名读回。
3. UDS/token pipe 的真实父子进程 E2E、Helper 崩溃恢复和版本握手。
4. Developer ID、hardened runtime、notarization 与发布产物校验。
5. 更新检查、下载、签名验证、safe shutdown、替换和重启矩阵。
6. migration recovery 与 N-1 兼容矩阵。
7. 正式密钥、上传、tag、release 和外部分发授权。

正式发布必须继续遵守：密钥不进入 argv、环境、日志、manifest 或仓库；缺少签名/notarization/读回证据时 fail closed；本地 ad-hoc build 不能冒充正式发布。

## 当前验证入口

Go Helper 当前只验证：

```bash
make verify
```

该入口覆盖 harness、project negative rules、Proto drift、全仓 race、vet 和 Helper build。Swift、`.app`、签名、公证、更新安装与 live E2E 明确不在当前结果内。
