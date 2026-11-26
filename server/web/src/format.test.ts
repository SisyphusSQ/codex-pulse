import { describe, expect, it } from 'vitest';
import {
  dollarAxis,
  dollars,
  providerColors,
  providerLabel,
  providerOptions,
  providerOrder,
  providerTagColor,
  quotaLimitLabel,
  quotaWindowShortLabel,
  sessionRoleTag,
  sourceKindLabel,
  tokens,
} from './format';

describe('Mac display units',()=>{
  it('uses Chinese ten-thousand and hundred-million units with one decimal and unit promotion',()=>{
    expect(tokens('57925841')).toBe('5792.6万');
    expect(tokens('790000000')).toBe('7.9亿');
    expect(tokens('10000')).toBe('1万');
    expect(tokens('12500')).toBe('1.2万');expect(tokens('13500')).toBe('1.4万');
    expect(tokens('99999999')).toBe('1亿');
    expect(tokens('1000')).toBe('1,000');
    expect(tokens('0')).toBe('0');expect(tokens(null)).toBe('未知');
    expect(tokens('9007199254740993')).toBe('90071992.5亿');
  });
  it('rounds signed micro-USD using integers, preserves cents and avoids negative display zero',()=>{
    expect(dollars('24011668')).toBe('$24.01');
    expect(dollars('123455000')).toBe('$123.46');
    expect(dollars('-123455000')).toBe('-$123.46');
    expect(dollars('999999')).toBe('$1.00');
    expect(dollars('0')).toBe('$0.00');expect(dollars(null)).toBe('未知');
    expect(dollars('9007199254740993')).toBe('$9,007,199,254.74');
    expect(dollarAxis(0.5)).toBe('$0.50');
  });
});

describe('AGY provider, source, quota pool and role formatting', () => {
  it('preserves 5-way provider order and stable Antigravity styling', () => {
    expect([...providerOrder]).toEqual(['codex', 'cursor', 'grok', 'dsh', 'agy']);
    expect(providerOptions().map(o => o.value)).toEqual(['', 'codex', 'cursor', 'grok', 'dsh', 'agy']);
    expect(providerOptions().map(o => o.label)).toEqual(['全部平台', 'Codex', 'Cursor', 'Grok', 'DSH', 'Antigravity']);
    expect(providerLabel('agy')).toBe('Antigravity');
    expect(providerColors.agy).toMatch(/^#[0-9a-f]{6}$/i);
    expect(providerTagColor('agy')).toBe('purple');
  });

  it('maps all AGY local and quota source identifiers without leaking raw code keys', () => {
    expect(sourceKindLabel('agy_ls_rpc')).toBe('Antigravity 本机 LS RPC');
    expect(sourceKindLabel('agy_local')).toBe('Antigravity 本地轨迹');
    expect(sourceKindLabel('agy.summaries')).toBe('Antigravity 会话摘要库');
    expect(sourceKindLabel('agy.trajectories')).toBe('Antigravity 本地轨迹库');
    expect(sourceKindLabel('agy.transcripts')).toBe('Antigravity Transcript 回退');
    expect(sourceKindLabel('agy.ls_rpc')).toBe('Antigravity 本机 LS RPC');
  });

  it('maps AGY quota limit IDs and dynamic pool slugs cleanly', () => {
    expect(quotaLimitLabel('agy.gemini')).toBe('Gemini');
    expect(quotaLimitLabel('agy.argon')).toBe('Gemini 4 Argon');
    expect(quotaLimitLabel('agy.carbon')).toBe('Gemini 4 Carbon');
    expect(quotaLimitLabel('agy.claude_gpt')).toBe('Claude & GPT');
    expect(quotaLimitLabel('agy.pool.gemini_ultra')).toBe('Gemini Ultra');
    expect(quotaLimitLabel('agy.pool.openai_gpt')).toBe('Openai GPT');
    expect(quotaWindowShortLabel('primary', 300)).toBe('5h');
    expect(quotaWindowShortLabel('secondary', 10080)).toBe('周');
  });

  it('maps lightweight session role tags only for non-main roles', () => {
    expect(sessionRoleTag('subagent')).toEqual({ label: '子代理', color: 'geekblue' });
    expect(sessionRoleTag('swarm_worker')).toEqual({ label: 'Swarm 成员', color: 'purple' });
    expect(sessionRoleTag('side_question')).toEqual({ label: '侧边提问', color: 'cyan' });
    expect(sessionRoleTag('main')).toBeNull();
    expect(sessionRoleTag(null)).toBeNull();
  });
});
