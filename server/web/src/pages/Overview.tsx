import { lazy, Suspense, useMemo, useState } from 'react';
import { Alert, Card, Collapse, Segmented, Statistic, Table, Tabs, Tag, Typography, theme } from 'antd';
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
  const {token}=theme.useToken();
  const option=useMemo<EChartsCoreOption>(()=>({
    tooltip:{...tooltip,trigger:'item',formatter:(params:unknown)=>{const p=params as {dataIndex:number};const row=rows.slice(0,12)[p.dataIndex];return row?`${providerNames[row.name]??row.name}\n${metricLabel(metricValue(row,metric),metric)}`:'';}},
    grid:{left:12,right:32,top:8,bottom:20,containLabel:true},
    xAxis:{type:'value',name:metric==='cost'?'USD':'Token'},yAxis:{type:'category',inverse:true,data:rows.slice(0,12).map(r=>providerNames[r.name]??r.name),axisLabel:{width:150,overflow:'truncate'}},
    series:[{type:'bar',data:rows.slice(0,12).map(r=>metricCoordinate(metricValue(r,metric),metric)),itemStyle:{color:token.colorPrimary,borderRadius:[0,3,3,0]}}],
  }),[rows,metric,token.colorPrimary]);
  return <div>
    {rows.length>12&&<Tag>图表显示前 12 项</Tag>}
    {rows.length?<><Chart option={option} label={`${title}，${metric==='tokens'?'Token':'API 等价成本'}`} height={Math.max(160,Math.min(rows.length,12)*28+40)} />
      <Collapse ghost size="small" items={[{key:'details',label:`全部明细 (${rows.length})`,children:<Table< Slice> size="small" rowKey="key" dataSource={rows} pagination={rows.length>12?{pageSize:12,showSizeChanger:false}:false} scroll={{x:560}} columns={[
        {title:'名称',dataIndex:'name',render:(name:string)=>providerNames[name]??name},
        {title:'Token',align:'right',render:(_,r)=>integer(r.totals.total_tokens)},
        {title:'API 等价成本',align:'right',render:(_,r)=><span>{dollars(r.totals.cost_micro_usd)}{r.totals.cost_status==='partial'&&'（已知小计）'}</span>},
       ]} />}]} /></>:<EmptyState />}
  </div>;
}

export default function Overview() {
  const {token}=theme.useToken();
  const [filter,setFilter]=useState(initialFilter);
  const [metric,setMetric]=useState<Metric>('tokens');
  const [trendType,setTrendType]=useState<'bar'|'line'>('bar');
  const query=useQuery({queryKey:['statistics','summary',filter],queryFn:({signal})=>getSummary(filter,signal)});
  const data=query.data;
  const trend=useMemo<EChartsCoreOption>(()=>({
    tooltip:{...tooltip,trigger:'axis',formatter:(params:unknown)=>{const p=(params as {dataIndex:number}[])[0];const row=data?.trend[p?.dataIndex];return row?`${row.date}\n${metricLabel(metric==='tokens'?row.totals.total_tokens:row.totals.cost_micro_usd,metric)}`:'';}},
    grid:{left:12,right:18,top:32,bottom:28,containLabel:true},xAxis:{type:'category',data:data?.trend.map(r=>r.date)??[]},yAxis:{type:'value',name:metric==='cost'?'USD':'Token'},
    series:[{type:trendType,name:metric==='tokens'?'Token':'USD',data:data?.trend.map(r=>metricCoordinate(metric==='tokens'?r.totals.total_tokens:r.totals.cost_micro_usd,metric))??[],itemStyle:{color:token.colorPrimary},connectNulls:false}],
  }),[data,metric,trendType,token.colorPrimary]);
  const heatmap=useMemo<EChartsCoreOption>(()=>{
    if(!data?.heatmap.length) return {};
    const rows=data.heatmap,values=rows.map(r=>coordinate(r.totals.total_tokens));
    return {tooltip:{...tooltip,formatter:(params:unknown)=>{const p=params as {dataIndex:number};const r=rows[p.dataIndex];return r?`${r.date}\n已收到 Token：${integer(r.totals.total_tokens)}`:'';}},
      calendar:{top:38,left:36,right:12,bottom:12,cellSize:['auto',16],range:[rows[0].date,rows.at(-1)!.date],yearLabel:{show:false},dayLabel:{firstDay:1,nameMap:'ZH'},monthLabel:{nameMap:'ZH'},itemStyle:{color:token.colorFillSecondary,borderColor:token.colorBgContainer,borderWidth:3}},
      visualMap:{min:0,max:Math.max(1,...values.filter((n):n is number=>n!==null)),show:false,inRange:{color:[token.colorPrimaryBg,token.colorPrimaryBorder,token.colorPrimary,token.colorPrimaryTextActive]}},
      series:[{type:'heatmap',coordinateSystem:'calendar',data:rows.map((r,i)=>({value:[r.date,values[i]??0],itemStyle:values[i]===null?{color:token.colorFillSecondary}:undefined}))}],
    };
  },[data,token.colorFillSecondary,token.colorBgContainer,token.colorPrimaryBg,token.colorPrimaryBorder,token.colorPrimary,token.colorPrimaryTextActive]);
  return <section>
    <div className="page-heading"><div><Typography.Title level={3}>用量总览</Typography.Title><Typography.Paragraph type="secondary">查看已收到的用量、成本与活动分布。</Typography.Paragraph></div><Segmented aria-label="图表指标" value={metric} options={[{label:'Token',value:'tokens'},{label:'API 等价成本',value:'cost'}]} onChange={v=>setMetric(v as Metric)} /></div>
    <StatsFilters value={filter} onChange={setFilter} refresh={()=>void query.refetch()} busy={query.isFetching} />
    {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={()=>void query.refetch()} />:data&&<>
      {query.error&&<Alert type="warning" showIcon title="刷新失败，以下保留上次读取的数据" description={query.error.message} className="form-alert" />}
      <Card className="kpi-panel"><div className="metric-grid">
        <div><Statistic title="已收到 Token" value={data.totals.total_tokens??'未知'} formatter={()=>integer(data.totals.total_tokens)} /><div className="metric-note">输入 {integer(data.totals.input_tokens)} · 输出 {integer(data.totals.output_tokens)}</div></div>
        <div><Statistic title="API 等价成本" value={data.totals.cost_micro_usd??'未知'} formatter={()=>dollars(data.totals.cost_micro_usd)} /><div className="metric-note">{data.totals.cost_status==='partial'?'已知金额小计，含未定价记录':'按历史价格估算'} · 不是实际账单</div></div>
        <div><Statistic title="已收到会话" value={data.totals.sessions} formatter={()=>integer(data.totals.sessions)} /><div className="metric-note">调用 {integer(data.totals.invocations)} · 执行设备归因未知</div></div>
      </div></Card>
      <CoverageNotice coverage={data.coverage} zone={filter.time_zone} />
      <Suspense fallback={<LoadingState label="正在加载图表…" />}>
        <div className="dashboard-main"><Card title="用量趋势" extra={<Segmented aria-label="趋势图形式" options={[{label:'柱状',value:'bar'},{label:'折线',value:'line'}]} value={trendType} onChange={v=>setTrendType(v as 'bar'|'line')} />}>
          <Chart option={trend} label="按自然日的用量趋势" />
          {metric==='cost'&&data.trend_cost_rounding_delta_micro_usd!=='0'&&<Typography.Text type="secondary">趋势与范围的舍入差额：{dollars(data.trend_cost_rounding_delta_micro_usd)}</Typography.Text>}
        </Card>
        <Card title="用量构成"><Tabs items={[
          {key:'providers',label:'Provider',children:<Composition rows={data.providers} title="Provider 构成" metric={metric} />},
          {key:'models',label:'模型',children:<Composition rows={data.models} title="模型构成" metric={metric} />},
          {key:'tools',label:'工具与技能',children:<><Typography.Paragraph type="secondary">仅展示名称与次数，无调用参数或结果正文。</Typography.Paragraph><Table size="small" rowKey="key" dataSource={[...data.tools.map(r=>({...r,key:`tool:${r.key}`,kind:'工具'})),...data.skills.map(r=>({...r,key:`skill:${r.key}`,kind:'技能'}))]} pagination={{pageSize:10,showSizeChanger:false}} columns={[{title:'类型',dataIndex:'kind'},{title:'名称',dataIndex:'name'},{title:'调用次数',align:'right',render:(_,r)=>integer(r.totals.invocations)}]} /></>},
        ]} /></Card></div>
        <Card className="section-card" title="过去一年 · 活动热力图" extra={<Tag>365 个自然日</Tag>}>
          <div className="heatmap-scroll"><div className="heatmap-inner"><Chart option={heatmap} label="过去一年已收到的每日 Token 热力图" height={200} /></div></div>
          <Typography.Paragraph type="secondary">{dayjs(data.heatmap_range.start_at_ms).tz(filter.time_zone).format('YYYY-MM-DD')} 至 {dayjs(data.heatmap_range.end_at_ms).tz(filter.time_zone).subtract(1,'day').format('YYYY-MM-DD')} · 灰色表示未知，浅蓝包含已观测的零；年度范围独立于上方日期筛选。</Typography.Paragraph>
          <CoverageNotice coverage={data.heatmap_coverage} zone={filter.time_zone} />
        </Card>

      </Suspense>

    </>}
  </section>;
}
