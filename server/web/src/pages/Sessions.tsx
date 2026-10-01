import { useState } from 'react';
import { Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { getSessions, type ListFilter } from '../api/records';
import { CoverageNotice } from '../components/CoverageNotice';
import { initialListFilter, ListFilters } from '../components/ListFilters';
import { ErrorState, LoadingState } from '../components/QueryState';
import { SessionPanel, SessionTable, TotalsLine } from '../components/RecordViews';

export default function Sessions(){
 const [filter,setFilter]=useState(initialListFilter),[selected,setSelected]=useState<string>();
 const query=useQuery({queryKey:['sessions','list',filter],queryFn:({signal})=>getSessions(filter,signal)});
 function change(v:ListFilter){setFilter(v);setSelected(undefined);}
 return <section><div className="page-heading"><div><span className="eyebrow">SESSIONS</span><Typography.Title level={2}>会话</Typography.Title><Typography.Paragraph type="secondary">标题、原始 Session ID 与结构化统计。搜索和分页查询中心完整范围。</Typography.Paragraph></div></div><ListFilters value={filter} onChange={change} refresh={()=>void query.refetch()} busy={query.isFetching} />
 {query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<><TotalsLine totals={query.data.totals} /><SessionTable rows={query.data.items} page={query.data.page} loading={query.isFetching} zone={filter.time_zone} onPage={(page,limit)=>change({...filter,page,limit})} onOpen={setSelected} /><CoverageNotice coverage={query.data.coverage} zone={filter.time_zone} /></>}
 {selected&&<SessionPanel id={selected} filter={filter} onClose={()=>setSelected(undefined)} />}</section>;
}
