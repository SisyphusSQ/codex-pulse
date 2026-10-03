import type {Catalog,ModelPrice,PlanPrice} from '../api/catalog';
import type {Client} from '../api/access';
import type {Device,Summary,Totals,Day,StatsFilter} from '../api/statistics';
import type {Usage} from '../api/usage';
import type {Subscription} from '../api/subscriptions';
import type {SessionRecord,ProjectRecord} from '../api/records';
import {quotaFixture,paceFixture,quotaNow} from '../test/quotaFixture';
import {summaryFixture} from '../test/statisticsFixture';
import {dayjs} from '../format';
import {heatmapDays,modelSeries,modelRows,sampleTotals,sessions,projects} from './fixtures';
import publicModels from '../../../internal/service/catalog_srv/models-20261002.json';
import publicPlans from '../../../internal/service/catalog_srv/plans-20261002.json';
import releases from '../../../internal/service/catalog_srv/releases-20261003.json';

export type ReviewScenario='current'|'expired'|'conflict'|'unknown';
export const reviewNow=Date.UTC(2026,9,3,1);
const key=(model:string)=>model.toLowerCase().replace(/\s+/g,'-');
const releaseByModel=new Map(releases.flatMap(r=>r.models.map(model=>[key(model),{released_at_ms:Date.parse(`${r.released_on}T00:00:00Z`),release_source_url:r.source_url}] as const)));
const models:ModelPrice[]=publicModels.map(r=>({...r,evidence:'current' as const,...releaseByModel.get(key(r.model))})).sort((a,b)=>(b.released_at_ms??-Infinity)-(a.released_at_ms??-Infinity)||a.model.localeCompare(b.model)||a.key.localeCompare(b.key));
const history:ModelPrice[]=models.filter(r=>r.provider==='codex'&&r.model==='gpt-6.1-sol'&&r.mode==='Standard · 短上下文').map(r=>({...r,key:'synthetic:history:gpt-6.1-sol',evidence:'historical',version:'openai-api-2026-09-29',cache_write_price:null,effective_from_ms:Date.UTC(2026,8,29),notes:'合成历史证据，演示版本查阅；不读取实际历史费用。'}));
export const reviewCatalog:Catalog={version:'reference-2026-10-02',models:[...models,...history,{key:'codex:observed:unpublished-model',provider:'codex',model:'unpublished-model',mode:'已观测 · 参考价未知',currency:'USD',unit:'未知',input_price:null,cached_price:null,cache_write_price:null,output_price:null,version:'',source_url:'',verified_at_ms:0,effective_from_ms:null,evidence:'observed',notes:'设计用未知型号；没有发布日期和公开价格。'}],plans:publicPlans as PlanPrice[]};
const definitions=modelRows.slice(0,4).map((m,i)=>({...m,provider:m.provider.toLowerCase(),model:['gpt-6.1-sol','gpt-4.1','Claude Sonnet 5.5','grok-4.7'][i]}));
const totals=(total:string|null,cost:string|null):Totals=>({...sampleTotals,total_tokens:total,cost_micro_usd:cost,cost_status:cost===null?'unknown':'complete',input_tokens:null,cached_tokens:null,output_tokens:null,reasoning_tokens:null,cache_write_tokens:null,sessions:total===null?0:2,invocations:total===null?0:6});
function sum(rows:Totals[]):Totals {
 const values=(field:'total_tokens'|'cost_micro_usd')=>rows.every(r=>r[field]===null)?null:rows.reduce((value,r)=>value+BigInt(r[field]??'0'),0n).toString();
 return {...totals(values('total_tokens'),values('cost_micro_usd')),cost_status:rows.some(r=>r.cost_micro_usd===null)?'partial':'complete',sessions:rows.reduce((n,r)=>n+r.sessions,0),invocations:rows.reduce((n,r)=>n+r.invocations,0)};
}
// 仅在生成合成 API 样本时汇总；业务页面继续使用 Server 返回值。
export function reviewUsage(filter:StatsFilter):Usage {
 const selected=definitions.filter(m=>!filter.provider||m.provider===filter.provider).filter(m=>!filter.model||m.model===filter.model);
 const dates=modelSeries[0].points.map((_,i)=>dayjs.utc('2026-07-06').add(i,'day').format('YYYY-MM-DD')).filter(d=>d>=filter.start_date&&d<filter.end_date_exclusive);
 const modelDays=selected.flatMap(m=>dates.map(date=>{const series=modelSeries[definitions.indexOf(m)],i=dayjs.utc(date).diff(dayjs.utc('2026-07-06'),'day');return {provider:m.provider,model:m.model,date,totals:totals(series.points[i]===null?null:String(series.points[i]),series.costs[i]===null?null:String(series.costs[i]))};}));
 const byModel=selected.map(m=>({provider:m.provider,model:m.model,totals:sum(modelDays.filter(r=>r.model===m.model).map(r=>r.totals))}));
 const trend=dates.map(date=>({date,start_at_ms:dayjs.utc(date).valueOf(),totals:sum(modelDays.filter(r=>r.date===date).map(r=>r.totals))}));
 return {range:{start_at_ms:dayjs.tz(filter.start_date,filter.time_zone).valueOf(),end_at_ms:dayjs.tz(filter.end_date_exclusive,filter.time_zone).valueOf(),time_zone:filter.time_zone},scope:'synthetic-design-fixture',totals:sum(byModel.map(r=>r.totals)),coverage:{...summaryFixture().coverage,collected_at_ms:reviewNow,stale:false},models:byModel,model_days:modelDays,trend,providers:[...new Set(selected.map(m=>m.provider))].map(provider=>({key:provider,name:provider,totals:sum(byModel.filter(m=>m.provider===provider).map(r=>r.totals))})),cursor_pools:[],model_cost_rounding_delta_micro_usd:'0',trend_cost_rounding_delta_micro_usd:'0'};
}
export function reviewSummary(filter:StatsFilter):Summary {
 const usage=reviewUsage(filter);
 const annual:Day[]=heatmapDays.map((d,i)=>({...d,totals:totals(d.totals.total_tokens,d.totals.total_tokens===null?null:d.totals.total_tokens==='0'?'0':String(110_000+(i*1234)%60_000))}));
 const annualTotals=sum(annual.map(d=>d.totals));
 return {...summaryFixture(),range:usage.range,scope:usage.scope,totals:usage.totals,coverage:usage.coverage,providers:usage.providers,models:usage.models.map(m=>({key:`${m.provider}:${m.model}`,name:m.model,totals:m.totals})),trend:usage.trend,heatmap:annual,heatmap_totals:annualTotals,heatmap_range:{start_at_ms:Date.UTC(2025,9,4),end_at_ms:Date.UTC(2026,9,4),time_zone:filter.time_zone},heatmap_activity:{total_tokens:annualTotals.total_tokens,peak_daily_tokens:'2420000',active_days:'235',current_streak_days:'5',longest_streak_days:'5',observed_days:330,unknown_days:35},devices:[{key:'machine-one',name:'合成采集机',totals:usage.totals}],tools:[{key:'exec',name:'exec_command',totals:usage.totals}],skills:[{key:'antd',name:'antd',totals:usage.totals}]};
}
export function reviewQuota(scenario:ReviewScenario) {
 const q=quotaFixture();
 q.evaluated_at_ms=reviewNow;
 const w=q.windows[0];
 w.current.used_percent=70;w.current.remaining_percent=30;
 w.current.window_start_at_ms=quotaNow-14400000;
 w.current.freshness=scenario==='current'?'fresh':'expired_unknown';
 if(scenario==='current'){w.current.observed_at_ms=reviewNow;w.current.resets_at_ms=reviewNow+3600000;w.current.window_start_at_ms=reviewNow-14400000;w.current.snapshot_reset_remaining_ms=3600000;}
 if(scenario==='conflict'){w.current.conflict=true;w.current.reason='source_conflict';}
 if(scenario==='unknown'){w.current.used_percent=null;w.current.remaining_percent=null;w.current.observed_at_ms=null;w.current.reason='unavailable';w.observations=[];}
 w.observations=w.observations.map(p=>({...p,used_percent:70,observed_at_ms:w.current.observed_at_ms??quotaNow,resets_at_ms:w.current.resets_at_ms}));
 q.credits[0]={...q.credits[0],observed_inventory:'3',snapshot_available_inventory:'2',snapshot_next_expires_at_ms:quotaNow+86400000};
 if(scenario==='unknown')q.credits[0]={...q.credits[0],observed_inventory:null,snapshot_available_inventory:null,snapshot_next_expires_at_ms:null,expiry_schedule:[],details_status:'unavailable'};
 return q;
}
export function reviewPace(scenario:ReviewScenario) {
 const p=paceFixture(),q=reviewQuota(scenario),w=p.windows[0];
 p.evaluated_at_ms=reviewNow;
 w.current=q.windows[0].current;w.snapshot_at_ms=w.current.observed_at_ms;
 w.elapsed_percent=80;w.pace_delta_pp=-10;
 w.current_points=[{observed_at_ms:quotaNow-3600000,elapsed_percent:60,used_percent:55,remaining_percent:45,linked_history:false},{observed_at_ms:w.current.observed_at_ms??quotaNow,elapsed_percent:80,used_percent:70,remaining_percent:30,linked_history:false}];
 if(scenario==='conflict'){w.unknown_reason='source_conflict';w.forecast.unknown_reason='source_conflict';}
 if(scenario==='unknown'){w.current_points=[];w.elapsed_percent=null;w.pace_delta_pp=null;w.unknown_reason='unavailable';w.forecast.unknown_reason='unavailable';}
 return p;
}
export const reviewDevices:Device[]=[{id:'machine-one',name:'合成采集机',revoked_at_ms:null,last_received_at_ms:reviewNow,providers:[{provider:'codex',version:'dev · 合成样本',collected_at_ms:quotaNow,coverage_start_ms:Date.UTC(2025,9,4),coverage_end_ms:quotaNow,pending_batches:8,status:'ready',received_at_ms:reviewNow,stale:true,sync_state:'ready',sync_checked_at_ms:reviewNow,full_sync_state:'running'},{provider:'cursor',version:'dev · 合成样本',collected_at_ms:quotaNow,coverage_start_ms:Date.UTC(2025,9,4),coverage_end_ms:quotaNow,pending_batches:0,status:'source_unavailable',received_at_ms:reviewNow,stale:true,sync_state:'source_unavailable',sync_checked_at_ms:reviewNow,full_sync_state:'running'}]}];
export const reviewClients:Client[]=[{id:'story-browser',name:'Storybook · 合成管理浏览器',purpose:'admin',created_at_ms:quotaNow,expires_at_ms:null,revoked_at_ms:null,last_received_at_ms:null},{id:'machine-one',name:'合成采集机',purpose:'collector',created_at_ms:quotaNow,expires_at_ms:null,revoked_at_ms:null,last_received_at_ms:reviewNow}];
export function reviewSubscription(account:string):Subscription {return {account_key:account,revision:'1',alias:null,automatic_plan:account==='account-one'?'Pro':null,manual_plan:null,resolved_plan:account==='account-one'?'Pro':null,date_kind:'monthly_renewal',renewal_day:15,membership_date:null,next_date:'2026-10-15',day_delta:12,date_state:'future',time_zone:'Asia/Shanghai',updated_at_ms:quotaNow};}
export const reviewSessions:SessionRecord[]=sessions.map(s=>({id:s.key,provider:s.provider.toLowerCase(),session_id:s.key,title:s.title,session_kind:'interactive',project_id:projects.find(p=>p.name===s.project)?.key??'unknown',project_group_id:projects.find(p=>p.name===s.project)?.key??'unknown',project_name:s.project,created_at_ms:quotaNow-86400000,last_active_at_ms:quotaNow,collected_at_ms:quotaNow,complete:true,conflict:false,sources:[{id:`source-${s.key}`,client_id:'machine-one',client_name:'合成采集机',collected_at_ms:quotaNow,revision:2,source_kind:'synthetic',complete:true,deleted:false}],totals:totals(s.tokens,s.cost)}));
export const reviewProjects:ProjectRecord[]=projects.map(p=>({id:p.key,name:p.name,members:[p.key],totals:totals(p.tokens,p.cost),last_active_at_ms:quotaNow,conflict:false}));
