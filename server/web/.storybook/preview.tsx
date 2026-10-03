import type { Preview } from '@storybook/react-vite';
import { App, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { pulseTheme } from '../src/theme';
import '../src/styles.css';
import '../src/design/studio.css';

const preview: Preview = {
  decorators: [Story => <ConfigProvider locale={zhCN} theme={pulseTheme}><App><Story /></App></ConfigProvider>],
  parameters: { layout: 'fullscreen', controls: { expanded: true }, options: { storySort: { order: ['订阅分类样稿', '本轮优化', '全站设计', '关键状态', '公共组件'] } } },
};
export default preview;
