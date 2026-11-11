import type {Meta,StoryObj} from '@storybook/react-vite';
import {Card} from 'antd';
import {ReviewApp} from './ReviewApp';
import {LoadingState} from '../components/QueryState';

const meta={title:'关键状态',component:ReviewApp,args:{initialPath:'/quota',scenario:'expired'},parameters:{layout:'fullscreen',docs:{disable:true}}} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Stale:Story={name:'01 保留最后观测'};
export const Conflict:Story={name:'02 来源冲突与预测暂停',args:{scenario:'conflict'}};
export const Empty:Story={name:'03 无数据',args:{initialPath:'/',scenario:'empty'}};
export const Loading:Story={name:'04 读取中',render:()=> <div className="storybook-component"><Card><LoadingState/></Card></div>};
export const Error:Story={name:'05 读取失败与重试',args:{initialPath:'/',scenario:'error'}};
export const ModelDetail:Story={name:'06 单模型分析',args:{initialPath:'/usage/models?provider=codex&model=gpt-6.1-sol',scenario:'current'}};
export const UnknownQuota:Story={name:'07 额度与余额未知',args:{scenario:'unknown'}};
export const StaleDevices:Story={name:'08 设备观测陈旧',args:{initialPath:'/devices'}};
export const EmptyProjects:Story={name:'09 项目空列表',args:{initialPath:'/projects',scenario:'empty'}};
export const EmptySessions:Story={name:'10 会话空列表',args:{initialPath:'/sessions',scenario:'empty'}};
export const QuotaError:Story={name:'11 账号读取失败',args:{scenario:'error'}};
