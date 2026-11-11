import type {Meta,StoryObj} from '@storybook/react-vite';
import {SubscriptionPlans} from '../components/SubscriptionPlans';
import {reviewCatalog} from './reviewFixtures';
import {ReviewApp} from './ReviewApp';

const meta={id:'subscription-plans',title:'订阅与模型价目',component:SubscriptionPlans,parameters:{layout:'fullscreen'},decorators:[Story=><div className="storybook-component"><Story/></div>],args:{plans:reviewCatalog.plans,initialProvider:'codex'},argTypes:{plans:{control:false},initialProvider:{control:'select',options:['codex','cursor','grok']}}} satisfies Meta<typeof SubscriptionPlans>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Codex:Story={name:'01 Codex · 当前订阅组件'};
export const Cursor:Story={name:'02 Cursor · 当前订阅组件',args:{initialProvider:'cursor'}};
export const Grok:Story={name:'03 Grok · 当前订阅组件',args:{initialProvider:'grok'}};
export const Business:Story={name:'04 个人、团队与其他渠道切换'};
export const ModelPrices:Story={name:'05 当前价目表页面',render:()=> <ReviewApp initialPath="/pricing"/>};
export const FocusedModelPrices:Story={name:'06 模型与历史版本',render:()=> <ReviewApp initialPath="/pricing?provider=codex&model=gpt-6.1-sol"/>};
export const ProductionSubscriptions:Story={name:'07 价目表与订阅页签',render:()=> <ReviewApp initialPath="/pricing?provider=codex"/>};
