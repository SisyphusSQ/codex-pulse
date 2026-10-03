import { lazy, Suspense, useMemo, useState } from 'react';
import { Button, Descriptions, Table, Tabs, Tag, Typography, theme } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import type { EChartsCoreOption } from 'echarts/core';
import { getSession, type Page, type SessionRecord } from '../api/records';
import type { Day, StatsFilter, Totals } from '../api/statistics';
import { coordinate, dateTime, dollars, integer, tokenAxis, tokens, providerNames } from '../format';
import { CoverageNotice } from './CoverageNotice';
import { ErrorState, LoadingState } from './QueryState';
import { ThroughputCell, ThroughputPanel } from './Throughput';
import { CacheHitRateCell, CacheHitRateDetail } from './CacheHitRate';

const Chart=lazy(()=>import('./Chart'));
export function TotalsLine({ totals }: { totals:Totals }) {return <div className="totals-line"><span>当前筛选范围 · 已收到 Token <strong>{tokens(totals.total_tokens)}</strong></span><span>API 等价成本 <strong>{dollars(totals.cost_micro_usd)}</strong>{totals.cost_status==='partial'?'（已知小计）':''}</span><span>会话 {integer(totals.sessions)}</span></div>;}
export function RecordTrend({ rows }: { rows:Day[] }){
 const {token}=theme.useToken();
 const option=useMemo<EChartsCoreOption>(()=>({tooltip:{renderMode:'richText',confine:true,trigger:'axis',axisPointer:{type:'shadow'},formatter:(params:unknown)=>{const p=(params as {dataIndex:number}[])[0],r=rows[p?.dataIndex];return r?`${r.date}\nToken：${tokens(r.totals.total_tokens)}`:'';}},grid:{left:12,right:18,top:24,bottom:24,containLabel:true},xAxis:{type:'category',data:rows.map(r=>r.date),axisLabel:{formatter:(value:string)=>value.slice(5)},axisLine:{show:false},axisTick:{show:false}},yAxis:{type:'value',name:'Token',axisLabel:{formatter:tokenAxis}},series:[{type:'bar',barMaxWidth:32,data:rows.map(r=>coordinate(r.totals.total_tokens)),itemStyle:{color:token.colorPrimary}}]}),[rows,token.colorPrimary]);
 return <Suspense fallback={<LoadingState label="正在加载趋势…" />}><Chart option={option} label="所选记录的自然日 Token 趋势" height={240} /></Suspense>;
}
export function SessionTable({ rows,page,loading,zone,onPage,onOpen,compact=false,selectedId }: { rows:SessionRecord[];page:Page;loading:boolean;zone:string;onPage(page:number,limit:number):void;onOpen(id:string):void;compact?:boolean;selectedId?:string }){
 return <Table<SessionRecord> rowKey="id" dataSource={rows} loading={loading} size={compact?'small':'middle'} scroll={compact?undefined:{x:1360}} showHeader={!compact} rowClassName={r=>r.id===selectedId?'selected-record':''} pagination={{current:page.page,pageSize:page.limit,total:page.total,showSizeChanger:true,pageSizeOptions:[10,25,50,100],simple:compact,size:compact?'small':undefined,showTotal:compact?undefined:total=>`共 ${integer(total)} 条`,onChange:onPage}} columns={compact?[
  {key:'session',render:(_,r)=><div className="record-list-row"><Button type="link" className="record-link" aria-pressed={r.id===selectedId} onClick={()=>onOpen(r.id)}>{r.title||'未命名会话'}</Button><div className="record-id">{r.session_id??'未关联会话的用量'}</div><div className="metric-note">{providerNames[r.provider]??r.provider} · {r.project_name||'未归因项目'}</div><div className="record-row-metrics"><span>{tokens(r.totals.total_tokens)} Token</span><span>{dollars(r.totals.cost_micro_usd)}{r.totals.cost_status==='partial'?'（小计）':''}</span></div><div className="record-row-metrics"><span>缓存 <CacheHitRateCell value={r.cache_hit_rate} /></span><span><ThroughputCell value={r.throughput} /></span></div><div className="metric-note">{dateTime(r.last_active_at_ms,zone)} · {r.complete?'来源快照完整':'来源快照部分'}{r.conflict?' · 来源冲突':''} · 来源 {r.sources.length} 个 · 执行归属未知</div></div>},
 ]:[
  {title:'会话 / 原始 Session ID',key:'title',width:280,render:(_,r)=><div><Button type="link" className="record-link" onClick={()=>onOpen(r.id)}>{r.title||'未命名会话'}</Button><div className="record-id">{r.session_id??'未关联会话的用量'}</div></div>},
  {title:'Provider',dataIndex:'provider',render:(p:string)=>providerNames[p]??p},
  {title:'项目',dataIndex:'project_name'},
  {title:'Token',align:'right',render:(_,r)=>tokens(r.totals.total_tokens)},
  {title:'缓存命中率',align:'right',render:(_,r)=><CacheHitRateCell value={r.cache_hit_rate} />},
  {title:'平均 TPS',align:'right',render:(_,r)=><ThroughputCell value={r.throughput} />},
  {title:'API 等价成本',align:'right',render:(_,r)=><span>{dollars(r.totals.cost_micro_usd)}{r.totals.cost_status==='partial'?'（小计）':''}</span>},
  {title:'最近活动',render:(_,r)=>dateTime(r.last_active_at_ms,zone)},
  {title:'事实状态',render:(_,r)=><><Tag color={r.complete?'green':'gold'}>{r.complete?'来源快照完整':'来源快照部分'}</Tag>{r.conflict&&<Tag color="red">来源冲突</Tag>}<div className="metric-note">采集来源 {r.sources.length} 个 · 执行归属未知</div></>},
 ]} />;
}
export function SessionPanel({ id,filter,onClose,backLabel='返回会话列表',projectName }: { id:string;filter:StatsFilter;onClose():void;backLabel?:string;projectName?:string }){
 const [turnLimit,setTurnLimit]=useState(20);
 const [activeTab,setActiveTab]=useState('usage');
 const query=useQuery({queryKey:['sessions','detail',id,filter,turnLimit],queryFn:({signal})=>getSession(id,filter,signal,turnLimit)});
 const data=query.data;
 return <div className="record-detail-view"><header className="record-detail-header"><Button className="record-back" aria-label={backLabel} icon={<ArrowLeftOutlined />} onClick={onClose}>{backLabel}</Button><Typography.Text type="secondary">{projectName?`${projectName} → 会话详情`:'会话详情'}</Typography.Text></header><div className="record-detail-body">{query.isPending?<LoadingState />:query.error?<ErrorState error={query.error} retry={()=>void query.refetch()} />:data&&<>
  <Typography.Title level={4}>{data.session.title||'未命名会话'}</Typography.Title>
  <Descriptions size="small" column={{xs:1,sm:2}} items={[
   {key:'session',label:'原始 Session ID',children:<span className="record-id">{data.session.session_id??'未关联会话'}</span>,span:2},
   {key:'provider',label:'Provider',children:providerNames[data.session.provider]??data.session.provider},
   {key:'project',label:'项目',children:data.session.project_name},
   {key:'created',label:'创建时间',children:dateTime(data.session.created_at_ms,filter.time_zone)},
   {key:'active',label:'最近活动',children:dateTime(data.session.last_active_at_ms,filter.time_zone)},
   {key:'collected',label:'采集截至',children:dateTime(data.session.collected_at_ms,filter.time_zone)},
  ]} />
  {data.session.conflict&&<Tag color="red">来源存在冲突，统计采用服务端已接受事实</Tag>}
  <TotalsLine totals={data.session.totals} />
  <Tabs activeKey={activeTab} onChange={setActiveTab} items={[
   {key:'usage',label:'用量与缓存',children:<><CacheHitRateDetail value={data.session.cache_hit_rate} /><RecordTrend rows={data.trend} /><CoverageNotice coverage={data.coverage} zone={filter.time_zone} /></>},
   {key:'tps',label:'输出 TPS',children:<ThroughputPanel value={data.session.throughput} turns={data.throughput_turns} zone={filter.time_zone} onLimit={setTurnLimit} sourceName={data.session.sources.find(s=>s.client_id===data.session.throughput?.source_client_id)?.client_name} />},
   {key:'sources',label:`采集来源 (${data.session.sources.length})`,children:<Table size="small" rowKey="id" dataSource={data.session.sources} pagination={false} scroll={{x:640}} columns={[{title:'设备',dataIndex:'client_name'},{title:'采集截至',render:(_,r)=>dateTime(r.collected_at_ms,filter.time_zone)},{title:'来源 / 修订',render:(_,r)=>`${r.source_kind} / ${r.revision}`},{title:'状态',render:(_,r)=>r.deleted?'已停止提供':r.complete?'快照完整':'部分快照'}]} />},
   {key:'tools',label:'工具与技能',children:<Table size="small" rowKey="key" pagination={{pageSize:10,showSizeChanger:false}} dataSource={[...data.tools.map(r=>({...r,key:`tool:${r.key}`,kind:'工具'})),...data.skills.map(r=>({...r,key:`skill:${r.key}`,kind:'技能'}))]} columns={[{title:'类型',dataIndex:'kind'},{title:'名称',dataIndex:'name'},{title:'调用次数',render:(_,r)=>integer(r.totals.invocations)}]} />},
  ]} />
 </>}</div></div>;
}
