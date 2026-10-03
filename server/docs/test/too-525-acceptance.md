# TOO-525 首页加载优化验收

需求与理念：[TOO-525](https://linear.app/sisyphus-sq/issue/TOO-525)。范围为中心 Web，Mac App/Helper、Go 业务/API 与数据库结构不变。生产部署通过重建内嵌 Web 的 Server 完成，不创建正式版本或替换 Mac 包。

## 开发验证（2026-10-04）

- Overview 与 OverviewActivity 共 11 个聚焦场景通过（Overview 最终 9 个、OverviewActivity 2 个，分批执行）；新增覆盖 summary 等待时 KPI/来源先显示、summary 失败隔离、视口触发机器查询、趋势复用 usage、未浏览机器不因顶部刷新请求、已加载机器可刷新、键盘主动加载及旧筛选请求取消；模型日桶 413 时保留 summary 权威范围总量。既有年度独立口径、精确数值、失败不为空及同条件缓存保持继续通过。
- TypeScript 类型检查通过；全 src AntD lint 为 0 问题。JSDOM 的 pseudo-element getComputedStyle 提示是既有模拟布局限制，不是浏览器验收证据。
- 浏览器使用现有 ReviewApp 合成 API 在独立临时 Vite 入口检查默认窗口与 390px 窄屏。默认窗口首屏机器区块保留占位，滚动后机器、趋势与构成正常加载；修复占位容器百分比高度引起的卡片拉长。窄屏保留 KPI 纵向排列及年度热力图局部横向滚动。未启动此前停用的 Storybook 服务。
- 私有测试输出保存在 `.artifacts/too-525/`，临时预览仅合成数据，不访问真实中心或 Codex Home。没有全仓长测或生产业务性能基准。

## 审查与交付边界

自查：各区块权限仍由同源 ApiClient/SessionProvider 管理，旧请求取消与会话失效清理沿用原机制，十进制与 NULL 保留；没有外部请求地址、危险 HTML、持久凭据、统计重算或 schema 变更。慢年度接口仍会影响年度卡片，加载优化不改变服务端耗时。

提交和部署收尾按约定不重复运行测试。部署读回包括生产 LaunchAgent、ready、二进制来源/摘要与内嵌静态资源，具体 PR 和生产结果在 Linear 回写；这些读回不替代长期或真实业务故障矩阵。
