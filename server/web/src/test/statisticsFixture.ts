import type { Coverage, Summary, Totals } from '../api/statistics';

export const unknownTotals:Totals={cost_basis:'historical',pricing_versions:[],cost_status:'unknown',input_tokens:null,cached_tokens:null,cache_write_tokens:null,output_tokens:null,reasoning_tokens:null,total_tokens:null,cost_micro_usd:null,reported_charge_micro_usd:null,sessions:0,invocations:0};
export const partialCoverage:Coverage={scope:'received_facts',range_covered:false,stale:true,state:'partial',known_providers:1,unpriced_facts:1,unknown_token_facts:1,untimed_facts:1,partial_sessions:1,conflicted_sessions:0,collected_at_ms:1790812800000};
export function summaryFixture():Summary {
  const range={start_at_ms:1790812800000,end_at_ms:1790899200000,time_zone:'Asia/Shanghai'};
  const totals={...unknownTotals,total_tokens:'9007199254740993',cost_micro_usd:'123456789',cost_status:'partial',sessions:2,invocations:3};
  return {cost_basis:'historical',trend_cost_rounding_delta_micro_usd:'0',range,scope:'global_deduplicated',totals,coverage:partialCoverage,providers:[{key:'codex',name:'codex',totals}],models:[],devices:[],trend:[{date:'2026-10-01',start_at_ms:range.start_at_ms,totals}],heatmap:[{date:'2026-10-01',start_at_ms:range.start_at_ms,totals}],heatmap_range:range,heatmap_coverage:{...partialCoverage,stale:false},weekday_hours:[],tools:[],skills:[]};
}
