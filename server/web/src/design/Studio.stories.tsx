import type { Meta, StoryObj } from '@storybook/react-vite';
import { Studio } from './Studio';
import { pageNames } from './fixtures';

const meta = {
  title: '全站设计', component: Studio,
  parameters: { layout: 'fullscreen' },
  args: { page: 'overview', scenario: 'normal', initialModel: 'all' },
  argTypes: {
    page: { control: 'select', options: Object.keys(pageNames) },
    scenario: { control: 'select', options: ['normal', 'stale', 'conflict', 'empty', 'loading', 'error'] },
    initialModel: { control: 'text' },
  },
} satisfies Meta<typeof Studio>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Overview: Story = { name: '01 用量概览' };
export const Models: Story = { name: '02 模型分析', args: { page: 'models' } };
export const Sources: Story = { name: '03 采集来源', args: { page: 'sources' } };
export const Accounts: Story = { name: '04 账号与额度', args: { page: 'accounts' } };
export const Projects: Story = { name: '05 项目分屏', args: { page: 'projects' } };
export const Sessions: Story = { name: '06 会话分屏', args: { page: 'sessions' } };
export const Pricing: Story = { name: '07 模型与订阅价目', args: { page: 'pricing' } };
export const Devices: Story = { name: '08 设备与授权', args: { page: 'devices' } };
export const SignIn: Story = { name: '09 浏览器配对', args: { page: 'signin' } };
