import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { createQueryClient } from '../App';
import { api } from '../api/client';
import { summaryFixture, unknownTotals } from '../test/statisticsFixture';
import { dayjs, dollars, integer } from '../format';
import type {Summary} from '../api/statistics';
import Overview from './Overview';

vi.mock('../components/Chart',()=>({default:({label}:{label:string})=><div role="img" aria-label={label} />}));
const fetcher=vi.fn<typeof fetch>();
const usageFrom=(s:Summary)=>({...s,models:[{provider:'codex',model:'gpt-6.1-sol',totals:s.totals}],model_days:[{provider:'codex',model:'gpt-6.1-sol',date:s.trend[0].date,totals:s.totals}],cursor_pools:[],model_cost_rounding_delta_micro_usd:'0'});
const success=(data:unknown)=>new Response(JSON.stringify({code:200,data}));
beforeEach(()=>{api.setSession({client_id:'synthetic-browser',name:'测试浏览器',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',fetcher);});
afterEach(()=>vi.unstubAllGlobals());
describe('overview facts',()=>{
  it.each(['0','92'])('displays the Server current streak %s without deriving it from heatmap rows',async current=>{
    const fixture=summaryFixture();fixture.heatmap_activity.current_streak_days=current;
    fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/source-usage')?success({range:fixture.range,items:[],scope:'collector_copies_may_overlap'}):success(fixture));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await screen.findByText('近 365 天 API 等价成本');
    const annual=within(document.querySelector('.annual-totals') as HTMLElement);
    expect(annual.getByText('当前连续天数').closest('.ant-statistic')).toHaveTextContent(`${current}天`);
  });
  it('requests the seven-day range and opens a real date picker for custom dates',async()=>{
    fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/source-usage')?success({range:summaryFixture().range,items:[],scope:'collector_copies_may_overlap'}):success(String(path).includes('/usage')?usageFrom(summaryFixture()):summaryFixture()));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await screen.findByText('当前范围 Token 总量');const user=userEvent.setup();
    await user.click(screen.getByText('7天',{selector:'.ant-segmented-item-label'}));
    await waitFor(()=>expect(fetcher.mock.calls.some(([path])=>{const u=new URL(String(path),'http://localhost');return u.pathname.endsWith('/totals')&&dayjs(u.searchParams.get('end_date_exclusive')).diff(dayjs(u.searchParams.get('start_date')),'day')===7;})).toBe(true));
    await user.click(screen.getByRole('button',{name:'自定义'}));
    await user.click(await screen.findByPlaceholderText(/Start date|开始日期/));
    // JSDOM has no layout for popup placement; Chrome verifies the visible calendar.
    expect(await screen.findByText('最近 90 天')).toBeInTheDocument();
    expect(document.querySelectorAll('.ant-picker-panel')).toHaveLength(2);
  });
  it('keeps exact generic counts and displays USD with two decimal places',()=>{
    expect(integer('9007199254740993')).toBe('9,007,199,254,740,993');
    expect(integer(null)).toBe('未知');expect(integer('0')).toBe('0');
    expect(dollars('1')).toBe('$0.00');expect(dollars('-1')).toBe('$0.00');
    expect(dollars('123456789')).toBe('$123.46');expect(dollars(null)).toBe('未知');
  });
  it('uses Server totals and separate annual coverage, sends date/provider filters, and retains cached data on refresh failure',async()=>{
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/source-usage')?success({range:summaryFixture().range,items:[],scope:'collector_copies_may_overlap'}):success(String(path).includes('/usage')?usageFrom(summaryFixture()):summaryFixture()));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    expect((await screen.findAllByText('90071992.5亿')).length).toBeGreaterThan(0);
    expect(screen.queryByText(/不是实际账单/)).not.toBeInTheDocument();
    expect(screen.getByText(/采集陈旧 · 更新/)).toBeInTheDocument();expect(screen.queryByText(/覆盖未确认/)).not.toBeInTheDocument();
    const user=userEvent.setup();await user.click(screen.getByRole('combobox',{name:'Provider'}));
    await user.click(await screen.findByText('Cursor',{selector:'.ant-select-item-option-content'}));
    await waitFor(()=>expect(fetcher.mock.calls.some(([path])=>new URL(String(path),'http://localhost').searchParams.get('provider')==='cursor')).toBe(true));
    const url=new URL(String(fetcher.mock.calls.find(([path])=>String(path).includes('/annual'))![0]),'http://localhost');
    expect(url.searchParams.get('start_date')).toMatch(/^\d{4}-\d{2}-\d{2}$/);expect(url.searchParams.get('time_zone')).toBe('Asia/Shanghai');expect(url.searchParams.has('start_at_ms')).toBe(false);
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/source-usage')?success({range:summaryFixture().range,items:[],scope:'collector_copies_may_overlap'}):new Response('',{status:503}));
    await user.click(screen.getByRole('button',{name:'刷新'}));
    await screen.findByText('刷新失败，以下保留上次读取的数据');expect(screen.getAllByText('$123.46').length).toBeGreaterThan(0);
  });
  it('shows zero for an empty range and annual metrics without rewriting API values',async()=>{
    const fixture=summaryFixture();fixture.totals={...unknownTotals};fixture.heatmap_totals={...unknownTotals};
    fixture.heatmap=[{...fixture.heatmap[0],totals:{...unknownTotals}}];
    fixture.heatmap_activity={total_tokens:null,peak_daily_tokens:null,active_days:null,current_streak_days:null,longest_streak_days:null,observed_days:0,unknown_days:365};
    fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/source-usage')?success({range:fixture.range,items:[],scope:'collector_copies_may_overlap'}):success(fixture));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await screen.findByText('近 365 天 Token 总量');
    const annual=within(document.querySelector('.annual-totals') as HTMLElement);
    expect(annual.getByText('近 365 天 Token 总量').closest('.ant-statistic')).toHaveTextContent('0');
    expect(annual.getByText('近 365 天 API 等价成本').closest('.ant-statistic')).toHaveTextContent('$0.00');
    expect(annual.getByText('当前连续天数').closest('.ant-statistic')).toHaveTextContent('0天');
    const range=within(document.querySelector('.summary-band') as HTMLElement);
    expect(range.getByText('当前范围 API 等价成本').closest('.ant-statistic')).toHaveTextContent('$0.00');
    expect(screen.queryByText('未知')).not.toBeInTheDocument();expect(fixture.totals.total_tokens).toBeNull();
  });
  it('keeps unpriced usage as a dash with a short tooltip',async()=>{
    const fixture=summaryFixture();fixture.totals.cost_micro_usd=null;
    fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):success(fixture));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await screen.findByText('当前范围 API 等价成本');
    const cost=screen.getByText('当前范围 API 等价成本').closest('.ant-statistic')!;
    expect(cost).toHaveTextContent('—');expect(cost).not.toHaveTextContent('$0.00');
    await userEvent.setup().hover(within(cost as HTMLElement).getByText('—'));
    expect(await screen.findByText('缺少价格或计费数据')).toBeInTheDocument();
  });
  it('does not turn a failed initial query into an empty range',async()=>{
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/source-usage')?success({range:summaryFixture().range,items:[],scope:'collector_copies_may_overlap'}):new Response('',{status:503}));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await screen.findAllByText('数据未能读取');expect(screen.queryByText('当前范围 Token 总量')).not.toBeInTheDocument();
  });
  it('keeps annual activity first before summary and new activity while preserving independent Server annual metrics',async()=>{
    const fixture=summaryFixture();
    fixture.heatmap_totals={...fixture.totals,total_tokens:'999999999999999999',cost_micro_usd:'9876543210',cost_status:'partial'};
    fixture.heatmap_activity={total_tokens:'999999999999999999',peak_daily_tokens:'100000',active_days:'15',current_streak_days:null,longest_streak_days:'4',observed_days:22,unknown_days:343};
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/source-usage')?success({range:summaryFixture().range,items:[],scope:'collector_copies_may_overlap'}):success(String(path).includes('/usage')?usageFrom(fixture):fixture));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await screen.findByText('近 365 天 API 等价成本');expect(screen.getByText('$9,876.54')).toBeInTheDocument();expect(document.querySelector('.annual-totals')).toHaveTextContent('10000000000亿');
    await userEvent.setup().click(await screen.findByRole('button',{name:/年度活动统计/}));
    expect(await screen.findByText('10000000000亿',{selector:'strong'})).toBeInTheDocument();
    const annual=within(document.querySelector('.annual-totals') as HTMLElement);
    expect(annual.getByText('单日 Token 使用峰值').closest('.ant-statistic')).toHaveTextContent('10万');
    expect(annual.getByText('最长连续天数').closest('.ant-statistic')).toHaveTextContent('4天');
    expect(annual.getByText('当前连续天数').closest('.ant-statistic')).toHaveTextContent('—');
    expect(screen.getByRole('link',{name:'查看账号额度与节奏'})).toHaveAttribute('href','/quota');
    await screen.findByRole('img',{name:'按自然日的用量趋势'});
    const activity=screen.getByText('全年活动').closest('.ant-card')!;
    const trend=screen.getByText('模型用量趋势').closest('.ant-card')!;
    const summary=screen.getByText('当前范围 Token 总量').closest('.summary-band')!;
    expect(activity.compareDocumentPosition(summary)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(activity.compareDocumentPosition(trend)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const added=screen.getByText('活动分布').closest('.ant-card')!;
    expect(activity.compareDocumentPosition(added)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    for(const title of ['平台 / 模型明细','采集来源','平台分布','模型分布'])expect(screen.getByText(title)).toBeInTheDocument();
    expect(screen.queryByText('工具与技能')).not.toBeInTheDocument();
  });
});

describe('overview independent and deferred loading', () => {
  let observers: { callback: IntersectionObserverCallback; target?: Element; disconnect: () => void }[];
  beforeEach(() => {
    observers = [];
    vi.stubGlobal('IntersectionObserver', class {
      record: (typeof observers)[number];
      constructor(callback: IntersectionObserverCallback) {
        this.record = { callback, disconnect: vi.fn() };
        observers.push(this.record);
      }
      observe(target: Element) { this.record.target = target; }
      disconnect() { this.record.disconnect(); }
    });
  });
  const enter = (title: string) => {
    const observer = observers.find(o => o.target?.getAttribute('aria-label') === title)!;
    act(() => observer.callback([{ isIntersecting: true, target: observer.target! } as IntersectionObserverEntry], {} as IntersectionObserver));
    return observer;
  };
  const mount = () => render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
  const response = (path: RequestInfo | URL) => String(path).includes('/devices/status') ? success([])
    : String(path).includes('/source-usage') ? success({ range: summaryFixture().range, items: [], scope: 'collector_copies_may_overlap' })
    : success(String(path).includes('/usage') ? usageFrom(summaryFixture()) : summaryFixture());
  it('hides revoked machines from overview rows and source choices without changing totals', async () => {
    const fixture=summaryFixture();
    const devices=[{id:'revoked-machine',name:'已撤销采集机样本',revoked_at_ms:0,last_received_at_ms:2,providers:[]},{id:'active-machine',name:'有效采集机样本',revoked_at_ms:null,last_received_at_ms:1,providers:[]}];
    fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success(devices):String(path).includes('/source-usage')?success({range:fixture.range,items:devices.map(d=>({machine:{client_id:d.id,client_name:d.name},revoked_at_ms:d.revoked_at_ms,totals:fixture.totals,coverage:fixture.coverage})),scope:'collector_copies_may_overlap'}):response(path));
    mount();
    await screen.findByText('有效采集机样本');
    expect(screen.queryByText('已撤销采集机样本')).not.toBeInTheDocument();
    const totalsCard=screen.getByText('当前范围 Token 总量').closest('.summary-band')!;
    expect(totalsCard).toHaveTextContent('90071992.5亿');
    enter('各机器采集的 Codex 用量');
    expect(await screen.findByRole('button',{name:'有效采集机样本'})).toBeInTheDocument();
    expect(screen.queryByRole('button',{name:'已撤销采集机样本'})).not.toBeInTheDocument();
    const user=userEvent.setup();await user.click(screen.getByRole('button',{name:'筛选'}));
    await user.click(await screen.findByRole('combobox',{name:'采集来源'}));
    expect(await screen.findByText('有效采集机样本',{selector:'.ant-select-item-option-content'})).toBeInTheDocument();
    expect(screen.queryByText('已撤销采集机样本',{selector:'.ant-select-item-option-content'})).not.toBeInTheDocument();
  });
  it('shows usage totals before summary and defers charts and machine requests', async () => {
    let finish!: (value: Response) => void;
    fetcher.mockImplementation(path => String(path).includes('/annual') ? new Promise(resolve => { finish = resolve; }) : Promise.resolve(response(path)));
    mount();
    await screen.findByText('当前范围 Token 总量');
    expect(screen.getByText('正在读取全年活动…')).toBeInTheDocument();
    expect(screen.getByText('采集来源', { selector: '.ant-card-head-title' })).toBeInTheDocument();
    expect(fetcher.mock.calls.some(([path]) => String(path).includes('/source-usage'))).toBe(false);
    expect(fetcher.mock.calls.some(([path]) => String(path).includes('/summary')||String(path).includes('/statistics/usage')||String(path).includes('/activity')||String(path).includes('/top-sessions'))).toBe(false);
    expect(screen.queryByRole('img', { name: '按自然日的用量趋势' })).not.toBeInTheDocument();
    enter('模型用量趋势');
    await screen.findByRole('img', { name: '按自然日的用量趋势' });
    expect(fetcher.mock.calls.filter(([path]) => String(path).includes('/statistics/usage'))).toHaveLength(1);
    enter('各机器采集的 Codex 用量');
    await waitFor(() => expect(fetcher.mock.calls.filter(([path]) => String(path).includes('/source-usage'))).toHaveLength(1));
    await act(async () => finish(success(summaryFixture())));
    await screen.findByText('近 365 天 Token 总量');
  });
  it('refreshes machines only after activation and supports keyboard activation', async () => {
    fetcher.mockImplementation(path => Promise.resolve(response(path)));
    mount();
    await screen.findByText('当前范围 Token 总量');
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新' })).not.toHaveAttribute('aria-busy', 'true'));
    await user.click(screen.getByRole('button', { name: '刷新' }));
    await waitFor(() => expect(fetcher.mock.calls.filter(([path]) => String(path).includes('/statistics/totals'))).toHaveLength(2));
    expect(fetcher.mock.calls.some(([path]) => String(path).includes('/source-usage'))).toBe(false);
    const button = screen.getByRole('button', { name: '加载各机器采集的 Codex 用量' });
    button.focus(); await user.keyboard('{Enter}');
    await screen.findByText('机器', { selector: '.ant-table-cell' });
    await waitFor(() => expect(screen.getByLabelText('刷新', { selector: 'button' })).not.toHaveAttribute('aria-busy', 'true'));
    await user.click(screen.getByLabelText('刷新', { selector: 'button' }));
    await waitFor(() => expect(fetcher.mock.calls.filter(([path]) => String(path).includes('/source-usage'))).toHaveLength(2));
    expect(observers.find(o => o.target?.getAttribute('aria-label') === '各机器采集的 Codex 用量')?.disconnect).toHaveBeenCalled();
  });
  it('keeps usage usable on summary failure and cancels the old filter request', async () => {
    let oldSignal: AbortSignal | undefined;
    fetcher.mockImplementation((path, options) => {
      const url = new URL(String(path), 'http://localhost');
      if (url.pathname.endsWith('/annual')) return Promise.resolve(new Response('', { status: 503 }));
      if (url.pathname.endsWith('/totals') && !url.searchParams.has('provider')) {
        oldSignal = options?.signal ?? undefined;
        return new Promise((_resolve, reject) => oldSignal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))));
      }
      return Promise.resolve(response(path));
    });
    mount();
    await screen.findAllByText('数据未能读取');
    const user = userEvent.setup();
    await user.click(screen.getByRole('combobox', { name: 'Provider' }));
    await user.click(await screen.findByText('Cursor', { selector: '.ant-select-item-option-content' }));
    await screen.findByText('当前范围 Token 总量');
    expect(oldSignal?.aborted).toBe(true);
    expect(fetcher.mock.calls.some(([path]) => String(path).includes('/statistics/totals') && new URL(String(path), 'http://localhost').searchParams.get('provider') === 'cursor')).toBe(true);
  });
  it('preserves summary totals when model usage cannot be read', async () => {
    fetcher.mockImplementation(path => Promise.resolve(String(path).includes('/statistics/usage')
      ? new Response('', { status: 413 }) : response(path)));
    mount();
    await screen.findByText('当前范围 Token 总量');
    enter('模型用量趋势');
    await screen.findByText('数据未能读取');
    expect(document.querySelector('.summary-band')).toHaveTextContent('90071992.5亿');
  });
});
