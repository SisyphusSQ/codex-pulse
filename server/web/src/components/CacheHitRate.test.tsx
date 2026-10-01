import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { CacheHitRateStats } from '../api/records';
import { cacheHitRate, CacheHitRateDetail } from './CacheHitRate';
const value=(basis_points:string|null):CacheHitRateStats=>({basis_points,input_tokens:'1000',cached_input_tokens:'900',unit:'basis_points',basis:'lifetime_cached_input',status:'complete',reason:'',source_client_id:null,conflict:false});
describe('lifetime cache hit rate',()=>{
 it('formats exact zero/full/half rounding and distinguishes unknown',()=>{expect(cacheHitRate(value('0'))).toBe('0.0%');expect(cacheHitRate(value('10000'))).toBe('100.0%');expect(cacheHitRate(value('313'))).toBe('3.1%');expect(cacheHitRate(value('9655'))).toBe('96.6%');expect(cacheHitRate(value(null))).toBe('—');expect(cacheHitRate(value('10001'))).toBe('—');});
 it('shows the lifetime basis and retains partial and missing-history reasons',()=>{render(<CacheHitRateDetail value={{...value(null),status:'partial',reason:'history_filtered'}}/>);expect(screen.getByText(/整段已索引会话/)).toBeInTheDocument();expect(screen.getByText(/未包含整个会话/)).toBeInTheDocument();expect(screen.getByText(/部分索引/)).toBeInTheDocument();});
});
