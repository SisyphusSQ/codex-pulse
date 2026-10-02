import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { createQueryClient } from '../App';
import { api } from '../api/client';
import { quotaFixture, paceFixture } from '../test/quotaFixture';
import { paceChartOption } from '../components/QuotaViews';
import Quota,{legacyUsageTarget} from './Quota';
import {MemoryRouter} from 'react-router-dom';

vi.mock('../components/Chart',()=>({default:({label}:{label:string})=><div role="img" aria-label={label} />}));
const fetcher=vi.fn<typeof fetch>();
const success=(data:unknown)=>new Response(JSON.stringify({code:200,data}));
beforeEach(()=>{api.setSession({client_id:'synthetic-browser',name:'测试浏览器',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',async (...args:Parameters<typeof fetch>)=>String(args[0]).includes('/subscription')?success({account_key:String(args[0]).split('/')[4],revision:'0',alias:null,automatic_plan:null,manual_plan:null,resolved_plan:null,date_kind:'',renewal_day:null,membership_date:null,next_date:null,day_delta:null,date_state:'unavailable',time_zone:'Asia/Shanghai',updated_at_ms:null}):fetcher(...args));});
afterEach(()=>vi.unstubAllGlobals());
function mount(){render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><Quota /></MemoryRouter></QueryClientProvider>);}
describe('quota facts and pace',()=>{
 it('preserves observed zero, sparse forecast and exact credits with separate reset/expiry',async()=>{
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(quotaFixture()));
  mount();expect((await screen.findAllByText('0%')).length).toBeGreaterThan(0);await userEvent.setup().click(screen.getByRole('button',{name:'节奏评估说明'}));await screen.findByText('实际采样数量或跨度不足');
  await userEvent.setup().click(screen.getByText('库存来源与到期明细'));
  expect(screen.getByText('正值代表使用快于均匀节奏')).toBeInTheDocument();expect(screen.getByLabelText('节奏偏差（百分点）')).toHaveTextContent('10.00');
  expect(screen.queryByRole('tab',{name:'Reset Credits (1)'})).not.toBeInTheDocument();expect(screen.getByText('9,007,199,254,740,993')).toBeInTheDocument();
  const credit=screen.getByText('Reset Credits').closest('.credits-section')!;expect(within(credit as HTMLElement).getByText('下一次 reset')).toBeInTheDocument();expect(within(credit as HTMLElement).getAllByText('未知').length).toBeGreaterThan(0);
 });
 it('shows expired last value without countdown, keeps original times after failed refresh, and escapes account metadata',async()=>{
  const quota=quotaFixture();quota.windows[0].current={...quota.windows[0].current,used_percent:50,remaining_percent:50,freshness:'expired_unknown',reason:'expired_unknown',reset_remaining_ms:null};quota.accounts[0].email='<img src=x onerror=alert(1)>';
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(quota));mount();
  await screen.findByText('当前未知');expect(document.querySelector('.quota-window-summary .ant-progress')).toBeNull();expect(screen.getByText(/上次剩余 50%/)).toBeInTheDocument();await screen.findByText('额度窗口与来源详情');await userEvent.setup().click(screen.getByText('额度窗口与来源详情'));await screen.findByText('未提供可信倒计时');expect(screen.getByText('reset 已过，等待新观测 · 窗口 300 分钟')).toBeInTheDocument();expect(document.querySelector('img[src="x"]')).toBeNull();
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):new Response('',{status:503}));await userEvent.setup().click(screen.getByRole('button',{name:'刷新额度'}));
  await screen.findByText('额度刷新失败，保留上次读取的数据与原时间');expect(document.querySelectorAll('.quota-numbers strong')[0]?.textContent).toBe('50%');expect(screen.getByText('未提供可信倒计时')).toBeInTheDocument();
 });
 it('uses raw-ID account identities even for equal emails and sends explicit account/provider/source filters',async()=>{
  fetcher.mockImplementation(async(path)=>{const u=new URL(String(path),'http://localhost');if(u.pathname.endsWith('/devices/status'))return success([{id:'machine-one',name:'合成采集机',providers:[],revoked_at_ms:null,last_received_at_ms:null}]);if(u.pathname.endsWith('/pace'))return success(paceFixture());const data=quotaFixture();if(u.searchParams.get('account_key')==='account-two'){data.accounts=data.accounts.filter(a=>a.key==='account-two');data.windows=[];data.credits=[];}return success(data);});
  mount();await screen.findAllByText('0%');const user=userEvent.setup();await user.click(screen.getByRole('combobox',{name:'额度账号'}));await user.click(await screen.findByText('Codex · same@example.invalid · raw-account-two',{selector:'.ant-select-item-option-content'}));
  await screen.findByText('当前账号暂无已收到的额度窗口。');expect(screen.getByText('当前账号暂无已收到的 Reset Credits，库存保持未知。')).toBeInTheDocument();await waitFor(()=>expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('account_key')==='account-two')).toBe(true));
  await user.click(screen.getByRole('button',{name:'筛选'}));await user.click(screen.getByRole('combobox',{name:'额度采集来源'}));await user.click(await screen.findByText('合成采集机',{selector:'.ant-select-item-option-content'}));await waitFor(()=>expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('client_id')==='machine-one')).toBe(true));
  await user.click(screen.getByRole('combobox',{name:'额度 Provider'}));await user.click(await screen.findByText('Cursor',{selector:'.ant-select-item-option-content'}));await waitFor(()=>expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('provider')==='cursor')).toBe(true));
 });
 it('keeps a descending real sample, original endpoints, and no interpolated point across collection gaps',()=>{
  const w=paceFixture().windows[0];const option=paceChartOption(w);const series=option.series as {data:{value:number[];observedAt:number}[];lineStyle:{width:number};showSymbol:boolean;connectNulls:boolean}[];
  expect(series[0].data).toHaveLength(2);expect(series[0].data.map(p=>p.value[1])).toEqual([20,30]);expect(series[0].data.map(p=>p.observedAt)).toEqual(w.current_points.map(p=>p.observed_at_ms));expect(series[0].lineStyle.width).toBe(2.5);expect(series[0].showSymbol).toBe(false);expect(series[0].connectNulls).toBe(true);
  w.current={...w.current,freshness:'stale'};expect((paceChartOption(w).series as {name:string}[]).some(s=>s.name==='本周期')).toBe(false);
 });

 it('keeps windows, resets and credits inside one selected raw-ID account even when emails match',async()=>{
  const data=quotaFixture();
  data.windows.push({...data.windows[0],key:'window-two',account_key:'account-two',current:{...data.windows[0].current,used_percent:25,remaining_percent:75}});
  data.credits.push({...data.credits[0],key:'credit-two',account_key:'account-two',observed_inventory:'3',available_inventory:'2'});
  fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(data));
  mount();await screen.findByText('Reset Credits');await screen.findByText('9,007,199,254,740,993');
  const user=userEvent.setup();await user.click(screen.getByRole('button',{name:'查看账号 Codex same@example.invalid raw-account-two'}));
  const detail=document.querySelector('.quota-detail') as HTMLElement;
  expect(within(detail).getByText('原始账号 ID：raw-account-two')).toBeInTheDocument();expect(within(detail).getByText('25%')).toBeInTheDocument();expect(within(detail).getByText('3',{selector:'.metric-value'})).toBeInTheDocument();expect(within(detail).queryByText('9,007,199,254,740,993')).not.toBeInTheDocument();
  await user.click(screen.getByRole('button',{name:'查看账号 Codex same@example.invalid raw-account-one'}));expect(within(document.querySelector('.quota-detail') as HTMLElement).getAllByText('0%').length).toBeGreaterThan(0);expect(screen.getByText('9,007,199,254,740,993')).toBeInTheDocument();
 });

 it('shows a credits-only account and keeps unassigned observations outside confirmed accounts',async()=>{
  const data=quotaFixture();data.windows=[];data.credits=[{...data.credits[0],account_key:'account-two',observed_inventory:'0',available_inventory:'0'},{...data.credits[0],key:'pending-credit',account_key:null,observed_inventory:'7',available_inventory:null,freshness:'unassigned'}];
  fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success({...paceFixture(),windows:[]}):success(data));
  mount();await screen.findByText('当前账号暂无已收到的额度窗口。');
  let detail=document.querySelector('.quota-detail') as HTMLElement;expect(within(detail).getByText('原始账号 ID：raw-account-two')).toBeInTheDocument();expect(within(detail).getAllByText('0',{selector:'.metric-value'})).toHaveLength(2);expect(within(detail).queryByText('7',{selector:'.metric-value'})).not.toBeInTheDocument();
  await userEvent.setup().click(screen.getByRole('button',{name:'查看待关联观测'}));detail=document.querySelector('.quota-detail') as HTMLElement;
  expect(within(detail).getByText('以下观测各自展示，不视为同一账号，也不按邮箱或采集设备推断归属。')).toBeInTheDocument();expect(within(detail).getByText('7',{selector:'.metric-value'})).toBeInTheDocument();expect(within(detail).queryByText('原始账号 ID：raw-account-two')).not.toBeInTheDocument();
 });
});

it('preserves provider, model and version parameters when migrating the legacy usage link',()=>{
 expect(legacyUsageTarget(new URLSearchParams('view=usage&provider=codex&model=gpt-6.1-sol&version=old'))).toBe('/usage/models?provider=codex&model=gpt-6.1-sol&version=old');
});
