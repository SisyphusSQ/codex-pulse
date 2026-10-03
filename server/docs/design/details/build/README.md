# 内嵌 Web 与六平台构建

先在 `server/` 执行 `make web-install`（Node >=22.22.2、npm）。所有构建目标先执行 `web-build`，再将 `web/dist` 的页面和 assets 通过 Go `embed` 编入 Server；各平台共享本次构建的同一份 Web。运行机器只需要二进制、配置与数据库，不需要 Node 或 Web 目录。

使用 GNU Make 和 POSIX shell；macOS/Linux 可直接执行，Windows 主机使用 Git Bash/MSYS2 配合 GNU Make，或在 WSL 交叉构建。Windows 原生 PowerShell 不直接解释本 Makefile 的 shell 片段。

| 系统 | GOOS | 架构 | 目标示例 | 输出 |
|---|---|---|---|---|
| Windows | windows | amd64 / arm64 | make build-windows-arm64 | bin/windows-arm64/codex-pulse-server.exe |
| macOS | darwin | amd64 / arm64 | make build-darwin-amd64 | bin/darwin-amd64/codex-pulse-server |
| Linux | linux | amd64 / arm64 | make build-linux-arm64 | bin/linux-arm64/codex-pulse-server |

- make build-all：构建全部六种组合；make -j2 build-all 可并行。
- make release-all：六种组合使用 -trimpath；release-<os>-<arch> 构建单个组合。
- make build / make release：保留本机构建入口；不覆盖其他平台目录。
- BUILD_DIR 和 BINARY_NAME 可覆盖；默认交叉构建 CGO_ENABLED=0，新增必须依赖 CGO 的库时需同时明确各平台 C 工具链并重新验证，不能静默删除目标。
- build/release 执行 Web 类型检查和资源/Go 构建，不执行测试、签名、打 tag、上传或发布；CI 的构建步骤使用相同入口。
- 交叉编译和可执行文件格式检查证明产物目标正确，不代表六个平台都完成运行验收。Windows .exe 后缀及目标目录由 Makefile 自动决定。

直接调用 Go 工具（包括聚焦 `go test`）前先执行 `make web-build`，否则干净 checkout 缺少 embed 文件会编译失败。Make 的 Go 测试/检查入口已经声明该前置步骤。Dockerfile 自动完成 Node/Web/Go 构建，最终镜像仅运行 Server。`web/dist` 是忽略的构建产物，不提交。

2026-10-03 验证边界：macOS/Linux × amd64/arm64 的内嵌 Web 二进制构建及格式读回通过；Windows 两目标因既有配额算法包间接引入 `internal/diagnostics`、`internal/store/sqlite` 的 Unix 代码而编译失败。保留六平台目标，没有把失败视为通过；Windows 需先分离纯计算依赖再验收。本轮未扩大到本机平台改造。Dockerfile 已适配单体构建，当前机器无 Docker，尚未实构容器。
