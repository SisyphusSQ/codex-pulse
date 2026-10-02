import { api, ApiError } from './client';

export type Decimal = string | null;
export interface Totals {
  cost_basis: string; pricing_versions: string[]; cost_status: string;
  input_tokens: Decimal; cached_tokens: Decimal; cache_write_tokens: Decimal;
  output_tokens: Decimal; reasoning_tokens: Decimal; total_tokens: Decimal;
  cost_micro_usd: Decimal; reported_charge_micro_usd: Decimal;
  sessions: number; invocations: number;
}
export interface Coverage {
  scope: string; range_covered: boolean; stale: boolean; state: string;
  known_providers: number; unpriced_facts: number; unknown_token_facts: number;
  untimed_facts: number; partial_sessions: number; conflicted_sessions: number;
  collected_at_ms: number | null;
}
export interface ReportingRange { start_at_ms: number; end_at_ms: number; time_zone: string }
export interface Slice { key: string; name: string; totals: Totals }
export interface Day { date: string; start_at_ms: number; totals: Totals }
export interface Hour { weekday: number; hour: number; tokens: Decimal; sessions: number }
export interface AnnualActivity {
  total_tokens: Decimal; peak_daily_tokens: Decimal; active_days: Decimal;
  current_streak_days: Decimal; longest_streak_days: Decimal;
  observed_days: number; unknown_days: number;
}
export interface Summary {
  cost_basis: string; trend_cost_rounding_delta_micro_usd: Decimal;
  range: ReportingRange; scope: string; totals: Totals; coverage: Coverage;
  providers: Slice[]; models: Slice[]; devices: Slice[]; trend: Day[];
  heatmap: Day[]; heatmap_range: ReportingRange; heatmap_coverage: Coverage;
  heatmap_activity: AnnualActivity;
  weekday_hours: Hour[]; tools: Slice[]; skills: Slice[];
}
export interface Device {
  id: string; name: string; revoked_at_ms: number | null; last_received_at_ms: number | null;
  providers: { provider: string; version: string; collected_at_ms: number | null; coverage_start_ms: number | null; coverage_end_ms: number | null; pending_batches: number; status: string; received_at_ms: number; stale: boolean }[];
}
export interface StatsFilter {
  start_date: string; end_date_exclusive: string; time_zone: string;
  provider: string; client_id: string;
  project_id?: string; model?: string; search?: string;
}
export function statsParams(filter: StatsFilter): Record<string, string> { return Object.fromEntries(Object.entries(filter).filter(([,value])=>typeof value==='string')); }

export async function getSummary(filter: StatsFilter, signal?: AbortSignal): Promise<Summary> {
  const value = await api.get<unknown>('/api/v1/statistics/summary', statsParams(filter), signal);
  if (!value || typeof value !== 'object') throw new ApiError(502);
  const s = value as Partial<Summary>;
  if (!s.range || !s.totals || !s.coverage || !s.heatmap_range || !s.heatmap_coverage || !s.heatmap_activity || ![s.providers,s.models,s.devices,s.trend,s.heatmap,s.weekday_hours,s.tools,s.skills].every(Array.isArray)) throw new ApiError(502);
  return s as Summary;
}
export async function getDevices(signal?: AbortSignal): Promise<Device[]> {
  const value = await api.get<unknown>('/api/v1/devices/status', {}, signal);
  if (!Array.isArray(value)) throw new ApiError(502);
  return value as Device[];
}
