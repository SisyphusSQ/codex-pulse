# Web 协作边界

- React/TypeScript/Vite/AntD/ECharts，锁定 package-lock，npm ci 可复现；不改变 Go 分层。
- API、DTO 类型按服务端 v1 contract；Token/金额十进制字符串和 NULL 保留，前端只格式化，不重算用量、去重、成本、周期或预测。
- 管理会话使用 HttpOnly Cookie；CSRF 只在内存，不把设备码、凭证或业务缓存放 localStorage/sessionStorage/URL/日志/静态环境变量。
- API 请求同源，状态变更 CSRF，过期/撤销清除缓存并取消旧请求；不要把错误/未取得数据当成空或0。
- 先查询已安装 AntD CLI 对应版本的 info/demo，再写组件；改后运行聚焦类型/行为检查和 antd lint。提交推送收尾不重复测试。
- 中文、可操作错误、键盘与响应式布局；标题/名称使用 React 默认转义，无 dangerouslySetInnerHTML。
