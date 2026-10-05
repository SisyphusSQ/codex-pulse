import { useCallback, useEffect, useRef } from 'react';
import { isCancelledError, useQuery, useQueryClient, type Query, type QueryClient, type QueryFilters, type QueryKey, type UseQueryOptions } from '@tanstack/react-query';
import { ApiError } from '../api/client';
import { useOperationNotifications } from './OperationNotifications';

const episodes = new WeakMap<QueryClient, Set<string>>();
const names: Record<string, string> = {
  usage: '模型用量', catalog: '价格目录', clients: '客户端授权', devices: '设备与采集来源',
  'quota-accounts': '账号筛选选项', quotas: '账号额度', pace: '节奏评估', subscription: '订阅设置',
};
const statisticsNames: Record<string, string> = {
  annual: '全年活动', totals: '范围用量', providers: '平台明细', models: '模型明细',
  activity: '活动分布', 'top-sessions': '高消耗会话', 'source-usage': '各机器采集用量',
};
export function querySubject(query: Query): string {
  const [kind, view] = query.queryKey;
  if (kind === 'statistics') return statisticsNames[String(view)] ?? '用量统计';
  if (kind === 'projects' || kind === 'sessions') return `${kind === 'projects' ? '项目' : '会话'}${view === 'detail' ? '详情' : '列表'}`;
  return names[String(kind)] ?? '中心数据';
}
export function queryFailure(query: Query): string | undefined {
  if (query.queryKey[0] === 'runtime-version') return undefined;
  const error = query.state.error;
  if (isCancelledError(error) || error instanceof DOMException && error.name === 'AbortError' || error instanceof ApiError && error.status === 401) return undefined;
  if (error) return error.message;
  const data = query.state.data as { cache?: { state?: string } } | undefined;
  if (data?.cache?.state === 'refresh_failed') return '中心后台更新失败，当前保留上次成功结果与原时间。';
  return undefined;
}
function prepareRefresh(client: QueryClient, filters: QueryFilters) {
  for (const query of client.getQueryCache().findAll(filters)) episodes.get(client)?.delete(query.queryHash);
}
export function refreshQueriesWithFeedback(client: QueryClient, filters: QueryFilters) {
  prepareRefresh(client, filters);
  return client.refetchQueries(filters);
}

// 仅包装明确的 refetch，自动轮询继续使用 React Query 原有调度。
export function useFeedbackQuery<TQueryFnData, TError = Error, TData = TQueryFnData, TQueryKey extends QueryKey = QueryKey>(options: UseQueryOptions<TQueryFnData, TError, TData, TQueryKey>) {
  const client = useQueryClient();
  const result = useQuery(options);
  const refetch = useCallback<typeof result.refetch>((refetchOptions) => {
    prepareRefresh(client, { queryKey: options.queryKey, exact: true });
    return result.refetch(refetchOptions);
  }, [client, options.queryKey, result.refetch]);
  return { ...result, refetch };
}

export function QueryNotifications() {
  const client = useQueryClient();
  const notify = useOperationNotifications();
  const feedback = useRef(notify);
  useEffect(() => { feedback.current = notify; }, [notify]);
  useEffect(() => {
    const faults = new Set<string>();
    episodes.set(client, faults);
    const key = (query: Query) => `query:${query.queryHash}`;
    function reconcile(query: Query) {
      const failure = query.isActive() ? queryFailure(query) : undefined;
      if (!failure) {
        faults.delete(query.queryHash);
        feedback.current.close(key(query));
        return;
      }
      if (faults.has(query.queryHash)) return;
      faults.add(query.queryHash);
      feedback.current.error(`${querySubject(query)}读取失败`, failure, () => {
        void refreshQueriesWithFeedback(client, { queryKey: query.queryKey, exact: true, type: 'active' });
      }, key(query));
    }
    const unsubscribe = client.getQueryCache().subscribe(event => {
      if (event.type === 'removed') {
        faults.delete(event.query.queryHash);
        feedback.current.close(key(event.query));
      } else if (event.type === 'observerAdded' || event.type === 'observerRemoved' || event.type === 'updated' && ['error', 'success'].includes(event.action.type)) {
        reconcile(event.query);
      }
    });
    client.getQueryCache().getAll().forEach(reconcile);
    return () => {
      unsubscribe();
      faults.forEach(hash => feedback.current.close(`query:${hash}`));
      episodes.delete(client);
    };
  }, [client]);
  return null;
}
