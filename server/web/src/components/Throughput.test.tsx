import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { ThroughputStats } from '../api/records';
import { activeDuration, ThroughputPanel, tps } from './Throughput';

const measured:ThroughputStats={average_output_milli_tps:'22355',output_tokens:'16327',active_duration_ms:'730364',included_turns:'1',excluded_turns:'0',open_turns:'0',unattributed_events:'0',status:'complete',reason:'',duration_source:'duration_ms',basis:'closed_turn_lifetime_output',average_unit:'milli_tokens_per_second',duration_unit:'milliseconds',source_client_id:null,conflict:false};
describe('lifetime throughput presentation',()=>{
 it('formats integer milli TPS exactly with display rounding and zero distinct from unavailable',()=>{
  expect(tps('22355')).toBe('22.36');expect(tps('18182')).toBe('18.18');expect(tps('0')).toBe('0.00');expect(tps(null)).toBe('--');
  expect(tps('9007199254740991')).toBe('9,007,199,254,740.99');expect(activeDuration('730364')).toBe('730.364 秒');
 });
 it('keeps lifetime mean separate from recent truncation and shows partial/source conflict',()=>{
  render(<ThroughputPanel value={{...measured,status:'partial',reason:'source_conflict',conflict:true}} turns={{items:[],total:'61',limit:20,truncated:true}} zone="UTC" onLimit={()=>{}} sourceName="合成来源" />);
  expect(screen.getByLabelText('average-output-tps')).toHaveTextContent(/22\.36\s*TPS/);expect(screen.getByText('来源 TPS 证据不一致')).toBeInTheDocument();expect(screen.getByText(/整体平均使用完整指标/)).toBeInTheDocument();expect(screen.getByText(/61/)).toBeInTheDocument();expect(screen.getByText(/采集来源：合成来源/)).toBeInTheDocument();
 });
 it('does not invent zero coverage for pending indexing or inherited history',()=>{
  render(<ThroughputPanel value={{...measured,average_output_milli_tps:null,output_tokens:null,active_duration_ms:null,included_turns:null,excluded_turns:null,open_turns:null,unattributed_events:null,status:'unavailable',reason:'inherited_history'}} zone="UTC" onLimit={()=>{}} />);
  expect(screen.getByText('--')).toBeInTheDocument();expect(screen.getAllByText('继承历史，无法确认独立 TPS').length).toBeGreaterThan(0);expect(screen.getByText(/参与 未知 轮/)).toBeInTheDocument();expect(screen.queryByText('0.00 TPS')).not.toBeInTheDocument();
 });
});
