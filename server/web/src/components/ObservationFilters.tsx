import {useState,type ReactNode} from 'react';
import {Button,Popover,Select,Space,Tag} from 'antd';
import {FilterOutlined,ReloadOutlined} from '@ant-design/icons';
import {useFeedbackQuery as useQuery} from './QueryNotifications';
import {getDevices} from '../api/statistics';
import {providerNames} from '../format';
export interface ObservationScope {provider:string;client_id:string}
export function ObservationFilters({value,onChange,refresh,busy,extra,providerLabel='平台筛选',sourceLabel='采集来源筛选',refreshLabel='刷新来源'}:{value:ObservationScope;onChange(v:ObservationScope):void;refresh():void;busy:boolean;extra?:ReactNode;providerLabel?:string;sourceLabel?:string;refreshLabel?:string}){
 const devices=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
 const [open,setOpen]=useState(false);
 return <div className="stats-toolbar-wrap"><div className="stats-toolbar"><Space wrap size={8}><Select className="provider-select" aria-label={providerLabel} value={value.provider} onChange={provider=>onChange({...value,provider})} options={[{value:'',label:'全部平台'},...Object.entries(providerNames).map(([value,label])=>({value,label}))]} />{extra}</Space><Space size={8}><Popover open={open} onOpenChange={setOpen} trigger="click" title="采集来源" content={<div className="toolbar-popover"><Select style={{width:'100%'}} aria-label={sourceLabel} loading={devices.isPending} value={value.client_id} showSearch={{optionFilterProp:'label'}} onChange={client_id=>onChange({...value,client_id})} options={[{value:'',label:'全部来源'},...(devices.data??[]).filter(d=>d.revoked_at_ms===null).map(d=>({value:d.id,label:d.name}))]} /><p className="metric-note">筛选采集事实，不表示执行归属。</p><Button size="small" onClick={()=>{onChange({...value,client_id:''});setOpen(false);}}>清除来源筛选</Button></div>}><Button aria-label="筛选" icon={<FilterOutlined />}>筛选{value.client_id?' · 1':''}</Button></Popover><Button icon={<ReloadOutlined />} aria-label={refreshLabel} loading={busy} onClick={()=>{void devices.refetch();refresh();}} /></Space></div>
 {(value.provider||value.client_id)&&<div className="active-filters"><span>已筛选</span>{value.provider&&<Tag closable onClose={()=>onChange({...value,provider:''})}>{providerNames[value.provider]??value.provider}</Tag>}{value.client_id&&<Tag closable onClose={()=>onChange({...value,client_id:''})}>{devices.data?.find(d=>d.id===value.client_id)?.name??value.client_id}</Tag>}<Button size="small" type="link" onClick={()=>onChange({provider:'',client_id:''})}>清除全部</Button></div>}

 </div>;
}
