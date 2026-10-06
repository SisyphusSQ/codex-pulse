import type { ReactNode } from 'react';
import { Button, Empty, Spin } from 'antd';
import { ApiError } from '../api/client';

export function LoadingState({ label = '正在读取中心数据…' }: { label?: string }) {
  return <div className="loading-state" role="status"><Spin /><span>{label}</span></div>;
}
export function ErrorState({ error, retry }: { error: Error; retry?: () => void }) {
  const forbidden = error instanceof ApiError && error.status === 403;
  return <div className="query-unavailable" role="status"><strong>{forbidden ? '没有访问权限' : '数据未能读取'}</strong><span className="metric-note">{error.message}</span>{retry && <Button size="small" onClick={retry}>重新读取</Button>}</div>;
}
export function EmptyState({ description = '当前范围没有已收到的记录。' }: { description?: ReactNode }) {
  return <Empty description={description} />;
}
