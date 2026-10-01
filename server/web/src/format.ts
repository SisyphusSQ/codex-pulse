import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';
import 'dayjs/locale/zh-cn';

dayjs.extend(utc);dayjs.extend(timezone);dayjs.locale('zh-cn');
export { dayjs };

export const providerNames: Record<string,string> = { codex: 'Codex', cursor: 'Cursor', grok: 'Grok' };
export function integer(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return '未知';
  return new Intl.NumberFormat('zh-CN').format(typeof value === 'number' ? value : BigInt(value));
}
export function dollars(value: string | null | undefined): string {
  if (value === null || value === undefined) return '未知';
  const n=BigInt(value), magnitude=n<0n?-n:n;
  const whole=integer((magnitude/1_000_000n).toString());
  const tail=(magnitude%1_000_000n).toString().padStart(6,'0').replace(/0+$/,'').padEnd(2,'0');
  return `${n<0n?'-':''}$${whole}.${tail}`;
}
export function dateTime(value: number | null | undefined, zone='Asia/Shanghai'): string {
  if (value === null || value === undefined) return '尚无观测';
  return new Intl.DateTimeFormat('zh-CN',{ dateStyle:'medium',timeStyle:'short',timeZone:zone }).format(value);
}
// 浮点数只用于坐标，标签与 tooltip 仍读原始十进制字符串。
export function coordinate(value: string | null): number | null { return value===null?null:Number(value); }
