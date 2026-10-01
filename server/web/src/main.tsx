import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import 'antd/dist/reset.css';
import './styles.css';
import { createQueryClient, PulseApp } from './App';

const queryClient = createQueryClient();
createRoot(document.getElementById('root')!).render(<StrictMode><ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: '#137b59', colorInfo: '#137b59', fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif', borderRadius: 8, colorBgLayout: '#f6f7f9' } }}><PulseApp queryClient={queryClient} /></ConfigProvider></StrictMode>);
