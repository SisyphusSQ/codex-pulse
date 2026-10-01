import { useState } from 'react';
import { Alert, Button, Card, Select, Table, Tabs, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { getPace, getQuotas, type QuotaFilter } from '../api/quotas';
import { getDevices } from '../api/statistics';
import { CreditsCard, PacePanel, QuotaEvidence, QuotaStatus, QuotaWindowCard, percent } from '../components/QuotaViews';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { dateTime, providerNames } from '../format';

export default function Quota(){
 const [filter,setFilter]=useState<QuotaFilter>({provider:'',client_id:'',account_key:''});
 const [selectedKey,setSelectedKey]=useState('');
 const [contentTab,setContentTab]=useState('windows');
 const [detailTab,setDetailTab]=useState('pace');
 const polling={staleTime:15_000,refetchInterval:30_000,refetchIntervalInBackground:false};
 const catalogFilter={...filter,account_key:''};
 const catalog=useQuery({queryKey:['quotas',catalogFilter],queryFn:({signal})=>getQuotas(catalogFilter,signal),...polling});
 const query=useQuery({queryKey:['quotas',filter],queryFn:({signal})=>getQuotas(filter,signal),...polling});
 const pace=useQuery({queryKey:['pace',filter],queryFn:({signal})=>getPace(filter,signal),...polling});
 const devices=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
 const data=query.data;
 const selected=data?.windows.find(w=>w.key===selectedKey)??data?.windows[0];
 const selectedPace=pace.data?.windows.find(w=>w.key===selected?.key);
 const busy=query.isFetching||pace.isFetching;
 function change(value:QuotaFilter){setFilter(value);setSelectedKey('');setDetailTab('pace');}
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
 {!data.windows.length&&!data.credits.length?<EmptyState description="尚无已收到的额度或 Credits。请启用设备上报；未确认账号不根据邮箱推断归属。" />:<>
 <Tabs activeKey={!data.windows.length&&data.credits.length?'credits':contentTab} onChange={setContentTab} items={[
 {key:'windows',label:`窗口与节奏 (${data.windows.length})`,children:<div className="quota-workspace">
 <Card title="账号窗口" className="quota-selection"><Table rowKey="key" size="small" dataSource={data.windows} pagination={false} rowClassName={w=>w.key===selected?.key?'selected-window':''} columns={[
  {title:'账号 / 窗口',render:(_,w)=><><Button type="link" className="record-link" onClick={()=>{setSelectedKey(w.key);setDetailTab('pace');}} aria-label={`查看节奏 ${w.limit_id} ${w.key}`}>{data.accounts.find(a=>a.key===w.account_key)?.email??'账号待关联'}</Button><div className="metric-note">{providerNames[w.provider]??w.provider} · {w.limit_id} · {w.window_minutes??'未知'} 分钟</div><div className="record-id">{data.accounts.find(a=>a.key===w.account_key)?.raw_id??'尚未确认账号 ID'}</div><QuotaStatus state={w.current.freshness} conflict={w.current.conflict} /></>},
  {title:'剩余',align:'right',width:84,render:(_,w)=><span className="numeric">{percent(w.current.remaining_percent)}</span>},
 ]} /></Card>
 {selected&&<Card className="quota-detail">
  <QuotaWindowCard window={selected} account={data.accounts.find(a=>a.key===selected.account_key)} devices={devices.data??[]} onSelect={()=>{setDetailTab('pace');document.getElementById('quota-pace-tabs')?.scrollIntoView({behavior:'smooth',block:'start'});}} />
  <Tabs id="quota-pace-tabs" activeKey={detailTab} onChange={setDetailTab} items={[
   {key:'pace',label:'节奏与历史',children:pace.isPending?<LoadingState label="正在读取节奏统计…" />:pace.error&&!pace.data?<ErrorState error={pace.error} retry={()=>void pace.refetch()} />:<>
    {pace.error&&<Alert type="warning" showIcon className="section-card" title="节奏刷新失败，保留上次评估" description={pace.error.message} />}
    {selectedPace?<PacePanel key={selected.key} window={selectedPace} evaluatedAt={pace.data!.evaluated_at_ms} />:<Alert type="info" className="section-card" title="节奏快照中暂无这个窗口，可刷新重读" />}
   </>},
   {key:'evidence',label:`来源证据 (${selected.observations.length})`,children:<QuotaEvidence window={selected} />},
  ]} />
 </Card>}
 </div>},
 {key:'credits',label:`Reset Credits (${data.credits.length})`,children:data.credits.length?<div className="chart-grid">{data.credits.map(c=><CreditsCard key={c.key} credits={c} account={data.accounts.find(a=>a.key===c.account_key)} devices={devices.data??[]} />)}</div>:<EmptyState description="当前筛选没有已收到的 Credits 观测。" />},
 ]} />
 </>}
 </>}
 </section>;
}
