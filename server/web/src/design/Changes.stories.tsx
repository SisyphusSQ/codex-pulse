import type {Meta,StoryObj} from '@storybook/react-vite';
import {Card} from 'antd';
import {ReviewApp} from './ReviewApp';

function ChangeIndex(){
 const pages=[['全站页面','/?path=/story/全站设计--overview'],['账号与 Credits','/?path=/story/too-702-credits--balance'],['订阅与模型价目','/?path=/story/subscription-plans--codex'],['关键状态','/?path=/story/关键状态--stale'],['公共组件','/?path=/story/公共组件--credits']];
 return <section className="storybook-index"><h1>当前 Web 样式与交互</h1><p className="metric-note">所有入口复用正式组件、pulseTheme 和 styles.css；统计与账号均为合成样本。</p><div className="storybook-index-grid">{pages.map(([name,url])=><Card key={name}><h2><a href={url} target="_top">{name}</a></h2></Card>)}</div></section>;
}
const meta={id:'too-523-updates',title:'功能场景',component:ReviewApp,parameters:{layout:'fullscreen',docs:{disable:true}},args:{initialPath:'/',scenario:'current'}} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Index:Story={name:'00 全部改动目录',render:()=> <ChangeIndex/>};
export const Overview:Story={name:'01 年度汇总与模型柱状图'};
export const Notifications:Story={name:'02 设备授权操作与通知',args:{initialPath:'/devices'}};
export const LastUpdate:Story={name:'03 额度与 Credits · 最后更新',args:{initialPath:'/quota'}};
export const RecoveredQuota:Story={name:'04 同步恢复 · 最新账号用量',args:{initialPath:'/quota',scenario:'current'}};
export const UnknownQuota:Story={name:'05 额度未知',args:{initialPath:'/quota',scenario:'unknown'}};
export const ConflictedQuota:Story={name:'06 真实来源冲突',args:{initialPath:'/quota',scenario:'conflict'}};
export const DeviceStatus:Story={name:'07 设备同步与全量状态',args:{initialPath:'/devices'}};
export const FullSync:Story={name:'08 Web 同步状态',args:{initialPath:'/devices'}};
export const Pricing:Story={name:'09 相关模型与 API 参考折算',args:{initialPath:'/pricing'}};
export const Metrics:Story={name:'10 采集来源状态',args:{initialPath:'/usage/sources'}};
export const Favicon:Story={name:'11 当前页面外壳与导航'};
export const BoundedQuota:Story={name:'12 四周期与按需详情',args:{initialPath:'/quota',scenario:'current'}};
