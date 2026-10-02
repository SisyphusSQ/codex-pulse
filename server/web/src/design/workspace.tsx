import { useState } from 'react';
import { App, Badge, Button, Card, Descriptions, Empty, Form, Input, Modal, Popconfirm, Popover, Segmented, Select, Space, Splitter, Steps, Table, Tabs, Tag, Timeline, Typography } from 'antd';
import { ArrowLeftOutlined, ArrowRightOutlined, DesktopOutlined, ExportOutlined, LinkOutlined, PlusOutlined, SafetyCertificateOutlined, SearchOutlined } from '@ant-design/icons';
import { dollars, tokens } from '../format';
import { devices, modelRows, projects, sessions, type Page, type Scenario } from './fixtures';
import { EvidenceIcon, MetricBand, type FilterValue } from './components';

export function RecordsDesign({ kind, filter, navigate }: { kind: 'projects' | 'sessions'; filter: FilterValue; navigate(page: Page): void }) {
  const { message } = App.useApp();
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState(kind === 'projects' ? projects[0].key : sessions[0].key);
  const [showDetail, setShowDetail] = useState(false);
  const [tab, setTab] = useState('summary');
  const [sessionKey, setSessionKey] = useState<string | null>(null);
  const [associate, setAssociate] = useState(false);
  const [projectOverrides, setProjectOverrides] = useState<Record<string, string>>({});
  const [pendingProject, setPendingProject] = useState('Codex Pulse');
  const sessionRows = sessions.filter(s => s.title.includes(search) || s.model.includes(search) || s.key.includes(search)).filter(s => filter.provider === 'all' || s.provider.toLowerCase() === filter.provider);
  const projectRows = projects.filter(p => p.name.includes(search));
  const project = projects.find(p => p.key === selected) ?? projects[0];
  const record = sessions.find(s => s.key === (sessionKey ?? selected)) ?? sessions[0];
  const projectName = projectOverrides[record.key] ?? record.project;
  const selectingSession = kind === 'sessions' || sessionKey !== null;
  function choose(key: string) { setSelected(key); setSessionKey(null); setShowDetail(true); setTab('summary'); }
  const detail = <div className="ds-record-detail">
    <div className="ds-detail-heading"><Button className="ds-mobile-back" icon={<ArrowLeftOutlined />} onClick={() => setShowDetail(false)}>列表</Button>{sessionKey && <Button type="text" icon={<ArrowLeftOutlined />} onClick={() => setSessionKey(null)}>返回项目</Button>}<div><h2>{selectingSession ? record.title : project.name}</h2><div className="ds-muted">{selectingSession ? `${record.provider} · ${record.model}` : project.desc}</div></div>{selectingSession && <Button className="ds-associate-button" icon={<LinkOutlined />} onClick={() => { setPendingProject(projectName); setAssociate(true); }}>关联项目</Button>}</div>
    {selectingSession ? <>
      <Typography.Text className="ds-record-id" copyable>{record.key}</Typography.Text>
      <MetricBand values={[{ label: 'Token', value: tokens(record.tokens), note: '生命周期已索引事实' }, { label: 'API 等价成本', value: dollars(record.cost), note: '历史费率估算' }, { label: '缓存命中率', value: record.cache, note: '全部输入的缓存比例' }]} />
      <Tabs activeKey={tab} onChange={setTab} items={[{ key: 'summary', label: '会话详情', children: <Descriptions column={{ xs: 1, md: 2 }} items={[{ key: 'tps', label: '活跃区间 TPS', children: <strong>{record.tps}</strong> }, { key: 'turns', label: '轮次', children: record.turns }, { key: 'project', label: '所属项目', children: projectName }, { key: 'date', label: '最后活动', children: record.time }, { key: 'source', label: '采集来源', children: '工作 Mac、家用 Mac' }, { key: 'execution', label: '执行归属', children: '未知' }, { key: 'input', label: '输入 Token', children: '118万' }, { key: 'cached', label: '缓存输入', children: '104.1万' }]} /> }, { key: 'turns', label: '轮次与性能', children: <Table size="small" rowKey="key" pagination={false} dataSource={[{ key: 1, turn: '第26轮', tokens: '6.4万', tps: '42.6', time: '09:42' }, { key: 2, turn: '第25轮', tokens: '8.1万', tps: '39.8', time: '09:35' }, { key: 3, turn: '第24轮', tokens: '4.6万', tps: '44.1', time: '09:28' }]} columns={[{ title: '轮次', dataIndex: 'turn' }, { title: '输出 Token', dataIndex: 'tokens' }, { title: '活跃 TPS', dataIndex: 'tps' }, { title: '原观测', dataIndex: 'time' }]} /> }, { key: 'sources', label: '来源证据', children: <Timeline items={[{ title: '今天09:42 · 工作 Mac', content: '已接收修订12，保留原观测时间。' }, { title: '今天09:35 · 家用 Mac', content: '同会话副本，不重复累计用量。' }]} /> }]} />
    </> : <>
      <MetricBand values={[{ label: '项目 Token', value: tokens(project.tokens), note: '范围内全部会话' }, { label: 'API 等价成本', value: dollars(project.cost), note: '按历史费率估算' }, { label: '会话', value: String(project.sessions), note: '不是当前页行数' }]} />
      <Tabs activeKey={tab} onChange={setTab} items={[{ key: 'summary', label: '项目会话', children: <div>{sessions.filter(s => s.project === project.name).map(s => <button type="button" className="ds-project-session" key={s.key} onClick={() => { setSessionKey(s.key); setTab('summary'); }}><div><strong>{s.title}</strong><span>{s.model} · {s.time}</span></div><div>{tokens(s.tokens)}<ArrowRightOutlined /></div></button>)}</div> }, { key: 'models', label: '模型构成', children: <Table size="small" rowKey="key" pagination={false} dataSource={modelRows.slice(0, 3)} columns={[{ title: '模型', dataIndex: 'model' }, { title: 'Token', align: 'right', render: (_, r) => tokens(r.tokens) }, { title: 'API 成本', align: 'right', render: (_, r) => dollars(r.cost) }]} /> }, { key: 'sources', label: '来源证据', children: <Descriptions column={1} items={[{ key: 'key', label: '项目键', children: <Typography.Text copyable>demo-project-{project.key}</Typography.Text> }, { key: 'source', label: '采集来源', children: '工作 Mac、家用 Mac' }, { key: 'updated', label: '最后观测', children: project.time }]} /> }]} />
    </>}
    <div className="ds-detail-footer"><Button type="link" onClick={() => navigate('models')}>查看模型分析</Button><span className="ds-muted">只展示结构化统计，不含对话正文。</span></div>
  </div>;
  return <>
    <Card className={`ds-record-workspace ${showDetail ? 'detail-open' : ''}`}>
      <Splitter className="ds-record-splitter">
        <Splitter.Panel defaultSize="35%" min={240} max="55%" className="ds-list-pane">
          <div className="ds-list-search"><Input aria-label={kind === 'projects' ? '搜索项目' : '搜索会话'} prefix={<SearchOutlined />} placeholder={kind === 'projects' ? '搜索项目' : '标题、模型或 Session ID'} value={search} allowClear onChange={e => setSearch(e.target.value)} /></div>
          <div className="ds-list-summary">{kind === 'projects' ? `${projectRows.length}个项目` : `${sessionRows.length}条设计样例`}</div>
          {kind === 'projects' ? projectRows.map(p => <button type="button" className={`ds-record-item ${selected === p.key ? 'selected' : ''}`} key={p.key} onClick={() => choose(p.key)}><div><strong>{p.name}</strong><span>{p.time}</span></div><p>{p.desc}</p><div><span>{p.sessions}个会话</span><strong>{tokens(p.tokens)}</strong></div></button>) : sessionRows.map(s => <button type="button" className={`ds-record-item ${selected === s.key ? 'selected' : ''}`} key={s.key} onClick={() => choose(s.key)}><div><strong>{s.title}</strong><span>{s.time}</span></div><p>{s.provider} · {s.model}</p><div><span>{s.project}</span><strong>{tokens(s.tokens)}</strong></div></button>)}
          {!(kind === 'projects' ? projectRows.length : sessionRows.length) && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配记录" />}
        </Splitter.Panel>
        <Splitter.Panel className="ds-detail-pane">{detail}</Splitter.Panel>
      </Splitter>
    </Card>
    <Modal title="关联项目" open={associate} onCancel={() => setAssociate(false)} onOk={() => { setProjectOverrides({ ...projectOverrides, [record.key]: pendingProject }); setAssociate(false); void message.success('关联已更新到当前设计预览'); }} okText="关联" cancelText="取消"><p className="ds-muted">关联会话后仍保留原始 Session ID 和采集来源。</p><Select aria-label="选择关联项目" className="ds-fill" value={pendingProject} onChange={setPendingProject} options={projects.filter(p => p.key !== 'unknown').map(p => ({ value: p.name, label: p.name }))} /></Modal>
  </>;
}

export function PricingDesign({ filter, initialModel, navigate }: { filter: FilterValue; initialModel: string; navigate(page: Page, model?: string): void }) {
  const [tab, setTab] = useState('models');
  const [query, setQuery] = useState(initialModel === 'all' ? '' : modelRows.find(r => r.key === initialModel)?.model ?? '');
  const [catalog, setCatalog] = useState('used');
  const [version, setVersion] = useState('current');
  const rows = modelRows.filter(r => (filter.provider === 'all' || r.provider.toLowerCase() === filter.provider) && r.model.toLowerCase().includes(query.toLowerCase()) && (catalog === 'all' || r.key !== 'unknown'));
  const referencePrice = (value: string) => value === '未知' ? <Tag>未定价</Tag> : <Popover content={`原始参考数值：$${value}/百万Token`}><span>{dollars((BigInt(value.split('.')[0]) * 1_000_000n + BigInt((value.split('.')[1] ?? '').padEnd(6, '0'))).toString())}</span></Popover>;
  return <Card className="ds-pricing-card"><Tabs activeKey={tab} onChange={setTab} items={[{ key: 'models', label: '模型价目', children: <>
    <div className="ds-catalog-toolbar"><Space wrap><Input.Search aria-label="搜索模型价目" placeholder="搜索模型名称" value={query} allowClear onChange={e => setQuery(e.target.value)} className="ds-search" /><Segmented aria-label="价目目录" value={catalog} onChange={v => setCatalog(String(v))} options={[{ label: '已使用模型', value: 'used' }, { label: '完整目录', value: 'all' }]} /></Space><Select aria-label="价格版本" value={version} onChange={setVersion} options={[{ label: '当前参考价格', value: 'current' }, { label: '历史价格版本', value: 'history' }]} /></div>
    <div className="ds-catalog-note">USD / 百万Token · {version === 'current' ? '参考目录核对：2026-10-03' : '历史版本：2026-09-01'}<EvidenceIcon label="模型价格说明"><p>当前参考价格用于查阅，不改写已记录的历史成本。示例费率为合成设计数据。</p><p>部分费率的精确值可在金额提示中查看。</p></EvidenceIcon></div>
    <Table size="small" rowKey="key" dataSource={rows} pagination={false} scroll={{ x: 900 }} expandable={{ expandedRowRender: r => <Descriptions size="small" column={{ xs: 1, md: 3 }} items={[{ key: 'basis', label: '价格用途', children: '公开参考，历史费用独立' }, { key: 'version', label: '版本', children: version === 'current' ? 'reference-2026-10' : 'reference-2026-09' }, { key: 'origin', label: '来源', children: '官方价目来源（设计示例）' }, { key: 'mode', label: '模式', children: '标准文本' }, { key: 'model', label: '原始模型名', children: <Typography.Text copyable>{r.model}</Typography.Text> }]} /> }} columns={[{ title: '模型', render: (_, r) => <div><strong>{r.model}</strong><div className="ds-muted">{r.provider}</div></div> }, { title: '输入', align: 'right', render: (_, r) => referencePrice(r.inputPrice) }, { title: '缓存输入', align: 'right', render: (_, r) => referencePrice(r.cachedPrice) }, { title: '输出', align: 'right', render: (_, r) => referencePrice(r.outputPrice) }, { title: '状态', render: (_, r) => <Tag color={r.key === 'unknown' ? undefined : 'blue'}>{r.key === 'unknown' ? '未定价' : '参考价格'}</Tag> }, { title: '用量', render: (_, r) => <Button type="link" size="small" onClick={() => navigate('models', r.key)}>查看用量</Button> }]} />
  </> }, { key: 'plans', label: '订阅与额度参考', children: <>
    <div className="ds-plan-grid">{[{ provider: 'Codex / ChatGPT', plan: 'Plus', price: '$20.00 / 月', quota: '按账号实际窗口观测', reset: '5小时 / 每周，以来源观测为准' }, { provider: 'Cursor', plan: 'Pro', price: '$20.00 / 月', quota: '用量池与订阅周期独立展示', reset: '按账号套餐与实际观测' }, { provider: 'Grok', plan: 'SuperGrok', price: '$30.00 / 月', quota: '具体额度以来源可采集信息为准', reset: '缺失规则保留未知' }].filter(p => filter.provider === 'all' || p.provider.toLowerCase().includes(filter.provider)).map(p => <Card size="small" key={p.provider} title={p.provider}><Tag>{p.plan}</Tag><strong className="ds-plan-price">{p.price}</strong><Descriptions size="small" column={1} items={[{ key: 'quota', label: '额度参考', children: p.quota }, { key: 'reset', label: 'reset规则', children: p.reset }]} /><Button type="link" onClick={() => navigate('accounts')}>查看账号订阅</Button></Card>)}</div><div className="ds-muted">套餐价格为设计样本；账号实际订阅、配额窗口和自动续费日期分别保存。</div>
  </> }]} /></Card>;
}

export function DevicesDesign({ scenario }: { scenario: Scenario }) {
  const { message } = App.useApp();
  const [selected, setSelected] = useState(devices[0].key);
  const [pairOpen, setPairOpen] = useState(false);
  const [kind, setKind] = useState('collector');
  const [step, setStep] = useState(0);
  const [revoked, setRevoked] = useState<string[]>([]);
  const [name, setName] = useState(devices[0].name);
  const [names, setNames] = useState<Record<string, string>>({});
  const current = devices.find(d => d.key === selected)!;
  return <div className="ds-stack">
    <div className="ds-device-head"><span className="ds-muted">采集时间、中心接收与设备授权分别查看</span><Button type="primary" icon={<PlusOutlined />} onClick={() => { setPairOpen(true); setStep(0); }}>接入设备</Button></div>
    <div className="ds-device-grid">{devices.map(d => <Card key={d.key} className={selected === d.key ? 'ds-device-card selected' : 'ds-device-card'} title={<Space><DesktopOutlined />{names[d.key] ?? d.name}</Space>} extra={<Badge status={revoked.includes(d.key) ? 'default' : d.state === '已同步' && scenario !== 'stale' ? 'success' : 'warning'} text={revoked.includes(d.key) ? '已撤销' : scenario === 'stale' ? '观测陈旧' : d.state} />}><p className="ds-muted">{d.host}</p><Space wrap>{d.providers.map(p => <Tag key={p}>{p}</Tag>)}</Space><Descriptions size="small" column={1} items={[{ key: 'time', label: '最近接收', children: d.last }, { key: 'pending', label: '待上传', children: d.queue ?? '未知' }, { key: 'version', label: 'App版本', children: d.version }]} /><Button onClick={() => { setSelected(d.key); setName(names[d.key] ?? d.name); }}>设备详情</Button></Card>)}</div>
    <Card title={`${names[current.key] ?? current.name} · 管理`} extra={<Tag>collector权限</Tag>}><Descriptions column={{ xs: 1, md: 2 }} items={[{ key: 'id', label: '原始设备ID', children: <Typography.Text copyable>{current.key}</Typography.Text> }, { key: 'access', label: '允许操作', children: '上报与自身进度查询' }, { key: 'range', label: '已观测区间', children: current.coverage }, { key: 'host', label: '接入方式', children: 'HTTP · Tailscale私网' }]} /><div className="ds-device-actions"><Input aria-label="设备名称" value={name} onChange={e => setName(e.target.value)} className="ds-search" /><Button disabled={!name.trim()} onClick={() => { setNames({ ...names, [current.key]: name.trim() }); void message.success('名称已更新到当前设计预览'); }}>保存名称</Button><Popconfirm title="撤销该设备的上报授权？" description="已接收统计保留，重新接入需要再次配对。" onConfirm={() => { setRevoked([...revoked, current.key]); void message.success('已在当前设计预览中撤销'); }} okText="撤销" cancelText="取消"><Button danger disabled={revoked.includes(current.key)}>撤销授权</Button></Popconfirm></div></Card>
    <Card size="small" title="浏览器管理会话"><Space><SafetyCertificateOutlined /><strong>当前管理浏览器</strong><Tag color="green">管理权限</Tag><span className="ds-muted">设备上报凭证不能获得管理权限</span></Space></Card>
    <Modal title="接入设备" open={pairOpen} onCancel={() => setPairOpen(false)} footer={step === 2 ? <Button type="primary" onClick={() => setPairOpen(false)}>完成</Button> : <Button type="primary" onClick={() => setStep(step + 1)}>{step === 0 ? '生成示例配对码' : '模拟配对完成'}</Button>}>
      <Steps size="small" current={step} items={[{ title: '用途' }, { title: '设备码' }, { title: '完成' }]} />
      {step === 0 ? <div className="ds-pair-body"><Form layout="vertical"><Form.Item label="配对用途"><Select aria-label="配对用途" value={kind} onChange={setKind} options={[{ value: 'collector', label: 'Mac设备上报' }, { value: 'admin', label: '浏览器管理' }]} /></Form.Item></Form><p className="ds-muted">HTTP与HTTPS共用设备码流程；用途决定凭证权限。</p></div> : step === 1 ? <div className="ds-pair-body"><Typography.Text className="ds-pair-code" copyable>DEMO-4826</Typography.Text><p>在{kind === 'collector' ? 'Mac客户端的中心同步设置' : '浏览器授权页'}中输入设备码。</p><Tag>示例码 · 不能用于真实授权</Tag></div> : <div className="ds-pair-body"><Badge status="success" text="配对流程已展示" /><p className="ds-muted">这是设计预览，不会签发任何实际凭证。</p></div>}
    </Modal>
  </div>;
}

export function SignInDesign({ navigate }: { navigate(page: Page): void }) {
  const [code, setCode] = useState('');
  const [error, setError] = useState(false);
  return <div className="ds-signin"><SafetyCertificateOutlined /><h1>连接你的用量中心</h1><p className="ds-muted">使用管理员提供的一次性浏览器配对码</p><Form layout="vertical" onFinish={() => code.trim() ? navigate('overview') : setError(true)}><Form.Item label="中心地址"><Input value="http://127.0.0.1:18085" readOnly /></Form.Item><Form.Item label="浏览器配对码" validateStatus={error ? 'error' : undefined} help={error ? '请输入示例配对码' : undefined}><Input aria-label="浏览器配对码" placeholder="例如 DEMO-4826" value={code} onChange={e => { setCode(e.target.value); setError(false); }} /></Form.Item><Button htmlType="submit" type="primary" block>进入设计预览</Button></Form><div className="ds-signin-note"><SafetyCertificateOutlined /> 一次性配对 · 可撤销管理会话</div></div>;
}

export function DesignIndex({ navigate }: { navigate(page: Page): void }) {
  return <div className="ds-stack"><MetricBand values={[{ label: '页面设计', value: '9', note: '覆盖完整Web工作台' }, { label: '公共交互', value: '筛选与分屏', note: '同一AntD视觉系统' }, { label: '数据环境', value: '合成样本', note: '不连接中心或真实账号' }]} /><Card title="设计参考"><Space wrap><Button icon={<ExportOutlined />} href="https://github.com/juejin-cn/juejin-usage" target="_blank" rel="noreferrer">掘金Usage</Button><Button icon={<ExportOutlined />} href="https://github.com/chengzuopeng/ccgauge" target="_blank" rel="noreferrer">ccgauge</Button><Button icon={<ExportOutlined />} href="https://github.com/hamzaahmedkhan/ccusage-web" target="_blank" rel="noreferrer">ccusage-web</Button></Space><p className="ds-muted">活动日历、紧凑筛选、模型分析与窗口历史；继续沿用Pulse的中文口径与账号隔离。</p><Button type="primary" onClick={() => navigate('overview')}>开始浏览所有页面</Button></Card></div>;
}
