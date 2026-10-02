import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import 'antd/dist/reset.css';
import './styles.css';
import { createQueryClient, PulseApp } from './App';
import { pulseTheme } from './theme';

const queryClient = createQueryClient();
createRoot(document.getElementById('root')!).render(<StrictMode><ConfigProvider locale={zhCN} theme={pulseTheme}><PulseApp queryClient={queryClient} /></ConfigProvider></StrictMode>);
