import type {Catalog,ModelPrice,PlanPrice} from '../api/catalog';
import type {Client} from '../api/access';
import type {Device,Summary,Totals,Day,StatsFilter} from '../api/statistics';
import type {Usage} from '../api/usage';
import type {Subscription} from '../api/subscriptions';
import type {CacheHitRateStats,SessionRecord,ProjectRecord,ThroughputStats} from '../api/records';
import type {PaceWindow,QuotaWindow} from '../api/quotas';
import {quotaFixture,paceFixture,quotaNow} from '../test/quotaFixture';
import {summaryFixture} from '../test/statisticsFixture';
import {dayjs} from '../format';
import {heatmapDays,modelSeries,modelRows,sampleTotals,sessions,projects} from './fixtures';
import publicModels from '../../../internal/service/catalog_srv/models-20261027.json';
import publicModelsAdditions from '../../../internal/service/catalog_srv/models-20261118.json';
import publicPlans from '../../../internal/service/catalog_srv/plans-20261002.json';
import publicPlansAdditions from '../../../internal/service/catalog_srv/plans-20261126.json';
import releases from '../../../internal/service/catalog_srv/releases-20261027.json';
import releasesAdditions from '../../../internal/service/catalog_srv/releases-20261118.json';
import releasesAgy from '../../../internal/service/catalog_srv/releases-20261126.json';

export type ReviewScenario='current'|'expired'|'conflict'|'unknown'|'empty'|'error'|'anonymous';
export const reviewNow=Date.UTC(2026,10,26,8,30);
const agyVerifiedAt=Date.UTC(2026,10,25);
const agyCarbonPhase2At=Date.UTC(2026,11,1);
const key=(model:string)=>model.toLowerCase().replace(/\s+/g,'-');
const releaseByModel=new Map([...releases,...releasesAdditions,...releasesAgy].flatMap(r=>r.models.map(model=>[key(model),{released_at_ms:Date.parse(`${r.released_on}T00:00:00Z`),release_source_url:r.source_url}] as const)));
const agyRelease=(model:string)=>releaseByModel.get(key(model))??releaseByModel.get(key(model).replace(/-(?:thinking|xhigh|high|medium|low|max|tiered|extra-low|preview)$/,''));
const models:ModelPrice[]=[...publicModels,...publicModelsAdditions].map(r=>({...r,evidence:'current' as const,...releaseByModel.get(key(r.model))})).sort((a,b)=>(b.released_at_ms??-Infinity)-(a.released_at_ms??-Infinity)||a.model.localeCompare(b.model)||a.key.localeCompare(b.key));
const history:ModelPrice[]=models.filter(r=>r.provider==='codex'&&r.model==='gpt-6.1-sol'&&r.mode==='Standard · 短上下文').map(r=>({...r,key:'synthetic:history:gpt-6.1-sol',evidence:'historical',version:'openai-api-2026-09-29',cache_write_price:null,effective_from_ms:Date.UTC(2026,8,29),notes:'合成历史证据，演示版本查阅；不读取实际历史费用。'}));
const agyCatalogNotes='Antigravity 公开 API 参考价格目录，按请求时间适用生效阶段单价；不是实际订阅账单，未收录模型保持未定价。';
const agyModels:ModelPrice[]=[
 {key:'agy:gemini-4-carbon:agy-api-2026-11-25:phase1:reference',provider:'agy',model:'gemini-4-carbon',mode:'Antigravity · API 参考目录 (phase1)',currency:'USD',unit:'1M tokens',input_price:'2',cached_price:'0.1',cache_write_price:null,output_price:'10',version:'agy-api-2026-11-25:phase1',source_url:'agy_provider_api_pricing',verified_at_ms:agyVerifiedAt,effective_from_ms:agyVerifiedAt,evidence:'current',notes:agyCatalogNotes},
 {key:'agy:gemini-4-carbon:agy-api-2026-11-25:phase1',provider:'agy',model:'gemini-4-carbon',mode:'Antigravity · API 公价估算 (phase1)',currency:'USD',unit:'1M tokens',input_price:'2',cached_price:'0.1',cache_write_price:null,output_price:'10',version:'agy-api-2026-11-25:phase1',source_url:'agy_provider_api_pricing',verified_at_ms:agyVerifiedAt,effective_from_ms:agyVerifiedAt,evidence:'historical',notes:agyCatalogNotes},
 {key:'agy:gemini-4-carbon:agy-api-2026-11-25:phase2',provider:'agy',model:'gemini-4-carbon',mode:'Antigravity · API 公价估算 (phase2)',currency:'USD',unit:'1M tokens',input_price:'4',cached_price:'0.2',cache_write_price:null,output_price:'20',version:'agy-api-2026-11-25:phase2',source_url:'agy_provider_api_pricing',verified_at_ms:agyVerifiedAt,effective_from_ms:agyCarbonPhase2At,evidence:'historical',notes:agyCatalogNotes},
 {key:'agy:gemini-3.1-pro-high:agy-api-2026-11-25:reference',provider:'agy',model:'gemini-3.1-pro-high',mode:'Antigravity · API 参考目录',currency:'USD',unit:'1M tokens',input_price:'2',cached_price:'0.2',cache_write_price:null,output_price:'12',version:'agy-api-2026-11-25',source_url:'agy_provider_api_pricing',verified_at_ms:agyVerifiedAt,effective_from_ms:agyVerifiedAt,evidence:'current',notes:agyCatalogNotes,...agyRelease('gemini-3.1-pro-high')},
 {key:'agy:claude-opus-4-6-thinking:agy-api-2026-11-25:reference',provider:'agy',model:'claude-opus-4-6-thinking',mode:'Antigravity · API 参考目录',currency:'USD',unit:'1M tokens',input_price:'5',cached_price:'0.5',cache_write_price:null,output_price:'25',version:'agy-api-2026-11-25',source_url:'agy_provider_api_pricing',verified_at_ms:agyVerifiedAt,effective_from_ms:agyVerifiedAt,evidence:'current',notes:agyCatalogNotes,...agyRelease('claude-opus-4-6-thinking')},
 {key:'agy:observed:gemini-experimental-preview',provider:'agy',model:'gemini-experimental-preview',mode:'已观测 · 参考价未知',currency:'USD',unit:'未知',input_price:null,cached_price:null,cache_write_price:null,output_price:null,version:'',source_url:'',verified_at_ms:0,effective_from_ms:null,evidence:'observed',notes:'保留实际模型名；没有匹配公开价格证据，不猜价或视为免费。'},
];
export const reviewCatalog:Catalog={version:'reference-2026-11-18',models:[...models,...history,...agyModels,{key:'codex:observed:unpublished-model',provider:'codex',model:'unpublished-model',mode:'已观测 · 参考价未知',currency:'USD',unit:'未知',input_price:null,cached_price:null,cache_write_price:null,output_price:null,version:'',source_url:'',verified_at_ms:0,effective_from_ms:null,evidence:'observed',notes:'设计用未知型号；没有发布日期和公开价格。'}],plans:[...publicPlans,...publicPlansAdditions] as PlanPrice[]};

const definitions=[
 ...modelRows.slice(0,4).map((m,i)=>({...m,provider:m.provider.toLowerCase(),model:['gpt-6.1-sol','gpt-4.1','Claude Sonnet 5.5','grok-4.7'][i]})),
 {key:'agy:gemini-4-carbon',provider:'agy',model:'gemini-4-carbon',tokens:'9240000',cost:'11400000',cache:'86.5%',input:'7920000',output:'1320000',sessions:18,inputPrice:'2.00',cachedPrice:'0.10',outputPrice:'10.00',color:'#6c5ce7'},
 {key:'agy:claude-opus-4-6-thinking',provider:'agy',model:'claude-opus-4-6-thinking',tokens:'6180000',cost:'14800000',cache:'79.2%',input:'5120000',output:'1060000',sessions:12,inputPrice:'5.00',cachedPrice:'0.50',outputPrice:'25.00',color:'#8f7ff0'},
];
const agySeries=[
 {key:'agy:gemini-4-carbon',points:modelSeries[0].points.map(v=>v===null?null:Math.round(v*0.62)),costs:modelSeries[0].costs.map(v=>v===null?null:Math.round(v*0.58))},
 {key:'agy:claude-opus-4-6-thinking',points:modelSeries[2].points.map(v=>v===null?null:Math.round(v*0.74)),costs:modelSeries[2].costs.map(v=>v===null?null:Math.round(v*0.92))},
];
const allSeries=[...modelSeries,...agySeries];
const sampleCacheRate=(basisPoints:string,input:string,cached:string):CacheHitRateStats=>({basis_points:basisPoints,input_tokens:input,cached_input_tokens:cached,unit:'basis_points',basis:'cached_input_over_total_input',status:'complete',reason:'observed',source_client_id:'machine-one',conflict:false});
const sampleThroughput=(milliTps:string,output:string,durationMs:string,turns:string):ThroughputStats=>({average_output_milli_tps:milliTps,output_tokens:output,active_duration_ms:durationMs,included_turns:turns,excluded_turns:'0',open_turns:'0',unattributed_events:'0',status:'complete',reason:'observed',duration_source:'agy_step_metadata',basis:'output_tokens_over_active_duration',average_unit:'milli_tps',duration_unit:'ms',source_client_id:'machine-one',conflict:false});
const totals=(total:string|null,cost:string|null):Totals=>({...sampleTotals,total_tokens:total,cost_micro_usd:cost,cost_status:cost===null?'unknown':'complete',input_tokens:null,cached_tokens:null,output_tokens:null,reasoning_tokens:null,cache_write_tokens:null,sessions:total===null?0:2,invocations:total===null?0:6});
function sum(rows:Totals[]):Totals {
 const values=(field:'total_tokens'|'cost_micro_usd')=>rows.every(r=>r[field]===null)?null:rows.reduce((value,r)=>value+BigInt(r[field]??'0'),0n).toString();
 return {...totals(values('total_tokens'),values('cost_micro_usd')),cost_status:rows.some(r=>r.cost_micro_usd===null)?'partial':'complete',sessions:rows.reduce((n,r)=>n+r.sessions,0),invocations:rows.reduce((n,r)=>n+r.invocations,0)};
}
// 仅在生成合成 API 样本时汇总；业务页面继续使用 Server 返回值。
export function reviewUsage(filter:StatsFilter):Usage {
 const selected=definitions.filter(m=>!filter.provider||m.provider===filter.provider).filter(m=>!filter.model||m.model===filter.model);
 const dates=Array.from({length:dayjs(filter.end_date_exclusive).diff(dayjs(filter.start_date),'day')},(_,i)=>dayjs(filter.start_date).add(i,'day').format('YYYY-MM-DD'));
 const modelDays=selected.flatMap(m=>dates.map(date=>{const series=allSeries[definitions.indexOf(m)],i=dates.indexOf(date)%series.points.length;return {provider:m.provider,model:m.model,date,totals:totals(series.points[i]===null?null:String(series.points[i]),series.costs[i]===null?null:String(series.costs[i]))};}));
 const byModel=selected.map(m=>({provider:m.provider,model:m.model,cache_hit_rate:m.provider==='agy'?sampleCacheRate(m.model==='gemini-4-carbon'?'8650':'7920','7920000','6850800'):sampleCacheRate('8120','15600000','12667200'),totals:sum(modelDays.filter(r=>r.model===m.model).map(r=>r.totals))}));
 const trend=dates.map(date=>({date,start_at_ms:dayjs.utc(date).valueOf(),totals:sum(modelDays.filter(r=>r.date===date).map(r=>r.totals))}));
 return {range:{start_at_ms:dayjs.tz(filter.start_date,filter.time_zone).valueOf(),end_at_ms:dayjs.tz(filter.end_date_exclusive,filter.time_zone).valueOf(),time_zone:filter.time_zone},scope:'synthetic-design-fixture',cache_hit_rate:selected.length?sampleCacheRate(filter.provider==='agy'?'8360':'8240','23520000','19662720'):null,totals:sum(byModel.map(r=>r.totals)),coverage:{...summaryFixture().coverage,collected_at_ms:reviewNow,stale:false},models:byModel,model_days:modelDays,trend,providers:[...new Set(selected.map(m=>m.provider))].map(provider=>({key:provider,name:provider,totals:sum(byModel.filter(m=>m.provider===provider).map(r=>r.totals))})),cursor_pools:[],model_cost_rounding_delta_micro_usd:'0',trend_cost_rounding_delta_micro_usd:'0'};
}
export function reviewSummary(filter:StatsFilter):Summary {
 const usage=reviewUsage(filter);
 const annual:Day[]=heatmapDays.map((d,i)=>({...d,totals:totals(d.totals.total_tokens,d.totals.total_tokens===null?null:d.totals.total_tokens==='0'?'0':String(110_000+(i*1234)%60_000))}));
 const annualTotals=sum(annual.map(d=>d.totals));
 const hourly=usage.trend.length===1;
 const start=dayjs.tz(filter.start_date,filter.time_zone),end=dayjs.tz(filter.end_date_exclusive,filter.time_zone);
 const count=Math.round(end.diff(start,'hour'));
 const weights=[5,10,7,2,6,14,18,16,10,12];
 const activity=hourly?Array.from({length:count},(_,hour)=>({start_at_ms:start.add(hour,'hour').valueOf(),end_at_ms:start.add(hour+1,'hour').valueOf(),tokens:hour<9||hour>18?null:usage.totals.total_tokens===null?null:(BigInt(usage.totals.total_tokens)*BigInt(weights[hour-9])/100n).toString(),sessions:hour<9||hour>18?null:'2'})):usage.trend.map(d=>({start_at_ms:dayjs.tz(d.date,filter.time_zone).valueOf(),end_at_ms:dayjs.tz(d.date,filter.time_zone).add(1,'day').valueOf(),tokens:d.totals.total_tokens,sessions:d.totals.total_tokens===null?null:String(d.totals.sessions)}));
 const top=reviewSessions.filter(r=>!filter.provider||r.provider===filter.provider).filter(r=>!filter.client_id||r.sources.some(s=>s.client_id===filter.client_id)).sort((a,b)=>Number(BigInt(b.totals.total_tokens??'0')-BigInt(a.totals.total_tokens??'0'))).slice(0,5).map((r,i)=>({...r,totals:{...r.totals,total_tokens:usage.totals.total_tokens===null?null:(BigInt(usage.totals.total_tokens)*BigInt([30,20,10,8,5][i])/100n).toString()}}));
 return {...summaryFixture(),activity_granularity:hourly?'hour':'day',activity_timeline:activity,top_sessions:top,weekday_hours:Array.from({length:168},(_,i)=>{const hour=i%24,weekday=Math.floor(i/24);return {weekday,hour,session_count:weekday===start.day()&&hour>=9&&hour<=18?'2':null,tokens:weekday===start.day()&&hour>=9&&hour<=18?(BigInt(usage.totals.total_tokens??'0')*BigInt(weights[hour-9])/100n).toString():null,sessions:weekday===start.day()&&hour>=9&&hour<=18?2:0};}),range:usage.range,scope:usage.scope,totals:usage.totals,coverage:usage.coverage,providers:usage.providers,models:usage.models.map(m=>({key:`${m.provider}:${m.model}`,name:m.model,totals:m.totals})),trend:usage.trend,heatmap:annual,heatmap_totals:annualTotals,heatmap_range:{start_at_ms:Date.UTC(2025,10,27),end_at_ms:Date.UTC(2026,10,27),time_zone:filter.time_zone},heatmap_activity:{total_tokens:annualTotals.total_tokens,peak_daily_tokens:'2420000',active_days:'235',current_streak_days:'5',longest_streak_days:'5',observed_days:330,unknown_days:35},devices:[{key:'machine-one',name:'SQMC03',totals:usage.totals}],tools:[],skills:[]};
}

function buildAgyWindows(scenario:ReviewScenario):QuotaWindow[] {
 const make=(limitId:string,windowKind:'primary'|'secondary',windowMinutes:number,usedPercent:number,defaultFreshness:'fresh'|'stale'|'expired_unknown',multiDevice=false):QuotaWindow=>{
  const key=`agy:${limitId}:${windowKind}`;
  const durationMs=windowMinutes*60_000;
  let freshness:string=defaultFreshness;
  let conflict=false;
  let reason='trusted';
  let used:number|null=usedPercent;
  let remaining:number|null=100-usedPercent;
  let observedAt:number|null=reviewNow-(defaultFreshness==='stale'?7_200_000:120_000);
  let resetsAt:number|null=defaultFreshness==='expired_unknown'?reviewNow-1_800_000:reviewNow+Math.round(durationMs*0.35);
  let resetRemaining:number|null=resetsAt!==null&&resetsAt>reviewNow?resetsAt-reviewNow:null;
  if(scenario==='expired'){
   freshness=windowKind==='primary'?'stale':'expired_unknown';
   observedAt=reviewNow-14_400_000;
   resetsAt=windowKind==='primary'?reviewNow+3_600_000:reviewNow-3_600_000;
   resetRemaining=null;
  }
  if(scenario==='conflict'){
   conflict=true;
   freshness='fresh';
   reason='source_conflict';
   resetRemaining=null;
  }
  if(scenario==='unknown'){
   used=null;
   remaining=null;
   observedAt=null;
   resetsAt=null;
   resetRemaining=null;
   freshness='unknown';
   reason='unavailable';
  }
  const obs=scenario==='unknown'?[]:[
   {id:`obs-${key}-1`,client_id:'machine-one',client_name:'SQMC03',observed_at_ms:observedAt??reviewNow,received_at_ms:(observedAt??reviewNow)+4_000,used_percent:used,resets_at_ms:resetsAt,window_start_at_ms:resetsAt===null?null:resetsAt-durationMs,source:'agy_ls_rpc',validity:'accepted',history_origin:'confirmed',disposition:'selected',reason:null,cycle_id:`cycle-${key}`,canonical_reset_at_ms:resetsAt},
   ...(multiDevice?[{id:`obs-${key}-2`,client_id:'machine-2',client_name:'SQMC04',observed_at_ms:(observedAt??reviewNow)-60_000,received_at_ms:(observedAt??reviewNow)-55_000,used_percent:used,resets_at_ms:resetsAt,window_start_at_ms:resetsAt===null?null:resetsAt-durationMs,source:'agy_ls_rpc',validity:'accepted',history_origin:'confirmed',disposition:'corroborated',reason:null,cycle_id:`cycle-${key}`,canonical_reset_at_ms:resetsAt}]:[]),
  ];
  return {
   key,provider:'agy',account_key:'account-agy',identity_state:'confirmed',limit_id:limitId,window_kind:windowKind,window_minutes:windowMinutes,
   current:{used_percent:used,remaining_percent:remaining,window_start_at_ms:resetsAt===null?null:resetsAt-durationMs,resets_at_ms:resetsAt,reset_remaining_ms:resetRemaining,snapshot_reset_remaining_ms:resetRemaining,observed_at_ms:observedAt,freshness,conflict,reason,selected_observation_id:obs[0]?.id??null,selected_client_id:obs[0]?.client_id??null,source:'agy_ls_rpc'},
   cycles:[],observations:obs,coverage:'observed_only',
  };
 };
 return [
  make('agy.gemini','primary',300,28,'fresh',true),
  make('agy.gemini','secondary',10080,16,'fresh',true),
  make('agy.argon','primary',300,9,'fresh',false),
  make('agy.carbon','primary',300,62,'fresh',true),
  make('agy.carbon','secondary',10080,39,'stale',false),
  make('agy.claude_gpt','primary',300,82,'fresh',true),
  make('agy.claude_gpt','secondary',10080,76,'expired_unknown',false),
 ];
}

export function reviewQuota(scenario:ReviewScenario) {
 const q=quotaFixture();
 q.accounts[1].plan='Plus';
 q.accounts.push({key:'account-agy',provider:'agy',raw_id:'agy_account_8f4a2c1e90b3',email:null,plan:'Google AI Ultra',collected_at_ms:reviewNow});
 q.evaluated_at_ms=reviewNow;
 q.paid_credits=[{account_key:q.accounts[0].key,credit:{
  limitId:'codex',state:scenario==='unknown'?'unavailable':'known',
  ...(scenario==='unknown'||scenario==='conflict'?{}:{balance:'62500.0000000000',hasCredits:true,unlimited:false}),
  observedAtMs:scenario==='unknown'?0:reviewNow,checkedAtMs:reviewNow,
  freshness:scenario==='unknown'?'unknown':scenario==='conflict'?'conflict':scenario==='current'?'fresh':'stale',
  consumptionState:'source_unavailable',history:scenario==='unknown'?[]:[
   {observedAtMs:reviewNow-2*86400000,balance:'65000.875',unlimited:false},
   {observedAtMs:reviewNow-86400000,balance:'64000.0000000000',unlimited:false},
   {observedAtMs:reviewNow,...(scenario==='conflict'?{}:{balance:'62500.0000000000'}),unlimited:false},
  ],
 }}];
 const w=q.windows[0];
 w.current.used_percent=70;w.current.remaining_percent=30;
 w.current.window_start_at_ms=quotaNow-14400000;
 w.current.freshness=scenario==='current'?'fresh':'expired_unknown';
 if(scenario==='current'){w.current.observed_at_ms=reviewNow;w.current.resets_at_ms=reviewNow+120960000;w.current.window_start_at_ms=reviewNow-483840000;w.current.snapshot_reset_remaining_ms=120960000;}
 if(scenario==='conflict'){w.current.conflict=true;w.current.reason='source_conflict';}
 if(scenario==='unknown'){w.current.used_percent=null;w.current.remaining_percent=null;w.current.observed_at_ms=null;w.current.reason='unavailable';w.observations=[];}
 w.observations=w.observations.map(p=>({...p,used_percent:70,observed_at_ms:w.current.observed_at_ms??quotaNow,resets_at_ms:w.current.resets_at_ms}));
 q.windows.push(...buildAgyWindows(scenario));
 q.credits[0]={...q.credits[0],observed_inventory:'3',snapshot_available_inventory:'2',snapshot_next_expires_at_ms:quotaNow+86400000};
 if(scenario==='unknown')q.credits[0]={...q.credits[0],observed_inventory:null,snapshot_available_inventory:null,snapshot_next_expires_at_ms:null,expiry_schedule:[],details_status:'unavailable'};
 return q;
}
export function reviewPace(scenario:ReviewScenario) {
 const p=paceFixture(),q=reviewQuota(scenario),w=p.windows[0];
 p.evaluated_at_ms=reviewNow;
 p.accounts=q.accounts;
 w.current=q.windows[0].current;w.snapshot_at_ms=w.current.observed_at_ms;
 w.elapsed_percent=80;w.pace_delta_pp=-10;
 w.current_points=[{observed_at_ms:(w.current.observed_at_ms??quotaNow)-120960000,elapsed_percent:60,used_percent:55,remaining_percent:45,linked_history:false},{observed_at_ms:w.current.observed_at_ms??quotaNow,elapsed_percent:80,used_percent:70,remaining_percent:30,linked_history:false}];
 if(scenario==='conflict'){w.unknown_reason='source_conflict';w.forecast.unknown_reason='source_conflict';}
 if(scenario==='unknown'){w.current_points=[];w.elapsed_percent=null;w.pace_delta_pp=null;w.unknown_reason='unavailable';w.forecast.unknown_reason='unavailable';}
 const agyPaceWindows:PaceWindow[]=q.windows.filter(item=>item.provider==='agy').map(item=>{
  const used=item.current.used_percent;
  const remaining=item.current.remaining_percent;
  const elapsed=scenario==='unknown'?null:65;
  const delta=used===null||elapsed===null?null:used-elapsed;
  const ts=item.current.observed_at_ms??reviewNow;
  const unavailable=scenario==='unknown'||scenario==='conflict'||item.current.freshness==='expired_unknown';
  return {
   ...item,
   snapshot_at_ms:item.current.observed_at_ms,
   elapsed_percent:unavailable?null:elapsed,
   pace_delta_pp:unavailable?null:delta,
   forecast:{state:unavailable?'unavailable':'available',method:unavailable?'none':'linear_recent',exhaust_at_ms:unavailable?null:reviewNow+3_600_000,lead_before_reset_ms:unavailable?null:1_800_000,evidence_count:unavailable?0:3,evidence_span_ms:unavailable?0:7_200_000,unknown_reason:scenario==='conflict'?'source_conflict':scenario==='unknown'?'unavailable':item.current.freshness==='expired_unknown'?'expired_unknown':null},
   current_points:used===null||remaining===null?[]:[
    {observed_at_ms:ts-7_200_000,elapsed_percent:25,used_percent:Math.max(0,used-24),remaining_percent:Math.min(100,remaining+24),linked_history:false},
    {observed_at_ms:ts-3_600_000,elapsed_percent:45,used_percent:Math.max(0,used-11),remaining_percent:Math.min(100,remaining+11),linked_history:false},
    {observed_at_ms:ts,elapsed_percent:65,used_percent:used,remaining_percent:remaining,linked_history:false},
   ],
   previous_cycle:null,historical_cycles:[],history_band:[],history_cycle_count:0,
   previous_remaining_at_elapsed:null,history_median_remaining_at_elapsed:null,
   unknown_reason:scenario==='conflict'?'source_conflict':scenario==='unknown'?'unavailable':item.current.freshness==='expired_unknown'?'expired_unknown':null,
  };
 });
 p.windows.push(...agyPaceWindows);
 return p;
}
export const reviewDevices:Device[]=[{id:'machine-one',name:'SQMC03',revoked_at_ms:null,last_received_at_ms:reviewNow,providers:[{provider:'codex',version:'dev · 合成样本',collected_at_ms:quotaNow,coverage_start_ms:Date.UTC(2025,9,4),coverage_end_ms:quotaNow,pending_batches:8,status:'ready',received_at_ms:reviewNow,stale:true,sync_state:'ready',sync_checked_at_ms:reviewNow,full_sync_state:'running'},{provider:'cursor',version:'dev · 合成样本',collected_at_ms:quotaNow,coverage_start_ms:Date.UTC(2025,9,4),coverage_end_ms:quotaNow,pending_batches:0,status:'source_unavailable',received_at_ms:reviewNow,stale:true,sync_state:'source_unavailable',sync_checked_at_ms:reviewNow,full_sync_state:'running'},{provider:'agy',version:'dev · 合成样本',collected_at_ms:reviewNow-120_000,coverage_start_ms:Date.UTC(2026,10,1),coverage_end_ms:reviewNow-120_000,pending_batches:0,status:'ready',received_at_ms:reviewNow,stale:false,sync_state:'ready',sync_checked_at_ms:reviewNow,full_sync_state:'completed'}]}];
export const reviewClients:Client[]=[{id:'story-browser',name:'Storybook · 合成管理浏览器',purpose:'admin',created_at_ms:quotaNow,expires_at_ms:null,revoked_at_ms:null,last_received_at_ms:null},{id:'machine-one',name:'SQMC03',purpose:'collector',created_at_ms:quotaNow,expires_at_ms:null,revoked_at_ms:null,last_received_at_ms:reviewNow}];
export function reviewSubscription(account:string):Subscription {
 const plan=account==='account-agy'?'Google AI Ultra':account==='account-one'?'Pro':'Plus';
 return {account_key:account,revision:'1',alias:null,automatic_plan:plan,manual_plan:null,resolved_plan:plan,date_kind:'monthly_renewal',renewal_day:15,membership_date:null,next_date:'2026-12-15',day_delta:19,date_state:'future',time_zone:'Asia/Shanghai',updated_at_ms:reviewNow};
}
const baseSessions:SessionRecord[]=sessions.map(s=>({id:s.key,provider:s.provider.toLowerCase(),session_id:s.key,title:s.title,session_kind:'interactive',project_id:projects.find(p=>p.name===s.project)?.key??'unknown',project_group_id:projects.find(p=>p.name===s.project)?.key??'unknown',project_name:s.project,created_at_ms:quotaNow-86400000,last_active_at_ms:quotaNow,collected_at_ms:quotaNow,complete:true,conflict:false,sources:[{id:`source-${s.key}`,client_id:'machine-one',client_name:'SQMC03',collected_at_ms:reviewNow,revision:2,source_kind:'synthetic',complete:true,deleted:false}],totals:totals(s.tokens,s.cost)}));
const agySessions:SessionRecord[]=[
 {id:'agy-session-main',provider:'agy',session_id:'agy-conv-9d2a1c',title:'Antigravity 多配额池与中心看板接入',session_kind:'interactive',session_role:'main',project_id:'pulse',project_group_id:'pulse',project_name:'Codex Pulse',created_at_ms:reviewNow-14_400_000,last_active_at_ms:reviewNow-300_000,collected_at_ms:reviewNow-120_000,complete:true,conflict:false,cache_hit_rate:sampleCacheRate('8740','4200000','3670800'),throughput:sampleThroughput('58400','540000','9246','18'),sources:[{id:'source-agy-main-1',client_id:'machine-one',client_name:'SQMC03',collected_at_ms:reviewNow-120_000,revision:4,source_kind:'agy.trajectories',complete:true,deleted:false},{id:'source-agy-main-2',client_id:'machine-2',client_name:'SQMC04',collected_at_ms:reviewNow-180_000,revision:3,source_kind:'agy.trajectories',complete:true,deleted:false}],totals:totals('4740000','6180000')},
 {id:'agy-session-sub',provider:'agy',session_id:'agy-conv-sub-41b0',title:'Windows 命名管道与进程边界测试',session_kind:'interactive',session_role:'subagent',parent_external_session_id:'agy-conv-9d2a1c',project_id:'pulse',project_group_id:'pulse',project_name:'Codex Pulse',created_at_ms:reviewNow-10_800_000,last_active_at_ms:reviewNow-1_200_000,collected_at_ms:reviewNow-120_000,complete:true,conflict:false,cache_hit_rate:sampleCacheRate('8120','1850000','1502200'),throughput:sampleThroughput('62100','280000','4508','11'),sources:[{id:'source-agy-sub-1',client_id:'machine-one',client_name:'SQMC03',collected_at_ms:reviewNow-120_000,revision:2,source_kind:'agy.trajectories',complete:true,deleted:false}],totals:totals('2130000','2840000')},
 {id:'agy-session-swarm',provider:'agy',session_id:'agy-conv-swarm-7e91',title:'中心 Web 多配额池组件重构',session_kind:'interactive',session_role:'swarm_worker',parent_external_session_id:'agy-conv-9d2a1c',project_id:'pulse',project_group_id:'pulse',project_name:'Codex Pulse',created_at_ms:reviewNow-9_000_000,last_active_at_ms:reviewNow-1_800_000,collected_at_ms:reviewNow-120_000,complete:true,conflict:false,cache_hit_rate:sampleCacheRate('7680','1420000','1090560'),throughput:sampleThroughput('49300','210000','4259','9'),sources:[{id:'source-agy-swarm-1',client_id:'machine-one',client_name:'SQMC03',collected_at_ms:reviewNow-120_000,revision:2,source_kind:'agy.summaries',complete:true,deleted:false}],totals:totals('1630000','2410000')},
 {id:'agy-session-side',provider:'agy',session_id:'agy-conv-side-12f3',title:'检查 Carbon 阶段二费率切换时间',session_kind:'interactive',session_role:'side_question',parent_external_session_id:'agy-conv-9d2a1c',project_id:'family',project_group_id:'family',project_name:'家庭服务',created_at_ms:reviewNow-5_400_000,last_active_at_ms:reviewNow-2_400_000,collected_at_ms:reviewNow-120_000,complete:false,conflict:false,cache_hit_rate:sampleCacheRate('6450','340000','219300'),throughput:null,sources:[{id:'source-agy-side-1',client_id:'machine-one',client_name:'SQMC03',collected_at_ms:reviewNow-120_000,revision:1,source_kind:'agy.transcripts',complete:false,deleted:false}],totals:totals('395000','510000')},
];
export const reviewSessions:SessionRecord[]=[...baseSessions,...agySessions];
export const reviewProjects:ProjectRecord[]=projects.map(p=>({id:p.key,name:p.name,members:[p.key],machines:[{client_id:'machine-one',client_name:'SQMC03'}],totals:totals(p.tokens,p.cost),last_active_at_ms:quotaNow,conflict:false}));

reviewDevices.push(...['SQMC04','SQMC05'].map((name,i)=>({...structuredClone(reviewDevices[0]),id:`machine-${i+2}`,name})));
reviewSessions[0].sources.push({...reviewSessions[0].sources[0],id:'source-two',client_id:'machine-2',client_name:'SQMC04'});
reviewProjects[0].machines.push({client_id:'machine-2',client_name:'SQMC04'});
export function reviewSourceUsage(filter:StatsFilter){
 const usage=reviewUsage({...filter,provider:'codex'});
 return {range:usage.range,scope:'collector_copies_may_overlap',items:filter.provider&&filter.provider!=='codex'?[]:reviewDevices.filter(d=>!filter.client_id||filter.client_id===d.id).map(d=>{const i=reviewDevices.findIndex(r=>r.id===d.id),total=usage.totals.total_tokens===null?null:BigInt(usage.totals.total_tokens)*BigInt([95,70,40][i]??50)/100n;return {machine:{client_id:d.id,client_name:d.name},totals:{...usage.totals,total_tokens:total?.toString()??null,input_tokens:total===null?null:(total*3n/4n).toString(),output_tokens:total===null?null:(total-total*3n/4n).toString(),sessions:5-i},coverage:{...usage.coverage,collected_at_ms:reviewNow,stale:i===2},revoked_at_ms:null};})};
}

reviewClients.push({...reviewClients[1],id:'revoked-machine',name:'已撤销合成机',revoked_at_ms:reviewNow},{...reviewClients[1],id:'machine-2',name:'SQMC04'});
