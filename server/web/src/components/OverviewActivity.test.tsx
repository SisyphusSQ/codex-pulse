import {describe,it,expect,vi,afterEach} from 'vitest';
import {render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {MemoryRouter} from 'react-router-dom';
import {summaryFixture} from '../test/statisticsFixture';
import {OverviewActivity,MachineUsage,sessionsTarget} from './OverviewActivity';
import {QueryClientProvider} from '@tanstack/react-query';
import {createQueryClient} from '../App';
import {api} from '../api/client';
import {unknownTotals,partialCoverage} from '../test/statisticsFixture';
import {initialFilter} from './StatsFilters';
vi.mock('./Chart',()=>({default:({onClick,label}:{onClick?:(i:number)=>void;label:string})=><button aria-label={label} onClick={()=>onClick?.(0)}>图表</button>}));
afterEach(()=>{vi.restoreAllMocks();vi.unstubAllGlobals();});
describe('overview activity interactions',()=>{
 it('preserves date/provider/machine filters on ranked session navigation',()=>{
  const filter={...initialFilter(1),provider:'codex',client_id:'machine',model:'model-a'};
  const target=sessionsTarget(filter,'session');const params=new URLSearchParams(target.split('?')[1]);
  expect(params.get('client_id')).toBe('machine');expect(params.get('start_date')).toBe(filter.start_date);expect(params.get('sort')).toBe('tokens');expect(params.get('selected')).toBe('session');
 });
 it('drills a daily bucket into its IANA natural day and displays empty heatmap cells as zero',async()=>{
  const data=summaryFixture();data.activity_granularity='day';data.activity_timeline=[{start_at_ms:Date.parse('2026-10-02T16:00:00Z'),end_at_ms:Date.parse('2026-10-03T16:00:00Z'),tokens:'100',sessions:'1'}];data.weekday_hours=[{weekday:6,hour:9,tokens:null,sessions:0,session_count:null}];const onDay=vi.fn();
  render(<MemoryRouter><OverviewActivity data={data} filter={initialFilter(30)} onDay={onDay}/></MemoryRouter>);
  await userEvent.setup().click(await screen.findByRole('button',{name:'每日用量，点击查看小时'}));
  expect(onDay).toHaveBeenCalledWith('2026-10-03');expect(screen.getByLabelText('周六 9时 · 0 Token')).toBeInTheDocument();
 });
});

it('shows zero for an empty machine and dashes for incomplete records',async()=>{
 api.setSession({client_id:'synthetic-browser',name:'测试',purpose:'admin',csrf:'synthetic',expires_at_ms:null});
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({code:200,data:{range:summaryFixture().range,scope:'collector_copies_may_overlap',items:[
  {machine:{client_id:'empty',client_name:'空机器'},totals:unknownTotals,coverage:partialCoverage,revoked_at_ms:null},
  {machine:{client_id:'missing',client_name:'缺字段机器'},totals:{...unknownTotals,sessions:1},coverage:partialCoverage,revoked_at_ms:null},
 ]}}))));
 render(<QueryClientProvider client={createQueryClient()}><MachineUsage filter={initialFilter(1)} onSelect={()=>{}}/></QueryClientProvider>);
 const empty=(await screen.findByRole('button',{name:'空机器'})).closest('tr')!;
 expect([...empty.querySelectorAll('td')].slice(1,5).map(c=>c.textContent)).toEqual(['0','0','0','0']);
 const missing=screen.getByRole('button',{name:'缺字段机器'}).closest('tr')!;
 expect([...missing.querySelectorAll('td')].slice(1,5).map(c=>c.textContent)).toEqual(['—','—','—','1']);
 expect(screen.queryByText('覆盖未确认')).not.toBeInTheDocument();
});
