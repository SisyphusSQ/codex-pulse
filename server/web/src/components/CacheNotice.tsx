import { Alert } from 'antd';
import type { StatisticsCache } from '../api/statistics';
import { dayjs } from '../format';

export function CacheNotice({cache,zone}:{cache?:StatisticsCache;zone:string}) {
  if (!cache) return null;
  const at=dayjs(cache.computed_at_ms).tz(zone).format('MM-DD HH:mm:ss');
  return <div aria-live="polite">
    <p className="metric-note">统计截至 {at} · 每分钟后台更新{cache.state==='refreshing'?' · 更新中':''}</p>
    {(cache.stale||cache.state==='refresh_failed')&&<Alert showIcon type="warning" title={cache.state==='refresh_failed'?'后台统计更新失败，保留上次成功结果':'统计结果已超过两分钟，正在等待后台更新'} description={`当前显示 ${at} 的计算结果。`}/>}
  </div>;
}
