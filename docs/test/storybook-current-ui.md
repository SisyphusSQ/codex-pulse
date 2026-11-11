# Storybook 全站当前样式验收

2026-11-11，按用户要求将整个 server Web Storybook 对齐当前实现。包含用量概览、模型分析、采集来源、账号、项目、会话、价目表、设备与授权及浏览器配对；不是只补 Credits 样例。

## 实现边界

- 全站页面、状态和历史功能故事复用 `ReviewApp → PulseApp`；公共组件和订阅直接导入正式 `components/`。
- 统一使用 AntD reset、`pulseTheme`、`styles.css`；只保留 Storybook 自身的合成数据标识及画布边距。不加载旧 `Studio`、`*Design` 或 `studio.css`，历史源文件保留但不参与活动故事。
- 合成 API 支持当前会话 GET/POST 恢复与续期，修复正式页面预览出现授权读取失败的问题。配对、退出、订阅及改名只修改故事内存。未知 API 返回 501，不透传真实中心。
- 空数据场景同时清空年度总量、热力图统计、余额和记录列表；错误使用实际失败响应。Credits 区分余额、Reset Credits 与未知实际消耗。
- 工具栏提供桌面 1440px、平板 768px 和窄屏 390px；概览、账号、项目和会话另有固定窄屏故事。

## 已执行

| 验证 | 结果 |
| --- | --- |
| TypeScript | `npm run typecheck` 通过 |
| 聚焦回归 | `reviewApi.test.ts`、`ReviewApp.test.tsx` 共 13 项通过；覆盖八页、配对、会话续期、未知 API 隔离、空数据和账号筛选 |
| 静态构建 | `npm run build-storybook -- --output-dir ../../.artifacts/storybook/build` 成功；构建保留大 chunk 提示 |
| AntD lint | 0 issues、0 skipped |
| 官方应用内浏览器 | 八个主要页面桌面及 390px 逐页读取实际数据并检查截图；没有授权读取错误，均为正式页面外壳 |
| 窄屏 | 八页 document clientWidth 和 scrollWidth 均为 390，无页面级水平溢出；表格、日历局部滚动沿用产品实现 |
| 交互 | 项目与会话进入实际详情面板，无查询错误；错误故事显示实际重试入口，配对故事显示正式表单 |
| 旧样式检查 | 活动故事没有旧设计组件导入；构建 JS 未包含 `ds-studio`、`ds-purchase`、`DesignToolbar`、`SubscriptionPlansDesign` |

测试中的 jsdom 输出不支持 pseudo-element getComputedStyle / scrollTo 提示，测试通过；真实视觉结论来自官方浏览器。当前索引共 70 条故事，不逐一声称全部交互都已人工验收。原始截图、浏览器逐页摘要、构建和测试日志保存在被忽略的 `.artifacts/storybook/`。

## 本机预览

从仓库根运行 `npm --prefix server/web run storybook`，仅监听 `127.0.0.1:6007`。

- [全站当前概览](http://127.0.0.1:6007/?path=/story/全站设计--overview)
- [账号与 Credits](http://127.0.0.1:6007/?path=/story/全站设计--accounts)
- [订阅组件](http://127.0.0.1:6007/?path=/story/subscription-plans--codex)
- [错误与重试](http://127.0.0.1:6007/?path=/story/关键状态--error)

本轮启动的预览服务留运行，方便用户查看。所有账号、会话和数值均为合成样本；未连接真实服务或部署生产，未提交/推送。这轮不重新运行全仓 Go、原生 App 或真实上报 E2E。
