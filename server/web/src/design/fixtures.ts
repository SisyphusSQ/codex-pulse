import type { Day, Totals } from '../api/statistics';
import { dayjs } from '../format';

// 所有数值、账号、项目和会话均为设计合成样本；不读取真实 Home 或中心 API。
export const sampleTotals: Totals = {
  input_tokens: '40880000', cached_tokens: '32817000', cache_write_tokens: '0', output_tokens: '7060000', reasoning_tokens: '640000',
  total_tokens: '48580000', cost_micro_usd: '38240000', cost_status: 'complete', reported_charge_micro_usd: null,
  reported_charge_status: 'unknown', cost_basis: 'synthetic-design-fixture', sessions: 128, invocations: 460, pricing_versions: ['reference-2026-10'],
};
export const modelRows = [
  { key: 'codex:gpt-5.4', provider: 'Codex', model: 'gpt-5.4', tokens: '18460000', cost: '18240000', cache: '82.4%', input: '15600000', output: '2860000', sessions: 52, inputPrice: '2.50', cachedPrice: '0.25', outputPrice: '15.00', color: '#2678f5' },
  { key: 'codex:gpt-5.4-mini', provider: 'Codex', model: 'gpt-5.4-mini', tokens: '12840000', cost: '7800000', cache: '76.2%', input: '10500000', output: '2340000', sessions: 38, inputPrice: '0.75', cachedPrice: '0.075', outputPrice: '4.50', color: '#32a89a' },
  { key: 'cursor:claude-sonnet-4.6', provider: 'Cursor', model: 'claude-sonnet-4.6', tokens: '10320000', cost: '8600000', cache: '91.2%', input: '9200000', output: '1120000', sessions: 23, inputPrice: '3.00', cachedPrice: '0.30', outputPrice: '15.00', color: '#8a70cf' },
  { key: 'grok:grok-4.1-fast', provider: 'Grok', model: 'grok-4.1-fast', tokens: '6320000', cost: '2600000', cache: '64.0%', input: '5580000', output: '740000', sessions: 11, inputPrice: '0.20', cachedPrice: '0.05', outputPrice: '0.50', color: '#d89a38' },
  { key: 'unknown', provider: 'Codex', model: '未归因模型', tokens: '640000', cost: '1000000', cache: '未知', input: 'null', output: 'null', sessions: 4, inputPrice: '未知', cachedPrice: '未知', outputPrice: '未知', color: '#8593a9' },
];
export const trendDates = Array.from({ length: 90 }, (_, i) => dayjs.utc('2026-07-06').add(i, 'day').format('YYYY-MM-DD'));
const shapes = [0.9, 1.4, 1.1, 0.3, 0.2, 0.8, 1.2, 1.6, 1.5, 1.1, 0.5, 0.3, 1.1, 1.8, 1.6, 1.3, 0.7, 0.4, 0.3, 1.2, 1.9, 1.6, 1.4, 1.0, 0.4, 0.3, 1.5, 1.7, 1.3, 1.0];
export const modelSeries = modelRows.slice(0, 4).map((r, i) => ({
  key: r.key,
  points: trendDates.map((_, d) => d === 70 + i ? null : Math.round(shapes[d % shapes.length] * (560000 - i * 110000) * (1 + Math.sin((d + i) * 1.4) * 0.22))),
  // 独立的合成金额日桶，不在页面按 Token 比例推算实际费用。
  costs: trendDates.map((_, d) => d === 70 + i ? null : Math.round(shapes[d % shapes.length] * [860000, 420000, 560000, 180000][i])),
}));
export const heatmapDays: Day[] = Array.from({ length: 365 }, (_, i) => {
  const date = dayjs.utc('2025-10-04').add(i, 'day').format('YYYY-MM-DD');
  const weekday = dayjs.utc(date).day();
  const total = i < 35 ? null : weekday === 0 || weekday === 6 ? '0' : String(Math.round((120000 + ((i * 11939) % 1500000)) * (i > 290 ? 1.5 : 1)));
  return { date, start_at_ms: dayjs.utc(date).valueOf(), totals: { ...sampleTotals, total_tokens: total } };
});
export const projects = [
  { key: 'pulse', name: 'Codex Pulse', desc: '多机用量中心与原生客户端', tokens: '18620000', cost: '16280000', sessions: 48, time: '今天 09:42', models: 'gpt-5.4、claude-sonnet-4.6' },
  { key: 'family', name: '家庭服务', desc: '账号订阅与预算工作台', tokens: '14260000', cost: '11860000', sessions: 36, time: '今天 08:15', models: 'gpt-5.4-mini、gpt-5.4' },
  { key: 'starter', name: 'Go Web Starter', desc: '服务模板与数据库集成', tokens: '9860000', cost: '6420000', sessions: 27, time: '昨天 22:31', models: 'gpt-5.4' },
  { key: 'unknown', name: '待关联会话', desc: '保留已收到事实，等待项目关联', tokens: '5840000', cost: '3680000', sessions: 17, time: '昨天 19:06', models: 'grok-4.1-fast' },
];
export const sessions = [
  { key: 'demo-session-001', title: '完善模型趋势与缓存指标', project: 'Codex Pulse', model: 'gpt-5.4', provider: 'Codex', tokens: '1420000', cost: '1820000', cache: '88.2%', tps: '42.6', turns: 26, time: '今天 09:42' },
  { key: 'demo-session-002', title: '整理账号额度工作台', project: 'Codex Pulse', model: 'claude-sonnet-4.6', provider: 'Cursor', tokens: '980000', cost: '1460000', cache: '91.8%', tps: '未知', turns: 18, time: '今天 08:50' },
  { key: 'demo-session-003', title: '订阅日期与短月处理', project: '家庭服务', model: 'gpt-5.4-mini', provider: 'Codex', tokens: '860000', cost: '480000', cache: '73.1%', tps: '64.2', turns: 12, time: '今天 08:15' },
  { key: 'demo-session-004', title: '查询层与分页结构', project: 'Go Web Starter', model: 'gpt-5.4', provider: 'Codex', tokens: '2140000', cost: '2630000', cache: '84.6%', tps: '39.1', turns: 34, time: '昨天 22:31' },
  { key: 'demo-session-005', title: '模型价格资料整理', project: '待关联会话', model: 'grok-4.1-fast', provider: 'Grok', tokens: '420000', cost: '110000', cache: '未知', tps: '未知', turns: 9, time: '昨天 19:06' },
];
export const devices = [
  { key: 'demo-device-01', name: '工作 Mac', host: 'Apple Silicon · macOS', providers: ['Codex', 'Cursor'], state: '已同步', last: '今天 09:42', queue: 0, version: 'v0.14.5', coverage: '2025-11-08 — 今天' },
  { key: 'demo-device-02', name: '家用 Mac', host: 'Apple Silicon · macOS', providers: ['Codex', 'Grok'], state: '有待上传', last: '今天 09:35', queue: 12, version: 'v0.14.5', coverage: '2025-12-21 — 今天' },
  { key: 'demo-device-03', name: '随身 Mac', host: 'Intel · macOS', providers: ['Codex'], state: '观测陈旧', last: '昨天 21:18', queue: null, version: 'v0.14.4', coverage: '2026-02-04 — 昨天' },
];
export const providerRows = [
  { key: 'codex', provider: 'Codex', tokens: '31940000', cost: '27040000', input: '26100000', output: '5200000', sessions: 94, color: '#2678f5' },
  { key: 'cursor', provider: 'Cursor', tokens: '10320000', cost: '8600000', input: '9200000', output: '1120000', sessions: 23, color: '#8a70cf' },
  { key: 'grok', provider: 'Grok', tokens: '6320000', cost: '2600000', input: '5580000', output: '740000', sessions: 11, color: '#d89a38' },
];
export const toolRows = [
  { key: 'exec', name: 'exec_command', calls: 218 }, { key: 'patch', name: 'apply_patch', calls: 97 },
  { key: 'browser', name: 'browser_use', calls: 72 }, { key: 'web', name: 'web.run', calls: 38 },
  { key: 'read', name: 'read_file', calls: 24 }, { key: 'other', name: '其他工具', calls: 11 },
];
export const skillRows = [{ key: 'antd', name: 'antd', calls: 28 }, { key: 'design', name: 'product-design', calls: 19 }, { key: 'go', name: 'go-workflow', calls: 14 }];
// 高密度合成观测；中间刻意保留缺口，用于检查跨缺口的视觉连线而非圆点堆叠。
export const paceSamples = Array.from({ length: 240 }, (_, i) => [65 * i / 239, i >= 85 && i <= 105 ? null : 100 - Math.floor(i * 88 / 239) / 2]);
export type Scenario = 'normal' | 'stale' | 'empty' | 'error' | 'conflict' | 'loading';
export type Page = 'overview' | 'models' | 'sources' | 'accounts' | 'projects' | 'sessions' | 'pricing' | 'devices' | 'signin';
export const pageNames: Record<Page, string> = { overview: '用量概览', models: '模型分析', sources: '采集来源', accounts: '账号与额度', projects: '项目', sessions: '会话', pricing: '价目表', devices: '设备与授权', signin: '浏览器授权' };
