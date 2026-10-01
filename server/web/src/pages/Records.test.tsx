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
  expect(screen.getByText('9,007,199,254,740,993')).toBeInTheDocument();
  const user=userEvent.setup();await user.click(screen.getByTitle('Next Page'));
  await screen.findByRole('button',{name:unsafe});
  expect(fetcher.mock.calls.some(([p])=>new URL(String(p),'http://localhost').searchParams.get('page')==='2')).toBe(true);
  await user.click(screen.getByRole('button',{name:unsafe}));await screen.findByRole('heading',{name:unsafe});
  expect(document.querySelector('img[src="x"]')).toBeNull();expect(screen.getAllByText('raw-two').length).toBe(2);
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
});
