import type { ReactNode } from 'react';
import { Button, Empty, Result, Spin } from 'antd';
import { ApiError } from '../api/client';

export function LoadingState({ label = '正在读取中心数据…' }: { label?: string }) {
  return <div className="loading-state" role="status"><Spin /><span>{label}</span></div>;
}
export function ErrorState({ error, retry }: { error: Error; retry?: () => void }) {
  const forbidden = error instanceof ApiError && error.status === 403;
  return <Result status={forbidden ? '403' : 'error'} title={forbidden ? '没有访问权限' : '数据未能读取'} subTitle={error.message} extra={retry && <Button onClick={retry}>重新读取</Button>} />;
}
export function EmptyState({ description = '当前范围没有已收到的记录。' }: { description?: ReactNode }) {
  return <Empty description={description} />;
}
