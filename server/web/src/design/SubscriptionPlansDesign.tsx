import { useEffect, useState } from 'react';
import { Button, Card, Collapse, Segmented, Select, Tabs, Tag } from 'antd';
import { CheckOutlined, ExportOutlined, InfoCircleOutlined } from '@ant-design/icons';
import { PricingReview } from './PricingReview';

type Platform = 'codex' | 'cursor' | 'grok';
interface Plan {
  name: string; price: string; unit?: string; audience: string;
  features: string[]; variants?: {label: string; value: string}[]; selected?: boolean;
}
const references = {
  codex: { name: 'Codex / ChatGPT', url: 'https://chatgpt.com/codex/pricing/', detail: 'https://learn.chatgpt.com/docs/pricing', caption: 'ChatGPT 套餐包含 Codex。按使用强度选择档位。' },
  cursor: { name: 'Cursor', url: 'https://cursor.com/pricing', detail: 'https://cursor.com/docs/models-and-pricing', caption: '从个人开发到团队协作，按 Agent 使用量选择。' },
  grok: { name: 'Grok', url: 'https://grok.com/plans', detail: 'https://x.ai/grok', caption: '聊天、搜索与创作，按能力和使用上限选择。' },
};
// 官方购买页与价目文档于 2026-10-03 核对。只供样稿，不写回业务目录。
const personal: Record<Platform, Plan[]> = {
  codex: [
    { name: 'Free / Go', price: '0', audience: '先体验 Codex', variants: [{label:'Free',value:'0'},{label:'Go',value:'8'}], features: ['有限的 Codex 试用', 'Go 提高 ChatGPT 消息与上传额度', '适合轻量体验'] },
    { name: 'Plus', price: '20', audience: '每周几次专注的编程', features: ['更高的 Codex 使用量', '桌面、CLI 与 IDE 工作流', '可购买 Credits 延长使用'] },
    { name: 'Pro', price: '200', audience: '每天长时间、高强度工作', selected: true, variants: [{label:'$100',value:'100'},{label:'$200',value:'200'},{label:'$500',value:'500'}], features: ['包含 Plus 的能力', '按使用强度选择三档套餐', '$500 档可使用 Astra Ultrafast'] },
  ],
  cursor: [
    { name: 'Hobby', price: '0', audience: '轻量试用与探索', features: ['有限的 Agent 请求', '可使用 Composer', '无需信用卡'] },
    { name: 'Individual', price: '60', audience: '日常 Agent 开发', selected: true, variants: [{label:'Pro',value:'20'},{label:'Pro+',value:'60'},{label:'Ultra',value:'200'}], features: ['更高的 Agent 额度', '前沿模型与云端 Agent', '支持 MCP、技能和钩子'] },
  ],
  grok: [
    { name: 'SuperGrok Lite', price: '10', audience: '基础聊天与轻量创作', features: ['更高的常规使用上限', '专家模式与图像、视频试用', '更长的对话'] },
    { name: 'SuperGrok', price: '30', audience: '更快的回答与更多创作', selected: true, features: ['包含 Lite 的能力', '更高限额与更快响应', 'Grok Bot、语音与创作工具'] },
    { name: 'SuperGrok Heavy', price: '300', audience: '复杂问题与密集使用', features: ['最高档使用额度与速度', '多 Agent 协作能力', '专属支持与抢先体验'] },
  ],
};
const business: Record<Platform, Plan[]> = {
  codex: [
    { name: 'Business', price: '25', unit: '/ 席位 / 月', audience: '小团队与成长中的组织', variants: [{label:'月付 $25',value:'25'},{label:'年付 $20/月',value:'20'}], features: ['2 席位起', '集中管理与工作区', '可用 Credits 扩展用量'] },
    { name: 'Enterprise', price: '联系销售', audience: '有组织治理需求的企业', features: ['按合同确定价格与额度', '组织管理与安全控制', '可采用灵活的 Credits 计费'] },
    { name: 'Edu', price: '联系销售', audience: '教育机构', features: ['机构合同与专属工作区', '具体额度依组织协议', '与个人套餐分开管理'] },
  ],
  cursor: [
    { name: 'Teams', price: '40', unit: '/ 席位 / 月', audience: '协作交付的软件团队', variants: [{label:'Standard',value:'40'},{label:'Premium',value:'120'}], features: ['集中计费与团队用量分析', '团队隐私与 SSO', '共享上下文与代码审查'] },
    { name: 'Enterprise', price: '联系销售', audience: '规模化组织', features: ['共享用量与权限控制', '审计日志与服务账号', '组织结算与优先支持'] },
  ],
  grok: [
    { name: 'Business', price: '官网确认', audience: '团队协作使用 Grok', features: ['团队套餐与组织计费', '额度以购买页和组织协议为准', '按账号观测查看真实用量'] },
    { name: 'Enterprise', price: '联系销售', audience: '企业与组织', features: ['定制套餐与合同', '组织级管理和支持', '不从个人套餐推算组织额度'] },
  ],
};
const rules: Record<Platform, {title: string; text: string}[]> = {
  codex: [{title:'套餐额度',text:'本地与云端任务共享套餐额度；不同模型和任务的消耗不同。'}, {title:'窗口与 Credits',text:'实际窗口、reset 与剩余额度在账号页查看；额外 Credits 独立展示。'}, {title:'API 使用',text:'API Key 按 API 价格计费，订阅月费不折算为固定 Token。'}],
  cursor: [{title:'两个用量池',text:'Cursor Models 与 Other Models 分别展示，用量随模型和套餐变化。'}, {title:'账期与额外用量',text:'用量池按实际账期重置；按用量计费项目与订阅月费分开。'}, {title:'地区套餐',text:'印度 Start 等地区套餐单独列出，货币、含税条件保持原样。'}],
  grok: [{title:'共享用量',text:'具体可用额度与重置时间由账号 Usage 观测提供，未公开数值保持未知。'}, {title:'购买渠道',text:'Grok 订阅、X 会员和 xAI API 属于不同购买渠道，分别查看。'}, {title:'账户用量',text:'套餐能力供比较；最后一次有效用量及其更新时间在账号页保留。'}],
};
function PlanCard({plan, platform}: {plan: Plan; platform: Platform}) {
  const [price,setPrice] = useState(plan.price);
  const numeric = /^\d+$/.test(price);
  const isAnnual = platform === 'codex' && plan.name === 'Business' && price === '20';
  return <Card className={`ds-purchase-card${plan.selected?' ds-purchase-current':''}`}>
    <div className="ds-purchase-card-heading"><h3>{plan.name}</h3>{plan.selected&&price===plan.price&&<Tag color="blue">示例账号在用</Tag>}</div>
    <p className="ds-purchase-audience">{plan.audience}</p>
    <div className="ds-purchase-price"><strong>{numeric?`$${price}`:price}</strong>{numeric&&<span>USD {plan.unit??'/ 月'}</span>}</div>
    <div className="ds-purchase-variant">{plan.variants?<Select aria-label={`${plan.name}套餐档位`} value={price} onChange={setPrice} options={plan.variants} />:<span>{numeric?'月付参考价':'具体价格以官网为准'}</span>}{isAnnual&&<small>按年支付，每席位每月折算</small>}</div>
    <Button block type={plan.selected?'primary':'default'} href={references[platform].url} target="_blank" rel="noopener noreferrer" icon={<ExportOutlined />}>查看官方套餐</Button>
    <ul className="ds-purchase-features">{plan.features.map(feature=><li key={feature}><CheckOutlined/><span>{feature}</span></li>)}</ul>
  </Card>;
}
export function SubscriptionPlansDesign({initialPlatform='codex',initialAudience='personal'}:{initialPlatform?:Platform;initialAudience?:'personal'|'business'}) {
  const [platform,setPlatform]=useState<Platform>(initialPlatform);
  const [audience,setAudience]=useState<'personal'|'business'>(initialAudience);
  useEffect(()=>setPlatform(initialPlatform),[initialPlatform]);
  useEffect(()=>setAudience(initialAudience),[initialAudience]);
  const reference=references[platform];
  const cards=audience==='personal'?personal[platform]:business[platform];
  return <section className={`ds-purchase ds-purchase-${platform}`}>
    <Tabs aria-label="订阅平台分类" activeKey={platform} onChange={key=>setPlatform(key as Platform)} items={Object.entries(references).map(([key,value])=>({key,label:value.name}))}/>
    <div className="ds-purchase-heading"><div><h2>{reference.name}</h2><p>{reference.caption}</p></div><Segmented aria-label="订阅类别" value={audience} onChange={value=>setAudience(value as 'personal'|'business')} options={[{label:'个人',value:'personal'},{label:'团队与企业',value:'business'}]}/></div>
    <div className="ds-purchase-grid" key={`${platform}:${audience}`}>{cards.map(plan=><PlanCard key={plan.name} plan={plan} platform={platform}/>)}</div>
    <div className="ds-purchase-rules"><h3>额度与计费怎么区分</h3><div>{rules[platform].map(rule=><article key={rule.title}><strong>{rule.title}</strong><p>{rule.text}</p></article>)}</div></div>
    <div className="ds-purchase-footer"><span><InfoCircleOutlined/> USD 参考价 · 税费与地区以购买渠道为准</span><a href={reference.detail} target="_blank" rel="noopener noreferrer">官方说明 · 核对于 2026-10-03</a><Button type="link" href="/?path=/story/too-523-updates--last-update">查看账号额度 →</Button></div>
    {platform==='cursor'&&<Collapse ghost size="small" items={[{key:'region',label:'地区套餐 · Start（印度）',children:<p>₹649 / 月，官网含税参考价。仅 Cursor Models 池；其他地区和渠道价格以官方说明为准。</p>}]}/>}
    {platform==='grok'&&<Collapse ghost size="small" items={[{key:'other',label:'其他来源套餐 · SuperGrok Plus',children:<p>当前中心参考目录保留 SuperGrok Plus $100/月；它未出现在本次 Grok 个人购买页中，暂放在其他来源，正式接入前再确认渠道与适用范围。</p>}]}/>}
  </section>;
}
export function SubscriptionPlansPreview({initialTab='plans',...props}:Parameters<typeof SubscriptionPlansDesign>[0]&{initialTab?:'models'|'plans'}) {
  return <PricingReview key={initialTab} initialTab={initialTab} subscriptionContent={<SubscriptionPlansDesign {...props}/>}/>;
}
