import { api, ApiError } from './client';
import { statsParams, type Coverage, type Day, type ReportingRange, type Slice, type StatsFilter, type Totals } from './statistics';

export interface Source { id:string;client_id:string;client_name:string;collected_at_ms:number;revision:number;source_kind:string;complete:boolean;deleted:boolean }
export interface ThroughputStats { average_output_milli_tps:string|null;output_tokens:string|null;active_duration_ms:string|null;included_turns:string|null;excluded_turns:string|null;open_turns:string|null;unattributed_events:string|null;status:'complete'|'partial'|'unavailable';reason:string;duration_source:string;basis:string;average_unit:string;duration_unit:string;source_client_id:string|null;conflict:boolean }
export interface ThroughputTurns { items:{key:string;started_at_ms:number|null;ended_at_ms:number|null;throughput:ThroughputStats}[];total:string|null;limit:number;truncated:boolean }
export interface SessionRecord { throughput?:ThroughputStats|null;id:string;provider:string;session_id:string|null;title:string;session_kind:string;project_id:string;project_group_id:string;project_name:string;created_at_ms:number|null;last_active_at_ms:number|null;collected_at_ms:number;complete:boolean;conflict:boolean;sources:Source[];totals:Totals }
export interface ProjectRecord { id:string;name:string;members:string[];totals:Totals;last_active_at_ms:number|null;conflict:boolean }
export interface Page { page:number;limit:number;total:number }
export interface Records<T> { range:ReportingRange;scope:string;page:Page;items:T[];totals:Totals;coverage:Coverage }
export interface SessionDetail { throughput_turns?:ThroughputTurns;session:SessionRecord;range:ReportingRange;trend:Day[];tools:Slice[];skills:Slice[];coverage:Coverage }
export interface ProjectDetail { project:ProjectRecord;sessions:Records<SessionRecord>;trend:Day[];models:Slice[] }
export interface ListFilter extends StatsFilter { search:string;model:string;sort:string;direction:string;page:number;limit:number }
const params=(filter:ListFilter)=>({...statsParams(filter),page:String(filter.page),limit:String(filter.limit)});
function records<T>(value:Records<T>):Records<T> { if(!value?.page||!Array.isArray(value.items)||!value.totals||!value.coverage) throw new ApiError(502);return value; }
export async function getSessions(filter:ListFilter,signal?:AbortSignal){return records(await api.get<Records<SessionRecord>>('/api/v1/sessions',params(filter),signal));}
export async function getProjects(filter:ListFilter,signal?:AbortSignal){return records(await api.get<Records<ProjectRecord>>('/api/v1/projects',params(filter),signal));}
export async function getSession(id:string,filter:StatsFilter,signal?:AbortSignal,turnLimit=20){const v=await api.get<SessionDetail>(`/api/v1/sessions/${encodeURIComponent(id)}`,{...statsParams(filter),throughput_limit:String(turnLimit)},signal);if(!v?.session||!v.coverage||!Array.isArray(v.trend)||!Array.isArray(v.session.sources)) throw new ApiError(502);return v;}
export async function getProject(id:string,filter:ListFilter,signal?:AbortSignal){const v=await api.get<ProjectDetail>(`/api/v1/projects/${encodeURIComponent(id)}`,params(filter),signal);if(!v?.project||!Array.isArray(v.trend)||!Array.isArray(v.models))throw new ApiError(502);records(v.sessions);return v;}
export async function associateProjects(ids:string[],target:string){const v=await api.request<{applied:boolean}>('/api/v1/projects/associate',{body:{project_ids:ids,target_id:target}});if(v?.applied!==true)throw new ApiError(502);return v;}
