import { Button, DatePicker, Select } from 'antd';
import { useQuery } from '@tanstack/react-query';
import type { StatsFilter } from '../api/statistics';
import { getDevices } from '../api/statistics';
import { dayjs } from '../format';

export function initialFilter(): StatsFilter {
  const today=dayjs().tz('Asia/Shanghai');
  return { start_date:today.subtract(29,'day').format('YYYY-MM-DD'),end_date_exclusive:today.add(1,'day').format('YYYY-MM-DD'),time_zone:'Asia/Shanghai',provider:'',client_id:'' };
}
export function StatsFilters({ value, onChange, refresh, busy }: { value: StatsFilter; onChange(value: StatsFilter): void; refresh(): void; busy: boolean }) {
  const devices=useQuery({ queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal) });
  const today=dayjs().tz(value.time_zone);
  return <div className="stats-filters">
    <div className="filter-control range-control"><label>统计日期</label><DatePicker.RangePicker allowClear={false} value={[dayjs(value.start_date),dayjs(value.end_date_exclusive).subtract(1,'day')]} onChange={(dates)=>{ if(dates?.[0]&&dates[1]) onChange({...value,start_date:dates[0].format('YYYY-MM-DD'),end_date_exclusive:dates[1].add(1,'day').format('YYYY-MM-DD')}); }} presets={[7,30,90,365].map((n)=>({label:`最近 ${n} 天`,value:[today.subtract(n-1,'day'),today]}))} /></div>
    <div className="filter-control"><label>Provider</label><Select aria-label="Provider" value={value.provider} onChange={(provider)=>onChange({...value,provider})} options={[{value:'',label:'全部 Provider'},{value:'codex',label:'Codex'},{value:'cursor',label:'Cursor'},{value:'grok',label:'Grok'}]} /></div>
    <div className="filter-control"><label>采集来源</label><Select aria-label="采集来源" loading={devices.isPending} value={value.client_id} showSearch={{optionFilterProp:'label'}} onChange={(client_id)=>onChange({...value,client_id})} options={[{value:'',label:'全部来源'},...(devices.data??[]).map((d)=>({value:d.id,label:d.name+(d.revoked_at_ms?'（已撤销）':'')}))]} /></div>
    <div className="filter-control"><label>报告时区</label><Select aria-label="报告时区" value={value.time_zone} onChange={(time_zone)=>onChange({...value,time_zone})} options={['Asia/Shanghai','UTC','Asia/Tokyo','America/New_York','Europe/Berlin'].map((zone)=>({value:zone,label:zone}))} /></div>
    <Button aria-label="刷新" aria-busy={busy} onClick={()=>{void devices.refetch();refresh();}} loading={busy}>刷新</Button>
    {devices.error && <div role="status" className="filter-error">设备选项读取失败；可刷新重试，当前筛选未改变。</div>}
  </div>;
}
