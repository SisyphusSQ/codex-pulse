import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {render,screen} from '@testing-library/react';
import {QueryClientProvider} from '@tanstack/react-query';
import {MemoryRouter} from 'react-router-dom';
import {createQueryClient} from '../App';
import {api} from '../api/client';
import {summaryFixture} from '../test/statisticsFixture';
import type {Usage as UsageValue} from '../api/usage';
import Usage,{usageChartOption} from './Usage';
vi.mock('../components/Chart',()=>({default:()=> <div aria-label="合成趋势" />}));
const fetcher=vi.fn<typeof fetch>();
beforeEach(()=>{api.setSession({client_id:'test',name:'测试',purpose:'admin',csrf:'synthetic',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',fetcher);});afterEach(()=>vi.unstubAllGlobals());
function data():UsageValue{const s=summaryFixture();return {range:s.range,scope:s.scope,totals:{...s.totals,reported_charge_micro_usd:'5000000',reported_charge_status:'partial'},coverage:s.coverage,providers:s.providers,cache_hit_rate:{basis_points:'5000',input_tokens:'1000',cached_input_tokens:'500',unit:'basis_points',basis:'range_cached_input',status:'complete',reason:'',source_client_id:null,conflict:false},models:[{provider:'codex',model:'gpt-6.1-sol',totals:s.totals}],model_days:[{provider:'codex',model:'gpt-6.1-sol',date:'2026-10-01',totals:s.totals}],trend:s.trend,cursor_pools:[],model_cost_rounding_delta_micro_usd:'0',trend_cost_rounding_delta_micro_usd:'0'};}
it('shows estimated and reported separately, with full raw model filter',async()=>{
 fetcher.mockImplementation(async url=>new Response(JSON.stringify({code:200,data:String(url).includes('/devices/status')?[]:data()})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={['/quota?view=usage&provider=codex&model=gpt-6.1-sol']}><Usage /></MemoryRouter></QueryClientProvider>);
 await screen.findByText('API 折算成本');expect(screen.getByText('50.0%')).toBeInTheDocument();expect(screen.getByText('已知上报金额小计 · 与估算分开展示')).toBeInTheDocument();expect(screen.getAllByText('$123.46').length).toBeGreaterThan(0);expect(screen.getAllByText('$5.00').length).toBeGreaterThan(0);expect(fetcher.mock.calls.some(([u])=>String(u).includes('model=gpt-6.1-sol'))).toBe(true);expect(screen.getByText('缓存写入 Token')).toBeInTheDocument();
});
it('leaves unknown model day buckets empty',()=>{const d=data();d.trend.push({...d.trend[0],date:'2026-10-02'});const o=usageChartOption(d,'cost',['codex:gpt-6.1-sol']);const series=o.series as {data:(number|null)[];type:string;stack?:string}[];expect(series[0].data).toEqual([123.456789,null]);expect(series[0].type).toBe('bar');expect(series[0].stack).toBeUndefined();});

it('stacks model bars while retaining zero, missing and partial costs',()=>{const d=data();d.models.push({...d.models[0],model:'other'});d.model_days.push({provider:'codex',model:'other',date:'2026-10-01',totals:{...d.totals,cost_micro_usd:'0'}});d.trend.push({...d.trend[0],date:'2026-10-02'});const o=usageChartOption(d,'cost',['codex:gpt-6.1-sol','codex:other']);const series=o.series as {type:string;stack:string;data:(number|null)[]}[];expect(series.every(s=>s.type==='bar'&&s.stack==='models')).toBe(true);expect(series[1].data).toEqual([0,null]);const tooltip=o.tooltip as {formatter(p:unknown):string};expect(tooltip.formatter([{seriesName:'Codex · gpt-6.1-sol',dataIndex:0,value:123.456789}])).toContain('已知小计');});

it('uses zeros only for absent overview model buckets while retaining unpriced records',()=>{
 const d=data();d.trend.push({...d.trend[0],date:'2026-10-02'},{...d.trend[0],date:'2026-10-03'});
 d.model_days.push({...d.model_days[0],date:'2026-10-03',totals:{...d.totals,cost_micro_usd:null}});
 const o=usageChartOption(d,'cost',['codex:gpt-6.1-sol'],true);
 expect((o.series as {data:(number|null)[]}[])[0].data).toEqual([123.456789,0,null]);
 const tooltip=o.tooltip as {formatter(p:unknown):string};
 expect(tooltip.formatter([{seriesName:'Codex · gpt-6.1-sol',dataIndex:1,value:0}])).toContain('$0.00');
 expect(tooltip.formatter([{seriesName:'Codex · gpt-6.1-sol',dataIndex:2,value:null}])).toContain('—');
 expect(d.model_days).toHaveLength(2);expect(d.model_days[1].totals.cost_micro_usd).toBeNull();
});

it('labels DSH mixed provider pricing as API reference estimates',async()=>{
 const value=data();value.models=[{provider:'dsh',model:'gpt-6.1-sol',totals:value.totals}];
 fetcher.mockImplementation(async url=>new Response(JSON.stringify({code:200,data:String(url).includes('/devices/status')?[]:value})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={['/usage/models?provider=dsh']}><Usage /></MemoryRouter></QueryClientProvider>);
 expect(await screen.findByText('模型 API 公价估算')).toBeInTheDocument();expect(screen.queryByText('DeepSeek 峰谷价估算')).not.toBeInTheDocument();
});
