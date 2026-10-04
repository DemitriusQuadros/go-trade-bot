import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // Every backend route lives under /api (see cmd/api/main.go's
      // NewServeMux), a namespace no SPA client-side route ever uses, so a
      // plain path-prefix proxy is enough - no more collision between e.g.
      // GET /backtest (API) and the SPA's /backtest page to work around.
      // This one rule already covers /api/agent/runs[...] too - no special
      // case needed for the agent feature's routes.
      '/api': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
  },
});
