import type {Meta,StoryObj} from '@storybook/react-vite';
import {ReviewApp} from './ReviewApp';
const meta={id:'too-524-center-overview',title:'TOO-524 中心优化验收',component:ReviewApp,parameters:{layout:'fullscreen',docs:{disable:true}},args:{ticket:'TOO-524',initialPath:'/',scenario:'current'}} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Today:Story={name:'01 今天 · 小时活动与机器用量'};
export const ThirtyDays:Story={name:'02 30天 · 点击日期查看小时',args:{initialPath:'/?days=30'}};
export const Accounts:Story={name:'03 Pro 7天 / Plus 未取得 · 保留节奏历史',args:{initialPath:'/quota'}};
export const Projects:Story={name:'04 项目采集机器',args:{initialPath:'/projects'}};
export const Sessions:Story={name:'05 会话来源 · 无工具技能',args:{initialPath:'/sessions'}};
export const Unknown:Story={name:'06 未取得用量',args:{scenario:'empty'}};
export const Error:Story={name:'07 接口失败与重试',args:{scenario:'error'}};
export const Mobile:Story={name:'08 窄屏概览',parameters:{viewport:{defaultViewport:'mobile1'}}};

export const Authorizations:Story={name:'09 APP 与管理浏览器授权',args:{initialPath:'/devices'}};
