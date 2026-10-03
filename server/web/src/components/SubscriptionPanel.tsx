import {useState} from 'react';
import {Alert,Button,DatePicker,Descriptions,Form,Input,InputNumber,Modal,Select,Typography} from 'antd';
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query';
import {Link} from 'react-router-dom';
import type {Dayjs} from 'dayjs';
import {getSubscription,saveSubscription} from '../api/subscriptions';
import type {Subscription} from '../api/subscriptions';
import {dayjs} from '../format';
import {useOperationNotifications} from './OperationNotifications';
import {ErrorState,LoadingState} from './QueryState';

interface Fields {alias?:string;manual_plan?:string;date_kind:Subscription['date_kind'];renewal_day?:number;membership_date?:Dayjs|null;time_zone:string}
function Editor({value,onSaved,onReload}:{value:Subscription;onSaved(value:Subscription):void;onReload():void}){
 const notify=useOperationNotifications();
 const [form]=Form.useForm<Fields>();
 const kind=Form.useWatch('date_kind',form)??value.date_kind;
 const mutation=useMutation({mutationFn:(v:Fields)=>saveSubscription(value.account_key,{expected_revision:value.revision,alias:v.alias?.trim()||null,manual_plan:v.manual_plan?.trim()||null,date_kind:v.date_kind,renewal_day:v.date_kind==='monthly_renewal'?v.renewal_day??null:null,membership_date:v.date_kind==='membership_expiry'?v.membership_date?.format('YYYY-MM-DD')??null:null,time_zone:v.time_zone}),onSuccess:onSaved,onError:error=>notify.error('订阅设置未保存',error.message)});
 return <Form<Fields> form={form} layout="vertical" className="subscription-editor" initialValues={{alias:value.alias??'',manual_plan:value.manual_plan??'',date_kind:value.date_kind,renewal_day:value.renewal_day??1,membership_date:value.membership_date?dayjs(value.membership_date):null,time_zone:value.time_zone}} onFinish={v=>mutation.mutate(v)} disabled={mutation.isPending}>
 <div className="subscription-fields"><Form.Item label="账号备注" name="alias" rules={[{max:128,message:'最多128个字符'}]}><Input maxLength={128} /></Form.Item>
 <Form.Item label="手动套餐" name="manual_plan" extra={`自动识别：${value.automatic_plan??'未知'}；留空使用识别值`} rules={[{max:64,message:'最多64个字符'}]}><Input maxLength={64} placeholder="如 Plus、Pro、SuperGrok" /></Form.Item>
 <Form.Item label="订阅日期类型" name="date_kind"><Select options={[{value:'',label:'未设置'},{value:'monthly_renewal',label:'每月续费日'},{value:'membership_expiry',label:'会员到期日'}]} /></Form.Item>
 {kind==='monthly_renewal'&&<Form.Item label="每月几号续费" name="renewal_day" rules={[{required:true,message:'填写1至31日'},{type:'number',min:1,max:31,message:'填写1至31日'}]} extra="短月份取当月最后一天，经过后滚动到下个月"><InputNumber min={1} max={31} precision={0} /></Form.Item>}
 {kind==='membership_expiry'&&<Form.Item label="会员到期日期" name="membership_date" rules={[{required:true,message:'选择完整到期日'}]}><DatePicker format="YYYY-MM-DD" /></Form.Item>}
 <Form.Item label="日期时区" name="time_zone" rules={[{required:true,message:'填写IANA时区'},{max:64,message:'时区过长'}]}><Input placeholder="Asia/Shanghai" maxLength={64} /></Form.Item></div>
 <Typography.Paragraph type="secondary">此处记录订阅日期，不执行扣款，也不修改平台额度reset或Credits到期。</Typography.Paragraph>
 {mutation.error&&<Button size="small" onClick={onReload}>重新读取已保存设置</Button>}
 <div className="association-bar"><Button type="primary" htmlType="submit" loading={mutation.isPending}>保存订阅设置</Button><Button onClick={()=>form.setFieldsValue({alias:'',manual_plan:'',date_kind:'',membership_date:null})}>清空手动设置</Button><Button onClick={onReload}>取消</Button></div>
 </Form>;
}
export function SubscriptionPanel({accountKey,provider}:{accountKey:string;provider:string}){
 const notify=useOperationNotifications();
 const cache=useQueryClient();const [editing,setEditing]=useState(false);
 const query=useQuery({queryKey:['subscription',accountKey],queryFn:({signal})=>getSubscription(accountKey,signal),refetchOnWindowFocus:false});
 const v=query.data;
 async function reload(){await query.refetch();setEditing(false);}
 if(query.isPending)return <LoadingState label="正在读取订阅设置…" />;
 if(query.error&&!v)return <ErrorState error={query.error} retry={()=>void query.refetch()} />;
 if(!v)return null;
 const dateText=v.next_date===null?'未设置':`${v.next_date}${v.date_state==='today'?' · 今天':v.day_delta!==null&&v.day_delta<0?` · 已过期${Math.abs(v.day_delta)}天`:v.day_delta!==null?` · 还有${v.day_delta}天`:''}`;
 return <div className="subscription-panel">
 {query.error&&<Alert type="warning" title="订阅读取失败，保留上次设置" description={query.error.message} className="form-alert" />}
 <div className="subscription-header"><Typography.Text strong>{v.alias||'账号订阅'}</Typography.Text><div><Link to={`/pricing?provider=${encodeURIComponent(provider)}`}>参考价目</Link> · <Button type="link" size="small" onClick={()=>{setEditing(!editing);}}>{editing?'收起编辑':'编辑订阅'}</Button></div></div>
 <Descriptions size="small" column={{xs:1,md:2}} items={[{key:'plan',label:'套餐',children:<>{v.resolved_plan??'未知'}{v.manual_plan&&<Typography.Text type="secondary">（手动；识别值 {v.automatic_plan??'未知'}）</Typography.Text>}</>},{key:'date',label:v.date_kind==='membership_expiry'?'会员到期':v.date_kind==='monthly_renewal'?`每月${v.renewal_day}日续费`:'订阅日期',children:dateText}]} />
 {editing&&<Modal open title="编辑账号订阅" footer={null} onCancel={()=>void reload()}><Editor key={`${v.account_key}:${v.revision}`} value={v} onSaved={next=>{cache.setQueryData(['subscription',accountKey],next);setEditing(false);notify.success('订阅设置已保存到中心');}} onReload={()=>void reload()} /></Modal>}
 </div>;
}
