import { Collapse, Table, Typography, type TableColumnsType } from 'antd';
import { accountModelUsageQueryKey, getAccountModelUsage, type AccountModelRow, type QuotaWindow } from '../api/quotas';
import { dateTime, integer, tokens } from '../format';
import { useFeedbackQuery } from './QueryNotifications';
import { ErrorState, LoadingState } from './QueryState';
import './accountModelUsage.css';

interface AccountModelUsageProps {
  accountKey: string;
  window: QuotaWindow;
  clientID: string;
}

function tokenCell(value: string | null) {
  if (value === null) return <Typography.Text type="secondary" aria-label="未知">—</Typography.Text>;
  return <span className="model-usage-number" title={integer(value)}>{tokens(value)}</span>;
}

function modelName(row: AccountModelRow) {
  if (row.kind === 'unmatched') return '明细未匹配';
  return row.name ?? row.model ?? '模型未识别';
}

const columns: TableColumnsType<AccountModelRow> = [
  {title:'模型',width:154,render:(_,row)=>modelName(row)},
  {title:'总 Token',dataIndex:'total_tokens',width:100,align:'right',render:value=><strong>{tokenCell(value)}</strong>},
  {title:'输入',dataIndex:'input_tokens',width:90,align:'right',render:tokenCell},
  {title:'缓存输入',dataIndex:'cached_tokens',width:98,align:'right',render:tokenCell},
  {title:'输出',dataIndex:'output_tokens',width:90,align:'right',render:tokenCell},
  {title:'推理',dataIndex:'reasoning_tokens',width:90,align:'right',render:tokenCell},
];

function ModelUsageDetails({accountKey, window, clientID}: AccountModelUsageProps) {
  const query = useFeedbackQuery({
    queryKey: [...accountModelUsageQueryKey, accountKey, window.key, clientID, window.current.window_start_at_ms, window.current.resets_at_ms],
    queryFn: ({signal}) => getAccountModelUsage(accountKey, window.key, clientID, signal),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  });
  if (query.isPending) return <LoadingState label="正在读取模型用量…" />;
  if (query.error && !query.data) return <ErrorState error={query.error} retry={()=>void query.refetch()} />;
  const usage = query.data;
  if (!usage || usage.recorded_tokens === null) return <div className="quiet-empty" role="status">本周期模型用量未知</div>;
  if (!usage.models.length) return <div className="quiet-empty" role="status">本周期暂无已记录用量</div>;
  return <>
    <div className="metric-note model-usage-period">{dateTime(usage.window_start_at_ms)} 至 {dateTime(usage.resets_at_ms)}</div>
    <div className="metric-note model-usage-scroll-hint">横向滑动查看 Token 分项</div>
    <Table<AccountModelRow>
      size="small"
      rowKey={row=>row.model ?? `:${row.kind}`}
      columns={columns}
      dataSource={usage.models}
      pagination={false}
      scroll={{x: 622}}
      summary={()=><Table.Summary.Row>
        <Table.Summary.Cell index={0}><strong>合计</strong></Table.Summary.Cell>
        <Table.Summary.Cell index={1} align="right"><strong>{tokenCell(usage.recorded_tokens)}</strong></Table.Summary.Cell>
        <Table.Summary.Cell index={2} colSpan={4} />
      </Table.Summary.Row>}
    />
    <div className="metric-note model-usage-caption">缓存输入已包含在输入 Token 中。</div>
  </>;
}

export function AccountModelUsage(props: AccountModelUsageProps) {
  return <Collapse className="account-model-usage" destroyOnHidden expandIconPlacement="end" items={[{
    key: 'usage',
    label: <Typography.Text strong>本周期模型用量</Typography.Text>,
    children: <ModelUsageDetails {...props} />,
  }]} />;
}
