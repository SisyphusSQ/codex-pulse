import {render,screen,cleanup} from '@testing-library/react';
import {describe,it,expect} from 'vitest';
import {CacheNotice} from './CacheNotice';

describe('Server statistics freshness',()=>{
  it('keeps collection freshness separate and exposes stale or failed refreshes',()=>{
    const cache={computed_at_ms:1791043200000,refresh_after_ms:60000,age_ms:1000,stale:false,state:'ready' as const};
    render(<CacheNotice cache={cache} zone="UTC"/>);
    expect(screen.getByText(/统计截至/)).toHaveTextContent('每分钟后台更新');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();cleanup();
    render(<CacheNotice cache={{...cache,stale:true,state:'refresh_failed'}} zone="UTC"/>);
    expect(screen.getByRole('alert')).toHaveTextContent('后台统计更新失败，保留上次成功结果');
  });
});
