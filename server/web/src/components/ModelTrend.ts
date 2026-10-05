import type {EChartsCoreOption} from 'echarts/core';
import type {Usage} from '../api/usage';
import {tokens,dollars,providerNames,tokenAxis,dollarAxis} from '../format';
import {usageDecimal,usageTokens,usageDollars} from './usageDisplay';
type Metric='tokens'|'cost';
const palette=['#2678f5','#3e9e82','#8a70cf','#d3a34b','#6b8aad','#c0779b'];
const key=(r:{provider:string;model:string})=>`${r.provider}:${r.model}`;
export function usageChartOption(data:Usage,metric:Metric,selection:string[],emptyAsZero=false):EChartsCoreOption{
 const buckets=new Map(data.model_days.map(r=>[`${key(r)}:${r.date}`,r.totals]));
 const sorted=data.models.filter(r=>selection.includes(key(r)));
 return {color:palette,tooltip:{renderMode:'richText',confine:true,trigger:'axis',axisPointer:{type:'shadow'},formatter:(params:unknown)=>{const ps=params as {seriesName:string;dataIndex:number;value:number|null}[];const day=data.trend[ps[0]?.dataIndex];if(!day)return '';return `${day.date}\n${ps.map(p=>{const model=sorted.find(r=>`${providerNames[r.provider]} · ${r.model}`===p.seriesName);const t=model?buckets.get(`${key(model)}:${day.date}`):undefined;return `${p.seriesName}：${emptyAsZero?(metric==='tokens'?usageTokens(usageDecimal(t,'total_tokens')):usageDollars(usageDecimal(t,'cost_micro_usd'))):metric==='tokens'?tokens(t?.total_tokens??null):dollars(t?.cost_micro_usd??null)}${metric==='cost'&&t?.cost_status==='partial'?'（已知小计）':''}`;}).join('\n')}`;}},
 legend:{type:'scroll',bottom:0,icon:'circle',itemWidth:7,itemHeight:7,textStyle:{color:'#617089',fontSize:12}},grid:{left:12,right:12,top:28,bottom:42,containLabel:true},xAxis:{type:'category',data:data.trend.map(r=>r.date),axisLabel:{formatter:(v:string)=>v.slice(5)}},yAxis:{type:'value',name:metric==='tokens'?'Token':'USD',axisLabel:{formatter:metric==='tokens'?tokenAxis:dollarAxis},splitLine:{lineStyle:{color:'#edf1f6'}}},
 series:sorted.map(r=>({name:`${providerNames[r.provider]} · ${r.model}`,type:'bar',stack:sorted.length>1?'models':undefined,barMaxWidth:32,itemStyle:{color:r.model==='unknown'?'#95a2b4':palette[data.models.findIndex(m=>key(m)===key(r))%palette.length]},data:data.trend.map(day=>{const t=buckets.get(`${key(r)}:${day.date}`);const value=emptyAsZero?usageDecimal(t,metric==='tokens'?'total_tokens':'cost_micro_usd'):metric==='tokens'?t?.total_tokens:t?.cost_micro_usd;return value==null?null:Number(value)/(metric==='cost'?1_000_000:1);})}))};
}
