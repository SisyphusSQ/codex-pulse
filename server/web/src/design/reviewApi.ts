import type {StatsFilter} from '../api/statistics';
import type {SubscriptionUpdate} from '../api/subscriptions';
import {initialFilter} from '../components/StatsFilters';
import {reviewCatalog,reviewClients,reviewDevices,reviewNow,reviewPace,reviewProjects,reviewQuota,reviewSessions,reviewSubscription,reviewSummary,reviewSourceUsage,reviewUsage,type ReviewScenario} from './reviewFixtures';

// 仅由 Storybook 加载。所有 /api 请求拦在当前 iframe；未配置路由返回 501，不透传中心。
export function installReviewApi(scenario:ReviewScenario) {
 const previous=window.fetch;
 const clients=structuredClone(reviewClients);
 const subscriptions=new Map<string,ReturnType<typeof reviewSubscription>>();
 function response(data:unknown,status=200) {return new Response(JSON.stringify({code:status,data}),{status,headers:{'Content-Type':'application/json'}});}
 const mock:typeof fetch=async(input,init)=>{
  const raw=typeof input==='string'?input:input instanceof URL?input.href:input.url;
  const url=new URL(raw,location.href);
  if(!url.pathname.startsWith('/api/'))return previous(input,init);
  const signal=init?.signal??(input instanceof Request?input.signal:null);
  if(signal?.aborted)throw new DOMException('Aborted','AbortError');
  const path=url.pathname,method=init?.method??(input instanceof Request?input.method:'GET');
  const filter={...initialFilter(),...Object.fromEntries(url.searchParams)} as StatsFilter;
  const summary=()=>reviewSummary(filter),usage=()=>reviewUsage(filter);
  const recordPage=<T,>(items:T[])=>({range:summary().range,scope:'synthetic-design-fixture',page:{page:1,limit:50,total:items.length},items,totals:summary().totals,coverage:summary().coverage});
  if(method==='GET'){
   if(scenario==='error'&&path.startsWith('/api/v1/statistics/'))return response(null,503);
   if(path==='/api/v1/session')return response({client_id:'story-browser',name:'设计预览 · 合成数据',purpose:'admin',csrf:'synthetic-story-only',expires_at_ms:null});
   if(path==='/api/v1/version')return response({version:'设计预览',commit:'合成样本',built_at:'',reporting_protocol:1,throughput_capsule:1,schema:3});
   if(path==='/api/v1/catalog')return response(reviewCatalog);
   if(['/api/v1/statistics/summary','/api/v1/statistics/annual','/api/v1/statistics/totals','/api/v1/statistics/activity','/api/v1/statistics/top-sessions','/api/v1/statistics/providers','/api/v1/statistics/models'].includes(path)){const v=summary();if(scenario==='empty'){v.totals={...v.totals,total_tokens:null};v.activity_timeline=v.activity_timeline.map(r=>({...r,tokens:null,sessions:null}));v.top_sessions=[];v.weekday_hours=v.weekday_hours.map(r=>({...r,tokens:null,sessions:0,session_count:null}));}return response(v);}
   if(path==='/api/v1/statistics/source-usage')return response(scenario==='empty'?{...reviewSourceUsage(filter),items:[]}:reviewSourceUsage(filter));
   if(path==='/api/v1/statistics/usage')return response(usage());
   if(path==='/api/v1/devices/status')return response(reviewDevices);
   if(path==='/api/v1/clients')return response({clients});
   if(path==='/api/v1/quotas'||path==='/api/v1/quotas/pace'||path==='/api/v1/quotas/accounts'){
    const data=path.endsWith('/pace')?reviewPace(scenario):reviewQuota(scenario);
    const match=(a:{provider:string;account_key:string|null})=>(!filter.provider||a.provider===filter.provider)&&(!url.searchParams.get('account_key')||a.account_key===url.searchParams.get('account_key'));
    const matchWindow=(w:{provider:string;account_key:string|null;key:string})=>match(w)&&(!url.searchParams.get('window_key')||w.key===url.searchParams.get('window_key'));
    if('credits' in data){data.windows=data.windows.filter(matchWindow);data.credits=data.credits.filter(match);}
    else data.windows=data.windows.filter(matchWindow);
    data.accounts=data.accounts.filter(a=>(!filter.provider||a.provider===filter.provider)&&(!url.searchParams.get('account_key')||a.key===url.searchParams.get('account_key')));
    if('credits' in data){
     if(path.endsWith('/accounts')){data.windows=[];data.credits=[];}
     for(const w of data.windows){w.observation_count=w.observations.length;if(url.searchParams.get('view')==='summary'){delete w.observation_count;w.observations=[];w.cycles=[];}else if(url.searchParams.get('view')==='evidence'){const page=Number(url.searchParams.get('page')??1),limit=Number(url.searchParams.get('limit')??20);w.observations=w.observations.slice((page-1)*limit,page*limit);}}
    }
    return response(data);
   }
   if(path==='/api/v1/sessions')return response(recordPage(reviewSessions));
   if(path==='/api/v1/projects')return response(recordPage(reviewProjects));
   if(path.startsWith('/api/v1/sessions/')){const session=reviewSessions.find(s=>s.id===decodeURIComponent(path.split('/').pop()??''));return session?response({session,range:summary().range,trend:summary().trend,tools:summary().tools,skills:summary().skills,coverage:summary().coverage}):response(null,404);}
   if(path.startsWith('/api/v1/projects/')){const project=reviewProjects.find(p=>p.id===decodeURIComponent(path.split('/').pop()??''));return project?response({project,sessions:recordPage(reviewSessions.filter(s=>s.project_id===project.id)),trend:summary().trend,models:summary().models}):response(null,404);}
  }
  const account=path.match(/^\/api\/v1\/accounts\/([^/]+)\/subscription$/)?.[1];
  if(account){
   const id=decodeURIComponent(account),current=subscriptions.get(id)??reviewSubscription(id);
   if(method==='GET')return response(current);
   if(method==='POST'){
    const update=JSON.parse(String(init?.body??'{}')) as SubscriptionUpdate;
    if(update.expected_revision!==current.revision)return response(null,409);
    const next={...current,...update,revision:String(BigInt(current.revision)+1n),resolved_plan:update.manual_plan??current.automatic_plan,updated_at_ms:reviewNow};
    subscriptions.set(id,next);return response(next);
   }
  }
  if(method==='POST'&&path.endsWith('/rename')){const id=decodeURIComponent(path.split('/').at(-2)??''),client=clients.find(c=>c.id===id);if(!client)return response(null,404);client.name=(JSON.parse(String(init?.body)) as {name:string}).name;return response({applied:true});}
  return response(null,501);
 };
 window.fetch=mock;
 return ()=>{if(window.fetch===mock)window.fetch=previous;};
}
