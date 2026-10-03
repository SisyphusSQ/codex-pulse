import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createQueryClient, PulseApp } from '../App';
import { api } from '../api/client';

const syntheticSession = { client_id: 'synthetic-browser', name: '<script>synthetic name</script>', purpose: 'admin', csrf: 'synthetic-csrf', expires_at_ms: null };
const success = (data: unknown) => new Response(JSON.stringify({ code: 200, data }));
const fetcher = vi.fn<typeof fetch>();

beforeEach(() => { api.setSession(null); window.location.hash = '#/'; fetcher.mockReset();vi.stubGlobal('fetch', fetcher); });
afterEach(() => { vi.unstubAllGlobals(); });

describe('browser authorization', () => {
  it('pairs once, clears the code, uses real empty data, and revokes on logout', async () => {
    fetcher.mockImplementation(async (path) => {
      if (path === '/api/v1/session') return new Response('', { status: 401 });
      if (path === '/api/v1/pair') return success(syntheticSession);
      if (path === '/api/v1/logout') return success({ applied: true });
      return success([]);
    });
    const client = createQueryClient();
    render(<PulseApp queryClient={client} />);
    const code = await screen.findByLabelText('浏览器配对码');
    const user = userEvent.setup();
    await user.type(code, 'AAAA-BBBB-CCCC-DDDD');
    await user.click(screen.getByRole('button', { name: '配对并进入' }));
    await screen.findByRole('button', { name: '退出授权' });
    expect(screen.getByText(syntheticSession.name)).toBeInTheDocument();
    expect(document.querySelector('script')).toBeNull();
    expect(fetcher.mock.calls.filter(([path]) => path === '/api/v1/pair')).toHaveLength(1);
    client.setQueryData(['private-data'], { title: 'synthetic session title' });
    await user.click(screen.getByRole('button', { name: '退出授权' }));
    expect(await screen.findByLabelText('浏览器配对码')).toHaveValue('');
    expect(client.getQueryData(['private-data'])).toBeUndefined();
    const logout = fetcher.mock.calls.find(([path]) => path === '/api/v1/logout');
    expect(logout?.[1]?.headers).toHaveProperty('X-Pulse-CSRF', syntheticSession.csrf);
    expect(localStorage.length).toBe(0);expect(sessionStorage.length).toBe(0);
  });

  it('restores a session, then clears all data when the server revokes it', async () => {
    fetcher.mockImplementation(async (path) => path === '/api/v1/session' ? success(syntheticSession) : new Response('', { status: 401 }));
    const client = createQueryClient();client.setQueryData(['private-data'], { name: 'synthetic private project' });
    render(<PulseApp queryClient={client} />);
    await screen.findByLabelText('浏览器配对码');
    expect(client.getQueryData(['private-data'])).toBeUndefined();
    expect(screen.queryByText('多机用量中心')).not.toBeInTheDocument();
  });

  it('keeps network failure distinct from signed-out and supports retry', async () => {
    fetcher.mockRejectedValueOnce(new TypeError('network failure')).mockResolvedValue(new Response('', { status: 401 }));
    render(<PulseApp queryClient={createQueryClient()} />);
    await screen.findByText('数据未能读取');
    expect(screen.queryByLabelText('浏览器配对码')).not.toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole('button', { name: '重新读取' }));
    await screen.findByLabelText('浏览器配对码');
  });

  it('does not claim logout succeeded when the request failed', async () => {
    fetcher.mockImplementation(async (path) => path === '/api/v1/session' ? success(syntheticSession) : path === '/api/v1/logout' ? new Response('', { status: 503 }) : success([]));
    render(<PulseApp queryClient={createQueryClient()} />);
    await screen.findByRole('button', { name: '退出授权' });
    await userEvent.setup().click(screen.getByRole('button', { name: '退出授权' }));
    await screen.findByText('中心暂时不可用，请检查服务状态后重试。');
    await waitFor(() => expect(screen.getByRole('button', { name: '退出授权' })).toBeEnabled());
    expect(screen.queryByLabelText('浏览器配对码')).not.toBeInTheDocument();
  });
});
