import type { Meta, StoryObj } from '@storybook/react-vite';
import { Studio } from './Studio';

const meta = { title: '关键状态', component: Studio, args: { page: 'accounts', scenario: 'stale' }, parameters: { layout: 'fullscreen' } } satisfies Meta<typeof Studio>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Stale: Story = { name: '01 陈旧观测与库存未知' };
export const Conflict: Story = { name: '02 来源冲突与预测暂停', args: { scenario: 'conflict' } };
export const Empty: Story = { name: '03 无数据', args: { page: 'overview', scenario: 'empty' } };
export const Loading: Story = { name: '04 读取中', args: { page: 'overview', scenario: 'loading' } };
export const Error: Story = { name: '05 读取失败与重试', args: { page: 'overview', scenario: 'error' } };
export const ModelDetail: Story = { name: '06 单模型分析', args: { page: 'models', scenario: 'normal', initialModel: 'codex:gpt-5.4' } };
export const UnknownModel: Story = { name: '07 未归因模型', args: { page: 'models', scenario: 'normal', initialModel: 'unknown' } };
export const StaleDevices: Story = { name: '08 设备观测陈旧', args: { page: 'devices', scenario: 'stale' } };
