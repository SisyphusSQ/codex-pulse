import { describe, expect, it } from 'vitest';
import { dollarAxis, dollars, tokens } from './format';

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
