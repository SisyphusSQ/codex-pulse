import type {Meta,StoryObj} from '@storybook/react-vite';
import {Card,Space,Button} from 'antd';
import {ReviewApp} from './ReviewApp';
import {reviewQuota,reviewPace,reviewDevices} from './reviewFixtures';
import {CreditsCard,PacePanel} from '../components/QuotaViews';
import {PaidCreditsCard} from '../components/PaidCreditsCard';
import {EmptyState,ErrorState,LoadingState} from '../components/QueryState';
import {EvidenceIcon} from '../components/EvidenceIcon';
import {SourceTable} from '../components/SourceTable';
import {ThroughputCell} from '../components/Throughput';
import {UsageCost} from '../components/UsageCost';
import {OperationNotifications,useOperationNotifications} from '../components/OperationNotifications';
import {ApiError} from '../api/client';

const meta={title:'公共组件',parameters:{layout:'fullscreen'},decorators:[Story=><div className="storybook-component"><Story/></div>]} satisfies Meta;
export default meta;
type Story=StoryObj<typeof meta>;
function ResetPreview({unknown=false}:{unknown?:boolean}){const q=reviewQuota(unknown?'unknown':'current');return <Card><CreditsCard credits={q.credits[0]} account={q.accounts[0]} devices={reviewDevices}/></Card>;}
function NotificationActions(){const notify=useOperationNotifications();return <Space wrap><Button onClick={()=>notify.success('订阅设置已保存到中心')}>保存成功</Button><Button onClick={()=>notify.error('操作未完成','请刷新后重试。',()=>notify.success('已重新读取'))}>失败与重试</Button></Space>;}
export const Toolbar:Story={name:'01 当前页面筛选与日期',render:()=> <ReviewApp/>};
export const Credits:Story={name:'02 Reset Credits 与到期明细',render:()=> <ResetPreview/>};
export const UnknownCredits:Story={name:'03 Reset Credits 库存未知',render:()=> <ResetPreview unknown/>};
export const PaceHelp:Story={name:'04 说明图标',render:()=> <Card><EvidenceIcon label="数据说明"><p>显示实际页面的说明样式；未知值不补成零。</p></EvidenceIcon></Card>};
export const DensePace:Story={name:'05 当前节奏与历史',render:()=> <Card><PacePanel window={reviewPace('current').windows[0]} evaluatedAt={reviewPace('current').evaluated_at_ms}/></Card>};
export const PaidCredits:Story={name:'06 计费 Credits 余额',render:()=> <Card><PaidCreditsCard value={reviewQuota('current').paid_credits![0]}/></Card>};
export const QueryStates:Story={name:'07 加载、空数据与权限错误',render:()=> <Space orientation="vertical" style={{width:'100%'}}><Card><LoadingState/></Card><Card><EmptyState/></Card><Card><ErrorState error={new ApiError(403)}/></Card></Space>};
export const Devices:Story={name:'08 采集来源表格',render:()=> <Card><SourceTable devices={reviewDevices} zone="Asia/Shanghai"/></Card>};
export const UnknownMetrics:Story={name:'09 费用与 TPS 未知值',render:()=> <Card><Space><UsageCost value={null}/><ThroughputCell/></Space></Card>};
export const Notifications:Story={name:'10 操作通知',render:()=> <OperationNotifications><Card><NotificationActions/></Card></OperationNotifications>};
