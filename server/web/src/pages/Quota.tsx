import { useState } from 'react';
import { Alert, Button, Card, Collapse, Progress, Select, Tabs, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { getPace, getQuotas, type QuotaAccount, type QuotaCredits, type QuotaFilter, type QuotaResponse, type QuotaWindow } from '../api/quotas';
import { getDevices } from '../api/statistics';
import { Link,Navigate,useSearchParams } from 'react-router-dom';
import {SubscriptionPanel} from '../components/SubscriptionPanel';
import {ObservationFilters} from '../components/ObservationFilters';
import {percent} from '../components/QuotaViews';
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

export function QuotaAccounts(){
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
 return <section>
 <ObservationFilters value={filter} providerLabel="额度 Provider" sourceLabel="额度采集来源" refreshLabel="刷新额度" onChange={scope=>change({...filter,...scope,account_key:''})} refresh={refresh} busy={busy} extra={<Select className="quota-account-filter" aria-label="额度账号" value={filter.account_key} loading={catalog.isPending} showSearch={{optionFilterProp:'label'}} onChange={account_key=>change({...filter,account_key})} options={[{value:'',label:'全部账号（含待关联）'},...(catalog.data?.accounts??[]).map(a=>({value:a.key,label:`${providerNames[a.provider]??a.provider} · ${a.email??'邮箱未提供'} · ${a.raw_id}`}))]} />} />
 {(catalog.error||devices.error)&&<Alert showIcon type="warning" className="form-alert" title="筛选选项读取失败，可刷新重试" />}
 <div className="metric-note quota-refresh-note">每30秒读取中心快照 · 不触发远端刷新 · App关闭期间的采样缺口保留</div>
 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={refresh} />:data&&<>
 {query.error&&<Alert showIcon type="warning" className="form-alert" title="额度刷新失败，保留上次读取的数据与原时间" description={query.error.message} />}
 <Typography.Paragraph type="secondary">额度评估：{dateTime(data.evaluated_at_ms)} · 仅已观测事实{data.windows.length?` · ${data.windows.length} 个窗口`:''}</Typography.Paragraph>
 {!groups.length?<EmptyState description="尚无已收到的额度或 Credits。请启用设备上报；未确认账号不根据邮箱推断归属。" />:<div className="quota-workspace">
 <Card title="账号" className="quota-selection">{groups.map(a=><button type="button" className={`account-item ${a.key===group?.key?'selected':''}`} key={a.key} aria-pressed={a.key===group?.key} onClick={()=>{setSelectedAccount(a.key);setSelectedKey('');setDetailTab('pace');}} aria-label={a.accountKey===null?'查看待关联观测':`查看账号 ${providerNames[a.provider]??a.provider} ${a.account?.email??'邮箱未提供'} ${a.account?.raw_id??a.accountKey}`}><span className="account-provider">{a.accountKey===null?'待关联观测':providerNames[a.provider]??a.provider}</span><strong>{a.account?.email??(a.accountKey===null?'尚未确认账号':'账号信息未取得')}</strong><span>{a.account?.plan??'套餐未知'} · {a.windows.length} 个额度窗口</span><small>{a.account?.raw_id??a.accountKey??'尚未确认账号 ID'}</small></button>)}</Card>
 {group&&<Card key={group.key} className="quota-detail">
  {group.accountKey===null?<Alert showIcon type="warning" className="form-alert" title="账号尚未确认关联" description="以下观测各自展示，不视为同一账号，也不按邮箱或采集设备推断归属。" />:group.account?<AccountIdentity account={group.account} provider={group.provider} showDetails={false} />:<Alert showIcon type="warning" className="form-alert" title="账号资料尚未取得" description="当前额度和 Credits 仍按已收到的账号键关联，邮箱、原始 ID 与套餐保持未知。" />}
  {group.account&&group.accountKey&&<SubscriptionPanel accountKey={group.accountKey} provider={group.provider} />}
  <div className="quota-window-overview">{group.windows.map(w=><Card key={w.key} size="small" className={w.key===selected?.key?'quota-window-summary selected-window':'quota-window-summary'}>
   <Button type="link" className="record-link" aria-pressed={w.key===selected?.key} onClick={()=>{setSelectedKey(w.key);setDetailTab('pace');}}>{w.limit_id} · {w.window_minutes==null?'窗口时长未知':w.window_minutes%1440===0?`${w.window_minutes/1440} 天`:w.window_minutes%60===0?`${w.window_minutes/60} 小时`:`${w.window_minutes} 分钟`}</Button>
   <div className="quota-summary-value">{w.current.freshness==='fresh'&&!w.current.conflict&&w.current.remaining_percent!==null?`剩余 ${percent(w.current.remaining_percent)}`:'当前未知'}</div>
   {w.current.freshness==='fresh'&&!w.current.conflict&&w.current.remaining_percent!==null?<Progress percent={w.current.remaining_percent} showInfo={false} size="small" />:<div className="metric-note">上次剩余 {percent(w.current.remaining_percent)} · 原观测 {dateTime(w.current.observed_at_ms)}</div>}
   <div className="metric-note">已用 <span>{percent(w.current.used_percent)}</span> · reset {dateTime(w.current.resets_at_ms)}</div>
   <div className="metric-note">{w.current.freshness==='fresh'?'新鲜观测':w.current.freshness.startsWith('expired')?'reset已过，当前额度未知':'观测陈旧或未知'}{w.current.conflict?' · 来源冲突':''}{group.accountKey===null?` · ${w.observations[0]?.client_name??'来源未知'}`:''}</div>
  </Card>)}</div>
  {!group.windows.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的额度窗口。</Typography.Paragraph>}
  {!group.credits.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的 Reset Credits，库存保持未知。</Typography.Paragraph>}
  {group.credits.map(c=><CreditsCard key={c.key} showAccount={group.accountKey===null} credits={c} account={group.account} devices={devices.data??[]} />)}
  {selected&&<>
  <Tabs id="quota-pace-tabs" activeKey={detailTab} onChange={setDetailTab} items={[
   {key:'pace',label:'节奏与历史',children:pace.isPending?<LoadingState label="正在读取节奏统计…" />:pace.error&&!pace.data?<ErrorState error={pace.error} retry={()=>void pace.refetch()} />:<>
    {pace.error&&<Alert type="warning" showIcon className="section-card" title="节奏刷新失败，保留上次评估" description={pace.error.message} />}
    {selectedPace?<PacePanel key={selected.key} window={selectedPace} evaluatedAt={pace.data!.evaluated_at_ms} />:<Alert type="info" className="section-card" title="节奏快照中暂无这个窗口，可刷新重读" />}
   </>},
   {key:'evidence',label:`来源证据 (${selected.observations.length})`,children:<QuotaEvidence window={selected} />},
  ]} /></>}
 <Collapse ghost size="small" className="quota-details" items={[
  ...(selected?[{key:'window',label:'额度窗口与来源详情',children:<QuotaWindowCard showAccount={group.accountKey===null} window={selected} account={group.account} devices={devices.data??[]} onSelect={()=>setDetailTab('pace')} />}]:[]),
 ]} />
 </Card>}
 </div>}
 </>}
 </section>;
}

export function legacyUsageTarget(params:URLSearchParams) {
 const next=new URLSearchParams(params);next.delete('view');
 return `/usage/models${next.size?`?${next.toString()}`:''}`;
}
export default function Quota(){
 const [params]=useSearchParams();
 if(params.get('view')==='usage')return <Navigate to={legacyUsageTarget(params)} replace />;
 return <section className="accounts-page"><div className="page-heading"><Link to="/pricing">模型与订阅价目表</Link></div><QuotaAccounts /></section>;
}
