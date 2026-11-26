import type {Meta,StoryObj} from '@storybook/react-vite';
import {ReviewApp} from './ReviewApp';

const meta={
 id:'too-736-agy',
 title:'TOO-736 Antigravity (AGY)',
 component:ReviewApp,
 parameters:{layout:'fullscreen',docs:{disable:true}},
 args:{ticket:'TOO-736',initialPath:'/quota?provider=agy',scenario:'current'},
} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story=StoryObj<typeof meta>;

export const MultiPoolQuota:Story={name:'01 多配额池账号与额度 · 正常'};
export const MultiPoolExpired:Story={name:'02 多配额池 · 陈旧与已过期降级',args:{scenario:'expired'}};
export const MultiPoolConflict:Story={name:'03 多配额池 · 来源冲突',args:{scenario:'conflict'}};
export const MultiPoolUnknown:Story={name:'04 多配额池 · 观测未知',args:{scenario:'unknown'}};
export const OverviewAgy:Story={name:'05 用量概览 · Antigravity',args:{initialPath:'/?provider=agy'}};
export const ModelsAgy:Story={name:'06 模型分析 · AGY 公价估算与缓存命中率',args:{initialPath:'/usage/models?provider=agy'}};
export const SessionsAgy:Story={name:'07 会话与角色标签 · 主会话/子代理/Swarm/侧边提问',args:{initialPath:'/sessions?provider=agy&selected=agy-session-main'}};
export const SourcesAgy:Story={name:'08 采集来源与通道 · 本地轨迹与 LS RPC',args:{initialPath:'/usage/sources?provider=agy'}};
export const DevicesAgy:Story={name:'09 设备上报状态 · 含 AGY',args:{initialPath:'/devices'}};
export const PricingAgy:Story={name:'10 价目表 · AGY 分阶段参考目录与未收录模型',args:{initialPath:'/pricing?provider=agy'}};
export const PricingPlansAgy:Story={name:'11 价目表 · Antigravity 订阅套餐',args:{initialPath:'/pricing?provider=agy&tab=plans'}};
export const MobileMultiPoolQuota:Story={name:'12 窄屏多配额池',args:{initialPath:'/quota?provider=agy'},globals:{viewport:{value:'mobile',isRotated:false}}};
export const MultiPoolModelUsage:Story={
 name:'13 多配额池 · 本周期模型用量展开',
 args:{ticket:'TOO-742',previewTitle:'AGY 账号模型用量',initialPath:'/quota?provider=agy',scenario:'current'},
 play:async({canvas,userEvent})=>{
  await userEvent.click(await canvas.findByRole('button',{name:'本周期模型用量'}));
  await canvas.findByText('Gemini 3.1 Pro (High)');
 },
};
