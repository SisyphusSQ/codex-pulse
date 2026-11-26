# AGY 账号胶囊回归验收

目标：后台/显式刷新时保持已确认账号展示连续，退出/切号不沿用旧身份；套餐按 LS tier ID 区分 Ultra 5× 与 20×，未知倍数不猜测。

## 聚焦入口

- Go：`go test ./internal/agyprovider/... ./internal/providerrefresh/...`。
- Swift：`swift run --package-path app/macos codex-pulse-app-tests --agy-account-only`，使用 synthetic Codex/AGY Home。覆盖同账号刷新、切号、退出及状态 provider 缓存/迟到结果。
- 真实 Home 原生验收：构建 development App；显式 `CODEX_HOME` 与 mode 0700 私有 `--runtime-directory`；以 `--agy-account-smoke` 启动。可通过绝对路径 `CODEX_PULSE_AGY_ACCEPTANCE_DIR` 保存私有原生视图快照。该入口只验收 AGY 弹窗，等待账号为 Ultra 5×，执行两轮显式刷新及后台更新观察，采样账号是否消失/变动、弹窗 frame 是否变化；结束后输出计数并停止 Helper。局部验收暂不安装关闭弹窗的外部事件监听，避免查看证据终止观察；不覆盖弹窗关闭行为。

本入口会读取真实 Session/JSONL，并可能更新私有 runtime、SQLite、偏好和 App Server housekeeping；必须先说明副作用。截图可能含邮箱，仅存本机忽略的 `.artifacts/`，不得提交或上传至公开 PR。

## 结果边界

账号倍数采用 `g1-ultra-lite-tier → 5×`、`g1-ultra-tier → 20×`；通用 Ultra 名称没有足够的倍数证据。真实账号不在验收期间主动切换；切号/退出通过聚焦 contract 回归覆盖。窗口截图与采样日志共同判断，不能以单张截图替代连续刷新证据。

提交/发版阶段复用已有证据，不重复测试。所有 source、产物、Release 与三机安装结果另行记录。
