import type { Decimal, Totals } from '../api/statistics';
import { dollars, integer, tokens } from '../format';

// 仅用于成功响应的展示：空记录显示零，已有记录缺字段仍保留缺失。
export const emptyUsage = (totals: Totals) => totals.sessions === 0 && totals.invocations === 0;
export function usageDecimal(totals: Totals | undefined, field: 'total_tokens' | 'input_tokens' | 'output_tokens' | 'cost_micro_usd'): Decimal {
  return totals ? totals[field] ?? (emptyUsage(totals) ? '0' : null) : '0';
}
export const usageTokens = (value: Decimal) => value === null ? '—' : tokens(value);
export const usageDollars = (value: Decimal) => value === null ? '—' : dollars(value);
export const usageInteger = (value: Decimal) => value === null ? '—' : integer(value);
