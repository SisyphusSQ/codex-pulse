import {useState} from 'react';
import {Button,Card,Collapse,Empty,Segmented,Select,Tabs,Typography} from 'antd';
import {ExportOutlined} from '@ant-design/icons';
import type {PlanPrice} from '../api/catalog';
import {referenceAmount,sourceLink} from '../api/catalog';
import {dayjs,providerNames} from '../format';

type Audience='personal'|'business'|'other';
const captions:Record<string,string>={codex:'ChatGPT 套餐包含 Codex，按使用强度选择档位。',cursor:'从个人开发到团队协作，按 Agent 使用量选择。',grok:'Grok 订阅、X 会员和 API 分别计费，实际额度在账号页查看。'};
function audience(plan:PlanPrice):Audience {
 if(plan.currency!=='USD'||(plan.provider==='grok'&&plan.name==='SuperGrok Plus'))return 'other';
 return /^(Business|Enterprise|Edu|Teams)/i.test(plan.name)?'business':'personal';
}
function groupName(plan:PlanPrice):string {
 if(plan.provider==='codex'&&/^Pro(?:\s|$)/.test(plan.name))return 'Pro';
 if(plan.provider==='codex'&&['Free','Go'].includes(plan.name))return 'Free / Go';
 if(plan.provider==='cursor'&&['Pro','Pro Plus','Ultra'].includes(plan.name))return 'Individual';
 if(plan.provider==='cursor'&&plan.name.startsWith('Teams '))return 'Teams';
 return plan.name;
}
function PlanCard({name,plans}:{name:string;plans:PlanPrice[]}) {
 const preferred=plans.find(p=>(name==='Pro'&&p.price==='200')||(name==='Individual'&&p.price==='60'))??plans[0];
 const [selected,setSelected]=useState(preferred.key);
 const plan=plans.find(p=>p.key===selected)??preferred;
 const source=sourceLink(plan.source_url);
 return <Card className="purchase-card" styles={{body:{height:'100%',display:'flex',flexDirection:'column'}}}>
  <h3>{name}</h3>
  <div className="purchase-price"><strong>{plan.price===null&&/Enterprise|Edu/.test(plan.name)?'联系销售':referenceAmount(plan.price,plan.currency)}</strong><span>{plan.currency} · {plan.cycle}</span></div>
  <div className="purchase-variant">{plans.length>1?<Select aria-label={`${name}套餐档位`} value={plan.key} onChange={setSelected} options={plans.map(p=>({value:p.key,label:`${p.name} · ${referenceAmount(p.price,p.currency)} · ${p.cycle}`}))}/>:<Typography.Text type="secondary">{plan.region}</Typography.Text>}</div>
  {source?<Button block href={source} target="_blank" rel="noopener noreferrer" icon={<ExportOutlined/>}>查看官方套餐</Button>:<Typography.Text type="secondary">无公开来源</Typography.Text>}
  <div className="purchase-features"><p>{plan.allowance}</p><p><strong>额度重置：</strong>{plan.reset_rule}</p></div>
  <div className="purchase-evidence"><span>{plan.region}</span><span>核对日期：{plan.verified_at_ms>0?dayjs(plan.verified_at_ms).utc().format('YYYY-MM-DD'):'未确认'}</span></div>
 </Card>;
}
export function SubscriptionPlans({plans,initialProvider='codex'}:{plans:PlanPrice[];initialProvider?:string}) {
 const platforms=[...new Set(['codex','cursor','grok',...plans.map(p=>p.provider)])];
 const [platform,setPlatform]=useState(platforms.includes(initialProvider)?initialProvider:'codex');
 const [category,setCategory]=useState<Audience>('personal');
 const groups=new Map<string,PlanPrice[]>();
 for(const plan of plans){if(plan.provider!==platform||audience(plan)!==category)continue;const name=groupName(plan);groups.set(name,[...(groups.get(name)??[]),plan]);}
 return <section className={`purchase-plans purchase-${platform}`}>
  <Tabs aria-label="订阅平台分类" activeKey={platform} onChange={setPlatform} items={platforms.map(key=>({key,label:providerNames[key]??key}))}/>
  <div className="purchase-heading"><div><h2>{platform==='codex'?'Codex / ChatGPT':providerNames[platform]??platform}</h2><p>{captions[platform]??'价格和额度以官方渠道与实际账号观测为准。'}</p></div><Segmented<Audience> aria-label="订阅类别" value={category} onChange={setCategory} options={[{label:'个人',value:'personal'},{label:'团队与企业',value:'business'},{label:'地区与其他渠道',value:'other'}]}/></div>
  {groups.size?<div className="purchase-grid" key={`${platform}:${category}`}>{[...groups].map(([name,rows])=><PlanCard key={name} name={name} plans={rows}/>)}</div>:<Empty description="当前目录未收录这一类别，请以官方购买页为准"/>}
  <div className="purchase-rules"><h3>额度与计费怎么区分</h3><div><article><strong>套餐支出</strong><p>月费、席位与年付折算按各自购买渠道展示，地区和税费条件保留。</p></article><article><strong>账号额度与用量</strong><p>窗口、剩余额度、reset 与 Credits 来自账号观测，保留最后更新时间。</p><a href="#/quota">查看账号额度 →</a></article><article><strong>API 参考折算</strong><p>模型价格用于比较已观测用量的 API 等价成本；订阅月费不推算固定 Token，也不与参考成本相加。</p></article></div></div>
  {platform==='grok'&&category==='other'&&<Collapse ghost size="small" items={[{key:'channel',label:'其他来源套餐说明',children:'SuperGrok Plus 保留原目录的价格与来源；不同购买渠道不合并为同一套餐，当前可购性请在官方渠道确认。'}]}/>}
 </section>;
}
