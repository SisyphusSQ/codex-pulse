import { useState, type ReactNode } from 'react';
import { Badge, Button, DatePicker, Empty, Form, Popover, Segmented, Select, Skeleton, Space, Tag, Typography } from 'antd';
import { CheckCircleOutlined, ClockCircleOutlined, ExclamationCircleOutlined, FilterOutlined, InfoCircleOutlined, ReloadOutlined } from '@ant-design/icons';
import { dayjs } from '../format';
import { devices, type Page, type Scenario } from './fixtures';

export type FilterValue = { provider: string; range: number | 'custom'; source: string; zone: string; start: string; end: string };
export const initialDesignFilter: FilterValue = { provider: 'all', range: 30, source: 'all', zone: 'Asia/Shanghai', start: '2026-09-04', end: '2026-10-03' };

export function DesignToolbar({ page, value, onChange, extra, onRefresh }: { page: Page; value: FilterValue; onChange(v: FilterValue): void; extra?: ReactNode; onRefresh(): void }) {
  const [moreOpen, setMoreOpen] = useState(false);
  const [dateOpen, setDateOpen] = useState(false);
  const dated = ['overview', 'models', 'projects', 'sessions'].includes(page);
  return <div className="ds-toolbar-wrap">
    <div className="ds-toolbar">
      <Space wrap size={8}>
        <Select aria-label="平台筛选" className="ds-provider-select" value={value.provider} onChange={provider => onChange({ ...value, provider })} options={['全部平台', 'Codex', 'Cursor', 'Grok'].map((label, i) => ({ label, value: i ? label.toLowerCase() : 'all' }))} />
        {extra}
      </Space>
      <Space wrap size={8}>
        {dated && <Segmented aria-label="日期范围" value={value.range} options={[{ label: '7天', value: 7 }, { label: '30天', value: 30 }, { label: '90天', value: 90 }]} onChange={range => onChange({ ...value, range: range as number, start: dayjs('2026-10-03').subtract(Number(range) - 1, 'day').format('YYYY-MM-DD'), end: '2026-10-03' })} />}
        {dated && <Popover trigger="click" open={dateOpen} onOpenChange={setDateOpen} title="自定义日期与报告时区" content={<div className="ds-popover"><Form layout="vertical"><Form.Item label="统计日期"><DatePicker.RangePicker aria-label="自定义日期" allowClear={false} value={[dayjs(value.start), dayjs(value.end)]} onChange={v => { if (v?.[0] && v[1]) onChange({ ...value, range: 'custom', start: v[0].format('YYYY-MM-DD'), end: v[1].format('YYYY-MM-DD') }); }} /></Form.Item><Form.Item label="报告时区"><Select aria-label="报告时区" value={value.zone} onChange={zone => onChange({ ...value, zone })} options={['Asia/Shanghai', 'UTC', 'America/New_York'].map(v => ({ value: v, label: v }))} /></Form.Item><Button type="primary" onClick={() => setDateOpen(false)}>完成</Button></Form></div>}><Button type={value.range === 'custom' ? 'primary' : 'default'}>自定义</Button></Popover>}
        <Popover title="采集来源" trigger="click" open={moreOpen} onOpenChange={setMoreOpen} content={<div className="ds-popover"><Select aria-label="采集来源筛选" className="ds-fill" value={value.source} onChange={source => onChange({ ...value, source })} options={[{ label: '全部来源', value: 'all' }, ...devices.map(d => ({ label: d.name, value: d.key }))]} /><p className="ds-muted">来源用于筛选采集事实，不表示执行归属。</p><Button size="small" onClick={() => { onChange({ ...value, source: 'all' }); setMoreOpen(false); }}>清除来源筛选</Button></div>}><Button icon={<FilterOutlined />}>筛选{value.source !== 'all' ? ' · 1' : ''}</Button></Popover>
        <Button aria-label="刷新设计数据" icon={<ReloadOutlined />} onClick={onRefresh} />
      </Space>
    </div>
    {(value.provider !== 'all' || value.source !== 'all' || value.range === 'custom') && <div className="ds-active-filters">
      <span>已筛选</span>
      {value.provider !== 'all' && <Tag closable onClose={() => onChange({ ...value, provider: 'all' })}>{value.provider}</Tag>}
      {value.source !== 'all' && <Tag closable onClose={() => onChange({ ...value, source: 'all' })}>{devices.find(d => d.key === value.source)?.name}</Tag>}
      {value.range === 'custom' && <Tag closable onClose={() => onChange({ ...value, range: initialDesignFilter.range, start: initialDesignFilter.start, end: initialDesignFilter.end })}>{value.start} — {value.end}</Tag>}
      <Button type="link" size="small" onClick={() => onChange(initialDesignFilter)}>清除全部</Button>
    </div>}
  </div>;
}

export function EvidenceIcon({ state = 'normal', label = '数据说明', children }: { state?: Scenario; label?: string; children: ReactNode }) {
  const warning = state === 'stale' || state === 'conflict';
  return <Popover trigger={['hover', 'click']} title={label} content={<div className="ds-evidence">{children}</div>}><Button type="text" size="small" aria-label={label} className={warning ? 'ds-warning-icon' : 'ds-info-icon'} icon={warning ? <ExclamationCircleOutlined /> : <InfoCircleOutlined />} /></Popover>;
}

export function ForecastStatus({ scenario = 'normal' }: { scenario?: Scenario }) {
  const restricted = scenario === 'stale' || scenario === 'conflict' || scenario === 'empty';
  return <div className="ds-section-title"><div><strong>节奏评估</strong><EvidenceIcon state={scenario} label="节奏评估说明"><p>{scenario === 'stale' ? '最近一次可信观测为昨天21:18，当前预测暂停。' : scenario === 'conflict' ? '不同采集来源对当前窗口的观测存在冲突，暂不生成预测。' : scenario === 'empty' ? '本周期采样尚不足，暂不能建立消耗趋势。' : '按已收到的可信采样计算，预测随新观测更新。'}</p><p className="ds-muted">实线连接相邻已知观测，仅作趋势参照；连线经过的缺失时段不新增采样或预测证据。历史基线不代表当前采样。</p><Typography.Text copyable>评估时间：2026-10-03 09:42</Typography.Text></EvidenceIcon>{!restricted && <Tag color="green">消耗慢于均匀节奏</Tag>}</div><span className="ds-muted">09:42 评估</span></div>;
}

export function MetricBand({ values }: { values: { label: string; value: string; note: string }[] }) {
  return <div className="ds-metric-band">{values.map(v => <div key={v.label}><span className="ds-metric-label">{v.label}</span><strong>{v.value}</strong><span className="ds-muted">{v.note}</span></div>)}</div>;
}

export function DesignState({ scenario, onRetry }: { scenario: Scenario; onRetry(): void }) {
  if (scenario === 'loading') return <div className="ds-state"><Skeleton active paragraph={{ rows: 5 }} /></div>;
  if (scenario === 'error') return <div className="ds-state ds-state-error"><ExclamationCircleOutlined /><h3>数据未能读取</h3><p className="ds-muted">暂时无法读取中心快照。原有筛选条件已保留。</p><Button onClick={onRetry}>重新读取</Button></div>;
  return <div className="ds-state"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前筛选范围没有已收到的数据" /><Button onClick={onRetry}>清除筛选</Button></div>;
}

export function FreshnessBadge({ scenario }: { scenario: Scenario }) {
  return scenario === 'stale' ? <Badge status="warning" text="观测陈旧" /> : scenario === 'conflict' ? <Badge status="error" text="来源冲突" /> : <Badge status="success" text="近期观测" />;
}
export const stateIcons = { normal: <CheckCircleOutlined />, stale: <ClockCircleOutlined />, conflict: <ExclamationCircleOutlined /> };
