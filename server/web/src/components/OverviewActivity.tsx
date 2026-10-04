import {lazy,Suspense,useMemo,useState} from 'react';
import {Alert,Button,Card,Empty,Segmented,Table,Tooltip,theme} from 'antd';
import {useQuery} from '@tanstack/react-query';
import {Link} from 'react-router-dom';
import type {EChartsCoreOption} from 'echarts/core';
import {getActivity,getTopSessions,getSourceUsage,statsParams,type CollectorUsage,type StatsFilter,type Summary,type Activity,type TopSessions} from '../api/statistics';
import {dayjs,dateTime,integer,tokens,tokenAxis,coordinate} from '../format';
import {sourceNames} from './RecordViews';
import {ErrorState,LoadingState} from './QueryState';
import {decimalSorter} from './sorting';
import {CacheNotice} from './CacheNotice';
const Chart=lazy(()=>import('./Chart'));
export function sessionsTarget(filter:StatsFilter,id?:string){const params=new URLSearchParams({...statsParams(filter),sort:'tokens',direction:'desc',...(id?{selected:id}:{})});return `/sessions?${params}`;}
function ActivityCard({data,filter,onDay}:{data:Activity;filter:StatsFilter;onDay(date:string):void}){
 const [metric,setMetric]=useState<'tokens'|'sessions'>('tokens');const {token}=theme.useToken();
 const hourly=data.activity_granularity==='hour';
 const rows=data.activity_timeline;
 const option=useMemo<EChartsCoreOption>(()=>({tooltip:{renderMode:'richText',confine:true,trigger:'axis',formatter:(params:unknown)=>{const r=rows[(params as {dataIndex:number}[])[0]?.dataIndex];if(!r)return '';return `${dayjs(r.start_at_ms).tz(filter.time_zone).format('MM-DD HH:mm Z')} – ${dayjs(r.end_at_ms).tz(filter.time_zone).format('HH:mm Z')}\n${metric==='tokens'?`${tokens(r.tokens)} Token`:`${integer(r.sessions)} 个会话`}`;}},grid:{left:12,right:12,top:24,bottom:28,containLabel:true},xAxis:{type:'category',data:rows.map(r=>dayjs(r.start_at_ms).tz(filter.time_zone).format(hourly?'HH:mm':'MM-DD')),axisTick:{show:false},axisLine:{show:false}},yAxis:{type:'value',axisLabel:{formatter:metric==='tokens'?tokenAxis:undefined}},series:[{type:'bar',barMaxWidth:32,data:rows.map(r=>({value:coordinate(metric==='tokens'?r.tokens:r.sessions),itemStyle:{color:r.start_at_ms>Date.now()?token.colorFillSecondary:token.colorPrimaryBgHover}}))}]}),[rows,metric,hourly,filter.time_zone,token.colorFillSecondary,token.colorPrimaryBgHover]);
 const known=data.weekday_hours.flatMap(r=>{const v=metric==='tokens'?r.tokens:r.session_count;return v===null?[]:[Number(v)];});const max=Math.max(1,...known);
 return <Card title="活动分布" extra={<Segmented aria-label="活动分布指标" size="small" value={metric} onChange={v=>setMetric(v as typeof metric)} options={[{value:'tokens',label:'Token 消耗'},{value:'sessions',label:'会话数量'}]}/>}> <div className="metric-note">{filter.start_date} · {hourly?'按小时':'按天，点击日期查看小时'} · {filter.time_zone}</div>
 <Suspense fallback={<LoadingState/>}><Chart option={option} label={hourly?'每小时用量':'每日用量，点击查看小时'} height={240} onClick={hourly?undefined:index=>{const r=rows[index];if(r)onDay(dayjs(r.start_at_ms).tz(filter.time_zone).format('YYYY-MM-DD'));}}/></Suspense>
 <div className="weekday-title"><strong>星期与小时分布</strong><span className="metric-note">未知格子留灰 · 会话按格去重</span></div>
 <div className="weekday-scroll"><div className="weekday-heatmap"><span/>{Array.from({length:24},(_,hour)=><span className="weekday-hour" key={`h:${hour}`}>{hour%3===0?hour:''}</span>)}{[1,2,3,4,5,6,0].flatMap((weekday,index)=>[<span key={`d:${weekday}`} className="weekday-name">{['周一','周二','周三','周四','周五','周六','周日'][index]}</span>,...Array.from({length:24},(_,hour)=>{const row=data.weekday_hours.find(r=>r.weekday===weekday&&r.hour===hour),value=!row?null:metric==='tokens'?row.tokens:row.session_count;const label=`${['周一','周二','周三','周四','周五','周六','周日'][index]} ${hour}时 · ${value===null?'未取得':metric==='tokens'?`${tokens(value)} Token`:`${integer(value)} 个会话`}`;return <Tooltip key={`${weekday}:${hour}`} title={label}><span tabIndex={0} aria-label={label} className={`activity-cell weekday-cell ${value===null?'unknown':Number(value)===0?'none':`level-${1+Math.min(3,Math.floor(Number(value)/max*4))}`}`}/></Tooltip>;})])}</div></div><div className="weekday-legend" aria-label="星期小时热力图色阶"><span className="activity-cell unknown"/>未知<span>少</span>{['none','level-1','level-2','level-3','level-4'].map(level=><span key={level} className={`activity-cell ${level}`}/>)}多</div></Card>;
}
function TopCard({data,filter}:{data:TopSessions;filter:StatsFilter}){
 return <Card title="高消耗会话"><div className="metric-note">所选范围内的 Token · 前 5 个会话</div>{data.top_sessions.length?<ol className="top-sessions">{data.top_sessions.map(r=><li key={r.id}><Link to={sessionsTarget(filter,r.id)}><span className="top-session-name">{r.title||'未命名会话'}<small><Tooltip title={`${r.project_name||'未归因项目'} · ${sourceNames(r.sources)}`}><span className="top-session-meta">{r.project_name||'未归因项目'} · {sourceNames(r.sources)}</span></Tooltip></small></span><strong>{tokens(r.totals.total_tokens)} Token</strong></Link></li>)}</ol>:<Empty description="当前范围暂无高消耗会话" image={Empty.PRESENTED_IMAGE_SIMPLE}/>}<Link to={sessionsTarget(filter)}>查看全部会话 →</Link></Card>;
}
export function MachineUsage({filter,onSelect}:{filter:StatsFilter;onSelect(client:string):void}){
 const query=useQuery({queryKey:['statistics','source-usage',filter],queryFn:({signal})=>getSourceUsage(filter,signal),refetchInterval:60_000});
 return <Card title="各机器采集的 Codex 用量" className="section-card" extra={<Button size="small" onClick={()=>void query.refetch()} loading={query.isFetching}>刷新</Button>}><div className="metric-note">按机器采集副本统计，机器之间可能重复；各行不能相加成全局用量。</div>{query.isPending?<LoadingState/>:query.error&&!query.data?<ErrorState error={query.error} retry={()=>void query.refetch()}/>:query.data&&<><CacheNotice cache={query.data.cache} zone={filter.time_zone}/>{query.error&&<Alert type="warning" title="刷新失败，保留上次采集数据"/>}<Table<CollectorUsage> rowKey={r=>r.machine.client_id} size="small" dataSource={query.data.items} pagination={{pageSize:10,showSizeChanger:false}} scroll={{x:780}} columns={[
 {title:'机器',render:(_,r)=><Button type="link" onClick={()=>onSelect(r.machine.client_id)}>{r.machine.client_name||r.machine.client_id}</Button>},
 {title:'Token 总量',align:'right',...decimalSorter<CollectorUsage>(r=>r.totals.total_tokens,true),render:(_,r)=>tokens(r.totals.total_tokens)},
 {title:'输入 Token',align:'right',...decimalSorter<CollectorUsage>(r=>r.totals.input_tokens),render:(_,r)=>tokens(r.totals.input_tokens)},
 {title:'输出 Token',align:'right',...decimalSorter<CollectorUsage>(r=>r.totals.output_tokens),render:(_,r)=>tokens(r.totals.output_tokens)},
 {title:'活跃会话',align:'right',...decimalSorter<CollectorUsage>(r=>r.totals.total_tokens===null&&r.totals.sessions===0?null:String(r.totals.sessions)),render:(_,r)=>integer(r.totals.total_tokens===null&&r.totals.sessions===0?null:r.totals.sessions)},
 {title:'采集截至',...decimalSorter<CollectorUsage>(r=>r.coverage.collected_at_ms?.toString()??null),render:(_,r)=>dateTime(r.coverage.collected_at_ms,filter.time_zone)},
 {title:'状态',render:(_,r)=>r.revoked_at_ms?'已撤销':r.coverage.state==='unknown'?'未取得':r.coverage.stale?'采集陈旧':'已收到 / 覆盖未确认'},
 ]}/></>}</Card>;
}

export function OverviewActivity({data,filter,onDay}:{data:Summary;filter:StatsFilter;onDay(date:string):void}){return <div className="overview-activity-grid"><ActivityCard data={data} filter={filter} onDay={onDay}/><TopCard data={data} filter={filter}/></div>;}
function ActivityQuery({filter,onDay}:{filter:StatsFilter;onDay(date:string):void}){
 const query=useQuery({queryKey:['statistics','activity',filter],queryFn:({signal})=>getActivity(filter,signal),refetchInterval:60_000});
 return <section>{query.isPending?<Card title="活动分布"><LoadingState/></Card>:!query.data?<Card title="活动分布"><ErrorState error={query.error} retry={()=>void query.refetch()}/></Card>:<><CacheNotice cache={query.data.cache} zone={filter.time_zone}/>{query.error&&<Alert type="warning" title="活动分布刷新失败，保留上次数据"/>}<ActivityCard data={query.data} filter={filter} onDay={onDay}/></>}</section>;
}
function TopQuery({filter}:{filter:StatsFilter}){
 const query=useQuery({queryKey:['statistics','top-sessions',filter],queryFn:({signal})=>getTopSessions(filter,signal),refetchInterval:60_000});
 return <section>{query.isPending?<Card title="高消耗会话"><LoadingState/></Card>:!query.data?<Card title="高消耗会话"><ErrorState error={query.error} retry={()=>void query.refetch()}/></Card>:<><CacheNotice cache={query.data.cache} zone={filter.time_zone}/>{query.error&&<Alert type="warning" title="高消耗会话刷新失败，保留上次数据"/>}<TopCard data={query.data} filter={filter}/></>}</section>;
}
export function IndependentOverviewActivity({filter,onDay}:{filter:StatsFilter;onDay(date:string):void}){return <div className="overview-activity-grid"><ActivityQuery filter={filter} onDay={onDay}/><TopQuery filter={filter}/></div>;}
