import {useState,type ReactNode} from 'react';
import {Alert,Button,Collapse,Select,Tabs,Typography} from 'antd';
import {ReloadOutlined} from '@ant-design/icons';
import {useQuery} from '@tanstack/react-query';
import {useSearchParams} from 'react-router-dom';
import {getUsage} from '../api/usage';
import {initialFilter} from '../components/StatsFilters';
import {getCatalog} from '../api/catalog';
import {ModelPriceCatalog} from '../components/ModelPriceCatalog';
import {SubscriptionPlans} from '../components/SubscriptionPlans';
import {providerNames} from '../format';
import {ErrorState,LoadingState} from '../components/QueryState';

export default function Pricing({subscriptionContent,initialTab='models'}:{subscriptionContent?:ReactNode;initialTab?:'models'|'plans'}={}){
 const [params]=useSearchParams();
 const [tab,setTab]=useState(initialTab);
 const [provider,setProvider]=useState(params.get('provider')??'');
 const query=useQuery({queryKey:['catalog'],queryFn:({signal})=>getCatalog(signal)});
 const data=query.data;
 const [recentRange]=useState(initialFilter);
 const usage=useQuery({queryKey:['usage',recentRange],queryFn:({signal})=>getUsage(recentRange,signal)});
 const used=new Set(usage.data?.models.map(r=>`${r.provider}:${r.model}`)??[]);
 return <section>{(tab==='models'||!subscriptionContent)&&<div className="stats-toolbar-wrap"><div className="stats-toolbar">{tab==='models'?<Select className="provider-select" aria-label="价目平台" value={provider} onChange={setProvider} options={[{value:'',label:'全部平台'},...Object.entries(providerNames).map(([value,label])=>({value,label}))]}/>:<span/>}<Button icon={<ReloadOutlined />} aria-label="刷新价目" loading={query.isFetching||usage.isFetching} onClick={()=>{void query.refetch();void usage.refetch();}} /></div></div>}
 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={()=>void query.refetch()} />:data&&<>
 {query.error&&<Alert type="warning" title="更新失败，保留上次价格目录" description={query.error.message} />}
 <Tabs activeKey={tab} onChange={key=>setTab(key==='plans'?'plans':'models')} items={[{key:'models',label:'模型价格',children:<ModelPriceCatalog models={data.models} used={used} provider={provider} initialSearch={params.get('model')??''} initialVersion={params.get('version')??''} usageError={usage.error} usageLoading={usage.isPending}/>},{key:'plans',label:'订阅与额度',children:subscriptionContent??<SubscriptionPlans plans={data.plans} initialProvider={provider||'codex'}/>}]} />
 {tab==='plans'&&<Collapse ghost size="small" items={[{key:'basis',label:'订阅与实际费用的关系',children:<Typography.Paragraph type="secondary">套餐支出、API 参考折算与订阅 Credits 分开显示；订阅价格不推算固定 Token 配额，实际额度与 reset 来自账号观测。Cursor 和 Grok 的上报费用与参考估算分别保留，不重复相加。</Typography.Paragraph>}]} />}
 </>}
 </section>;
}
