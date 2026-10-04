import { render, screen, cleanup } from '@testing-library/react';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { describe, it, expect } from 'vitest';
import { StatisticsCacheFooter } from './StatisticsCacheFooter';

function ActiveStatistics() {
  useQuery({queryKey:['statistics','totals'],queryFn:async()=>({}),staleTime:Infinity});
  return <StatisticsCacheFooter/>;
}
describe('page footer statistics status',()=>{
  it('shows one policy without card timestamps and ignores inactive failed filters',()=>{
    const client=new QueryClient();
    client.setQueryData(['statistics','totals'],{cache:{state:'ready',stale:false}});
    client.setQueryData(['statistics','old-filter'],{cache:{state:'refresh_failed',stale:true}});
    render(<QueryClientProvider client={client}><ActiveStatistics/></QueryClientProvider>);
    expect(screen.getByText(/每分钟后台更新/)).not.toHaveTextContent('更新失败');
    expect(screen.queryByText(/统计截至/)).not.toBeInTheDocument();cleanup();
  });
  it('reports failed active refreshes in the shared footer',()=>{
    const client=new QueryClient();
    client.setQueryData(['statistics','totals'],{cache:{state:'refresh_failed',stale:true}});
    render(<QueryClientProvider client={client}><ActiveStatistics/></QueryClientProvider>);
    expect(screen.getByRole('status')).toHaveTextContent('保留上次成功结果');cleanup();
  });
});
