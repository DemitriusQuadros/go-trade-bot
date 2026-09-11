import { defineConfig, type ProxyOptions } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';
import type { IncomingMessage } from 'http';

// Some backend REST prefixes (e.g. /settings, /candles, /backtest) collide
// with client-side SPA route paths of the same name. An XHR/fetch call never
// sets an `Accept: text/html` header, but a real browser document navigation
// (typing the URL, hitting refresh, or Vite's own dev-server request for the
// page shell) always does - use that to tell the two apart. Without this,
// a full-page load/refresh on e.g. /settings gets proxied straight to the
// Go backend's JSON handler instead of falling through to Vite's SPA
// fallback (index.html), and the page never renders.
function apiProxy(): ProxyOptions {
  return {
    target: 'http://localhost:8080',
    bypass: (req: IncomingMessage) => {
      const accept = req.headers.accept || '';
      if (accept.includes('text/html')) {
        return req.url; // let Vite handle it as a normal page navigation
      }
      return undefined; // proxy it
    },
  };
}

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
      '/strategy': apiProxy(),
      '/signal': apiProxy(),
      '/account': apiProxy(),
      '/broker': apiProxy(),
      '/backtest': apiProxy(),
      '/optimize': apiProxy(),
      '/performance': apiProxy(),
      '/stream': apiProxy(),
      '/settings': apiProxy(),
      '/candles': apiProxy(),
      '/api': apiProxy(),
    },
  },
  build: {
    outDir: 'dist',
  },
});
