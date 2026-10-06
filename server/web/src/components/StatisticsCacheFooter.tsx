import { useSyncExternalStore } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Button, Popover } from 'antd';
import type { StatisticsCache } from '../api/statistics';
import { queryFailure, querySubject, refreshQueriesWithFeedback } from './QueryNotifications';

// 只汇总当前页面活跃查询的后台状态，历史筛选缓存不影响提示。
export function StatisticsCacheFooter() {
  const client = useQueryClient();
  const queries = client.getQueryCache();
  const state = useSyncExternalStore(
    notify => queries.subscribe(notify),
    () => {
      const active = queries.findAll({ type: 'active' });
      const caches = active.map(query => (query.state.data as { cache?: StatisticsCache } | undefined)?.cache);
      const background = caches.some(cache => cache?.state === 'refresh_failed') ? 'failed' : caches.some(cache => cache?.stale) ? 'stale' : 'ready';
      return JSON.stringify({ background, failures: active.filter(query => queryFailure(query)).map(query => query.queryHash), reasons: active.map(query => queryFailure(query)) });
    },
  );
  const { background, failures } = JSON.parse(state) as { background: string; failures: string[] };
  const failed = queries.findAll({ type: 'active' }).filter(query => failures.includes(query.queryHash));
  return <span className="statistics-cache-footer" role={background === 'ready' && !failed.length ? undefined : 'status'}>
    用量统计每分钟后台更新，可能延迟 1–2 分钟。
    {background === 'failed' && ' 部分统计后台更新失败，当前保留上次成功结果。'}
    {background === 'stale' && ' 部分统计更新延迟，当前显示缓存数据。'}
    {failed.length > 0 && <Popover trigger="click" title="当前页面读取状态" content={<div className="query-failure-details">{failed.map(query => <div key={query.queryHash}><strong>{querySubject(query)}</strong><p>{queryFailure(query)}</p><Button size="small" onClick={() => void refreshQueriesWithFeedback(client, { queryKey: query.queryKey, exact: true, type: 'active' })}>重新读取{querySubject(query)}</Button></div>)}</div>}><Button type="link" size="small">{failed.length} 项读取失败 · 查看并重试</Button></Popover>}
  </span>;
}
