# 额度页后台刷新与账号确认节奏

## 验收目标

- 本地索引通知只更新额度页的用量；主概览的后台发布不再连带重载当前功能页。
- 在线额度提交只更新对应 Provider 的额度；账号资料夹读按当前 Codex 额度来源的成功周期去重。
- 首载和手动刷新仍有加载反馈；后台更新保留已有数据，账号 scope/generation 不匹配时隐藏旧资料。

## 2026-09-17 验证结果

| 验证 | 结果 |
| --- | --- |
| Go `internal/core`、`internal/providerrefresh`、`internal/app` 包测试 | PASS |
| Swift Core Client 确定性测试（含 cancel probe） | PASS |
| Swift App 确定性测试与原生 App 构建 | PASS |
| 真实 Codex Home、已有 0700 私有 runtime 的 development App smoke | PASS：10 个 UI 页面、Codex 账号卡片可用、Helper 正常关闭且 socket 清理 |

Swift 测试覆盖：索引通知不重复读取额度页和账号、后台用量加载保留上一次值、Codex 与 Cursor 额度通知按 Provider 隔离、非额度时间范围的 Overview 只重读 quota/pace，以及账号切换时不发布旧账号资料。Go 测试覆盖：同一额度成功周期复用已确认账号，下次额度成功后重新夹读并更新套餐，以及 Cursor 额度通知的 domain 映射。

真实 Home smoke 验证了当前代码的启动、数据读取、页面和退出链路；持续运行期间加载指示是否长期稳定仍需日常使用观察。系统睡眠/唤醒、真实双账号切换未在本次 smoke 中执行。
