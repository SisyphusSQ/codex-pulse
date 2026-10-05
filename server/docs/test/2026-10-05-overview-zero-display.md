# TOO-528 用量概览的零值展示

## 范围与语义

成功响应中，空记录的范围、日期、小时和机器指标显示 0；没有模型日桶时概览趋势显示零值。已有会话或调用但数值缺失显示 `—`，未定价和部分定价只提供简短的悬浮/焦点提示。API 的 NULL、后台事实、历史价格和去重保持原样，加载与失败不转换成零。额度、TPS 和模型分析页的默认语义不变。

首页删除常驻覆盖未确认、已观测、未知日期留空、正值环图口径及重复采集说明；保留更新时间、实际采集陈旧、刷新失败与“各机器用量可能重复”。完整采集口径仍在采集来源页。

## 开发证据（2026-10-05）

- Pass：`npm test -- src/pages/Overview.test.tsx src/pages/Usage.test.tsx src/components/ActivityHeatmap.test.tsx src/components/OverviewActivity.test.tsx`，4 文件、26 用例。
- Pass：`npm run typecheck`。
- Pass：`antd lint src --format json`，0 issues、0 skipped。
- 使用合成 API 响应，不读取真实 Home、不上传个人数据。JSDOM 对伪元素的 getComputedStyle 输出环境提示，未导致失败；这些用例不构成真实 Chrome 视觉验收。
- 覆盖：空年度/范围成本 `$0.00`、空机器四列零值、有记录缺字段/未定价 `—`、热力图大整数分位/键盘交互、模型缺日桶零值与未定价区别、请求失败/缓存保留、懒加载与旧筛选取消。
- 自审：零值仅为展示，不写回事实；排序与显示一致；费用缺失保留焦点提示；未扩大接口权限、监听或采集范围，无 schema 变更。

## 部署入口与边界

中文 PR 合并后，在 SQMC04 拉取对应主线提交，使用新发行目录重新构建内嵌 Web 的 Server；备份并保留旧配置/发行目录，更新现有 production LaunchAgent。版本、服务与 Chrome 页面结果读回后回写 Linear。提交与部署收尾按约定不重复执行测试，真实上线结论以实际读回为准；本摘要记录开发证据，不提前声明上线成功。
