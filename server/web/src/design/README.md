# 交互设计稿

此目录仅由`.storybook`加载；业务入口不导入。全部内容是合成样本，不请求中心API、不读取Home、不创建真实授权。

- `Studio`：完整导航、九个页面、共享筛选与状态。
- `analytics`：概览、模型、来源、账号、Credits与节奏。
- `workspace`：项目/会话分屏、价目、设备与浏览器配对。
- `fixtures`：固定样本，真实NULL/零/未知概念分别保留。
- 三组stories：全站设计9条、关键状态8条、公共组件5条。
- `studio.css`：`ds-`命名空间，复用业务主题/单位格式/日历几何。

启动`npm --prefix server/web run storybook`，打开`http://127.0.0.1:6007/`。设计决定、公开参考与范围见[设计说明](../../../../docs/design/details/multi-machine-reporting/storybook-design.md)。
