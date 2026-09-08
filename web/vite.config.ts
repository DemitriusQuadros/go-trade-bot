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
      '/strategy': 'http://localhost:8080',
      '/signal': 'http://localhost:8080',
      '/account': 'http://localhost:8080',
      '/broker': 'http://localhost:8080',
      '/backtest': 'http://localhost:8080',
      '/optimize': 'http://localhost:8080',
      '/performance': 'http://localhost:8080',
      '/stream': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
  },
});
