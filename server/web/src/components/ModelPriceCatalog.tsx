import {useState} from 'react';
import {Alert,Card,Collapse,Descriptions,Input,Segmented,Select,Table,Tag,Typography} from 'antd';
import {Link} from 'react-router-dom';
import {referenceAmount,sourceLink,type ModelPrice} from '../api/catalog';
import {dayjs,dateTime,providerNames} from '../format';

// 2026-10-03 核对 https://learn.chatgpt.com/docs/models；旧型号仍可从实际用量和历史证据查阅。
const codexReferenceModels=new Set(['gpt-6.1-sol','gpt-6-astra','gpt-6-sol','gpt-6-luna','gpt-5.6-sol','gpt-5.6-terra','gpt-5.6-luna']);
type Scope='related'|'used'|'history';
interface ModelGroup extends ModelPrice {rates:ModelPrice[];used:boolean;usageModel:string}
interface Props {models:ModelPrice[];used:ReadonlySet<string>;provider:string;initialSearch?:string;initialVersion?:string;usageError?:Error|null;usageLoading?:boolean}

function modelName(row:Pick<ModelPrice,'provider'|'model'>):string {
 return row.provider==='cursor'?row.model.replace(/(?:\s+500k)?\s+\(Fast\)$|\s+500k$/i,''):row.model;
}
function modelKey(row:Pick<ModelPrice,'provider'|'model'>):string {
 const name=modelName(row);
 return `${row.provider}:${row.provider==='codex'?name.toLowerCase().replace(/\s+/g,'-'):name.toLowerCase()}`;
}
function isTextRate(row:ModelPrice):boolean {
 return row.currency==='USD'&&row.unit==='1M tokens'&&!/Batch|Flex|Audio|Image|Embedding|语音|转录/i.test(row.mode)&&!/(?:image|audio|realtime|transcribe|embedding|tts|gpt-live|cyber)/i.test(row.model);
}
function isReferenceRate(row:ModelPrice,used:boolean):boolean {
 return isTextRate(row)||used&&row.currency==='USD'&&!/Batch|Flex/i.test(row.mode);
}
function baselineOrder(a:ModelPrice,b:ModelPrice):number {
 const rank=(r:ModelPrice)=>r.evidence==='observed'?10:(/Ultrafast/i.test(r.mode)?4:/Fast/i.test(`${r.mode} ${r.model}`)?2:0)+(/长上下文|500k/i.test(`${r.mode} ${r.model}`)?1:0);
 return rank(a)-rank(b)||(b.effective_from_ms??b.verified_at_ms)-(a.effective_from_ms??a.verified_at_ms)||a.key.localeCompare(b.key);
}
function releaseOrder(a:ModelPrice,b:ModelPrice):number {
 const left=a.released_at_ms??-Infinity,right=b.released_at_ms??-Infinity;
 return left===right?a.model.localeCompare(b.model)||a.provider.localeCompare(b.provider):right-left;
}
function source(url:string,label='官方来源') {
 const href=sourceLink(url);
 return href?<a href={href} target="_blank" rel="noopener noreferrer">{label}</a>:<Typography.Text type="secondary">无公开来源</Typography.Text>;
}
const releaseDate=(row:ModelPrice)=>row.released_at_ms==null?'未知':dayjs(row.released_at_ms).utc().format('YYYY-MM-DD');
const priceColumns=(keys:('input_price'|'cached_price'|'cache_write_price'|'output_price')[])=>keys.map(key=>({title:({input_price:'输入',cached_price:'缓存输入',cache_write_price:'缓存写入',output_price:'输出'})[key],align:'right' as const,width:110,render:(_:unknown,row:ModelPrice)=>referenceAmount(row[key],row.currency)}));

function RateDetails({row}:{row:ModelGroup}) {
 const special=row.rates.filter(r=>r.evidence==='current'&&isReferenceRate(r,row.used)&&r.key!==row.key).sort(baselineOrder);
 const credits=row.rates.filter(r=>r.evidence==='current'&&r.currency==='credits');
 const historical=row.rates.filter(r=>r.evidence==='historical');
 const ratesTable=(rates:ModelPrice[])=><Table<ModelPrice> rowKey="key" size="small" dataSource={rates} pagination={false} scroll={{x:850}} columns={[{title:'计费条件',width:240,render:(_,r)=><>{r.mode}{r.model!==row.model&&<div className="metric-note">{r.model}</div>}<div className="metric-note">{r.currency} / {r.unit}</div></>},...priceColumns(['input_price','cached_price','cache_write_price','output_price']),{title:'来源与说明',width:240,render:(_,r)=><>{source(r.source_url)}<div className="metric-note">{r.version}</div><div className="metric-note">{r.notes}</div></>}]} />;
 return <div className="catalog-details"><Descriptions size="small" column={{xs:1,md:2}} items={[
  {key:'release',label:'发布时间',children:releaseDate(row)},
  {key:'release-source',label:'发布来源',children:source(row.release_source_url??'','发布说明')},
  {key:'version',label:'价格版本',children:row.version||'未定价'},
  {key:'verified',label:'核对日期',children:row.verified_at_ms?dayjs(row.verified_at_ms).utc().format('YYYY-MM-DD'):'尚未核对'},
  {key:'effective',label:'生效边界',children:row.effective_from_ms===null?'公开页面未提供精确生效时间':dateTime(row.effective_from_ms)},
  {key:'write',label:'基础价缓存写入',children:referenceAmount(row.cache_write_price,row.currency)},
  {key:'source',label:'价格来源',children:source(row.source_url)},
  {key:'basis',label:'适用说明',children:row.notes||'按该模型官方规则使用'},
 ]}/><Collapse ghost size="small" items={[
  ...(special.length?[{key:'special',label:`其他计费条件（${special.length}）`,children:ratesTable(special)}]:[]),
  ...(credits.length?[{key:'credits',label:'订阅 Credits 参考',children:<><p className="metric-note">Credits 与 API 美元单价分别计价，额度以账号实际观测为准。</p>{ratesTable(credits)}</>}]:[]),
  ...(historical.length?[{key:'history',label:`历史计价证据（${historical.length}）`,children:ratesTable(historical)}]:[]),
 ]}/></div>;
}

export function ModelPriceCatalog({models,used,provider,initialSearch='',initialVersion='',usageError,usageLoading=false}:Props) {
 const [scope,setScope]=useState<Scope>(initialVersion?'history':'related');
 const [search,setSearch]=useState(initialSearch);
 const [version,setVersion]=useState(initialVersion);
 const usedModels=new Set([...used].map(value=>{const split=value.indexOf(':');return modelKey({provider:value.slice(0,split),model:value.slice(split+1)});}));
 const grouped=new Map<string,ModelPrice[]>();
 for(const row of models){const key=modelKey(row),group=grouped.get(key);if(group)group.push(row);else grouped.set(key,[row]);}
 const rows:ModelGroup[]=[];
 for(const [key,rates] of grouped){
  const isUsed=usedModels.has(key);
  const candidates=rates.filter(r=>scope==='history'?r.evidence==='historical'&&(!version||r.version===version):r.evidence==='observed'||r.evidence==='current'&&isReferenceRate(r,isUsed));
  const primary=[...candidates].sort(baselineOrder)[0];
  if(!primary||provider&&primary.provider!==provider)continue;
  const relevant=primary.evidence==='observed'||isUsed||rates.some(r=>r.model.toLowerCase()===search.trim().toLowerCase())||primary.provider==='cursor'||primary.provider==='grok'||primary.provider==='codex'&&codexReferenceModels.has(modelKey(primary).slice('codex:'.length));
  if(scope==='related'&&!relevant||scope==='used'&&!isUsed)continue;
  if(search&&!rates.some(r=>r.model.toLowerCase().includes(search.toLowerCase())))continue;
  const usageKey=[...used].find(value=>{const split=value.indexOf(':');return modelKey({provider:value.slice(0,split),model:value.slice(split+1)})===key;});
  rows.push({...primary,model:modelName(primary),rates,used:isUsed,usageModel:usageKey?.slice(usageKey.indexOf(':')+1)??primary.model});
 }
 rows.sort(releaseOrder);
 const versions=[...new Set(models.filter(r=>r.evidence==='historical'&&(!provider||r.provider===provider)).map(r=>r.version).filter(Boolean))];
 return <>
  <div className="catalog-toolbar"><Segmented aria-label="使用目录" value={scope} onChange={value=>setScope(value as Scope)} options={[{value:'related',label:'相关模型'},{value:'used',label:'近30天已使用'},{value:'history',label:'历史计价'}]}/><Input.Search aria-label="搜索模型价格" placeholder="搜索模型" value={search} onChange={event=>setSearch(event.target.value)} allowClear style={{maxWidth:320}}/>{scope==='history'&&<Select aria-label="价格版本" value={version} onChange={setVersion} showSearch={{optionFilterProp:'label'}} options={[{value:'',label:'全部历史价格版本'},...versions.map(value=>({value,label:value}))]}/>}</div>
  {usageError&&<Alert type="warning" title="已使用模型读取失败，目录仍可查，无法确认使用情况" description={usageError.message}/>}
  <div className="metric-note catalog-note">{scope==='history'?'历史费率证据 · 展开查看各版本 · 不改写已记录成本':`API 参考折算 · ${rows.some(r=>r.evidence!=='observed'&&r.unit!=='1M tokens')?'价格单位见模型说明':'USD / 百万 Token'} · 默认基础文本价，特殊条件展开查看`}<span className="catalog-order-note">按发布时间从新到旧 · 未确认日期置后</span></div>
  <Card className="catalog-table"><Table<ModelGroup> rowKey={row=>modelKey(row)} size="small" dataSource={rows} loading={scope==='used'&&usageLoading} pagination={{pageSize:25,showSizeChanger:true,showTotal:n=>`共 ${n} 个模型`}} scroll={{x:920}} locale={{emptyText:scope==='used'&&usageError?'无法确认已使用模型':'没有符合条件的模型'}} expandable={{expandedRowRender:row=><RateDetails row={row}/>}} columns={[
   {title:'模型',width:250,render:(_,row)=><><Typography.Text strong>{row.model}</Typography.Text><div className="metric-note">{row.evidence==='observed'?'参考价未知':row.mode}{row.used&&<Tag>已使用</Tag>}</div>{row.evidence!=='observed'&&row.unit!=='1M tokens'&&<div className="metric-note">{row.currency} / {row.unit}</div>}</>},
   {title:'发布时间',width:115,render:(_,row)=>releaseDate(row)},
   {title:'平台',width:90,render:(_,row)=>providerNames[row.provider]??row.provider},
   ...priceColumns(['input_price','cached_price','output_price']),
   {title:'用量',width:90,render:(_,row)=><Link to={`/usage/models?provider=${encodeURIComponent(row.provider)}&model=${encodeURIComponent(row.usageModel)}`}>查看用量</Link>},
  ]}/></Card>
  <Collapse ghost size="small" items={[{key:'comparison',label:'API 参考折算与订阅费用怎么比较',children:<Typography.Paragraph type="secondary">API 参考折算按模型 Token 与适用的历史费率估算，缓存输入按缓存价计算；订阅费用是账号的套餐支出，两者可以比较同一时间范围的使用强度。参考折算不代表实际账单，不用于反推订阅剩余额度。默认展示 Standard 基础文本参考价；Fast、长上下文和缓存写入条件可展开核对，订阅倍率不直接套用 API 金额。当前价目不重新计算过去费用。</Typography.Paragraph>}]} />
 </>;
}
