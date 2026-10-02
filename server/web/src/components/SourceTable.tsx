import { Badge, Button, Table, Tag, Tooltip } from 'antd';
import {LaptopOutlined} from '@ant-design/icons';
import type { Device } from '../api/statistics';
import { dateTime, integer, providerNames } from '../format';

const sourceStates: Record<string,string> = { ready:'采集就绪',paused:'已暂停',disabled:'已关闭',reconnect_required:'需重新接入',failed:'采集失败',unknown:'状态未知' };

export function SourceTable({ devices, compact=false, zone='Asia/Shanghai',selectedId,onSelect }: { devices:Device[]; compact?:boolean; zone?:string;selectedId?:string;onSelect?(id:string):void }) {
  if(compact)return <div className="source-mini">{devices.slice(0,3).map(d=><div key={d.id}><div><LaptopOutlined /><strong>{d.name}</strong>{d.revoked_at_ms!==null&&<Tag>已撤销</Tag>}</div><Tooltip trigger={['hover','focus']} title={d.providers.map(p=><div key={p.provider}>{providerNames[p.provider]??p.provider} · 原采集 {dateTime(p.collected_at_ms,zone)}</div>)}><span tabIndex={0}><Badge status={!d.providers.length?'default':d.providers.some(p=>p.stale)?'warning':'success'} text={!d.providers.length?'暂无来源观测':d.providers.every(p=>p.stale)?'采集证据陈旧':d.providers.some(p=>p.stale)?'含陈旧证据':'近期采集证据'} /></span></Tooltip><span className="metric-note">中心接收 {dateTime(d.last_received_at_ms,zone)} · {d.providers.map(p=>providerNames[p.provider]??p.provider).join('、')||'尚无平台观测'}</span></div>)}</div>;
  return <Table<Device> rowKey="id" size="small" dataSource={compact?devices.slice(0,3):devices} rowClassName={d=>d.id===selectedId?'selected-record':''} pagination={compact?false:{pageSize:10,showSizeChanger:false}} scroll={{x:compact?480:860}} columns={[
    {title:'设备',width:compact?130:180,render:(_,d)=><>{onSelect?<Button type="link" className="record-link" onClick={()=>onSelect(d.id)} aria-pressed={d.id===selectedId}>{d.name}</Button>:<strong>{d.name}</strong>}{d.revoked_at_ms!==null&&<div><Tag>已撤销</Tag></div>}{!compact&&<div className="record-id">{d.id}</div>}</>},
    {title:'采集证据',render:(_,d)=>d.providers.length?compact?<Tooltip trigger={['hover','focus']} title={d.providers.map(p=><div key={p.provider}>{providerNames[p.provider]??p.provider} · {p.stale?'陈旧':'近期证据'} · 原采集 {dateTime(p.collected_at_ms,zone)}</div>)}><span tabIndex={0} className={d.providers.some(p=>p.stale)?'source-stale':'source-recent'}>{d.providers.every(p=>p.stale)?'采集证据陈旧':d.providers.some(p=>p.stale)?'含陈旧证据':'近期采集证据'}</span></Tooltip>:d.providers.map(p=><div className="source-line" key={p.provider}><span>{providerNames[p.provider]??p.provider}</span><span className={p.stale?'source-stale':'source-recent'}>{p.stale?'陈旧':'近期证据'}</span><span>{sourceStates[p.status]??'状态未知'}</span></div>):<span className="metric-note">暂无来源观测</span>},
    {title:compact?'中心接收':'原采集时间',width:compact?174:200,render:(_,d)=>compact?dateTime(d.last_received_at_ms,zone):d.providers.length?d.providers.map(p=><div className="source-line" key={p.provider}>{dateTime(p.collected_at_ms,zone)}</div>):'未知'},
    ...(!compact?[
      {title:'中心接收',width:190,render:(_:unknown,d:Device)=>dateTime(d.last_received_at_ms,zone)},
      {title:'版本 / 待传批次',width:160,render:(_:unknown,d:Device)=>d.providers.map(p=><div className="source-line" key={p.provider}>{p.version||'版本未知'} · {integer(p.pending_batches)}</div>)},
    ]:[]),
  ]} expandable={compact?undefined:{expandedRowRender:d=><div className="source-coverage">{d.providers.map(p=><div key={p.provider}><strong>{providerNames[p.provider]??p.provider}</strong> · 上报覆盖 {dateTime(p.coverage_start_ms,zone)} → {dateTime(p.coverage_end_ms,zone)} · 原采集 {dateTime(p.collected_at_ms,zone)} · 接收 {dateTime(p.received_at_ms,zone)}</div>)}</div>}} />;
}
