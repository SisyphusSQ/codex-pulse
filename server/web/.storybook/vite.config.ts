import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 独立设计环境：不继承业务 API proxy。复用业务页的故事由合成适配器拦截 API。
export default defineConfig({ plugins: [react()], server: { host: '127.0.0.1', proxy: {} }, build: { target: 'es2022' } });
