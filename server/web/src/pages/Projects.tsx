import { useLayoutEffect, useRef, useState } from 'react';
import { Alert, Button, Modal, Select, Table, Tabs, Typography } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { associateProjects, getProject, getProjects, type ListFilter, type ProjectRecord } from '../api/records';
import { CoverageNotice } from '../components/CoverageNotice';
import { initialListFilter, ListFilters, RecordSearchControls, sameRecordScope } from '../components/ListFilters';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { RecordTrend, SessionPanel, SessionTable, TotalsLine } from '../components/RecordViews';
import { RecordWorkspace } from '../components/RecordWorkspace';
import { dateTime, dollars, integer, tokens } from '../format';

function ProjectPanel({id,filter,onClose}:{id:string;filter:ListFilter;onClose():void}){
 const [page,setPage]=useState({page:1,limit:25}),[session,setSession]=useState<string>();
 const [activeTab,setActiveTab]=useState('sessions');
 const body=useRef<HTMLDivElement>(null),savedScroll=useRef(0);
 useLayoutEffect(()=>{if(!session&&body.current)body.current.scrollTop=savedScroll.current;},[session]);
 function openSession(id:string){savedScroll.current=body.current?.scrollTop??0;setSession(id);}
 const query=useQuery({queryKey:['projects','detail',id,filter,page],queryFn:({signal})=>getProject(id,{...filter,...page},signal)});
 return <div className="project-detail-stack"><div className="record-detail-view" hidden={!!session}><header className="record-detail-header"><Button className="record-back" aria-label="返回项目列表" icon={<ArrowLeftOutlined />} onClick={onClose}>返回项目列表</Button><Typography.Text type="secondary">项目详情</Typography.Text></header><div ref={body} className="record-detail-body">{query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<>
  <Typography.Title level={4}>{query.data.project.name}</Typography.Title><Typography.Paragraph type="secondary">显式关联成员 {query.data.project.members.length} 个 · 同名不会自动合并</Typography.Paragraph><TotalsLine totals={query.data.project.totals} />
  <Tabs activeKey={activeTab} onChange={setActiveTab} items={[
   {key:'sessions',label:'会话贡献',children:<><Typography.Paragraph type="secondary">会话与项目采用各自历史舍入口径，金额可能有微美元差异。</Typography.Paragraph><SessionTable compact rows={query.data.sessions.items} page={query.data.sessions.page} loading={query.isFetching} zone={filter.time_zone} onPage={(page,limit)=>setPage({page,limit})} onOpen={openSession} /><CoverageNotice coverage={query.data.sessions.coverage} zone={filter.time_zone} /></>},
   {key:'trend',label:'用量趋势',children:<RecordTrend rows={query.data.trend} />},
   {key:'models',label:'模型贡献',children:<Table rowKey="key" size="small" dataSource={query.data.models} pagination={{pageSize:10,showSizeChanger:false}} columns={[{title:'模型',dataIndex:'name'},{title:'Token',align:'right',render:(_,r)=>tokens(r.totals.total_tokens)},{title:'API 等价成本',align:'right',render:(_,r)=>dollars(r.totals.cost_micro_usd)}]} />},
  ]} />
 </>}</div></div>
 {session&&<SessionPanel key={session} id={session} filter={filter} projectName={query.data?.project.name} backLabel="返回项目" onClose={()=>setSession(undefined)} />}
 </div>;
}
export default function Projects(){
 const [filter,setFilter]=useState(initialListFilter),[detail,setDetail]=useState<string>(),[selected,setSelected]=useState<ProjectRecord[]>([]),[operation,setOperation]=useState<'link'|'unlink'>(),[target,setTarget]=useState('');
 const client=useQueryClient();
 const query=useQuery({queryKey:['projects','list',filter],queryFn:({signal})=>getProjects(filter,signal)});
 const ids=[...new Set(selected.flatMap(r=>r.members))];
 const mutation=useMutation({mutationFn:()=>associateProjects(ids,operation==='link'?target:''),onSuccess:async()=>{setOperation(undefined);setSelected([]);setDetail(undefined);await Promise.all([client.invalidateQueries({queryKey:['projects']}),client.invalidateQueries({queryKey:['sessions']}),client.invalidateQueries({queryKey:['statistics']})]);}});
 function change(v:ListFilter){setFilter(v);if(!sameRecordScope(v,filter)){setDetail(undefined);setSelected([]);}}
 function open(mode:'link'|'unlink'){mutation.reset();setTarget(selected[0]?.members[0]??'');setOperation(mode);}
 return <section><ListFilters value={filter} onChange={change} refresh={()=>void query.refetch()} busy={query.isFetching} />
 <div className="association-bar"><span>已选 {selected.length} 个项目组 / {ids.length} 个成员</span><Button disabled={selected.length<2||ids.length>100} onClick={()=>open('link')}>关联所选项目</Button><Button disabled={!ids.length||ids.length>100} onClick={()=>open('unlink')}>解除所选关联</Button>{ids.length>100&&<span role="status">一次最多选择 100 个项目成员</span>}</div>
 {query.data&&<TotalsLine totals={query.data.totals} />}
 <RecordWorkspace selectedId={detail} listLabel="项目列表" detailLabel="项目详情" list={<>
 <RecordSearchControls value={filter} onChange={change} /><div className="record-list-heading">项目记录{query.data?` · 共 ${integer(query.data.page.total)} 个`:''}</div>
 {query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<><Table<ProjectRecord> size="small" rowKey="id" dataSource={query.data.items} loading={query.isFetching} rowClassName={r=>r.id===detail?'selected-record':''} rowSelection={{selectedRowKeys:selected.map(r=>r.id),preserveSelectedRowKeys:true,getCheckboxProps:r=>({'aria-label':`选择项目 ${r.name} ${r.id.slice(0,12)}`}),getTitleCheckboxProps:()=>({'aria-label':'选择本页项目'}),onChange:keys=>{const known=new Map([...selected,...query.data.items].map(r=>[r.id,r]));setSelected(keys.map(key=>known.get(String(key))).filter((r):r is ProjectRecord=>r!==undefined));}}} pagination={{current:query.data.page.page,pageSize:query.data.page.limit,total:query.data.page.total,showSizeChanger:true,pageSizeOptions:[10,25,50,100],simple:true,size:'small',onChange:(page,limit)=>change({...filter,page,limit})}} columns={[
  {key:'project',title:'项目',render:(_,r)=><div className="record-list-row"><Button type="link" className="record-link" aria-pressed={r.id===detail} onClick={()=>setDetail(r.id)}>{r.name||'未命名项目'}</Button><div className="record-id">{r.id.slice(0,12)} · {r.members.length} 个成员</div><div className="record-row-metrics"><span>{tokens(r.totals.total_tokens)} Token</span><span>{dollars(r.totals.cost_micro_usd)}{r.totals.cost_status==='partial'?'（小计）':''}</span><span>{integer(r.totals.sessions)} 个会话</span></div><div className="metric-note">{dateTime(r.last_active_at_ms,filter.time_zone)} · {r.conflict?'来源冲突':'已收到事实'}</div></div>},
 ]} /><CoverageNotice coverage={query.data.coverage} zone={filter.time_zone} /></>}
 </>} detail={detail?<ProjectPanel key={detail} id={detail} filter={{...filter,page:1,limit:25}} onClose={()=>setDetail(undefined)} />:<EmptyState description="选择一个项目，查看汇总、趋势和会话贡献。" />} />
 <Modal title={operation==='link'?'确认跨机器项目关联':'确认解除项目关联'} open={!!operation} onCancel={()=>!mutation.isPending&&setOperation(undefined)} onOk={()=>mutation.mutate()} confirmLoading={mutation.isPending} okButtonProps={{disabled:operation==='link'&&!target}} okText="确认执行" cancelText="取消" closable={{'aria-label':'关闭项目关联确认'}}>
  <Typography.Paragraph>{operation==='link'?'所选成员将加入目标项目组。来源上报不会覆盖这个显式关系，后续可以解除。':'所选成员将各自恢复独立项目身份；原始统计事实保留。'}</Typography.Paragraph><ul>{selected.map(r=><li key={r.id}>{r.name} · {r.id.slice(0,12)} · {r.members.length} 个成员</li>)}</ul>
  {operation==='link'&&<Select aria-label="目标项目组" style={{width:'100%'}} value={target} onChange={setTarget} options={selected.map(r=>({value:r.members[0],label:`${r.name} · ${r.id.slice(0,12)}`}))} />}
  {mutation.error&&<Alert className="section-card" type="error" showIcon title="项目关联未完成" description={mutation.error.message} />}
 </Modal></section>;
}
