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
# 用量总览

首页通过中心 summary API 读取范围 KPI、自然日趋势、Provider/模型构成及独立 365 日热力图。日期、时区、Provider 和采集来源由 Server 筛选。Token/微美元在表格、KPI 和 tooltip 中保留精确十进制字符串；浮点转换只用于图表坐标，成本坐标单位 USD。API 等价成本与账单区分，partial 显示已知小计。

当前范围及年度覆盖分别显示采集截至、陈旧状态、未定价/缺失/冲突。无日级事实的热力图格子保持未知灰色，有明确零事实才显示零；成功收批次不代表全量历史。来源与执行归属不同，执行设备缺少证据时显示未知。构成表使用服务端完整返回结果，图表最多展示前 12 项并明确提示。

图表按路由和 ECharts 模块延迟加载，SVG renderer 随容器尺寸变化并在卸载时释放。tooltip 使用文本 richText，标题/模型/工具名不进入 HTML。刷新失败保留同一筛选下的缓存并标记失败；切换筛选等待对应数据，不把旧范围冒充新范围。

## 项目和会话

两页共用自然日/时区、Provider、来源、字面搜索、精确模型、服务端排序与 10–100 条分页。范围 totals 在分页前由 Server 计算；详情使用相同筛选。会话保留标题、原始 Session ID 和全部采集来源，未关联用量明确没有原始会话身份；没有正文或路径。

项目详情的项目、模型、趋势与分页会话贡献来自一次 Server 只读快照。项目名相同保留独立身份；表格同时显示中心键前缀和成员数。跨页选中关系保存在页面内存，改变统计范围清空选择；关联前确认成员和目标，一次最多 100 个成员。关联/解除均由管理员 CSRF 写接口处理；成功后刷新相关查询并清空选择，失败保留明确错误。解除只恢复各成员独立项目身份，统计事实保留。
