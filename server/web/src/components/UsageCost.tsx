import { Tooltip } from 'antd';
import type { Decimal } from '../api/statistics';
import { usageDollars } from './usageDisplay';

export function UsageCost({ value, status }: { value: Decimal; status?: string }) {
  const reason = value === null ? '缺少价格或计费数据' : status === 'partial' ? '部分记录未定价' : undefined;
  return <Tooltip title={reason} trigger={['hover', 'focus']}><span className="numeric" tabIndex={reason ? 0 : undefined}>{usageDollars(value)}</span></Tooltip>;
}
