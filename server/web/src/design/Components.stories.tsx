import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react-vite';
import { Card } from 'antd';
import { CreditsDesign, designPaceOption, PaceContent } from './analytics';
import { DesignToolbar, ForecastStatus, initialDesignFilter } from './components';

function ToolbarPreview() { const [value, setValue] = useState(initialDesignFilter); return <div className="ds-component-preview"><DesignToolbar page="overview" value={value} onChange={setValue} onRefresh={() => setValue(initialDesignFilter)} /><Card title="当前筛选"><p>平台：{value.provider} · 来源：{value.source}</p><p>{value.start} — {value.end} · {value.zone}</p></Card></div>; }
const meta = { title: '公共组件', component: ToolbarPreview, parameters: { layout: 'padded' } } satisfies Meta<typeof ToolbarPreview>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Toolbar: Story = { name: '01 紧凑筛选与日期' };
export const Credits: Story = { name: '02 Credits 与到期明细', render: () => <Card className="ds-component-preview"><CreditsDesign /></Card> };
export const UnknownCredits: Story = { name: '03 Credits 库存未知', render: () => <Card className="ds-component-preview"><CreditsDesign scenario="stale" /></Card> };
export const PaceHelp: Story = { name: '04 节奏受限说明图标', render: () => <Card className="ds-component-preview"><ForecastStatus scenario="stale" /><p className="ds-muted">点击或悬停标题旁的图标查看原因。</p></Card> };
export const DensePace: Story = { name: '05 密集额度采样曲线', render: () => <Card className="ds-component-preview"><PaceContent scenario="normal" chart={designPaceOption} /></Card> };
