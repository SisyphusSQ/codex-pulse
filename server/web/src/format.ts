import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';
import 'dayjs/locale/zh-cn';

dayjs.extend(utc);dayjs.extend(timezone);dayjs.locale('zh-cn');
export { dayjs };

export const providerOrder = ['codex', 'cursor', 'grok', 'dsh', 'agy'] as const;
export type ProviderID = (typeof providerOrder)[number];
export const providerNames: Record<string, string> = { codex: 'Codex', cursor: 'Cursor', grok: 'Grok', dsh: 'DSH', agy: 'Antigravity' };
export const providerColors: Record<string, string> = { codex: '#2678f5', cursor: '#3e9e82', grok: '#d3a34b', dsh: '#299caa', agy: '#6c5ce7' };
export const providerTagColors: Record<string, string> = { codex: 'blue', cursor: 'green', grok: 'gold', dsh: 'cyan', agy: 'purple' };
export function providerLabel(provider: string | null | undefined): string {
  if (!provider) return '未知平台';
  return providerNames[provider] ?? provider;
}
export function providerOptions(): { value: string; label: string }[] {
  return [{ value: '', label: '全部平台' }, ...providerOrder.map(value => ({ value, label: providerNames[value] }))];
}
export function providerTagColor(provider: string | null | undefined): string | undefined {
  return provider ? providerTagColors[provider] : undefined;
}

const sourceKindLabels: Record<string, string> = {
  light_index: '轻量索引',
  strict_index: '完整索引',
  cursor_local: 'Cursor 本地',
  cursor_dashboard: 'Cursor Dashboard',
  grok_local: 'Grok 本地',
  grok_billing: 'Grok 计费接口',
  dsh_local: 'DSH 本地',
  agy_local: 'Antigravity 本地轨迹',
  agy_ls_rpc: 'Antigravity 本机 LS RPC',
  'agy.summaries': 'Antigravity 会话摘要库',
  'agy.conversation_summaries': 'Antigravity 会话摘要库',
  'agy.trajectories': 'Antigravity 本地轨迹库',
  'agy.conversation_steps': 'Antigravity 本地轨迹库',
  'agy.transcripts': 'Antigravity Transcript 回退',
  'agy.transcript_fallback': 'Antigravity Transcript 回退',
  'agy.ls_rpc': 'Antigravity 本机 LS RPC',
  'agy.language_server': 'Antigravity 本机 LS RPC',
  app_server: 'App Server',
  app_server_rate_limits: 'App Server 限额',
  local_jsonl: '本地 JSONL',
  legacy_wham: '历史 WHAM',
};
export function sourceKindLabel(kind: string | null | undefined): string {
  if (!kind) return '来源未知';
  if (sourceKindLabels[kind]) return sourceKindLabels[kind];
  if (kind.startsWith('agy.')) return `Antigravity · ${formatSlugWords(kind.slice(4)) || '本地来源'}`;
  return kind;
}

export const quotaPoolOrder = ['agy.gemini', 'agy.argon', 'agy.carbon', 'agy.claude_gpt'] as const;
const quotaLimitLabels: Record<string, string> = {
  'agy.gemini': 'Gemini',
  'agy.argon': 'Gemini 4 Argon',
  'agy.carbon': 'Gemini 4 Carbon',
  'agy.claude_gpt': 'Claude & GPT',
};
const slugAcronyms: Record<string, string> = { gpt: 'GPT', ai: 'AI', agy: 'AGY', rpc: 'RPC', ls: 'LS', llm: 'LLM', api: 'API' };
function formatSlugWords(slug: string): string {
  return slug
    .split(/[-_.\s]+/)
    .filter(Boolean)
    .map(part => slugAcronyms[part.toLowerCase()] ?? `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(' ');
}
export function quotaLimitLabel(limitId: string): string {
  if (quotaLimitLabels[limitId]) return quotaLimitLabels[limitId];
  if (limitId.startsWith('agy.pool.')) {
    const formatted = formatSlugWords(limitId.slice('agy.pool.'.length));
    return formatted || '自定义配额池';
  }
  if (limitId.startsWith('agy.')) {
    const formatted = formatSlugWords(limitId.slice('agy.'.length));
    return formatted || 'Antigravity 配额池';
  }
  return limitId;
}
export function quotaPoolRank(limitId: string): number {
  const index = (quotaPoolOrder as readonly string[]).indexOf(limitId);
  return index === -1 ? quotaPoolOrder.length : index;
}
export function quotaWindowShortLabel(windowKind: string, windowMinutes: number | null): string {
  if (windowKind === 'primary' || windowMinutes === 300) return '5h';
  if (windowKind === 'secondary' || windowMinutes === 10080) return '周';
  if (windowMinutes == null) return '窗口未知';
  if (windowMinutes % 1440 === 0) return `${windowMinutes / 1440}天`;
  if (windowMinutes % 60 === 0) return `${windowMinutes / 60}h`;
  return `${windowMinutes}分钟`;
}

const sessionRoleMeta: Record<string, { label: string; color: string }> = {
  subagent: { label: '子代理', color: 'geekblue' },
  swarm_worker: { label: 'Swarm 成员', color: 'purple' },
  side_question: { label: '侧边提问', color: 'cyan' },
};
export function sessionRoleTag(role: string | null | undefined): { label: string; color: string } | null {
  if (!role) return null;
  return sessionRoleMeta[role] ?? null;
}
export function integer(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return '未知';
  return new Intl.NumberFormat('zh-CN').format(typeof value === 'number' ? value : BigInt(value));
}
// 与中文 Mac 客户端一致：万/亿，最多一位小数；进位可跨单位，计算保留 BigInt 精度。
export function tokens(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return '未知';
  const n = BigInt(value);
  if (n < 0n) return '未知';
  if (n < 10_000n) return integer(value);
  let divisor = n < 100_000_000n ? 10_000n : 100_000_000n;
  let tenths = (n * 10n + divisor / 2n) / divisor;
  if (divisor === 10_000n && tenths >= 100_000n) {
    divisor = 100_000_000n;
  }
  // Mac 的单位晋升用 rounded()，最终 NumberFormatter 使用 half-even。
  const scaled = n * 10n, whole = scaled / divisor, remainder = scaled % divisor;
  tenths = whole + (remainder * 2n > divisor || (remainder * 2n === divisor && whole % 2n !== 0n) ? 1n : 0n);
  return `${tenths / 10n}${tenths % 10n ? `.${tenths % 10n}` : ''}${divisor === 10_000n ? '万' : '亿'}`;
}
// 轴坐标本来就是浮点近似；真实标签和 tooltip 读取十进制字符串。
export const tokenAxis = (value: number) => tokens(Math.round(value));
export const dollarAxis = (value: number) => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value);
export function dollars(value: string | null | undefined): string {
  if (value === null || value === undefined) return '未知';
  const n=BigInt(value), magnitude=n<0n?-n:n;
  const cents=(magnitude+5_000n)/10_000n;
  return `${n<0n&&cents>0n?'-':''}$${integer((cents/100n).toString())}.${(cents%100n).toString().padStart(2,'0')}`;
}
export function dateTime(value: number | null | undefined, zone='Asia/Shanghai'): string {
  if (value === null || value === undefined) return '尚无观测';
  if (Number.isNaN(new Date(value).getTime())) return '时间超出显示范围';
  return new Intl.DateTimeFormat('zh-CN',{ dateStyle:'medium',timeStyle:'short',timeZone:zone }).format(value);
}
// 浮点数只用于坐标，标签与 tooltip 仍读原始十进制字符串。
export function coordinate(value: string | null): number | null { return value===null?null:Number(value); }
