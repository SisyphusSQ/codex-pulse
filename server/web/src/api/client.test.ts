import { describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from './client';
import type { Session } from './client';

const session: Session = { client_id: 'synthetic-browser', name: '测试浏览器', purpose: 'admin', csrf: 'synthetic-csrf', expires_at_ms: null };
const envelope = (data: unknown) => new Response(JSON.stringify({ code: 200, data }), { headers: { 'Content-Type': 'application/json' } });

describe('v1 browser client', () => {
  it('uses same-origin cookies, in-memory CSRF, and keeps decimal strings/null', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(envelope({ total_tokens: '9007199254740993', unknown: null, zero: '0' }));
    const client = new ApiClient(fetcher);
    client.setSession(session);
    const value = await client.request('/api/v1/projects/associate', { body: { project_ids: ['synthetic'] } });
    expect(value).toEqual({ total_tokens: '9007199254740993', unknown: null, zero: '0' });
    expect(fetcher.mock.calls[0][1]).toMatchObject({ method: 'POST', credentials: 'same-origin', redirect: 'error', headers: { 'X-Pulse-CSRF': session.csrf } });
    client.setSession(null);
    fetcher.mockResolvedValueOnce(envelope(session));
    await client.pair('AAAA-BBBB-CCCC-DDDD');
    const pair = fetcher.mock.calls[1][1];
    expect(JSON.parse(pair?.body as string)).toEqual({ code: 'AAAA-BBBB-CCCC-DDDD', mode: 'browser' });
    expect(pair?.headers).not.toHaveProperty('Authorization');
    expect(pair?.headers).not.toHaveProperty('X-Pulse-CSRF');
  });

  it('does not expose server error bodies and notifies revocation', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('private source response', { status: 401 }));
    const client = new ApiClient(fetcher);
    const expire = vi.fn();client.onUnauthorized = expire;
    await expect(client.get('/api/v1/quotas')).rejects.toMatchObject({ status: 401 });
    expect(expire).toHaveBeenCalledOnce();
    fetcher.mockResolvedValueOnce(new Response('private source response', { status: 500 }));
    await expect(client.get('/api/v1/quotas')).rejects.toThrow('中心处理失败');
  });

  it('discards late responses after identity changes even if transport ignores cancellation', async () => {
    let release!: (response: Response) => void;
    const fetcher = vi.fn<typeof fetch>().mockImplementation(() => new Promise((resolve) => { release = resolve; }));
    const client = new ApiClient(fetcher);
    client.setSession(session);
    const pending = client.get('/api/v1/sessions');
    const signal = fetcher.mock.calls[0][1]?.signal;
    client.setSession({ ...session, client_id: 'other-browser' });
    expect(signal?.aborted).toBe(true);
    release(envelope({ title: 'old browser private data' }));
    await expect(pending).rejects.toMatchObject({ status: 401 });
  });

  it('rejects malformed success and external destinations', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('{}', { status: 200 }));
    const client = new ApiClient(fetcher);
    await expect(client.get('/api/v1/session')).rejects.toBeInstanceOf(ApiError);
    await expect(client.get('https://example.invalid/api/v1/session')).rejects.toMatchObject({ status: 400 });
    expect(fetcher).toHaveBeenCalledOnce();
  });
});
