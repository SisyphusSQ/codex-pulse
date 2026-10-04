import { lazy, Suspense, useMemo, useState } from 'react';
import { Alert, Button, Card, Collapse, Popover, Select, Segmented, Statistic, Table, Tooltip, Typography, theme } from 'antd';
import { InfoCircleOutlined } from '@ant-design/icons';
import { useIsFetching, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link,useSearchParams } from 'react-router-dom';
import type { EChartsCoreOption } from 'echarts/core';
import { getDevices, getAnnual, getTotals, getBreakdown, type StatsFilter, type Decimal, type Slice } from '../api/statistics';
import { getUsage, type Usage } from '../api/usage';
import { usageChartOption } from '../components/ModelTrend';
import { SourceTable } from '../components/SourceTable';
import { IndependentOverviewActivity, MachineUsage, sessionsTarget } from '../components/OverviewActivity';
import { decimalSorter } from '../components/sorting';
import { ActivityHeatmap } from '../components/ActivityHeatmap';
import { CoverageNotice } from '../components/CoverageNotice';
import { initialFilter, StatsFilters } from '../components/StatsFilters';
import { DeferredSection, SectionPlaceholder } from '../components/DeferredSection';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { dayjs, dollars, integer, tokens, providerNames } from '../format';

const Chart = lazy(() => import('../components/Chart'));
type Metric = 'tokens' | 'cost';
const metricOptions = [{ label: 'Token', value: 'tokens' }, { label: '费用', value: 'cost' }];
const metricValue = (row: Slice, metric: Metric): Decimal => metric === 'tokens' ? row.totals.total_tokens : row.totals.cost_micro_usd;
const metricLabel = (value: Decimal, metric: Metric) => metric === 'tokens' ? tokens(value) : dollars(value);
const metricCoordinate = (value: Decimal, metric: Metric) => value === null ? null : Number(value) / (metric === 'cost' ? 1_000_000 : 1);
const tooltip = { renderMode: 'richText' as const, confine: true };
const palette = ['#2678f5', '#3e9e82', '#8a70cf', '#d3a34b', '#6b8aad', '#c0779b'];
const providerColors: Record<string, string> = { codex: '#2678f5', cursor: '#3e9e82', grok: '#d3a34b' };

function SliceTable({ rows }: { rows: Slice[] }) {
  return <Table<Slice> size="small" rowKey="key" dataSource={rows} pagination={rows.length > 8 ? { pageSize: 8, showSizeChanger: false } : false} scroll={{ x: 420 }} columns={[
    { title: '名称', dataIndex: 'name', render: (name: string) => providerNames[name] ?? name },
    { title:'会话',align:'right',...decimalSorter<Slice>(r=>r.totals.sessions),render:(_,row)=>integer(row.totals.sessions) },
    { title: 'Token', align: 'right', ...decimalSorter<Slice>(r=>r.totals.total_tokens,true), render: (_, row) => tokens(row.totals.total_tokens) },
    { title: 'API 等价成本', align: 'right', ...decimalSorter<Slice>(r=>r.totals.cost_micro_usd), render: (_, row) => <div><span className="numeric">{dollars(row.totals.cost_micro_usd)}</span>{row.totals.cost_status === 'partial' && <Tooltip trigger={['hover','focus']} title="已知金额小计，含未定价记录"><InfoCircleOutlined tabIndex={0} aria-label="已知小计" className="partial-cost-icon" /></Tooltip>}</div> },
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
    <div className="metric-note">{metric === 'tokens' ? '按已收到的 Token 查看构成' : '按 API 等价成本查看构成'} · 仅已知正值进入环图</div>
    {slices.length ? <div className="distribution-layout">
      <div className="distribution-chart"><Suspense fallback={<LoadingState label="正在加载图表…" />}><Chart option={option} label={`${title}环图`} height={228} /></Suspense><span className="donut-caption" style={{ color: token.colorTextSecondary }}>{metric === 'tokens' ? 'Token' : 'API 等价成本'}</span></div>
      <div className="distribution-legend">{slices.slice(0, 8).map((row, index) => <div className="distribution-legend-row" key={row.key}>
        <span className="distribution-dot" style={{ background: color(row, index) }} />
        <Tooltip title={providerNames[row.name] ?? row.name}><span className="distribution-name">{providerNames[row.name] ?? row.name}</span></Tooltip>
        <span className="numeric">{metricLabel(metricValue(row, metric), metric)}</span>
      </div>)}{slices.length > 8 && <div className="metric-note">另有 {slices.length - 8} 项，环图已包含；见全部明细</div>}</div>
    </div> : <EmptyState />}
    <Collapse ghost size="small" items={[{ key: 'details', label: `全部明细 (${rows.length})`, children: <SliceTable rows={rows} /> }]} />
  </Card>;
}

function UsageChart({ data, metric, selection }: { data: Usage; metric: Metric; selection: string[] }) {
  const option = useMemo(() => usageChartOption(data, metric, selection), [data, metric, selection]);
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
        <Card className="section-card overview-trend" title={<div className="trend-title"><span>模型用量趋势</span><div className="overview-quality">{coverage && <Popover trigger="click" placement="bottomLeft" content={<div className="evidence-popover"><CoverageNotice coverage={coverage} zone={filter.time_zone} /></div>}><Button type="text" size="small" icon={<InfoCircleOutlined />}>{coverage.state==='unknown'?'暂无已知用量':'覆盖未确认'} · {coverage.stale?'采集证据陈旧':'有近期采集证据'} · 截至 {coverage.collected_at_ms===null?'尚无观测':dayjs(coverage.collected_at_ms).tz(filter.time_zone).format('MM-DD HH:mm')}</Button></Popover>}</div></div>} extra={<div className="overview-chart-controls">
          <Segmented aria-label="图表指标" size="small" options={metricOptions} value={metric} onChange={value => setMetric(value as Metric)} />
        </div>}>
          <Select aria-label="概览趋势模型" className="usage-model-selector" mode="multiple" maxCount={12} maxTagCount="responsive" value={selection} onChange={setChosen} options={available.map(r=>({value:`${r.provider}:${r.model}`,label:`${providerNames[r.provider]??r.provider} · ${r.model==='unknown'?'模型未归因':r.model}`}))} placeholder="选择趋势模型" />
          {usage.isPending?<LoadingState label="正在读取模型日桶…" />:usage.error&&!usage.data?<ErrorState error={usage.error} retry={()=>void usage.refetch()} />:<>
          {usage.error&&<Alert type="warning" title="模型趋势更新失败，保留上次读取的数据" description={usage.error.message} />}
          {selection.length && usage.data?<UsageChart data={usage.data} metric={metric} selection={selection} />:<EmptyState description="当前范围尚无已收到的模型日桶。" />}
          </>}
          <div className="metric-note">按具体模型的真实日桶 · 最多12个模型 · 未知日期留空</div>
          {metric === 'cost' && usage.data && usage.data.trend_cost_rounding_delta_micro_usd !== '0' && <Typography.Text type="secondary">趋势与范围的舍入差额：{usage.data.trend_cost_rounding_delta_micro_usd!==null&&BigInt(usage.data.trend_cost_rounding_delta_micro_usd)>-10_000n&&BigInt(usage.data.trend_cost_rounding_delta_micro_usd)<10_000n?'不足 $0.01':dollars(usage.data.trend_cost_rounding_delta_micro_usd)}</Typography.Text>}
        </Card></>;
}
function BreakdownQuery({filter,kind,distribution=false}:{filter:StatsFilter;kind:'providers'|'models';distribution?:boolean}){
 const query=useQuery({queryKey:['statistics',kind,filter],queryFn:({signal})=>getBreakdown(filter,kind,signal),refetchInterval:60_000});
 const title=kind==='providers'?'平台分布':'模型分布';
 return query.isPending?<LoadingState label="正在读取用量明细…"/>:!query.data?<ErrorState error={query.error} retry={()=>void query.refetch()}/>:<>{query.error&&<Alert type="warning" title="明细更新失败，保留上次读取的数据"/>}{distribution?<Distribution rows={query.data[kind]??[]} title={title}/>:<SliceTable rows={query.data[kind]??[]}/>}</>;
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
  const sources=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
  const devices=(sources.data??[]).filter(d=>!filter.client_id||d.id===filter.client_id).map(d=>({...d,providers:d.providers.filter(p=>!filter.provider||p.provider===filter.provider)}));
  return <section className="overview-page">
    <StatsFilters value={filter} onChange={v=>{setFilter(v);}} refresh={() => {void queryClient.refetchQueries({queryKey:['statistics'],type:'active'});void queryClient.refetchQueries({queryKey:['usage'],type:'active'});}} busy={query.isFetching||usage.isFetching||machineFetching} />
    {query.isPending ? <SectionPlaceholder title="全年活动" height={300} /> : query.error && !data ? <Card title="全年活动"><ErrorState error={query.error} retry={() => void query.refetch()} /></Card> : data && <>
      {query.error && <Alert type="warning" showIcon title="刷新失败，以下保留上次读取的数据" description={query.error.message} className="form-alert" />}
      <Card className="overview-activity" title="全年活动" extra={<div className="annual-actions"><span className="metric-note">{data.heatmap_coverage.state==='unknown'?'年度用量未知':'年度覆盖未确认'} · {data.heatmap_coverage.stale?'采集陈旧':'近期采集'}</span><Popover trigger="click" placement="bottomRight" content={<div className="evidence-popover">
        <div className="activity-metrics">{[
          ['近 365 天已收到 Token', data.heatmap_activity.total_tokens],
          ['已观测峰值日 Token', data.heatmap_activity.peak_daily_tokens],
          ['已观测活跃天数', data.heatmap_activity.active_days],
          ['当前连续天数', data.heatmap_activity.current_streak_days],
          ['已观测最长连续天数', data.heatmap_activity.longest_streak_days],
        ].map(([title, value], index) => <div key={title!}><strong>{index<2?tokens(value):integer(value)}</strong><span>{title}</span></div>)}</div>
        <div className="metric-note">年度范围独立于上方统计日期；未知日期不补零，活跃天数与最长连续天数仅计已观测事实。</div>
        <CoverageNotice coverage={data.heatmap_coverage} zone={filter.time_zone} />
        </div>}><Button type="text" size="small">年度活动统计</Button></Popover></div>}>
        <div className="annual-totals" aria-label="年度用量汇总">
          <div><Statistic title="近 365 天 Token 总量" value={data.heatmap_totals?.total_tokens??data.heatmap_activity.total_tokens??'未知'} formatter={()=>tokens(data.heatmap_totals?.total_tokens??data.heatmap_activity.total_tokens)} /><span className="metric-note">按年度活动范围汇总已收到事实</span></div>
          <div><Statistic title="近 365 天 API 等价成本" value={data.heatmap_totals?.cost_micro_usd??'未知'} formatter={()=>dollars(data.heatmap_totals?.cost_micro_usd??null)} /><span className="metric-note">{data.heatmap_totals?.cost_status==='partial'?'已知金额小计，含未定价记录':'按历史价格估算'} · USD</span></div>
        </div>
        {data.heatmap.length ? <ActivityHeatmap days={data.heatmap} /> : <EmptyState description="年度活动暂时不可用" />}
        <div className="activity-scope"><span>{dayjs(data.heatmap_range.start_at_ms).tz(filter.time_zone).format('YYYY-MM-DD')} 至 {dayjs(data.heatmap_range.end_at_ms).tz(filter.time_zone).subtract(1, 'day').format('YYYY-MM-DD')}</span><span>{filter.time_zone}</span><span>{filter.provider ? providerNames[filter.provider] : '全部客户端'}</span></div>
      </Card>
    </>}
      <Card className="summary-band" style={{ minHeight: 132 }}>
        {usage.error && totals && <Alert type="warning" showIcon title={"范围用量刷新失败，保留上次读取的数据"} description={usage.error.message} />}
        {!totals && usage.isPending ? <LoadingState label="正在读取范围用量…" /> : usage.error && !totals ? <ErrorState error={usage.error} retry={() => void usage.refetch()} /> : totals && <div className="metric-grid overview-kpis">
        <div><Statistic title="当前范围 Token 总量" value={totals.total_tokens ?? '未知'} formatter={() => tokens(totals.total_tokens)} /><div className="metric-note">输入 {tokens(totals.input_tokens)} · 输出 {tokens(totals.output_tokens)}</div></div>
        <div><Statistic title="当前范围 API 等价成本" value={totals.cost_micro_usd ?? '未知'} formatter={() => dollars(totals.cost_micro_usd)} /><div className="metric-note">{totals.cost_status === 'partial' ? '已知金额小计，含未定价记录' : '按历史价格估算'} · 不是实际账单</div></div>
        <div><Statistic title="已收到会话" value={totals.sessions} formatter={() => integer(totals.sessions)} /><div className="metric-note">按 Token 活动去重 · 采集来源见下方</div></div>
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
              {sources.error&&<Alert type="warning" title="来源更新失败，保留上次证据" />}
              <SourceTable devices={devices} compact zone={filter.time_zone} />
              <div className="metric-note source-footnote">采集来源多对多 · 执行归属未知 · 近期证据不证明历史完整</div>
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
