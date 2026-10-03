import { useQuery } from '@tanstack/react-query';
import { Typography } from 'antd';
import { api, ApiError } from '../api/client';

interface Version { version:string; commit:string; built_at:string; reporting_protocol:number; throughput_capsule:number; schema:number }
export function RuntimeInfo() {
  const query=useQuery({queryKey:['runtime-version'],queryFn:async({signal})=>{
    const data=await api.get<Version>('/api/v1/version',{},signal);
    if (!data || typeof data.version!=='string' || typeof data.commit!=='string' || !Number.isSafeInteger(data.reporting_protocol) || !Number.isSafeInteger(data.throughput_capsule) || !Number.isSafeInteger(data.schema)) throw new ApiError(502);
    return data;
  },staleTime:Infinity});
  if (!query.data) return <Typography.Text type="secondary"> · {query.isError?'版本信息未读取':'正在读取版本…'}</Typography.Text>;
  return <Typography.Text type="secondary"> · 中心 {query.data.version}（{query.data.commit}） · 上报协议 {query.data.reporting_protocol} / TPS {query.data.throughput_capsule} / 结构 {query.data.schema}</Typography.Text>;
}
