import { lazy, Suspense, useMemo, useState } from 'react';
import { Button, Card, Collapse, Descriptions, Progress, Select, Statistic, Table, Tag, Typography, theme } from 'antd';
import type { EChartsCoreOption } from 'echarts/core';
import type { PacePoint, PaceWindow, QuotaAccount, QuotaCredits, QuotaWindow } from '../api/quotas';
import type { Device } from '../api/statistics';
import { dateTime, integer, providerNames } from '../format';
import { EvidenceIcon } from './EvidenceIcon';
import { LoadingState } from './QueryState';

export const quotaReasons:Record<string,string>={trusted:'可信观测',stale:'采集证据陈旧，保留最后可信值',expired:'reset 已过，等待新观测',source_conflict:'采集来源证据冲突',suspicious:'证据可疑，保留最后可信值',unavailable:'暂无可信观测',binding_unavailable:'账号尚未确认关联',window_unavailable:'没有可信窗口',window_invalid:'窗口时间无效',evidence_stale:'观测陈旧，无法预测',evidence_sparse:'实际采样数量或跨度不足',evidence_flat:'近期采样没有可信增长趋势',evidence_invalid:'采样证据无效',evidence_budget:'采样超出预测预算'};
quotaReasons.expired_unknown=quotaReasons.expired;
quotaReasons.suspicious_candidate=quotaReasons.suspicious;
const freshness:Record<string,string>={fresh:'近期可信',stale:'陈旧',expired_unknown:'reset 已过，当前未知',never_loaded:'尚未取得可信值',unknown:'未知',unassigned:'账号待关联',suspicious:'可疑'};
const disposition:Record<string,string>={selected:'当前选择',eligible:'可用证据',superseded:'历史证据',suspicious:'可疑',rejected:'已拒绝'};
const historyOrigin:Record<string,string>={confirmed:'确认账号历史',linked_history:'显式关联历史',legacy_unassigned:'旧版待关联历史',unassigned:'待关联'};
const Chart=lazy(()=>import('./Chart'));
export const percent=(n:number|null|undefined)=>n==null?'未知':`${new Intl.NumberFormat('zh-CN',{maximumFractionDigits:2}).format(n)}%`;
export const duration=(ms:number|null|undefined)=>{
 if(ms==null)return '未知';
 const seconds=Math.max(0,Math.floor(ms/1000)),days=Math.floor(seconds/86400),hours=Math.floor(seconds/3600)%24,minutes=Math.floor(seconds/60)%60;
 return `${days?`${days} 天 `:''}${hours} 小时 ${minutes} 分 ${seconds%60} 秒`;
};
export function QuotaStatus({state,conflict}:{state:string;conflict:boolean}){return <><Tag color={state==='fresh'&&!conflict?'green':'gold'}>{freshness[state]??'状态未知'}</Tag>{conflict&&<Tag color="red">来源冲突</Tag>}</>;}
export function AccountIdentity({account,provider,showDetails=true}:{account?:QuotaAccount;provider:string;showDetails?:boolean}){
 return <div className="account-identity"><Tag>{providerNames[provider]??provider}</Tag><strong>{account?.email??(account?'邮箱未提供':'账号待关联')}</strong><div className="record-id">原始账号 ID：{account?.raw_id??'尚未确认'}</div>{showDetails&&<div className="metric-note">套餐：{account?.plan??'未知'}{account?` · 资料采集：${dateTime(account.collected_at_ms)}`:''}</div>}</div>;
}
export function QuotaWindowCard({window,account,devices,onSelect,showAccount=true}:{window:QuotaWindow;account?:QuotaAccount;devices:Device[];onSelect():void;showAccount?:boolean}){
 const c=window.current;
 const {token}=theme.useToken();
 const source=devices.find(d=>d.id===c.selected_client_id)?.name??window.observations.find(o=>o.client_id===c.selected_client_id)?.client_name??'暂无可信采集来源';
 return <Card title={`${window.limit_id} · ${window.window_kind==='primary'?'主窗口':window.window_kind==='secondary'?'次窗口':window.window_kind}`} extra={<QuotaStatus state={c.freshness} conflict={c.conflict} />} className="quota-card">
 {showAccount&&<AccountIdentity account={account} provider={window.provider} />}
 <div className="quota-numbers"><div><div className="metric-label">已用</div><strong>{percent(c.used_percent)}</strong>{c.used_percent!=null&&<Progress percent={c.used_percent} showInfo={false} strokeColor={c.freshness==='fresh'&&!c.conflict?token.colorPrimary:token.colorWarning} />}</div><div><div className="metric-label">剩余</div><strong>{percent(c.remaining_percent)}</strong></div></div>
 <Typography.Paragraph type="secondary">{quotaReasons[c.reason]??'当前状态由中心确认'} · 窗口 {window.window_minutes==null?'未知':`${integer(window.window_minutes)} 分钟`}</Typography.Paragraph>
 <Descriptions size="small" column={{xs:1,sm:2}} items={[
 {key:'reset',label:'实际 reset',children:dateTime(c.resets_at_ms)},
 {key:'remaining',label:'中心确认时剩余',children:c.reset_remaining_ms!=null?duration(c.reset_remaining_ms):'未提供可信倒计时'},
 {key:'observed',label:'原观测时间',children:dateTime(c.observed_at_ms)},
 ]} />
 <Collapse ghost size="small" items={[{key:'source',label:'窗口与采集来源',children:<Descriptions size="small" column={1} items={[
 {key:'start',label:'实际窗口开始',children:dateTime(c.window_start_at_ms)},
 {key:'source',label:'采集来源',children:`${source} · ${c.source??'来源未知'}`},
 ]} />}]} />
 <Button type="link" className="record-link" onClick={onSelect} aria-label={`查看节奏 ${window.limit_id} ${window.key}`}>查看节奏和观测证据</Button>
 </Card>;
}

function trustedPace(window:PaceWindow){return window.current.freshness==='fresh'&&!window.current.conflict;}
export function paceChartOption(window:PaceWindow, color='#2678f5'):EChartsCoreOption{
 const points=(name:string,values:PacePoint[],color:string,width:number)=>({name,type:'line',showSymbol:false,smooth:false,lineStyle:{width,color,type:'solid'},itemStyle:{color},connectNulls:true,data:values.map(p=>({value:[p.elapsed_percent,p.remaining_percent],observedAt:p.observed_at_ms,linked:p.linked_history}))});
 return {animation:false,tooltip:{renderMode:'richText',confine:true,trigger:'axis',formatter:(params:unknown)=>{
 const ps=params as {seriesName:string;value:number[];data:{observedAt?:number;linked?:boolean;minimum?:number;maximum?:number}}[];
 return ps.map(p=>`${p.seriesName} · 进度 ${percent(p.value[0])} · 剩余 ${percent(p.value[1])}${p.data.observedAt!=null?`\n实际观测 ${dateTime(p.data.observedAt)}${p.data.linked?' · 关联历史':''}`:p.seriesName==='理想节奏'?' · 均匀消耗参照':`\n历史范围 ${percent(p.data.minimum)} — ${percent(p.data.maximum)}`}`).join('\n');
 }},legend:{type:'scroll',top:4,itemWidth:18,itemHeight:8,itemGap:16,textStyle:{color:'#617089',fontSize:12}},grid:{top:66,left:12,right:18,bottom:24,containLabel:true},xAxis:{type:'value',min:0,max:100,axisLabel:{formatter:'{value}%'},splitLine:{lineStyle:{color:'#edf1f6'}}},yAxis:{type:'value',min:0,max:100,name:'剩余额度',axisLabel:{formatter:'{value}%'},splitLine:{lineStyle:{color:'#edf1f6'}}},series:[
 points('本周期',trustedPace(window)?window.current_points:[],color,2.5),points('上一周期',window.previous_cycle?.points??[],'#7890b3',1.8),
 {name:'历史中位数',type:'line',showSymbol:false,itemStyle:{color:'#8a70cf'},lineStyle:{type:'dashed',width:1.7,color:'#8a70cf'},data:window.history_band.map(p=>({value:[p.elapsed_percent,p.median_remaining],minimum:p.minimum_remaining,maximum:p.maximum_remaining}))},
 {name:'理想节奏',type:'line',symbol:'none',lineStyle:{type:'dotted',width:1.3,color:'#a8b2c1'},itemStyle:{color:'#a8b2c1'},data:[[0,100],[100,0]]},
 ]};
}
export function PacePanel({window,evaluatedAt}:{window:PaceWindow;evaluatedAt:number}){
 const {token}=theme.useToken();
 const [cycleID,setCycleID]=useState('current');
 const option=useMemo(()=>paceChartOption(window,token.colorPrimary),[window,token.colorPrimary]);
 const trusted=trustedPace(window),previous=window.previous_cycle;
 const histories=window.historical_cycles.filter(c=>c.id!==previous?.id);
 const cycleOptions=[{value:'current',label:'本周期'},...(previous?[{value:previous.id,label:`上一周期 · ${dateTime(previous.resets_at_ms)}`}]:[]),...histories.map(c=>({value:c.id,label:`历史 · ${dateTime(c.resets_at_ms)}`}))];
 const visibleCycleID=cycleOptions.some(c=>c.value===cycleID)?cycleID:'current';
 const chosen=visibleCycleID==='current'?undefined:[...(previous?[previous]:[]),...histories].find(c=>c.id===visibleCycleID);
 const rows=visibleCycleID==='current'?window.current_points:chosen?.points??[];
 const f=window.forecast,unavailable=!trusted||f.state==='unavailable';
 const states:Record<string,string>={unavailable:'暂不能推算',on_track:'预计可坚持到 reset',at_risk:'预计会在 reset 前耗尽',exhausted:'观测额度已耗尽'};
 const reason=quotaReasons[f.unknown_reason??window.unknown_reason??window.current.reason??'unavailable']??'暂缺预测证据';
 const hasSamples=(trusted&&window.current_points.length)||previous?.points.length||window.history_band.length;
 return <section className="pace-section">
 <div className="section-heading"><div><strong>节奏评估</strong><EvidenceIcon warning={unavailable||f.state==='at_risk'} label="节奏评估说明"><p>{unavailable?reason:'根据中心已收到的可信采样评估，随新观测更新。'}</p><p>实线连接相邻已知观测，仅作趋势参照；缺口不新增观测或预测证据，不延伸到最后采样之外。历史中位数与理想线不是当前采样。</p><div className="metric-note">评估时间：{dateTime(evaluatedAt)} · 预测规则由中心返回</div></EvidenceIcon>{trusted&&window.pace_delta_pp!=null&&<Tag color={window.pace_delta_pp>0?'gold':'green'}>{window.pace_delta_pp>0?'消耗快于均匀节奏':'消耗慢于均匀节奏'}</Tag>}</div><span className="metric-note">{dateTime(evaluatedAt)} 评估</span></div>
 <div className="pace-metrics"><div><div className="metric-label">已使用</div><Statistic value={percent(trusted?window.current.used_percent:null)} /><div className="metric-note">选中额度窗口</div></div><div><div className="metric-label">周期进度</div><Statistic value={percent(window.elapsed_percent)} /><div className="metric-note">按实际窗口时间</div></div><div><div className="metric-label">节奏差</div><Statistic aria-label="节奏偏差（百分点）" value={trusted?window.pace_delta_pp??'未知':'未知'} formatter={()=>!trusted||window.pace_delta_pp==null?'未知':`${window.pace_delta_pp.toFixed(2)} pp`} /><div className="metric-note">正值代表使用快于均匀节奏</div></div><div><div className="metric-label">历史基线</div><Statistic value={window.history_cycle_count?`${integer(window.history_cycle_count)}个周期`:'未知'} /><div className="metric-note">首尾覆盖的历史周期</div></div></div>
 {hasSamples?<Suspense fallback={<LoadingState label="正在加载节奏图…" />}><Chart option={option} label="本周期、上一周期、历史中位数与理想节奏" height={300} /></Suspense>:<div className="quiet-empty">尚无可绘制的可信周期采样</div>}
 <div className="pace-axes"><span>横轴：周期进度</span><span>纵轴：剩余额度</span></div>
 <div className="pace-results"><section className={unavailable?'limited':f.state==='at_risk'||f.state==='exhausted'?'at-risk':'on-track'}><strong>保守耗尽推算</strong><span>{unavailable?states.unavailable:states[f.state]??states.unavailable}</span><small>{unavailable?'原因见节奏评估旁的说明图标':`预测实际证据 ${integer(f.evidence_count)} 条 · 跨度 ${duration(f.evidence_span_ms)}`}</small>{!unavailable&&f.exhaust_at_ms!=null&&<small>预测耗尽 {dateTime(f.exhaust_at_ms)} · 提前 {duration(f.lead_before_reset_ms)}</small>}</section><section><strong>同进度对比 · 剩余额度</strong><dl><dt>本周期</dt><dd>{percent(trusted?window.current.remaining_percent:null)}</dd><dt>上一周期</dt><dd>{percent(window.previous_remaining_at_elapsed)}</dd><dt>历史中位数</dt><dd>{percent(window.history_median_remaining_at_elapsed)}</dd></dl></section></div>
 <Collapse ghost size="small" items={[{key:'samples',label:'实际采样明细与历史周期',children:<><Typography.Paragraph type="secondary">预测实际证据 {integer(f.evidence_count)} 条 · 跨度 {duration(f.evidence_span_ms)} · {f.method==='recent_theil_sen'?'近期稳健趋势':'尚未建立趋势'}。历史基线 {integer(window.history_cycle_count)} 个首尾覆盖周期，不能代表中间全程采集。</Typography.Paragraph><div className="association-bar"><Select aria-label="查看采样周期" value={visibleCycleID} onChange={setCycleID} options={cycleOptions} /><Typography.Text type="secondary">{chosen?`${dateTime(chosen.window_start_at_ms)} → ${dateTime(chosen.resets_at_ms)} · ${chosen.complete?'首尾覆盖':'首尾未覆盖'}`:'本周期仅展示实际采样，不延伸到现在'}</Typography.Text></div>
 <Table size="small" rowKey={r=>`${r.observed_at_ms}:${r.used_percent}:${r.linked_history}`} dataSource={rows} pagination={rows.length>10?{pageSize:10,showSizeChanger:false}:false} scroll={{x:600}} columns={[{title:'实际观测',render:(_,r)=>dateTime(r.observed_at_ms)},{title:'窗口进度',render:(_,r)=>percent(r.elapsed_percent)},{title:'已用 / 剩余',render:(_,r)=>`${percent(r.used_percent)} / ${percent(r.remaining_percent)}`},{title:'历史来源',render:(_,r)=>r.linked_history?'显式关联历史':'确认账号采样'}]} /></>}]} />
 </section>;
}
export function QuotaEvidence({window}:{window:QuotaWindow}){
 return <Card title="采集来源与周期证据" className="section-card"><Typography.Paragraph type="secondary">共 {integer(window.observations.length)} 条原始观测统计、{integer(window.cycles.length)} 个已观测周期。相同额度不按机器相加；接收时间和原采集时间分别保留，关联历史不刷新当前值。</Typography.Paragraph>
 <Table size="small" rowKey="id" dataSource={window.observations} pagination={{pageSize:10,showSizeChanger:false}} scroll={{x:1180}} columns={[{title:'采集设备',dataIndex:'client_name'},{title:'原观测时间',render:(_,r)=>dateTime(r.observed_at_ms)},{title:'中心接收',render:(_,r)=>dateTime(r.received_at_ms)},{title:'已用',render:(_,r)=>percent(r.used_percent)},{title:'实际 reset',render:(_,r)=>dateTime(r.resets_at_ms)},{title:'规范 reset',render:(_,r)=>dateTime(r.canonical_reset_at_ms)},{title:'来源',dataIndex:'source'},{title:'历史归属',render:(_,r)=>historyOrigin[r.history_origin]??'已记录历史'},{title:'仲裁',render:(_,r)=>`${disposition[r.disposition]??'未选中'}${r.reason?` · ${quotaReasons[r.reason]??'证据不满足当前规则'}`:''}`}]} />
 </Card>;
}
export function CreditsCard({credits,account,devices,showAccount=true}:{credits:QuotaCredits;account?:QuotaAccount;devices:Device[];showAccount?:boolean}){
 const statuses:Record<string,string>={complete:'详情完整',partial:'详情部分已知',unknown:'详情未知',unavailable:'详情未取得',failed:'详情读取失败'};
 return <section className="credits-section" aria-label="Reset Credits 库存">
 <div className="section-heading"><div><strong>Reset Credits</strong><EvidenceIcon label="Reset Credits 说明" warning={credits.available_inventory===null}><p>到期与 reset 是不同事件。库存不按设备相加；只有新鲜、无冲突且详情完整时，中心提供扣除已知到期后的可用数量。</p></EvidenceIcon></div><QuotaStatus state={credits.freshness} conflict={credits.conflict} /></div>{showAccount&&<AccountIdentity account={account} provider={credits.provider} />}
 <div className="credit-overview"><div><div className="metric-label">当前确认可用</div><div className="metric-value">{integer(credits.available_inventory)}</div><span className="metric-note">{credits.available_inventory===null?'等待可信库存观测':'扣除已知到期后的库存'}</span></div><div><div className="metric-label">最后观测库存</div><div className="metric-value credit-secondary">{integer(credits.observed_inventory)}</div><span className="metric-note">{dateTime(credits.observed_at_ms)}</span></div><div><div className="metric-label">下一次到期</div><div className="credit-date">{dateTime(credits.next_expires_at_ms)}</div><Tag>{statuses[credits.details_status]??'详情未知'}</Tag></div></div>
 <Collapse ghost size="small" items={[{key:'evidence',label:'库存来源与到期明细',children:<><Descriptions size="small" column={{xs:1,md:2}} items={[{key:'time',label:'原观测时间',children:dateTime(credits.observed_at_ms)},{key:'source',label:'采集来源',children:<>{devices.find(d=>d.id===credits.client_id)?.name??'来源设备'}<div className="record-id">{credits.client_id}</div></>},{key:'reset',label:'下一次 reset',children:dateTime(credits.next_reset_at_ms)}]} /><Table size="small" rowKey={(r,index)=>`${r.expires_at_ms}:${index}`} dataSource={credits.expiry_schedule} pagination={credits.expiry_schedule.length>10?{pageSize:10,showSizeChanger:false}:false} columns={[{title:'到期时间',render:(_,r)=>r.expires_at_ms==null?'未提供到期时间':dateTime(r.expires_at_ms)},{title:'数量',align:'right',render:(_,r)=>integer(r.count)}]} /></>}]} />
 </section>;
}
