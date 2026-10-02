import { useState } from 'react';
import { Button, Input, Select } from 'antd';
import type { ListFilter } from '../api/records';
import { initialFilter, StatsFilters } from './StatsFilters';

export function initialListFilter():ListFilter{return {...initialFilter(),search:'',model:'',sort:'activity',direction:'desc',page:1,limit:25};}
export function sameRecordScope(a:ListFilter,b:ListFilter):boolean {
 return a.search===b.search&&a.model===b.model&&a.provider===b.provider&&a.client_id===b.client_id&&a.start_date===b.start_date&&a.end_date_exclusive===b.end_date_exclusive&&a.time_zone===b.time_zone;
}
export function ListFilters({value,onChange,refresh,busy}:{value:ListFilter;onChange(v:ListFilter):void;refresh():void;busy:boolean}){
 const [search,setSearch]=useState(value.search),[model,setModel]=useState(value.model);
 const change=(v:ListFilter)=>onChange({...v,page:1});
 function apply(){change({...value,search:search.trim(),model:model.trim()});}
 return <><StatsFilters value={value} onChange={v=>change({...value,...v})} refresh={refresh} busy={busy} /><form className="list-filters" onSubmit={event=>{event.preventDefault();apply();}}><Input aria-label="搜索标题、Session ID 或项目名" placeholder="搜索标题、Session ID 或项目名" maxLength={256} value={search} onChange={event=>setSearch(event.target.value)} /><Input aria-label="精确模型名称" placeholder="精确模型名称（可选）" maxLength={256} value={model} onChange={event=>setModel(event.target.value)} /><Select aria-label="排序依据" value={value.sort} onChange={sort=>change({...value,sort})} options={[{value:'activity',label:'最近活动'},{value:'name',label:'名称'},{value:'tokens',label:'Token'},{value:'cost',label:'API 等价成本'}]} /><Select aria-label="排序方向" value={value.direction} onChange={direction=>change({...value,direction})} options={[{value:'desc',label:'降序'},{value:'asc',label:'升序'}]} /><Button htmlType="submit">应用搜索</Button></form></>;
}
