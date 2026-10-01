export interface Session {
  client_id: string;
  name: string;
  purpose: 'admin';
  csrf: string;
  expires_at_ms: number | null;
}

const errors: Record<number, string> = {
  400: '提交的内容或筛选条件无效，请检查后重试。',
  401: '授权已过期或已撤销，请重新配对。',
  403: '当前授权无权执行此操作，请确认配对码用途和访问地址。',
  404: '这条记录不存在，或当前范围内无法访问。',
  409: '数据发生冲突，请刷新后重新确认。',
  413: '当前范围超过处理预算，请缩小筛选范围。',
  426: '客户端与中心协议不兼容，请更新到对应版本。',
  429: '请求过于频繁，请稍后再试。',
  502: '中心返回了无法识别的响应，请检查服务版本。',
  503: '中心暂时不可用，请检查服务状态后重试。',
};

export class ApiError extends Error {
  constructor(readonly status: number) {
    super(errors[status] ?? (status === 0 ? '无法连接中心，请检查网络后重试。' : '中心处理失败，请稍后重试。'));
    this.name = 'ApiError';
  }
}

export class ApiClient {
  private csrf = '';
  private epoch = 0;
  private owner = new AbortController();
  onUnauthorized?: () => void;

  constructor(private readonly fetcher: typeof fetch = (...args) => fetch(...args)) {}

  setSession(session: Session | null) {
    this.owner.abort();
    this.owner = new AbortController();
    this.epoch++;
    this.csrf = session?.csrf ?? '';
  }

  async request<T>(path: string, options: { body?: unknown; signal?: AbortSignal } = {}): Promise<T> {
    if (!path.startsWith('/api/v1/')) throw new ApiError(400);
    const epoch = this.epoch;
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (options.body !== undefined) {
      headers['Content-Type'] = 'application/json';
      if (this.csrf) headers['X-Pulse-CSRF'] = this.csrf;
    }
    const signals = [this.owner.signal, AbortSignal.timeout(15_000)];
    if (options.signal) signals.push(options.signal);
    const signal = AbortSignal.any(signals);
    try {
      const response = await this.fetcher(path, {
        method: options.body === undefined ? 'GET' : 'POST',
        headers,
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        credentials: 'same-origin',
        redirect: 'error',
        cache: 'no-store',
        signal,
      });
      if (epoch !== this.epoch) throw new ApiError(401);
      if (!response.ok) {
        if (response.status === 401) this.onUnauthorized?.();
        throw new ApiError(response.status);
      }
      let envelope: unknown;
      try { envelope = await response.json(); } catch { throw new ApiError(502); }
      if (epoch !== this.epoch) throw new ApiError(401);
      if (!envelope || typeof envelope !== 'object' || !('code' in envelope) || envelope.code !== 200 || !('data' in envelope)) throw new ApiError(502);
      return envelope.data as T;
    } catch (error) {
      if (error instanceof ApiError) throw error;
      if (options.signal?.aborted || this.owner.signal.aborted || epoch !== this.epoch) throw error;
      throw new ApiError(0);
    }
  }

  get<T>(path: string, params: Record<string, string | number | undefined> = {}, signal?: AbortSignal) {
    const values = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) if (value !== undefined && value !== '') values.set(key, String(value));
    return this.request<T>(path + (values.size ? `?${values}` : ''), { signal });
  }

  async session(signal?: AbortSignal) { return readSession(await this.request<unknown>('/api/v1/session', { signal })); }
  async pair(code: string) { return readSession(await this.request<unknown>('/api/v1/pair', { body: { code, mode: 'browser' } })); }
  logout() { return this.request<{ applied: boolean }>('/api/v1/logout', { body: {} }); }
}

function readSession(value: unknown): Session {
  if (!value || typeof value !== 'object') throw new ApiError(502);
  const s = value as Partial<Session>;
  if (typeof s.client_id !== 'string' || typeof s.name !== 'string' || s.purpose !== 'admin' || typeof s.csrf !== 'string' || !s.csrf || !(s.expires_at_ms === null || typeof s.expires_at_ms === 'number' && Number.isSafeInteger(s.expires_at_ms))) throw new ApiError(502);
  return s as Session;
}

export const api = new ApiClient();
