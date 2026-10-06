import { useSearchParams } from 'react-router-dom';
import { useState } from 'react';
import {useFeedbackQuery as useQuery} from '../components/QueryNotifications';
import { getSessions, type ListFilter } from '../api/records';
import { CoverageNotice } from '../components/CoverageNotice';
import { initialListFilter, ListFilters, RecordSearchControls, sameRecordScope } from '../components/ListFilters';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { SessionPanel, SessionTable, TotalsLine } from '../components/RecordViews';
import { RecordWorkspace } from '../components/RecordWorkspace';
import { integer } from '../format';

export default function Sessions(){
 const [params]=useSearchParams();
 const [filter,setFilter]=useState(()=>{
  const value=initialListFilter();
  for(const key of ['start_date','end_date_exclusive','time_zone','provider','client_id','model','search','project_id'] as const){const v=params.get(key);if(v!==null)value[key]=v;}
  if(params.get('sort')==='tokens')value.sort='tokens';return value;
 }),[selected,setSelected]=useState<string|undefined>(()=>params.get('selected')??undefined);
 const query=useQuery({queryKey:['sessions','list',filter],queryFn:({signal})=>getSessions(filter,signal)});
 const detailFilter={...filter,page:1,limit:25};
 function change(v:ListFilter){setFilter(v);if(!sameRecordScope(v,filter))setSelected(undefined);}
 return <section><ListFilters value={filter} onChange={change} refresh={()=>void query.refetch()} busy={query.isFetching} />
 {query.data&&<TotalsLine totals={query.data.totals} />}
 <RecordWorkspace selectedId={selected} listLabel="会话列表" detailLabel="会话详情" list={<>
  <RecordSearchControls value={filter} onChange={change} /><div className="record-list-heading">会话记录{query.data?` · 共 ${integer(query.data.page.total)} 条`:''}</div>
  {query.isPending?<LoadingState />:query.error&&!query.data?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<><SessionTable compact selectedId={selected} rows={query.data.items} page={query.data.page} loading={query.isFetching} zone={filter.time_zone} onPage={(page,limit)=>change({...filter,page,limit})} onOpen={setSelected} /><CoverageNotice coverage={query.data.coverage} zone={filter.time_zone} /></>}
 </>} detail={selected?<SessionPanel key={selected} id={selected} filter={detailFilter} onClose={()=>setSelected(undefined)} />:<EmptyState description="选择一个会话，查看用量、缓存和活动详情。" />} />
 </section>;
}
