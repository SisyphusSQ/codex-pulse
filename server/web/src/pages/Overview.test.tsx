import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { createQueryClient } from '../App';
import { api } from '../api/client';
import { summaryFixture } from '../test/statisticsFixture';
import { dollars, integer } from '../format';
import Overview from './Overview';

vi.mock('../components/Chart',()=>({default:({label}:{label:string})=><div role="img" aria-label={label} />}));
const fetcher=vi.fn<typeof fetch>();
const success=(data:unknown)=>new Response(JSON.stringify({code:200,data}));
beforeEach(()=>{api.setSession({client_id:'synthetic-browser',name:'测试浏览器',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',fetcher);});
afterEach(()=>vi.unstubAllGlobals());
describe('overview facts',()=>{
  it('formats exact integers, signed micro dollars, and unknown separately from zero',()=>{
    expect(integer('9007199254740993')).toBe('9,007,199,254,740,993');
    expect(integer(null)).toBe('未知');expect(integer('0')).toBe('0');
    expect(dollars('1')).toBe('$0.000001');expect(dollars('-1')).toBe('-$0.000001');
    expect(dollars('123456789')).toBe('$123.456789');expect(dollars(null)).toBe('未知');
  });
  it('uses Server totals and separate annual coverage, sends date/provider filters, and retains cached data on refresh failure',async()=>{
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):success(summaryFixture()));
    render(<QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider>);
    expect((await screen.findAllByText('9,007,199,254,740,993')).length).toBeGreaterThan(0);
    expect(screen.getByText('已知金额小计，含未定价记录 · 不是实际账单')).toBeInTheDocument();
    expect(screen.getByText('采集证据陈旧')).toBeInTheDocument();expect(await screen.findByText('有近期采集证据')).toBeInTheDocument();
    const user=userEvent.setup();await user.click(screen.getByRole('combobox',{name:'Provider'}));
    await user.click(await screen.findByText('Cursor',{selector:'.ant-select-item-option-content'}));
    await waitFor(()=>expect(fetcher.mock.calls.some(([path])=>new URL(String(path),'http://localhost').searchParams.get('provider')==='cursor')).toBe(true));
    const url=new URL(String(fetcher.mock.calls.find(([path])=>String(path).includes('summary'))![0]),'http://localhost');
    expect(url.searchParams.get('start_date')).toMatch(/^\d{4}-\d{2}-\d{2}$/);expect(url.searchParams.get('time_zone')).toBe('Asia/Shanghai');expect(url.searchParams.has('start_at_ms')).toBe(false);
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):new Response('',{status:503}));
    await user.click(screen.getByRole('button',{name:'刷新'}));
    await screen.findByText('刷新失败，以下保留上次读取的数据');expect(screen.getAllByText('$123.456789').length).toBeGreaterThan(0);
  });
  it('does not turn a failed initial query into an empty range',async()=>{
    fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):new Response('',{status:503}));
    render(<QueryClientProvider client={createQueryClient()}><Overview /></QueryClientProvider>);
    await screen.findByText('数据未能读取');expect(screen.queryByText('已收到 Token')).not.toBeInTheDocument();
  });
});
