import { useState } from 'react';
import { Button, Card, Progress, Segmented, Select, Tag, Typography } from 'antd';
import {useFeedbackQuery as useQuery} from '../components/QueryNotifications';
import { getPace, getQuotas, getQuotaAccounts, type QuotaAccount, type QuotaCredits, type QuotaFilter, type QuotaResponse, type QuotaWindow } from '../api/quotas';
import { getDevices, type Device } from '../api/statistics';
import { Navigate,useSearchParams } from 'react-router-dom';
import {EvidenceIcon} from '../components/EvidenceIcon';
import {SubscriptionPanel} from '../components/SubscriptionPanel';
import {ObservationFilters} from '../components/ObservationFilters';
import {AccountIdentity, CreditsCard, duration, PacePanel, percent} from '../components/QuotaViews';
import {PaidCreditsCard} from '../components/PaidCreditsCard';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { dateTime, providerLabel, quotaLimitLabel, quotaPoolRank, quotaWindowShortLabel, sourceKindLabel, tokens } from '../format';

interface AccountGroup { key:string; accountKey:string|null; account?:QuotaAccount; provider:string; windows:QuotaWindow[]; credits:QuotaCredits[] }
interface AgyPool { limitId:string; label:string; primary?:QuotaWindow; secondary?:QuotaWindow; extras:QuotaWindow[]; windows:QuotaWindow[]; source:string|null }

function allowedMinutes(plan:string|null|undefined):number[]{return ['pro','prolite','pro5x','pro20x'].includes(plan?.toLowerCase()??'')?[10080]:plan?.toLowerCase()==='plus'?[300,10080]:[];}
function quotaSlots(group:AccountGroup){return group.provider==='codex'?allowedMinutes(group.account?.plan).map(minutes=>({minutes,window:group.windows.find(w=>w.window_minutes===minutes)})):group.windows.map(window=>({minutes:window.window_minutes,window}));}
function agyPools(windows:QuotaWindow[]):AgyPool[]{
 const grouped=new Map<string,QuotaWindow[]>();
 for(const w of windows){
  const list=grouped.get(w.limit_id);
  if(list)list.push(w);
  else grouped.set(w.limit_id,[w]);
 }
 return [...grouped.entries()]
  .sort(([a],[b])=>quotaPoolRank(a)-quotaPoolRank(b)||a.localeCompare(b))
  .map(([limitId,items])=>{
   const primary=items.find(w=>w.window_kind==='primary'||w.window_minutes===300);
   const secondary=items.find(w=>w!==primary&&(w.window_kind==='secondary'||w.window_minutes===10080));
   const extras=items.filter(w=>w!==primary&&w!==secondary);
   const ordered=[...(primary?[primary]:[]),...(secondary?[secondary]:[]),...extras];
   const source=ordered.find(w=>w.current.source)?.current.source??null;
   return {limitId,label:quotaLimitLabel(limitId),primary,secondary,extras,windows:ordered,source};
  });
}
function accountGroups(data:QuotaResponse):AccountGroup[]{
 const groups=new Map<string,AccountGroup>();
 for(const account of data.accounts)groups.set(`account:${account.key}`,{key:`account:${account.key}`,accountKey:account.key,account,provider:account.provider,windows:[],credits:[]});
 for(const w of data.windows)if(w.account_key!==null){const group=groups.get(`account:${w.account_key}`);if(group&&(w.provider!=='codex'||w.limit_id==='codex'&&w.window_minutes!==null&&allowedMinutes(group.account?.plan).includes(w.window_minutes)))group.windows.push(w);}
 for(const c of data.credits)if(c.account_key!==null)groups.get(`account:${c.account_key}`)?.credits.push(c);
 for(const group of groups.values()){
  if(group.provider==='agy'){
   group.windows.sort((a,b)=>quotaPoolRank(a.limit_id)-quotaPoolRank(b.limit_id)||a.limit_id.localeCompare(b.limit_id)||(a.window_minutes??Infinity)-(b.window_minutes??Infinity));
  }else{
   group.windows.sort((a,b)=>(a.window_minutes??Infinity)-(b.window_minutes??Infinity));
  }
 }
 return [...groups.values()];
}

function agyWindowBadges(w:QuotaWindow){
 const tags=[];
 if(w.current.conflict){
  tags.push(<Tag key="conflict" color="red">来源冲突</Tag>);
 }else if(w.current.freshness==='expired_unknown'){
  tags.push(<Tag key="expired" color="default">已过期</Tag>);
 }else if(w.current.freshness==='stale'){
  tags.push(<Tag key="last-known" color="gold">last-known</Tag>);
  tags.push(<Tag key="stale" color="orange">陈旧</Tag>);
 }else if(w.current.freshness==='fresh'){
  tags.push(<Tag key="fresh" color="blue">最新观测</Tag>);
 }else{
  tags.push(<Tag key="unknown" color="default">未知</Tag>);
 }
 return tags;
}

function AgyPoolWindowSlot({poolLabel,slotLabel,window:w,selectedKey,onSelect,devices}:{poolLabel:string;slotLabel:string;window?:QuotaWindow;selectedKey?:string;onSelect(key:string):void;devices:Device[]}){
 if(!w){
  return <div className="agy-pool-window agy-pool-window-missing">
   <div className="agy-pool-window-head"><strong>{slotLabel}</strong><Tag color="default">未取得</Tag></div>
   <div className="quota-summary-value">--</div>
   <div className="metric-note">等待该窗口的有效采集观测</div>
  </div>;
 }
 const expired=w.current.freshness==='expired_unknown';
 const stale=w.current.freshness==='stale';
 const unknown=w.current.remaining_percent===null;
 const showActiveValue=!expired&&!unknown;
 const countdownMs=w.current.freshness==='fresh'&&!w.current.conflict?(w.current.reset_remaining_ms??w.current.snapshot_reset_remaining_ms??null):null;
 const clientName=w.current.selected_client_id?(devices.find(d=>d.id===w.current.selected_client_id)?.name??w.current.selected_client_id):null;
 const distinctClients=new Set(w.observations.map(o=>o.client_id)).size;
 return <div className={`agy-pool-window ${w.key===selectedKey?'selected-window':''}`}>
  <div className="agy-pool-window-head">
   <Button type="link" className="record-link" aria-pressed={w.key===selectedKey} aria-label={`${poolLabel} · ${slotLabel}`} onClick={()=>onSelect(w.key)}>{slotLabel}</Button>
   <div className="agy-pool-badges">{agyWindowBadges(w)}</div>
  </div>
  <div className="quota-summary-value">{showActiveValue?`剩余 ${percent(w.current.remaining_percent)}`:'--'}</div>
  {showActiveValue&&<Progress aria-label={`${poolLabel} ${slotLabel} 剩余额度`} percent={w.current.remaining_percent!} showInfo={false} size="small" strokeColor={stale?'#d3a34b':undefined} />}
  {expired&&w.current.remaining_percent!==null&&<div className="metric-note">已过 reset · 上次剩余 {percent(w.current.remaining_percent)}</div>}
  {!expired&&unknown&&<div className="metric-note">尚无有效观测</div>}
  <div className="metric-note">{countdownMs!=null?`距重置 ${duration(countdownMs)} · `:''}reset {dateTime(w.current.resets_at_ms)}</div>
  <div className="metric-note">最后观测 {dateTime(w.current.observed_at_ms)}{clientName?` · 采集设备 ${clientName}`:''}{distinctClients>1?` · 多机证据 ${distinctClients} 台`:''}</div>
 </div>;
}

export function QuotaAccounts(){

 const [filter,setFilter]=useState<QuotaFilter>({provider:'',client_id:'',account_key:''});
 const [selectedAccount,setSelectedAccount]=useState('');
 const [selectedKey,setSelectedKey]=useState('');
 const polling={staleTime:15_000,refetchInterval:60_000,refetchIntervalInBackground:false};
 const catalogFilter={...filter,account_key:''};
 const catalog=useQuery({queryKey:['quota-accounts',catalogFilter],queryFn:({signal})=>getQuotaAccounts(catalogFilter,signal),...polling});
 const query=useQuery({queryKey:['quotas',filter],queryFn:({signal})=>getQuotas(filter,signal,{view:'summary'}),...polling});
 const devices=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
 const data=query.data;
 const groups=data?accountGroups(data):[];
 const group=groups.find(a=>a.key===selectedAccount)??groups.find(a=>a.windows.length||a.credits.length||data?.paid_credits?.some(c=>c.account_key===a.accountKey))??groups[0];
 const selected=group?.windows.find(w=>w.key===selectedKey)??group?.windows[0];
 const pace=useQuery({queryKey:['pace',filter,selected?.key],queryFn:({signal})=>getPace(filter,signal,selected?.key),enabled:Boolean(selected),...polling});
 const selectedPace=pace.data?.windows.find(w=>w.key===selected?.key);
 const busy=query.isFetching||pace.isFetching;
 const isAgy=group?.provider==='agy';
 const pools=isAgy&&group?agyPools(group.windows):[];
 const activePool=isAgy?pools.find(p=>p.windows.some(w=>w.key===selected?.key))??pools[0]:undefined;
 function change(value:QuotaFilter){setFilter(value);setSelectedAccount('');setSelectedKey('');}
 function refresh(){void query.refetch();if(selected)void pace.refetch();void catalog.refetch();void devices.refetch();}
 function switchAgyPool(limitId:string){
  const nextPool=pools.find(p=>p.limitId===limitId);
  if(!nextPool)return;
  const preferredKind=selected?.window_kind;
  const nextWindow=nextPool.windows.find(w=>w.window_kind===preferredKind)??nextPool.windows[0];
  if(nextWindow)setSelectedKey(nextWindow.key);
 }
 return <section>
 <ObservationFilters value={filter} providerLabel="额度 Provider" sourceLabel="额度采集来源" refreshLabel="刷新额度" onChange={scope=>change({...filter,...scope,account_key:''})} refresh={refresh} busy={busy} extra={<Select className="quota-account-filter" aria-label="额度账号" value={filter.account_key} loading={catalog.isPending} showSearch={{optionFilterProp:'label'}} onChange={account_key=>change({...filter,account_key})} options={[{value:'',label:'全部账号'},...(catalog.data?.accounts??[]).map(a=>({value:a.key,label:`${providerLabel(a.provider)} · ${a.provider==='agy'?(a.plan??'Antigravity 账号'):(a.email??'邮箱未提供')} · ${a.raw_id}`}))]} />} />

 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={refresh} />:data&&<>

 <div className="quota-snapshot-note"><span>中心读取 {dateTime(data.evaluated_at_ms)}</span><EvidenceIcon label="中心快照说明"><p>每60秒读取中心摘要；历史按选中窗口加载，最多保留四个周期。额度、节奏和 Credits 显示最后一次有效更新及其时间，收到新观测后更新；App关闭期间保留这份数据。当前收到 {data.windows.length} 个窗口。</p></EvidenceIcon></div>
 {!groups.length?<EmptyState description="尚无已收到的额度或 Credits。请启用设备上报；未确认账号不根据邮箱推断归属。" />:<div className="quota-workspace">
 <Card title="账号" className="quota-selection">{groups.map(a=>{const agyAccount=a.provider==='agy';const agyPoolCount=agyAccount?agyPools(a.windows).length:0;return <button type="button" className={`account-item ${a.key===group?.key?'selected':''}`} key={a.key} aria-pressed={a.key===group?.key} onClick={()=>{setSelectedAccount(a.key);setSelectedKey('');}} aria-label={a.accountKey===null?'查看待关联观测':`查看账号 ${providerLabel(a.provider)} ${agyAccount?(a.account?.plan??'Antigravity 账号'):(a.account?.email??'邮箱未提供')} ${a.account?.raw_id??a.accountKey}`}><span className="account-provider">{a.accountKey===null?'待关联观测':providerLabel(a.provider)}</span><strong>{agyAccount?(a.account?.plan??'Antigravity 账号'):(a.account?.email??(a.accountKey===null?'尚未确认账号':'账号信息未取得'))}</strong><span>{agyAccount?`${agyPoolCount} 个配额池 · ${a.windows.length} 个窗口`:`${a.account?.plan??'套餐未知'} · ${a.windows.length} 个额度窗口`}</span><small>{a.account?.raw_id??a.accountKey??'尚未确认账号 ID'}</small></button>;})}</Card>
 {group&&<Card key={group.key} className="quota-detail">
  <AccountIdentity account={group.account} provider={group.provider} showDetails={false} />
  {group.account&&group.accountKey&&<SubscriptionPanel accountKey={group.accountKey} provider={group.provider} />}
  {isAgy?<>
   {pools.length>0&&<div className="metric-note agy-pool-footnote">多设备同账号观测按原采集时间仲裁，不相加设备配额；可通过上方采集来源筛选单台设备证据。</div>}
   <div className="agy-pool-grid">{pools.map(pool=><Card key={pool.limitId} size="small" className={`agy-pool-card ${activePool?.limitId===pool.limitId?'selected-pool':''}`} aria-label={`${pool.label} 配额池`}>
    <div className="agy-pool-header"><strong>{pool.label}</strong>{pool.source&&<Tag>{sourceKindLabel(pool.source)}</Tag>}</div>
    <div className="agy-pool-windows">
     <AgyPoolWindowSlot poolLabel={pool.label} slotLabel="5h" window={pool.primary} selectedKey={selected?.key} onSelect={setSelectedKey} devices={devices.data??[]} />
     <AgyPoolWindowSlot poolLabel={pool.label} slotLabel="周" window={pool.secondary} selectedKey={selected?.key} onSelect={setSelectedKey} devices={devices.data??[]} />
     {pool.extras.map(extra=><AgyPoolWindowSlot key={extra.key} poolLabel={pool.label} slotLabel={quotaWindowShortLabel(extra.window_kind,extra.window_minutes)} window={extra} selectedKey={selected?.key} onSelect={setSelectedKey} devices={devices.data??[]} />)}
    </div>
   </Card>)}</div>
   {!group.windows.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的配额池观测。</Typography.Paragraph>}
  </>:<>
   <div className="quota-window-overview">{quotaSlots(group).map(({minutes,window:w})=>w?<Card key={w.key} size="small" className={w.key===selected?.key?'quota-window-summary selected-window':'quota-window-summary'}>
    <Button type="link" className="record-link" aria-pressed={w.key===selected?.key} onClick={()=>{setSelectedKey(w.key);}}>{quotaLimitLabel(w.limit_id)} · {w.window_minutes==null?'窗口时长未知':w.window_minutes%1440===0?`${w.window_minutes/1440} 天`:w.window_minutes%60===0?`${w.window_minutes/60} 小时`:`${w.window_minutes} 分钟`}</Button>
    <div className="quota-summary-value">{w.current.remaining_percent!==null?`剩余 ${percent(w.current.remaining_percent)}`:'尚无有效观测'}</div>
    {w.current.remaining_percent!==null?<Progress aria-label="最后观测剩余额度" percent={w.current.remaining_percent} showInfo={false} size="small" />:null}
    <div className="metric-note">已用 <span>{percent(w.current.used_percent)}</span> · reset {dateTime(w.current.resets_at_ms)}</div>
    {w.provider==='codex'&&w.window_minutes===10080&&<div className="metric-note">本周期已记录 Token：<Typography.Text strong>{w.recorded_tokens==null?'—':tokens(w.recorded_tokens)}</Typography.Text></div>}
    <div className="metric-note">最后更新 {dateTime(w.current.observed_at_ms)}{w.current.conflict?' · 来源冲突':''}{w.current.freshness==='suspicious'?' · 发现可疑观测，保留有效值':''}{group.accountKey===null?` · ${devices.data?.find(d=>d.id===w.current.selected_client_id)?.name??'来源未知'}`:''}</div>
   </Card>:<Card key={`missing:${minutes}`} size="small" className="quota-window-summary"><strong>{minutes===300?'5 小时':'7 天'}</strong><div className="quota-summary-value">未取得</div><div className="metric-note">等待该窗口的有效采集观测</div></Card>)}</div>
   {!group.windows.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的额度窗口。</Typography.Paragraph>}
   {!group.credits.length&&<Typography.Paragraph type="secondary">当前账号暂无已收到的 Reset Credits，库存保持未知。</Typography.Paragraph>}
   {group.credits.map(c=><CreditsCard key={c.key} showAccount={group.accountKey===null} credits={c} account={group.account} devices={devices.data??[]} />)}
  </>}
  {selected&&<>
  <div className="agy-pace-heading">
   <Typography.Title level={5}>节奏与历史</Typography.Title>
   {isAgy&&activePool&&<div className="agy-pace-controls">
    {pools.length>1&&<Segmented aria-label="切换配额池" size="small" value={activePool.limitId} onChange={v=>switchAgyPool(String(v))} options={pools.map(p=>({value:p.limitId,label:p.label}))} />}
    {activePool.windows.length>1&&<Segmented aria-label="切换配额窗口" size="small" value={selected.key} onChange={v=>setSelectedKey(String(v))} options={activePool.windows.map(w=>({value:w.key,label:quotaWindowShortLabel(w.window_kind,w.window_minutes)}))} />}
   </div>}
  </div>
  {pace.isPending?<LoadingState label="正在读取节奏统计…" />:pace.error&&!pace.data?<ErrorState error={pace.error} retry={()=>void pace.refetch()} />:<>

    {selectedPace?<PacePanel key={selected.key} window={selectedPace} evaluatedAt={pace.data!.evaluated_at_ms} title={isAgy?`${quotaLimitLabel(selected.limit_id)} · ${quotaWindowShortLabel(selected.window_kind,selected.window_minutes)} 节奏评估`:undefined} sourceLabel={isAgy&&selected.current.source?sourceKindLabel(selected.current.source):undefined} />:<div className="quiet-empty" role="status">节奏快照中暂无这个窗口，可刷新重读。</div>}
   </>}
  </>}
  {!isAgy&&(query.data?.paid_credits??[]).filter(value=>value.account_key===group.accountKey).map(value=><PaidCreditsCard key={`${value.account_key}:${value.credit.limitId}`} value={value}/>)}
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
