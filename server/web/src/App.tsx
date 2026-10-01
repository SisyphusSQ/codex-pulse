import { lazy, Suspense, useState } from 'react';
import { Alert, Button, Layout, Menu, Result, Typography } from 'antd';
import { HashRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ApiError } from './api/client';
import { SessionProvider, useSession } from './auth/SessionProvider';
import { SignIn } from './auth/SignIn';
import { ErrorState, LoadingState } from './components/QueryState';

const Overview=lazy(()=>import('./pages/Overview'));

export function createQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { staleTime: 30_000, retry: false, refetchOnWindowFocus: true, gcTime: 300_000 }, mutations: { retry: false } } });
}
const navigation = [
  { key: '/', label: '用量总览' },
  { key: '/projects', label: '项目' },
  { key: '/sessions', label: '会话' },
  { key: '/quota', label: '额度与节奏' },
  { key: '/devices', label: '设备与授权' },
];

function Shell() {
  const navigate = useNavigate();
  const location = useLocation();
  const { session, logout } = useSession();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const selected = navigation.find((item) => item.key !== '/' && location.pathname.startsWith(item.key))?.key ?? '/';

  async function leave() {
    setBusy(true);setError(undefined);
    try { await logout(); } catch (cause) { setError(cause instanceof ApiError ? cause.message : '退出未完成，请稍后重试。'); }
    finally { setBusy(false); }
  }

  return <Layout className="app-layout">
    <Layout.Header className="app-header">
      <div className="header-top"><a className="brand" href="#/" aria-label="Codex Pulse 首页"><span className="brand-mark" aria-hidden="true">⌁</span>Codex Pulse<span className="brand-subtitle">多机中心</span></a>
        <div className="header-account"><Typography.Text type="secondary">{session?.name}</Typography.Text><Button aria-label="退出授权" aria-busy={busy} onClick={leave} loading={busy}>退出授权</Button></div>
      </div>
      <nav aria-label="主导航"><Menu mode="horizontal" selectedKeys={[selected]} items={navigation} onClick={({ key }) => navigate(key)} /></nav>
    </Layout.Header>
    <Layout.Content className="app-content">
      {error && <Alert type="error" title={error} showIcon className="form-alert" />}
      <Suspense fallback={<LoadingState />}><Routes>
        <Route path="/" element={<Overview />} />
        <Route path="*" element={<Result status="404" title="页面不存在" extra={<Button onClick={() => navigate('/')}>返回总览</Button>} />} />
      </Routes></Suspense>
    </Layout.Content>
    <Layout.Footer className="app-footer">Codex Pulse · 本地采集，自主汇总</Layout.Footer>
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
  return <QueryClientProvider client={queryClient}><SessionProvider><HashRouter><SessionGate /></HashRouter></SessionProvider></QueryClientProvider>;
}
