# 中心 Web 查询通知验收（TOO-530）

## 目标与范围

统一一次性操作和查询失败为 AntD Notification，移除正式页面的黄／红提示条。首次失败保留中性重试入口；同一查询键刷新失败保留最后成功结果及原时间；字段校验与陈旧、未知、冲突等持久状态仍可就近查证。此次不改变 API、Go 业务、数据库结构、原生 App 或网络配置。

排查发现 24 处黄／红内联提示：21 处 warning Alert、1 处登录 error Alert、2 处筛选错误文本；其中 2 处账号分支在当前条件下不可达。另有 21 处共用 ErrorState 调用，已经改成中性紧凑状态。TPS 来源冲突改为短 Tag 和可点击的解释入口。

## 可复用入口

在仓库根目录运行：

```sh
npm --prefix server/web run typecheck
npm --prefix server/web run test -- src/components/QueryNotifications.test.tsx src/components/StatisticsCacheFooter.test.tsx src/pages/Overview.test.tsx src/pages/Quota.test.tsx src/pages/Records.test.tsx src/auth/SessionProvider.test.tsx src/components/Throughput.test.tsx src/pages/Usage.test.tsx src/pages/Pricing.test.tsx src/pages/Devices.test.tsx src/components/SubscriptionPanel.test.tsx
npm --prefix server/web run storybook -- --port 6010
```

故事入口：`/?path=/story/too-530-query-feedback--error`；正常为 `--current`，额度冲突为 `--quota`。故事复用正式 PulseApp，所有 API 都被合成适配器拦截，不连接真实中心，不读取 Codex Home。

## 验收场景

- 共用查询首次失败只有一条对应通知，正文不出现 Alert，不伪装成空数据；明确展示重新读取入口。
- 后台重复失败不重复弹通知，关闭后保持关闭；同一查询键的缓存值和原时间仍在。页脚可查看当前活跃失败项及逐项重试。
- 明确重试再次失败可重新通知；成功恢复清除故障通知及页脚失败项；HTTP 200 的 `cache.state=refresh_failed` 也能正确提示与恢复。
- 改变筛选不把旧范围结果用于新范围；失活查询关闭通知；取消与失效授权不产生查询通知。
- 配对失败保留输入，订阅及其他字段校验留在 Form；项目和会话列表、选中详情刷新失败仍保留数据。
- 桌面与 390×844 窄屏通知均位于右上，从右侧向左进入，持久数据状态仍可见；通知可关闭，失败详情能在关闭后查看并重试。

## 2026-10-05 开发证据

11 个受影响测试文件的 70 个用例已通过。首轮两个新记录页断言未计入窄屏隐藏但保持挂载的列表；修正测试定位后，该文件 7 个用例重新运行通过，其余 10 个文件原有结果通过。查询通知 6 个专项用例包含去重、关闭、恢复、后台快照故障、取消／401 和筛选切换。TypeScript 类型检查及 components/pages/auth 三目录 AntD CLI 检查通过（0 issues）。

浏览器使用合成数据核对正式概览的正常与失败状态，正文 `.ant-alert` 为 0；确认桌面与窄屏统一右上通知，以及中性重试状态。截图只保留在忽略的 `.artifacts/too-530/`。模拟页面与测试结果不代替生产业务 E2E、真实断网／恢复或三机采集验收；本次未执行全仓长测。

提交和部署收尾按约定不重复运行开发阶段测试；构建、部署版本、资源、进程与 ready 的回读单独记录在 Linear 交付评论。
