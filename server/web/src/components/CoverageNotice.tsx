import { Collapse, Tag, Typography } from 'antd';
import type { Coverage } from '../api/statistics';
import { dateTime, integer } from '../format';

export function CoverageNotice({ coverage, zone }: { coverage: Coverage; zone: string }) {
 return <div className="coverage-notice">
  <div className="coverage-summary"><Tag color={coverage.state==='unknown'?'default':'gold'}>{coverage.state==='unknown'?'暂无已知用量':'已收到事实 · 覆盖未确认'}</Tag><span>{coverage.stale?'采集证据陈旧':'有近期采集证据'}</span><span>采集截至：{dateTime(coverage.collected_at_ms,zone)}</span></div>
  <Collapse ghost size="small" items={[{key:'coverage',label:'覆盖范围与数据质量',children:<><Typography.Paragraph type="secondary">当前统计基于中心已收到的事实。设备就绪或收到某一批次，并不证明全部历史已经上传。采集来源属于多对多关系；执行设备缺少证据时保留未知。</Typography.Paragraph><div className="coverage-counts"><span>未定价 {integer(coverage.unpriced_facts)}</span><span>未知计数 {integer(coverage.unknown_token_facts)}</span><span>无时间 {integer(coverage.untimed_facts)}</span><span>部分会话 {integer(coverage.partial_sessions)}</span><span>冲突会话 {integer(coverage.conflicted_sessions)}</span></div></>}]} />
 </div>;
}
