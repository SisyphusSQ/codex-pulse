import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { Day } from '../api/statistics';
import { unknownTotals } from '../test/statisticsFixture';
import { dayjs } from '../format';
import { ActivityHeatmap, activityCalendar } from './ActivityHeatmap';

function day(date:string,tokens:string|null):Day {
  return {date,start_at_ms:dayjs.utc(date).valueOf(),totals:{...unknownTotals,total_tokens:tokens}};
}

describe('aligned annual activity',()=>{
  it('aligns Sunday rows across month and DST boundaries without timezone-dependent shifts',()=>{
    const days=Array.from({length:8},(_,index)=>day(dayjs.utc('2026-10-30').add(index,'day').format('YYYY-MM-DD'),'1'));
    const calendar=activityCalendar(days);
    expect(calendar.cells[0]).toMatchObject({column:2,row:7}); // Friday
    expect(calendar.cells[3]).toMatchObject({column:3,row:3}); // Monday, DST ended Nov 1
    expect(calendar.months).toEqual([{key:'2026-11',title:'11月',column:3}]);
    expect(calendar.weeks).toBe(2);
  });
  it('distinguishes unknown, true zero and exact 50/75/90 quantiles above Number precision',()=>{
    const values=[null,'0',...Array.from({length:10},(_,index)=>(9007199254740992n+BigInt(index)).toString())];
    const calendar=activityCalendar(values.map((tokens,index)=>day(dayjs.utc('2026-10-01').add(index,'day').format('YYYY-MM-DD'),tokens)));
    expect(calendar.cells.map(cell=>cell.intensity)).toEqual(['unknown','none',...Array(6).fill('level-1'),...Array(2).fill('level-2'),'level-3','level-4']);
    expect(activityCalendar([]).cells).toEqual([]);
  });
  it('exposes exact day values with a single keyboard entry and moves by weeks',async()=>{
    const days=Array.from({length:10},(_,index)=>day(dayjs.utc('2026-10-01').add(index,'day').format('YYYY-MM-DD'),index===0?null:index===1?'0':'9007199254740993'));
    render(<ActivityHeatmap days={days} />);
    expect(screen.getByRole('button',{name:'2026-10-01 · 已收到 Token：未知'})).toHaveClass('unknown');
    expect(screen.getByRole('button',{name:'2026-10-02 · 已收到 Token：0'})).toHaveClass('none');
    const buttons=screen.getAllByRole('button');
    expect(buttons.filter(button=>button.tabIndex===0)).toHaveLength(1);
    const user=userEvent.setup();await user.tab();
    expect(buttons[9]).toHaveFocus();
    await user.keyboard('{ArrowLeft}');expect(buttons[2]).toHaveFocus();
    expect(buttons[2]).toHaveAccessibleName('2026-10-03 · 已收到 Token：90071992.5亿');
    await user.click(buttons[2]);
    expect(await screen.findByText('2026-10-03 · 已收到 Token：90071992.5亿')).toBeInTheDocument();
    await user.keyboard('{Home}');expect(buttons[0]).toHaveFocus();
    await user.keyboard('{End}');expect(buttons[9]).toHaveFocus();
  });
});
