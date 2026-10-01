# 六平台构建

使用 GNU Make 和 POSIX shell；macOS/Linux 可直接执行，Windows 主机使用 Git Bash/MSYS2 配合 GNU Make，或在 WSL 交叉构建。Windows 原生 PowerShell 不直接解释本 Makefile 的 shell 片段。

| 系统 | GOOS | 架构 | 目标示例 | 输出 |
|---|---|---|---|---|
| Windows | windows | amd64 / arm64 | make build-windows-arm64 | bin/windows-arm64/codex-pulse-server.exe |
| macOS | darwin | amd64 / arm64 | make build-darwin-amd64 | bin/darwin-amd64/codex-pulse-server |
| Linux | linux | amd64 / arm64 | make build-linux-arm64 | bin/linux-arm64/codex-pulse-server |

- make build-all：构建全部六种组合；make -j2 build-all 可并行。
- make release-all：六种组合使用 -trimpath；release-<os>-<arch> 构建单个组合。
- make build / make release：保留本机构建入口；不覆盖其他平台目录。
- BUILD_DIR 和 codex-pulse-server_NAME 可覆盖；默认交叉构建 CGO_ENABLED=0，新增必须依赖 CGO 的库时需同时明确各平台 C 工具链并重新验证，不能静默删除目标。
- build/release 只构建，不执行测试、签名、打 tag、上传或发布；CI 的构建步骤使用相同入口。
- 交叉编译和可执行文件格式检查证明产物目标正确，不代表六个平台都完成运行验收。Windows .exe 后缀及目标目录由 Makefile 自动决定。
