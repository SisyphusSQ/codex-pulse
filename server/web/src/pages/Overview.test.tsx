import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { createQueryClient } from '../App';
import { api } from '../api/client';
import { summaryFixture } from '../test/statisticsFixture';
import { dayjs, dollars, integer } from '../format';
import Overview from './Overview';

vi.mock('../components/Chart',()=>({default:({label}:{label:string})=><div role="img" aria-label={label} />}));
const fetcher=vi.fn<typeof fetch>();
const success=(data:unknown)=>new Response(JSON.stringify({code:200,data}));
beforeEach(()=>{api.setSession({client_id:'synthetic-browser',name:'测试浏览器',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',fetcher);});
afterEach(()=>vi.unstubAllGlobals());
describe('overview facts',()=>{
  it('requests the seven-day range and opens a real date picker for custom dates',async()=>{
    fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):success(summaryFixture()));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await screen.findByText('Token 总量');const user=userEvent.setup();
    await user.click(screen.getByText('7天',{selector:'.ant-segmented-item-label'}));
    await waitFor(()=>expect(fetcher.mock.calls.some(([path])=>{const u=new URL(String(path),'http://localhost');return u.pathname.endsWith('/summary')&&dayjs(u.searchParams.get('end_date_exclusive')).diff(dayjs(u.searchParams.get('start_date')),'day')===7;})).toBe(true));
    await user.click(screen.getByText('自定义',{selector:'.ant-segmented-item-label'}));
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
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):success(summaryFixture()));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    expect((await screen.findAllByText('90071992.5亿')).length).toBeGreaterThan(0);
    expect(screen.getByText('已知金额小计，含未定价记录 · 不是实际账单')).toBeInTheDocument();
    expect(screen.getByRole('button',{name:/采集证据陈旧/})).toBeInTheDocument();expect(await screen.findByText('年度覆盖未确认 · 近期采集')).toBeInTheDocument();
    const user=userEvent.setup();await user.click(screen.getByRole('combobox',{name:'Provider'}));
    await user.click(await screen.findByText('Cursor',{selector:'.ant-select-item-option-content'}));
    await waitFor(()=>expect(fetcher.mock.calls.some(([path])=>new URL(String(path),'http://localhost').searchParams.get('provider')==='cursor')).toBe(true));
    const url=new URL(String(fetcher.mock.calls.find(([path])=>String(path).includes('summary'))![0]),'http://localhost');
    expect(url.searchParams.get('start_date')).toMatch(/^\d{4}-\d{2}-\d{2}$/);expect(url.searchParams.get('time_zone')).toBe('Asia/Shanghai');expect(url.searchParams.has('start_at_ms')).toBe(false);
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):new Response('',{status:503}));
    await user.click(screen.getByRole('button',{name:'刷新'}));
    await screen.findByText('刷新失败，以下保留上次读取的数据');expect(screen.getAllByText('$123.46').length).toBeGreaterThan(0);
  });
  it('does not turn a failed initial query into an empty range',async()=>{
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):new Response('',{status:503}));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await screen.findByText('数据未能读取');expect(screen.queryByText('Token 总量')).not.toBeInTheDocument();
  });
  it('places annual activity above summary and trend while keeping independent Server annual metrics',async()=>{
    const fixture=summaryFixture();
    fixture.heatmap_activity={total_tokens:'999999999999999999',peak_daily_tokens:'100000',active_days:'15',current_streak_days:null,longest_streak_days:'4',observed_days:22,unknown_days:343};
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):success(fixture));
    render(<MemoryRouter><QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider></MemoryRouter>);
    await userEvent.setup().click(await screen.findByRole('button',{name:/年度活动统计/}));
    expect(await screen.findByText('10000000000亿',{selector:'strong'})).toBeInTheDocument();
    expect(screen.getByText('当前连续天数').parentElement).toHaveTextContent('未知');
    expect(screen.getByRole('link',{name:'查看账号额度与节奏'})).toHaveAttribute('href','/quota');
    await screen.findByRole('img',{name:'按自然日的用量趋势'});
    const activity=screen.getByText('全年活动').closest('.ant-card')!;
    const trend=screen.getByText('每日用量趋势').closest('.ant-card')!;
    const summary=screen.getByText('Token 总量').closest('.summary-band')!;
    expect(activity.compareDocumentPosition(summary)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(activity.compareDocumentPosition(trend)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
