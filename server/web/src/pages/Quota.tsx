import { useState } from 'react';
import { Alert, Button, Card, Select, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { getPace, getQuotas, type QuotaFilter } from '../api/quotas';
import { getDevices } from '../api/statistics';
import { CreditsCard, PacePanel, QuotaEvidence, QuotaWindowCard } from '../components/QuotaViews';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { dateTime, providerNames } from '../format';

export default function Quota(){
 const [filter,setFilter]=useState<QuotaFilter>({provider:'',client_id:'',account_key:''});
 const [selectedKey,setSelectedKey]=useState('');
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
 function change(value:QuotaFilter){setFilter(value);setSelectedKey('');}
 function refresh(){void query.refetch();void pace.refetch();if(filter.account_key)void catalog.refetch();void devices.refetch();}
 return <section><div className="page-heading"><div><span className="eyebrow">QUOTA & PACE</span><Typography.Title level={2}>额度与节奏</Typography.Title><Typography.Paragraph type="secondary">统一查看账号窗口、真实 reset 和已观测曲线。各台机器的额度不累加。</Typography.Paragraph></div></div>
 <div className="stats-filters"><div className="filter-control"><label>Provider</label><Select aria-label="额度 Provider" value={filter.provider} onChange={provider=>change({...filter,provider,account_key:''})} options={[{value:'',label:'全部 Provider'},...Object.entries(providerNames).map(([value,label])=>({value,label}))]} /></div>
 <div className="filter-control"><label>采集来源</label><Select aria-label="额度采集来源" value={filter.client_id} loading={devices.isPending} showSearch={{optionFilterProp:'label'}} onChange={client_id=>change({...filter,client_id,account_key:''})} options={[{value:'',label:'全部来源'},...(devices.data??[]).map(d=>({value:d.id,label:d.name+(d.revoked_at_ms?'（已撤销）':'')}))]} /></div>
 <div className="filter-control quota-account-filter"><label>确认账号</label><Select aria-label="额度账号" value={filter.account_key} loading={catalog.isPending} showSearch={{optionFilterProp:'label'}} onChange={account_key=>change({...filter,account_key})} options={[{value:'',label:'全部账号（含待关联）'},...(catalog.data?.accounts??[]).map(a=>({value:a.key,label:`${providerNames[a.provider]??a.provider} · ${a.email??'邮箱未提供'} · ${a.raw_id}`}))]} /></div><Button onClick={refresh} loading={busy} aria-label="刷新额度">刷新</Button></div>
 {(catalog.error||devices.error)&&<Alert showIcon type="warning" className="form-alert" title="筛选选项读取失败，可刷新重试" />}
 <Typography.Paragraph type="secondary">每 30 秒读取中心已有统计；不会触发各机器主动刷新。剩余时间为中心确认时的快照，自动读回后更新；关闭 App 期间没有采样时不补造曲线。</Typography.Paragraph>
 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={refresh} />:data&&<>
 {query.error&&<Alert showIcon type="warning" className="form-alert" title="额度刷新失败，保留上次读取的数据与原时间" description={query.error.message} />}
 <Typography.Paragraph type="secondary">额度评估：{dateTime(data.evaluated_at_ms)} · 仅已观测事实{data.windows.length?` · ${data.windows.length} 个窗口`:''}</Typography.Paragraph>
 {!data.windows.length&&!data.credits.length?<EmptyState description="尚无已收到的额度或 Credits。请启用设备上报；未确认账号不根据邮箱推断归属。" />:<>
 <div className="chart-grid">{data.windows.map(w=><QuotaWindowCard key={w.key} window={w} account={data.accounts.find(a=>a.key===w.account_key)} devices={devices.data??[]} onSelect={()=>setSelectedKey(w.key)} />)}</div>
 {selected&&<Card title="选择查看的窗口" className="section-card"><Select className="quota-window-select" aria-label="额度窗口" value={selected.key} onChange={setSelectedKey} options={data.windows.map(w=>({value:w.key,label:`${providerNames[w.provider]??w.provider} · ${data.accounts.find(a=>a.key===w.account_key)?.email??'账号待关联'} · ${w.limit_id} · ${w.window_minutes??'未知'} 分钟 · ${w.key.slice(0,6)}`}))} /></Card>}
 {selected&&(pace.isPending?<LoadingState label="正在读取节奏统计…" />:pace.error&&!pace.data?<ErrorState error={pace.error} retry={()=>void pace.refetch()} />:<>
 {pace.error&&<Alert type="warning" showIcon className="section-card" title="节奏刷新失败，保留上次评估" description={pace.error.message} />}
 {selectedPace?<PacePanel key={selected.key} window={selectedPace} evaluatedAt={pace.data!.evaluated_at_ms} />:<Alert type="info" className="section-card" title="节奏快照中暂无这个窗口，可刷新重读" />}
 </>)}
 {selected&&<QuotaEvidence window={selected} />}
 {data.credits.map(c=><CreditsCard key={c.key} credits={c} account={data.accounts.find(a=>a.key===c.account_key)} devices={devices.data??[]} />)}
 </>}
 </>}
 </section>;
}
