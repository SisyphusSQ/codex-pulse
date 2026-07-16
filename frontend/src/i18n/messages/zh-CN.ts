export const zhCNMessages = {
  app: {
    name: "Codex Pulse",
    description: "本机 Codex 用量与额度伴侣",
    service: {
      loading: "正在连接本机服务…",
      ready: "本机服务已连接",
      error: "本机服务暂不可用",
      errorDescription: "应用壳仍可使用。重新连接后会继续读取本机服务状态。",
      retry: "重新连接",
    },
    metadata: {
      platform: "运行平台",
      locale: "界面语言",
    },
  },
  nav: {
    primaryLabel: "主导航",
    overview: "概览",
    sessions: "会话",
    projects: "项目",
    quota: "配额",
    localStatus: "本机状态",
    settings: "设置",
  },
  routes: {
    overview: { title: "概览", description: "配额、Token 与 API 等价成本" },
    sessions: { title: "会话", description: "会话活动、模型与用量下钻" },
    projects: { title: "项目", description: "按安全归因查看项目使用情况" },
    quota: { title: "配额", description: "当前窗口、来源与 Reset credits" },
    localStatus: { title: "本机状态", description: "数据采集、索引与后台任务" },
    settings: { title: "设置", description: "本机数据、隐私与应用偏好" },
  },
  shell: {
    foundation: {
      title: "应用基础已就绪",
      description: "共享布局正在读取真实 Wails Bootstrap 元数据；业务数据由对应页面通过生成绑定接入。",
    },
    localOnly: {
      title: "本机数据",
      description: "Codex only · 不上传原始内容",
    },
  },
} as const;
