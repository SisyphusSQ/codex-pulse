import { api, ApiError } from './client';

export interface QuotaFilter { provider:string; client_id:string; account_key:string }
export interface QuotaAccount { key:string; provider:string; raw_id:string; email:string|null; plan:string|null; collected_at_ms:number }
export interface QuotaCurrent {
 used_percent:number|null; remaining_percent:number|null; window_start_at_ms:number|null; resets_at_ms:number|null;
 reset_remaining_ms:number|null; observed_at_ms:number|null; freshness:string; conflict:boolean; reason:string;
 snapshot_reset_remaining_ms?:number|null;
 selected_observation_id:string|null; selected_client_id:string|null; source:string|null;
}
export interface QuotaObservation {
 id:string; client_id:string; client_name:string; observed_at_ms:number; received_at_ms:number;
 used_percent:number|null; resets_at_ms:number|null; window_start_at_ms:number|null; source:string; validity:string;
 history_origin:string; disposition:string; reason:string|null; cycle_id:string|null; canonical_reset_at_ms:number|null;
}
export interface QuotaCycle { id:string; start_at_ms:number; resets_at_ms:number; observation_ids:string[]; linked_history:boolean }
export interface QuotaWindow {
 observation_count?:number; observation_page?:number; observation_limit?:number; key:string; provider:string; account_key:string|null; identity_state:string; limit_id:string; window_kind:string;
 window_minutes:number|null; current:QuotaCurrent; cycles:QuotaCycle[]; observations:QuotaObservation[]; coverage:string;
}
export interface QuotaCredits {
 key:string; provider:string; account_key:string|null; client_id:string; observed_at_ms:number;
 observed_inventory:string|null; available_inventory:string|null; details_status:string; freshness:string; conflict:boolean;
 snapshot_available_inventory?:string|null; snapshot_next_expires_at_ms?:number|null;
 next_reset_at_ms:number|null; next_expires_at_ms:number|null; expiry_schedule:{expires_at_ms:number|null; count:string}[];
}
export interface QuotaResponse { evaluated_at_ms:number; rule_version:string; accounts:QuotaAccount[]; windows:QuotaWindow[]; credits:QuotaCredits[]; coverage:string }
export interface PacePoint { observed_at_ms:number; elapsed_percent:number; used_percent:number; remaining_percent:number; linked_history:boolean }
export interface PaceCycle { id:string; window_start_at_ms:number; resets_at_ms:number; complete:boolean; points:PacePoint[] }
export interface HistoryBandPoint { elapsed_percent:number; median_remaining:number; minimum_remaining:number; maximum_remaining:number; cycle_count:number }
export interface PaceForecast { state:string; method:string; exhaust_at_ms:number|null; lead_before_reset_ms:number|null; evidence_count:number; evidence_span_ms:number; unknown_reason:string|null }
export interface PaceWindow {
 snapshot_at_ms?:number|null;
 observation_count?:number; observation_page?:number; observation_limit?:number; key:string; provider:string; account_key:string|null; identity_state:string; limit_id:string; window_kind:string; window_minutes:number|null;
 current:QuotaCurrent; elapsed_percent:number|null; pace_delta_pp:number|null; forecast:PaceForecast; current_points:PacePoint[];
 previous_cycle:PaceCycle|null; historical_cycles:PaceCycle[]; history_band:HistoryBandPoint[]; history_cycle_count:number;
 previous_remaining_at_elapsed:number|null; history_median_remaining_at_elapsed:number|null; unknown_reason:string|null; coverage:string;
}
export interface PaceResponse { evaluated_at_ms:number; rule_version:string; accounts:QuotaAccount[]; windows:PaceWindow[]; coverage:string }
const params=(filter:QuotaFilter)=>({...filter});
export async function getQuotas(filter:QuotaFilter,signal?:AbortSignal,extra:Record<string,string|number|undefined>={}):Promise<QuotaResponse>{
 const value=await api.get<QuotaResponse>('/api/v1/quotas',{...params(filter),...extra},signal);
 if(!value||![value.accounts,value.windows,value.credits].every(Array.isArray))throw new ApiError(502);
 return value;
}
export async function getPace(filter:QuotaFilter,signal?:AbortSignal,windowKey?:string):Promise<PaceResponse>{
 const value=await api.get<PaceResponse>('/api/v1/quotas/pace',{...params(filter),window_key:windowKey},signal);
 if(!value||![value.accounts,value.windows].every(Array.isArray))throw new ApiError(502);
 return value;
}

export async function getQuotaAccounts(filter:QuotaFilter,signal?:AbortSignal):Promise<QuotaResponse>{
 const value=await api.get<QuotaResponse>('/api/v1/quotas/accounts',params(filter),signal);
 if(!value||!Array.isArray(value.accounts))throw new ApiError(502);return value;
}
