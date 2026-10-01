import { Alert, Tag, Tooltip } from 'antd';
import type { Coverage } from '../api/statistics';
import { dateTime, integer } from '../format';

export function CoverageNotice({ coverage, zone }: { coverage: Coverage; zone: string }) {
  return <div className="coverage-notice">
    <div className="coverage-meta"><Tag color={coverage.state==='unknown'?'default':'gold'}>{coverage.state==='unknown'?'暂无已知用量':'已收到事实 · 覆盖未确认'}</Tag><Tag>{coverage.stale?'采集证据陈旧':'有近期采集证据'}</Tag><span>采集截至：{dateTime(coverage.collected_at_ms,zone)}</span></div>
    <Alert type="info" showIcon title="当前统计基于中心已收到的事实" description="设备就绪或收到某一批次，并不证明全部历史已经上传。采集来源属于多对多关系；执行设备缺少证据时保留未知。" />
    <div className="coverage-counts">
      <Tooltip title="存在没有历史价格证据的事实，已知金额只是小计。"><span>未定价 {integer(coverage.unpriced_facts)}</span></Tooltip>
      <span>未知计数 {integer(coverage.unknown_token_facts)}</span><span>无时间 {integer(coverage.untimed_facts)}</span><span>部分会话 {integer(coverage.partial_sessions)}</span><span>冲突会话 {integer(coverage.conflicted_sessions)}</span>
    </div>
  </div>;
}
