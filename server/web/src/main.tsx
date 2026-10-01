import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import 'antd/dist/reset.css';
import './styles.css';
import { createQueryClient, PulseApp } from './App';

const queryClient = createQueryClient();
createRoot(document.getElementById('root')!).render(<StrictMode><ConfigProvider locale={zhCN} theme={{ cssVar: {key: 'pulse'}, token: { fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif' }, components: { Layout: { headerBg: '#ffffff', siderBg: '#ffffff', headerHeight: 56 }, Statistic: { contentFontSize: 26 } } }}><PulseApp queryClient={queryClient} /></ConfigProvider></StrictMode>);
