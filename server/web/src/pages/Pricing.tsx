import {useState} from 'react';
import {Alert,Card,Collapse,Descriptions,Input,Select,Table,Tabs,Tag,Typography} from 'antd';
import {useQuery} from '@tanstack/react-query';
import {Link,useSearchParams} from 'react-router-dom';
import {getCatalog,referenceAmount,sourceLink} from '../api/catalog';
import type {ModelPrice,PlanPrice} from '../api/catalog';
import {dayjs,dateTime,providerNames} from '../format';
import {ErrorState,LoadingState} from '../components/QueryState';

export default function Pricing(){
 const [params]=useSearchParams();
 const [provider,setProvider]=useState(params.get('provider')??'');
 const [search,setSearch]=useState(params.get('model')??'');
 const [evidence,setEvidence]=useState(params.get('version')?'all':'current');
 const [version,setVersion]=useState(params.get('version')??'');
 const [currency,setCurrency]=useState('USD');
 const query=useQuery({queryKey:['catalog'],queryFn:({signal})=>getCatalog(signal)});
 const data=query.data;
 const models=(data?.models??[]).filter(r=>(!provider||r.provider===provider)&&(!currency||r.currency===currency)&&(!version||r.version===version)&&(evidence==='all'||r.evidence===evidence||evidence==='current'&&r.evidence==='observed')&&r.model.toLowerCase().includes(search.toLowerCase()));
 const plans=(data?.plans??[]).filter(r=>!provider||r.provider===provider);
 const source=(url:string)=>{const href=sourceLink(url);return href?<a href={href} target="_blank" rel="noopener noreferrer">官方来源</a>:<Typography.Text type="secondary">无公开来源</Typography.Text>;};
 const compare=(key:'input_price'|'cached_price'|'cache_write_price'|'output_price')=>(a:ModelPrice,b:ModelPrice)=>a[key]===null?b[key]===null?0:1:b[key]===null?-1:Number(a[key])-Number(b[key]);
 return <section><div className="page-heading"><div><Typography.Title level={3}>价目表</Typography.Title><Typography.Paragraph type="secondary">模型参考价格、订阅套餐与额度规则，覆盖三个平台。</Typography.Paragraph></div></div>
 <div className="stats-filters"><div className="filter-control"><label>计费平台</label><Select aria-label="价目平台" value={provider} onChange={v=>{setProvider(v);setVersion('');}} options={[{value:'',label:'全部平台'},...Object.entries(providerNames).map(([value,label])=>({value,label}))]} /></div></div>
 {query.isPending?<LoadingState />:query.error&&!data?<ErrorState error={query.error} retry={()=>void query.refetch()} />:data&&<>
 {query.error&&<Alert type="warning" title="更新失败，保留上次价格目录" description={query.error.message} />}
 <Tabs items={[{key:'models',label:'模型价格',children:<>
 <div className="catalog-toolbar"><Input.Search aria-label="搜索模型价格" placeholder="搜索模型" value={search} onChange={e=>setSearch(e.target.value)} allowClear style={{maxWidth:320}} />
 <Select aria-label="价格目录范围" value={evidence} onChange={v=>{setEvidence(v);setVersion('');}} options={[{value:'current',label:'公开参考与未定价模型'},{value:'historical',label:'历史计价证据'},{value:'all',label:'全部目录'}]} />
 <Select aria-label="价格版本" value={version} onChange={setVersion} showSearch={{optionFilterProp:'label'}} options={[{value:'',label:'全部价格版本'},...[...new Set(data.models.filter(r=>!provider||r.provider===provider).map(r=>r.version).filter(Boolean))].map(value=>({value,label:value}))]} />
 <Select aria-label="价格单位" value={currency} onChange={setCurrency} options={[{value:'',label:'全部计价单位'},{value:'USD',label:'美元参考价'},{value:'credits',label:'Codex Credits'}]} />
 </div>
 <div className="metric-note catalog-note">同一模型的模式、模态和单位分别列出。统计使用已上报的历史费率；参考目录更新不会覆盖历史成本。</div>
 <Card className="catalog-table"><Table<ModelPrice> rowKey="key" size="small" dataSource={models} pagination={{pageSize:25,showSizeChanger:true,showTotal:n=>`共 ${n} 条参考价格`}} scroll={{x:1120}} expandable={{expandedRowRender:r=><Descriptions size="small" column={{xs:1,md:2}} items={[{key:'version',label:'价格版本',children:r.version||'未定价'},{key:'verified',label:'核对日期',children:r.verified_at_ms?dayjs(r.verified_at_ms).utc().format('YYYY-MM-DD'):'尚未核对'},{key:'effective',label:'生效边界',children:r.effective_from_ms===null?'公开页面未提供精确生效时间':dateTime(r.effective_from_ms)},{key:'basis',label:'适用说明',children:r.notes||'按该模型官方规则使用'},{key:'source',label:'价格来源',children:source(r.source_url)}]} />}} columns={[
 {title:'模型 / 计费条件',width:280,render:(_,r)=><><Typography.Text strong>{r.model}</Typography.Text><div className="metric-note">{r.mode}</div></>},
 {title:'平台',width:95,render:(_,r)=>providerNames[r.provider]??r.provider},
 {title:'计价单位',width:135,render:(_,r)=>`${r.currency==='credits'?'Credits':r.currency} / ${r.unit}`},
 ...(['input_price','cache_write_price','cached_price','output_price'] as const).map((key,i)=>({title:['输入','缓存写入','缓存读取','输出'][i],align:'right' as const,width:110,sorter:compare(key),render:(_:unknown,r:ModelPrice)=>referenceAmount(r[key],r.currency)})),
 {title:'目录',width:90,render:(_,r)=><Tag>{r.evidence==='current'?'参考':r.evidence==='historical'?'历史':'未知'}</Tag>},
 {title:'用量',width:90,render:(_,r)=><Link to={`/quota?view=usage&provider=${encodeURIComponent(r.provider)}&model=${encodeURIComponent(r.model)}`}>查看用量</Link>},
 ]} /></Card>
 </>},{key:'plans',label:'订阅与额度',children:<>
 <Alert type="info" showIcon title="套餐价格和使用规则仅作参考" description="实际账号的剩余额度、reset和Credits来自采集事实；订阅价格不推算固定Token数量。未公开价格保持未知。" className="form-alert" />
 <Card><Table<PlanPrice> rowKey="key" size="small" dataSource={plans} pagination={false} scroll={{x:960}} columns={[
 {title:'平台 / 套餐',width:170,render:(_,r)=><><Typography.Text strong>{r.name}</Typography.Text><div className="metric-note">{providerNames[r.provider]??r.provider}</div></>},
 {title:'参考价格',width:150,render:(_,r)=><><Typography.Text>{referenceAmount(r.price,r.currency)}</Typography.Text><div className="metric-note">{r.cycle}</div></>},
 {title:'包含额度与规则',dataIndex:'allowance',width:300},{title:'reset规则',dataIndex:'reset_rule',width:230},
 {title:'来源',width:90,render:(_,r)=>source(r.source_url)},
 ]} expandable={{expandedRowRender:r=><Descriptions size="small" items={[{key:'region',label:'地区与购买渠道',children:r.region},{key:'date',label:'核对日期',children:dayjs(r.verified_at_ms).utc().format('YYYY-MM-DD')}]} />}} /></Card>
 </>}]} />
 <Collapse ghost size="small" items={[{key:'basis',label:'参考价格与实际费用的关系',children:<Typography.Paragraph type="secondary">Codex：API基础文本等价估算与订阅Credits分别计价。Cursor：文档价目估算与Dashboard上报费用分开，reported charge已含适用的Cursor Token Rate，不重复相加。Grok：xAI参考价估算与完整reported cost分开。图片、视频和语音保留各自计费单位。</Typography.Paragraph>}]} />
 </>}
 </section>;
}
