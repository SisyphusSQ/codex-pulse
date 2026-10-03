import type {Meta,StoryObj} from '@storybook/react-vite';
import {SubscriptionPlansPreview} from './SubscriptionPlansDesign';
import {PricingReview} from './PricingReview';

const meta={id:'subscription-plans',title:'订阅分类样稿',component:SubscriptionPlansPreview,parameters:{layout:'fullscreen'},args:{initialPlatform:'codex',initialAudience:'personal',initialTab:'plans'},argTypes:{initialPlatform:{control:'select',options:['codex','cursor','grok']},initialAudience:{control:'select',options:['personal','business']},initialTab:{control:'select',options:['models','plans']}}} satisfies Meta<typeof SubscriptionPlansPreview>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Codex:Story={name:'01 Codex · 个人套餐'};
export const Cursor:Story={name:'02 Cursor · 个人套餐',args:{initialPlatform:'cursor'}};
export const Grok:Story={name:'03 Grok · 个人套餐',args:{initialPlatform:'grok'}};
export const Business:Story={name:'04 团队与企业',args:{initialAudience:'business'}};
export const ModelPrices:Story={name:'05 模型价格与订阅切换',args:{initialTab:'models'}};
export const FocusedModelPrices:Story={name:'06 相关模型与 API 参考折算',args:{initialTab:'models'}};
export const ProductionSubscriptions:Story={name:'07 正式订阅分类与目录证据',render:()=> <PricingReview initialTab="plans" subscriptionContent={undefined}/>};
