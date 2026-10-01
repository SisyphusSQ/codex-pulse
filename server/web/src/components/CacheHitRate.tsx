import { Typography } from 'antd';
import type { CacheHitRateStats } from '../api/records';
import { integer } from '../format';

const reasons:Record<string,string>={not_reported:'客户端尚未上报',unsupported_provider:'此 Provider 暂不支持',not_applicable:'没有输入 Token',unavailable:'输入计数不可用',rollup_missing:'尚未建立会话统计',rollup_ambiguous:'会话统计不明确',history_filtered:'上报历史范围未包含整个会话',source_conflict:'来源存在冲突'};
export function cacheHitRate(value?:CacheHitRateStats|null){
 if(value?.unit!=='basis_points'||value.basis_points===null||value.basis_points===undefined||!/^\d+$/.test(value.basis_points))return '—';
 const basis=BigInt(value.basis_points);if(basis>10_000n)return '—';
 const tenths=(basis+5n)/10n;return `${tenths/10n}.${tenths%10n}%`;
}
export function CacheHitRateCell({value}:{value?:CacheHitRateStats|null}){return <span className="numeric">{cacheHitRate(value)}</span>;}
export function CacheHitRateDetail({value}:{value?:CacheHitRateStats|null}){return <div className="cache-hit-detail"><Typography.Text strong>缓存命中率 {cacheHitRate(value)}</Typography.Text><Typography.Paragraph type="secondary">整段已索引会话的缓存输入 / 全部输入，缓存已包含在输入中。{value?.basis_points!==null&&value?.basis_points!==undefined?`缓存 ${integer(value.cached_input_tokens)} / 输入 ${integer(value.input_tokens)}。`:''}{value?.reason?reasons[value.reason]??'统计不可用':''}{value?.status==='partial'?' · 部分索引或来源证据':''}；不随当前日期筛选或轮次分页重算。</Typography.Paragraph></div>;}
