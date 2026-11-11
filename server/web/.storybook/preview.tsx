import type { Preview } from '@storybook/react-vite';
import { App, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { pulseTheme } from '../src/theme';
import 'antd/dist/reset.css';
import '../src/styles.css';
import './preview.css';

const preview: Preview = {
  decorators: [Story => <ConfigProvider locale={zhCN} theme={pulseTheme}><App><Story /></App></ConfigProvider>],
  parameters: { viewport:{options:{desktop:{name:'桌面 1440px',styles:{width:'1440px',height:'1000px'},type:'desktop'},tablet:{name:'平板 768px',styles:{width:'768px',height:'1024px'},type:'tablet'},mobile:{name:'窄屏 390px',styles:{width:'390px',height:'844px'},type:'mobile'}}}, layout: 'fullscreen', controls: { expanded: true }, options: { storySort: { order: ['全站设计', '关键状态', '公共组件', '订阅与模型价目', 'Credits 余额与消耗', '功能场景'] } } },
};
export default preview;
