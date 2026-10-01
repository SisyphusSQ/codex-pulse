import { Descriptions, Tag, Tooltip, Typography } from 'antd';
import type { CacheHitRateStats } from '../api/records';
import { integer } from '../format';

const reasons:Record<string,string>={not_reported:'客户端尚未上报',unsupported_provider:'此 Provider 暂不支持',not_applicable:'没有输入 Token',unavailable:'输入计数不可用',rollup_missing:'尚未建立会话统计',rollup_ambiguous:'会话统计不明确',history_filtered:'上报历史范围未包含整个会话',source_conflict:'来源存在冲突'};
export function cacheHitRate(value?:CacheHitRateStats|null){
 if(value?.unit!=='basis_points'||value.basis_points===null||value.basis_points===undefined||!/^\d+$/.test(value.basis_points))return '—';
 const basis=BigInt(value.basis_points);if(basis>10_000n)return '—';
 const tenths=(basis+5n)/10n;return `${tenths/10n}.${tenths%10n}%`;
}
export function CacheHitRateCell({value}:{value?:CacheHitRateStats|null}){
 const label=cacheHitRate(value),reason=reasons[value?.reason??'']??'客户端尚未上报';
 return <Tooltip title={label==='—'?reason:`生命周期缓存输入 / 全部输入${value?.status==='partial'?' · 部分索引或来源证据':''}`}><span className="numeric" aria-label={`缓存命中率 ${label}${label==='—'?`，${reason}`:''}`}>{label}</span></Tooltip>;
}
export function CacheHitRateDetail({value}:{value?:CacheHitRateStats|null}){return <div className="cache-hit-detail">
 <Descriptions size="small" layout="vertical" column={{xs:1,sm:3}} items={[
  {key:'rate',label:'缓存命中率',children:<Typography.Text strong>{cacheHitRate(value)}</Typography.Text>},
  {key:'input',label:'生命周期输入',children:integer(value?.input_tokens)},
  {key:'cache',label:'缓存输入',children:integer(value?.cached_input_tokens)},
 ]} />
 <Typography.Paragraph type="secondary">整段已索引会话的缓存输入 / 全部输入，缓存已包含在输入中。不随当前日期筛选或轮次分页重算。</Typography.Paragraph>
 {value?.reason&&<Typography.Text type="secondary">{reasons[value.reason]??'统计不可用'}</Typography.Text>}
 {value?.status==='partial'&&<Tag color={value.conflict?'red':'gold'}>部分索引或来源证据</Tag>}
 </div>;}
