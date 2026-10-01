import { useState } from 'react';
import { Card, Typography } from 'antd';
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
 return <section><div className="page-heading"><div><Typography.Title level={3}>会话</Typography.Title><Typography.Paragraph type="secondary">查询标题、原始 Session ID 与生命周期指标。</Typography.Paragraph></div></div><ListFilters value={filter} onChange={change} refresh={()=>void query.refetch()} busy={query.isFetching} />
 {query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<><TotalsLine totals={query.data.totals} /><Card className="table-card" title="会话记录"><SessionTable rows={query.data.items} page={query.data.page} loading={query.isFetching} zone={filter.time_zone} onPage={(page,limit)=>change({...filter,page,limit})} onOpen={setSelected} /></Card><CoverageNotice coverage={query.data.coverage} zone={filter.time_zone} /></>}
 {selected&&<SessionPanel id={selected} filter={filter} onClose={()=>setSelected(undefined)} />}</section>;
}
