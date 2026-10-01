# 会话缓存命中率验证

TOO-508；开发分支 `suqing/too-508-session-cache-hit-rate`。

## 口径与边界

Codex 会话列表及详情使用同一 SessionItem：已索引整段会话缓存输入 / 全部输入。缓存是 input 子集，不加入分母、不平均各轮比例、不用列表筛选范围或 Turn 分页重算。Go 使用精确整数四舍五入至 basis points（100 = 1%），Swift 仅格式化为一位小数。

| 场景 | 预期 |
| --- | --- |
| input=1000, cached=900 | 90.0%，独立于价格是否可用 |
| input>0, cached=0 / cached=input | 0.0% / 100.0% |
| input=0, cached=0 | -- / not_applicable |
| input/cached 缺失，rollup missing/ambiguous | --，保留 unknown reason 及页面部分数据提示 |
| cached>input | 比例 unavailable，其他合法 Token 仍可读 |
| input=32, cached=1 | Go 313 basis points（半值向上）；Swift 本地化到一位小数 |
| JS-safe 大计数 | 无乘法溢出，比例保持 0..10000 |
| Cursor/Grok / 旧 Helper 无字段 | --，不复制 Codex 桶定义 |

不新增 schema、parser、数据采集、联网请求、排序或中心上报；不持久化比例，不输出原始 JSONL、正文、路径或凭据。

## 聚焦验证入口

以下测试使用 synthetic facts；会写入构建缓存和临时测试目录，不读取真实 Home。

```bash
go test ./internal/query ./internal/query/usagecost ./internal/core
swift run --package-path app/macos codex-pulse-app-tests --cache-hit-rate-only
swift build --package-path app/macos --product codex-pulse-app
bash scripts/proto/generate.sh --check
bash scripts/proto/generate-swift.sh --check
git diff --check
```

## 真实 Home 原生验收

启动前显式设置真实 `${CODEX_HOME:-$HOME/.codex}` 与 `0700` 私有 `CODEX_PULSE_APP_RUNTIME`；使用 development App 的 `--runtime-directory` 入口。启动只读访问 Session/JSONL，并可能写 runtime SQLite、偏好、日志及标准 App Server housekeeping。读回 preferences canonical Home/device/inode、App/Helper 环境和 Helper 参数确认实际身份，不覆盖已有 Home 配置。

1. 打开 Codex「会话」，选择已有输入的稳定会话，核对列表与详情的缓存命中率一致。
2. 用 Helper 返回的 input/cached Token 独立复算比例；分页和筛选后同一会话仍显示整段累计口径。
3. 中英文、窄列表和详情说明可读，页面部分数据提示保留，未知无虚假零。
4. 原始本地证据放 `.artifacts/too-508/`（忽略）；此文档只记录脱敏结论。CI、发布与未执行检查不得报为已通过。

## 实测结果

2026-10-02（Asia/Shanghai）：

| 验证层级 | 结果与边界 |
| --- | --- |
| Go query / usagecost / core | Pass；受影响三个包全包测试通过，含零值、未知、范围校验、价格独立性、局部异常与分页契约 |
| 聚焦 race | Pass；缓存命中率与详情分页用例通过 |
| Go / Swift Proto | Pass；正式生成器检查无漂移 |
| Swift presentation | Pass；`--cache-hit-rate-only` 中英文、零/全命中、未知、错误单位和非法比例用例通过 |
| 开发 App 构建 | Pass；debug App、Helper、资源与框架完成组装，未签名、未发布 |
| 架构与差异 | Pass；项目架构检查及 `git diff --check` 通过 |
| 真实 Home 绑定 | Pass；私有 runtime mode 0700，preferences Home/path/device/inode 及 App/Helper 环境、Helper 参数读回一致 |
| 真实 Home 页面 | Pass；匿名普通样本列表/详情同为 96.5%，独立只读复算一致；零命中样本列表/详情同为 0.0%，详情 accessibility value 同值 |
| 视觉范围 | 常规窗口、窗口缩放与较窄详情列已检查；最小列表宽度未完成独立原生实测，中英文通过 presentation 测试而非两种语言的原生截图矩阵 |
| 开发阶段未执行 | 全仓长测、完整 live E2E 矩阵、CI、正式签名、公证、提交、推送与发布 |

对抗式审查修复了详情无障碍树只暴露标签的问题，新增字段现在显式提供百分比 value。安全自查：新增 surface 只有派生比例与固定未知原因，沿用 UDS/pipe 鉴权；未新增正文、路径、凭据、网络请求或原始 JSONL 输出。开发 App 保持会话详情可见，本机原始构建证据留在 ignored `.artifacts/too-508/`。

## 功能验收与发布范围确认

用户于 2026-10-02 确认本卡以缓存命中率功能测试和真实 Home 会话列表/详情显示一致为验收范围，现有证据已满足要求；最小列表宽度、完整项目 E2E 与额外原生语言矩阵不作为本次发布阻塞项。上述未执行项保留为事实，不补写通过结论。

本次按约定不在提交、PR 合并或 v0.14.5 发布收尾重复执行测试。发行构建在 SQMC05 使用 `--skip-app-tests`；最终 Bundle、签名、DMG/ZIP、Sparkle、SHA-256、tag、Release 与 appcast 的非测试读回仍须完成。正式发布结果以对应 tag、Release 和本机发行证据为准。
