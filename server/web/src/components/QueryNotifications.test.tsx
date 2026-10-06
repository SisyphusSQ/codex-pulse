import { useState } from 'react';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { ConfigProvider } from 'antd';
import { describe, expect, it, vi } from 'vitest';
import { createQueryClient } from '../App';
import { ApiError } from '../api/client';
import { OperationNotifications } from './OperationNotifications';
import { QueryNotifications, useFeedbackQuery } from './QueryNotifications';
import { ErrorState } from './QueryState';
import { StatisticsCacheFooter } from './StatisticsCacheFooter';

type Snapshot = { value: string; time: string; cache?: { state: string; stale: boolean } };
function Widget({ load, scope = 'one' }: { load(): Promise<Snapshot>; scope?: string }) {
  const query = useFeedbackQuery({ queryKey: ['devices', scope], queryFn: load });
  return <section aria-label={`数据 ${scope}`}><button onClick={() => void query.refetch()}>主动重读</button>{query.data ? <span>{query.data.value} · {query.data.time}</span> : query.error ? <ErrorState error={query.error} retry={() => void query.refetch()} /> : '读取中'}</section>;
}
function mount(load: () => Promise<Snapshot>, duplicate = false) {
  const client = createQueryClient();
  render(<ConfigProvider theme={{token:{motion:false}}}><OperationNotifications><QueryClientProvider client={client}><QueryNotifications /><Widget load={load} />{duplicate && <Widget load={load} />}<StatisticsCacheFooter /></QueryClientProvider></OperationNotifications></ConfigProvider>);
  return client;
}
async function backgroundRefresh(client: ReturnType<typeof createQueryClient>) {
  await act(async () => { await client.refetchQueries({ queryKey: ['devices'], type: 'active' }); });
}
describe('query failure feedback', () => {
  it('notifies once for a shared initial failure and leaves neutral retry states, not empty data', async () => {
    const load = vi.fn<() => Promise<Snapshot>>().mockRejectedValue(new ApiError(503));
    const client = mount(load, true);
    await screen.findByText('设备与采集来源读取失败');
    expect(load).toHaveBeenCalledTimes(1);
    expect(screen.getAllByText('数据未能读取')).toHaveLength(2);
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(document.querySelector('.ant-alert')).toBeNull();
    expect(document.querySelector('.ant-notification-topRight')).not.toBeNull();
    expect(document.querySelector('.ant-notification-top')).toBeNull();
    await backgroundRefresh(client);
    expect(screen.getAllByText('设备与采集来源读取失败')).toHaveLength(1);
  });
  it('keeps cached values and their time, does not reopen a dismissed polling fault, and lets manual retry notify again', async () => {
    const load = vi.fn<() => Promise<Snapshot>>().mockResolvedValue({ value: '已知结果', time: '原观测时间' });
    const client = mount(load);
    await screen.findByText('已知结果 · 原观测时间');
    load.mockRejectedValue(new ApiError(503));
    await backgroundRefresh(client);
    await screen.findByText('设备与采集来源读取失败');
    expect(screen.queryByText('数据未能读取')).not.toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(document.querySelector('.ant-notification-notice-close') as HTMLElement);
    await waitFor(() => expect(screen.queryByText('设备与采集来源读取失败')).not.toBeInTheDocument());
    await backgroundRefresh(client);
    expect(screen.queryByText('设备与采集来源读取失败')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '1 项读取失败 · 查看并重试' }));
    expect(await screen.findByText('当前页面读取状态')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '重新读取设备与采集来源' }));
    await screen.findByText('设备与采集来源读取失败');
    expect(screen.getByText('已知结果 · 原观测时间')).toBeInTheDocument();
    load.mockResolvedValue({ value: '新结果', time: '新观测时间' });
    await user.click(screen.getByRole('button', { name: '主动重读' }));
    await screen.findByText('新结果 · 新观测时间');
    await waitFor(() => expect(screen.queryByText('设备与采集来源读取失败')).not.toBeInTheDocument());
    expect(screen.queryByRole('button', { name: /项读取失败/ })).not.toBeInTheDocument();
  });
  it('notifies for a backend cache failure returned with HTTP success and clears it after recovery', async () => {
    const load = vi.fn<() => Promise<Snapshot>>().mockResolvedValue({ value: '快照', time: '原时间', cache: { state: 'refresh_failed', stale: true } });
    const client = mount(load);
    await screen.findByText('设备与采集来源读取失败');
    expect(screen.getByText('快照 · 原时间')).toBeInTheDocument();
    await userEvent.setup().click(document.querySelector('.ant-notification-notice-close') as HTMLElement);
    await backgroundRefresh(client);
    expect(screen.queryByText('设备与采集来源读取失败')).not.toBeInTheDocument();
    load.mockResolvedValue({ value: '恢复结果', time: '更新时间', cache: { state: 'ready', stale: false } });
    await backgroundRefresh(client);
    await screen.findByText('恢复结果 · 更新时间');
    expect(screen.queryByRole('button', { name: /项读取失败/ })).not.toBeInTheDocument();
  });
  it.each([new DOMException('Aborted', 'AbortError'), new ApiError(401)])('does not notify cancellation or an expired authorization', async error => {
    mount(async () => { throw error; });
    await screen.findByText('数据未能读取');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /项读取失败/ })).not.toBeInTheDocument();
  });
  it('does not display a previous filter snapshot when the new filter fails, and dismisses inactive failures', async () => {
    const client = createQueryClient();
    function Filters() {
      const [scope, setScope] = useState('one');
      return <><button onClick={() => setScope(scope === 'one' ? 'two' : 'one')}>切换筛选</button><Widget scope={scope} load={async () => { if (scope === 'two') throw new ApiError(403); return { value: '原筛选结果', time: '原时间' }; }} /></>;
    }
    render(<ConfigProvider theme={{token:{motion:false}}}><OperationNotifications><QueryClientProvider client={client}><QueryNotifications /><Filters /></QueryClientProvider></OperationNotifications></ConfigProvider>);
    await screen.findByText('原筛选结果 · 原时间');
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '切换筛选' }));
    const next = within(await screen.findByRole('region', { name: '数据 two' }));
    expect(await next.findByText('没有访问权限')).toBeInTheDocument();
    expect(next.queryByText('原筛选结果 · 原时间')).not.toBeInTheDocument();
    await screen.findByText('设备与采集来源读取失败');
    await user.click(screen.getByRole('button', { name: '切换筛选' }));
    await screen.findByText('原筛选结果 · 原时间');
    await waitFor(() => expect(screen.queryByText('设备与采集来源读取失败')).not.toBeInTheDocument());
  });
});
