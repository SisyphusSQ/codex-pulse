import { createContext, useContext, useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api, ApiError } from '../api/client';
import type { Session } from '../api/client';

interface SessionState {
  session: Session | null;
  status: 'loading' | 'anonymous' | 'authenticated' | 'error';
  error: Error | null;
  signIn(code: string): Promise<void>;
  logout(): Promise<void>;
  retry(): void;
}
const SessionContext = createContext<SessionState | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [session, setSession] = useState<Session | null>(null);
  const [status, setStatus] = useState<SessionState['status']>('loading');
  const [error, setError] = useState<Error | null>(null);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    api.onUnauthorized = () => {
      api.setSession(null);
      queryClient.clear();
      setSession(null);
      setStatus('anonymous');
    };
    api.session(controller.signal).then((value) => {
      if (controller.signal.aborted) return;
      api.setSession(value);
      setSession(value);
      setStatus('authenticated');
      setError(null);
    }).catch((cause: unknown) => {
      if (controller.signal.aborted) return;
      if (cause instanceof ApiError && cause.status === 401) { setStatus('anonymous'); return; }
      setError(cause instanceof ApiError ? cause : new ApiError(0));
      setStatus('error');
    });
    return () => { controller.abort(); api.onUnauthorized = undefined; };
  }, [queryClient, reload]);

  async function signIn(code: string) {
    const value = await api.pair(code);
    api.setSession(value);
    queryClient.clear();
    setSession(value);
    setStatus('authenticated');
    setError(null);
  }

  async function logout() {
    // 网络失败时保留可操作会话，不能把未成功撤销伪装成已退出。
    await api.logout();
    api.setSession(null);
    queryClient.clear();
    setSession(null);
    setStatus('anonymous');
  }

  return <SessionContext.Provider value={{ session, status, error, signIn, logout, retry: () => { setStatus('loading'); setReload((n) => n + 1); } }}>{children}</SessionContext.Provider>;
}

export function useSession() {
  const value = useContext(SessionContext);
  if (!value) throw new Error('SessionProvider is required');
  return value;
}
