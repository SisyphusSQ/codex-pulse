import { lazy, Suspense, useMemo, useState } from 'react';
import { Alert, Button, Card, Collapse, Descriptions, Progress, Select, Statistic, Table, Tag, Typography, theme } from 'antd';
import type { EChartsCoreOption } from 'echarts/core';
import type { PacePoint, PaceWindow, QuotaAccount, QuotaCredits, QuotaWindow } from '../api/quotas';
import type { Device } from '../api/statistics';
import { dateTime, integer, providerNames } from '../format';
import { EmptyState, LoadingState } from './QueryState';

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
export function AccountIdentity({account,provider}:{account?:QuotaAccount;provider:string}){
 return <div className="account-identity"><Tag>{providerNames[provider]??provider}</Tag><strong>{account?.email??(account?'邮箱未提供':'账号待关联')}</strong><div className="record-id">原始账号 ID：{account?.raw_id??'尚未确认'}</div><div className="metric-note">套餐：{account?.plan??'未知'}{account?` · 资料采集：${dateTime(account.collected_at_ms)}`:''}</div></div>;
}
export function QuotaWindowCard({window,account,devices,onSelect}:{window:QuotaWindow;account?:QuotaAccount;devices:Device[];onSelect():void}){
 const c=window.current;
 const {token}=theme.useToken();
 const source=devices.find(d=>d.id===c.selected_client_id)?.name??window.observations.find(o=>o.client_id===c.selected_client_id)?.client_name??'暂无可信采集来源';
 return <Card title={`${window.limit_id} · ${window.window_kind==='primary'?'主窗口':window.window_kind==='secondary'?'次窗口':window.window_kind}`} extra={<QuotaStatus state={c.freshness} conflict={c.conflict} />} className="quota-card">
 <AccountIdentity account={account} provider={window.provider} />
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

export function paceChartOption(window:PaceWindow, color='#1677ff'):EChartsCoreOption{
 const points=(name:string,values:PacePoint[],color:string)=>({name,type:'line',showSymbol:true,symbolSize:7,lineStyle:{opacity:0},itemStyle:{color},connectNulls:false,data:values.map(p=>({value:[p.elapsed_percent,p.remaining_percent],observedAt:p.observed_at_ms,linked:p.linked_history}))});
 return {tooltip:{renderMode:'richText',confine:true,trigger:'item',formatter:(params:unknown)=>{const p=params as {seriesName:string;value:number[];data:{observedAt?:number;linked?:boolean}};return `${p.seriesName}\n窗口进度 ${percent(p.value[0])} · 剩余 ${percent(p.value[1])}${p.data.observedAt!=null?`\n实际观测 ${dateTime(p.data.observedAt)}${p.data.linked?' · 关联历史':''}`:'\n按历史周期计算的进度网格'}`;}},legend:{type:'scroll',top:0},grid:{top:64,left:12,right:16,bottom:50,containLabel:true},xAxis:{type:'value',min:0,max:100,name:'窗口进度 %',nameLocation:'middle',nameGap:26},yAxis:{type:'value',min:0,max:100,name:'剩余额度 %'},series:[
 points('本周期实际采样',window.current_points,color),points('上一周期实际采样',window.previous_cycle?.points??[],'#a9b5c2'),
 ...[['历史中位基线','median_remaining','#485e79'],['历史上界','maximum_remaining','#bdc7d3'],['历史下界','minimum_remaining','#bdc7d3']].map(([name,field,color])=>({name,type:'line',symbol:'none',itemStyle:{color},lineStyle:{type:'dashed',width:field==='median_remaining'?2:1,color},data:window.history_band.map(p=>[p.elapsed_percent,p[field as 'median_remaining'|'minimum_remaining'|'maximum_remaining']])})),
 ]};
}
export function PacePanel({window,evaluatedAt}:{window:PaceWindow;evaluatedAt:number}){
 const {token}=theme.useToken();
 const [cycleID,setCycleID]=useState('current');
 const option=useMemo(()=>paceChartOption(window,token.colorPrimary),[window,token.colorPrimary]);
 const previous=window.previous_cycle;
 const histories=window.historical_cycles.filter(c=>c.id!==previous?.id);
 const cycleOptions=[{value:'current',label:'本周期'},...(previous?[{value:previous.id,label:`上一周期 · ${dateTime(previous.resets_at_ms)}`}]:[]),...histories.map(c=>({value:c.id,label:`历史 · ${dateTime(c.resets_at_ms)}`}))];
 const visibleCycleID=cycleOptions.some(c=>c.value===cycleID)?cycleID:'current';
 const chosen=visibleCycleID==='current'?undefined:[...(previous?[previous]:[]),...histories].find(c=>c.id===visibleCycleID);
 const rows=visibleCycleID==='current'?window.current_points:chosen?.points??[];
 const f=window.forecast;
 const states:Record<string,string>={unavailable:'预测暂不可用',on_track:'预计可坚持到 reset',at_risk:'预计会在 reset 前耗尽',exhausted:'观测额度已耗尽'};
 return <Card className="section-card">
 <Typography.Paragraph type="secondary">节奏评估：{dateTime(evaluatedAt)}</Typography.Paragraph>
 <div className="metric-grid"><div><div className="metric-label">实际窗口进度</div><Statistic value={percent(window.elapsed_percent)} /></div><div><div className="metric-label">节奏偏差（百分点）</div><Statistic aria-label="节奏偏差（百分点）" value={window.pace_delta_pp??'未知'} formatter={()=>window.pace_delta_pp==null?'未知':window.pace_delta_pp.toFixed(2)} /><div className="metric-note">正值代表使用快于均匀节奏</div></div><div><div className="metric-label">同进度历史中位剩余</div><Statistic value={percent(window.history_median_remaining_at_elapsed)} /></div></div>
 <Alert type={f.state==='at_risk'?'warning':f.state==='exhausted'?'error':f.state==='on_track'?'success':'info'} showIcon title={states[f.state]??'预测暂不可用'} description={f.state==='unavailable'?quotaReasons[f.unknown_reason??window.unknown_reason??'unavailable']??'暂缺预测证据':f.state==='at_risk'?`预测耗尽 ${dateTime(f.exhaust_at_ms)} · 提前 ${duration(f.lead_before_reset_ms)}`:'基于中心已有采样评估，随新观测更新。'} />

 {window.current_points.length||previous?.points.length||window.history_band.length?<Suspense fallback={<LoadingState label="正在加载节奏图…" />}><Chart option={option} label="本周期和上一周期实际采样，以及历史计算基线" height={330} /></Suspense>:<EmptyState description="当前没有可绘制的可信周期采样。" />}
 <Typography.Paragraph type="secondary">实际采样以点显示，不连接采集缺口。虚线为历史基线，不是新采样。</Typography.Paragraph>
 <Collapse ghost size="small" items={[{key:'samples',label:'实际采样明细与历史周期',children:<><Typography.Paragraph type="secondary">预测实际证据 {integer(f.evidence_count)} 条 · 跨度 {duration(f.evidence_span_ms)} · {f.method==='recent_theil_sen'?'近期稳健趋势':'尚未建立趋势'}。历史基线 {integer(window.history_cycle_count)} 个首尾覆盖周期，不能代表中间全程采集。</Typography.Paragraph><div className="association-bar"><Select aria-label="查看采样周期" value={visibleCycleID} onChange={setCycleID} options={cycleOptions} /><Typography.Text type="secondary">{chosen?`${dateTime(chosen.window_start_at_ms)} → ${dateTime(chosen.resets_at_ms)} · ${chosen.complete?'首尾覆盖':'首尾未覆盖'}`:'本周期仅展示实际采样，不延伸到现在'}</Typography.Text></div>
 <Table size="small" rowKey={r=>`${r.observed_at_ms}:${r.used_percent}:${r.linked_history}`} dataSource={rows} pagination={{pageSize:10,showSizeChanger:false}} scroll={{x:600}} columns={[{title:'实际观测',render:(_,r)=>dateTime(r.observed_at_ms)},{title:'窗口进度',render:(_,r)=>percent(r.elapsed_percent)},{title:'已用 / 剩余',render:(_,r)=>`${percent(r.used_percent)} / ${percent(r.remaining_percent)}`},{title:'历史来源',render:(_,r)=>r.linked_history?'显式关联历史':'确认账号采样'}]} /></>}]} />
 </Card>;
}
export function QuotaEvidence({window}:{window:QuotaWindow}){
 return <Card title="采集来源与周期证据" className="section-card"><Typography.Paragraph type="secondary">共 {integer(window.observations.length)} 条原始观测统计、{integer(window.cycles.length)} 个已观测周期。相同额度不按机器相加；接收时间和原采集时间分别保留，关联历史不刷新当前值。</Typography.Paragraph>
 <Table size="small" rowKey="id" dataSource={window.observations} pagination={{pageSize:10,showSizeChanger:false}} scroll={{x:1180}} columns={[{title:'采集设备',dataIndex:'client_name'},{title:'原观测时间',render:(_,r)=>dateTime(r.observed_at_ms)},{title:'中心接收',render:(_,r)=>dateTime(r.received_at_ms)},{title:'已用',render:(_,r)=>percent(r.used_percent)},{title:'实际 reset',render:(_,r)=>dateTime(r.resets_at_ms)},{title:'规范 reset',render:(_,r)=>dateTime(r.canonical_reset_at_ms)},{title:'来源',dataIndex:'source'},{title:'历史归属',render:(_,r)=>historyOrigin[r.history_origin]??'已记录历史'},{title:'仲裁',render:(_,r)=>`${disposition[r.disposition]??'未选中'}${r.reason?` · ${quotaReasons[r.reason]??'证据不满足当前规则'}`:''}`}]} />
 </Card>;
}
export function CreditsCard({credits,account,devices}:{credits:QuotaCredits;account?:QuotaAccount;devices:Device[]}){
 const statuses:Record<string,string>={complete:'详情完整',partial:'详情部分已知',unknown:'详情未知',unavailable:'详情未取得',failed:'详情读取失败'};
 return <Card title="Reset Credits" extra={<QuotaStatus state={credits.freshness} conflict={credits.conflict} />} className="section-card"><AccountIdentity account={account} provider={credits.provider} />
 <div className="metric-grid"><div><div className="metric-label">原观测库存</div><div className="metric-value">{integer(credits.observed_inventory)}</div></div><div><div className="metric-label">中心确认可用库存</div><div className="metric-value">{integer(credits.available_inventory)}</div></div><div><div className="metric-label">详情状态</div><div className="metric-value">{statuses[credits.details_status]??'详情未知'}</div></div></div>
 <dl className="metadata-list"><dt>原观测时间</dt><dd>{dateTime(credits.observed_at_ms)}</dd><dt>采集来源</dt><dd>{devices.find(d=>d.id===credits.client_id)?.name??'来源设备'}<div className="record-id">{credits.client_id}</div></dd><dt>下一次到期</dt><dd>{credits.next_expires_at_ms==null?'未知':dateTime(credits.next_expires_at_ms)}</dd><dt>下一次 reset</dt><dd>{credits.next_reset_at_ms==null?'未知':dateTime(credits.next_reset_at_ms)}</dd></dl>
 <Typography.Paragraph type="secondary">到期与 reset 是不同事件。库存不按设备相加；只有新鲜、无冲突且详情完整时，中心提供扣除已知到期后的可用数量。</Typography.Paragraph>
 <Table size="small" rowKey={r=>String(r.expires_at_ms)} dataSource={credits.expiry_schedule} pagination={{pageSize:10,showSizeChanger:false}} columns={[{title:'到期时间',render:(_,r)=>r.expires_at_ms==null?'未提供到期时间':dateTime(r.expires_at_ms)},{title:'数量',align:'right',render:(_,r)=>integer(r.count)}]} />
 </Card>;
}
