import { useSyncExternalStore } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { StatisticsCache } from '../api/statistics';

// 只汇总当前页面活跃查询的后台状态，历史筛选缓存不影响提示。
export function StatisticsCacheFooter() {
  const queries = useQueryClient().getQueryCache();
  const state = useSyncExternalStore(
    notify => queries.subscribe(notify),
    () => {
      const caches = queries.findAll({ type: 'active' }).map(query => (query.state.data as { cache?: StatisticsCache } | undefined)?.cache);
      if (caches.some(cache => cache?.state === 'refresh_failed')) return 'failed';
      if (caches.some(cache => cache?.stale)) return 'stale';
      return 'ready';
    },
  );
  return <span className="statistics-cache-footer" role={state === 'ready' ? undefined : 'status'}>
    用量统计每分钟后台更新，可能延迟 1–2 分钟。
    {state === 'failed' && ' 部分统计后台更新失败，当前保留上次成功结果。'}
    {state === 'stale' && ' 部分统计更新延迟，当前显示缓存数据。'}
  </span>;
}
