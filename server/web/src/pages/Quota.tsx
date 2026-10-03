import { useState } from 'react';
import { Alert, Button, Card, Collapse, Progress, Select, Tabs, Typography } from 'antd';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { getPace, getQuotas, getQuotaAccounts, type QuotaAccount, type QuotaCredits, type QuotaFilter, type QuotaResponse, type QuotaWindow } from '../api/quotas';
import { getDevices } from '../api/statistics';
import { Navigate,useSearchParams } from 'react-router-dom';
import {EvidenceIcon} from '../components/EvidenceIcon';
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
 const queryClient=useQueryClient();
 const [filter,setFilter]=useState<QuotaFilter>({provider:'',client_id:'',account_key:''});
 const [selectedAccount,setSelectedAccount]=useState('');
 const [selectedKey,setSelectedKey]=useState('');
 const [detailTab,setDetailTab]=useState('pace');
 const polling={staleTime:15_000,refetchInterval:30_000,refetchIntervalInBackground:false};
 const catalogFilter={...filter,account_key:''};
 const catalog=useQuery({queryKey:['quota-accounts',catalogFilter],queryFn:({signal})=>getQuotaAccounts(catalogFilter,signal),...polling});
 const query=useQuery({queryKey:['quotas',filter],queryFn:({signal})=>getQuotas(filter,signal,{view:'summary'}),...polling});
 const devices=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
 const data=query.data;
 const groups=data?accountGroups(data):[];
 const group=groups.find(a=>a.key===selectedAccount)??groups.find(a=>a.windows.length||a.credits.length)??groups[0];
 const selected=group?.windows.find(w=>w.key===selectedKey)??group?.windows[0];
 const pace=useQuery({queryKey:['pace',filter,selected?.key],queryFn:({signal})=>getPace(filter,signal,selected?.key),enabled:Boolean(selected)&&detailTab==='pace',...polling});
 const selectedPace=pace.data?.windows.find(w=>w.key===selected?.key);
 const busy=query.isFetching||pace.isFetching;
 function change(value:QuotaFilter){setFilter(value);setSelectedAccount('');setSelectedKey('');setDetailTab('pace');}
 function refresh(){void query.refetch();if(selected&&detailTab==='pace')void pace.refetch();if(selected&&detailTab==='evidence')void queryClient.invalidateQueries({queryKey:['quota-evidence',filter,selected.key]});void catalog.refetch();void devices.refetch();}
 return <section>
 <ObservationFilters value={filter} providerLabel="额度 Provider" sourceLabel="额度采集来源" refreshLabel="刷新额度" onChange={scope=>change({...filter,...scope,account_key:''})} refresh={refresh} busy={busy} extra={<Select className="quota-account-filter" aria-label="额度账号" value={filter.account_key} loading={catalog.isPending} showSearch={{optionFilterProp:'label'}} onChange={account_key=>change({...filter,account_key})} options={[{value:'',label:'全部账号（含待关联）'},...(catalog.data?.accounts??[]).map(a=>({value:a.key,label:`${providerNames[a.provider]??a.provider} · ${a.email??'邮箱未提供'} · ${a.raw_id}`}))]} />} />
 {(catalog.error||devices.error)&&<Alert showIcon type="warning" className="form-alert" title="筛选选项读取失败，可刷新重试" />}
 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={refresh} />:data&&<>
 {query.error&&<Alert showIcon type="warning" className="form-alert" title="额度刷新失败，保留上次读取的数据与原时间" description={query.error.message} />}
 <div className="quota-snapshot-note"><span>中心读取 {dateTime(data.evaluated_at_ms)}</span><EvidenceIcon label="中心快照说明"><p>每30秒读取中心摘要；历史按选中窗口加载，最多保留四个周期。额度、节奏和 Credits 显示最后一次有效更新及其时间，收到新观测后更新；App关闭期间保留这份数据。当前收到 {data.windows.length} 个窗口。</p></EvidenceIcon></div>
 {!groups.length?<EmptyState description="尚无已收到的额度或 Credits。请启用设备上报；未确认账号不根据邮箱推断归属。" />:<div className="quota-workspace">
 <Card title="账号" className="quota-selection">{groups.map(a=><button type="button" className={`account-item ${a.key===group?.key?'selected':''}`} key={a.key} aria-pressed={a.key===group?.key} onClick={()=>{setSelectedAccount(a.key);setSelectedKey('');setDetailTab('pace');}} aria-label={a.accountKey===null?'查看待关联观测':`查看账号 ${providerNames[a.provider]??a.provider} ${a.account?.email??'邮箱未提供'} ${a.account?.raw_id??a.accountKey}`}><span className="account-provider">{a.accountKey===null?'待关联观测':providerNames[a.provider]??a.provider}</span><strong>{a.account?.email??(a.accountKey===null?'尚未确认账号':'账号信息未取得')}</strong><span>{a.account?.plan??'套餐未知'} · {a.windows.length} 个额度窗口</span><small>{a.account?.raw_id??a.accountKey??'尚未确认账号 ID'}</small></button>)}</Card>
 {group&&<Card key={group.key} className="quota-detail">
  {group.accountKey===null?<Alert showIcon type="warning" className="form-alert" title="账号尚未确认关联" description="以下观测各自展示，不视为同一账号，也不按邮箱或采集设备推断归属。" />:group.account?<AccountIdentity account={group.account} provider={group.provider} showDetails={false} />:<Alert showIcon type="warning" className="form-alert" title="账号资料尚未取得" description="当前额度和 Credits 仍按已收到的账号键关联，邮箱、原始 ID 与套餐保持未知。" />}
  {group.account&&group.accountKey&&<SubscriptionPanel accountKey={group.accountKey} provider={group.provider} />}
  <div className="quota-window-overview">{group.windows.map(w=><Card key={w.key} size="small" className={w.key===selected?.key?'quota-window-summary selected-window':'quota-window-summary'}>
   <Button type="link" className="record-link" aria-pressed={w.key===selected?.key} onClick={()=>{setSelectedKey(w.key);setDetailTab('pace');}}>{w.limit_id} · {w.window_minutes==null?'窗口时长未知':w.window_minutes%1440===0?`${w.window_minutes/1440} 天`:w.window_minutes%60===0?`${w.window_minutes/60} 小时`:`${w.window_minutes} 分钟`}</Button>
   <div className="quota-summary-value">{w.current.remaining_percent!==null?`剩余 ${percent(w.current.remaining_percent)}`:'尚无有效观测'}</div>
   {w.current.remaining_percent!==null?<Progress aria-label="最后观测剩余额度" percent={w.current.remaining_percent} showInfo={false} size="small" />:null}
   <div className="metric-note">已用 <span>{percent(w.current.used_percent)}</span> · reset {dateTime(w.current.resets_at_ms)}</div>
   <div className="metric-note">最后更新 {dateTime(w.current.observed_at_ms)}{w.current.conflict?' · 来源冲突':''}{w.current.freshness==='suspicious'?' · 发现可疑观测，保留有效值':''}{group.accountKey===null?` · ${devices.data?.find(d=>d.id===w.current.selected_client_id)?.name??'来源未知'}`:''}</div>
  </Card>)}</div>
  {!group.windows.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的额度窗口。</Typography.Paragraph>}
  {!group.credits.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的 Reset Credits，库存保持未知。</Typography.Paragraph>}
  {group.credits.map(c=><CreditsCard key={c.key} showAccount={group.accountKey===null} credits={c} account={group.account} devices={devices.data??[]} />)}
  {selected&&<>
  <Tabs destroyOnHidden id="quota-pace-tabs" activeKey={detailTab} onChange={setDetailTab} items={[
   {key:'pace',label:'节奏与历史',children:pace.isPending?<LoadingState label="正在读取节奏统计…" />:pace.error&&!pace.data?<ErrorState error={pace.error} retry={()=>void pace.refetch()} />:<>
    {pace.error&&<Alert type="warning" showIcon className="section-card" title="节奏刷新失败，保留上次评估" description={pace.error.message} />}
    {selectedPace?<PacePanel key={selected.key} window={selectedPace} evaluatedAt={pace.data!.evaluated_at_ms} />:<Alert type="info" className="section-card" title="节奏快照中暂无这个窗口，可刷新重读" />}
   </>},
   {key:'evidence',label:selected.observation_count==null?'来源证据':`来源证据 (${selected.observation_count})`,children:<QuotaEvidence window={selected} filter={filter} />},
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
 return <section className="accounts-page"><QuotaAccounts /></section>;
}
