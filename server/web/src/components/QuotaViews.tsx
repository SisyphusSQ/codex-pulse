import {decimalSorter} from './sorting';
import { lazy, Suspense, useMemo, useState } from 'react';
import { Collapse, Descriptions, Select, Statistic, Table, Tag, Typography, theme } from 'antd';
import type { EChartsCoreOption } from 'echarts/core';
import type { PacePoint, PaceWindow, QuotaAccount, QuotaCredits } from '../api/quotas';
import type { Device } from '../api/statistics';
import { dateTime, integer, providerNames } from '../format';
import { EvidenceIcon } from './EvidenceIcon';
import { LoadingState } from './QueryState';

export const quotaReasons:Record<string,string>={trusted:'显示最后一次有效更新',stale:'显示最后一次有效更新',expired:'显示最后一次有效更新',source_conflict:'采集来源证据冲突',suspicious:'证据可疑，保留最后可信值',unavailable:'暂无可信观测',binding_unavailable:'账号尚未确认关联',window_unavailable:'没有可信窗口',window_invalid:'窗口时间无效',evidence_stale:'观测陈旧，无法预测',evidence_sparse:'实际采样数量或跨度不足',evidence_flat:'近期采样没有可信增长趋势',evidence_invalid:'采样证据无效',evidence_budget:'采样超出预测预算'};
quotaReasons.expired_unknown=quotaReasons.expired;
quotaReasons.suspicious_candidate=quotaReasons.suspicious;
const freshness:Record<string,string>={fresh:'最后观测',stale:'最后观测',expired_unknown:'最后观测',never_loaded:'尚无有效观测',unknown:'未知',unassigned:'账号待关联',suspicious:'可疑'};
const Chart=lazy(()=>import('./Chart'));
export const percent=(n:number|null|undefined)=>n==null?'未知':`${new Intl.NumberFormat('zh-CN',{maximumFractionDigits:2}).format(n)}%`;
export const duration=(ms:number|null|undefined)=>{
 if(ms==null)return '未知';
 const seconds=Math.max(0,Math.floor(ms/1000)),days=Math.floor(seconds/86400),hours=Math.floor(seconds/3600)%24,minutes=Math.floor(seconds/60)%60;
 return `${days?`${days} 天 `:''}${hours} 小时 ${minutes} 分 ${seconds%60} 秒`;
};
export function QuotaStatus({state,conflict}:{state:string;conflict:boolean}){return <><Tag color={['fresh','stale','expired_unknown'].includes(state)&&!conflict?'blue':'gold'}>{freshness[state]??'状态未知'}</Tag>{conflict&&<Tag color="red">来源冲突</Tag>}</>;}
export function AccountIdentity({account,provider,showDetails=true}:{account?:QuotaAccount;provider:string;showDetails?:boolean}){
 return <div className="account-identity"><Tag>{providerNames[provider]??provider}</Tag><strong>{account?.email??(account?'邮箱未提供':'账号待关联')}</strong><div className="record-id">原始账号 ID：{account?.raw_id??'尚未确认'}</div>{showDetails&&<div className="metric-note">套餐：{account?.plan??'未知'}{account?` · 资料采集：${dateTime(account.collected_at_ms)}`:''}</div>}</div>;
}
export function paceChartOption(window:PaceWindow, color='#2678f5'):EChartsCoreOption{
 const points=(name:string,values:PacePoint[],color:string,width:number)=>({name,type:'line',showSymbol:values.length===1,symbolSize:5,smooth:false,lineStyle:{width,color,type:'solid'},itemStyle:{color},connectNulls:true,data:values.map(p=>({value:[p.elapsed_percent,p.remaining_percent],observedAt:p.observed_at_ms,linked:p.linked_history}))});
 return {animation:false,tooltip:{renderMode:'richText',confine:true,trigger:'axis',formatter:(params:unknown)=>{
 const ps=params as {seriesName:string;value:number[];data:{observedAt?:number;linked?:boolean;minimum?:number;maximum?:number}}[];
 return ps.map(p=>`${p.seriesName} · 进度 ${percent(p.value[0])} · 剩余 ${percent(p.value[1])}${p.data.observedAt!=null?`\n实际观测 ${dateTime(p.data.observedAt)}${p.data.linked?' · 关联历史':''}`:p.seriesName==='理想节奏'?' · 均匀消耗参照':`\n历史范围 ${percent(p.data.minimum)} — ${percent(p.data.maximum)}`}`).join('\n');
 }},legend:{type:'scroll',top:4,itemWidth:18,itemHeight:8,itemGap:16,textStyle:{color:'#617089',fontSize:12}},grid:{top:66,left:12,right:18,bottom:24,containLabel:true},xAxis:{type:'value',min:0,max:100,axisLabel:{formatter:'{value}%'},splitLine:{lineStyle:{color:'#edf1f6'}}},yAxis:{type:'value',min:0,max:100,name:'剩余额度',axisLabel:{formatter:'{value}%'},splitLine:{lineStyle:{color:'#edf1f6'}}},series:[
 points('观测周期',window.current_points,color,2.5),points('上一周期',window.previous_cycle?.points??[],'#7890b3',1.8),
 {name:'历史中位数',type:'line',showSymbol:false,itemStyle:{color:'#8a70cf'},lineStyle:{type:'dashed',width:1.7,color:'#8a70cf'},data:window.history_band.map(p=>({value:[p.elapsed_percent,p.median_remaining],minimum:p.minimum_remaining,maximum:p.maximum_remaining}))},
 {name:'理想节奏',type:'line',symbol:'none',lineStyle:{type:'dotted',width:1.3,color:'#a8b2c1'},itemStyle:{color:'#a8b2c1'},data:[[0,100],[100,0]]},
 ]};
}
export function PacePanel({window,evaluatedAt}:{window:PaceWindow;evaluatedAt:number}){
 const {token}=theme.useToken();
 const [cycleID,setCycleID]=useState('current');
 const option=useMemo(()=>paceChartOption(window,token.colorPrimary),[window,token.colorPrimary]);
 const previous=window.previous_cycle,snapshotAt=window.snapshot_at_ms??window.current.observed_at_ms;
 const histories=window.historical_cycles.filter(c=>c.id!==previous?.id);
 const cycleOptions=[{value:'current',label:'观测周期'},...(previous?[{value:previous.id,label:`上一周期 · ${dateTime(previous.resets_at_ms)}`}]:[]),...histories.map(c=>({value:c.id,label:`历史 · ${dateTime(c.resets_at_ms)}`}))];
 const visibleCycleID=cycleOptions.some(c=>c.value===cycleID)?cycleID:'current';
 const chosen=visibleCycleID==='current'?undefined:[...(previous?[previous]:[]),...histories].find(c=>c.id===visibleCycleID);
 const rows=visibleCycleID==='current'?window.current_points:chosen?.points??[];
 const f=window.forecast,unavailable=f.state==='unavailable';
 const states:Record<string,string>={unavailable:'暂不能推算',on_track:'预计可坚持到 reset',at_risk:'预计会在 reset 前耗尽',exhausted:'观测额度已耗尽'};
 const reason=quotaReasons[f.unknown_reason??window.unknown_reason??window.current.reason??'unavailable']??'暂缺预测证据';
 const hasSamples=window.current_points.length||previous?.points.length||window.history_band.length;
 return <section className="pace-section">
 <div className="section-heading"><div><strong>节奏评估</strong><EvidenceIcon warning={unavailable||f.state==='at_risk'} label="节奏评估说明"><p>{unavailable?reason:'根据最后一次有效更新时已收到的采样评估，随新观测更新。'}</p><p>实线连接相邻已知观测，仅作趋势参照；缺口不新增观测或预测证据，不延伸到最后采样之外。历史中位数与理想线不是当前采样。</p><div className="metric-note">数据截至：{dateTime(snapshotAt)} · 中心读取：{dateTime(evaluatedAt)} · 推算规则由中心返回</div></EvidenceIcon>{window.pace_delta_pp!=null&&<Tag color={window.pace_delta_pp>0?'gold':'green'}>{window.pace_delta_pp>0?'消耗快于均匀节奏':'消耗慢于均匀节奏'}</Tag>}</div><span className="metric-note">最后更新 {dateTime(snapshotAt)}</span></div>
 <div className="pace-metrics"><div><div className="metric-label">已使用</div><Statistic value={percent(window.current.used_percent)} /><div className="metric-note">最后观测的额度窗口</div></div><div><div className="metric-label">周期进度</div><Statistic value={percent(window.elapsed_percent)} /><div className="metric-note">截至最后更新时刻</div></div><div><div className="metric-label">节奏差</div><Statistic aria-label="节奏偏差（百分点）" value={window.pace_delta_pp??'未知'} formatter={()=>window.pace_delta_pp==null?'未知':`${window.pace_delta_pp.toFixed(2)} pp`} /><div className="metric-note">正值代表使用快于均匀节奏</div></div><div><div className="metric-label">历史基线</div><Statistic value={window.history_cycle_count?`${integer(window.history_cycle_count)}个周期`:'未知'} /><div className="metric-note">首尾覆盖的历史周期</div></div></div>
 {hasSamples?<Suspense fallback={<LoadingState label="正在加载节奏图…" />}><Chart option={option} label="观测周期、上一周期、历史中位数与理想节奏" height={300} /></Suspense>:<div className="quiet-empty">尚无可绘制的可信周期采样</div>}
 <div className="pace-axes"><span>横轴：周期进度</span><span>纵轴：剩余额度</span></div>
 <div className="pace-results"><section className={unavailable?'limited':f.state==='at_risk'||f.state==='exhausted'?'at-risk':'on-track'}><strong>最后更新时的耗尽推算</strong><span>{unavailable?states.unavailable:states[f.state]??states.unavailable}</span><small>{unavailable?'原因见节奏评估旁的说明图标':`预测实际证据 ${integer(f.evidence_count)} 条 · 跨度 ${duration(f.evidence_span_ms)}`}</small>{!unavailable&&f.exhaust_at_ms!=null&&<small>预测耗尽 {dateTime(f.exhaust_at_ms)} · 提前 {duration(f.lead_before_reset_ms)}</small>}</section><section><strong>最后更新时的同进度对比</strong><dl><dt>观测周期</dt><dd>{percent(window.current.remaining_percent)}</dd><dt>上一周期</dt><dd>{percent(window.previous_remaining_at_elapsed)}</dd><dt>历史中位数</dt><dd>{percent(window.history_median_remaining_at_elapsed)}</dd></dl></section></div>
 <Collapse ghost size="small" items={[{key:'samples',label:'实际采样明细与历史周期',children:<><Typography.Paragraph type="secondary">预测实际证据 {integer(f.evidence_count)} 条 · 跨度 {duration(f.evidence_span_ms)} · {f.method==='recent_theil_sen'?'近期稳健趋势':'尚未建立趋势'}。历史基线 {integer(window.history_cycle_count)} 个首尾覆盖周期，不能代表中间全程采集。</Typography.Paragraph><div className="association-bar"><Select aria-label="查看采样周期" value={visibleCycleID} onChange={setCycleID} options={cycleOptions} /><Typography.Text type="secondary">{chosen?`${dateTime(chosen.window_start_at_ms)} → ${dateTime(chosen.resets_at_ms)} · ${chosen.complete?'首尾覆盖':'首尾未覆盖'}`:'观测周期截至最后更新，展示保留的真实采样，密集曲线保留端点和极值'}</Typography.Text></div>
 <Table size="small" rowKey={r=>`${r.observed_at_ms}:${r.used_percent}:${r.linked_history}`} dataSource={rows} pagination={rows.length>10?{pageSize:10,showSizeChanger:false}:false} scroll={{x:600}} columns={[{title:'实际观测',...decimalSorter<{observed_at_ms:number}>(r=>r.observed_at_ms,true),render:(_,r)=>dateTime(r.observed_at_ms)},{title:'窗口进度',render:(_,r)=>percent(r.elapsed_percent)},{title:'已用 / 剩余',render:(_,r)=>`${percent(r.used_percent)} / ${percent(r.remaining_percent)}`},{title:'历史来源',render:(_,r)=>r.linked_history?'显式关联历史':'确认账号采样'}]} /></>}]} />
 </section>;
}
export function CreditsCard({credits,account,devices,showAccount=true}:{credits:QuotaCredits;account?:QuotaAccount;devices:Device[];showAccount?:boolean}){
 const statuses:Record<string,string>={complete:'详情完整',partial:'详情部分已知',unknown:'详情未知',unavailable:'详情未取得',failed:'详情读取失败'};
 return <section className="credits-section" aria-label="Reset Credits 库存">
 <div className="section-heading"><div><strong>Reset Credits</strong><EvidenceIcon label="Reset Credits 说明" warning={credits.conflict||credits.freshness==='suspicious'}><p>库存及到期时间对应最后一次有效更新。观测时可用数量由中心按该次观测的完整到期明细计算；到期与 reset 是不同事件，库存不按设备相加。</p></EvidenceIcon></div><QuotaStatus state={credits.freshness} conflict={credits.conflict} /></div>{showAccount&&<AccountIdentity account={account} provider={credits.provider} />}
 <div className="credit-overview"><div><div className="metric-label">最后观测库存</div><div className="metric-value">{integer(credits.observed_inventory)}</div><span className="metric-note">最后更新 {dateTime(credits.observed_at_ms)}</span></div><div><div className="metric-label">观测时可用</div><div className="metric-value credit-secondary">{integer(credits.snapshot_available_inventory)}</div><span className="metric-note">{credits.snapshot_available_inventory==null?'详情未完整确认':'按观测时的到期明细'}</span></div><div><div className="metric-label">观测时最近到期</div><div className="credit-date">{dateTime(credits.snapshot_next_expires_at_ms)}</div><Tag>{statuses[credits.details_status]??'详情未知'}</Tag></div></div>
 <Collapse ghost size="small" items={[{key:'evidence',label:'库存到期明细',children:<><Descriptions size="small" column={{xs:1,md:2}} items={[{key:'time',label:'原观测时间',children:dateTime(credits.observed_at_ms)},{key:'source',label:'采集来源',children:<>{devices.find(d=>d.id===credits.client_id)?.name??'来源设备'}<div className="record-id">{credits.client_id}</div></>},{key:'reset',label:'下一次 reset',children:credits.next_reset_at_ms==null?'未知':dateTime(credits.next_reset_at_ms)}]} /><Table size="small" rowKey={(r,index)=>`${r.expires_at_ms}:${index}`} dataSource={credits.expiry_schedule} pagination={credits.expiry_schedule.length>10?{pageSize:10,showSizeChanger:false}:false} columns={[{title:'到期时间',...decimalSorter<{expires_at_ms:number|null}>(r=>r.expires_at_ms,true),render:(_,r)=>r.expires_at_ms==null?'未提供到期时间':dateTime(r.expires_at_ms)},{title:'数量',align:'right',...decimalSorter<{count:string}>(r=>r.count),render:(_,r)=>integer(r.count)}]} /></>}]} />
 </section>;
}
