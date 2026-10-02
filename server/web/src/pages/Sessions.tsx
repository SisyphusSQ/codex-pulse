import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getSessions, type ListFilter } from '../api/records';
import { CoverageNotice } from '../components/CoverageNotice';
import { initialListFilter, ListFilters, RecordSearchControls, sameRecordScope } from '../components/ListFilters';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { SessionPanel, SessionTable, TotalsLine } from '../components/RecordViews';
import { RecordWorkspace } from '../components/RecordWorkspace';
import { integer } from '../format';

export default function Sessions(){
 const [filter,setFilter]=useState(initialListFilter),[selected,setSelected]=useState<string>();
 const query=useQuery({queryKey:['sessions','list',filter],queryFn:({signal})=>getSessions(filter,signal)});
 const detailFilter={...filter,page:1,limit:25};
 function change(v:ListFilter){setFilter(v);if(!sameRecordScope(v,filter))setSelected(undefined);}
 return <section><ListFilters value={filter} onChange={change} refresh={()=>void query.refetch()} busy={query.isFetching} />
 {query.data&&<TotalsLine totals={query.data.totals} />}
 <RecordWorkspace selectedId={selected} listLabel="会话列表" detailLabel="会话详情" list={<>
  <RecordSearchControls value={filter} onChange={change} /><div className="record-list-heading">会话记录{query.data?` · 共 ${integer(query.data.page.total)} 条`:''}</div>
  {query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<><SessionTable compact selectedId={selected} rows={query.data.items} page={query.data.page} loading={query.isFetching} zone={filter.time_zone} onPage={(page,limit)=>change({...filter,page,limit})} onOpen={setSelected} /><CoverageNotice coverage={query.data.coverage} zone={filter.time_zone} /></>}
 </>} detail={selected?<SessionPanel key={selected} id={selected} filter={detailFilter} onClose={()=>setSelected(undefined)} />:<EmptyState description="选择一个会话，查看用量、缓存和活动详情。" />} />
 </section>;
}
