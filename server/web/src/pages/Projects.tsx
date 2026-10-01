import { useState } from 'react';
import { Alert, Button, Card, Modal, Select, Table, Tag, Typography } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { associateProjects, getProject, getProjects, type ListFilter, type ProjectRecord } from '../api/records';
import { CoverageNotice } from '../components/CoverageNotice';
import { initialListFilter, ListFilters } from '../components/ListFilters';
import { ErrorState, LoadingState } from '../components/QueryState';
import { RecordTrend, SessionPanel, SessionTable, TotalsLine } from '../components/RecordViews';
import { dateTime, dollars, integer } from '../format';

function ProjectPanel({id,filter,onClose}:{id:string;filter:ListFilter;onClose():void}){
 const [page,setPage]=useState({page:1,limit:25}),[session,setSession]=useState<string>();
 const query=useQuery({queryKey:['projects','detail',id,filter,page],queryFn:({signal})=>getProject(id,{...filter,...page},signal)});
 return <Card className="section-card" title="项目详情" extra={<Button onClick={onClose}>关闭详情</Button>}>{query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<>
  <Typography.Title level={3}>{query.data.project.name}</Typography.Title><Typography.Paragraph type="secondary">显式关联成员 {query.data.project.members.length} 个 · 同名不会自动合并</Typography.Paragraph><TotalsLine totals={query.data.project.totals} /><RecordTrend rows={query.data.trend} />
  <Typography.Title level={4}>模型贡献</Typography.Title><Table rowKey="key" size="small" dataSource={query.data.models} pagination={{pageSize:10,showSizeChanger:false}} columns={[{title:'模型',dataIndex:'name'},{title:'Token',align:'right',render:(_,r)=>integer(r.totals.total_tokens)},{title:'API 等价成本',align:'right',render:(_,r)=>dollars(r.totals.cost_micro_usd)}]} />
  <Typography.Title level={4}>会话贡献</Typography.Title><Typography.Paragraph type="secondary">会话与项目采用各自历史舍入口径，金额可能有微美元差异。</Typography.Paragraph><SessionTable rows={query.data.sessions.items} page={query.data.sessions.page} loading={query.isFetching} zone={filter.time_zone} onPage={(page,limit)=>setPage({page,limit})} onOpen={setSession} /><CoverageNotice coverage={query.data.sessions.coverage} zone={filter.time_zone} />
  {session&&<SessionPanel key={session} id={session} filter={filter} onClose={()=>setSession(undefined)} />}
 </>}</Card>;
}
export default function Projects(){
 const [filter,setFilter]=useState(initialListFilter),[detail,setDetail]=useState<string>(),[selected,setSelected]=useState<ProjectRecord[]>([]),[operation,setOperation]=useState<'link'|'unlink'>(),[target,setTarget]=useState('');
 const client=useQueryClient();
 const query=useQuery({queryKey:['projects','list',filter],queryFn:({signal})=>getProjects(filter,signal)});
 const ids=[...new Set(selected.flatMap(r=>r.members))];
 const mutation=useMutation({mutationFn:()=>associateProjects(ids,operation==='link'?target:''),onSuccess:async()=>{setOperation(undefined);setSelected([]);setDetail(undefined);await Promise.all([client.invalidateQueries({queryKey:['projects']}),client.invalidateQueries({queryKey:['sessions']}),client.invalidateQueries({queryKey:['statistics']})]);}});
 function change(v:ListFilter){setFilter(v);setDetail(undefined);if(v.search!==filter.search||v.model!==filter.model||v.provider!==filter.provider||v.client_id!==filter.client_id||v.start_date!==filter.start_date||v.end_date_exclusive!==filter.end_date_exclusive||v.time_zone!==filter.time_zone)setSelected([]);}
 function open(mode:'link'|'unlink'){mutation.reset();setTarget(selected[0]?.members[0]??'');setOperation(mode);}
 return <section><div className="page-heading"><div><span className="eyebrow">PROJECTS</span><Typography.Title level={2}>项目</Typography.Title><Typography.Paragraph type="secondary">按来源保留项目身份。跨机器的同一项目可显式关联，名称相同不会自动合并。</Typography.Paragraph></div></div><ListFilters value={filter} onChange={change} refresh={()=>void query.refetch()} busy={query.isFetching} />
 <div className="association-bar"><span>已选 {selected.length} 个项目组 / {ids.length} 个成员</span><Button disabled={selected.length<2||ids.length>100} onClick={()=>open('link')}>关联所选项目</Button><Button disabled={!ids.length||ids.length>100} onClick={()=>open('unlink')}>解除所选关联</Button>{ids.length>100&&<span role="status">一次最多选择 100 个项目成员</span>}</div>
 {query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<><TotalsLine totals={query.data.totals} /><Table<ProjectRecord> rowKey="id" dataSource={query.data.items} loading={query.isFetching} rowSelection={{selectedRowKeys:selected.map(r=>r.id),preserveSelectedRowKeys:true,getCheckboxProps:r=>({'aria-label':`选择项目 ${r.name} ${r.id.slice(0,12)}`}),getTitleCheckboxProps:()=>({'aria-label':'选择本页项目'}),onChange:keys=>{const known=new Map([...selected,...query.data.items].map(r=>[r.id,r]));setSelected(keys.map(key=>known.get(String(key))).filter((r):r is ProjectRecord=>r!==undefined));}}} scroll={{x:900}} pagination={{current:query.data.page.page,pageSize:query.data.page.limit,total:query.data.page.total,showSizeChanger:true,pageSizeOptions:[10,25,50,100],onChange:(page,limit)=>change({...filter,page,limit}),showTotal:total=>`共 ${integer(total)} 条`}} columns={[
  {title:'项目',render:(_,r)=><div><Button type="link" className="record-link" onClick={()=>setDetail(r.id)}>{r.name||'未命名项目'}</Button><div className="record-id">{r.id.slice(0,12)} · {r.members.length} 个成员</div></div>},
  {title:'Token',align:'right',render:(_,r)=>integer(r.totals.total_tokens)},{title:'API 等价成本',align:'right',render:(_,r)=><span>{dollars(r.totals.cost_micro_usd)}{r.totals.cost_status==='partial'?'（小计）':''}</span>},{title:'会话',align:'right',render:(_,r)=>integer(r.totals.sessions)},{title:'最近活动',render:(_,r)=>dateTime(r.last_active_at_ms,filter.time_zone)},{title:'状态',render:(_,r)=>r.conflict?<Tag color="red">来源冲突</Tag>:<Tag>已收到事实</Tag>},
 ]} /><CoverageNotice coverage={query.data.coverage} zone={filter.time_zone} /></>}
 {detail&&<ProjectPanel key={detail} id={detail} filter={filter} onClose={()=>setDetail(undefined)} />}
 <Modal title={operation==='link'?'确认跨机器项目关联':'确认解除项目关联'} open={!!operation} onCancel={()=>!mutation.isPending&&setOperation(undefined)} onOk={()=>mutation.mutate()} confirmLoading={mutation.isPending} okButtonProps={{disabled:operation==='link'&&!target}} okText="确认执行" cancelText="取消" closable={{'aria-label':'关闭项目关联确认'}}>
  <Typography.Paragraph>{operation==='link'?'所选成员将加入目标项目组。来源上报不会覆盖这个显式关系，后续可以解除。':'所选成员将各自恢复独立项目身份；原始统计事实保留。'}</Typography.Paragraph><ul>{selected.map(r=><li key={r.id}>{r.name} · {r.id.slice(0,12)} · {r.members.length} 个成员</li>)}</ul>
  {operation==='link'&&<Select aria-label="目标项目组" style={{width:'100%'}} value={target} onChange={setTarget} options={selected.map(r=>({value:r.members[0],label:`${r.name} · ${r.id.slice(0,12)}`}))} />}
  {mutation.error&&<Alert className="section-card" type="error" showIcon title="项目关联未完成" description={mutation.error.message} />}
 </Modal></section>;
}
