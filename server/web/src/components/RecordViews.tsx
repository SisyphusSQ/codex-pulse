import { lazy, Suspense, useMemo } from 'react';
import { Button, Card, Table, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import type { EChartsCoreOption } from 'echarts/core';
import { getSession, type Page, type SessionRecord } from '../api/records';
import type { Day, StatsFilter, Totals } from '../api/statistics';
import { coordinate, dateTime, dollars, integer, providerNames } from '../format';
import { CoverageNotice } from './CoverageNotice';
import { ErrorState, LoadingState } from './QueryState';

const Chart=lazy(()=>import('./Chart'));
export function TotalsLine({ totals }: { totals:Totals }) {return <div className="totals-line"><span>当前筛选范围 · 已收到 Token <strong>{integer(totals.total_tokens)}</strong></span><span>API 等价成本 <strong>{dollars(totals.cost_micro_usd)}</strong>{totals.cost_status==='partial'?'（已知小计）':''}</span><span>会话 {integer(totals.sessions)}</span></div>;}
export function RecordTrend({ rows }: { rows:Day[] }){
 const option=useMemo<EChartsCoreOption>(()=>({tooltip:{renderMode:'richText',confine:true,trigger:'axis',formatter:(params:unknown)=>{const p=(params as {dataIndex:number}[])[0],r=rows[p?.dataIndex];return r?`${r.date}\nToken：${integer(r.totals.total_tokens)}`:'';}},grid:{left:12,right:18,top:24,bottom:24,containLabel:true},xAxis:{type:'category',data:rows.map(r=>r.date)},yAxis:{type:'value',name:'Token'},series:[{type:'bar',data:rows.map(r=>coordinate(r.totals.total_tokens)),itemStyle:{color:'#137b59'}}]}),[rows]);
 return <Suspense fallback={<LoadingState label="正在加载趋势…" />}><Chart option={option} label="所选记录的自然日 Token 趋势" height={240} /></Suspense>;
}
export function SessionTable({ rows,page,loading,zone,onPage,onOpen }: { rows:SessionRecord[];page:Page;loading:boolean;zone:string;onPage(page:number,limit:number):void;onOpen(id:string):void }){
 return <Table<SessionRecord> rowKey="id" dataSource={rows} loading={loading} scroll={{x:1050}} pagination={{current:page.page,pageSize:page.limit,total:page.total,showSizeChanger:true,pageSizeOptions:[10,25,50,100],showTotal:total=>`共 ${integer(total)} 条`,onChange:onPage}} columns={[
  {title:'会话 / 原始 Session ID',key:'title',render:(_,r)=><div><Button type="link" className="record-link" onClick={()=>onOpen(r.id)}>{r.title||'未命名会话'}</Button><div className="record-id">{r.session_id??'未关联会话的用量'}</div></div>},
  {title:'Provider',dataIndex:'provider',render:(p:string)=>providerNames[p]??p},
  {title:'项目',dataIndex:'project_name'},
  {title:'Token',align:'right',render:(_,r)=>integer(r.totals.total_tokens)},
  {title:'API 等价成本',align:'right',render:(_,r)=><span>{dollars(r.totals.cost_micro_usd)}{r.totals.cost_status==='partial'?'（小计）':''}</span>},
  {title:'最近活动',render:(_,r)=>dateTime(r.last_active_at_ms,zone)},
  {title:'事实状态',render:(_,r)=><><Tag color={r.complete?'green':'gold'}>{r.complete?'来源快照完整':'来源快照部分'}</Tag>{r.conflict&&<Tag color="red">来源冲突</Tag>}<div className="metric-note">采集来源 {r.sources.length} 个 · 执行归属未知</div></>},
 ]} />;
}
export function SessionPanel({ id,filter,onClose }: { id:string;filter:StatsFilter;onClose():void }){
 const query=useQuery({queryKey:['sessions','detail',id,filter],queryFn:({signal})=>getSession(id,filter,signal)});
 const data=query.data;
 return <Card className="section-card" title="会话详情" extra={<Button onClick={onClose}>关闭详情</Button>}>{query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:data&&<>
  <Typography.Title level={3}>{data.session.title||'未命名会话'}</Typography.Title><dl className="metadata-list"><dt>原始 Session ID</dt><dd>{data.session.session_id??'未关联会话'}</dd><dt>Provider / 项目</dt><dd>{providerNames[data.session.provider]??data.session.provider} · {data.session.project_name}</dd><dt>创建 / 最近活动</dt><dd>{dateTime(data.session.created_at_ms,filter.time_zone)} / {dateTime(data.session.last_active_at_ms,filter.time_zone)}</dd><dt>采集截至</dt><dd>{dateTime(data.session.collected_at_ms,filter.time_zone)}</dd></dl>
  {data.session.conflict&&<Tag color="red">来源存在冲突，统计采用服务端已接受事实</Tag>}
  <TotalsLine totals={data.session.totals} /><CoverageNotice coverage={data.coverage} zone={filter.time_zone} /><RecordTrend rows={data.trend} />
  <Typography.Title level={4}>采集来源</Typography.Title><Table size="small" rowKey="id" dataSource={data.session.sources} pagination={false} scroll={{x:640}} columns={[{title:'设备',dataIndex:'client_name'},{title:'采集截至',render:(_,r)=>dateTime(r.collected_at_ms,filter.time_zone)},{title:'来源 / 修订',render:(_,r)=>`${r.source_kind} / ${r.revision}`},{title:'状态',render:(_,r)=>r.deleted?'已停止提供':r.complete?'快照完整':'部分快照'}]} />
  <Typography.Title level={4}>工具与技能</Typography.Title><Table size="small" rowKey="key" pagination={{pageSize:10,showSizeChanger:false}} dataSource={[...data.tools.map(r=>({...r,key:`tool:${r.key}`,kind:'工具'})),...data.skills.map(r=>({...r,key:`skill:${r.key}`,kind:'技能'}))]} columns={[{title:'类型',dataIndex:'kind'},{title:'名称',dataIndex:'name'},{title:'调用次数',render:(_,r)=>integer(r.totals.invocations)}]} />
 </>}</Card>;
}
