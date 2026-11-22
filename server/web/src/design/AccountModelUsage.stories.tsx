import type { Meta, StoryObj } from '@storybook/react-vite';
import type { AccountModelRow } from '../api/quotas';
import { ReviewApp } from './ReviewApp';

const models: AccountModelRow[] = [
  {kind:'model',model:'gpt-6.2-astra',name:'GPT-6.2 Astra',total_tokens:'728200000',input_tokens:'720000000',cached_tokens:'674000000',output_tokens:'3400000',reasoning_tokens:'4800000'},
  {kind:'model',model:'gpt-6.2-sol',name:'GPT-6.2 Sol',total_tokens:'275100000',input_tokens:'270000000',cached_tokens:'243000000',output_tokens:'2900000',reasoning_tokens:'2200000'},
  {kind:'model',model:'gpt-6.1-terra',name:'GPT-6.1 Terra',total_tokens:'82100000',input_tokens:'80000000',cached_tokens:'72000000',output_tokens:'1300000',reasoning_tokens:'800000'},
];

const complete = {recorded_tokens:'1085400000',models};
const partial = {
  recorded_tokens: complete.recorded_tokens,
  models: [
    ...models.slice(0, 2),
    {kind:'unknown_model',model:null,name:null,total_tokens:'63300000',input_tokens:'62000000',cached_tokens:'53000000',output_tokens:'900000',reasoning_tokens:'400000'},
    {kind:'unmatched',model:null,name:null,total_tokens:'18800000',input_tokens:null,cached_tokens:null,output_tokens:null,reasoning_tokens:null},
  ] satisfies AccountModelRow[],
};

const meta = {
  id: 'too-724-account-model-usage',
  title: '账号周期模型用量',
  component: ReviewApp,
  parameters: {layout:'fullscreen',docs:{disable:true}},
  args: {initialPath:'/quota',ticket:'TOO-724',previewTitle:'账号模型用量',modelUsage:complete},
  play: async ({canvas, userEvent}) => {
    await userEvent.click(await canvas.findByRole('button',{name:'本周期模型用量'}));
    await canvas.findByText('GPT-6.2 Astra');
  },
} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Complete: Story = {name:'01 账号页 · 按模型展开'};
export const Partial: Story = {name:'02 部分明细未知',args:{modelUsage:partial}};
export const Mobile: Story = {name:'03 窄屏账号页',globals:{viewport:{value:'mobile',isRotated:false}}};
