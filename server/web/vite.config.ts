import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    strictPort: true,
    proxy: {
      '/api': { target: process.env.PULSE_WEB_PROXY_TARGET ?? 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
  build: { sourcemap: false, target: 'es2022', rolldownOptions: { output: { strictExecutionOrder: true, codeSplitting: { groups: [{ name: 'vendor', test: /node_modules/, minSize: 30_000, maxSize: 400_000, priority: 10 }] } } } },
  test: { environment: 'jsdom', setupFiles: ['./src/test/setup.ts'], restoreMocks: true },
});
