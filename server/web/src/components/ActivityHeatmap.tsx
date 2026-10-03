import { useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { Tooltip } from 'antd';
import type { Day } from '../api/statistics';
import { dayjs, tokens } from '../format';

// 参考掘金正值的 50/75/90 分位色阶；只决定颜色，日值与年度指标仍由 Server 提供。
export function activityCalendar(days: Day[]) {
  if (!days.length) return { cells: [], months: [], weeks: 0 };
  const positives = days.flatMap(day => day.totals.total_tokens !== null && BigInt(day.totals.total_tokens) > 0n ? [BigInt(day.totals.total_tokens)] : []).sort((a,b) => a < b ? -1 : a > b ? 1 : 0);
  const thresholds = [50, 75, 90].map(percent => {
    if (!positives.length) return 0n;
    const position = (positives.length - 1) * percent;
    const index = Math.floor(position / 100);
    const left = positives[index];
    const right = positives[Math.min(index + 1, positives.length - 1)];
    return left + ((right - left) * BigInt(position % 100) + 50n) / 100n;
  });
  const first = dayjs.utc(days[0].date);
  const leading = first.day();
  const cells = days.map(day => {
    const offset = dayjs.utc(day.date).diff(first, 'day');
    const value = day.totals.total_tokens === null ? null : BigInt(day.totals.total_tokens);
    const intensity = value === null ? 'unknown' : value === 0n ? 'none' : `level-${1 + thresholds.filter(threshold => value > threshold).length}`;
    return { day, column: Math.floor((leading + offset) / 7) + 2, row: (leading + offset) % 7 + 2, intensity };
  });
  const weeks = cells.at(-1)!.column - 1;
  const months = cells.filter(cell => cell.day.date.endsWith('-01')).map(cell => ({ key: cell.day.date.slice(0,7), title: `${Number(cell.day.date.slice(5,7))}月`, column: cell.column }));
  if (!days[0].date.endsWith('-01') && (!months.length || months[0].column >= 4)) months.unshift({key:days[0].date.slice(0,7),title:`${Number(days[0].date.slice(5,7))}月`,column:2});
  return { cells, months, weeks };
}

export function ActivityHeatmap({ days }: { days: Day[] }) {
  const calendar = useMemo(() => activityCalendar(days), [days]);
  const [selected, setSelected] = useState<string | null>(null);
  const scroll = useRef<HTMLDivElement>(null);
  const buttons = useRef(new Map<string, HTMLButtonElement>());
  const lastDate = days.at(-1)?.date;
  useEffect(() => {
    const element = scroll.current;
    if (element) element.scrollLeft = element.scrollWidth - element.clientWidth;
  }, [lastDate]);
  const focused = selected && buttons.current.has(selected) ? selected : lastDate;
  function move(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const offsets: Record<string,number> = { ArrowLeft: -7, ArrowRight: 7, ArrowUp: -1, ArrowDown: 1, Home: -index, End: days.length - 1 - index };
    if (!(event.key in offsets)) return;
    event.preventDefault();
    const next = days[Math.max(0, Math.min(days.length - 1, index + offsets[event.key]))];
    buttons.current.get(next.date)?.focus();
  }
  return <div className="activity-heatmap">
    <div className="activity-calendar-scroll" ref={scroll}>
      <div className="activity-calendar" role="group" aria-label="过去一年已收到的每日 Token 热力图，方向键选择日期" style={{ '--activity-weeks': calendar.weeks } as CSSProperties}>
        {calendar.months.map(month => <span className="activity-month" key={month.key} style={{gridColumn:month.column,gridRow:1,justifySelf:month.column===calendar.weeks+1?'end':undefined}}>{month.title}</span>)}
        {['日','一','二','三','四','五','六'].map((label,row) => <span className="activity-weekday" key={row} style={{gridColumn:1,gridRow:row+2}}>{label}</span>)}
        {calendar.cells.map(({day,column,row,intensity},index) => {
          const detail = `${day.date} · 已收到 Token：${tokens(day.totals.total_tokens)}`;
          return <Tooltip key={day.date} title={detail} trigger={['hover','focus']} styles={{root:{maxWidth:'calc(100vw - 24px)'}}} destroyOnHidden>
            <button type="button" className={`activity-cell ${intensity}`} style={{gridColumn:column,gridRow:row}} aria-label={detail} tabIndex={focused===day.date?0:-1}
              ref={element=>{if(element) buttons.current.set(day.date,element);else buttons.current.delete(day.date);}}
              onFocus={()=>setSelected(day.date)} onKeyDown={event=>move(event,index)} />
          </Tooltip>;
        })}
      </div>
    </div>
    <div className="activity-legend" aria-label="Token 活跃度由少到多，灰色表示未知">
      <span className="activity-cell unknown" />未知<span className="legend-gap" />少
      {['none','level-1','level-2','level-3','level-4'].map(level => <span key={level} className={`activity-cell ${level}`} />)}多
    </div>
  </div>;
}
