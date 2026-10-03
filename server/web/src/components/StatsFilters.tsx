import { useState, type ReactNode } from 'react';
import { Button, DatePicker, Form, Popover, Segmented, Select, Space, Tag } from 'antd';
import { FilterOutlined, ReloadOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import type { StatsFilter } from '../api/statistics';
import { getDevices } from '../api/statistics';
import { dayjs, providerNames } from '../format';

export function initialFilter(days=30): StatsFilter {
  const today=dayjs().tz('Asia/Shanghai');
  return { start_date:today.subtract(days-1,'day').format('YYYY-MM-DD'),end_date_exclusive:today.add(1,'day').format('YYYY-MM-DD'),time_zone:'Asia/Shanghai',provider:'',client_id:'' };
}
export function StatsFilters({ value, onChange, refresh, busy, extra }: { value: StatsFilter; onChange(value: StatsFilter): void; refresh(): void; busy: boolean; extra?: ReactNode }) {
  const devices=useQuery({ queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal) });
  const today=dayjs().tz(value.time_zone);
  const [dateOpen,setDateOpen]=useState(false),[sourceOpen,setSourceOpen]=useState(false);
  const activeRange=[1,7,30,90].find(n=>value.start_date===today.subtract(n-1,'day').format('YYYY-MM-DD')&&value.end_date_exclusive===today.add(1,'day').format('YYYY-MM-DD'))??'custom';
  function range(n:number):StatsFilter { return {...value,start_date:today.subtract(n-1,'day').format('YYYY-MM-DD'),end_date_exclusive:today.add(1,'day').format('YYYY-MM-DD')}; }
  const sourceName=devices.data?.find(d=>d.id===value.client_id)?.name??value.client_id;
  return <div className="stats-toolbar-wrap"><div className="stats-toolbar">
    <Space wrap size={8}><Select className="provider-select" aria-label="Provider" value={value.provider} onChange={provider=>onChange({...value,provider})} options={[{value:'',label:'全部平台'},{value:'codex',label:'Codex'},{value:'cursor',label:'Cursor'},{value:'grok',label:'Grok'}]} />{extra}</Space>
    <Space wrap size={8}>
      <Segmented aria-label="快捷统计范围" value={activeRange} options={[{value:1,label:'今天'},{value:7,label:'7天'},{value:30,label:'30天'},{value:90,label:'90天'}]} onChange={n=>onChange(range(Number(n)))} />
      <Popover trigger="click" open={dateOpen} onOpenChange={setDateOpen} title="自定义日期与报告时区" content={<div className="toolbar-popover"><Form layout="vertical"><Form.Item label="统计日期"><DatePicker.RangePicker classNames={{popup:{root:'stats-date-popup'}}} aria-label="统计日期" allowClear={false} value={[dayjs(value.start_date),dayjs(value.end_date_exclusive).subtract(1,'day')]} onChange={dates=>{if(dates?.[0]&&dates[1])onChange({...value,start_date:dates[0].format('YYYY-MM-DD'),end_date_exclusive:dates[1].add(1,'day').format('YYYY-MM-DD')});}} presets={[1,7,30,90,365].map(n=>({label:n===1?'今天':`最近 ${n} 天`,value:[today.subtract(n-1,'day'),today]}))} /></Form.Item><Form.Item label="报告时区"><Select aria-label="报告时区" value={value.time_zone} onChange={time_zone=>onChange({...value,time_zone})} options={['Asia/Shanghai','UTC','Asia/Tokyo','America/New_York','Europe/Berlin'].map(zone=>({value:zone,label:zone}))} /></Form.Item><Button type="primary" onClick={()=>setDateOpen(false)}>完成</Button></Form></div>}><Button type={activeRange==='custom'?'primary':'default'}>自定义</Button></Popover>
      <Popover trigger="click" title="采集来源" open={sourceOpen} onOpenChange={setSourceOpen} content={<div className="toolbar-popover"><Select aria-label="采集来源" style={{width:'100%'}} loading={devices.isPending} value={value.client_id} showSearch={{optionFilterProp:'label'}} onChange={client_id=>onChange({...value,client_id})} options={[{value:'',label:'全部来源'},...(devices.data??[]).map(d=>({value:d.id,label:d.name+(d.revoked_at_ms?'（已撤销）':'')}))]} /><p className="metric-note">筛选采集事实，不表示执行归属。</p><Button size="small" onClick={()=>{onChange({...value,client_id:''});setSourceOpen(false);}}>清除来源筛选</Button></div>}><Button aria-label="筛选" icon={<FilterOutlined />}>筛选{value.client_id?' · 1':''}</Button></Popover>
      <Button icon={<ReloadOutlined />} aria-label="刷新" aria-busy={busy} onClick={()=>{void devices.refetch();refresh();}} loading={busy} />
    </Space>
  </div>
    {(value.provider||value.client_id||value.model||value.search||activeRange==='custom')&&<div className="active-filters"><span>已筛选</span>{value.provider&&<Tag closable onClose={()=>onChange({...value,provider:''})}>{providerNames[value.provider]??value.provider}</Tag>}{value.client_id&&<Tag closable onClose={()=>onChange({...value,client_id:''})}>{sourceName}</Tag>}{value.model&&<Tag closable onClose={()=>onChange({...value,model:''})}>模型：{value.model}</Tag>}{value.search&&<Tag closable onClose={()=>onChange({...value,search:''})}>搜索：{value.search}</Tag>}{activeRange==='custom'&&<Tag closable onClose={()=>onChange(range(30))}>{value.start_date} — {dayjs(value.end_date_exclusive).subtract(1,'day').format('YYYY-MM-DD')}</Tag>}<Button type="link" size="small" onClick={()=>onChange({...range(30),provider:'',client_id:'',model:'',search:''})}>清除全部</Button></div>}
    {devices.error&&<div role="status" className="filter-error">设备选项读取失败；可刷新重试，当前筛选未改变。</div>}
  </div>;
}
