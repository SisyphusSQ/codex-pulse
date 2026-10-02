import { useMemo, useState } from 'react';
import { App, Badge, Button, Card, Collapse, DatePicker, Descriptions, Empty, Form, Input, InputNumber, Modal, Popover, Segmented, Select, Space, Table, Tabs, Tag, Typography } from 'antd';
import { ArrowRightOutlined, CheckCircleOutlined, LaptopOutlined, MoreOutlined } from '@ant-design/icons';
import type { EChartsCoreOption } from 'echarts/core';
import Chart from '../components/Chart';
import { ActivityHeatmap } from '../components/ActivityHeatmap';
import { dayjs, dollarAxis, dollars, tokenAxis, tokens } from '../format';
import { devices, heatmapDays, modelRows, modelSeries, paceSamples, providerRows, skillRows, toolRows, trendDates, type Page, type Scenario } from './fixtures';
import { EvidenceIcon, ForecastStatus, FreshnessBadge, MetricBand, type FilterValue } from './components';

export function ModelTrend({ filter, model = 'all', compact = false }: { filter: FilterValue; model?: string; compact?: boolean }) {
  const [metric, setMetric] = useState('tokens');
  const [selected, setSelected] = useState<string[]>(modelSeries.map(m => m.key));
  const available = modelRows.filter(r => r.key !== 'unknown' && (filter.provider === 'all' || r.provider.toLowerCase() === filter.provider) && (model === 'all' || r.key === model));
  const visible = available.filter(r => selected.includes(r.key));
  const dates = trendDates.filter(d => d >= filter.start && d <= filter.end);
  const option = useMemo<EChartsCoreOption>(() => ({
    animation: false,
    color: visible.map(r => r.color),
    tooltip: { renderMode: 'richText', confine: true, trigger: 'axis', valueFormatter: (v: unknown) => v === null ? '未知' : metric === 'tokens' ? tokens(Math.round(Number(v))) : `$${Number(v).toFixed(2)}` },
    legend: { bottom: 0, icon: 'circle', itemWidth: 7, itemHeight: 7, textStyle: { color: '#617089', fontSize: 12 } },
    grid: { left: 10, right: 12, top: 28, bottom: 42, containLabel: true },
    xAxis: { type: 'category', data: dates.map(d => d.slice(5)), axisTick: { show: false }, axisLine: { show: false }, axisLabel: { color: '#718096', fontSize: 11 } },
    yAxis: { type: 'value', name: metric === 'tokens' ? 'Token' : 'USD', axisLabel: { formatter: metric === 'tokens' ? tokenAxis : dollarAxis, color: '#718096' }, splitLine: { lineStyle: { color: '#edf1f6' } } },
    series: visible.map(r => ({ name: r.model, type: 'line', smooth: false, showSymbol: false, connectNulls: false, lineStyle: { width: 2.5 }, data: dates.map(date => { const fixture = modelSeries.find(m => m.key === r.key)!; const index = trendDates.indexOf(date); const p = metric === 'tokens' ? fixture.points[index] : fixture.costs[index]; return p === null ? null : metric === 'tokens' ? p : p / 1_000_000; }) })),
  }), [visible.map(r => r.key).join(','), metric, filter.start, filter.end]);
  return <Card title="模型用量趋势" className="ds-chart-card" extra={<Segmented aria-label="趋势指标" value={metric} onChange={v => setMetric(String(v))} options={[{ label: 'Token', value: 'tokens' }, { label: 'API 成本', value: 'cost' }]} />}>
    <div className="ds-chart-tools"><span className="ds-muted">按自然日 · 未知日期保留断点</span>{!compact && <Select aria-label="趋势模型" mode="multiple" maxTagCount={1} value={selected.filter(key => available.some(r => r.key === key))} onChange={setSelected} options={available.map(r => ({ label: r.model, value: r.key }))} className="ds-model-select" />}</div>
    {visible.length ? <Chart option={option} label="按模型的折线趋势" height={compact ? 235 : 275} /> : <div className="ds-chart-empty">{model === 'unknown' ? '未归因模型暂无可分解的日趋势' : '选择模型后查看趋势'}</div>}
  </Card>;
}

export function OverviewDesign({ filter, scenario, navigate }: { filter: FilterValue; scenario: Scenario; navigate(page: Page, model?: string): void }) {
  const [detail, setDetail] = useState('providers');
  const [calls, setCalls] = useState('tools');
  const rows = modelRows.filter(r => filter.provider === 'all' || r.provider.toLowerCase() === filter.provider);
  const providers = providerRows.filter(r => filter.provider === 'all' || r.key === filter.provider);
  const sources = devices.filter(d => (filter.source === 'all' || filter.source === d.key) && (filter.provider === 'all' || d.providers.some(p => p.toLowerCase() === filter.provider)));
  const summary = filter.provider === 'codex' ? ['3194万', '$27.04', '94'] : filter.provider === 'cursor' ? ['1032万', '$8.60', '23'] : filter.provider === 'grok' ? ['632万', '$2.60', '11'] : ['4858万', '$38.24', '128'];
  const details = detail === 'models' ? rows.map(r => ({ ...r, name: r.model })) : providers.map(r => ({ ...r, name: r.provider }));
  return <div className="ds-stack">
    <Card title="全年活动" className="ds-activity-card" extra={<Space size={6}><Badge status="warning" text="年度覆盖未确认" /><EvidenceIcon state={scenario} label="年度活动统计"><p>过去365天的已收到事实，年度范围独立于上方统计日期。</p><Descriptions size="small" column={1} items={[{ key: 'total', label: '近365天已收到 Token', children: '2.4亿' }, { key: 'peak', label: '已观测峰值日 Token', children: '218.1万' }, { key: 'days', label: '已观测活跃', children: '235天' }, { key: 'streak', label: '当前连续', children: '0天' }, { key: 'longest', label: '已观测最长连续', children: '5天' }, { key: 'unknown', label: '未知日期', children: '35天' }]} /><p className="ds-muted">浅灰表示未知，最浅蓝表示明确的零；近期收到数据不证明年度历史完整。</p></EvidenceIcon></Space>}>
      <ActivityHeatmap days={heatmapDays} />
      <div className="ds-heatmap-caption"><span>2025-10-04 — 2026-10-03</span><span>全部来源 · {filter.zone}</span></div>
    </Card>
    <MetricBand values={[{ label: 'Token 总量', value: summary[0], note: filter.provider === 'all' ? '输入4088万 · 输出706万' : `输入${tokens(providers[0]?.input)} · 输出${tokens(providers[0]?.output)}` }, { label: 'API 等价成本', value: summary[1], note: '按历史费率估算 · 不是实际账单' }, { label: '已收到会话', value: summary[2], note: '调用460次 · 执行归属未知' }]} />
    <div className="ds-coverage-summary"><Tag color="gold">已收到事实 · 覆盖未确认</Tag><span>原采集截至 10-03 09:42</span><EvidenceIcon state={scenario} label="覆盖范围与数据质量"><Descriptions size="small" column={1} items={[{ key: 'range', label: '统计范围', children: `${filter.start} — ${filter.end}` }, { key: 'scope', label: '范围覆盖', children: '未确认完整' }, { key: 'unknown', label: '未知 Token 记录', children: '2条' }, { key: 'untimed', label: '无时间事实', children: '1条' }, { key: 'partial', label: '部分会话', children: '3条' }, { key: 'conflict', label: '来源冲突会话', children: '1条' }]} /><p className="ds-muted">仅统计已接受事实；采集来源不代表执行归属，不把缺口补成零。</p></EvidenceIcon></div>
    <ModelTrend filter={filter} compact />
    <div className="ds-two-columns">
      <Card title="客户端与模型用量" extra={<Segmented aria-label="用量明细维度" size="small" value={detail} onChange={v => setDetail(String(v))} options={[{ label: '客户端', value: 'providers' }, { label: '模型', value: 'models' }]} />}>
        <Table size="small" rowKey="key" pagination={false} dataSource={details} scroll={{ x: 470 }} columns={[{ title: detail === 'models' ? '模型' : '客户端', render: (_, r) => <Button type="link" className="ds-text-link" onClick={() => navigate('models', detail === 'models' ? r.key : 'all')}>{r.name}</Button> }, { title: 'Token', align: 'right', render: (_, r) => tokens(r.tokens) }, { title: 'API 成本', align: 'right', render: (_, r) => dollars(r.cost) }, { title: '会话', align: 'right', dataIndex: 'sessions' }]} />
        <Button type="link" size="small" onClick={() => navigate('models')}>查看完整用量 <ArrowRightOutlined /></Button>
      </Card>
      <Card title="采集来源" extra={<Button type="link" size="small" onClick={() => navigate('sources')}>查看全部 <ArrowRightOutlined /></Button>}>
        <div className="ds-source-mini">{sources.map(d => <div key={d.key}><div><LaptopOutlined /><strong>{d.name}</strong></div><Badge status={d.state === '已同步' ? 'success' : 'warning'} text={d.state} /><span className="ds-muted">{d.last} · {d.providers.join('、')} · {d.queue === null ? '积压未知' : `待上传${d.queue}批`}</span></div>)}</div>
        <div className="ds-inline-note">采集来源与执行归属分别保留<EvidenceIcon label="来源说明"><p>同一会话可能存在多份采集副本；来源不作为执行设备归因，也不重复累计。</p></EvidenceIcon></div>
      </Card>
    </div>
    <div className="ds-two-columns"><DistributionDesign title="客户端分布" rows={providers.map(r => ({ ...r, name: r.provider }))} /><DistributionDesign title="模型分布" rows={rows.map(r => ({ ...r, name: r.model }))} /></div>
    <Card title="工具与技能" extra={<Segmented aria-label="调用类型" value={calls} onChange={v => setCalls(String(v))} options={[{ label: '工具', value: 'tools' }, { label: '技能', value: 'skills' }]} />}>
      <Table size="small" rowKey="key" pagination={false} dataSource={calls === 'tools' ? toolRows : skillRows} columns={[{ title: calls === 'tools' ? '工具名称' : '技能名称', dataIndex: 'name' }, { title: '已收到调用次数', align: 'right', dataIndex: 'calls' }]} />
    </Card>
    <div className="ds-overview-links"><Button type="link" onClick={() => navigate('accounts')}>账号额度与节奏</Button><Button type="link" onClick={() => navigate('projects')}>项目明细</Button><Button type="link" onClick={() => navigate('sessions')}>会话明细</Button></div>
  </div>;
}

function DistributionDesign({ title, rows }: { title: string; rows: { key: string; name: string; tokens: string; cost: string; color: string }[] }) {
  const [metric, setMetric] = useState('tokens');
  const option: EChartsCoreOption = { animation: false, tooltip: { renderMode: 'richText', confine: true, trigger: 'item', formatter: (p: unknown) => { const r = rows[(p as { dataIndex: number }).dataIndex]; return `${r.name}\n${metric === 'tokens' ? tokens(r.tokens) : dollars(r.cost)}`; } }, series: [{ type: 'pie', radius: ['65%', '83%'], label: { show: false }, labelLine: { show: false }, padAngle: 1.5, data: rows.map(r => ({ name: r.name, value: Number(metric === 'tokens' ? r.tokens : r.cost), itemStyle: { color: r.color } })) }] };
  return <Card title={title} extra={<Segmented aria-label={`${title}指标`} size="small" value={metric} onChange={v => setMetric(String(v))} options={[{ label: 'Token', value: 'tokens' }, { label: 'API 成本', value: 'cost' }]} />}>
    <div className="ds-distribution"><div className="ds-donut"><Chart option={option} label={`${title}环图`} height={200} /><span>{metric === 'tokens' ? 'Token' : 'API 成本'}</span></div><div className="ds-distribution-legend">{rows.map(r => <div key={r.key}><i style={{ background: r.color }} /><span>{r.name}</span><strong>{metric === 'tokens' ? tokens(r.tokens) : dollars(r.cost)}</strong></div>)}</div></div>
    <Collapse ghost size="small" items={[{ key: 'details', label: `全部明细（${rows.length}）`, children: <Table size="small" rowKey="key" pagination={false} dataSource={rows} scroll={{ x: 400 }} columns={[{ title: '名称', dataIndex: 'name' }, { title: 'Token', align: 'right', render: (_, r) => tokens(r.tokens) }, { title: 'API 成本', align: 'right', render: (_, r) => dollars(r.cost) }]} /> }]} />
  </Card>;
}

export function ModelsDesign({ filter, initialModel, navigate }: { filter: FilterValue; initialModel: string; navigate(page: Page, model?: string): void }) {
  const [model, setModel] = useState(initialModel);
  const [search, setSearch] = useState('');
  const rows = modelRows.filter(r => (filter.provider === 'all' || r.provider.toLowerCase() === filter.provider) && (model === 'all' || r.key === model) && r.model.toLowerCase().includes(search.toLowerCase()));
  const chosen = modelRows.find(r => r.key === model);
  return <div className="ds-stack">
    <div className="ds-model-heading"><Select aria-label="分析模型" value={model} onChange={setModel} showSearch={{ optionFilterProp: 'label' }} options={[{ label: '全部模型', value: 'all' }, ...modelRows.map(r => ({ label: `${r.provider} · ${r.model}`, value: r.key }))]} /><span className="ds-muted">用量、缓存与历史成本在同一统计范围查看</span></div>
    <MetricBand values={[{ label: '模型 Token', value: chosen ? tokens(chosen.tokens) : '4858万', note: '按已收到事实统计' }, { label: 'API 等价成本', value: chosen ? dollars(chosen.cost) : '$38.24', note: '来源上报费用单独保留' }, { label: '缓存命中率', value: chosen?.cache ?? '80.3%', note: '缓存输入 / 全部输入' }]} />
    <ModelTrend key={model} filter={filter} model={model} />
    <Card title="模型用量与成本" extra={<Input.Search aria-label="搜索模型" placeholder="搜索模型" value={search} onChange={e => setSearch(e.target.value)} className="ds-search" />}>
      <Table size="small" rowKey="key" dataSource={rows} pagination={false} scroll={{ x: 850 }} expandable={{ expandedRowRender: r => <Descriptions size="small" column={{ xs: 1, md: 3 }} items={[{ key: 'price', label: '历史价格版本', children: 'reference-2026-10' }, { key: 'basis', label: '成本依据', children: 'API 等价估算' }, { key: 'charge', label: '来源上报费用', children: '未提供' }, { key: 'link', label: '价格详情', children: <Button type="link" size="small" onClick={() => navigate('pricing', r.key)}>查看历史费率</Button> }]} /> }} columns={[
        { title: '模型', render: (_, r) => <div><strong>{r.model}</strong><div className="ds-muted">{r.provider}</div></div> },
        { title: '输入', align: 'right', render: (_, r) => r.input === 'null' ? '未知' : tokens(r.input) }, { title: '输出', align: 'right', render: (_, r) => r.output === 'null' ? '未知' : tokens(r.output) },
        { title: '缓存命中', align: 'right', dataIndex: 'cache' }, { title: 'API 成本', align: 'right', render: (_, r) => dollars(r.cost) }, { title: '会话', align: 'right', dataIndex: 'sessions' },
      ]} />
    </Card>
  </div>;
}

export function SourcesDesign({ filter, navigate }: { filter: FilterValue; navigate(page: Page): void }) {
  const [selected, setSelected] = useState(devices[0].key);
  const rows = devices.filter(d => (filter.source === 'all' || d.key === filter.source) && (filter.provider === 'all' || d.providers.some(p => p.toLowerCase() === filter.provider)));
  const current = rows.find(d => d.key === selected) ?? rows[0];
  return <div className="ds-stack">
    <MetricBand values={[{ label: '已接入设备', value: '3', note: '2台近期采集 · 1台观测陈旧' }, { label: '待上传批次', value: '12', note: '保留到中心事务确认后' }, { label: '最近中心接收', value: '09:42', note: '原采集时间另行保留' }]} />
    <Card title="来源与覆盖" extra={<Button type="link" onClick={() => navigate('devices')}>管理设备</Button>}>
      <Table rowKey="key" size="small" dataSource={rows} pagination={false} scroll={{ x: 760 }} rowClassName={d => d.key === current?.key ? 'ds-selected-row' : ''} onRow={d => ({ onClick: () => setSelected(d.key) })} columns={[{ title: '设备', render: (_, d) => <Button type="link" onClick={() => setSelected(d.key)}>{d.name}</Button> }, { title: '平台', render: (_, d) => d.providers.join('、') }, { title: '采集状态', render: (_, d) => <Badge status={d.state === '已同步' ? 'success' : 'warning'} text={d.state} /> }, { title: '原采集时间', dataIndex: 'last' }, { title: '待上传', render: (_, d) => d.queue ?? '未知' }, { title: '版本', dataIndex: 'version' }]} />
    </Card>
    {current && <Card title={`${current.name} · 来源详情`} extra={<Popover trigger="click" content={<Button onClick={() => navigate('devices')}>打开设备管理</Button>}><Button type="text" icon={<MoreOutlined />} aria-label="来源操作" /></Popover>}>
      <Descriptions column={{ xs: 1, md: 3 }} items={[{ key: 'id', label: '原始设备ID', children: <Typography.Text copyable>{current.key}</Typography.Text> }, { key: 'range', label: '观测区间', children: current.coverage }, { key: 'time', label: '中心接收', children: current.last }]} />
      <div className="ds-evidence-line"><CheckCircleOutlined /> 已接收事实可查询<span className="ds-muted">近期接收不证明历史完整；缺口保留未知。</span></div>
    </Card>}
  </div>;
}

export function CreditsDesign({ scenario = 'normal' }: { scenario?: Scenario }) {
  const [evidence, setEvidence] = useState(false);
  const stale = scenario === 'stale' || scenario === 'conflict';
  return <section className="ds-credits-section">
    <div className="ds-section-title"><div><strong>Reset Credits</strong><EvidenceIcon state={scenario} label="Credits 说明"><p>可用数量由中心确认；不同设备的库存不相加。</p><p>到期与额度 reset 是不同事件。陈旧或冲突时，当前库存保持未知。</p></EvidenceIcon></div><Space size={10}><FreshnessBadge scenario={scenario} /><Button type="link" size="small" onClick={() => setEvidence(!evidence)}>{evidence ? '收起来源证据' : '来源证据'}</Button></Space></div>
    <div className="ds-credit-overview"><div><span className="ds-metric-label">当前可用</span><strong>{stale ? '未知' : '3'}{!stale && <small>份</small>}</strong><span className="ds-muted">{stale ? '上次观测 3份 · 昨天21:18' : '最近观测 今天09:42'}</span></div><div><span className="ds-metric-label">已观测到期时间</span><strong className="ds-credit-date">10月4日 09:00</strong><span className="ds-muted">原记录中的2份库存</span></div><div><span className="ds-metric-label">下次 reset</span><strong className="ds-credit-date">10月8日 09:00</strong><Tag>详情完整</Tag></div></div>
    <Table size="small" rowKey="key" pagination={false} dataSource={[{ key: 'first', date: '2026-10-04 09:00', count: 2 }, { key: 'second', date: '2026-10-11 09:00', count: 1 }]} columns={[{ title: '已观测到期明细', dataIndex: 'date' }, { title: '数量', align: 'right', dataIndex: 'count' }]} />
    {evidence && <Descriptions className="ds-credit-evidence" size="small" column={{ xs: 1, md: 2 }} items={[{ key: 'device', label: '采集来源', children: '工作 Mac' }, { key: 'id', label: '原始来源ID', children: <Typography.Text copyable>demo-device-01</Typography.Text> }, { key: 'observed', label: '原观测', children: stale ? '2026-10-02 21:18' : '2026-10-03 09:42' }, { key: 'count', label: '原观测库存', children: '3份' }]} />}
  </section>;
}

export const designPaceOption: EChartsCoreOption = {
  animation: false,
  tooltip: { renderMode: 'richText', trigger: 'axis', confine: true },
  grid: { left: 12, right: 18, top: 66, bottom: 24, containLabel: true },
  legend: { top: 4, itemWidth: 18, itemHeight: 8, itemGap: 16, textStyle: { color: '#617089', fontSize: 12 } },
  xAxis: { type: 'value', min: 0, max: 100, axisLabel: { color: '#718096', formatter: '{value}%' }, splitLine: { lineStyle: { color: '#edf1f6' } } },
  yAxis: { type: 'value', min: 0, max: 100, name: '剩余额度', axisLabel: { formatter: '{value}%' }, splitLine: { lineStyle: { color: '#edf1f6' } } },
  series: [
    { name: '本周期', type: 'line', smooth: false, connectNulls: true, showSymbol: false, lineStyle: { type: 'solid', width: 2.5, color: '#2678f5' }, itemStyle: { color: '#2678f5' }, data: paceSamples },
    { name: '上一周期', type: 'line', smooth: false, connectNulls: true, showSymbol: false, lineStyle: { type: 'solid', width: 1.8, color: '#7890b3' }, itemStyle: { color: '#7890b3' }, data: [[0, 100], [20, 86], [40, 70], [60, 54], [65, 49.6], [80, 36], [100, 20]] },
    { name: '近四周期中位数', type: 'line', smooth: false, showSymbol: false, lineStyle: { type: 'dashed', width: 1.7, color: '#8a70cf' }, itemStyle: { color: '#8a70cf' }, data: [[0, 100], [20, 85], [40, 69], [60, 52], [65, 47.7], [80, 35], [100, 20]] },
    { name: '理想节奏', type: 'line', symbol: 'none', lineStyle: { type: 'dotted', width: 1.3, color: '#a8b2c1' }, itemStyle: { color: '#a8b2c1' }, data: [[0, 100], [100, 0]] },
  ],
};

type DesignSubscription = { plan: string; renewal: number; mode: 'monthly' | 'expiry' | 'none'; expiry: string | null; zone: string; note: string };

export function AccountsDesign({ filter, scenario, navigate }: { filter: FilterValue; scenario: Scenario; navigate(page: Page): void }) {
  const { message } = App.useApp();
  const [account, setAccount] = useState('main');
  const [window, setWindow] = useState('5h');
  const [tab, setTab] = useState('pace');
  const [editOpen, setEditOpen] = useState(false);
  const [subscriptions, setSubscriptions] = useState<Record<string, DesignSubscription>>({ main: { plan: 'Pro', renewal: 31, mode: 'monthly', expiry: null, zone: 'Asia/Shanghai', note: '' }, second: { plan: 'Plus', renewal: 15, mode: 'monthly', expiry: null, zone: 'Asia/Shanghai', note: '' }, cursor: { plan: 'Pro', renewal: 8, mode: 'monthly', expiry: null, zone: 'Asia/Shanghai', note: '' }, grok: { plan: 'SuperGrok', renewal: 12, mode: 'monthly', expiry: null, zone: 'Asia/Shanghai', note: '' } });
  const accounts = [{ key: 'main', email: 'work@example.invalid', provider: 'Codex', desc: '两个额度窗口', source: 'demo-device-01' }, { key: 'second', email: 'work@example.invalid', provider: 'Codex', desc: '独立原始账号ID', source: 'demo-device-03' }, { key: 'cursor', email: 'studio@example.invalid', provider: 'Cursor', desc: '月度用量池', source: 'demo-device-01' }, { key: 'grok', email: 'lab@example.invalid', provider: 'Grok', desc: '额度观测待补齐', source: 'demo-device-02' }].filter(a => (filter.provider === 'all' || a.provider.toLowerCase() === filter.provider) && (filter.source === 'all' || a.source === filter.source));
  const current = accounts.find(a => a.key === account) ?? accounts[0];
  const currentKey = current?.key ?? account;
  const subscription = subscriptions[currentKey];
  const { plan, renewal } = subscription;
  const unknown = currentKey === 'grok';
  const restricted = scenario === 'stale' || scenario === 'conflict' || currentKey === 'second';
  const actualScenario = unknown ? 'empty' : restricted ? (scenario === 'conflict' ? 'conflict' : 'stale') : scenario;
  const windows = current?.provider === 'Codex' ? [{ key: '5h', title: '5小时额度', remaining: '56%', reset: '今天 11:30', note: '还有1小时48分' }, { key: '7d', title: '7天额度', remaining: '72%', reset: '10月8日 09:00', note: '还有4天23小时' }] : [{ key: 'monthly', title: current?.provider === 'Cursor' ? '月度用量池' : '额度窗口', remaining: '62%', reset: '10月8日 00:00', note: '按原始用量池观测' }];
  const currentWindow = windows.find(w => w.key === window) ?? windows[0];
  if (!current) return <Card><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前筛选没有匹配账号" /></Card>;
  return <div className="ds-account-grid">
    <Card title="账号" className="ds-account-list">{accounts.map(a => <button type="button" className={`ds-account-item ${currentKey === a.key ? 'selected' : ''}`} key={a.key} onClick={() => { setAccount(a.key); setWindow('5h'); }}><span className="ds-account-provider">{a.provider}</span><strong>{a.email}</strong><span>{subscriptions[a.key].plan} · {a.desc}</span><small>demo-account-{a.key}</small></button>)}</Card>
    <Card className="ds-account-detail">
      <div className="ds-account-title"><div><Tag>{current.provider}</Tag><strong>{current.email}</strong><div className="ds-muted"><Typography.Text copyable>demo-account-{currentKey}</Typography.Text></div></div><Button onClick={() => setEditOpen(true)}>编辑订阅</Button></div>
      <div className="ds-subscription-line"><span>订阅套餐 <strong>{plan}</strong></span><span>{subscription.mode === 'monthly' ? `每月${renewal}日续费` : subscription.mode === 'expiry' ? '订阅到期日' : '续费日期未设置'} <strong>{subscription.mode === 'monthly' ? `2026-10-${String(renewal).padStart(2, '0')}` : subscription.expiry}</strong></span>{subscription.note && <span>{subscription.note}</span>}<Button type="link" size="small" onClick={() => navigate('pricing')}>参考价目</Button></div>
      <div className="ds-window-row">{windows.map(w => <button type="button" key={w.key} onClick={() => setWindow(w.key)} className={`ds-window-card ${currentWindow.key === w.key ? 'selected' : ''}`}><div><strong>{w.title}</strong><Badge status={restricted || unknown ? 'warning' : 'success'} text={restricted || unknown ? '当前未知' : '近期观测'} /></div><strong className="ds-window-value">{restricted || unknown ? '当前未知' : `${w.remaining} 剩余`}</strong><span>{unknown ? '尚未收到额度观测' : restricted ? `上次剩余 ${w.remaining} · 昨天21:18` : `reset ${w.reset}`}</span><small>{restricted || unknown ? '等待新观测' : w.note}</small></button>)}</div>
      {current.provider === 'Codex' && <CreditsDesign key={`credits:${currentKey}`} scenario={actualScenario} />}
      <Tabs activeKey={tab} onChange={setTab} items={[{ key: 'pace', label: '节奏与历史', children: <div>
        <PaceContent key={`${currentKey}:${currentWindow.key}`} scenario={actualScenario} chart={designPaceOption} />
      </div> }, { key: 'evidence', label: '来源证据', children: <Descriptions column={{ xs: 1, md: 2 }} items={[{ key: 'source', label: '当前来源', children: devices.find(d => d.key === current.source)?.name }, { key: 'time', label: '原采集时间', children: unknown ? '尚无观测' : restricted ? '昨天21:18' : '今天09:42' }, { key: 'reset', label: '原观测 reset', children: unknown ? '未知' : currentWindow.reset }, { key: 'window', label: '窗口身份', children: `demo-account-${currentKey} / ${currentWindow.key}` }]} /> }]} />
      {editOpen && <SubscriptionEditor key={`subscription:${currentKey}`} value={subscription} onCancel={() => setEditOpen(false)} onSave={value => { setSubscriptions({ ...subscriptions, [currentKey]: value }); setEditOpen(false); void message.success('已更新当前设计预览'); }} />}
    </Card>
  </div>;
}

export function PaceContent({ scenario, chart }: { scenario: Scenario; chart: EChartsCoreOption }) {
  const [history, setHistory] = useState('current');
  const unavailable = ['stale', 'conflict', 'empty'].includes(scenario);
  const hasHistory = scenario !== 'empty';
  const series = chart.series as { name: string }[];
  const visibleChart = unavailable ? { ...chart, series: series.filter(s => s.name !== '本周期') } : chart;
  const detailRows = history === 'current'
    ? [{ key: 1, time: '09:42', progress: '65%', remaining: '56%', source: '工作 Mac' }, { key: 2, time: '09:35', progress: '62.7%', remaining: '57.5%', source: '工作 Mac' }, { key: 3, time: '09:20', progress: '57.7%', remaining: '61%', source: '家用 Mac' }, { key: 4, time: '08:00', progress: '31%', remaining: '79%', source: '工作 Mac' }]
    : [{ key: 1, time: '昨天11:30', progress: '100%', remaining: '20%', source: '工作 Mac' }, { key: 2, time: '昨天09:45', progress: '65%', remaining: '49.6%', source: '家用 Mac' }, { key: 3, time: '昨天08:30', progress: '40%', remaining: '70%', source: '工作 Mac' }];
  return <div className="ds-pace-section">
    <ForecastStatus scenario={scenario} />
    <MetricBand values={[
      { label: '已使用', value: unavailable ? '未知' : '44%', note: '选中额度窗口' },
      { label: '周期进度', value: scenario === 'empty' ? '未知' : '65%', note: '按实际窗口时间' },
      { label: '节奏差', value: unavailable ? '未知' : '−21.0 pp', note: '相对于均匀消耗' },
      { label: '历史基线', value: hasHistory ? '4个周期' : '未知', note: '首尾覆盖的历史周期' },
    ]} />
    {hasHistory ? <Chart option={visibleChart} label="本周期、上一周期、近四周期中位数与理想节奏" height={300} /> : <div className="ds-quiet-empty">尚无可绘制的周期观测</div>}
    <div className="ds-pace-axes"><span>横轴：周期进度</span><span>纵轴：剩余额度</span></div>
    <div className="ds-pace-results">
      <section className={`ds-forecast-result ${unavailable ? 'limited' : ''}`}><strong>保守耗尽推算</strong><span>{unavailable ? '暂不能推算' : '预计可坚持到 reset'}</span><small>{unavailable ? '原因见节奏评估旁的说明图标' : '219条已知观测 · 最近采样跨度3小时12分'}</small></section>
      <section><strong>同进度对比</strong><dl><dt>上一周期</dt><dd>{unavailable ? '当前无法比较' : '剩余多6.4个百分点'}</dd><dt>近四周期中位数</dt><dd>{unavailable ? '当前无法比较' : '剩余多8.3个百分点'}</dd></dl></section>
    </div>
    <Collapse ghost size="small" items={[{ key: 'samples', label: '实际采样明细与历史周期', children: <>
      <div className="ds-history-toolbar"><span className="ds-muted">连线只用于趋势参照，缺失时段不新增观测</span><Select aria-label="查看采样周期" value={history} onChange={setHistory} options={[{ value: 'current', label: '当前周期' }, { value: 'previous', label: '上一周期' }]} /></div>
      <Table size="small" rowKey="key" pagination={false} dataSource={!hasHistory ? [] : unavailable && history === 'current' ? detailRows.map((r, i) => ({ ...r, time: ['昨天21:18', '昨天21:11', '昨天20:55', '昨天20:40'][i] })) : detailRows} scroll={{ x: 490 }} columns={[{ title: '原观测时间', dataIndex: 'time' }, { title: '周期进度', dataIndex: 'progress' }, { title: '剩余额度', dataIndex: 'remaining' }, { title: '采集来源', dataIndex: 'source' }]} />
    </> }]} />
  </div>;
}
function SubscriptionEditor({ value, onCancel, onSave }: { value: DesignSubscription; onCancel(): void; onSave(value: DesignSubscription): void }) {
  const [form] = Form.useForm<{ plan: string; day: number; mode: DesignSubscription['mode']; expires: ReturnType<typeof dayjs> | null; zone: string; note: string }>();
  const mode = Form.useWatch('mode', form) ?? value.mode;
  return <Modal title="编辑账号订阅" open onCancel={onCancel} onOk={() => form.submit()} okText="保存" cancelText="取消">
    <Form form={form} layout="vertical" initialValues={{ plan: value.plan, day: value.renewal, mode: value.mode, expires: value.expiry ? dayjs(value.expiry) : null, zone: value.zone, note: value.note }} onFinish={v => onSave({ plan: v.plan, renewal: v.day, mode: v.mode, expiry: v.mode === 'expiry' ? v.expires!.format('YYYY-MM-DD') : null, zone: v.zone, note: v.note })}>
      <Form.Item label="账号备注" name="note"><Input maxLength={60} placeholder="便于识别的账号备注" /></Form.Item>
      <Form.Item label="套餐" name="plan" rules={[{ required: true }]}><Select options={['Pro', 'Plus', 'SuperGrok', '自定义'].map(value => ({ value, label: value }))} /></Form.Item>
      <Form.Item label="订阅日期" name="mode"><Segmented aria-label="订阅日期类型" block options={[{ value: 'monthly', label: '每月续费' }, { value: 'expiry', label: '指定到期日' }, { value: 'none', label: '未设置' }]} /></Form.Item>
      {mode === 'monthly' && <Form.Item label="每月续费日" name="day" rules={[{ required: true }]}><InputNumber min={1} max={31} precision={0} className="ds-fill" /></Form.Item>}
      {mode === 'expiry' && <Form.Item label="完整到期日" name="expires" rules={[{ required: true, message: '请选择到期日' }]}><DatePicker className="ds-fill" /></Form.Item>}
      <Form.Item label="日期时区" name="zone" rules={[{ required: true }]}><Select showSearch={{ optionFilterProp: 'label' }} options={['Asia/Shanghai', 'UTC', 'America/New_York'].map(v => ({ value: v, label: v }))} /></Form.Item>
      <Typography.Text type="secondary">短月按月末处理；订阅日期与额度 reset 分开。</Typography.Text>
    </Form>
  </Modal>;
}
