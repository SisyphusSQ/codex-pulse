import {Collapse,Descriptions,Table,Typography} from 'antd';
import type {PaidCredit, PaidCreditPoint} from '../api/quotas';
import {dateTime} from '../format';

export function creditAmount(value?:string,unlimited=false):string {
 if(unlimited)return '无限额';
 if(value===undefined||!/^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(value))return '—';
 const [integer,fraction]=value.split('.');
 return BigInt(integer).toLocaleString('zh-CN')+(fraction===undefined?'':`.${fraction}`);
}

export function PaidCreditsCard({value}:{value:PaidCredit}) {
 const credit=value.credit;
 return <section className="credits-section" aria-label="Credits 余额与消耗">
  <div className="section-heading"><strong>Credits{credit.limitId!=='codex'?` · ${credit.limitId}`:''}</strong><Typography.Text type="secondary">{credit.freshness==='fresh'?'最近观测':credit.freshness==='conflict'?'来源冲突':'尚未更新'}</Typography.Text></div>
  <div className="metric-label">可用余额</div>
  <div className="metric-value">{creditAmount(credit.balance,credit.unlimited)}</div>
  <div className="metric-note">最后观测 {credit.observedAtMs>0?dateTime(credit.observedAtMs):'尚未取得'}</div>
  <Descriptions size="small" column={{xs:1,md:3}} items={['今日消耗','近7天消耗','近30天消耗'].map(label=>({key:label,label,children:'—'}))}/>
  <p className="metric-note">官方暂未提供实际消耗统计，余额变化不等于实际扣费。</p>
  <Collapse ghost size="small" items={[{key:'history',label:'近30天余额历史',children:<>
   <p className="metric-note">每天保留最后一次有效观测（UTC），采集空档不补值。</p>
   <Table<PaidCreditPoint> size="small" rowKey="observedAtMs" dataSource={credit.history} pagination={false} columns={[{title:'观测时间',render:(_,row)=>dateTime(row.observedAtMs)},{title:'余额（Credits）',align:'right',render:(_,row)=>creditAmount(row.balance,row.unlimited)}]}/>
  </>}]} />
 </section>;
}
