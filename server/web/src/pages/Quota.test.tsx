import {OperationNotifications} from '../components/OperationNotifications';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { createQueryClient } from '../App';
import { api } from '../api/client';
import { quotaFixture, paceFixture } from '../test/quotaFixture';
import { paceChartOption } from '../components/QuotaViews';
import { dateTime } from '../format';
import Quota,{legacyUsageTarget} from './Quota';
import {MemoryRouter} from 'react-router-dom';

vi.mock('../components/Chart',()=>({default:({label}:{label:string})=><div role="img" aria-label={label} />}));
const fetcher=vi.fn<typeof fetch>();
const success=(data:unknown)=>new Response(JSON.stringify({code:200,data}));
beforeEach(()=>{api.setSession({client_id:'synthetic-browser',name:'测试浏览器',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',async (...args:Parameters<typeof fetch>)=>String(args[0]).includes('/subscription')?success({account_key:String(args[0]).split('/')[4],revision:'0',alias:null,automatic_plan:null,manual_plan:null,resolved_plan:null,date_kind:'',renewal_day:null,membership_date:null,next_date:null,day_delta:null,date_state:'unavailable',time_zone:'Asia/Shanghai',updated_at_ms:null}):fetcher(...args));});
afterEach(()=>vi.unstubAllGlobals());
function mount(){render(<OperationNotifications><QueryClientProvider client={createQueryClient()}><MemoryRouter><Quota /></MemoryRouter></QueryClientProvider></OperationNotifications>);}
describe('quota facts and pace',()=>{
 it('loads summary and selected pace without a source evidence entry',async()=>{
  const data=quotaFixture();data.windows[0].observation_count=41;
  fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(data));
  mount();await screen.findByRole('button',{name:'节奏评估说明'});
  const urls=()=>fetcher.mock.calls.map(([p])=>new URL(String(p),'http://localhost'));
  expect(urls().some(u=>u.pathname==='/api/v1/quotas/accounts')).toBe(true);
  expect(urls().some(u=>u.searchParams.get('view')==='summary')).toBe(true);
  expect(urls().filter(u=>u.pathname.endsWith('/pace')).every(u=>u.searchParams.get('window_key')===data.windows[0].key)).toBe(true);
  expect(urls().some(u=>u.searchParams.get('view')==='evidence')).toBe(false);
  expect(screen.queryByRole('tab',{name:/来源证据/})).not.toBeInTheDocument();
  expect(screen.getByText('节奏与历史')).toBeInTheDocument();
 });
 it.each(['stale','expired_unknown'])('keeps the last snapshot and forecast when polling changes freshness to %s',async freshness=>{
  const quota=quotaFixture(),pace=paceFixture();
  quota.windows[0].current={...quota.windows[0].current,used_percent:70,remaining_percent:30};
  pace.windows[0].current=quota.windows[0].current;
  pace.windows[0].forecast={state:'at_risk',method:'recent_theil_sen',exhaust_at_ms:quota.evaluated_at_ms+1800000,lead_before_reset_ms:1800000,evidence_count:4,evidence_span_ms:1800000,unknown_reason:null};
  quota.credits[0]={...quota.credits[0],observed_inventory:'3',snapshot_available_inventory:'2',snapshot_next_expires_at_ms:quota.evaluated_at_ms+60000};
  fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(pace):success(quota));
  mount();await screen.findByText('剩余 30%');await screen.findByText('预计会在 reset 前耗尽');
  const observed=quota.windows[0].current.observed_at_ms;
  quota.evaluated_at_ms+=10*86400000;pace.evaluated_at_ms=quota.evaluated_at_ms;
  quota.windows[0].current={...quota.windows[0].current,freshness,reason:freshness,reset_remaining_ms:null};pace.windows[0].current=quota.windows[0].current;
  quota.credits[0]={...quota.credits[0],freshness:'stale',available_inventory:null,next_expires_at_ms:null};
  await userEvent.setup().click(screen.getByRole('button',{name:'刷新额度'}));
  await waitFor(()=>expect(screen.getByText(`中心读取 ${dateTime(quota.evaluated_at_ms)}`)).toBeInTheDocument());
  expect(screen.getByText('剩余 30%')).toBeInTheDocument();expect(screen.getByRole('progressbar',{name:'最后观测剩余额度'})).toBeInTheDocument();
  expect(screen.getAllByText(`最后更新 ${dateTime(observed)}`).length).toBeGreaterThan(1);
  expect(screen.getByLabelText('节奏偏差（百分点）')).toHaveTextContent('10.00');expect(screen.getByText('60%')).toBeInTheDocument();
  expect(screen.getByRole('img',{name:'观测周期、上一周期、历史中位数与理想节奏'})).toBeInTheDocument();expect(screen.getByText('预计会在 reset 前耗尽')).toBeInTheDocument();
  expect(screen.queryByText('当前未知')).not.toBeInTheDocument();expect(screen.queryByText('陈旧')).not.toBeInTheDocument();
  const credits=screen.getByRole('region',{name:'Reset Credits 库存'});
  expect(within(credits).getByText('3',{selector:'.metric-value'})).toBeInTheDocument();expect(within(credits).getByText('2',{selector:'.metric-value'})).toBeInTheDocument();
  expect(within(credits).getByText(dateTime(quota.credits[0].snapshot_next_expires_at_ms))).toBeInTheDocument();
 });
 it('keeps missing quota unknown and exposes real conflicts beside saved values',async()=>{
  const quota=quotaFixture(),pace=paceFixture();
  quota.windows[0].current={...quota.windows[0].current,used_percent:null,remaining_percent:null,observed_at_ms:null,resets_at_ms:null,freshness:'never_loaded',reason:'unavailable'};
  pace.windows[0]={...pace.windows[0],current:quota.windows[0].current,snapshot_at_ms:null,elapsed_percent:null,pace_delta_pp:null,current_points:[],forecast:{...pace.windows[0].forecast,unknown_reason:'window_unavailable'}};
  fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(pace):success(quota));
  mount();await screen.findByText('尚无有效观测');expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();expect(screen.queryByRole('img',{name:'观测周期、上一周期、历史中位数与理想节奏'})).not.toBeInTheDocument();
  quota.windows[0].current={...quota.windows[0].current,used_percent:40,remaining_percent:60,observed_at_ms:quota.evaluated_at_ms,conflict:true,reason:'source_conflict',freshness:'fresh'};
  await userEvent.setup().click(screen.getByRole('button',{name:'刷新额度'}));await screen.findByText('剩余 60%');expect(screen.getByText(/来源冲突/)).toBeInTheDocument();
 });
 it('preserves observed zero, sparse forecast and exact credits with separate reset/expiry',async()=>{
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(quotaFixture()));
  mount();expect((await screen.findAllByText('0%')).length).toBeGreaterThan(0);await userEvent.setup().click(await screen.findByRole('button',{name:'节奏评估说明'}));await screen.findByText('实际采样数量或跨度不足');
  await userEvent.setup().click(screen.getByText('库存到期明细'));
  expect(screen.getByText('正值代表使用快于均匀节奏')).toBeInTheDocument();expect(screen.getByLabelText('节奏偏差（百分点）')).toHaveTextContent('10.00');
  expect(screen.queryByRole('tab',{name:'Reset Credits (1)'})).not.toBeInTheDocument();expect(screen.getByText('9,007,199,254,740,993')).toBeInTheDocument();
  const credit=screen.getByText('Reset Credits').closest('.credits-section')!;expect(within(credit as HTMLElement).getByText('下一次 reset')).toBeInTheDocument();expect(within(credit as HTMLElement).getAllByText('未知').length).toBeGreaterThan(0);
 });
 it('shows expired last value without countdown, keeps original times after failed refresh, and escapes account metadata',async()=>{
  const quota=quotaFixture();quota.windows[0].current={...quota.windows[0].current,used_percent:50,remaining_percent:50,freshness:'expired_unknown',reason:'expired_unknown',reset_remaining_ms:null};quota.accounts[0].email='<img src=x onerror=alert(1)>';
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(quota));mount();
  await screen.findByText('剩余 50%');expect(document.querySelector('.quota-window-summary .ant-progress')).not.toBeNull();expect(screen.queryByText('当前未知')).not.toBeInTheDocument();expect(screen.queryByText('额度窗口与来源详情')).not.toBeInTheDocument();expect(document.querySelector('img[src="x"]')).toBeNull();
  fetcher.mockImplementation(async(path)=>String(path).includes('/devices/status')?success([]):new Response('',{status:503}));await userEvent.setup().click(screen.getByRole('button',{name:'刷新额度'}));
  await screen.findByText('额度刷新失败，保留上次读取的数据与原时间');expect(screen.getByText('剩余 50%')).toBeInTheDocument();
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
  w.current={...w.current,freshness:'stale'};expect((paceChartOption(w).series as {name:string}[]).some(s=>s.name==='观测周期')).toBe(true);
 });

 it('keeps windows, resets and credits inside one selected raw-ID account even when emails match',async()=>{
  const data=quotaFixture();data.accounts[1].plan='Plus';
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
  const data=quotaFixture();data.windows=[];data.credits=[{...data.credits[0],account_key:'account-two',observed_inventory:'0',available_inventory:'0',snapshot_available_inventory:'0'},{...data.credits[0],key:'pending-credit',account_key:null,observed_inventory:'7',available_inventory:null,freshness:'unassigned'}];
  fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success({...paceFixture(),windows:[]}):success(data));
  mount();await screen.findByText('当前账号暂无已收到的额度窗口。');
  await userEvent.setup().click(screen.getByRole('button',{name:'刷新额度'}));
  await waitFor(()=>expect(fetcher.mock.calls.filter(([p])=>new URL(String(p),'http://localhost').searchParams.get('view')==='summary').length).toBeGreaterThan(1));
  expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').pathname.endsWith('/pace'))).toBe(false);
  let detail=document.querySelector('.quota-detail') as HTMLElement;expect(within(detail).getByText('原始账号 ID：raw-account-two')).toBeInTheDocument();expect(within(detail).getAllByText('0',{selector:'.metric-value'})).toHaveLength(2);expect(within(detail).queryByText('7',{selector:'.metric-value'})).not.toBeInTheDocument();
  expect(screen.queryByRole('button',{name:'查看待关联观测'})).not.toBeInTheDocument();
  expect(screen.queryByText('7',{selector:'.metric-value'})).not.toBeInTheDocument();
 });
});

it('preserves provider, model and version parameters when migrating the legacy usage link',()=>{
 expect(legacyUsageTarget(new URLSearchParams('view=usage&provider=codex&model=gpt-6.1-sol&version=old'))).toBe('/usage/models?provider=codex&model=gpt-6.1-sol&version=old');
});
it('hides Pro monthly and short windows and shows missing Plus slots as unavailable',async()=>{
 const data=quotaFixture();data.accounts[1].plan='Plus';data.windows.push({...data.windows[0],key:'old-month',window_minutes:43200},{...data.windows[0],key:'old-short',window_minutes:300});
 fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(data));mount();
 expect(await screen.findByRole('button',{name:'codex · 7 天'})).toBeInTheDocument();expect(screen.queryByRole('button',{name:'codex · 30 天'})).not.toBeInTheDocument();expect(screen.queryByRole('button',{name:'codex · 5 小时'})).not.toBeInTheDocument();
 await userEvent.setup().click(screen.getByRole('button',{name:/查看账号 .*raw-account-two/}));expect(screen.getAllByText('未取得')).toHaveLength(2);expect(screen.getByText('5 小时')).toBeInTheDocument();expect(screen.getByText('7 天')).toBeInTheDocument();
});

it.each([["0","0"],["12345678","1234.6万"],["1234567890","12.3亿"],[null,"—"]])('shows account weekly recorded tokens %s with zero/unknown distinct',async(value,display)=>{
 const data=quotaFixture();data.windows[0].recorded_tokens=value;
 fetcher.mockImplementation(async path=>String(path).includes('/devices/status')?success([]):String(path).includes('/pace')?success(paceFixture()):success(data));mount();
 const label=await screen.findByText('本周期已记录 Token：');expect(label.parentElement).toHaveTextContent(String(display));
});
