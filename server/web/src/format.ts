import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';
import 'dayjs/locale/zh-cn';

dayjs.extend(utc);dayjs.extend(timezone);dayjs.locale('zh-cn');
export { dayjs };

export const providerNames: Record<string,string> = { codex: 'Codex', cursor: 'Cursor', grok: 'Grok', dsh: 'DSH' };
export function integer(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return '未知';
  return new Intl.NumberFormat('zh-CN').format(typeof value === 'number' ? value : BigInt(value));
}
// 与中文 Mac 客户端一致：万/亿，最多一位小数；进位可跨单位，计算保留 BigInt 精度。
export function tokens(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return '未知';
  const n = BigInt(value);
  if (n < 0n) return '未知';
  if (n < 10_000n) return integer(value);
  let divisor = n < 100_000_000n ? 10_000n : 100_000_000n;
  let tenths = (n * 10n + divisor / 2n) / divisor;
  if (divisor === 10_000n && tenths >= 100_000n) {
    divisor = 100_000_000n;
  }
  // Mac 的单位晋升用 rounded()，最终 NumberFormatter 使用 half-even。
  const scaled = n * 10n, whole = scaled / divisor, remainder = scaled % divisor;
  tenths = whole + (remainder * 2n > divisor || (remainder * 2n === divisor && whole % 2n !== 0n) ? 1n : 0n);
  return `${tenths / 10n}${tenths % 10n ? `.${tenths % 10n}` : ''}${divisor === 10_000n ? '万' : '亿'}`;
}
// 轴坐标本来就是浮点近似；真实标签和 tooltip 读取十进制字符串。
export const tokenAxis = (value: number) => tokens(Math.round(value));
export const dollarAxis = (value: number) => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value);
export function dollars(value: string | null | undefined): string {
  if (value === null || value === undefined) return '未知';
  const n=BigInt(value), magnitude=n<0n?-n:n;
  const cents=(magnitude+5_000n)/10_000n;
  return `${n<0n&&cents>0n?'-':''}$${integer((cents/100n).toString())}.${(cents%100n).toString().padStart(2,'0')}`;
}
export function dateTime(value: number | null | undefined, zone='Asia/Shanghai'): string {
  if (value === null || value === undefined) return '尚无观测';
  if (Number.isNaN(new Date(value).getTime())) return '时间超出显示范围';
  return new Intl.DateTimeFormat('zh-CN',{ dateStyle:'medium',timeStyle:'short',timeZone:zone }).format(value);
}
// 浮点数只用于坐标，标签与 tooltip 仍读原始十进制字符串。
export function coordinate(value: string | null): number | null { return value===null?null:Number(value); }
