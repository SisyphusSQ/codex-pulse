import { useEffect, useState } from 'react';
import { App, Badge, Breadcrumb, Button, Drawer, Layout, Menu, Tag } from 'antd';
import { BarChartOutlined, DashboardOutlined, DatabaseOutlined, DesktopOutlined, DollarOutlined, FolderOutlined, LineChartOutlined, MenuOutlined, MessageOutlined, SafetyCertificateOutlined } from '@ant-design/icons';
import { AccountsDesign, ModelsDesign, OverviewDesign, SourcesDesign } from './analytics';
import { DesignState, DesignToolbar, initialDesignFilter } from './components';
import { pageNames, type Page, type Scenario } from './fixtures';
import { DevicesDesign, PricingDesign, RecordsDesign, SignInDesign } from './workspace';

const icons = { overview: <BarChartOutlined />, models: <LineChartOutlined />, sources: <DatabaseOutlined />, accounts: <DashboardOutlined />, projects: <FolderOutlined />, sessions: <MessageOutlined />, pricing: <DollarOutlined />, devices: <DesktopOutlined />, signin: <SafetyCertificateOutlined /> };

export interface StudioProps { page: Page; scenario?: Scenario; initialModel?: string }
export function Studio({ page: initialPage, scenario: initialScenario = 'normal', initialModel = 'all' }: StudioProps) {
  const { message } = App.useApp();
  const [page, setPage] = useState(initialPage);
  const [scenario, setScenario] = useState(initialScenario);
  const [model, setModel] = useState(initialModel);
  const [filter, setFilter] = useState(initialDesignFilter);
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => { setPage(initialPage); setModel(initialModel); }, [initialPage, initialModel]);
  useEffect(() => setScenario(initialScenario), [initialScenario]);
  function navigate(next: Page, selectedModel = 'all') { setPage(next); setModel(selectedModel); setScenario('normal'); setMenuOpen(false); }
  function refresh() { setScenario('normal'); void message.success('已恢复设计样本'); }
  const menu = <Menu selectedKeys={[page]} onClick={({ key }) => navigate(key as Page)} items={Object.entries(pageNames).map(([key, label]) => ({ key, label, icon: icons[key as Page] }))} />;
  const blocked = ['empty', 'error', 'loading'].includes(scenario);
  const body = blocked ? <DesignState scenario={scenario} onRetry={() => { if (scenario === 'empty') setFilter(initialDesignFilter); setScenario('normal'); }} /> : page === 'overview' ? <OverviewDesign filter={filter} scenario={scenario} navigate={navigate} /> : page === 'models' ? <ModelsDesign key={model} filter={filter} initialModel={model} navigate={navigate} /> : page === 'sources' ? <SourcesDesign filter={filter} navigate={navigate} /> : page === 'accounts' ? <AccountsDesign filter={filter} scenario={scenario} navigate={navigate} /> : page === 'projects' || page === 'sessions' ? <RecordsDesign key={page} kind={page} filter={filter} navigate={navigate} /> : page === 'pricing' ? <PricingDesign key={model} filter={filter} initialModel={model} navigate={navigate} /> : <DevicesDesign scenario={scenario} />;
  return <Layout className="ds-studio">
    <Layout.Sider theme="light" width={208} className="ds-sidebar"><div className="ds-brand"><BarChartOutlined /><strong>Codex Pulse</strong></div><div className="ds-brand-caption">多机用量中心</div>{menu}<div className="ds-sidebar-bottom"><Badge status="success" />本地采集 · 中心汇总</div></Layout.Sider>
    <Layout className="ds-workspace"><Layout.Header className="ds-topbar"><div><Button className="ds-mobile-menu" type="text" aria-label="打开导航" icon={<MenuOutlined />} onClick={() => setMenuOpen(true)} /><Breadcrumb items={[{ title: '用量中心' }, { title: pageNames[page] }]} /></div><Tag className="ds-design-tag">设计预览 · 合成数据</Tag></Layout.Header>
      <Layout.Content className="ds-content">{page === 'signin' ? <SignInDesign navigate={navigate} /> : <><div className="ds-page-heading"><div><h1>{pageNames[page]}</h1></div>{page === 'overview' && <Button type="link" onClick={() => navigate('accounts')}>查看账号额度与节奏</Button>}</div>{page !== 'devices' && <DesignToolbar page={page} value={filter} onChange={setFilter} onRefresh={refresh} />}{body}</>}</Layout.Content>
      <Layout.Footer className="ds-footer"><span>Codex Pulse</span><span>交互设计稿 · 不连接真实账号</span></Layout.Footer>
    </Layout>
    <Drawer title="Codex Pulse" placement="left" size={260} open={menuOpen} onClose={() => setMenuOpen(false)}>{menu}</Drawer>
  </Layout>;
}
