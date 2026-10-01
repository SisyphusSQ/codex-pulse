import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { createQueryClient } from '../App';
import { api } from '../api/client';
import { quotaFixture, paceFixture } from '../test/quotaFixture';
import { paceChartOption } from '../components/QuotaViews';
import Quota from './Quota';

vi.mock('../components/Chart',()=>({default:({label}:{label:string})=><div role="img" aria-label={label} />}));
const fetcher=vi.fn<typeof fetch>();
const success=(data:unknown)=>new Response(JSON.stringify({code:200,data}));
beforeEach(()=>{api.setSession({client_id:'synthetic-browser',name:'测试浏览器',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',fetcher);});
afterEach(()=>vi.unstubAllGlobals());
function mount(){render(<QueryClientProvider client={createQueryClient()}><Quota /></QueryClientProvider>);}
describe('quota facts and pace',()=>{
 it('preserves observed zero, sparse forecast and exact credits with separate reset/expiry',async()=>{
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(quotaFixture()));
  mount();expect((await screen.findAllByText('0%')).length).toBeGreaterThan(0);await screen.findByText('实际采样数量或跨度不足');expect(screen.getByText('9,007,199,254,740,993')).toBeInTheDocument();
  expect(screen.getByText('正值代表使用快于均匀节奏')).toBeInTheDocument();expect(screen.getByText('10.00 个百分点')).toBeInTheDocument();
  const credit=screen.getByText('Reset Credits').closest('.ant-card')!;expect(within(credit as HTMLElement).getByText('下一次 reset')).toBeInTheDocument();expect(within(credit as HTMLElement).getAllByText('未知').length).toBeGreaterThan(0);
 });
 it('shows expired last value without countdown, keeps original times after failed refresh, and escapes account metadata',async()=>{
  const quota=quotaFixture();quota.windows[0].current={...quota.windows[0].current,used_percent:50,remaining_percent:50,freshness:'expired_unknown',reason:'expired_unknown',reset_remaining_ms:null};quota.accounts[0].email='<img src=x onerror=alert(1)>';
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(quota));mount();
  await screen.findByText('未提供可信倒计时');expect(screen.getByText('reset 已过，等待新观测 · 窗口 300 分钟')).toBeInTheDocument();expect(document.querySelector('img[src="x"]')).toBeNull();
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):new Response('',{status:503}));await userEvent.setup().click(screen.getByRole('button',{name:'刷新额度'}));
  await screen.findByText('额度刷新失败，保留上次读取的数据与原时间');expect(document.querySelectorAll('.quota-numbers strong')[0]?.textContent).toBe('50%');expect(screen.getByText('未提供可信倒计时')).toBeInTheDocument();
 });
 it('uses raw-ID account identities even for equal emails and sends explicit account/provider/source filters',async()=>{
  fetcher.mockImplementation(async(path)=>{const u=new URL(String(path),'http://localhost');if(u.pathname.endsWith('/devices/status'))return success([{id:'machine-one',name:'合成采集机',providers:[],revoked_at_ms:null,last_received_at_ms:null}]);if(u.pathname.endsWith('/pace'))return success(paceFixture());const data=quotaFixture();if(u.searchParams.get('account_key')==='account-two'){data.windows=[];data.credits=[];}return success(data);});
  mount();await screen.findAllByText('0%');const user=userEvent.setup();await user.click(screen.getByRole('combobox',{name:'额度账号'}));await user.click(await screen.findByText('Codex · same@example.invalid · raw-account-two',{selector:'.ant-select-item-option-content'}));
  await screen.findByText('尚无已收到的额度或 Credits。请启用设备上报；未确认账号不根据邮箱推断归属。');await waitFor(()=>expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('account_key')==='account-two')).toBe(true));
  await user.click(screen.getByRole('combobox',{name:'额度采集来源'}));await user.click(await screen.findByText('合成采集机',{selector:'.ant-select-item-option-content'}));await waitFor(()=>expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('client_id')==='machine-one')).toBe(true));
  await user.click(screen.getByRole('combobox',{name:'额度 Provider'}));await user.click(await screen.findByText('Cursor',{selector:'.ant-select-item-option-content'}));await waitFor(()=>expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('provider')==='cursor')).toBe(true));
 });
 it('keeps a descending real sample, original endpoints, and no interpolated point across collection gaps',()=>{
  const w=paceFixture().windows[0];const option=paceChartOption(w);const series=option.series as {data:{value:number[];observedAt:number}[];lineStyle:{opacity?:number}}[];
  expect(series[0].data).toHaveLength(2);expect(series[0].data.map(p=>p.value[1])).toEqual([20,30]);expect(series[0].data.map(p=>p.observedAt)).toEqual(w.current_points.map(p=>p.observed_at_ms));expect(series[0].lineStyle.opacity).toBe(0);
 });
});
