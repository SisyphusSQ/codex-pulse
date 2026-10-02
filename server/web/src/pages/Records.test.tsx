import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { createQueryClient } from '../App';
import { api } from '../api/client';
import type { ProjectRecord, SessionRecord } from '../api/records';
import { partialCoverage, summaryFixture } from '../test/statisticsFixture';
import Projects from './Projects';
import Sessions from './Sessions';

vi.mock('../components/Chart',()=>({default:()=> <div>合成趋势图</div>}));
const fetcher=vi.fn<typeof fetch>();
const success=(data:unknown)=>new Response(JSON.stringify({code:200,data}));
const summary=summaryFixture();
const session=(id:string,title:string):SessionRecord=>({id,provider:'codex',session_id:`raw-${id}`,title,session_kind:'session',project_id:'a'.repeat(64),project_group_id:'a'.repeat(64),project_name:'合成项目',created_at_ms:1790812800000,last_active_at_ms:1790812800000,collected_at_ms:1790812800000,complete:false,conflict:true,sources:[],totals:{...summary.totals,total_tokens:'10',sessions:1}});
const project=(id:string,name:string):ProjectRecord=>({id,name,members:[id],totals:summary.totals,last_active_at_ms:null,conflict:false});
const records=(items:unknown[],page=1,total=30)=>({items,page:{page,limit:25,total},totals:summary.totals,coverage:partialCoverage,range:summary.range,scope:'global_deduplicated'});
beforeEach(()=>{api.setSession({client_id:'synthetic',name:'合成',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',fetcher);});
afterEach(()=>vi.unstubAllGlobals());
describe('server records and explicit project relationship',()=>{
 it('paginates and searches on Server, keeps full totals, opens escaped session details',async()=>{
  const unsafe='<img src=x onerror=alert(1)>';
  fetcher.mockImplementation(async path=>{
   const url=new URL(String(path),'http://localhost');
   if(url.pathname==='/api/v1/devices/status')return success([]);
   if(url.pathname==='/api/v1/sessions/two')return success({session:session('two',unsafe),range:summary.range,trend:[],tools:[],skills:[],coverage:partialCoverage});
   const page=Number(url.searchParams.get('page')??1);return success(records([session(page===1?'one':'two',page===1?'第一页会话':unsafe)],page));
  });
  render(<QueryClientProvider client={createQueryClient()}><Sessions /></QueryClientProvider>);
  await screen.findByRole('button',{name:'第一页会话'});
  expect(screen.getByText('90071992.5亿')).toBeInTheDocument();
  const user=userEvent.setup();await user.click(screen.getByTitle('Next Page'));
  await screen.findByRole('button',{name:unsafe});
  expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('page')==='2')).toBe(true);
  await user.click(screen.getByRole('button',{name:unsafe}));await screen.findByRole('heading',{name:unsafe});
  expect(document.querySelector('img[src="x"]')).toBeNull();expect(screen.getAllByText('raw-two').length).toBe(2);
  await user.click(screen.getByRole('button',{name:'关闭详情'}));
  await user.type(screen.getByRole('textbox',{name:'搜索标题、Session ID 或项目名'}),'raw-one');await user.click(screen.getByRole('button',{name:'应用搜索'}));
  await waitFor(()=>expect(fetcher.mock.calls.some(([p])=>{const u=new URL(String(p),'http://localhost');return u.pathname==='/api/v1/sessions'&&u.searchParams.get('search')==='raw-one'&&u.searchParams.get('page')==='1';})).toBe(true));
  expect(screen.queryByRole('heading',{name:unsafe})).not.toBeInTheDocument();
 });
 it('preserves selections across pages, requires explicit review, sends members and CSRF, clears selection after successful association',async()=>{
  fetcher.mockImplementation(async(path,options)=>{
   const url=new URL(String(path),'http://localhost');
   if(url.pathname==='/api/v1/devices/status')return success([]);
   if(url.pathname==='/api/v1/projects/associate'){expect(options?.headers).toHaveProperty('X-Pulse-CSRF','synthetic-csrf');return success({applied:true});}
   const page=Number(url.searchParams.get('page')??1);return success(records([project((page===1?'a':'b').repeat(64),page===1?'项目一':'项目二')],page));
  });
  render(<QueryClientProvider client={createQueryClient()}><Projects /></QueryClientProvider>);
  const user=userEvent.setup();await screen.findByRole('button',{name:'项目一'});await user.click(within(screen.getByRole('button',{name:'项目一'}).closest('tr')!).getByRole('checkbox'));
  await user.click(screen.getByTitle('Next Page'));await screen.findByRole('button',{name:'项目二'});await user.click(within(screen.getByRole('button',{name:'项目二'}).closest('tr')!).getByRole('checkbox'));
  await user.click(screen.getByRole('button',{name:'关联所选项目'}));const dialog=await screen.findByRole('dialog');expect(within(dialog).getByText(/项目一/,{selector:'li'})).toBeInTheDocument();expect(within(dialog).getByText(/项目二/,{selector:'li'})).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([p])=>p==='/api/v1/projects/associate')).toBe(false);
  await user.click(within(dialog).getByRole('button',{name:'确认执行'}));await waitFor(()=>expect(screen.getByText('关联所选项目').closest('button')).toBeDisabled());
  const mutation=fetcher.mock.calls.find(([p])=>p==='/api/v1/projects/associate');expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({project_ids:['a'.repeat(64),'b'.repeat(64)],target_id:'a'.repeat(64)});
  expect(screen.getByText('关联所选项目').closest('button')).toBeDisabled();
 });
 it('keeps the TPS tab selected after changing the recent-turn request and preserves lifecycle cache rate',async()=>{
  fetcher.mockImplementation(async path=>{
   const url=new URL(String(path),'http://localhost');
   if(url.pathname==='/api/v1/devices/status')return success([]);
   if(url.pathname==='/api/v1/sessions/one')return success({session:{...session('one','生命周期会话'),cache_hit_rate:{basis_points:'9000',input_tokens:'1000',cached_input_tokens:'900',unit:'basis_points',basis:'lifetime_cached_input',status:'complete',reason:'',source_client_id:null,conflict:false},throughput:{average_output_milli_tps:'22355',output_tokens:'16327',active_duration_ms:'730364',included_turns:'61',excluded_turns:'0',open_turns:'0',unattributed_events:'0',status:'complete',reason:'',duration_source:'duration_ms',basis:'closed_turn_lifetime_output',average_unit:'milli_tokens_per_second',duration_unit:'milliseconds',source_client_id:null,conflict:false}},range:summary.range,trend:[],tools:[],skills:[],coverage:partialCoverage,throughput_turns:{items:[],total:'61',limit:Number(url.searchParams.get('throughput_limit')),truncated:true}});
   return success(records([session('one','生命周期会话')],1,1));
  });
  render(<QueryClientProvider client={createQueryClient()}><Sessions /></QueryClientProvider>);
  const user=userEvent.setup();await user.click(await screen.findByRole('button',{name:'生命周期会话'}));await screen.findByText('90.0%');await user.click(screen.getByRole('tab',{name:'输出 TPS'}));await user.click(screen.getByRole('combobox',{name:'最近 TPS 轮次条数'}));await user.click(await screen.findByText('最近 50 条',{selector:'.ant-select-item-option-content'}));
  await waitFor(()=>expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('throughput_limit')==='50')).toBe(true));
  await waitFor(()=>expect(screen.getByRole('tab',{name:'输出 TPS'})).toHaveAttribute('aria-selected','true'));expect(screen.getByLabelText('average-output-tps')).toHaveTextContent(/22\.36\s*TPS/);
  await user.click(screen.getByRole('tab',{name:'用量与缓存'}));expect(screen.getByText('90.0%')).toBeInTheDocument();
 });

});
