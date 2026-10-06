import type {Meta,StoryObj} from '@storybook/react-vite';
import {ReviewApp} from './ReviewApp';
const meta={id:'too-530-query-feedback',title:'TOO-530 查询反馈验收',component:ReviewApp,parameters:{layout:'fullscreen',docs:{disable:true}},args:{ticket:'TOO-530',initialPath:'/',scenario:'current'}} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Current:Story={name:'01 正常概览'};
export const Error:Story={name:'02 查询失败 · 通知与紧凑重试',args:{scenario:'error'}};
export const Quota:Story={name:'03 额度与持久状态',args:{initialPath:'/quota',scenario:'conflict'}};
