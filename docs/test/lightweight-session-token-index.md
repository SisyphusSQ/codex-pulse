# 轻量会话与 Token 索引验证

## 结论

- 日期：2026-07-19（Asia/Shanghai）
- 结果：`PASS`
- 数据边界：只读访问用户明确确认的 Codex Home；App Server state DB、rollout JSONL 均未修改
- 写入边界：只在 `0700` 临时目录创建隔离 SQLite，成功后删除；未读取或改写 Codex Pulse 现有生产数据库
- 外部副作用：未 commit、push、创建 Issue/PR/MR 或发布产物

正常启动已改为 metadata-first 两阶段路径：Codex App Server `thread/list` 先提供真实标题、cwd、时间和 rollout path；首屏开放后，utility 后台任务才扫描 Token。首次路径不再运行 full-history bootstrap，不生成 Turn、历史 quota receipt、parser diagnostic 或 source generation staged facts。

Token scanner 以 64KiB 分块读取，未包含精确字节串 `"token_count"` 的行不做 JSON decode。每个 rollout 保存 Home/file identity、size、mtime、parser version、prefix proof 和完整行 offset；无变化复用、同文件追加扫描、截断/替换/parser bump 重建均有自动化测试。Turn timeline 保留为打开单会话详情时的按需严格深索引；它复用原 parser 与 checkpoint/generation fence、跳过历史 quota facts，并通过 lifecycle drain fence 支持取消和退出恢复。

设计参考与协议依据：

- [SessionNest Token scanner](https://github.com/nemoob/sessionnest/blob/main/Sources/SessionNest/ThreadTokenUsage.swift)
- [SessionNest metadata-first list model](https://github.com/nemoob/sessionnest/blob/main/Sources/SessionNest/SessionListModel.swift)
- [Codex App Server thread/list protocol](https://github.com/openai/codex/blob/rust-v0.144.0/codex-rs/app-server-protocol/src/protocol/v2/thread.rs)
- [Codex state DB thread listing](https://github.com/openai/codex/blob/rust-v0.144.0/codex-rs/thread-store/src/local/list_threads.rs)

## 真实 Home 只读 E2E

执行入口：

```bash
go run ./scripts/lightindex-e2e \
  --home <confirmed-codex-home> \
  --confirm READ_ONLY_CONFIRMED
```

输出只包含整数统计和固定枚举，不包含 Home 路径、session ID、标题、cwd、Git 信息、prompt、response、tool output 或原始 JSON。

| 指标 | 结果 |
| --- | ---: |
| App Server metadata 首屏 | 324 ms |
| utility 后台 Token 全扫 | 8,981 ms |
| metadata + 全扫总时间 | 9,307 ms |
| 会话 / rollout | 885 / 885 |
| 完成扫描 rollout | 885 |
| App Server 返回 rollout 的逻辑大小 | 3,827,572,245 bytes |
| scanner 物理读取 | 4,073,540,511 bytes |
| `token_count` candidate / JSON decode | 154,826 / 154,826 |
| SQLite main / WAL | 31,485,952 / 4,350,752 bytes |
| 二次 metadata refresh | 232 ms |
| 二次后台 refresh | 269 ms |
| 未变化 rollout | 884；新增内容读取 0 bytes |
| 运行中发生 append 的 rollout | 1；只读取新增 7,048 bytes |
| 隔离数据库清理 | PASS |

物理读取大于逻辑文件大小，来自有界 prefix proof 和 offset 行边界校验，不代表全行 JSON decode。二次 refresh 恰逢一个活跃 rollout 追加，因此把 884 个真正无变化文件和 1 个变化文件分开统计，避免把活跃写入误报为 reuse 成本。

旧基线约为 6.7GB filesystem rollout corpus 和约 823MB SQLite；新 E2E 的 3.83GB 是 App Server state DB 当前返回的产品可见 rollout 集，两者范围不同，不能把字节数当成一一对应的全量吞吐对比。可直接比较的是新鲜轻量 SQLite main 文件约 31.5MB，相对约 823MB 旧库缩小约 96%，且 324ms metadata ready 已与 8.98s Token 全扫解耦。未为对比再次生成旧生产数据库。

## 隐私与安全语义

- `light_sessions` 只持久化产品需要的 metadata，不保存 App Server `preview`。
- Token 表只保存累计计数、按日/时间点正增量、offset 和不可逆文件身份证明。
- metadata path 和 Token reader 都经过 confirmed Home device/inode fence、根内路径和 symlink 拒绝检查。
- rebuild 使用 pending generation；成功后原子激活，失败/取消时旧 active generation 继续可查。
- durable offset 只推进到完整换行边界；EOF 半行在下次 append 继续处理。
- 正常首次路径不写 legacy Turn/quota/generation 表。按需深索引只处理被打开的一个会话；为保留提交重试语义，strict ingester 仅保留 active generation 的有界末批 receipt，不重建全历史 quota receipts。
- `make m11-privacy-audit`：`m11-privacy-v1` PASS；48 张 schema 表、21 个 generated 文件、94 个 package regular files 和 9 个 package symlink 通过统一隐私 contract。

## 自动化验证

| Gate | 结果 |
| --- | --- |
| `go test ./... -count=1` | PASS |
| `go test -race ./... -count=1` | PASS |
| `go vet ./...` | PASS（由 `make verify` 执行） |
| frontend typecheck | PASS |
| frontend Vitest | PASS，57 files / 186 tests |
| frontend production build | PASS；只有既有的 500kB chunk warning |
| `make harness-verify` | PASS |
| `make verify-project` | PASS |
| `make verify-generated` | PASS；bindings 与 Go module files stable |
| `make verify-package` | PASS；arm64、minOS 15.0、ad-hoc 签名、App/ZIP 解包复验 |
| `make m11-privacy-audit` | PASS；内部完整串行执行 `make verify` |
| `git diff --check` | PASS |

## 对抗式复核

1. **offset 漏扫或重扫**：覆盖跨 64KiB 行、EOF 半行、取消和 checkpoint 恢复；offset 只在完整行事务提交后推进。
2. **把活动文件误判为无变化**：E2E 二刷按 size/mtime 分离 unchanged 与 changed；884 个 unchanged 的内容读取为 0，活跃 append 只读新增 7,048 bytes。
3. **Home 切换或退出竞争**：Home switch 先取消旧 generation，metadata/token replacement 受旧/新 Home fence 约束；按需深索引也进入统一 drain admission。
4. **轻量值覆盖严格真相**：App Server metadata 始终是标题/cwd/path/时间真相；Token 轻量汇总只补 Token，Turn/model/cost 在深索引前明确为 partial/unknown。
5. **隐私或写放大回流**：schema/DTO/package canary 审计通过；正常首次路径没有 prompt/response/tool output/raw JSON，也不调用 legacy full-history scheduler。

## 残余边界

- App Server 缺失或拒绝某个 rollout path 时，会话 metadata 仍可展示，Token 状态标记为 deferred；不会回退到全量 rollout metadata 解析。
- 精确 model/cost 与 Turn timeline 仍需要用户打开单会话后执行严格深索引；这是显式能力边界，不把 unknown 伪装成 0。
- 旧约 823MB 数据库不会在 startup 自动 `VACUUM` 或删除。历史空间回收应作为独立、显式维护动作设计。
