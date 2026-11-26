import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { createQueryClient } from '../App';
import { api } from '../api/client';
import type { Device } from '../api/statistics';
import { SourceTable } from './SourceTable';
import Sources from '../pages/Sources';

beforeEach(() => api.setSession({ client_id: 'test', name: '测试', purpose: 'admin', csrf: 'synthetic', expires_at_ms: null }));
afterEach(() => vi.unstubAllGlobals());

it('shows actual device evidence with original cutoff, revocation and unknown values without claiming execution usage',()=>{
  const device:Device={id:'synthetic',name:'<img src=x onerror=alert(1)>',revoked_at_ms:1,last_received_at_ms:2,providers:[{provider:'codex',version:'test',collected_at_ms:null,coverage_start_ms:null,coverage_end_ms:null,pending_batches:0,status:'ready',received_at_ms:2,stale:true}]};
  render(<SourceTable devices={[device]} />);
  expect(screen.getByText(device.name)).toBeInTheDocument();expect(document.querySelector('img[src="x"]')).toBeNull();
  expect(screen.getByText('已撤销')).toBeInTheDocument();expect(screen.getByText('陈旧')).toBeInTheDocument();expect(screen.getByText('尚无观测')).toBeInTheDocument();
  expect(screen.queryByText('正常')).not.toBeInTheDocument();expect(screen.queryByText('会话数')).not.toBeInTheDocument();
});

it('displays Antigravity source status, coverage windows and mapped AGY local/LS RPC channels without raw keys', async () => {
  const now = 1795680000000;
  const agyDevice: Device = {
    id: 'agy-mac-1',
    name: 'SQMC05 Studio',
    revoked_at_ms: null,
    last_received_at_ms: now,
    providers: [{
      provider: 'agy',
      version: '0.19.0',
      collected_at_ms: now - 30000,
      coverage_start_ms: now - 86400000,
      coverage_end_ms: now - 30000,
      pending_batches: 0,
      status: 'ready',
      received_at_ms: now,
      stale: false,
      sync_state: 'ready',
      sync_checked_at_ms: now,
    }],
  };
  vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ code: 200, data: [agyDevice] })));
  render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><Sources /></MemoryRouter></QueryClientProvider>);
  expect(await screen.findByText('SQMC05 Studio · 来源详情')).toBeInTheDocument();
  expect(screen.getAllByText('Antigravity').length).toBeGreaterThan(0);
  expect(screen.getByText('Antigravity 本地轨迹 · Antigravity 本机 LS RPC')).toBeInTheDocument();
  expect(document.body.textContent).not.toContain('agy_local');
  expect(document.body.textContent).not.toContain('agy_ls_rpc');
});
