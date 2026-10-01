import { lazy, Suspense, useMemo, useState } from 'react';
import { Alert, Card, Segmented, Table, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import type { EChartsCoreOption } from 'echarts/core';
import { getSummary, type Decimal, type Slice } from '../api/statistics';
import { CoverageNotice } from '../components/CoverageNotice';
import { initialFilter, StatsFilters } from '../components/StatsFilters';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { coordinate, dayjs, dollars, integer, providerNames } from '../format';

const Chart=lazy(()=>import('../components/Chart'));
type Metric='tokens'|'cost';
const metricValue=(row:Slice,metric:Metric):Decimal=>metric==='tokens'?row.totals.total_tokens:row.totals.cost_micro_usd;
const metricLabel=(value:Decimal,metric:Metric)=>metric==='tokens'?integer(value):dollars(value);
const metricCoordinate=(value:Decimal,metric:Metric)=>value===null?null:Number(value)/(metric==='cost'?1_000_000:1);
const tooltip={renderMode:'richText' as const,confine:true};

function Composition({ rows, title, metric }: { rows:Slice[];title:string;metric:Metric }) {
  const option=useMemo<EChartsCoreOption>(()=>({
    tooltip:{...tooltip,trigger:'item',formatter:(params:unknown)=>{const p=params as {dataIndex:number};const row=rows.slice(0,12)[p.dataIndex];return row?`${providerNames[row.name]??row.name}\n${metricLabel(metricValue(row,metric),metric)}`:'';}},
    grid:{left:12,right:32,top:8,bottom:20,containLabel:true},
    xAxis:{type:'value',name:metric==='cost'?'USD':'Token'},yAxis:{type:'category',inverse:true,data:rows.slice(0,12).map(r=>providerNames[r.name]??r.name),axisLabel:{width:150,overflow:'truncate'}},
    series:[{type:'bar',data:rows.slice(0,12).map(r=>metricCoordinate(metricValue(r,metric),metric)),itemStyle:{color:'#137b59',borderRadius:[0,3,3,0]}}],
  }),[rows,metric]);
  return <Card title={title} extra={rows.length>12?<Tag>图表显示前 12 项</Tag>:undefined}>
    {rows.length?<><Chart option={option} label={`${title}，${metric==='tokens'?'Token':'API 等价成本'}`} height={Math.max(160,Math.min(rows.length,12)*28+40)} />
      <Table< Slice> size="small" rowKey="key" dataSource={rows} pagination={rows.length>12?{pageSize:12,showSizeChanger:false}:false} scroll={{x:560}} columns={[
        {title:'名称',dataIndex:'name',render:(name:string)=>providerNames[name]??name},
        {title:'Token',align:'right',render:(_,r)=>integer(r.totals.total_tokens)},
        {title:'API 等价成本',align:'right',render:(_,r)=><span>{dollars(r.totals.cost_micro_usd)}{r.totals.cost_status==='partial'&&'（已知小计）'}</span>},
      ]} /></>:<EmptyState />}
  </Card>;
}

export default function Overview() {
  const [filter,setFilter]=useState(initialFilter);
  const [metric,setMetric]=useState<Metric>('tokens');
  const [trendType,setTrendType]=useState<'bar'|'line'>('bar');
  const query=useQuery({queryKey:['statistics','summary',filter],queryFn:({signal})=>getSummary(filter,signal)});
  const data=query.data;
  const trend=useMemo<EChartsCoreOption>(()=>({
    tooltip:{...tooltip,trigger:'axis',formatter:(params:unknown)=>{const p=(params as {dataIndex:number}[])[0];const row=data?.trend[p?.dataIndex];return row?`${row.date}\n${metricLabel(metric==='tokens'?row.totals.total_tokens:row.totals.cost_micro_usd,metric)}`:'';}},
    grid:{left:12,right:18,top:32,bottom:28,containLabel:true},xAxis:{type:'category',data:data?.trend.map(r=>r.date)??[]},yAxis:{type:'value',name:metric==='cost'?'USD':'Token'},
    series:[{type:trendType,name:metric==='tokens'?'Token':'USD',data:data?.trend.map(r=>metricCoordinate(metric==='tokens'?r.totals.total_tokens:r.totals.cost_micro_usd,metric))??[],itemStyle:{color:'#137b59'},connectNulls:false}],
  }),[data,metric,trendType]);
  const heatmap=useMemo<EChartsCoreOption>(()=>{
    if(!data?.heatmap.length) return {};
    const rows=data.heatmap,values=rows.map(r=>coordinate(r.totals.total_tokens));
    return {tooltip:{...tooltip,formatter:(params:unknown)=>{const p=params as {dataIndex:number};const r=rows[p.dataIndex];return r?`${r.date}\n已收到 Token：${integer(r.totals.total_tokens)}`:'';}},
      calendar:{top:38,left:36,right:12,bottom:12,cellSize:['auto',16],range:[rows[0].date,rows.at(-1)!.date],yearLabel:{show:false},dayLabel:{firstDay:1,nameMap:'ZH'},monthLabel:{nameMap:'ZH'},itemStyle:{color:'#edf0ef',borderColor:'#fff',borderWidth:3}},
      visualMap:{min:0,max:Math.max(1,...values.filter((n):n is number=>n!==null)),show:false,inRange:{color:['#e0f2e8','#92d3b2','#40946c','#125338']}},
      series:[{type:'heatmap',coordinateSystem:'calendar',data:rows.map((r,i)=>({value:[r.date,values[i]??0],itemStyle:values[i]===null?{color:'#edf0ef'}:undefined}))}],
    };
  },[data]);
  return <section>
    <div className="page-heading"><div><span className="eyebrow">USAGE OVERVIEW</span><Typography.Title level={2}>用量总览</Typography.Title><Typography.Paragraph type="secondary">聚合各台机器的用量事实，保留来源、未知值和历史价格。</Typography.Paragraph></div><Segmented aria-label="图表指标" value={metric} options={[{label:'Token',value:'tokens'},{label:'API 等价成本',value:'cost'}]} onChange={v=>setMetric(v as Metric)} /></div>
    <StatsFilters value={filter} onChange={setFilter} refresh={()=>void query.refetch()} busy={query.isFetching} />
    {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={()=>void query.refetch()} />:data&&<>
      {query.error&&<Alert type="warning" showIcon title="刷新失败，以下保留上次读取的数据" description={query.error.message} className="form-alert" />}
      <div className="metric-grid">
        <Card><div className="metric-label">已收到 Token</div><div className="metric-value">{integer(data.totals.total_tokens)}</div><div className="metric-note">输入 {integer(data.totals.input_tokens)} · 输出 {integer(data.totals.output_tokens)}</div></Card>
        <Card><div className="metric-label">API 等价成本</div><div className="metric-value">{dollars(data.totals.cost_micro_usd)}</div><div className="metric-note">{data.totals.cost_status==='partial'?'已知金额小计，含未定价记录':'按历史价格估算'} · 不是实际账单</div></Card>
        <Card><div className="metric-label">已收到会话 / 调用</div><div className="metric-value">{integer(data.totals.sessions)} <span>/ {integer(data.totals.invocations)}</span></div><div className="metric-note">执行设备归因：未知</div></Card>
      </div>
      <CoverageNotice coverage={data.coverage} zone={filter.time_zone} />
      <Suspense fallback={<LoadingState label="正在加载图表…" />}>
        <Card className="section-card" title="用量趋势" extra={<Segmented aria-label="趋势图形式" options={[{label:'柱状',value:'bar'},{label:'折线',value:'line'}]} value={trendType} onChange={v=>setTrendType(v as 'bar'|'line')} />}>
          <Chart option={trend} label="按自然日的用量趋势" />
          {metric==='cost'&&data.trend_cost_rounding_delta_micro_usd!=='0'&&<Typography.Text type="secondary">趋势与范围的舍入差额：{dollars(data.trend_cost_rounding_delta_micro_usd)}</Typography.Text>}
        </Card>
        <Card className="section-card" title="过去一年 · 活动热力图" extra={<Tag>365 个自然日</Tag>}>
          <div className="heatmap-scroll"><div className="heatmap-inner"><Chart option={heatmap} label="过去一年已收到的每日 Token 热力图" height={200} /></div></div>
          <Typography.Paragraph type="secondary">{dayjs(data.heatmap_range.start_at_ms).tz(filter.time_zone).format('YYYY-MM-DD')} 至 {dayjs(data.heatmap_range.end_at_ms).tz(filter.time_zone).subtract(1,'day').format('YYYY-MM-DD')} · 灰色表示未知，浅绿包含已观测的零；年度范围独立于上方日期筛选。</Typography.Paragraph>
          <CoverageNotice coverage={data.heatmap_coverage} zone={filter.time_zone} />
        </Card>
        <div className="chart-grid section-card"><Composition rows={data.providers} title="Provider 构成" metric={metric} /><Composition rows={data.models} title="模型构成" metric={metric} /></div>
      </Suspense>
      <Card title="执行设备归因" className="section-card"><Typography.Paragraph>采集来源可在顶部筛选。中心目前没有可信的执行设备证据，不能按采集机器分摊消耗。</Typography.Paragraph></Card>
      <Card title="工具与技能调用" className="section-card"><Typography.Paragraph type="secondary">仅展示已收到的名称与次数，无调用参数或结果正文。</Typography.Paragraph><Table size="small" rowKey="key" dataSource={[...data.tools.map(r=>({...r,key:`tool:${r.key}`,kind:'工具'})),...data.skills.map(r=>({...r,key:`skill:${r.key}`,kind:'技能'}))]} pagination={{pageSize:10,showSizeChanger:false}} columns={[{title:'类型',dataIndex:'kind'},{title:'名称',dataIndex:'name'},{title:'调用次数',align:'right',render:(_,r)=>integer(r.totals.invocations)}]} /></Card>
    </>}
  </section>;
}
