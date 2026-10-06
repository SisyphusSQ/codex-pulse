import { useState } from 'react';
import { Button, Card, Progress, Select, Typography } from 'antd';
import {useFeedbackQuery as useQuery} from '../components/QueryNotifications';
import { getPace, getQuotas, getQuotaAccounts, type QuotaAccount, type QuotaCredits, type QuotaFilter, type QuotaResponse, type QuotaWindow } from '../api/quotas';
import { getDevices } from '../api/statistics';
import { Navigate,useSearchParams } from 'react-router-dom';
import {EvidenceIcon} from '../components/EvidenceIcon';
import {SubscriptionPanel} from '../components/SubscriptionPanel';
import {ObservationFilters} from '../components/ObservationFilters';
import {percent} from '../components/QuotaViews';
import { AccountIdentity, CreditsCard, PacePanel } from '../components/QuotaViews';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { dateTime, providerNames, tokens } from '../format';

interface AccountGroup { key:string; accountKey:string|null; account?:QuotaAccount; provider:string; windows:QuotaWindow[]; credits:QuotaCredits[] }
function allowedMinutes(plan:string|null|undefined):number[]{return ['pro','prolite','pro5x','pro20x'].includes(plan?.toLowerCase()??'')?[10080]:plan?.toLowerCase()==='plus'?[300,10080]:[];}
function quotaSlots(group:AccountGroup){return group.provider==='codex'?allowedMinutes(group.account?.plan).map(minutes=>({minutes,window:group.windows.find(w=>w.window_minutes===minutes)})):group.windows.map(window=>({minutes:window.window_minutes,window}));}
function accountGroups(data:QuotaResponse):AccountGroup[]{
 const groups=new Map<string,AccountGroup>();
 for(const account of data.accounts)groups.set(`account:${account.key}`,{key:`account:${account.key}`,accountKey:account.key,account,provider:account.provider,windows:[],credits:[]});
 for(const w of data.windows)if(w.account_key!==null){const group=groups.get(`account:${w.account_key}`);if(group&&(w.provider!=='codex'||w.limit_id==='codex'&&w.window_minutes!==null&&allowedMinutes(group.account?.plan).includes(w.window_minutes)))group.windows.push(w);}
 for(const c of data.credits)if(c.account_key!==null)groups.get(`account:${c.account_key}`)?.credits.push(c);
 for(const group of groups.values())group.windows.sort((a,b)=>(a.window_minutes??Infinity)-(b.window_minutes??Infinity));
 return [...groups.values()];
}

export function QuotaAccounts(){

 const [filter,setFilter]=useState<QuotaFilter>({provider:'',client_id:'',account_key:''});
 const [selectedAccount,setSelectedAccount]=useState('');
 const [selectedKey,setSelectedKey]=useState('');
 const polling={staleTime:15_000,refetchInterval:30_000,refetchIntervalInBackground:false};
 const catalogFilter={...filter,account_key:''};
 const catalog=useQuery({queryKey:['quota-accounts',catalogFilter],queryFn:({signal})=>getQuotaAccounts(catalogFilter,signal),...polling});
 const query=useQuery({queryKey:['quotas',filter],queryFn:({signal})=>getQuotas(filter,signal,{view:'summary'}),...polling});
 const devices=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
 const data=query.data;
 const groups=data?accountGroups(data):[];
 const group=groups.find(a=>a.key===selectedAccount)??groups.find(a=>a.windows.length||a.credits.length)??groups[0];
 const selected=group?.windows.find(w=>w.key===selectedKey)??group?.windows[0];
 const pace=useQuery({queryKey:['pace',filter,selected?.key],queryFn:({signal})=>getPace(filter,signal,selected?.key),enabled:Boolean(selected),...polling});
 const selectedPace=pace.data?.windows.find(w=>w.key===selected?.key);
 const busy=query.isFetching||pace.isFetching;
 function change(value:QuotaFilter){setFilter(value);setSelectedAccount('');setSelectedKey('');}
 function refresh(){void query.refetch();if(selected)void pace.refetch();void catalog.refetch();void devices.refetch();}
 return <section>
 <ObservationFilters value={filter} providerLabel="额度 Provider" sourceLabel="额度采集来源" refreshLabel="刷新额度" onChange={scope=>change({...filter,...scope,account_key:''})} refresh={refresh} busy={busy} extra={<Select className="quota-account-filter" aria-label="额度账号" value={filter.account_key} loading={catalog.isPending} showSearch={{optionFilterProp:'label'}} onChange={account_key=>change({...filter,account_key})} options={[{value:'',label:'全部账号'},...(catalog.data?.accounts??[]).map(a=>({value:a.key,label:`${providerNames[a.provider]??a.provider} · ${a.email??'邮箱未提供'} · ${a.raw_id}`}))]} />} />

 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={refresh} />:data&&<>

 <div className="quota-snapshot-note"><span>中心读取 {dateTime(data.evaluated_at_ms)}</span><EvidenceIcon label="中心快照说明"><p>每30秒读取中心摘要；历史按选中窗口加载，最多保留四个周期。额度、节奏和 Credits 显示最后一次有效更新及其时间，收到新观测后更新；App关闭期间保留这份数据。当前收到 {data.windows.length} 个窗口。</p></EvidenceIcon></div>
 {!groups.length?<EmptyState description="尚无已收到的额度或 Credits。请启用设备上报；未确认账号不根据邮箱推断归属。" />:<div className="quota-workspace">
 <Card title="账号" className="quota-selection">{groups.map(a=><button type="button" className={`account-item ${a.key===group?.key?'selected':''}`} key={a.key} aria-pressed={a.key===group?.key} onClick={()=>{setSelectedAccount(a.key);setSelectedKey('');}} aria-label={a.accountKey===null?'查看待关联观测':`查看账号 ${providerNames[a.provider]??a.provider} ${a.account?.email??'邮箱未提供'} ${a.account?.raw_id??a.accountKey}`}><span className="account-provider">{a.accountKey===null?'待关联观测':providerNames[a.provider]??a.provider}</span><strong>{a.account?.email??(a.accountKey===null?'尚未确认账号':'账号信息未取得')}</strong><span>{a.account?.plan??'套餐未知'} · {a.windows.length} 个额度窗口</span><small>{a.account?.raw_id??a.accountKey??'尚未确认账号 ID'}</small></button>)}</Card>
 {group&&<Card key={group.key} className="quota-detail">
  <AccountIdentity account={group.account} provider={group.provider} showDetails={false} />
  {group.account&&group.accountKey&&<SubscriptionPanel accountKey={group.accountKey} provider={group.provider} />}
  <div className="quota-window-overview">{quotaSlots(group).map(({minutes,window:w})=>w?<Card key={w.key} size="small" className={w.key===selected?.key?'quota-window-summary selected-window':'quota-window-summary'}>
   <Button type="link" className="record-link" aria-pressed={w.key===selected?.key} onClick={()=>{setSelectedKey(w.key);}}>{w.limit_id} · {w.window_minutes==null?'窗口时长未知':w.window_minutes%1440===0?`${w.window_minutes/1440} 天`:w.window_minutes%60===0?`${w.window_minutes/60} 小时`:`${w.window_minutes} 分钟`}</Button>
   <div className="quota-summary-value">{w.current.remaining_percent!==null?`剩余 ${percent(w.current.remaining_percent)}`:'尚无有效观测'}</div>
   {w.current.remaining_percent!==null?<Progress aria-label="最后观测剩余额度" percent={w.current.remaining_percent} showInfo={false} size="small" />:null}
   <div className="metric-note">已用 <span>{percent(w.current.used_percent)}</span> · reset {dateTime(w.current.resets_at_ms)}</div>
   {w.provider==='codex'&&w.window_minutes===10080&&<div className="metric-note">本周期已记录 Token：<Typography.Text strong>{w.recorded_tokens==null?'—':tokens(w.recorded_tokens)}</Typography.Text></div>}
   <div className="metric-note">最后更新 {dateTime(w.current.observed_at_ms)}{w.current.conflict?' · 来源冲突':''}{w.current.freshness==='suspicious'?' · 发现可疑观测，保留有效值':''}{group.accountKey===null?` · ${devices.data?.find(d=>d.id===w.current.selected_client_id)?.name??'来源未知'}`:''}</div>
  </Card>:<Card key={`missing:${minutes}`} size="small" className="quota-window-summary"><strong>{minutes===300?'5 小时':'7 天'}</strong><div className="quota-summary-value">未取得</div><div className="metric-note">等待该窗口的有效采集观测</div></Card>)}</div>
  {!group.windows.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的额度窗口。</Typography.Paragraph>}
  {!group.credits.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的 Reset Credits，库存保持未知。</Typography.Paragraph>}
  {group.credits.map(c=><CreditsCard key={c.key} showAccount={group.accountKey===null} credits={c} account={group.account} devices={devices.data??[]} />)}
  {selected&&<>
  <Typography.Title level={5}>节奏与历史</Typography.Title>
  {pace.isPending?<LoadingState label="正在读取节奏统计…" />:pace.error&&!pace.data?<ErrorState error={pace.error} retry={()=>void pace.refetch()} />:<>

    {selectedPace?<PacePanel key={selected.key} window={selectedPace} evaluatedAt={pace.data!.evaluated_at_ms} />:<div className="quiet-empty" role="status">节奏快照中暂无这个窗口，可刷新重读。</div>}
   </>}
  </>}
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
