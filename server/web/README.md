# Codex Pulse Web

React 19 / TypeScript 7 / Vite 8 / AntD 6 / ECharts 6。依赖固定在 package.json 与 package-lock.json，Node 要求 >=22.22.2；标准安装使用 npm ci。React Router HashRouter 的深链接只请求静态壳，数据走同源 v1 API。

```sh
cd server/web
npm ci --ignore-scripts
npm run dev
```

另一个终端按 [中心说明](../README.md) 初始化隔离数据库并启动 Server。Vite 默认把 `/api` 转发到 `http://127.0.0.1:8080`，保留 Host/Origin；开发配置精确允许 `http://localhost:5173` 和 `http://127.0.0.1:5173`，不设通配 CORS。改变开发地址时同步 Server origins。仅供 Vite 的 `PULSE_WEB_PROXY_TARGET` 可指定已授权中心；它不进入静态包，不包含凭据。

首次管理员在受信任服务端终端运行 `codex-pulse-server db bootstrap`，在 Web 输入短期浏览器配对码。采集设备码不能登录管理页面。凭证只在 HttpOnly Cookie，CSRF 只在内存；刷新后重新读取会话。退出只有服务端撤销成功才返回授权页，网络失败保持可操作错误；撤销/过期取消旧请求、清除内存缓存。

不把 code/credential/CSRF 放 localStorage、sessionStorage、URL、控制台、环境变量或构建产物。Token/微美元字符串与 NULL 原样保留；前端只格式化与展示，业务聚合、仲裁和预测来自 Server。

```sh
npm run typecheck
npm test
npm run lint:antd
npm run build
```

按开发风险选择验证，提交推送收尾不重复测试。dist/node_modules/.vite 不提交，静态托管与运行产物由运行交付卡完成。

2026-10-01：8 个授权/client UI 测试、类型、构建、AntD lint 通过；所有锁定依赖来自 registry.npmjs.org，审计未报告漏洞。环回 HTTP/隔离 SQLite/合成一次性码的真实浏览器配对、刷新恢复、退出撤销和 390px 窄屏通过；没有读取 Codex Home 或个人/Agent 凭据。三机真实 Home、MySQL、完整看板未验收，业务页面由其余 Execution 卡继续接入。
