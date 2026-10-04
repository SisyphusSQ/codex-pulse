import {lazy,Suspense,useMemo,useState} from 'react';
import {Alert,Card,Collapse,Descriptions,Select,Segmented,Statistic,Table,Typography} from 'antd';
import {CacheNotice} from '../components/CacheNotice';
import {useQuery} from '@tanstack/react-query';
import {Link,useSearchParams} from 'react-router-dom';
import {getUsage} from '../api/usage';
import type {UsageModel} from '../api/usage';
import {decimalSorter} from '../components/sorting';
import type {Slice,Totals} from '../api/statistics';
import {tokens,dollars,integer,providerNames} from '../format';
import {initialFilter,StatsFilters} from '../components/StatsFilters';
import {CacheHitRateCell} from '../components/CacheHitRate';
import {CoverageNotice} from '../components/CoverageNotice';
import {EmptyState,ErrorState,LoadingState} from '../components/QueryState';
import {usageChartOption} from '../components/ModelTrend';
export {usageChartOption} from '../components/ModelTrend';
const Chart=lazy(()=>import('../components/Chart'));
type Metric='tokens'|'cost';
const key=(r:{provider:string;model:string})=>`${r.provider}:${r.model}`;
function estimatedLabel(provider:string){return provider==='codex'?'API 折算成本':provider==='cursor'?'文档价目估算':provider==='grok'?'xAI 参考价估算':'参考价估算合计';}
function totalDetails(t:Totals){return [{key:'input',label:'输入 Token',children:tokens(t.input_tokens)},{key:'cached',label:'缓存读取 Token',children:tokens(t.cached_tokens)},{key:'write',label:'缓存写入 Token',children:tokens(t.cache_write_tokens)},{key:'output',label:'输出 Token',children:tokens(t.output_tokens)},{key:'reasoning',label:'独立 reasoning Token',children:tokens(t.reasoning_tokens)},{key:'version',label:'历史价格版本',children:t.pricing_versions.length?t.pricing_versions.join('、'):'未提供价格证据'}];}
export default function Usage(){
 const [params]=useSearchParams();
 const [filter,setFilter]=useState(()=>({...initialFilter(),provider:['codex','cursor','grok'].includes(params.get('provider')??'')?params.get('provider')!:'',model:params.get('model')??''}));
 const [metric,setMetric]=useState<Metric>('tokens');const [chosen,setChosen]=useState<string[]|null>(null);
 const query=useQuery({queryKey:['usage',filter],queryFn:({signal})=>getUsage(filter,signal),refetchInterval:60_000});const data=query.data;
 const available=data?.models??[];
 const selection=chosen?.filter(v=>available.some(r=>key(r)===v))??available.slice(0,12).map(key);
 const option=useMemo(()=>data?usageChartOption(data,metric,selection):{},[data,metric,selection.join('\x00')]);
 return <section className="usage-page">
 <StatsFilters value={filter} onChange={v=>{setFilter({...v,model:v.model??''});setChosen(null);}} refresh={()=>void query.refetch()} busy={query.isFetching} extra={<Select className="model-filter" aria-label="用量模型" value={filter.model} showSearch={{optionFilterProp:'label'}} onChange={model=>{setFilter({...filter,model});setChosen(null);}} options={[{value:'',label:'全部模型'},...[...new Set(available.map(r=>r.model))].map(value=>({value,label:value==='unknown'?'模型未归因':value}))]} />} />
 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={()=>void query.refetch()} />:data&&<>
 {query.error&&<Alert showIcon type="warning" title="更新失败，保留上次用量与原时间" description={query.error.message} className="form-alert" />}
 <Card className="summary-band"><div className="metric-grid usage-kpis"><div><Statistic title="Token 总量" value={data.totals.total_tokens??'未知'} formatter={()=>tokens(data.totals.total_tokens)} /><div className="metric-note">输入 {tokens(data.totals.input_tokens)} · 输出 {tokens(data.totals.output_tokens)}</div></div>
 <div><Statistic title={estimatedLabel(filter.provider)} value={data.totals.cost_micro_usd??'未知'} formatter={()=>dollars(data.totals.cost_micro_usd)} /><div className="metric-note">{data.totals.cost_status==='partial'?'已知金额小计':'按历史费率计算'} · 估算</div></div>
 <div><Statistic title={filter.provider==='cursor'?'Cursor 上报费用':filter.provider==='grok'?'Grok 上报费用':'来源上报费用'} value={data.totals.reported_charge_micro_usd??'未知'} formatter={()=>dollars(data.totals.reported_charge_micro_usd)} /><div className="metric-note">{data.totals.reported_charge_status==='partial'?'已知上报金额小计':'数据源提供的金额'} · 与估算分开展示</div></div>
 <div><Statistic title="未定价记录" value={data.coverage.unpriced_facts} formatter={()=>integer(data.coverage.unpriced_facts)} /><div className="metric-note">缺失Token或价格不计作免费</div></div></div></Card>
 <CacheNotice cache={data.cache} zone={filter.time_zone}/><CoverageNotice coverage={data.coverage} zone={filter.time_zone} />
 <Card title="按模型的用量趋势" className="section-card" extra={<Segmented aria-label="用量趋势指标" value={metric} onChange={v=>setMetric(v as Metric)} options={[{label:'Token',value:'tokens'},{label:'估算成本',value:'cost'}]} />}>
 <Select className="usage-model-selector" aria-label="趋势模型" mode="multiple" maxCount={12} maxTagCount="responsive" value={selection} onChange={setChosen} options={available.map(r=>({value:key(r),label:`${providerNames[r.provider]} · ${r.model}`}))} placeholder="选择趋势模型" />

 {available.length&&selection.length?<Suspense fallback={<LoadingState label="正在加载模型趋势…" />}><Chart option={option} height={300} label="按模型的真实用量折线" /></Suspense>:<EmptyState description="当前筛选范围暂无模型用量。" />}
 <div className="metric-note">最多12个模型 · 实际日桶 · 未知日期留空</div>
 </Card>
 <Card size="small" className="section-card"><Descriptions size="small" column={{xs:1,md:3}} items={[...totalDetails(data.totals).slice(0,5),{key:'cache-rate',label:'范围缓存命中率',children:<CacheHitRateCell value={data.cache_hit_rate} />}]} /></Card>
 {data.cursor_pools.length>0&&<Card title="Cursor 用量池" className="section-card"><Table size="small" rowKey="key" pagination={false} dataSource={data.cursor_pools} columns={[{title:'用量池',render:(_,r)=>r.key==='cursor.models'?'Cursor Models':r.key==='cursor.other_models'?'Other Models':'未归类'},{title:'Token',...decimalSorter<Slice>(r=>r.totals.total_tokens,true),render:(_,r)=>tokens(r.totals.total_tokens)},{title:'Dashboard 上报费用',...decimalSorter<Slice>(r=>r.totals.reported_charge_micro_usd),render:(_,r)=>dollars(r.totals.reported_charge_micro_usd)},{title:'文档价目估算',...decimalSorter<Slice>(r=>r.totals.cost_micro_usd),render:(_,r)=>dollars(r.totals.cost_micro_usd)}]} /><Typography.Text type="secondary">上报费用已经包含适用的Cursor Token Rate，不再次相加；缺少归类证据保留未归类。</Typography.Text></Card>}
 <Card title="模型用量与成本" className="section-card"><Table<UsageModel> rowKey={key} size="small" dataSource={available} pagination={{pageSize:15,showSizeChanger:true}} scroll={{x:1080}} expandable={{expandedRowRender:r=><><Descriptions size="small" column={{xs:1,md:2}} items={totalDetails(r.totals)} /><div className="catalog-toolbar">{r.totals.pricing_versions.map(v=><Link key={v} to={`/pricing?provider=${r.provider}&model=${encodeURIComponent(r.model)}&version=${encodeURIComponent(v)}`}>查看 {v}</Link>)}</div></>}} columns={[
 {title:'模型',width:230,render:(_,r)=><><Typography.Text strong>{r.model==='unknown'?'模型未归因':r.model}</Typography.Text><div className="metric-note">{providerNames[r.provider]??r.provider}</div></>},
 {title:'输入',align:'right',...decimalSorter<UsageModel>(r=>r.totals.input_tokens),render:(_,r)=>tokens(r.totals.input_tokens)},{title:'缓存读取',align:'right',...decimalSorter<UsageModel>(r=>r.totals.cached_tokens),render:(_,r)=>tokens(r.totals.cached_tokens)},{title:'缓存命中率',align:'right',render:(_,r)=><CacheHitRateCell value={r.cache_hit_rate} />},
 {title:'输出',align:'right',...decimalSorter<UsageModel>(r=>r.totals.output_tokens),render:(_,r)=>tokens(r.totals.output_tokens)},{title:'总 Token',align:'right',...decimalSorter<UsageModel>(r=>r.totals.total_tokens,true),render:(_,r)=>tokens(r.totals.total_tokens)},
 {title:'估算成本',align:'right',...decimalSorter<UsageModel>(r=>r.totals.cost_micro_usd),render:(_,r)=><>{dollars(r.totals.cost_micro_usd)}{r.totals.cost_status==='partial'&&<div className="metric-note">已知小计</div>}</>},
 {title:'上报费用',align:'right',...decimalSorter<UsageModel>(r=>r.totals.reported_charge_micro_usd),render:(_,r)=>dollars(r.totals.reported_charge_micro_usd)},
 ]} /><Typography.Text type="secondary">模型与范围、每日趋势的金额可能存在微美元舍入差额：模型 {dollars(data.model_cost_rounding_delta_micro_usd)}；趋势 {dollars(data.trend_cost_rounding_delta_micro_usd)}。计算保留完整精度。</Typography.Text></Card>
 <Collapse ghost size="small" items={[{key:'formula',label:'成本计算口径与历史价格',children:<><Descriptions size="small" column={1} items={[{key:'versions',label:'本范围价格版本',children:data.totals.pricing_versions.join('、')||'未取得'},{key:'basis',label:'统计范围',children:'中心已接受事实；全局去重，来源筛选读取该来源自己的快照'}]} /><Typography.Paragraph>Codex：非缓存输入×输入价 + 缓存输入×缓存价 +（输出+独立reasoning）×输出价，按每百万Token费率折算。当前Mac基础文本估算不推断长上下文、Fast或缓存写入价格。Cursor缓存读取/写入分别计价；Grok参考价与完整上报费用分别保留。更新参考目录不会覆盖历史成本。</Typography.Paragraph></>}]} />
 </>}
 <div className="overview-links"><Link to="/pricing">查看模型价目表</Link><Link to="/projects">项目明细</Link><Link to="/sessions">会话明细</Link></div><div className="metric-note">全局用量独立于额度账号；无可靠账号归因的历史保留在全局 / 采集来源。</div>
 </section>;
}
