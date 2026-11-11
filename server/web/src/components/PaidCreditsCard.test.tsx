import {render,screen} from '@testing-library/react';
import {describe,it,expect} from 'vitest';
import {creditAmount,PaidCreditsCard} from './PaidCreditsCard';

describe('official credit balance',()=>{
 it('formats exact decimals without converting to floating point',()=>{
  expect(creditAmount('62500')).toBe('62,500');
  expect(creditAmount('9007199254740993.000000000000000001')).toBe('9,007,199,254,740,993.000000000000000001');
  expect(creditAmount('0')).toBe('0');
  expect(creditAmount()).toBe('—');
  expect(creditAmount(undefined,true)).toBe('无限额');
 });
 it('never calls a falling balance actual consumption',()=>{
  render(<PaidCreditsCard value={{account_key:'a',credit:{limitId:'codex',state:'known',balance:'62500',observedAtMs:1,freshness:'fresh',checkedAtMs:1,consumptionState:'source_unavailable',history:[]}}}/>);
  expect(screen.getByText('62,500')).toBeInTheDocument();
  expect(screen.getByText('官方暂未提供实际消耗统计，余额变化不等于实际扣费。')).toBeInTheDocument();
  expect(screen.getAllByText('—')).toHaveLength(3);
 });
});
