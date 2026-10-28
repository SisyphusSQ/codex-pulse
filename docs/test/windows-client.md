# Windows 客户端开发与验收

开发机器为 Windows 11 24H2 build 26100 x64。开发与真实产品验收分别记录；后者必须显式绑定真实 Codex Home 和私有 runtime，不能由构建或 synthetic 数据替代。

## 工程基线

```powershell
./scripts/windows/build.ps1 -LockedRestore
./scripts/windows/generate-proto.ps1 -Check
dotnet run --project app/windows/CodexPulse.Tests
```

输出为各项目忽略的 `bin/obj`，包依赖使用官方 NuGet 源和版本锁。Protocol 从同一 core.proto 生成，重建后可比较 `obj/Debug/net10.0/api/codexpulse/core/v1/Core.cs` 和 `CoreGrpc.cs` 的 SHA-256 检查可重放性。macOS Swift 构建仍只在 macOS 验证。

此阶段尚未交付 Helper 平台移植、业务页面、安装包或真实 Codex 验收，不将空白窗口结果计入 Master 业务通过项。

2026-10-29 开发证据：Windows 11 build 26100 x64，`build.ps1 -LockedRestore` 成功（0 warnings / 0 errors）；`generate-proto.ps1 -Check` 的两个生成文件 SHA-256 一致；C# executable tests 5 项通过（optional absent、真实零、int64 最大值、invalidation stream、凭据 unary）。依赖还原未报告 NuGet 安全警告。未执行原生窗口运行、Codex 真实 Home、ARM64/Windows 10、macOS Swift 回归或 CI；Go/Swift 生成文件与 core.proto 未修改。提交/合并收尾复用上述证据，不重复测试。

## Windows 私有存储

2026-10-29，在 synthetic 目录/数据库执行并通过：

- `go test ./internal/platformfs ./internal/store/sqlite ./internal/diagnostics ./internal/apisubscriptions -run 'TestWindows|TestStore(WriteIsFIFO|SupportsConcurrentWAL|ViewEnforcesReadOnly|CloseDrains)|TestRefreshDiagnostics(ArePrivate|WriteFailure|RetainFault)|TestSQLiteCredentialStore' -count=1`：ACL、Everyone ACL 拒绝、hard link/junction 拒绝、LockFileEx 竞争、no-clobber、ReplaceFile/占用失败/中断恢复、WAL 备份恢复、owner lease 与取消、FIFO/事务回滚/只读/并发/关闭、有限诊断和凭据重启/代次/删除。
- `TestWindowsPinnedDirectoryCannotBeReplacedUntilReleased`：运行期目录不能移动，释放后可以移动。测试曾暴露 metadata-only handle 不能固定路径，已增加 FILE_LIST_DIRECTORY 并修复；最终通过。
- Preferences 存储独立文件集测试：`go test internal/preferences/file_store.go internal/preferences/types.go internal/preferences/clone.go internal/preferences/onboarding.go internal/preferences/validation.go internal/preferences/migration.go internal/preferences/json_shape.go internal/preferences/file_store_windows_test.go -run TestWindows -count=1`，覆盖初始化、重开、CAS 冲突、权威回读及可取消跨实例 Home lease。整包暂依赖后续 Codex Windows 来源移植，不把独立测试当整包已通过。
- `go test internal/store/migration_backup.go internal/store/migration_backup_windows_test.go -run TestWindows -count=1`：真实 Windows 可用空间查询下的不足分类和取消。
- `GOOS=darwin GOARCH=arm64 go test -c`：SQLite、Preferences 与 App runtime 测试二进制交叉编译成功；只证明编译，不是 macOS 运行回归。产物留忽略的 `.artifacts/windows/too-659/`。

尚未执行 Windows 完整 Helper/迁移 recovery UI、reporting 队列整包、真实 Codex Home/恢复和 macOS 运行测试，分别继续由 Home、通信、Provider、恢复及同步工作包/最终 Master 验收接通。无 CI 结果；提交/合并收尾不重复测试。安全自查：新权限与替换链路拒绝不安全对象，凭据和诊断边界保留，未新增网络端点或正文采集。
