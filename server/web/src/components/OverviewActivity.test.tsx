import {describe,it,expect,vi,afterEach} from 'vitest';
import {render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {MemoryRouter} from 'react-router-dom';
import {summaryFixture} from '../test/statisticsFixture';
import {OverviewActivity,sessionsTarget} from './OverviewActivity';
import {initialFilter} from './StatsFilters';
vi.mock('./Chart',()=>({default:({onClick,label}:{onClick?:(i:number)=>void;label:string})=><button aria-label={label} onClick={()=>onClick?.(0)}>图表</button>}));
afterEach(()=>vi.restoreAllMocks());
describe('overview activity interactions',()=>{
 it('preserves date/provider/machine filters on ranked session navigation',()=>{
  const filter={...initialFilter(1),provider:'codex',client_id:'machine',model:'model-a'};
  const target=sessionsTarget(filter,'session');const params=new URLSearchParams(target.split('?')[1]);
  expect(params.get('client_id')).toBe('machine');expect(params.get('start_date')).toBe(filter.start_date);expect(params.get('sort')).toBe('tokens');expect(params.get('selected')).toBe('session');
 });
 it('drills a daily bucket into its IANA natural day and leaves unknown heatmap cells explicit',async()=>{
  const data=summaryFixture();data.activity_granularity='day';data.activity_timeline=[{start_at_ms:Date.parse('2026-10-02T16:00:00Z'),end_at_ms:Date.parse('2026-10-03T16:00:00Z'),tokens:'100',sessions:'1'}];data.weekday_hours=[{weekday:6,hour:9,tokens:null,sessions:0,session_count:null}];const onDay=vi.fn();
  render(<MemoryRouter><OverviewActivity data={data} filter={initialFilter(30)} onDay={onDay}/></MemoryRouter>);
  await userEvent.setup().click(await screen.findByRole('button',{name:'每日用量，点击查看小时'}));
  expect(onDay).toHaveBeenCalledWith('2026-10-03');expect(screen.getByLabelText('周六 9时 · 未取得')).toBeInTheDocument();
 });
});
