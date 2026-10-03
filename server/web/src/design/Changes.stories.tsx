import type {Meta,StoryObj} from '@storybook/react-vite';
import {Card} from 'antd';
import {ReviewApp} from './ReviewApp';
import {Studio} from './Studio';
import {FaviconDesign,FullSyncDesign,MetricsDesign,NotificationDesign} from './SyncDesign';

function ChangeIndex(){
 const changes=[
  ['notifications','操作通知','保存、改名等结果统一为 Notification；可预览成功和失败重试。'],
  ['metrics','Prometheus 指标','独立指标鉴权、采集/接收时间、积压与数据库状态。后端能力说明。'],
  ['overview','年度汇总与柱状图','全年 Token 与 API 等价成本常驻；按模型自然日柱状图。'],
  ['recovered-quota','同步修复后的账号用量','大快照分片不阻塞最新额度；账号仍按可靠身份关联。'],
  ['full-sync','增量与全量补传','原生 App 的确认、进行中、暂停、来源受阻与完成状态样稿。'],
  ['device-status','设备同步状态','正式页面组件展示同步状态和全量任务状态。'],
  ['favicon','SVG 标签页图标','实际 SVG 资源及大小预览。'],
  ['last-update','账号最后更新数据','reset 后保留最后有效额度、Credits 与节奏快照；原时间可见。'],
  ['unknown-quota','无有效额度观测','未知仍为未知，不根据套餐价格生成虚假的用量。'],
  ['conflicted-quota','来源冲突','保留真实冲突提示，不伪装成新观测。'],
  ['pricing','相关模型与 API 参考折算','复用正式 Pricing 页面，一行一个模型、发布时间倒排；特殊条件和历史证据展开查看。'],
  ['bounded-quota','四周期与按需详情','首屏读取摘要；节奏只加载选中窗口；来源证据展开后分页，最后有效值保留。'],
 ];
 return <Studio page="overview" hideToolbar reviewContent={<div className="ds-stack"><div className="ds-review-banner"><strong>TOO-523 · 本轮全部改动</strong><span>正式页面组件 + 合成 API 样本；后端与原生功能单列说明。</span></div><div className="ds-review-grid">{changes.map(([id,title,text])=><Card key={id}><h3><a href={`/?path=/story/too-523-updates--${id}`}>{title} →</a></h3><p>{text}</p></Card>)}<Card><h3><a href="/?path=/story/subscription-plans--codex">订阅分类样稿 →</a></h3><p>参考官方购买页，按平台及个人/团队分类；本次待评审样稿，未替换正式订阅页。</p></Card></div></div>}/>;
}
const meta={id:'too-523-updates',title:'本轮优化',component:ReviewApp,parameters:{layout:'fullscreen',docs:{disable:true}},args:{initialPath:'/',scenario:'expired'}} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Index:Story={name:'00 全部改动目录',render:()=> <ChangeIndex/>};
export const Overview:Story={name:'01 年度汇总与模型柱状图'};
export const Notifications:Story={name:'02 操作通知',render:()=> <NotificationDesign/>};
export const LastUpdate:Story={name:'03 额度与 Credits · 最后更新',args:{initialPath:'/quota'}};
export const RecoveredQuota:Story={name:'04 同步恢复 · 最新账号用量',args:{initialPath:'/quota',scenario:'current'}};
export const UnknownQuota:Story={name:'05 额度未知',args:{initialPath:'/quota',scenario:'unknown'}};
export const ConflictedQuota:Story={name:'06 真实来源冲突',args:{initialPath:'/quota',scenario:'conflict'}};
export const DeviceStatus:Story={name:'07 设备同步与全量状态',args:{initialPath:'/devices'}};
export const FullSync:Story={name:'08 原生增量与全量补传',render:()=> <FullSyncDesign/>};
export const Pricing:Story={name:'09 相关模型与 API 参考折算',args:{initialPath:'/pricing'}};
export const Metrics:Story={name:'10 Prometheus 指标说明',render:()=> <MetricsDesign/>};
export const Favicon:Story={name:'11 SVG 浏览器图标',render:()=> <FaviconDesign/>};
export const BoundedQuota:Story={name:'12 四周期与按需详情',args:{initialPath:'/quota',scenario:'current'}};
