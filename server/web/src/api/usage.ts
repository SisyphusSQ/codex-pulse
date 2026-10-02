import {api,ApiError} from './client';
import {statsParams} from './statistics';
import type {StatsFilter,Totals,Coverage,ReportingRange,Slice,Day,Decimal} from './statistics';
export interface UsageModel {provider:string;model:string;totals:Totals}
export interface UsageModelDay extends UsageModel {date:string}
export interface Usage {range:ReportingRange;scope:string;totals:Totals;coverage:Coverage;models:UsageModel[];model_days:UsageModelDay[];trend:Day[];providers:Slice[];cursor_pools:Slice[];model_cost_rounding_delta_micro_usd:Decimal;trend_cost_rounding_delta_micro_usd:Decimal}
export async function getUsage(filter:StatsFilter,signal?:AbortSignal){const v=await api.get<Usage>('/api/v1/statistics/usage',statsParams(filter),signal);if(!v||!v.totals||!v.coverage||![v.models,v.model_days,v.trend,v.providers,v.cursor_pools].every(Array.isArray))throw new ApiError(502);return v;}
