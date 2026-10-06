import { lazy, Suspense, useMemo, useState } from 'react';
import { Button, Card, Collapse, Popover, Select, Segmented, Statistic, Table, Tooltip, theme } from 'antd';
import {useIsFetching, useQueryClient} from '@tanstack/react-query';
import {refreshQueriesWithFeedback, useFeedbackQuery as useQuery} from '../components/QueryNotifications';
import { Link,useSearchParams } from 'react-router-dom';
import type { EChartsCoreOption } from 'echarts/core';
import { getDevices, getAnnual, getTotals, getBreakdown, type StatsFilter, type Decimal, type Slice } from '../api/statistics';
import { getUsage, type Usage } from '../api/usage';
import { usageChartOption } from '../components/ModelTrend';
import { SourceTable } from '../components/SourceTable';
import { IndependentOverviewActivity, MachineUsage, sessionsTarget } from '../components/OverviewActivity';
import { decimalSorter } from '../components/sorting';
import { ActivityHeatmap } from '../components/ActivityHeatmap';
import { UsageCost } from '../components/UsageCost';
import { emptyUsage, usageDecimal, usageTokens, usageDollars, usageInteger } from '../components/usageDisplay';
import { initialFilter, StatsFilters } from '../components/StatsFilters';
import { DeferredSection, SectionPlaceholder } from '../components/DeferredSection';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { dayjs, integer, providerNames } from '../format';

const Chart = lazy(() => import('../components/Chart'));
type Metric = 'tokens' | 'cost';
const metricOptions = [{ label: 'Token', value: 'tokens' }, { label: '费用', value: 'cost' }];
const metricValue = (row: Slice, metric: Metric): Decimal => usageDecimal(row.totals, metric === 'tokens' ? 'total_tokens' : 'cost_micro_usd');
const metricLabel = (value: Decimal, metric: Metric) => metric === 'tokens' ? usageTokens(value) : usageDollars(value);
const metricCoordinate = (value: Decimal, metric: Metric) => value === null ? null : Number(value) / (metric === 'cost' ? 1_000_000 : 1);
const tooltip = { renderMode: 'richText' as const, confine: true };
const palette = ['#2678f5', '#3e9e82', '#8a70cf', '#d3a34b', '#6b8aad', '#c0779b'];
const providerColors: Record<string, string> = { codex: '#2678f5', cursor: '#3e9e82', grok: '#d3a34b', dsh: '#299caa' };

function SliceTable({ rows }: { rows: Slice[] }) {
  return <Table<Slice> size="small" rowKey="key" dataSource={rows} pagination={rows.length > 8 ? { pageSize: 8, showSizeChanger: false } : false} scroll={{ x: 420 }} columns={[
    { title: '名称', dataIndex: 'name', render: (name: string) => providerNames[name] ?? name },
    { title:'会话',align:'right',...decimalSorter<Slice>(r=>r.totals.sessions),render:(_,row)=>integer(row.totals.sessions) },
    { title: 'Token', align: 'right', ...decimalSorter<Slice>(r=>usageDecimal(r.totals,'total_tokens'),true), render: (_, row) => usageTokens(usageDecimal(row.totals,'total_tokens')) },
    { title: 'API 等价成本', align: 'right', ...decimalSorter<Slice>(r=>usageDecimal(r.totals,'cost_micro_usd')), render: (_, row) => <UsageCost value={usageDecimal(row.totals,'cost_micro_usd')} status={row.totals.cost_status}/> },
  ]} />;
}

function Distribution({ rows, title }: { rows: Slice[]; title: string }) {
  const { token } = theme.useToken();
  const [metric, setMetric] = useState<Metric>('tokens');
  // 环图仅用已知正值绘制面积；未知、真零及有符号舍入差额均保留在全部明细。
  const slices = useMemo(() => rows.filter(row => {
    const value = metricValue(row, metric);
    return value !== null && BigInt(value) > 0n;
  }), [rows, metric]);
  const color = (row: Slice, index: number) => row.name==='unknown'?'#95a2b4':providerColors[row.key] ?? palette[index % palette.length];
  const option = useMemo<EChartsCoreOption>(() => ({
    tooltip: { ...tooltip, trigger: 'item', formatter: (params: unknown) => {
      const row = slices[(params as { dataIndex: number }).dataIndex];
      return row ? `${providerNames[row.name] ?? row.name}\n${metricLabel(metricValue(row, metric), metric)}` : '';
    } },
    series: [{ type: 'pie', radius: ['60%', '82%'], center: ['50%', '50%'], padAngle: 1.4, minAngle: 0,
      label: { show: false }, labelLine: { show: false }, itemStyle: { borderRadius: 3 },
      data: slices.map((row, index) => ({ name: providerNames[row.name] ?? row.name, value: metricCoordinate(metricValue(row, metric), metric), itemStyle: { color: color(row, index) } })) }],
  }), [slices, metric]);
  return <Card title={title} className="overview-distribution" extra={<Segmented aria-label={`${title}指标`} size="small" options={metricOptions} value={metric} onChange={value => setMetric(value as Metric)} />}>
    {slices.length ? <div className="distribution-layout">
      <div className="distribution-chart"><Suspense fallback={<LoadingState label="正在加载图表…" />}><Chart option={option} label={`${title}环图`} height={228} /></Suspense><span className="donut-caption" style={{ color: token.colorTextSecondary }}>{metric === 'tokens' ? 'Token' : 'API 等价成本'}</span></div>
      <div className="distribution-legend">{slices.slice(0, 8).map((row, index) => <div className="distribution-legend-row" key={row.key}>
        <span className="distribution-dot" style={{ background: color(row, index) }} />
        <Tooltip title={providerNames[row.name] ?? row.name}><span className="distribution-name">{providerNames[row.name] ?? row.name}</span></Tooltip>
        <span className="numeric">{metricLabel(metricValue(row, metric), metric)}</span>
      </div>)}{slices.length > 8 && <div className="metric-note">另有 {slices.length - 8} 项，环图已包含；见全部明细</div>}</div>
    </div> : <EmptyState description={rows.some(row=>metricValue(row,metric)===null)?metric==='cost'?'暂无计价数据':'暂无用量数据':'暂无用量'} />}
    <Collapse ghost size="small" items={[{ key: 'details', label: `全部明细 (${rows.length})`, children: <SliceTable rows={rows} /> }]} />
  </Card>;
}

function UsageChart({ data, metric, selection }: { data: Usage; metric: Metric; selection: string[] }) {
  const option = useMemo(() => usageChartOption(data, metric, selection, true), [data, metric, selection]);
  return <Chart option={option} label="按自然日的用量趋势" height={280} />;
}


function OverviewUsageTrend({filter}:{filter:StatsFilter}){
 const [metric,setMetric]=useState<Metric>('tokens');
 const [chosen,setChosen]=useState<string[]|null>(null);
 const usage=useQuery({queryKey:['usage',filter],queryFn:({signal})=>getUsage(filter,signal),refetchInterval:60_000});
 const available=usage.data?.models??[];
 const selection=chosen?.filter(k=>available.some(r=>`${r.provider}:${r.model}`===k))??available.slice(0,12).map(r=>`${r.provider}:${r.model}`);
 const coverage=usage.data?.coverage;
 return <>
        <Card className="section-card overview-trend" title={<div className="trend-title"><span>模型用量趋势</span>{coverage && <span className="metric-note">{coverage.stale?'采集陈旧 · ':''}{coverage.collected_at_ms===null?'暂无采集':`更新 ${dayjs(coverage.collected_at_ms).tz(filter.time_zone).format('MM-DD HH:mm')}`}</span>}</div>} extra={<div className="overview-chart-controls">
          <Segmented aria-label="图表指标" size="small" options={metricOptions} value={metric} onChange={value => setMetric(value as Metric)} />
        </div>}>
          <Select aria-label="概览趋势模型" className="usage-model-selector" mode="multiple" maxCount={12} maxTagCount="responsive" value={selection} onChange={setChosen} options={available.map(r=>({value:`${r.provider}:${r.model}`,label:`${providerNames[r.provider]??r.provider} · ${r.model==='unknown'?'模型未归因':r.model}`}))} placeholder="选择趋势模型" />
          {usage.isPending?<LoadingState label="正在读取模型日桶…" />:usage.error&&!usage.data?<ErrorState error={usage.error} retry={()=>void usage.refetch()} />:<>

          {selection.length && usage.data?<UsageChart data={usage.data} metric={metric} selection={selection} />:<EmptyState description="暂无模型用量" />}
          </>}
        </Card></>;
}
function BreakdownQuery({filter,kind,distribution=false}:{filter:StatsFilter;kind:'providers'|'models';distribution?:boolean}){
 const query=useQuery({queryKey:['statistics',kind,filter],queryFn:({signal})=>getBreakdown(filter,kind,signal),refetchInterval:60_000});
 const title=kind==='providers'?'平台分布':'模型分布';
 return query.isPending?<LoadingState label="正在读取用量明细…"/>:!query.data?<ErrorState error={query.error} retry={()=>void query.refetch()}/>:<>{distribution?<Distribution rows={query.data[kind]??[]} title={title}/>:<SliceTable rows={query.data[kind]??[]}/>}</>;
}

export default function Overview() {
  const queryClient = useQueryClient();
  const machineFetching = useIsFetching({ queryKey: ['statistics', 'source-usage'] }) > 0;
  const [params]=useSearchParams();
  const [filter, setFilter] = useState(()=>({...initialFilter([1,7,30,90].includes(Number(params.get('days')))?Number(params.get('days')):1),...Object.fromEntries([...params].filter(([key])=>['start_date','end_date_exclusive','time_zone','provider','client_id'].includes(key)))}));
  const [breakdown,setBreakdown]=useState<'providers'|'models'>('providers');
  const usage=useQuery({queryKey:['statistics','totals',filter],queryFn:({signal})=>getTotals(filter,signal),refetchInterval:60_000});
  const {start_date:_start,end_date_exclusive:_end,...annualFilter}=filter;
  const query=useQuery({queryKey:['statistics','annual',annualFilter],queryFn:({signal})=>getAnnual(filter,signal),refetchInterval:60_000});
  const data=query.data;
  const totals=usage.data?.totals;
  const annualEmpty = data ? data.heatmap_totals ? emptyUsage(data.heatmap_totals) : data.heatmap.every(day=>emptyUsage(day.totals)) && (data.heatmap_activity.total_tokens===null || data.heatmap_activity.total_tokens==='0') : false;
  const annualValue = (value: Decimal) => value ?? (annualEmpty ? '0' : null);
  const annualTokens = data?.heatmap_totals ? usageDecimal(data.heatmap_totals,'total_tokens') : annualValue(data?.heatmap_activity.total_tokens??null);
  const annualCost = data?.heatmap_totals ? usageDecimal(data.heatmap_totals,'cost_micro_usd') : annualValue(null);
  const sources=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
  const devices=(sources.data??[]).filter(d=>d.revoked_at_ms===null&&(!filter.client_id||d.id===filter.client_id)).map(d=>({...d,providers:d.providers.filter(p=>!filter.provider||p.provider===filter.provider)}));
  return <section className="overview-page">
    <StatsFilters value={filter} onChange={v=>{setFilter(v);}} refresh={() => {void refreshQueriesWithFeedback(queryClient,{queryKey:['statistics'],type:'active'});void refreshQueriesWithFeedback(queryClient,{queryKey:['usage'],type:'active'});}} busy={query.isFetching||usage.isFetching||machineFetching} />
    {query.isPending ? <SectionPlaceholder title="全年活动" height={300} /> : query.error && !data ? <Card title="全年活动"><ErrorState error={query.error} retry={() => void query.refetch()} /></Card> : data && <>

      <Card className="overview-activity" title="全年活动" extra={<div className="annual-actions">{data.heatmap_coverage.stale && <span className="metric-note">采集陈旧</span>}<Popover trigger="click" placement="bottomRight" content={<div className="evidence-popover">
        <div className="activity-metrics">{[
          ['近 365 天 Token', data.heatmap_activity.total_tokens],
          ['峰值日 Token', data.heatmap_activity.peak_daily_tokens],
          ['活跃天数', data.heatmap_activity.active_days],
          ['当前连续天数', data.heatmap_activity.current_streak_days],
          ['最长连续天数', data.heatmap_activity.longest_streak_days],
        ].map(([title, value], index) => <div key={title!}><strong>{index<2?usageTokens(annualValue(value)):usageInteger(annualValue(value))}</strong><span>{title}</span></div>)}</div>
        </div>}><Button type="text" size="small">年度活动统计</Button></Popover></div>}>
        <div className="annual-totals" aria-label="年度用量汇总">
          <div><Statistic title="近 365 天 Token 总量" value={annualTokens??'—'} formatter={()=>usageTokens(annualTokens)} /></div>
          <div><Statistic title="近 365 天 API 等价成本" value={annualCost??'—'} formatter={()=><UsageCost value={annualCost} status={data.heatmap_totals?.cost_status}/>} /></div>
          <div><Statistic title="单日 Token 使用峰值" value={annualValue(data.heatmap_activity.peak_daily_tokens)??'—'} formatter={()=>usageTokens(annualValue(data.heatmap_activity.peak_daily_tokens))} /></div>
          <div><Statistic title="最长连续天数" value={annualValue(data.heatmap_activity.longest_streak_days)??'—'} formatter={()=>usageInteger(annualValue(data.heatmap_activity.longest_streak_days))} suffix={annualValue(data.heatmap_activity.longest_streak_days)===null?undefined:'天'} /></div>
          <div><Statistic title="当前连续天数" value={annualValue(data.heatmap_activity.current_streak_days)??'—'} formatter={()=>usageInteger(annualValue(data.heatmap_activity.current_streak_days))} suffix={annualValue(data.heatmap_activity.current_streak_days)===null?undefined:'天'} /></div>
        </div>
        {data.heatmap.length ? <ActivityHeatmap days={data.heatmap} /> : <EmptyState description="年度活动暂时不可用" />}
        <div className="activity-scope"><span>{dayjs(data.heatmap_range.start_at_ms).tz(filter.time_zone).format('YYYY-MM-DD')} 至 {dayjs(data.heatmap_range.end_at_ms).tz(filter.time_zone).subtract(1, 'day').format('YYYY-MM-DD')}</span><span>{filter.time_zone}</span><span>{filter.provider ? providerNames[filter.provider] : '全部客户端'}</span></div>
      </Card>
    </>}
      <Card className="summary-band" style={{ minHeight: 132 }}>

        {!totals && usage.isPending ? <LoadingState label="正在读取范围用量…" /> : usage.error && !totals ? <ErrorState error={usage.error} retry={() => void usage.refetch()} /> : totals && <div className="metric-grid overview-kpis">
        <div><Statistic title="当前范围 Token 总量" value={usageDecimal(totals,'total_tokens')??'—'} formatter={() => usageTokens(usageDecimal(totals,'total_tokens'))} /><div className="metric-note">输入 {usageTokens(usageDecimal(totals,'input_tokens'))} · 输出 {usageTokens(usageDecimal(totals,'output_tokens'))}</div></div>
        <div><Statistic title="当前范围 API 等价成本" value={usageDecimal(totals,'cost_micro_usd')??'—'} formatter={() => <UsageCost value={usageDecimal(totals,'cost_micro_usd')} status={totals.cost_status}/>} /></div>
        <div><Statistic title="活跃会话" value={totals.sessions} formatter={() => integer(totals.sessions)} /></div>
      </div>}</Card>

      <DeferredSection title="活动分布与高消耗会话" height={530}><IndependentOverviewActivity filter={filter} onDay={date=>setFilter({...filter,start_date:date,end_date_exclusive:dayjs(date).add(1,'day').format('YYYY-MM-DD')})}/></DeferredSection>
      <DeferredSection title="各机器采集的 Codex 用量" height={280}><MachineUsage filter={filter} onSelect={client_id=>setFilter({...filter,client_id})}/></DeferredSection>
      <DeferredSection title="模型用量趋势" height={400}>
        <OverviewUsageTrend filter={filter} />
      </DeferredSection>
        <div className="overview-support">
          <DeferredSection title="平台 / 模型明细" height={360}><Card title="平台 / 模型明细" extra={<div className="overview-chart-controls"><Segmented size="small" aria-label="用量明细维度" value={breakdown} options={[{value:'providers',label:'平台'},{value:'models',label:'模型'}]} onChange={value=>setBreakdown(value as 'providers'|'models')} /><Link to="/usage/models">查看用量</Link></div>}><BreakdownQuery filter={filter} kind={breakdown}/></Card></DeferredSection>
          <Card title="采集来源" extra={<Link to="/usage/sources">查看全部{devices.length>3?` (${devices.length})`:''}</Link>}>
            {sources.isPending?<LoadingState label="正在读取采集证据…" />:sources.error&&!sources.data?<ErrorState error={sources.error} retry={()=>void sources.refetch()} />:<>

              <SourceTable devices={devices} compact zone={filter.time_zone} />
            </>}
          </Card>
        </div>
        <div className="overview-breakdowns">
          <DeferredSection title="平台分布" height={360}><BreakdownQuery filter={filter} kind="providers" distribution/></DeferredSection>
          <DeferredSection title="模型分布" height={360}><BreakdownQuery filter={filter} kind="models" distribution/></DeferredSection>
        </div>

        <div className="overview-links"><Link to="/quota">查看账号额度与节奏</Link><Link to="/projects">项目明细</Link><Link to={sessionsTarget(filter)}>会话明细</Link></div>

  </section>;
}
