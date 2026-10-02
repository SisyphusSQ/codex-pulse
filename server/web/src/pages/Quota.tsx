import { useState } from 'react';
import { Alert, Button, Card, Select, Table, Tabs, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { getPace, getQuotas, type QuotaAccount, type QuotaCredits, type QuotaFilter, type QuotaResponse, type QuotaWindow } from '../api/quotas';
import { getDevices } from '../api/statistics';
import { AccountIdentity, CreditsCard, PacePanel, QuotaEvidence, QuotaWindowCard } from '../components/QuotaViews';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { dateTime, providerNames } from '../format';

interface AccountGroup { key:string; accountKey:string|null; account?:QuotaAccount; provider:string; windows:QuotaWindow[]; credits:QuotaCredits[] }
function accountGroups(data:QuotaResponse):AccountGroup[]{
 const groups=new Map<string,AccountGroup>();
 for(const account of data.accounts)groups.set(`account:${account.key}`,{key:`account:${account.key}`,accountKey:account.key,account,provider:account.provider,windows:[],credits:[]});
 for(const row of [...data.windows,...data.credits]){
  const key=row.account_key===null?'unassigned':`account:${row.account_key}`;
  if(!groups.has(key))groups.set(key,{key,accountKey:row.account_key,provider:row.provider,windows:[],credits:[]});
 }
 for(const w of data.windows)groups.get(w.account_key===null?'unassigned':`account:${w.account_key}`)!.windows.push(w);
 for(const c of data.credits)groups.get(c.account_key===null?'unassigned':`account:${c.account_key}`)!.credits.push(c);
 return [...groups.values()];
}

export default function Quota(){
 const [filter,setFilter]=useState<QuotaFilter>({provider:'',client_id:'',account_key:''});
 const [selectedAccount,setSelectedAccount]=useState('');
 const [selectedKey,setSelectedKey]=useState('');
 const [detailTab,setDetailTab]=useState('pace');
 const polling={staleTime:15_000,refetchInterval:30_000,refetchIntervalInBackground:false};
 const catalogFilter={...filter,account_key:''};
 const catalog=useQuery({queryKey:['quotas',catalogFilter],queryFn:({signal})=>getQuotas(catalogFilter,signal),...polling});
 const query=useQuery({queryKey:['quotas',filter],queryFn:({signal})=>getQuotas(filter,signal),...polling});
 const pace=useQuery({queryKey:['pace',filter],queryFn:({signal})=>getPace(filter,signal),...polling});
 const devices=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
 const data=query.data;
 const groups=data?accountGroups(data):[];
 const group=groups.find(a=>a.key===selectedAccount)??groups.find(a=>a.windows.length||a.credits.length)??groups[0];
 const selected=group?.windows.find(w=>w.key===selectedKey)??group?.windows[0];
 const selectedPace=pace.data?.windows.find(w=>w.key===selected?.key);
 const busy=query.isFetching||pace.isFetching;
 function change(value:QuotaFilter){setFilter(value);setSelectedAccount('');setSelectedKey('');setDetailTab('pace');}
 function refresh(){void query.refetch();void pace.refetch();if(filter.account_key)void catalog.refetch();void devices.refetch();}
 return <section><div className="page-heading"><div><Typography.Title level={3}>额度与节奏</Typography.Title><Typography.Paragraph type="secondary">账号额度、reset 与历史节奏，保留各来源观测。</Typography.Paragraph></div></div>
 <div className="stats-filters"><div className="filter-control"><label>Provider</label><Select aria-label="额度 Provider" value={filter.provider} onChange={provider=>change({...filter,provider,account_key:''})} options={[{value:'',label:'全部 Provider'},...Object.entries(providerNames).map(([value,label])=>({value,label}))]} /></div>
 <div className="filter-control"><label>采集来源</label><Select aria-label="额度采集来源" value={filter.client_id} loading={devices.isPending} showSearch={{optionFilterProp:'label'}} onChange={client_id=>change({...filter,client_id,account_key:''})} options={[{value:'',label:'全部来源'},...(devices.data??[]).map(d=>({value:d.id,label:d.name+(d.revoked_at_ms?'（已撤销）':'')}))]} /></div>
 <div className="filter-control quota-account-filter"><label>确认账号</label><Select aria-label="额度账号" value={filter.account_key} loading={catalog.isPending} showSearch={{optionFilterProp:'label'}} onChange={account_key=>change({...filter,account_key})} options={[{value:'',label:'全部账号（含待关联）'},...(catalog.data?.accounts??[]).map(a=>({value:a.key,label:`${providerNames[a.provider]??a.provider} · ${a.email??'邮箱未提供'} · ${a.raw_id}`}))]} /></div><Button onClick={refresh} loading={busy} aria-label="刷新额度">刷新</Button></div>
 {(catalog.error||devices.error)&&<Alert showIcon type="warning" className="form-alert" title="筛选选项读取失败，可刷新重试" />}
 <Typography.Paragraph type="secondary">每 30 秒读取中心已有统计；不会触发各机器主动刷新。剩余时间为中心确认时的快照，自动读回后更新；关闭 App 期间没有采样时不补造曲线。</Typography.Paragraph>
 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={refresh} />:data&&<>
 {query.error&&<Alert showIcon type="warning" className="form-alert" title="额度刷新失败，保留上次读取的数据与原时间" description={query.error.message} />}
 <Typography.Paragraph type="secondary">额度评估：{dateTime(data.evaluated_at_ms)} · 仅已观测事实{data.windows.length?` · ${data.windows.length} 个窗口`:''}</Typography.Paragraph>
 {!groups.length?<EmptyState description="尚无已收到的额度或 Credits。请启用设备上报；未确认账号不根据邮箱推断归属。" />:<div className="quota-workspace">
 <Card title="账号" className="quota-selection"><Table<AccountGroup> rowKey="key" size="small" showHeader={false} dataSource={groups} pagination={false} rowClassName={a=>a.key===group?.key?'selected-window':''} columns={[
  {key:'account',render:(_,a)=><div className="record-list-row"><Button type="link" className="record-link" aria-pressed={a.key===group?.key} onClick={()=>{setSelectedAccount(a.key);setSelectedKey('');setDetailTab('pace');}} aria-label={a.accountKey===null?'查看待关联观测':`查看账号 ${providerNames[a.provider]??a.provider} ${a.account?.email??'邮箱未提供'} ${a.account?.raw_id??a.accountKey}`}>{a.accountKey===null?'待关联观测':a.account?.email??'账号信息未取得'}</Button><div className="record-id">{a.accountKey===null?'尚未确认账号 ID':a.account?.raw_id??a.accountKey}</div><div className="metric-note">{a.accountKey!==null?`${providerNames[a.provider]??a.provider} · ${a.account?.plan??'套餐未知'} · `:''}{a.windows.length} 个额度窗口 · {a.credits.length} 份 Credits 库存</div></div>},
 ]} /></Card>
 {group&&<Card key={group.key} className="quota-detail" title={group.accountKey===null?'待关联观测':'账号额度与重置次数'}>
  {group.accountKey===null?<Alert showIcon type="warning" className="form-alert" title="账号尚未确认关联" description="以下观测各自展示，不视为同一账号，也不按邮箱或采集设备推断归属。" />:group.account?<AccountIdentity account={group.account} provider={group.provider} />:<Alert showIcon type="warning" className="form-alert" title="账号资料尚未取得" description="当前额度和 Credits 仍按已收到的账号键关联，邮箱、原始 ID 与套餐保持未知。" />}
  {selected?<><Tabs className="quota-window-tabs" activeKey={selected.key} onChange={key=>{setSelectedKey(key);setDetailTab('pace');}} items={group.windows.map(w=>({key:w.key,label:`${w.limit_id} · ${w.window_kind==='primary'?'主窗口':w.window_kind==='secondary'?'次窗口':w.window_kind} · ${w.window_minutes??'未知'} 分钟${group.accountKey===null?` · ${w.observations[0]?.client_name??'来源未知'} · ${w.key.slice(0,8)}`:''}`}))} />
  <QuotaWindowCard showAccount={group.accountKey===null} window={selected} account={group.account} devices={devices.data??[]} onSelect={()=>{setDetailTab('pace');document.getElementById('quota-pace-tabs')?.scrollIntoView({behavior:'smooth',block:'start'});}} /></>:<Typography.Paragraph type="secondary" className="section-card">当前账号暂无已收到的额度窗口。</Typography.Paragraph>}
  {group.credits.length?group.credits.map(c=><CreditsCard key={c.key} showAccount={group.accountKey===null} credits={c} account={group.account} devices={devices.data??[]} />):<Typography.Paragraph type="secondary" className="section-card">{group.accountKey===null?'暂无待关联的 Credits 观测。':'当前账号暂无已收到的 Reset Credits，库存保持未知。'}</Typography.Paragraph>}
  {selected&&<>
  <Tabs id="quota-pace-tabs" activeKey={detailTab} onChange={setDetailTab} items={[
   {key:'pace',label:'节奏与历史',children:pace.isPending?<LoadingState label="正在读取节奏统计…" />:pace.error&&!pace.data?<ErrorState error={pace.error} retry={()=>void pace.refetch()} />:<>
    {pace.error&&<Alert type="warning" showIcon className="section-card" title="节奏刷新失败，保留上次评估" description={pace.error.message} />}
    {selectedPace?<PacePanel key={selected.key} window={selectedPace} evaluatedAt={pace.data!.evaluated_at_ms} />:<Alert type="info" className="section-card" title="节奏快照中暂无这个窗口，可刷新重读" />}
   </>},
   {key:'evidence',label:`来源证据 (${selected.observations.length})`,children:<QuotaEvidence window={selected} />},
  ]} /></>}
 </Card>}
 </div>}
 </>}
 </section>;
}
