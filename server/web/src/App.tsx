import { lazy, Suspense, useEffect, useState } from 'react';
import { Breadcrumb, Button, Drawer, Grid, Layout, Menu, Result, Typography } from 'antd';
import { BarChartOutlined, DatabaseOutlined, DesktopOutlined, DollarOutlined, FolderOutlined, LineChartOutlined, MenuOutlined, MessageOutlined, TeamOutlined } from '@ant-design/icons';
import { HashRouter, Link, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ApiError } from './api/client';
import { SessionProvider, useSession } from './auth/SessionProvider';
import { SignIn } from './auth/SignIn';
import { ErrorState, LoadingState } from './components/QueryState';
import { OperationNotifications, useOperationNotifications } from './components/OperationNotifications';
import { RuntimeInfo } from './components/RuntimeInfo';

const Overview=lazy(()=>import('./pages/Overview'));
const Projects=lazy(()=>import('./pages/Projects'));
const Sessions=lazy(()=>import('./pages/Sessions'));
const Quota=lazy(()=>import('./pages/Quota'));
const Pricing=lazy(()=>import('./pages/Pricing'));
const Devices=lazy(()=>import('./pages/Devices'));
const Usage=lazy(()=>import('./pages/Usage'));
const Sources=lazy(()=>import('./pages/Sources'));

export function createQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { staleTime: 30_000, retry: false, refetchOnWindowFocus: true, gcTime: 300_000 }, mutations: { retry: false } } });
}
const navigation = [
  { key: '/', label: '用量概览', icon: <BarChartOutlined /> },
  { key: '/usage/models', label: '模型分析', icon: <LineChartOutlined /> },
  { key: '/usage/sources', label: '采集来源', icon: <DatabaseOutlined /> },
  { key: '/quota', label: '账号', icon: <TeamOutlined /> },
  { key: '/projects', label: '项目', icon: <FolderOutlined /> },
  { key: '/sessions', label: '会话', icon: <MessageOutlined /> },
  { key: '/pricing', label: '价目表', icon: <DollarOutlined /> },
  { key: '/devices', label: '设备与授权', icon: <DesktopOutlined /> },
];

function Shell() {
  const navigate = useNavigate();
  const location = useLocation();
  const { session, logout } = useSession();
  const [busy, setBusy] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const screens = Grid.useBreakpoint();
  const mobile = !screens.md;
  const notify=useOperationNotifications();
  const selected = navigation.find(item => item.key !== '/' && location.pathname.startsWith(item.key))?.key ?? '/';
  const title = navigation.find(item=>item.key===selected)?.label;

  useEffect(()=>{window.scrollTo(0,0);},[location.pathname]);

  async function leave() {
    setBusy(true);
    try { await logout(); } catch (cause) { notify.error('退出授权未完成',cause instanceof ApiError ? cause.message : '请稍后重试。',()=>void leave()); }
    finally { setBusy(false); }
  }

  const menu = <nav aria-label="主导航"><Menu mode="inline" selectedKeys={[selected]} items={navigation} onClick={({ key }) => {navigate(key);setMenuOpen(false);}} /></nav>;
  const brand = <a className="brand" href="#/" aria-label="Codex Pulse 首页"><BarChartOutlined /> <span>Codex Pulse</span></a>;
  return <Layout className="app-layout">
    {!mobile && <Layout.Sider width={208} theme="light" className="app-sidebar">{brand}<div className="sidebar-caption">多机用量中心</div>{menu}<div className="sidebar-bottom"><Typography.Text type="secondary">App 采集 · 中心汇总</Typography.Text></div></Layout.Sider>}
    <Layout className="app-workspace">
      <Layout.Header className="app-header">
        <div className="header-location">{mobile && <Button type="text" icon={<MenuOutlined />} aria-label="打开导航" onClick={()=>setMenuOpen(true)} />}<Breadcrumb items={[{title:'用量中心'},{title}]} /></div>
        <div className="header-account"><Typography.Text className="account-name" ellipsis>{session?.name}</Typography.Text><Button type="text" aria-label="退出授权" aria-busy={busy} onClick={leave} loading={busy}>退出授权</Button></div>
      </Layout.Header>
      <Layout.Content className="app-content">
        <div className="workspace-heading"><h1>{title}</h1>{selected==='/'&&<Link to="/quota">查看账号额度与节奏</Link>}{selected==='/quota'&&<Link to="/pricing">模型与订阅价目表</Link>}</div>
        <Suspense fallback={<LoadingState />}><Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/usage/models" element={<Usage />} />
          <Route path="/usage/sources" element={<Sources />} />
          <Route path="/projects" element={<Projects />} />
          <Route path="/sessions" element={<Sessions />} />
          <Route path="/quota" element={<Quota />} />
          <Route path="/pricing" element={<Pricing />} />
          <Route path="/devices" element={<Devices />} />
          <Route path="*" element={<Result status="404" title="页面不存在" extra={<Button onClick={() => navigate('/')}>返回总览</Button>} />} />
        </Routes></Suspense>
      </Layout.Content>
      <Layout.Footer className="app-footer"><span>Codex Pulse · 多机中心</span><RuntimeInfo /></Layout.Footer>
    </Layout>
    {mobile && <Drawer open={menuOpen} placement="left" title="Codex Pulse" size={256} onClose={()=>setMenuOpen(false)}>{menu}</Drawer>}
  </Layout>;
}

function SessionGate() {
  const { status, error, retry } = useSession();
  if (status === 'loading') return <LoadingState label="正在恢复浏览器授权…" />;
  if (status === 'error') return <ErrorState error={error ?? new ApiError(0)} retry={retry} />;
  if (status === 'anonymous') return <SignIn />;
  return <Shell />;
}

export function PulseApp({ queryClient }: { queryClient: QueryClient }) {
  return <OperationNotifications><QueryClientProvider client={queryClient}><SessionProvider><HashRouter><SessionGate /></HashRouter></SessionProvider></QueryClientProvider></OperationNotifications>;
}
