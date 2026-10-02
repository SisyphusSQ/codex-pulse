import { Alert, Button, Card, Typography } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getDevices } from '../api/statistics';
import { SourceTable } from '../components/SourceTable';
import { ErrorState, LoadingState } from '../components/QueryState';

export default function Sources() {
  const query=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal)});
  return <section><div className="page-heading"><Typography.Paragraph type="secondary">设备采集与传输证据；同一会话可以来自多个设备。</Typography.Paragraph><Button icon={<ReloadOutlined />} loading={query.isFetching} onClick={()=>void query.refetch()}>刷新来源</Button></div>
    {query.isPending?<LoadingState />:query.error&&!query.data?<ErrorState error={query.error} retry={()=>void query.refetch()} />:query.data&&<>
      {query.error&&<Alert showIcon type="warning" title="来源更新失败，保留上次证据与原时间" description={query.error.message} className="form-alert" />}
      <Card title="采集来源" extra={<Link to="/devices">管理设备与授权</Link>}><SourceTable devices={query.data} /><div className="metric-note source-footnote">近期证据不证明全部历史已上传。采集来源不代表执行设备，不按设备副本累加消耗；覆盖范围为设备上报证据。</div></Card>
    </>}
  </section>;
}
