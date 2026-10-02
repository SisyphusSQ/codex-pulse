import {useState} from 'react';
import {Alert,Card,Descriptions,Typography} from 'antd';
import {useQuery} from '@tanstack/react-query';
import {Link} from 'react-router-dom';
import {getDevices} from '../api/statistics';
import {SourceTable} from '../components/SourceTable';
import {ObservationFilters,type ObservationScope} from '../components/ObservationFilters';
import {EmptyState,ErrorState,LoadingState} from '../components/QueryState';
import {dateTime,integer,providerNames} from '../format';
export default function Sources(){
 const [scope,setScope]=useState<ObservationScope>({provider:'',client_id:''});
 const [selected,setSelected]=useState('');
 const query=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
 const rows=(query.data??[]).filter(d=>(!scope.client_id||d.id===scope.client_id)&&(!scope.provider||d.providers.some(p=>p.provider===scope.provider))).map(d=>({...d,providers:d.providers.filter(p=>!scope.provider||p.provider===scope.provider)}));
 const current=rows.find(d=>d.id===selected)??rows[0];
 return <section><ObservationFilters value={scope} onChange={setScope} refresh={()=>void query.refetch()} busy={query.isFetching} />
 {query.isPending?<LoadingState />:query.error&&!query.data?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<>
 {query.error&&<Alert showIcon type="warning" title="来源更新失败，保留上次证据与原时间" description={query.error.message} className="form-alert" />}
 <Card title="来源与覆盖" extra={<Link to="/devices">管理设备与授权</Link>}><SourceTable devices={rows} selectedId={current?.id} onSelect={setSelected} /><div className="metric-note source-footnote">近期证据不证明全部历史已上传。采集来源不代表执行设备；不按设备副本累加消耗。</div></Card>
 {current?<Card className="section-card" title={`${current.name} · 来源详情`}><Descriptions size="small" column={{xs:1,md:2}} items={[{key:'id',label:'原始设备 ID',children:<Typography.Text copyable>{current.id}</Typography.Text>},{key:'received',label:'最近中心接收',children:dateTime(current.last_received_at_ms)}]} />{current.providers.map(p=><div className="source-provider-details" key={p.provider}><Typography.Text strong>{providerNames[p.provider]??p.provider}</Typography.Text><Descriptions size="small" column={{xs:1,md:3}} items={[{key:'range',label:'上报覆盖',children:`${dateTime(p.coverage_start_ms)} → ${dateTime(p.coverage_end_ms)}`},{key:'observed',label:'原采集时间',children:dateTime(p.collected_at_ms)},{key:'received',label:'中心接收',children:dateTime(p.received_at_ms)},{key:'version',label:'版本',children:p.version||'版本未知'},{key:'queue',label:'待上传批次',children:integer(p.pending_batches)},{key:'status',label:'证据',children:p.stale?'观测陈旧':'近期观测'}]} /></div>)}</Card>:<EmptyState description="当前筛选没有来源观测。" />}
 </>}
 </section>;
}
