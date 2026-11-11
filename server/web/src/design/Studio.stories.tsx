import type {Meta,StoryObj} from '@storybook/react-vite';
import {ReviewApp} from './ReviewApp';

const meta={title:'全站设计',component:ReviewApp,parameters:{layout:'fullscreen',docs:{disable:true}},args:{initialPath:'/',scenario:'current'},argTypes:{initialPath:{control:'select',options:['/','/usage/models','/usage/sources','/quota','/projects','/sessions','/pricing','/devices']},scenario:{control:'select',options:['current','expired','conflict','unknown','empty','error','anonymous']}}} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Overview:Story={name:'01 用量概览'};
export const Models:Story={name:'02 模型分析',args:{initialPath:'/usage/models'}};
export const Sources:Story={name:'03 采集来源',args:{initialPath:'/usage/sources'}};
export const Accounts:Story={name:'04 账号与额度',args:{initialPath:'/quota'}};
export const Projects:Story={name:'05 项目',args:{initialPath:'/projects'}};
export const Sessions:Story={name:'06 会话',args:{initialPath:'/sessions'}};
export const Pricing:Story={name:'07 模型与订阅价目',args:{initialPath:'/pricing'}};
export const Devices:Story={name:'08 设备与授权',args:{initialPath:'/devices'}};
export const SignIn:Story={name:'09 浏览器配对',args:{scenario:'anonymous'}};

export const MobileOverview:Story={name:'10 窄屏概览',globals:{viewport:{value:'mobile',isRotated:false}}};
export const MobileAccounts:Story={name:'11 窄屏账号',args:{initialPath:'/quota'},globals:{viewport:{value:'mobile',isRotated:false}}};
export const MobileProjects:Story={name:'12 窄屏项目与详情',args:{initialPath:'/projects'},globals:{viewport:{value:'mobile',isRotated:false}}};
export const MobileSessions:Story={name:'13 窄屏会话与详情',args:{initialPath:'/sessions'},globals:{viewport:{value:'mobile',isRotated:false}}};
